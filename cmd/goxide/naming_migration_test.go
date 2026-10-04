package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNamingMigrationProcess(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	binary := filepath.Join(t.TempDir(), "goxide")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", binary, "./cmd/goxide")
	build.Dir = repo
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	run := func(env []string, args ...string) (string, error) {
		c := exec.Command(binary, args...)
		c.Env = append(os.Environ(), env...)
		out, err := c.CombinedOutput()
		return string(out), err
	}
	root := t.TempDir()
	write := func(n, s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, n), []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.org/naming\ngo 1.27\n")
	write("main.gox", "package main\nfunc main(){println(Value)}\n")
	write("value.go", "package main\nconst Value=42\n")
	outdir := filepath.Join(t.TempDir(), "generated")
	if out, err := run(nil, "transpile", "-o", outdir, root); err != nil {
		t.Fatalf("mixed Go/Goxide: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(outdir, "main.go")); err != nil {
		t.Fatal(err)
	}
	if out, err := run(nil, "run", root); err != nil || strings.TrimSpace(out) != "42" {
		t.Fatalf("run: %v %s", err, out)
	}
	for _, mode := range []string{"transpile", "expand", "build", "run"} {
		write("main.hgo", "package main\nfunc main(){}\n")
		if out, err := run(nil, mode, filepath.Join(root, "main.hgo")); err == nil || !strings.Contains(out, "rename to .gox") {
			t.Fatalf("legacy %s: %v %s", mode, err, out)
		}
	}
	conflict := filepath.Join(t.TempDir(), "conflict")
	if out, err := run(nil, "transpile", "-o", conflict, root); err == nil || !strings.Contains(out, "legacy .hgo") {
		t.Fatalf("conflict: %v %s", err, out)
	}
	if _, err := os.Stat(conflict); !os.IsNotExist(err) {
		t.Fatalf("partial output remains: %v", err)
	}
	os.Remove(filepath.Join(root, "main.hgo"))
	write("main.gox", "//hgo:cfg off\npackage main\nfunc main(){}\n")
	if out, err := run(nil, "expand", filepath.Join(root, "main.gox")); err == nil || !strings.Contains(out, "legacy hgo directive") {
		t.Fatalf("cfg: %v %s", err, out)
	}
	for _, env := range [][]string{{"HGO_TARGET_DIR="}, {"HGO_TRUST_DIR=old", "GOXIDE_TRUST_DIR=new"}, {"HGO_TARGET_DIR=old", "GOXIDE_TARGET_DIR=new"}} {
		if out, err := run(env, "--help"); err == nil || !strings.Contains(out, "unset the old variable") {
			t.Fatalf("env: %v %s", err, out)
		}
	}
	for _, args := range [][]string{{"--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		if out, err := run(nil, args...); err != nil || !strings.Contains(out, "goxide") || strings.Contains(out, "hgo") {
			t.Fatalf("help/completion: %v %s", err, out)
		}
	}
}
