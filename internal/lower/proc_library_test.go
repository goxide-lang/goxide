package lower_test

import (
	"bytes"
	goast "go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

const procFixturePackage = "example.org/macros"

type procFixtureImporter struct {
	packages map[string]*types.Package
	native   types.Importer
}

func (i procFixtureImporter) Import(name string) (*types.Package, error) {
	if pkg := i.packages[name]; pkg != nil {
		return pkg, nil
	}
	return i.native.Import(name)
}

func (i procFixtureImporter) PackageOrigin(name string) (string, bool, error) {
	if strings.HasPrefix(name, "github.com/goxide-lang/syntax/") {
		return "github.com/goxide-lang/syntax", false, nil
	}
	if name == procFixturePackage || strings.HasPrefix(name, procFixturePackage+"/") {
		return procFixturePackage, false, nil
	}
	_, err := i.native.Import(name)
	return "", true, err
}

func (i procFixtureImporter) PackageIndex(source, module string) ([]string, error) {
	if source == "toolchain" {
		return []string{"strings"}, nil
	}
	var names []string
	for name := range i.packages {
		if strings.HasPrefix(name, module+"/") || name == module {
			names = append(names, name)
		}
	}
	return names, nil
}

func procFixtureSDK(t *testing.T) procFixtureImporter {
	t.Helper()
	result := procFixtureImporter{packages: map[string]*types.Package{}, native: importer.Default()}
	for name, source := range map[string]string{
		procmacro.SDKPackage:           "package proc_macro; type Context struct{}; type Output struct{}",
		procmacro.TokenPackage:         "package token; type TokenStream struct{}",
		procFixturePackage:             "package macros; func Helper() int { return 99 }; func Derive_impl() {}",
		procFixturePackage + "/hidden": "package hidden; func Helper() int { return 99 }",
	} {
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "types.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg, err := (&types.Config{Importer: result}).Check(name, set, []*goast.File{file}, nil)
		if err != nil {
			t.Fatal(err)
		}
		result.packages[name] = pkg
	}
	return result
}

func procFixtureRoots() paths.Roots {
	return paths.Roots{
		"sdkpm":    {PackagePath: procmacro.SDKPackage, ModulePath: "github.com/goxide-lang/syntax"},
		"sdktoken": {PackagePath: procmacro.TokenPackage, ModulePath: "github.com/goxide-lang/syntax"},
		"text":     {PackagePath: "strings", Source: "toolchain"},
	}
}

const procFixtureSource = `package macros
use sdkpm as pm;
use sdktoken as tokens;
#[proc_macro]
pub func identity(ctx pm::Context, input tokens::TokenStream) pm::Output { return pm::Output{} }
#[proc_macro_attribute]
pub func keep(ctx pm::Context, args tokens::TokenStream, item tokens::TokenStream) pm::Output { return pm::Output{} }
#[proc_macro_derive(Describe, attributes(describe))]
pub func derive_impl(ctx pm::Context, input tokens::TokenStream) pm::Output { return pm::Output{} }
pub func Helper() int { return 99 }
`

func compileProcFixture(t *testing.T, source string) (map[string][]byte, *analysis.ProcLibrary, procFixtureImporter) {
	t.Helper()
	imports := procFixtureSDK(t)
	output, library, err := lower.CompileProcLibrary(map[string][]byte{"macros.gox": []byte(source)}, procFixturePackage, imports, procFixtureRoots(), lower.PackageOptions{Config: testConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	return output, library, imports
}

func procFixtureHost(entries ...pm.Entry) func(pm.Request) ([]byte, error) {
	return func(request pm.Request) ([]byte, error) {
		output, err := pm.Dispatch(request, entries)
		if err != nil {
			return nil, err
		}
		var response bytes.Buffer
		if err := pm.WriteResponse(&response, request, output); err != nil {
			return nil, err
		}
		return response.Bytes(), nil
	}
}

func TestCompileProcLibraryKeepsFunctionsAndValidatesSDKSignatures(t *testing.T) {
	output, library, _ := compileProcFixture(t, procFixtureSource)
	wantNames := map[string]string{"identity": "Identity", "keep": "Keep", "Describe": "Derive_impl"}
	if len(output) != 1 || len(library.Entries) != 3 || !reflect.DeepEqual(library.GoNames, wantNames) {
		t.Fatalf("exports=%v entries=%v output=%v", library.GoNames, library.Entries, output)
	}
	for _, name := range []string{"Identity", "Keep", "Derive_impl", "Helper"} {
		if !bytes.Contains(output["macros.gox"], []byte("func "+name+"(")) {
			t.Fatalf("producer omitted function %s:\n%s", name, output["macros.gox"])
		}
	}
	for _, test := range []struct{ name, source, want string }{
		{"wrong context", strings.Replace(procFixtureSource, "ctx pm::Context", "ctx int", 1), "parameter 1 must have canonical SDK type"},
		{"wrong output", strings.Replace(procFixtureSource, "pm::Output { return pm::Output{} }", "int { return 1 }", 1), "result must have canonical SDK type"},
		{"self invocation", procFixtureSource + "func Invoke() int { return identity!(1) }", "cannot be invoked by its defining crate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, library, err := lower.CompileProcLibrary(map[string][]byte{"macros.gox": []byte(test.source)}, procFixturePackage, procFixtureSDK(t), procFixtureRoots(), lower.PackageOptions{Config: testConfig(t)})
			if err == nil || output != nil || library != nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "macros.gox:") {
				t.Fatalf("output=%v library=%v error=%v, want %s", output, library, err, test.want)
			}
		})
	}
}

func TestCompilePackageProceduralAliasesAndSourceOnlyExports(t *testing.T) {
	_, library, imports := compileProcFixture(t, procFixtureSource)
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output { return pm.Success(input) }})
	roots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
	for _, spelling := range []struct{ use, call string }{
		{"use macros as selected;", "selected::identity"},
		{"use macros::identity as selected;", "selected"},
		{"use macros::*;", "identity"},
	} {
		source := "package main\n" + spelling.use + "\nfunc main() { println(" + spelling.call + "!(42)) }\n"
		options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}
		output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte(source)}, "consumer", imports, roots, options)
		if err != nil {
			t.Fatal(err)
		}
		again, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte(source)}, "consumer", imports, roots, options)
		if err != nil || !reflect.DeepEqual(output, again) || len(output) != 1 || bytes.Contains(output["main.gox"], []byte(procFixturePackage)) {
			t.Fatalf("consumer output or repeat differs: %v\n%s", err, output["main.gox"])
		}
	}
	for _, body := range []string{
		"use macros; func main() { println(macros::Helper()) }",
		"use macros; func main() { macros::derive_impl() }",
		"use macros::Helper; func main() { println(Helper()) }",
		"use macros as _; func main() {}",
		"use macros; func main() { macros::identity() }",
	} {
		output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package main\n" + body)}, "consumer", imports, roots, lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}})
		if err == nil || output != nil {
			t.Fatalf("runtime proc export accepted: %s\n%s", body, output["main.gox"])
		}
	}
	output, err := lower.CompilePackage(map[string][]byte{"macros.gox": []byte(procFixtureSource)}, procFixturePackage, imports, procFixtureRoots(), lower.PackageOptions{Config: testConfig(t)})
	if err == nil || output != nil || !strings.Contains(err.Error(), "registration requires a proc_macro lib") {
		t.Fatalf("ordinary package registration accepted: %v", err)
	}
}

