package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
)

func TestCompileModulesMacroReexportPackageOrder(t *testing.T) {
	// Resolver package order must not change aliases through a private child.
	for _, provider := range []string{"alib", "zlib"} {
		for _, export := range []struct{ name, source string }{{"named", "pub use hidden::add_two;"}, {"glob", "pub use hidden::*;"}} {
			t.Run(provider+"/"+export.name, func(t *testing.T) {
				var previous []byte
				for attempt := 0; attempt < 2; attempt++ {
					root := t.TempDir()
					app, library := filepath.Join(root, "app"), filepath.Join(root, "library")
					writeDiagnosticModuleFile(t, app, "go.mod", []byte("module app\n\ngo 1.27\nrequire "+provider+" v0.0.0\nreplace "+provider+" => ../library\n"))
					writeDiagnosticModuleFile(t, app, "main.gox", []byte("package main\nuse lib::add_two as plus;use lib::add_two as unused;func main(){println(plus!(40))}\n"))
					writeDiagnosticModuleFile(t, library, "go.mod", []byte("module "+provider+"\n\ngo 1.27\n"))
					writeDiagnosticModuleFile(t, library, "lib.gox", []byte("package library\nmod hidden;"+export.source+"\n"))
					writeDiagnosticModuleFile(t, library, "hidden/macros.gox", []byte("package hidden\npub macro_rules! add_two{($value:expr)=>{$value+2};}\n"))
					if err := lower.CompileModules([]analysis.Module{
						{Path: "app", Dir: app, Roots: paths.Roots{"lib": {PackagePath: provider, ModulePath: provider}}, Config: testConfig(t)},
						{Path: provider, Dir: library, RootPackage: provider, Config: testConfig(t)},
					}); err != nil {
						t.Fatal(err)
					}
					generated, err := os.ReadFile(filepath.Join(app, "main.go"))
					if err != nil {
						t.Fatal(err)
					}
					if attempt != 0 && !bytes.Equal(previous, generated) {
						t.Fatal("same macro reexport produced different Go")
					}
					previous = generated
					if attempt == 0 {
						runGo(t, app, "build", "./...")
						if got := runGo(t, app, "run", "."); got != "42\n" {
							t.Fatalf("output=%q, want 42", got)
						}
					}
				}
			})
		}
	}
}
