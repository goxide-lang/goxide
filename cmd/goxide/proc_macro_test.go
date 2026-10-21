package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIProceduralMacroUsesRestrictedHost(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	env := append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	query := exec.Command(tool, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/syntax")
	query.Dir, query.Env = repo, env
	sdk, err := query.Output()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	binary := filepath.Join(root, "goxide")
	build := exec.Command(tool, "run", "./scripts/build.go", "-o", binary)
	build.Dir, build.Env = repo, env
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stamped CLI: %v\n%s", err, output)
	}
	t.Run("interrupt_before_macro_execution", func(t *testing.T) {
		testCLIInterruptDuringPreparation(t, binary, tool, env)
	})
	write := func(name, content string) {
		t.Helper()
		name = filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("macros/goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/cli-macros"
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies.syntax]
path=%q
`, strings.TrimSpace(string(sdk))))
	write("macros/lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func echo(ctx pm::Context, input token::TokenStream) pm::Output {
 var output = pm::Success(input)
 output.Diagnostics = append(output.Diagnostics, pm::Diagnostic{Severity: pm::Warning, Message: "macro warning", Span: ctx.CallSpan()})
 return output
}
`)
	write("app/goxide.toml", `[go_mod]
module="example.org/cli-consumer"
go="1.27"
[[bin]]
name="app"
path="."
[dependencies.macros]
path="../macros"
`)
	write("app/main.gox", `package main
use macros;
use fmt;
func main() { fmt::Println(macros::echo!(6 * 7), macros::echo!(3 * 3)) }
`)
	execute := func(args ...string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Dir, command.Env = root, env
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		return stdout.String(), stderr.String(), err
	}
	for _, args := range [][]string{{"run", "--offline", "app"}, {"run", "--offline", "--locked", "app"}} {
		stdout, stderr, err := execute(args...)
		if err != nil || stdout != "42 9\n" || !strings.Contains(stderr, "main.gox:4:") || strings.Count(stderr, "warning: macro warning") != 2 {
			t.Fatalf("CLI %v: %v\nstdout: %s\nstderr: %s", args, err, stdout, stderr)
		}
	}
	write("macros/lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func echo(ctx pm::Context, input token::TokenStream) pm::Output {
 var output = pm::Failure("first error at call", ctx.CallSpan())
 output.Diagnostics = append(output.Diagnostics, pm::Diagnostic{Severity: pm::Note, Message: "middle note at definition", Span: ctx.DefSpan()})
 output.Diagnostics = append(output.Diagnostics, pm::Diagnostic{Severity: pm::Error, Message: "last error at definition", Span: ctx.DefSpan()})
 return output
}
`)
	stdout, stderr, err := execute("run", "--offline", "--locked", "app")
	if err == nil || stdout != "" || !strings.Contains(stderr, "procedural macro expansion failed") || !strings.Contains(stderr, "macro defined at") || !strings.Contains(stderr, "; called at") {
		t.Fatalf("CLI macro failure: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	previous := -1
	for _, diagnostic := range []struct{ message, position string }{
		{"error: first error at call", "main.gox:4:"},
		{"note: middle note at definition", "lib.gox:4:1:"},
		{"error: last error at definition", "lib.gox:4:1:"},
	} {
		index := strings.Index(stderr, diagnostic.message)
		_, message, _ := strings.Cut(diagnostic.message, ": ")
		if index <= previous || strings.Count(stderr, message) != 1 {
			t.Fatalf("diagnostic order or count changed for %q:\n%s", diagnostic.message, stderr)
		}
		line := stderr[strings.LastIndex(stderr[:index], "\n")+1 : index]
		if !strings.Contains(line, diagnostic.position) {
			t.Fatalf("diagnostic position lost for %q:\n%s", diagnostic.message, stderr)
		}
		previous = index
	}
	for _, project := range []string{"app", "macros"} {
		for _, name := range []string{"go.mod", "go.sum"} {
			if _, err := os.Stat(filepath.Join(root, project, name)); !os.IsNotExist(err) {
				t.Fatalf("CLI wrote authoring %s/%s: %v", project, name, err)
			}
		}
	}
}
