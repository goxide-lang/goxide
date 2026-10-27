package format_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
	sourceformat "github.com/goxide-lang/goxide/internal/format"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
)

var wasiBudgetMultiplier time.Duration = 1

func TestFormattingLayoutAndIdempotence(t *testing.T) {
	source := "package main\nfunc  f [ T any ] (x T)T {return x}\nfunc main(){n:=f[int](1+2);println(n)}\n"
	want := "package main\nfunc f[T any](x T) T {\n\treturn x\n}\nfunc main() {\n\tn := f[int](1 + 2);\n\tprintln(n)\n}\n"
	got, err := sourceformat.Source(t.Context(), "main.gox", source, sourceformat.Options{})
	if err != nil || got != want {
		t.Fatalf("format: %v\ngot:\n%s\nwant:\n%s", err, got, want)
	}
	next, err := sourceformat.Source(t.Context(), "main.gox", got, sourceformat.Options{})
	if err != nil || next != got {
		t.Fatalf("second format changed output: %v\n%s", err, next)
	}
}

func TestFormattingPreservesCommentsMacrosDirectivesAndCRLF(t *testing.T) {
	source := "\ufeff//go:build !disabled\n\npackage p\n\n// Number documents a type.\n//goxide:check strict\ntype Number struct {\n// Value documents a field.\nValue int // field tail\n}\n\nmacro_rules! identity { ($x:expr) => { /* keep  exact */ $x }; }\n//go:embed data.txt\nvar payload string\n//line original.gox:50\nfunc f() {x:=identity!(  1 /* inside */ + 2  );_ = x // tail\n_ = `raw\n  string`\n}\n"
	source = strings.ReplaceAll(source, "\n", "\r\n")
	got, err := sourceformat.Source(t.Context(), "comments.gox", source, sourceformat.Options{InsertSpaces: true, TabSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"\ufeff//go:build !disabled\r\n\r\npackage p", "//goxide:check strict\r\ntype Number", "//go:embed data.txt\r\nvar payload", "//line original.gox:50\r\nfunc f", "{ /* keep  exact */ $x }", "identity!(  1 /* inside */ + 2  )", "`raw\r\n  string`", "Value int // field tail", "_ = x // tail"} {
		if !strings.Contains(got, text) {
			t.Fatalf("lost authored text %q:\n%s", text, got)
		}
	}
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatal("formatter introduced LF in a CRLF document")
	}
	if second, err := sourceformat.Source(t.Context(), "comments.gox", got, sourceformat.Options{InsertSpaces: true, TabSize: 2}); err != nil || second != got {
		t.Fatalf("comment/macro formatting is not idempotent: %v\n%s", err, second)
	}
}

func TestFormattingKeepsExpressionAndControlFlowSemantics(t *testing.T) {
	for _, source := range []string{
		"package p\nfunc f(){for i:=0;i<3;i++{if x:=i;x>0{println(x)}}}\n",
		"package p\nfunc f(){switch x:=1;x{case 1:println(x);default:println(0)}}\n",
		"package p\nfunc f(x int){a:=& &x;b:=x + +x;_ = a;_ = b}\n",
		"package p\nfunc f(){return\nvalue()}\n",
		"package p\nfunc f() {x:=[]int{1,2};m:=map[string]int{\"k\":1};_ = x[1:2];_ = m}\n",
		"package p\n#[cfg(feature(\"x\"))]\nfunc f(){println(1)}\n",
		"package p\nfunc f(){\n//line original.gox:50\nprintln(1)\n  //line not-active.gox:30\nprintln(2)\n}\n",
	} {
		got, err := sourceformat.Source(t.Context(), "forms.gox", source, sourceformat.Options{})
		if err != nil {
			t.Fatalf("%v\n%s", err, source)
		}
		if second, err := sourceformat.Source(t.Context(), "forms.gox", got, sourceformat.Options{}); err != nil || second != got {
			t.Fatalf("not idempotent: %v\n%s\n%s", err, got, second)
		}
	}
}

