//go:build !js && !race

package playground

import (
	"context"
	"encoding/json"
	"fmt"
	"go/importer"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/typecheck"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// This exercises the fixed-build boundary: actual checked Goxide source is
// compiled with standard Go to WASI, its metadata survives JSON transport, and
// the in-memory consumer has only native runtime exports, not the macro SDK.
// Race instrumentation can exceed the unchanged five-second guest startup
// budget; the regular playground and snapshot tests separately run with -race.
func TestPackageCheckedProceduralSnapshot(t *testing.T) {
	directory := t.TempDir()
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	var baseEnvironment []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB":
			continue
		}
		baseEnvironment = append(baseEnvironment, entry)
	}
	baseEnvironment = append(baseEnvironment, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
	runGo := func(dir string, wasm bool, arguments ...string) string {
		t.Helper()
		command := exec.Command(goTool, arguments...)
		command.Dir, command.Env = dir, append([]string(nil), baseEnvironment...)
		if wasm {
			command.Env = append(command.Env, "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
		}
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		return string(output)
	}
	sdk := strings.TrimSpace(runGo(".", false, "list", "-m", "-f", "{{.Dir}}", "github.com/goxide-lang/syntax"))
	producer := filepath.Join(directory, "producer")
	write := func(name, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const producerPath = "example.org/playground_macros"
	write(filepath.Join(producer, "go.mod"), fmt.Sprintf("module %s\n\ngo 1.27\nrequire github.com/goxide-lang/syntax v0.0.0\nreplace github.com/goxide-lang/syntax => %q\n", producerPath, filepath.ToSlash(sdk)))
	environment, err := config.NewEnvironment(config.Target{OS: "wasip1", Arch: "wasm"}, baseEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	configuration := config.Config{Environment: environment, Features: config.Features{Enabled: map[string]bool{"selected": true}, Declared: map[string]bool{"selected": true}}}
	sourceName := filepath.Join(producer, "macros.gox")
	generated, checked, err := lower.CompileProcLibrary(map[string][]byte{sourceName: []byte(playgroundProcSource)}, producerPath,
		typecheck.NewImporter(sourceName, typecheck.ImporterOptions{Config: configuration}),
		paths.Roots{"syntax": {GoPrefix: "github.com/goxide-lang/syntax", ModulePath: "github.com/goxide-lang/syntax", Source: "module"}},
		lower.PackageOptions{Config: configuration})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(producer, "macros_gox.go"), string(generated[sourceName]))
	var launcher strings.Builder
	fmt.Fprintf(&launcher, `package main
import (
 "fmt"
 "os"
 macros %q
 pm "github.com/goxide-lang/syntax/proc_macro"
)
func main() {
 request, err := pm.ReadRequest(os.Stdin)
 if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
 output, err := pm.Dispatch(request, []pm.Entry{
`, producerPath)
	fields := map[pm.Kind]string{pm.Function: "Function", pm.Attribute: "Attribute", pm.Derive: "Derive"}
	for _, entry := range checked.Entries {
		fmt.Fprintf(&launcher, "{Name:%q,%s:macros.%s},\n", entry.Name, fields[entry.Kind], checked.GoNames[entry.Name])
	}
	fmt.Fprintln(&launcher, `})
 if err == nil { err = pm.WriteResponse(os.Stdout, request, output) }
 if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}`)
	write(filepath.Join(producer, "cmd/guest/main.go"), launcher.String())
	wasmFile := filepath.Join(directory, "macros.wasm")
	runGo(producer, true, "build", "-o", wasmFile, "./cmd/guest")
	wasm, err := os.ReadFile(wasmFile)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := lower.SnapshotProcLibrary(checked)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metadata), directory) {
		t.Fatal("fixed macro metadata contains a build machine path")
	}
	var restored lower.ProcLibrarySnapshot
	if err := json.Unmarshal(metadata, &restored); err != nil {
		t.Fatal(err)
	}
	host, err := procmacro.NewWASIHost(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	consumerImports := importer.Default()
	invocations := 0
	library, err := lower.LoadProcLibrarySnapshot(restored, consumerImports, func(request pm.Request) ([]byte, error) {
		invocations++
		return host.Run(wasm, request)
	})
	if err != nil {
		t.Fatal(err)
	}
	consumerEnvironment, err := config.NewEnvironment(config.Target{OS: "linux", Arch: "amd64"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics []expand.ProcDiagnostic
	var diagnosticFiles []string
	const facadePath = "example.org/playground_facade"
	options := Options{
		Config:   config.Config{Environment: consumerEnvironment, Features: config.Features{Enabled: map[string]bool{"consumer": true}}},
		Importer: consumerImports, ProcLibraries: []*lower.ProcLibrary{library},
		Roots:        paths.Roots{"chosen": {PackagePath: facadePath, ModulePath: facadePath}},
		RuntimeRoots: []lower.RuntimeRoot{{LogicalModule: PackagePath, BindingID: "fixed-consumer", Namespace: paths.Namespace{PackagePath: PackagePath, ModulePath: PackagePath}}},
		SourcePackages: []lower.SourcePackage{{
			PackagePath: facadePath, ModulePath: facadePath, Target: consumerEnvironment.Target,
			Sources: map[string][]byte{"lib.gox": []byte("package facade\npub use macros::*;\n")},
			Roots:   paths.Roots{"macros": {PackagePath: producerPath, ModulePath: producerPath}},
		}},
		OnProcDiagnostic: func(filename string, diagnostic expand.ProcDiagnostic) {
			diagnosticFiles = append(diagnosticFiles, filename)
			diagnostics = append(diagnostics, diagnostic)
		},
	}
	files := []File{{"main.gox", `package main
use chosen as macros;
#[macros::keep]
#[derive(macros::Answer)]
struct Record { #[answer] Value int }
pub func Base() int { return 40 }
func main() {
 var value = Record{Value: macros::identity!(Answer())}
 println(value.Value, macros::runtime!())
}
`}}
	output, err := CompilePackage(files, options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := CompilePackage(files, options)
	if err != nil || !reflect.DeepEqual(output, again) || invocations != 8 || len(output) != 1 || output[0].Path != "main.go" {
		t.Fatalf("fixed procedural compilation is unstable: calls=%d error=%v output=%v", invocations, err, output)
	}
	if !reflect.DeepEqual(diagnosticFiles, []string{"main.gox", producerPath + "/macros.gox", "main.gox", producerPath + "/macros.gox"}) || len(diagnostics) != 4 {
		t.Fatalf("procedural diagnostics lost order or original filenames: %v %v", diagnosticFiles, diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Location.File == nil || diagnostic.Location.Span.End <= diagnostic.Location.Span.Start || diagnostic.Expn == 0 {
			t.Fatalf("procedural diagnostic lost location: %+v", diagnostic)
		}
	}
	if strings.Contains(output[0].Source, producerPath) || strings.Contains(output[0].Source, "github.com/goxide-lang/syntax") {
		t.Fatalf("macro SDK or producer leaked into consumer:\n%s", output[0].Source)
	}
	consumer := filepath.Join(directory, "consumer")
	write(filepath.Join(consumer, "go.mod"), "module playground\n\ngo 1.27\n")
	write(filepath.Join(consumer, output[0].Path), output[0].Source)
	if stdout := runGo(consumer, false, "run", "."); stdout != "42 40\n" {
		t.Fatalf("generated consumer = %q", stdout)
	}
	// A failed macro returns its complete diagnostic sequence and no output.
	diagnostics, diagnosticFiles = nil, nil
	failure, err := CompilePackage([]File{{"bad.gox", "package p\nuse chosen;\nfunc F(){chosen::failure!()}\n"}}, options)
	if err == nil || failure != nil || !strings.Contains(err.Error(), "bad.gox:3:") || !strings.Contains(err.Error(), "expected failure") || len(diagnostics) != 2 || diagnostics[0].Severity != pm.Warning || diagnostics[1].Severity != pm.Error {
		t.Fatalf("macro failure lost diagnostics or emitted output: %v, %v, %v", failure, err, diagnostics)
	}
	// The fixed catalog grants no macro package runtime exports or undeclared roots.
	for _, source := range []string{
		"package p\nuse chosen;\nfunc F(){chosen::Identity()}\n",
		"package p\nuse missing;\nfunc F(){missing::identity!()}\n",
	} {
		if output, err := CompilePackage([]File{{"denied.gox", source}}, options); err == nil || output != nil {
			t.Fatalf("runtime macro value or missing root accepted: %v", err)
		}
	}
}

const playgroundProcSource = `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[cfg(feature("selected"))]
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 if ctx.Target().OS != "linux" || ctx.Target().Arch != "amd64" || !ctx.HasFeature("consumer") {
  return pm::Failure("consumer context was not preserved", ctx.CallSpan())
 }
 var output = pm::Success(input)
 output.Diagnostics = append(output.Diagnostics,
  pm::Diagnostic{Severity: pm::Warning, Message: "input retained", Span: ctx.CallSpan()},
  pm::Diagnostic{Severity: pm::Note, Message: "macro definition", Span: ctx.DefSpan()})
 return output
}
#[proc_macro_attribute]
pub func keep(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output {
 return pm::Success(item)
}
#[proc_macro_derive(Answer, attributes(answer))]
pub func answer(ctx pm::Context, input token::TokenStream) pm::Output {
 return pm::FromParse(token::ParseText("pub func Answer() int { return 42 }", ctx.DefSite()))
}
#[proc_macro]
pub func runtime(ctx pm::Context, input token::TokenStream) pm::Output {
 var root, err = ctx.RuntimeRoot("playground")
 if err != nil { return pm::Failure(err.Error(), ctx.CallSpan()) }
 var tail, parseErr = token::ParseText("::Base()", ctx.DefSite())
 if parseErr != nil { return pm::Failure(parseErr.Error(), ctx.CallSpan()) }
 return pm::Success(root.Append(tail.Tokens()...))
}
#[proc_macro]
pub func failure(ctx pm::Context, input token::TokenStream) pm::Output {
 var output = pm::Failure("expected failure", ctx.CallSpan())
 output.Diagnostics = append([]pm::Diagnostic{pm::Diagnostic{Severity: pm::Warning, Message: "before failure", Span: ctx.CallSpan()}}, output.Diagnostics...)
 return output
}
`
