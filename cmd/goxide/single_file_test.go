package main

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func singleFileWrite(t *testing.T, root, name, source string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
}

func singleFileSnapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()
	files := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		files[rel] = sha256.Sum256(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestSingleFileModuleGraphAndOverlay(t *testing.T) {
	base := t.TempDir()
	bundle := filepath.Join(base, "bundle")
	module := filepath.Join(bundle, "consumer")
	mod := "module example.org/consumer\n\ngo 1.27\n\nrequire example.org/dep v0.0.0\nreplace example.org/dep => ../dep\ngodebug panicnil=1\n"
	singleFileWrite(t, module, "go.mod", mod)
	fixtureRootsFile(t, module, `[roots.dep]
module="example.org/dep"
package="example.org/dep"
[roots.hidden]
module="example.org/consumer"
package="example.org/consumer/internal/hidden"
[roots.native]
module="example.org/consumer"
package="example.org/consumer/native"
`)
	singleFileWrite(t, bundle, "dep/go.mod", "module example.org/dep\ngo 1.27\n")
	singleFileWrite(t, bundle, "dep/value.go", "package dep\nfunc Value()int{return 3}\n")
	singleFileWrite(t, module, "internal/hidden/value.go", "package hidden\nfunc Value()int{return 5}\n")
	singleFileWrite(t, module, "native/value.go", "package native\nfunc Value()int{return 7}\n")
	singleFileWrite(t, module, "cmd/app/main.gox", `package main
use fmt;
use os;
use dep;
use hidden;
use native;
func main(){fmt::Printf("%d %q\n",dep::Value()+hidden::Value()+native::Value(),os::Args[1:])}
`)
	singleFileWrite(t, module, "cmd/app/poison.go", "package main\ninvalid sibling Go\n")
	singleFileWrite(t, module, "cmd/app/poison.gox", "invalid sibling Goxide\n")
	// Generation now reads sibling Go declarations. Its determinism check uses
	// a separate package; run/build below still prove isolated file selection.
	transpileSource, err := os.ReadFile(filepath.Join(module, "cmd/app/main.gox"))
	if err != nil {
		t.Fatal(err)
	}
	singleFileWrite(t, module, "cmd/transpile/main.gox", string(transpileSource))
	// A different cwd graph and ambient workspace must not supply dependencies.
	cwd := filepath.Join(base, "unrelated")
	singleFileWrite(t, cwd, "go.mod", "module example.org/unrelated\ngo 1.27\nrequire example.org/dep v0.0.0\nreplace example.org/dep => ./wrong\n")
	singleFileWrite(t, cwd, "wrong/go.mod", "module example.org/dep\ngo 1.27\n")
	singleFileWrite(t, cwd, "wrong/value.go", "package dep\nfunc Value()int{return 300}\n")
	t.Chdir(cwd)
	t.Setenv("GOWORK", filepath.Join(base, "nonexistent.work"))
	t.Setenv("GOFLAGS", "-tags=ambient_not_selected")
	before := singleFileSnapshot(t, bundle)
	input := filepath.Join(module, "cmd/app/main.gox")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	var generated []byte
	for i := 0; i < 2; i++ {
		var out, stderr bytes.Buffer
		if err := run([]string{"transpile", "-o", "-", "--runtime", filepath.Join(base, "absent-runtime"), filepath.Join(module, "cmd/transpile/main.gox")}, &out, &stderr); err != nil {
			t.Fatalf("transpile: %v: %s", err, &stderr)
		}
		if i == 0 {
			generated = append([]byte(nil), out.Bytes()...)
		} else if !bytes.Equal(generated, out.Bytes()) {
			t.Fatal("single-file output changed between identical invocations")
		}
	}
	checkRun := func(input string) {
		t.Helper()
		var out, stderr bytes.Buffer
		args := []string{"run", "--go", tool, "--runtime", filepath.Join(base, "absent-runtime"), input, "--", "--offline", "", "a b"}
		if err := run(args, &out, &stderr); err != nil || out.String() != "15 [\"--offline\" \"\" \"a b\"]\n" || stderr.Len() != 0 {
			t.Fatalf("run: %v stdout=%q stderr=%q", err, &out, &stderr)
		}
	}
	checkRun(input)
	binary := filepath.Join(base, "built")
	var out, stderr bytes.Buffer
	if err := run([]string{"build", "--go", tool, "-o", binary, input}, &out, &stderr); err != nil {
		t.Fatalf("build: %v: %s", err, &stderr)
	}
	if data, err := exec.Command(binary).CombinedOutput(); err != nil || string(data) != "15 []\n" {
		t.Fatalf("built binary: %v: %s", err, data)
	}
	if got := singleFileSnapshot(t, bundle); !reflect.DeepEqual(before, got) {
		t.Fatal("source or dependency tree changed")
	}
	// Moving the complete fixture must preserve relative replacement meaning.
	moved := filepath.Join(base, "moved")
	if err := os.Rename(bundle, moved); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "selected.gox")
	if err := os.Symlink(filepath.Join(moved, "consumer/cmd/app/main.gox"), link); err != nil {
		t.Fatal(err)
	}
	checkRun(link)
	if got := singleFileSnapshot(t, moved); !reflect.DeepEqual(before, got) {
		t.Fatal("moved source or dependency tree changed")
	}
}

