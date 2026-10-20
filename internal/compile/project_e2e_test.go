package compile_test

import (
	"bytes"
	"errors"
	goast "go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestP5ProjectBuildRunTestAndStableLock(t *testing.T) {
	cacheRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	moduleCache := filepath.Join(cacheRoot, "modules")
	t.Setenv("GOMODCACHE", moduleCache)
	t.Cleanup(func() {
		command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "clean", "-modcache")
		command.Env = append(project.GoEnv(true), "GOMODCACHE="+moduleCache)
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("clean isolated module cache: %v\n%s", err, output)
		}
	})
	root := p5Project(t)
	app := filepath.Join(root, "app")
	before := p5Snapshot(t, root)
	options := compile.Options{GoTool: filepath.Join(runtime.GOROOT(), "bin", "go")}
	first, err := compile.PrepareProject(app, options)
	if err != nil {
		t.Fatal(err)
	}
	p5Go(t, first.Root, "build", "./...")
	if got := p5Go(t, first.Root, "run", first.Entry); got != "initialized\n18 42 5 true\n{\"Value\":7} shared\n" {
		t.Fatalf("project run = %q", got)
	}
	p5Go(t, first.Root, "test", "-count=1", "-race", "./...", "example.org/p5/kit/...")
	p5Go(t, first.Root, "vet", "./...", "example.org/p5/kit/...")
	p5CheckImports(t, first.Root)

	generated := p5Snapshot(t, filepath.Dir(first.Root))
	lockPath := filepath.Join(app, "goxide.lock")
	lock := p5Read(t, lockPath)
	lockInfo, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(lock, []byte("github.com/goxide-lang/serde")) || bytes.Contains(lock, []byte(first.Root)) {
		t.Fatalf("lock must retain the selected serde dependency without generated paths: %s", lock)
	}
	options.Offline, options.Locked = true, true
	second, err := compile.PrepareProject(app, options)
	if err != nil {
		t.Fatal(err)
	}
	if second.Root != first.Root || second.Entry != first.Entry {
		t.Fatalf("same project changed output location: %#v -> %#v", first, second)
	}
	if !reflect.DeepEqual(generated, p5Snapshot(t, filepath.Dir(second.Root))) {
		t.Fatal("repeated project generation changed bytes")
	}
	if !bytes.Equal(lock, p5Read(t, lockPath)) {
		t.Fatal("locked project changed lock bytes")
	}
	afterInfo, err := os.Stat(lockPath)
	if err != nil || !afterInfo.ModTime().Equal(lockInfo.ModTime()) {
		t.Fatalf("unchanged lock was rewritten: %v", err)
	}
	if got := p5Go(t, second.Root, "run", second.Entry); got != "initialized\n18 42 5 true\n{\"Value\":7} shared\n" {
		t.Fatalf("offline locked run = %q", got)
	}
	for name, want := range before {
		if got := p5Read(t, filepath.Join(root, filepath.FromSlash(name))); !bytes.Equal(got, want) {
			t.Fatalf("project preparation modified author file %s", name)
		}
	}
	for _, module := range []string{"app", "kit"} {
		for _, name := range []string{"go.mod", "go.sum"} {
			if _, err := os.Stat(filepath.Join(root, module, name)); !os.IsNotExist(err) {
				t.Fatalf("author project gained %s/%s: %v", module, name, err)
			}
		}
	}

	manifest := filepath.Join(app, "goxide.toml")
	changed := bytes.Replace(p5Read(t, manifest), []byte(`fmt = { go = "fmt" }`), []byte(`fmt = { go = "bytes" }`), 1)
	if err := os.WriteFile(manifest, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := compile.PrepareProject(app, options); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("locked manifest mapping change was not rejected: %v", err)
	}
	if !bytes.Equal(lock, p5Read(t, lockPath)) || !reflect.DeepEqual(generated, p5Snapshot(t, filepath.Dir(first.Root))) {
		t.Fatal("failed locked preparation modified prior lock or generated module")
	}
}

func TestP5ImportedReadOnlyReturnKeepsPermission(t *testing.T) {
	root := p5Project(t)
	app := filepath.Join(root, "app")
	filename := filepath.Join(app, "service", "bad.gox")
	source := "package service\nuse kit;\nfunc bad() { value := kit::ReadOnly(); *value = 99 }\n"
	if err := os.WriteFile(filename, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := compile.PrepareProject(app, compile.Options{})
	var diagnostic check.Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != check.ReferencePermission {
		t.Fatalf("cross-module reexport lost read-only permission: %v", err)
	}
	start := uint32(strings.Index(source, "*value ="))
	if diagnostic.File != "example.org/p5/app/service/bad.gox" || diagnostic.Span.Start < start || diagnostic.Span.End > start+uint32(len("*value")) || diagnostic.Span.Start >= diagnostic.Span.End {
		t.Fatalf("permission diagnostic does not identify the consumer write: %#v", diagnostic)
	}
	if _, err := os.Stat(filepath.Join(app, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("failed source check published a lock: %v", err)
	}
}

func p5Project(t *testing.T) string {
	t.Helper()
	versions := map[string]string{}
	for _, line := range strings.Split(string(p5Read(t, filepath.Join("..", "..", "go.mod"))), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "github.com/goxide-lang/std":
				versions["@STD_VERSION@"] = fields[1]
			case "github.com/goxide-lang/serde":
				versions["@SERDE_VERSION@"] = fields[1]
			}
		}
	}
	if len(versions) != 2 {
		t.Fatal("compiler go.mod must pin both std and serde")
	}
	root := t.TempDir()
	for name, contents := range p5Snapshot(t, filepath.Join("testdata", "p5")) {
		for marker, version := range versions {
			contents = bytes.ReplaceAll(contents, []byte(marker), []byte(version))
		}
		destination := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func p5Go(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	command.Dir = root
	command.Env = append(project.GoEnv(true), "GOFLAGS=-mod=readonly")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func p5Snapshot(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = p5Read(t, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func p5Read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func p5CheckImports(t *testing.T, root string) {
	t.Helper()
	for name, contents := range p5Snapshot(t, filepath.Dir(root)) {
		if filepath.Ext(name) != ".go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, contents, 0)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if seen[path] {
				t.Fatalf("%s imports %s twice", name, path)
			}
			seen[path] = true
		}
		if file.Name.Name == "kit" && filepath.Base(name) == "api.go" {
			for _, declaration := range file.Decls {
				if _, ok := declaration.(*goast.FuncDecl); ok {
					t.Fatalf("public reexport introduced a function wrapper in %s", name)
				}
				if declaration, ok := declaration.(*goast.GenDecl); ok && declaration.Tok == token.VAR {
					t.Fatalf("public reexport copied variable storage in %s", name)
				}
			}
		}
	}
}