func TestCompilePackageProceduralDefinitionUsesAndMissingHost(t *testing.T) {
	producer := strings.Replace(procFixtureSource, "use sdkpm", "use text as format;\nuse sdkpm", 1)
	_, library, imports := compileProcFixture(t, producer)
	roots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
	source := []byte("package main\nuse macros;\nfunc main() { format := 7; _ = format; println(macros::identity!()) }\n")
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}
	output, err := lower.CompilePackage(map[string][]byte{"main.gox": source}, "consumer", imports, roots, options)
	if err == nil || output != nil || !strings.Contains(err.Error(), "host") {
		t.Fatalf("missing execution host accepted: %v", err)
	}
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
		return pm.FromParse(public.ParseText(`format::ToUpper("hello")`, ctx.DefSite()))
	}})
	output, err = lower.CompilePackage(map[string][]byte{"main.gox": source}, "consumer", imports, roots, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 1 || !bytes.Contains(output["main.gox"], []byte(`"strings"`)) || !bytes.Contains(output["main.gox"], []byte(`.ToUpper("hello")`)) || bytes.Contains(output["main.gox"], []byte("github.com/goxide-lang/syntax")) {
		t.Fatalf("definition imports lost or producer leaked:\n%s", output["main.gox"])
	}
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.go", output["main.gox"])
	if got := runGo(t, directory, "run", "."); got != "HELLO\n" {
		t.Fatalf("definition-site call = %q", got)
	}
}

