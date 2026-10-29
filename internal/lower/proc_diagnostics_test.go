package lower_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func procDiagnosticOutput(ctx pm.Context, input public.TokenStream, failure bool) pm.Output {
	output := pm.Success(input)
	output.Diagnostics = append(output.Diagnostics, pm.Diagnostic{Severity: pm.Warning, Message: "first warning", Span: ctx.CallSpan()})
	if failure {
		output.Diagnostics = append(output.Diagnostics, pm.Diagnostic{Severity: pm.Error, Message: "expansion failure", Span: ctx.CallSpan()})
	}
	output.Diagnostics = append(output.Diagnostics, pm.Diagnostic{Severity: pm.Note, Message: "last note", Span: ctx.DefSpan()})
	return output
}

func checkProcDiagnostics(t *testing.T, diagnostics []expand.ProcDiagnostic, filenames []string, failure bool, callFiles ...string) {
	t.Helper()
	want := []string{"warning:first warning", "note:last note"}
	if failure {
		want = []string{"warning:first warning", "error:expansion failure", "note:last note"}
	}
	var repeated, wantFiles []string
	for _, callFile := range callFiles {
		repeated = append(repeated, want...)
		wantFiles = append(wantFiles, callFile)
		if failure {
			wantFiles = append(wantFiles, callFile)
		}
		wantFiles = append(wantFiles, procFixturePackage+"/macros.gox")
	}
	var got []string
	for _, diagnostic := range diagnostics {
		got = append(got, fmt.Sprintf("%s:%s", diagnostic.Severity, diagnostic.Message))
		if diagnostic.Location.File == nil || diagnostic.Location.Span.End <= diagnostic.Location.Span.Start || diagnostic.Expn == 0 {
			t.Fatalf("diagnostic lost source location or expansion: %+v", diagnostic)
		}
		if diagnostic.Severity == pm.Note && diagnostic.Location.File.Package.Name != "macros" {
			t.Fatalf("definition-site diagnostic moved to consumer: %+v", diagnostic)
		}
	}
	if !reflect.DeepEqual(got, repeated) {
		t.Fatalf("diagnostics = %v, want %v", got, repeated)
	}
	if !reflect.DeepEqual(filenames, wantFiles) {
		t.Fatalf("diagnostic filenames = %v, want %v", filenames, wantFiles)
	}
}

func TestCompilePackageProceduralDiagnostics(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure=%t", failure), func(t *testing.T) {
			_, library, imports := compileProcFixture(t, procFixtureSource)
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, input public.TokenStream) pm.Output {
				return procDiagnosticOutput(ctx, input, failure)
			}})
			var diagnostics []expand.ProcDiagnostic
			var filenames []string
			options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}, OnProcDiagnostic: func(filename string, diagnostic expand.ProcDiagnostic) {
				filenames = append(filenames, filename)
				diagnostics = append(diagnostics, diagnostic)
			}}
			roots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package main\nuse macros;\nfunc main() { println(macros::identity!(42)) }\n")}, "consumer", imports, roots, options)
			if failure {
				if err == nil || output != nil || !strings.Contains(err.Error(), "expansion failure") {
					t.Fatalf("output=%v error=%v", output, err)
				}
			} else if err != nil || len(output) != 1 {
				t.Fatalf("output=%v error=%v", output, err)
			}
			checkProcDiagnostics(t, diagnostics, filenames, failure, "main.gox")
		})
	}
}

func TestCompileProcLibraryProceduralDiagnostics(t *testing.T) {
	_, upstream, imports := compileProcFixture(t, procFixtureSource)
	upstream.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, input public.TokenStream) pm.Output {
		return procDiagnosticOutput(ctx, input, false)
	}})
	roots := procFixtureRoots()
	roots["macros"] = paths.Namespace{PackagePath: procFixturePackage, ModulePath: procFixturePackage}
	source := procFixtureSource + "\nuse macros as upstream;\nfunc Extra() int { return upstream::identity!(42) }\n"
	var diagnostics []expand.ProcDiagnostic
	var filenames []string
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{upstream}, OnProcDiagnostic: func(filename string, diagnostic expand.ProcDiagnostic) {
		filenames = append(filenames, filename)
		diagnostics = append(diagnostics, diagnostic)
	}}
	output, library, err := lower.CompileProcLibrary(map[string][]byte{"producer.gox": []byte(source)}, "example.org/dependent", imports, roots, options)
	if err != nil || len(output) != 1 || library == nil {
		t.Fatalf("output=%v library=%v error=%v", output, library, err)
	}
	checkProcDiagnostics(t, diagnostics, filenames, false, "producer.gox")
}

func TestCompileModulesProceduralDiagnostics(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprintf("failure=%t", failure), func(t *testing.T) {
			_, library, _ := compileProcFixture(t, procFixtureSource)
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, input public.TokenStream) pm.Output {
				return procDiagnosticOutput(ctx, input, failure)
			}})
			var diagnostics []expand.ProcDiagnostic
			var filenames []string
			var modules []analysis.Module
			for _, name := range []string{"first", "second"} {
				directory := t.TempDir()
				writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module "+name+"\n\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nuse macros;\nfunc main() { println(macros::identity!(42)) }\n"))
				modules = append(modules, analysis.Module{Path: name, Dir: directory, Roots: paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}, Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}})
			}
			modules[0].OnProcDiagnostic = func(filename string, diagnostic expand.ProcDiagnostic) {
				filenames = append(filenames, filename)
				diagnostics = append(diagnostics, diagnostic)
			}
			modules[1].OnProcDiagnostic = func(string, expand.ProcDiagnostic) { t.Fatal("non-leading module callback invoked") }
			err := lower.CompileModules(modules)
			callFiles := []string{"first/main.gox", "second/main.gox"}
			if failure {
				callFiles = callFiles[:1]
				if err == nil || !strings.Contains(err.Error(), "expansion failure") {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			checkProcDiagnostics(t, diagnostics, filenames, failure, callFiles...)
		})
	}
}

func TestCompileModulesProceduralDiagnosticsSkipDiscoveryRetries(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	invocations := 0
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, input public.TokenStream) pm.Output {
		invocations++
		return procDiagnosticOutput(ctx, input, false)
	}})
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nuse macros;\nmacros::identity!(pub mod child;)\nfunc main() { println(crate::child::Value()) }\n"))
	writeDiagnosticModuleFile(t, directory, "child/value.gox", []byte("package child\nuse macros;\npub func Value() int { return macros::identity!(42) }\n"))
	var diagnostics []expand.ProcDiagnostic
	var filenames []string
	err := lower.CompileModules([]analysis.Module{{Path: "consumer", Dir: directory, Roots: paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}, Config: testConfig(t), Crates: []analysis.Crate{{Root: "consumer", SourceDir: "consumer"}}, ProcLibraries: []*analysis.ProcLibrary{library}, OnProcDiagnostic: func(filename string, diagnostic expand.ProcDiagnostic) {
		filenames = append(filenames, filename)
		diagnostics = append(diagnostics, diagnostic)
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if invocations != 3 {
		t.Fatalf("macro invocations = %d, want one discovery invocation and two final invocations", invocations)
	}
	checkProcDiagnostics(t, diagnostics, filenames, false, "consumer/child/value.gox", "consumer/main.gox")
}
