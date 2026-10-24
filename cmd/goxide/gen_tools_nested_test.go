package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestGenerationToolsRespectModuleOwners(t *testing.T) {
	for _, nested := range []bool{false, true} {
		name := "same module subpackage"
		if nested {
			name = "independent nested module"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			app := filepath.Join(root, "app")
			mod := "module example.org/app\ngo 1.27\nrequire example.org/macros v0.0.0\nreplace example.org/macros => ../macros\n"
			runtimeDirectory := filepath.Join(root, "macros", "runtime")
			if nested {
				mod += "require example.org/macros/runtime v0.0.0\nreplace example.org/macros/runtime => ../runtime\n"
				runtimeDirectory = filepath.Join(root, "runtime")
				singleFileWrite(t, runtimeDirectory, "go.mod", "module example.org/macros/runtime\ngo 1.27\n")
			}
			singleFileWrite(t, app, "go.mod", mod)
			singleFileWrite(t, root, "macros/go.mod", "module example.org/macros\ngo 1.27\n")
			singleFileWrite(t, root, "macros/api.go", "package macros\nfunc init(){panic(\"generation-only package executed\")}\n")
			singleFileWrite(t, runtimeDirectory, "api.go", "package runtime\nfunc Value()int{return 42}\n")
			filename := filepath.Join(app, "main.gox")
			generated := map[string][]byte{filename: []byte("package main\nimport \"example.org/macros/runtime\"\nfunc main(){println(runtime.Value())}\n")}
			singleFileWrite(t, app, "main_gox.go", string(generated[filename]))
			owner := &singleFileModule{root: app, input: filename, usedSourceRoots: map[string]paths.Namespace{
				"example.org/macros": {PackagePath: "example.org/macros", ModulePath: "example.org/macros"},
			}}
			cfg := testdeps.Configuration()
			importer := typecheck.NewImporter(filepath.Join(app, "main_gox.go"), typecheck.ImporterOptions{Config: cfg})
			output, source, err := generationToolsSource(owner, nil, generated, importer)
			if err != nil {
				t.Fatal(err)
			}
			if nested {
				if output == "" || !bytes.Contains(source, []byte(`_ "example.org/macros"`)) {
					t.Fatalf("nested module import did not retain its independently selected macro parent: %s", source)
				}
				if err := os.WriteFile(output, source, 0600); err != nil {
					t.Fatal(err)
				}
			} else if output != "" || len(source) != 0 {
				t.Fatalf("ordinary same-module import produced a redundant generation anchor: %s", source)
			}
			goCommand := func(arguments ...string) []byte {
				t.Helper()
				command := exec.Command(cfg.Environment.GoTool, arguments...)
				command.Dir, command.Env = app, cfg.Environment.ExecEnv()
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("go %v: %v\n%s", arguments, err, output)
				}
				return output
			}
			goCommand("mod", "tidy")
			updated, err := os.ReadFile(filepath.Join(app, "go.mod"))
			if err != nil || !bytes.Contains(updated, []byte("example.org/macros v0.0.0")) {
				t.Fatalf("tidy dropped the selected macro module: %v\n%s", err, updated)
			}
			if got := string(goCommand("run", "-mod=readonly", ".")); got != "42\n" {
				t.Fatalf("independent Go output = %q, want 42", got)
			}
		})
	}
}