func TestCompilePackagePreparedProcDoesNotAddSDKToConsumer(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	consumerImports := procFixtureImporter{packages: map[string]*types.Package{}, native: importer.Default()}
	roots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}
	source := map[string][]byte{"main.gox": []byte("package main\nuse macros;\nfunc main() { _ = macros::identity!(42) }\n")}
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output { return pm.Success(input) }})
	if output, err := lower.CompilePackage(source, "consumer", consumerImports, roots, options); err != nil || len(output) != 1 {
		t.Fatalf("unused producer SDK became a target dependency: %v", err)
	}
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
		return pm.FromParse(public.ParseText("pm::Output{}", ctx.DefSite()))
	}})
	if output, err := lower.CompilePackage(source, "consumer", consumerImports, roots, options); err == nil || output != nil {
		t.Fatalf("generated SDK runtime reference bypassed consumer dependency graph:\n%s", output["main.gox"])
	}
}

type procInternalImporter struct{ procFixtureImporter }

func (i procInternalImporter) PackageOrigin(name string) (string, bool, error) {
	if name == "example.org/internal/hidden" {
		return "example.org", false, nil
	}
	return i.procFixtureImporter.PackageOrigin(name)
}

func TestCompilePackageProcDisplaySpanCannotGrantInternalAccess(t *testing.T) {
	base := procFixtureSDK(t)
	pkg := types.NewPackage("example.org/internal/hidden", "hidden")
	signature := types.NewSignatureType(nil, nil, nil, types.NewTuple(), types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int])), false)
	pkg.Scope().Insert(types.NewFunc(token.NoPos, pkg, "Helper", signature))
	pkg.MarkComplete()
	base.packages[pkg.Path()] = pkg
	imports := procInternalImporter{base}
	roots := procFixtureRoots()
	roots["hidden"] = paths.Namespace{PackagePath: pkg.Path(), ModulePath: "example.org"}
	producer := strings.Replace(procFixtureSource, "use sdkpm", "use hidden as secret;\nuse sdkpm", 1) + "\nfunc read() int { return secret::Helper() }"
	_, library, err := lower.CompileProcLibrary(map[string][]byte{"macros.gox": []byte(producer)}, procFixturePackage, imports, roots, lower.PackageOptions{Config: testConfig(t)})
	if err != nil {
		t.Fatal(err)
	}
	consumerRoots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
	for _, generated := range []string{"use hidden as _;", "use hidden as selected;", "use hidden::Helper as chosen;", "func leaked() int { return secret::Helper() }"} {
		t.Run(generated, func(t *testing.T) {
			library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				stream, err := public.ParseText(generated, ctx.DefSite())
				if err != nil {
					t.Fatal(err)
				}
				tokens := stream.Tokens()
				for index := range tokens {
					tokens[index] = tokens[index].WithSpan(ctx.DefSpan())
				}
				return pm.Success(public.New(tokens...))
			}})
			output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte("package main\nuse macros;\nmacros::identity!()\nfunc main() {}")}, "elsewhere.org/consumer", imports, consumerRoots, lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}})
			if err == nil || output != nil || !strings.Contains(err.Error(), "internal package example.org/internal/hidden is not allowed from elsewhere.org/consumer") {
				t.Fatalf("display span changed internal access: %v\n%s", err, output["main.gox"])
			}
		})
	}
}

func TestCompileModulesProceduralReexport(t *testing.T) {
	_, library, _ := compileProcFixture(t, procFixtureSource)
	library.Run = procFixtureHost(pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output { return pm.Success(input) }})
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.gox", []byte("package main\nuse facade;\nfunc main() { println(facade::forward!(42)) }\n"))
	writeDiagnosticModuleFile(t, directory, "facade/macros.gox", []byte("package facade\nuse macros;\npub use macros::identity as forward;\n"))
	roots := paths.Roots{
		"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage},
		"facade": {PackagePath: "consumer/facade", ModulePath: "consumer"},
	}
	err := lower.CompileModules([]analysis.Module{{Path: "consumer", Dir: directory, Roots: roots, Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, directory, "run", "."); got != "42\n" {
		t.Fatalf("reexport execution = %q", got)
	}
}
