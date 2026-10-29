package lower_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestMacroNamespaceDeclarations(t *testing.T) {
	for _, declaration := range []struct{ name, source, body string }{
		{"type", "struct Shared{Value int}", "value:=Shared{Value:3};println(value.Value,Shared!())"},
		{"trait", "trait Shared{func read(self Self)int};struct Value{};impl Shared for Value{func read(self Self)int{return 3}}", "value:=Value{};println(value.read(),Shared!())"},
		{"function", "func Shared()int{return 3}", "println(Shared(),Shared!())"},
	} {
		for _, macroFirst := range []bool{false, true} {
			order := "ordinary-first"
			parts := []string{declaration.source, "macro_rules! Shared{()=>{4};}"}
			if macroFirst {
				order, parts[0], parts[1] = "macro-first", parts[1], parts[0]
			}
			t.Run(declaration.name+"/"+order, func(t *testing.T) {
				var imports []string
				if declaration.name == "trait" {
					imports = []string{"github.com/goxide-lang/std/typeinfo"}
				}
				runMacroNamespaceFixture(t, map[string]string{"main.gox": "package main\n" + strings.Join(parts, "\n") + "\nfunc main(){" + declaration.body + "}\n"}, "3 4\n", imports)
			})
		}
	}
}

func TestMacroNamespaceImports(t *testing.T) {
	for _, test := range []struct {
		name, imports, body, output, runtime string
	}{
		{"distinct-sources", "use namespacefixture::values::{Shared,Call};use namespacefixture::macros::{Shared,Call};", "value:=Shared{Value:3};println(value.Value,Shared!(),Call(),Call!())", "3 7 5 11\n", "values"},
		{"aliases", "use namespacefixture::values::Shared as Selected;use namespacefixture::macros::Shared as Selected;", "value:=Selected{Value:3};println(value.Value,Selected!())", "3 7\n", "values"},
		{"one-use-both-spaces", "use namespacefixture::dual::{Shared,Call};", "value:=Shared{Value:3};println(value.Value,Shared!(),Call(),Call!())", "3 7 5 11\n", "dual"},
		{"public-reexport", "use namespacefixture::bridge::{Selected,Call,Rule};struct Value{};impl Rule for Value{func read(self Self)int{return 13}}", "value:=Selected{Value:3};local:=Value{};println(value.Value,Selected!(),Call(),Call!(),local.read(),Rule!())", "3 7 5 11 13 17\n", "dual"},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := "pub struct Shared{pub Value int}\npub func Call()int{return 5}\n"
			macros := "pub macro_rules! Shared{()=>{7};}\npub macro_rules! Call{()=>{11};}\n"
			sources := map[string]string{"main.gox": "package main\n" + test.imports + "\nfunc main(){" + test.body + "}\n"}
			if test.runtime == "values" {
				sources["values/lib.gox"] = "package values\n" + values
				sources["macros/lib.gox"] = "package macros\n" + macros
			} else {
				sources["dual/lib.gox"] = "package dual\n" + values + macros +
					"pub trait Rule{func read(self Self)int}\npub macro_rules! Rule{()=>{17};}\n"
			}
			imports := []string{"namespacefixture/" + test.runtime}
			if test.name == "public-reexport" {
				sources["facade/lib.gox"] = "package facade\npub use namespacefixture::dual::{Shared as Selected,Call,Rule};\n"
				sources["bridge/lib.gox"] = "package bridge\npub use namespacefixture::facade::*;\n"
				imports = append([]string{"github.com/goxide-lang/std/typeinfo"}, imports...)
			}
			runMacroNamespaceFixture(t, sources, test.output, imports)
		})
	}
}

