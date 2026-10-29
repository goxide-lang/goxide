package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestGenerationToolsRetainOnlyGenerationDependencies(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "app")
	singleFileWrite(t, app, "go.mod", "module example.org/app\ngo 1.27\nrequire example.org/macros v0.0.0\nreplace example.org/macros => ../macros\n")
	singleFileWrite(t, root, "macros/go.mod", "module example.org/macros\ngo 1.27\n")
	singleFileWrite(t, root, "macros/host.go", "package macros\nfunc init() { panic(\"macro host must not execute at runtime\") }\n")
	filename := filepath.Join(app, "main.gox")
	generated := map[string][]byte{filename: []byte("package main\nfunc main(){println(42)}\n")}
	singleFileWrite(t, app, "main_gox.go", string(generated[filename]))
	owner := &singleFileModule{root: app, input: filename, usedSourceRoots: map[string]paths.Namespace{"example.org/macros": {PackagePath: "example.org/macros"}}}
	name, source, err := generationToolsSource(owner, nil, generated)
	if err != nil || name == "" {
		t.Fatalf("dependency artifact: %v", err)
	}
	if err := os.WriteFile(name, source, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := testdeps.Configuration()
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
	mod, err := os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil || !bytes.Contains(mod, []byte("require example.org/macros v0.0.0")) {
		t.Fatalf("tidy dropped macro dependency: %v\n%s", err, mod)
	}
	if output := goCommand("run", "-mod=readonly", "."); strings.TrimSpace(string(output)) != "42" {
		t.Fatalf("generated program output: %s", output)
	}
	_, repeated, err := generationToolsSource(owner, map[string][]byte{name: source}, generated)
	if err != nil || !bytes.Equal(source, repeated) {
		t.Fatalf("dependency artifact is not deterministic: %v\n%s", err, repeated)
	}
	owner.usedSourceRoots = nil
	_, empty, err := generationToolsSource(owner, nil, generated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, empty, 0600); err != nil {
		t.Fatal(err)
	}
	goCommand("mod", "tidy")
	mod, err = os.ReadFile(filepath.Join(app, "go.mod"))
	if err != nil || bytes.Contains(mod, []byte("require example.org/macros")) {
		t.Fatalf("unused macro dependency survived tidy: %v\n%s", err, mod)
	}
}
