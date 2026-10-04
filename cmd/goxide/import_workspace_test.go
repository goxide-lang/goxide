package main

import (
	"encoding/json"
	"github.com/hxsf/goxide/internal/project"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func nativeFixtureCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	c.Dir = root
	c.Env = append(os.Environ(), "GOWORK="+filepath.Join(root, "go.work"), "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("Go %v: %s %v", args, b, e)
	}
	return string(b)
}
func nativeWorkspaceFixture(t *testing.T) string {
	root := t.TempDir()
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n ./app\n ./lib\n)\ngodebug panicnil=1\n")
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.26\ngodebug panicnil=0\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/lib\";\"runtime/debug\")\nfunc main(){fmt.Println(lib.Value()); info,_:=debug.ReadBuildInfo(); for _,s:=range info.Settings{if s.Key==\"DefaultGODEBUG\"{fmt.Println(s.Value)}}}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nfunc Value()string{return \"work\"}\n")
	return root
}
func TestNativeWorkspaceImportExplicitGraph(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	want := nativeFixtureCommand(t, root, "run", "-buildvcs=false", "./app")
	before := sourceHash(t, root)
	plan, e := importCall(t, root, "--workspace", "--plan", "--offline")
	if e != nil {
		t.Fatalf("%s %v", plan, e)
	}
	if !strings.Contains(plan, "example.org/app -> example.org/lib") || !strings.Contains(plan, "workspace = true") {
		t.Fatal(plan)
	}
	if sourceHash(t, root) != before {
		t.Fatal("plan changed original")
	}
	dest := filepath.Join(t.TempDir(), "new")
	out, e := importCall(t, root, "--workspace", "--out", dest, "--offline")
	if e != nil {
		t.Fatalf("%s %v", out, e)
	}
	if sourceHash(t, root) != before {
		t.Fatal("migration changed original")
	}
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || out != want {
		t.Fatalf("%s %v", out, e)
	}
	out, e = projectRun(t, dest, "test", "--workspace", "--offline", "--locked")
	if e != nil {
		t.Fatalf("%s %v", out, e)
	}
	if _, e = projectRun(t, dest, "run", "-p", "example.org/app", "--tags", "new", "--offline"); e == nil {
		t.Fatal("configuration scope not enforced")
	}
}
func TestNativeWorkspaceVendorColdOffline(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "dep/go.mod", "module example.org/dep\ngo 1.27\n")
	workspaceWrite(t, root, "dep/dep.go", "package dep\nfunc Value()string{return \"upstream\"}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../dep\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/dep\"\nfunc Value()string{return dep.Value()}\n")
	nativeFixtureCommand(t, root, "work", "vendor")
	workspaceWrite(t, root, "vendor/example.org/dep/dep.go", "package dep\nfunc Value()string{return \"modified\"}\n")
	workspaceWrite(t, root, "vendor/example.org/dep/LICENSE", "fixture license\n")
	if e := os.Chmod(filepath.Join(root, "vendor/example.org/dep/LICENSE"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.RemoveAll(filepath.Join(root, "dep")); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOMODCACHE", t.TempDir())
	before := sourceHash(t, root)
	dest := filepath.Join(t.TempDir(), "new")
	out, e := importCall(t, root, "--workspace", "--out", dest, "--offline")
	if e != nil {
		t.Fatalf("%s %v", out, e)
	}
	if sourceHash(t, root) != before {
		t.Fatal("vendor import changed original")
	}
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || !strings.Contains(out, "modified") {
		t.Fatalf("%s %v", out, e)
	}
	local := "local-modules/" + project.Digest("example.org/dep")[:24]
	license := filepath.Join(dest, local, "LICENSE")
	b, e := os.ReadFile(license)
	if e != nil || string(b) != "fixture license\n" {
		t.Fatal("license", e)
	}
	info, e := os.Stat(license)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("license mode", e)
	}
	workspaceWrite(t, dest, local+"/dep.go", "package dep\nfunc Value()string{return \"changed\"}\n")
	if out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked"); e != nil || !strings.Contains(out, "changed") {
		t.Fatalf("local edit: %s %v", out, e)
	}
	moved := dest + "-moved"
	if e = os.Rename(dest, moved); e != nil {
		t.Fatal(e)
	}
	os.RemoveAll(root)
	os.RemoveAll(filepath.Join(moved, "target"))
	if out, e = projectRun(t, moved, "run", "-p", "example.org/app", "--offline", "--locked"); e != nil || !strings.Contains(out, "changed") {
		t.Fatalf("moved: %s %v", out, e)
	}

}