func TestSingleFileModuleSiblingAndMissingDependency(t *testing.T) {
	base := t.TempDir()
	module := filepath.Join(base, "consumer")
	singleFileWrite(t, module, "go.mod", "module example.org/consumer\ngo 1.27\n")
	fixtureRootsFile(t, module, fixtureSerdeRoots)
	singleFileWrite(t, module, "main.gox", "package main\nuse fmt;\nfunc main(){fmt::Println(sibling())}\n")
	singleFileWrite(t, module, "sibling.go", "package main\nfunc sibling()int{return 19}\n")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	var out, stderr bytes.Buffer
	if err := run([]string{"run", "--go", tool, filepath.Join(module, "main.gox")}, &out, &stderr); err == nil || !strings.Contains(err.Error()+stderr.String(), "unresolved name") || !strings.Contains(err.Error()+stderr.String(), "main.gox:3:26:") {
		t.Fatalf("sibling supplied a single-file declaration or source location was lost: %v: %s", err, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if err := run([]string{"run", "--go", tool, module}, &out, &stderr); err != nil || out.String() != "19\n" {
		t.Fatalf("directory control: %v: %s %s", err, &out, &stderr)
	}
	// The compiler's own graph contains this dependency; a configured consumer
	// must not acquire it from --runtime or cwd.
	singleFileWrite(t, module, "missing.gox", "package main\nuse serde::json as _;\nfunc main(){}\n")
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	before := singleFileSnapshot(t, module)
	out.Reset()
	stderr.Reset()
	if err := run([]string{"run", "--go", tool, "--runtime", root, filepath.Join(module, "missing.gox")}, &out, &stderr); err == nil || !strings.Contains(err.Error()+stderr.String(), "github.com/goxide-lang/serde") {
		t.Fatalf("missing dependency was supplied by runtime: %v: %s", err, &stderr)
	}
	if got := singleFileSnapshot(t, module); !reflect.DeepEqual(before, got) {
		t.Fatal("failed compile changed source configuration")
	}
}

func TestSingleFileModuleNearestOwnerAndOutputs(t *testing.T) {
	base := t.TempDir()
	singleFileWrite(t, base, "outer/go.mod", "module example.org/outer\ngo 1.27\n")
	inner := filepath.Join(base, "outer/nested")
	singleFileWrite(t, inner, "go.mod", "module example.org/inner\ngo 1.27\n")
	fixtureRootsFile(t, inner, `[roots.leaf]
module="example.org/inner"
package="example.org/inner/internal/leaf"
`)
	singleFileWrite(t, inner, "internal/leaf/leaf.go", "package leaf\nfunc Value()int{return 23}\n")
	singleFileWrite(t, inner, "app/main.gox", "package main\nuse fmt;\nuse leaf;\nfunc main(){fmt::Println(leaf::Value())}\n")
	input := filepath.Join(inner, "app/main.gox")
	m, err := prepareSingleFileModule(input)
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	if m.packagePath != "example.org/inner/app" {
		t.Fatalf("wrong owner: %s", m.packagePath)
	}
	mod, err := os.ReadFile(m.modFile)
	if err != nil || string(mod) != "module example.org/inner\ngo 1.27\n" {
		t.Fatalf("configuration was rewritten: %q %v", mod, err)
	}
	var out bytes.Buffer
	if err := run([]string{"run", "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), input}, &out, &out); err != nil || out.String() != "23\n" {
		t.Fatalf("nested module internal access: %v: %s", err, &out)
	}
	out.Reset()
	if err := run([]string{"build", "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), "-o", filepath.Join(inner, "binary"), input}, &out, &out); err == nil || !strings.Contains(err.Error(), "outside the source module") {
		t.Fatalf("build wrote inside source module: %v: %s", err, &out)
	}
	if _, err := os.Stat(filepath.Join(inner, "binary")); !os.IsNotExist(err) {
		t.Fatal("rejected build wrote an output")
	}
	other := "amd64"
	if runtime.GOARCH == other {
		other = "arm64"
	}
	t.Setenv("GOARCH", other)
	if _, err := prepareSingleFileModule(input); err == nil || !strings.Contains(err.Error(), "requires host GOOS/GOARCH") {
		t.Fatalf("cross-target configuration accepted: %v", err)
	}
	t.Setenv("GOARCH", "")
	singleFileWrite(t, base, "go-env", "GOARCH="+other+"\n")
	t.Setenv("GOENV", filepath.Join(base, "go-env"))
	if _, err := prepareSingleFileModule(input); err == nil || !strings.Contains(err.Error(), "requires host GOOS/GOARCH") {
		t.Fatalf("saved cross-target GOENV configuration accepted: %v", err)
	}
}

func TestSingleFileModuleOutputPreflight(t *testing.T) {
	base := t.TempDir()
	module := filepath.Join(base, "consumer")
	singleFileWrite(t, module, "go.mod", "module example.org/consumer\ngo 1.27\n")
	singleFileWrite(t, module, "input.gox", "package main\nfunc main(){}\n")
	input := filepath.Join(module, "input.gox")
	config := filepath.Join(module, "go.mod")
	symlink, hardlink := filepath.Join(base, "linked-map"), filepath.Join(base, "linked-source")
	if err := os.Symlink(config, symlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(config, hardlink); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(base, "must-not-write-map")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	before := singleFileSnapshot(t, module)
	for _, args := range [][]string{
		{"build", "--go", tool, "--source-map", config, "-o", filepath.Join(base, "binary"), input},
		{"build", "--go", tool, "--source-map", partial, "-o", filepath.Join(module, "binary"), input},
		{"transpile", "--source-map", symlink, input},
		{"expand", "-o", hardlink, input},
		{"transpile", "-o", filepath.Join(module, "input.go"), input},
	} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("unsafe configured output accepted: %q", args)
		}
		if got := singleFileSnapshot(t, module); !reflect.DeepEqual(before, got) {
			t.Fatalf("rejected output changed source: %q", args)
		}
		if _, err := os.Lstat(partial); !os.IsNotExist(err) {
			t.Fatalf("rejected build wrote a partial map: %q", args)
		}
	}
	t.Chdir(module)
	var out bytes.Buffer
	if err := run([]string{"build", "--go", tool, input}, &out, &out); err == nil || !strings.Contains(err.Error(), "outside the source module") {
		t.Fatalf("default binary wrote into source: %v: %s", err, &out)
	}
	// A caller-selected temporary directory may itself be in the source tree.
	// Reject before making any compiler-owned files there.
	t.Setenv("TMPDIR", module)
	if _, err := prepareSingleFileModule(input); err == nil || !strings.Contains(err.Error(), "temporary directory must be outside") {
		t.Fatalf("temporary files inside source accepted: %v", err)
	}
	if got := singleFileSnapshot(t, module); !reflect.DeepEqual(before, got) {
		t.Fatal("output or temporary-directory rejection changed source")
	}
}