func TestMacroNamespaceGlobShadowing(t *testing.T) {
	for _, test := range []struct {
		name, source, output string
		imports              []string
	}{
		{"independent-globs", "use namespacefixture::values::*;use namespacefixture::macros::*;", "3 7\n", []string{"namespacefixture/values"}},
		{"ordinary-declaration", "use namespacefixture::dual::*;func Shared()int{return 3}", "3 11\n", nil},
		{"macro-declaration", "use namespacefixture::dual::*;macro_rules! Shared{()=>{7};}", "5 7\n", []string{"namespacefixture/dual"}},
		{"ordinary-import", "use namespacefixture::values::Shared;use namespacefixture::dual::*;", "3 11\n", []string{"namespacefixture/values"}},
		{"macro-import", "use namespacefixture::macros::Shared;use namespacefixture::dual::*;", "5 7\n", []string{"namespacefixture/dual"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runMacroNamespaceFixture(t, map[string]string{
				"values/lib.gox": "package values\npub func Shared()int{return 3}\n",
				"macros/lib.gox": "package macros\npub macro_rules! Shared{()=>{7};}\n",
				"dual/lib.gox":   "package dual\npub func Shared()int{return 5}\npub macro_rules! Shared{()=>{11};}\n",
				"main.gox":       "package main\n" + test.source + "\nfunc main(){println(Shared(),Shared!())}\n",
			}, test.output, test.imports)
		})
	}
}

func TestMacroNamespaceAmbiguityStaysWithinOneSpace(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"macro", "func Shared()int{return 3};func main(){println(Shared!())}"},
		{"ordinary", "macro_rules! Shared{()=>{3};};func main(){println(Shared())}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := map[string]string{
				"left/lib.gox":  "package left\npub func Shared()int{return 1}\npub macro_rules! Shared{()=>{10};}\n",
				"right/lib.gox": "package right\npub func Shared()int{return 2}\npub macro_rules! Shared{()=>{20};}\n",
				"main.gox":      "package main\nuse namespacefixture::left::*;use namespacefixture::right::*;\n" + test.source + "\n",
			}
			input := writeMacroNamespaceFixture(t, sources)
			generated, err := lower.PrepareModule([]string{input}, "namespacefixture", testConfig(t))
			if generated != "" {
				defer os.RemoveAll(generated)
			}
			if err == nil || !strings.Contains(err.Error(), "ambiguous") {
				t.Fatalf("same-space glob ambiguity was lost: %v", err)
			}
		})
	}
}

func TestMacroNamespaceDefinitionAndCallerHygiene(t *testing.T) {
	runMacroNamespaceFixture(t, map[string]string{
		"defs/lib.gox": `package defs
pub func Shared()int{return 10}
pub macro_rules! Shared{()=>{20};}
pub macro_rules! Definition{()=>{Shared()+Shared!()};}
pub macro_rules! Caller{()=>{$caller::Shared()+$caller::Shared!()};}
`,
		"callers/lib.gox": "package callers\npub macro_rules! Shared{()=>{200};}\n",
		"main.gox": `package main
use namespacefixture::defs::{Definition,Caller};
use namespacefixture::callers::Shared;
func Shared()int{return 100}
func main(){println(Definition!(),Caller!(),Shared(),Shared!())}
`,
	}, "30 300 100 200\n", []string{"namespacefixture/defs"})
}

func TestMacroNamespaceQualifiedPrefixUsesOrdinaryModule(t *testing.T) {
	runMacroNamespaceFixture(t, map[string]string{
		"util/lib.gox": "package util\npub mod leaf;\n",
		"util/leaf/lib.gox": `package leaf
pub func Runtime()int{return 3}
pub macro_rules! Value{()=>{42};}
`,
		"main.gox": `package main
use namespacefixture::util;
macro_rules! util{()=>{5};}
use util::leaf::Runtime as runtime;
func main(){println(util!(),util::leaf::Value!(),runtime())}
`,
	}, "5 42 3\n", []string{"namespacefixture/util/leaf"})
}

