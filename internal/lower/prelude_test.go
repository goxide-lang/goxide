package lower_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestAutomaticPreludeImportsOnDemand(t *testing.T) {
	const standard = "github.com/goxide-lang/std"
	for _, test := range []struct {
		name, source, output string
		imports              []string
	}{
		{name: "unused", source: "func main(){}"},
		{name: "macro-only", source: `func main(){if eleven!()!=11{panic("macro")};println("builtin")}`, output: "builtin\n"},
		{name: "canonical-values", source: `func main(){var n Number=7;if answer()!=41 || helpers::answer()!=41 || n!=7{panic("values")}}`, imports: []string{standard + "/values"}},
		{name: "explicit-use", source: `use custom::answer; use custom::eleven; func main(){if answer()!=7 || eleven!()!=13{panic("override")}}`, imports: []string{"app/custom"}},
		{name: "explicit-glob", source: `use custom::*;func main(){if answer()!=7 || eleven!()!=13{panic("glob override")}}`, imports: []string{"app/custom"}},
		{name: "package-declaration", source: `func answer()int{return 3};macro_rules! eleven{()=>{5};}; func main(){if answer()!=3 || eleven!()!=5{panic("local override")}}`},
		{name: "macro-definition-scope", source: `use client;func answer()int{return 3};macro_rules! eleven{()=>{5};};func main(){if client::from_prelude!()!=52{panic("definition scope")}}`, imports: []string{standard + "/values"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var previous []byte
			for attempt := 0; attempt < 2; attempt++ {
				root := t.TempDir()
				app, std := filepath.Join(root, "app"), filepath.Join(root, "std")
				writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire "+standard+" v0.0.0\nreplace "+standard+" => ../std\n"))
				writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\npub mod custom;\n"+test.source+"\n"))
				writeDiagnosticModuleFile(t, app, "custom/lib.gox", []byte("package custom\npub func answer()int{return 7}\npub macro_rules! eleven{()=>{13};}\n"))
				writeDiagnosticModuleFile(t, app, "client/lib.gox", []byte("package client\npub macro_rules! from_prelude{()=>{answer()+eleven!()};}\n"))
				writeDiagnosticModuleFile(t, std, "go.mod", []byte("module "+standard+"\n\ngo 1.27\n"))
				writeDiagnosticModuleFile(t, std, "lib.gox", []byte("package std\npub mod prelude;pub mod values;pub mod macros;\n"))
				writeDiagnosticModuleFile(t, std, "prelude/lib.gox", []byte("package prelude\npub use library::values::Number;pub use library::values::answer;pub use library::values as helpers;pub use library::macros::eleven;pub use library::macros::println;\n"))
				writeDiagnosticModuleFile(t, std, "values/lib.gox", []byte("package values\npub type Number int\npub func answer()int{return 41}\n"))
				writeDiagnosticModuleFile(t, std, "macros/lib.gox", []byte("package macros\npub macro_rules! eleven{()=>{11};}\npub macro_rules! println{()=>{17};}\n"))
				roots := paths.Roots{
					"standard": {PackagePath: standard, ModulePath: standard},
					"custom":   {PackagePath: "app/custom", ModulePath: "app"},
					"client":   {PackagePath: "app/client", ModulePath: "app"},
				}
				if err := lower.CompileModules([]analysis.Module{
					{Path: "app", Dir: app, Roots: roots, Config: testConfig(t)},
					{Path: standard, Dir: std, RootPackage: standard, Roots: paths.Roots{"library": {PackagePath: standard, ModulePath: standard}}, Config: testConfig(t)},
				}); err != nil {
					t.Fatal(err)
				}
				generated, err := os.ReadFile(filepath.Join(app, "main.go"))
				if err != nil {
					t.Fatal(err)
				}
				file, err := parser.ParseFile(token.NewFileSet(), "main.go", generated, parser.ImportsOnly)
				if err != nil {
					t.Fatal(err)
				}
				var imports []string
				for _, spec := range file.Imports {
					p, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						t.Fatal(err)
					}
					imports = append(imports, p)
				}
				if !reflect.DeepEqual(imports, test.imports) {
					t.Fatalf("imports=%v, want %v:\n%s", imports, test.imports, generated)
				}
				if attempt != 0 && !bytes.Equal(previous, generated) {
					t.Fatalf("same input generated different Go:\n%s\n%s", previous, generated)
				}
				previous = generated
				if attempt == 0 {
					runGo(t, app, "build", "./...")
					if got := runGo(t, app, "run", "."); got != test.output {
						t.Fatalf("output=%q, want %q", got, test.output)
					}
				}
			}
		})
	}
}