func TestNativeWorkspaceReplaceScopeAndGraphGuard(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	for _, name := range []string{"one", "two", "chosen"} {
		workspaceWrite(t, root, name+"/go.mod", "module example.org/dep\ngo 1.27\n")
		workspaceWrite(t, root, name+"/dep.go", "package dep\nfunc Value()string{return \""+name+"\"}\n")
	}
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n./app\n./lib\n./other\n)\nreplace example.org/dep => ./chosen\n")
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../one\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../two\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/dep\"\nfunc Value()string{return dep.Value()}\n")
	workspaceWrite(t, root, "other/go.mod", "module example.org/other\ngo 1.27\n")
	workspaceWrite(t, root, "other/other.go", "package other\nfunc Value()string{return \"outside\"}\n")
	dest := filepath.Join(t.TempDir(), "new")
	out, e := importCall(t, root, "--workspace", "--out", dest, "--offline")
	if e != nil {
		t.Fatalf("%s %v", out, e)
	}
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || !strings.Contains(out, "chosen") {
		t.Fatalf("%s %v", out, e)
	}
	// Neither all-member metadata nor --workspace selection authorizes a new edge.
	workspaceWrite(t, dest, "members/m0/main.go", "package main\nimport(\"fmt\";\"example.org/other\")\nfunc main(){fmt.Println(other.Value())}\n")
	for _, args := range [][]string{{"build", "-p", "example.org/app", "--offline", "--locked"}, {"build", "--workspace", "--offline", "--locked"}} {
		if _, e = projectRun(t, dest, args...); e == nil {
			t.Fatal("undeclared workspace import accepted")
		}
	}
}

func TestNativeWorkspaceImportVersionAndRejections(t *testing.T) {
	proxy, _ := versionProxy(t)
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		publishVersion(t, proxy, "example.org/d", v, map[string]string{"go.mod": "module example.org/d\ngo 1.27\n", "d.go": "package d\nfunc Value()string{return \"" + v + "\"}\n"})
	}
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\nrequire example.org/d v1.0.0\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/d v1.1.0\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/d\"\nfunc Value()string{return d.Value()}\n")
	dest := filepath.Join(t.TempDir(), "new")
	out, e := importCall(t, root, "--workspace", "--out", dest)
	if e != nil {
		t.Fatalf("%s %v", out, e)
	}
	out, e = projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked")
	if e != nil || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("%s %v", out, e)
	}
	before := sourceHash(t, root)
	workspaceWrite(t, root, "app/extra.gox", "package main\n")
	rejected := filepath.Join(t.TempDir(), "rejected")
	if _, e = importCall(t, root, "--workspace", "--out", rejected, "--offline"); e == nil {
		t.Fatal("enhanced source accepted")
	}
	if _, e = os.Stat(rejected); !os.IsNotExist(e) {
		t.Fatal("failed destination published")
	}
	os.Remove(filepath.Join(root, "app", "extra.gox"))
	if sourceHash(t, root) != before {
		t.Fatal("rejection changed original")
	}
}

func TestNativeWorkspaceVendorMissingPackagePreservesSource(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "dep/go.mod", "module example.org/dep\ngo 1.27\n")
	workspaceWrite(t, root, "dep/dep.go", "package dep\nfunc Value()string{return \"dependency\"}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../dep\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/dep\"\nfunc Value()string{return dep.Value()}\n")
	nativeFixtureCommand(t, root, "work", "vendor")
	if err := os.RemoveAll(filepath.Join(root, "dep")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "vendor", "example.org", "dep", "dep.go")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", t.TempDir())
	before := sourceHash(t, root)
	dest := filepath.Join(t.TempDir(), "failed")
	out, err := importCall(t, root, "--workspace", "--out", dest, "--offline")
	if err == nil {
		t.Fatalf("missing vendor package accepted: %s", out)
	}
	if sourceHash(t, root) != before {
		t.Fatal("original changed on vendor failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed migration published destination")
	}
}

func TestSuppliedWorkspaceSelectedVersionsAndEmptyModule(t *testing.T) {
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "d/go.mod", "module example.org/d\ngo 1.27\n")
	workspaceWrite(t, root, "d/d.go", "package d\nconst Value=\"selected\"\n")
	workspaceWrite(t, root, "empty/go.mod", "module example.org/empty\ngo 1.27\n")
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n./app\n./lib\n)\nreplace example.org/d => ./d\nreplace example.org/empty => ./empty\nreplace example.org/unused => ./unavailable\n")
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\nrequire (\nexample.org/d v1.0.0\nexample.org/empty v1.0.0\n)\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\")\nfunc main(){fmt.Println(d.Value)}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/d v1.1.0\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/d\"\nfunc Value()string{return d.Value}\n")
	nativeFixtureCommand(t, root, "work", "vendor")
	os.RemoveAll(filepath.Join(root, "d"))
	os.RemoveAll(filepath.Join(root, "empty"))
	dest := filepath.Join(t.TempDir(), "imported")
	if b, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := projectRun(t, dest, "run", "-p", "example.org/app", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "selected" {
		t.Fatalf("%s %v", b, e)
	}
	data, e := os.ReadFile(filepath.Join(dest, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	var lock project.Lock
	if e = json.Unmarshal(data, &lock); e != nil {
		t.Fatal(e)
	}
	if lock.NativeWorkspace.Modules["example.org/d"].Version != "v1.1.0" {
		t.Fatal("lost Go selection", string(data))
	}
	empty := filepath.Join(dest, "local-modules", project.Digest("example.org/empty")[:24])
	entries, e := os.ReadDir(empty)
	if e != nil || len(entries) != 1 || entries[0].Name() != "go.mod" {
		t.Fatalf("fabricated empty package: %v %v", entries, e)
	}
}
