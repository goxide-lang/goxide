package lower_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestMacroNamespaceImportedAliasDoesNotShadowExternalDependencyRoot(t *testing.T) {
	previous := map[string][]byte{}
	for attempt := 0; attempt < 2; attempt++ {
		root := t.TempDir()
		app, library := filepath.Join(root, "app"), filepath.Join(root, "zlib")
		writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire zlib v0.0.0\nreplace zlib => ../zlib\n"))
		writeDiagnosticModuleFile(t, app, "main.gox", []byte(`package main
use macros::util;
use util::{Runtime as runtime,Value};
func main(){println(util!(),Value!(),runtime())}
`))
		writeDiagnosticModuleFile(t, app, "macros/lib.gox", []byte("package macros\npub macro_rules! util{()=>{5};}\n"))
		writeDiagnosticModuleFile(t, library, "go.mod", []byte("module zlib\n\ngo 1.27\n"))
		writeDiagnosticModuleFile(t, library, "lib.gox", []byte("package zlib\npub func Runtime()int{return 3}\npub macro_rules! Value{()=>{42};}\n"))
		if err := lower.CompileModules([]lower.Module{
			{Path: "app", Dir: app, Config: testConfig(t), Roots: paths.Roots{
				"macros": {PackagePath: "app/macros", ModulePath: "app"},
				"util":   {PackagePath: "zlib", ModulePath: "zlib"},
			}},
			{Path: "zlib", Dir: library, RootPackage: "zlib", Config: testConfig(t)},
		}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"app/main.go", "app/macros/lib.go", "zlib/lib.go"} {
			generated, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			if attempt != 0 && !bytes.Equal(previous[name], generated) {
				t.Fatalf("cross-module fallback changed generated %s", name)
			}
			previous[name] = generated
		}
		file, err := parser.ParseFile(token.NewFileSet(), "main.go", previous["app/main.go"], parser.ImportsOnly)
		if err != nil || len(file.Imports) != 1 || file.Imports[0].Path.Value != `"zlib"` {
			t.Fatalf("unexpected runtime imports: %v\n%s", err, previous["app/main.go"])
		}
		if attempt == 0 {
			runGo(t, app, "build", "./...")
			if got := runGo(t, app, "run", "."); got != "5 42 3\n" {
				t.Fatalf("output=%q, want 5 42 3", got)
			}
		}
	}
}