func TestMacroNamespaceProceduralFunctionAndAttribute(t *testing.T) {
	_, library, imports := compileProcFixture(t, procFixtureSource)
	functionCalls, attributeCalls := 0, 0
	library.Run = procFixtureHost(
		pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output {
			functionCalls++
			return pm.Success(input)
		}},
		pm.Entry{Name: "keep", Attribute: func(_ pm.Context, _, input public.TokenStream) pm.Output {
			attributeCalls++
			return pm.Success(input)
		}},
	)
	const source = `package main
use macros::{identity,keep};
func identity()int{return 5}
#[keep] struct keep{Value int}
func main(){value:=keep{Value:9};println(identity(),identity!(7),value.Value)}
`
	roots := paths.Roots{"macros": {PackagePath: procFixturePackage, ModulePath: procFixturePackage}}
	options := lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*analysis.ProcLibrary{library}}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		output, err := lower.CompilePackage(map[string][]byte{"main.gox": []byte(source)}, "consumer", imports, roots, options)
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, output["main.gox"]) {
			t.Fatal("same procedural macro input changed generated Go")
		}
		previous = output["main.gox"]
	}
	if functionCalls != 2 || attributeCalls != 2 {
		t.Fatalf("function calls=%d, attribute calls=%d, want two of each", functionCalls, attributeCalls)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", previous, parser.ImportsOnly)
	if err != nil || len(file.Imports) != 0 {
		t.Fatalf("procedural macro imports leaked into runtime: %v\n%s", err, previous)
	}
	directory := t.TempDir()
	writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, directory, "main.go", previous)
	runGo(t, directory, "build", "./...")
	if output := runGo(t, directory, "run", "."); output != "5 7 9\n" {
		t.Fatalf("output=%q, want 5 7 9", output)
	}
}

func TestMacroNamespaceImportedAliasDoesNotShadowDependencyRoot(t *testing.T) {
	sources := map[string]string{
		"macros/lib.gox": "package macros\npub macro_rules! util{()=>{5};}\n",
		"util/lib.gox":   "package util\npub mod leaf;\n",
		"util/leaf/lib.gox": `package leaf
pub func Runtime()int{return 3}
pub macro_rules! Value{()=>{42};}
`,
		"main.gox": `package main
use macros::util;
use util::leaf::{Runtime as runtime,Value};
func main(){println(util!(),Value!(),runtime())}
`,
	}
	roots := paths.Roots{
		"macros": {PackagePath: "namespacefixture/macros", ModulePath: "namespacefixture"},
		"util":   {PackagePath: "namespacefixture/util", ModulePath: "namespacefixture"},
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := writeMacroNamespaceFixture(t, sources)
		if err := lower.CompileModules([]analysis.Module{{Path: "namespacefixture", Dir: directory, Config: testConfig(t), Roots: roots}}); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(directory, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, output) {
			t.Fatal("macro alias changed dependency root output between generations")
		}
		previous = output
		file, err := parser.ParseFile(token.NewFileSet(), "main.go", output, parser.ImportsOnly)
		if err != nil || len(file.Imports) != 1 || file.Imports[0].Path.Value != `"namespacefixture/util/leaf"` {
			t.Fatalf("unexpected dependency root imports: %v\n%s", err, output)
		}
		if attempt == 0 {
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != "5 42 3\n" {
				t.Fatalf("output=%q, want 5 42 3", got)
			}
		}
	}
}

func writeMacroNamespaceFixture(t *testing.T, sources map[string]string) string {
	t.Helper()
	input := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(input, "go.mod"), []byte("module namespacefixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, source := range sources {
		writeDiagnosticModuleFile(t, input, name, []byte(source))
	}
	return input
}

func runMacroNamespaceFixture(t *testing.T, sources map[string]string, want string, wantImports []string) {
	t.Helper()
	input := writeMacroNamespaceFixture(t, sources)
	var module string
	previous := map[string][]byte{}
	for attempt := 0; attempt < 2; attempt++ {
		generated, err := lower.PrepareModule([]string{input}, "namespacefixture", testConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(generated) })
		module = generated
		for name := range sources {
			name = strings.TrimSuffix(name, ".gox") + ".go"
			data, err := os.ReadFile(filepath.Join(generated, name))
			if err != nil {
				t.Fatal(err)
			}
			if attempt != 0 && !bytes.Equal(previous[name], data) {
				t.Fatalf("same source changed generated %s", name)
			}
			previous[name] = data
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", previous["main.go"], parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var imports []string
	for _, spec := range file.Imports {
		name, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		imports = append(imports, name)
	}
	sort.Strings(imports)
	if !reflect.DeepEqual(imports, wantImports) {
		t.Fatalf("Go imports=%v, want only runtime dependencies %v:\n%s", imports, wantImports, previous["main.go"])
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("output=%q, want %q", got, want)
	}
}