func TestFormattingExistingLanguageExamples(t *testing.T) {
	for _, name := range []string{"static_protocol/main.gox", "enum_borrows/main.gox", "qualified_packages/main.gox", "macro_packages/main.gox", "t0_joint/main.gox", "t0_joint/config/types.gox", "option_serde/defaults.gox", "reexports/impl/macros.gox"} {
		t.Run(name, func(t *testing.T) {
			text, err := os.ReadFile(filepath.Join("..", "..", "examples", name))
			if err != nil {
				t.Fatal(err)
			}
			formatted, err := sourceformat.Source(t.Context(), name, string(text), sourceformat.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if again, err := sourceformat.Source(t.Context(), name, formatted, sourceformat.Options{}); err != nil || again != formatted {
				t.Fatalf("second formatting changed source: %v", err)
			}
		})
	}
}

func TestFormattingPreservesLineDirectiveActivation(t *testing.T) {
	for _, prefix := range []string{"", "  ", "\n\t", "\n\n  ", "\ufeff  "} {
		text := prefix + "//line original.gox:50\npackage p\nfunc f(){\n//line inside.gox:20\nprintln(1)\n  //line inactive.gox:30\nprintln(2)\n}\n"
		formatted, err := sourceformat.Source(t.Context(), "line.gox", text, sourceformat.Options{})
		if err != nil || !strings.HasPrefix(formatted, prefix+"//line original.gox:50\n") || !strings.Contains(formatted, "\n//line inside.gox:20\n") || !strings.Contains(formatted, "\n  //line inactive.gox:30\n") {
			t.Fatalf("line directive activation changed: %v\n%q", err, formatted)
		}
	}
}

func TestFormattingRejectsIncompleteSourceAndCancellation(t *testing.T) {
	for _, source := range []string{"package p\nfunc broken(", "package p\nmacro_rules! m { () => { x }", "package p\nfunc f() { \"unterminated }"} {
		if text, err := sourceformat.Source(t.Context(), "broken.gox", source, sourceformat.Options{}); err == nil || text != "" || !strings.Contains(err.Error(), "formatting unavailable") {
			t.Fatalf("incomplete source returned an edit: %q, %v", text, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sourceformat.Source(ctx, "cancel.gox", "package p\n", sourceformat.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestFormattedTraitsMatchMacrosAndGenericsBuildAndRun(t *testing.T) {
	const source = `package main
use fmt;
trait Value {func value(self Self)int}
type Number struct{N int}
impl Value for Number {func value(self Self)int{return self.N}}
func read[T Value](v T)int{return v.value()}
enum Choice{Some(int),None}
macro_rules! number {($v:expr)=>{ /* expression */ $v };}
func main(){v:=Choice::Some(number!(  read(Number{N:42})  ));fmt::Println(match v{Choice::Some(n)=>n,Choice::None=>0})}
`
	formatted, err := sourceformat.Source(t.Context(), "main.gox", source, sourceformat.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	standard := ""
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			standard = fmt.Sprintf("[dependencies.standard]\ngo='github.com/goxide-lang/std'\nversion=%q\n", dependency.Version)
		}
	}
	if standard == "" {
		t.Fatal("compiler go.mod must pin std")
	}
	write("goxide.toml", "[go_mod]\nmodule='example.org/formatted'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n"+standard)
	write("main.gox", formatted)
	identity, err := procmacro.ReadSDKIdentity(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), ".", project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
	host, err := procmacro.NewWASIHostWithBudgets(t.Context(), procmacro.MaxInvocationTime*wasiBudgetMultiplier, procmacro.MaxCompilationTime*wasiBudgetMultiplier)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close() })
	first, err := compile.PrepareProject(root, compile.Options{Offline: true, RunProcMacro: host.Run})
	if err != nil {
		t.Fatalf("formatted generation: %v\n%s", err, formatted)
	}
	generated := filepath.Join(first.Root, "goxide_targets/bin/app/main.go")
	before, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	second, err := compile.PrepareProject(root, compile.Options{Offline: true, Locked: true, RunProcMacro: host.Run})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(second.Root, "goxide_targets/bin/app/main.go"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("same formatted input changed generated Go: %v", err)
	}
	command := exec.CommandContext(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", second.Entry)
	command.Dir, command.Env = second.Root, project.GoEnv(true)
	if output, err := command.CombinedOutput(); err != nil || string(output) != "42\n" {
		t.Fatalf("formatted program: %v\n%s\nGenerated:\n%s", err, output, after)
	}
}
