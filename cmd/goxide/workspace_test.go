package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
)

func workspaceWrite(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func TestWorkspaceUnifiedVersionsSeparateFeatures(t *testing.T) {
	proxy, cache := versionProxy(t)
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		publishVersion(t, proxy, "example.org/d", v, map[string]string{"go.mod": "module example.org/d\n\ngo 1.27\n", "d.go": fmt.Sprintf("package d\nfunc Version()string{return %q}\n", v)})
	}
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", `[workspace]
format=1
members=["app","server","common"]
default-members=["app"]
[workspace.dependencies]
common={module="example.org/common",package="example.org/common",path="common",default_features=false}
`)
	for _, m := range []string{"app", "server"} {
		f := "json"
		v := "v1.0.0"
		if m == "server" {
			f = "yaml"
			v = "v1.1.0"
		}
		workspaceWrite(t, root, m+"/goxide.toml", fmt.Sprintf(`[package]
format=2
module="example.org/%s"
go="1.27"
[dependencies]
common={workspace=true,features=[%q]}
fmt={go = "fmt"}
d={go = "example.org/d", version = %q,package="example.org/d"}
[[go.require]]
module="example.org/d"
version=%q
[[bin]]
name="%s"
path="main.gox"
`, m, f, v, v, m))
		workspaceWrite(t, root, m+"/main.gox", "package main\nuse fmt;\nuse common;\nuse d;\nfunc main(){fmt::Println(d::Version(),common::Flags())}\n")
	}
	workspaceWrite(t, root, "common/goxide.toml", `[package]
format=2
module="example.org/common"
go="1.27"
[features]
json=[]
yaml=[]
[lib]
name="common"
path="common.gox"
`)
	workspaceWrite(t, root, "common/common.gox", "package common\nfunc Flags()string{if cfg!(feature(\"yaml\")){return \"yaml\"};if cfg!(feature(\"json\")){return \"json\"};return \"none\"}\n")
	for _, args := range [][]string{{"run", "--package", "example.org/app"}, {"run", "--package", "example.org/app", "--offline", "--locked"}, {"run", "--package", "example.org/server", "--offline", "--locked"}} {
		output, e := projectRun(t, root, args...)
		if e != nil {
			t.Fatalf("%v: %v\n%s", args, e, output)
		}
		want := "v1.1.0 json"
		if strings.Contains(strings.Join(args, " "), "server") {
			want = "v1.1.0 yaml"
		}
		if !strings.Contains(output, want) {
			t.Fatalf("want %s, got %s", want, output)
		}
	}
	// Only the pinned version is needed by a locked rebuild.
	for _, ext := range []string{"mod", "info", "zip", "ziphash"} {
		os.Remove(filepath.Join(cache, "cache", "download", "example.org", "d", "@v", "v1.0.0."+ext))
	}
	if output, e := projectRun(t, root, "run", "--package", "example.org/app", "--offline", "--locked"); e != nil {
		t.Fatalf("pinned-only cache: %s %v", output, e)
	}
	// Both selected roots contribute features, but one shared version result.
	output, e := projectRun(t, root, "build", "--workspace", "--offline", "--locked")
	if e != nil {
		t.Fatalf("batch: %s %v", output, e)
	}
	for _, binary := range strings.Fields(output) {
		b, e := exec.Command(binary).CombinedOutput()
		if e != nil || !strings.Contains(string(b), "v1.1.0 yaml") {
			t.Fatalf("batch binary %s: %s %v", binary, b, e)
		}
	}
	if output, e = projectRun(t, root, "run", "--package", "example.org/app", "--offline", "--locked"); e != nil || !strings.Contains(output, "v1.1.0 json") {
		t.Fatalf("batch feature leak: %s %v", output, e)
	}
	b, e := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	var lock project.Lock
	if e = json.Unmarshal(b, &lock); e != nil {
		t.Fatal(e)
	}
	if lock.Version != 3 || lock.Unified == nil {
		t.Fatal(string(b))
	}
	for _, dir := range []string{"app", "server", "common"} {
		if _, e = os.Stat(filepath.Join(root, dir, "goxide.lock")); !os.IsNotExist(e) {
			t.Fatalf("member lock exists: %s %v", dir, e)
		}
	}
}

func TestWorkspaceCopiedGitLockIgnoresUnreachable(t *testing.T) {
	root := t.TempDir()
	repos := t.TempDir()
	git := func(repo string, args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=f@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=f@example.invalid")
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	for _, name := range []string{"used", "unused"} {
		repo := filepath.Join(repos, name)
		workspaceWrite(t, repo, "go.mod", "module example.org/"+name+"\n\ngo 1.27\n")
		workspaceWrite(t, repo, "lib.go", "package "+name+"\nfunc Value()string{return \"one\"}\n")
		git(repo, "init", "-b", "main")
		git(repo, "add", ".")
		git(repo, "commit", "-m", "one")
	}
	workspaceWrite(t, root, "goxide.toml", `[workspace]
format=1
members=["app","server"]
default-members=["app"]
`)
	manifest := func(name, dep string) string {
		return fmt.Sprintf(`[package]
format=2
module="example.org/%s"
go="1.27"
[dependencies]
dep={module="example.org/%s",git = %q, branch = "main"}
[[bin]]
name="app"
path="main.go"
`, name, dep, filepath.Join(repos, dep))
	}
	for _, pair := range [][2]string{{"app", "used"}, {"server", "unused"}} {
		workspaceWrite(t, root, pair[0]+"/goxide.toml", manifest(pair[0], pair[1]))
		workspaceWrite(t, root, pair[0]+"/main.go", "package main\nimport(\"fmt\";d \"example.org/"+pair[1]+"\")\nfunc main(){fmt.Println(d.Value())}\n")
	}
	// Unselected scripts are neither prepared nor granted trust.
	file := filepath.Join(root, "server", "goxide.toml")
	b, _ := os.ReadFile(file)
	workspaceWrite(t, root, "server/goxide.toml", string(b)+"\n[build]\nentry=\"tools/build.go\"\noutputs=[\"gen/data.go\"]\n")
	workspaceWrite(t, root, "server/tools/build.go", "this is deliberately invalid Go; do not compile it")
	output, e := projectRun(t, root, "run", "--package", "example.org/app")
	if e != nil || !strings.Contains(output, "one") {
		t.Fatalf("%s %v", output, e)
	}
	locked, e := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	used := filepath.Join(repos, "used")
	workspaceWrite(t, used, "lib.go", "package used\nfunc Value()string{return \"two\"}\n")
	git(used, "add", ".")
	git(used, "commit", "-m", "two")
	if e = os.Rename(filepath.Join(repos, "unused"), filepath.Join(repos, "unused-gone")); e != nil {
		t.Fatal(e)
	}
	split := t.TempDir()
	workspaceWrite(t, split, "goxide.toml", manifest("app", "used"))
	workspaceWrite(t, split, "goxide.lock", string(locked))
	source, _ := os.ReadFile(filepath.Join(root, "app", "main.go"))
	workspaceWrite(t, split, "main.go", string(source))
	// A fresh clone must checkout the lock's old commit, never current main.
	output, e = projectRun(t, split, "run", "--locked")
	if e != nil || !strings.Contains(output, "one") {
		t.Fatalf("copied lock: %s %v", output, e)
	}
	output, e = projectRun(t, split, "run", "--locked", "--offline")
	if e != nil || !strings.Contains(output, "one") {
		t.Fatalf("offline copied lock: %s %v", output, e)
	}
	var l project.Lock
	if e = json.Unmarshal(locked, &l); e != nil {
		t.Fatal(e)
	}
	// Even an ordinary online build cannot repair a copied lock by moving main.
	originalGit := l.Git
	l.Git = map[string]string{}
	badPin, _ := json.Marshal(l)
	workspaceWrite(t, split, "goxide.lock", string(badPin))
	if _, e = projectRun(t, split, "run"); e == nil || !strings.Contains(e.Error(), "lock") {
		t.Fatalf("missing Git pin silently resolved: %v", e)
	}
	l.Git = originalGit
	// Policy refresh must not repair an existing pin by re-resolving main.
	rootApp, _ := os.ReadFile(filepath.Join(root, "app", "goxide.toml"))
	workspaceWrite(t, root, "app/goxide.toml", string(rootApp)+"\n[features]\nnew=[]\n")
	workspaceWrite(t, root, "goxide.lock", string(badPin))
	if _, e = projectRun(t, root, "run", "--package", "example.org/app"); e == nil || !strings.Contains(e.Error(), "git pin") {
		t.Fatalf("policy refresh moved missing pin: %v", e)
	}
	delete(l.Unified.Modules, "example.org/used")
	bad, _ := json.Marshal(l)
	workspaceWrite(t, split, "goxide.lock", string(bad))
	if _, e = projectRun(t, split, "run", "--locked", "--offline"); e == nil || !strings.Contains(e.Error(), "missing reachable module") {
		t.Fatalf("missing reachable entry: %v", e)
	}
}

func TestWorkspaceRootPackageManagedOutput(t *testing.T) {
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", `[workspace]
format=1
members=[]
[package]
format=2
module="example.org/root"
go="1.27"
[[bin]]
name="root"
path="main.go"
`)
	workspaceWrite(t, root, "main.go", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"root\")}\n")
	for i := 0; i < 2; i++ {
		out, e := projectRun(t, root, "run", "--offline")
		if e != nil || !strings.Contains(out, "root") {
			t.Fatalf("%s %v", out, e)
		}
	}
}

func TestWorkspaceNativeMembershipDoesNotReplace(t *testing.T) {
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", "[workspace]\nformat=1\nmembers=[\"app\",\"lib\"]\n")
	workspaceWrite(t, root, "app/goxide.toml", "[package]\nformat=2\nmodule=\"example.org/app\"\ngo=\"1.27\"\n[[go.require]]\nmodule=\"example.org/lib\"\nversion=\"v1.0.0\"\n[[bin]]\nname=\"app\"\npath=\"main.go\"\n")
	workspaceWrite(t, root, "app/main.go", "package main\nfunc main(){}\n")
	workspaceWrite(t, root, "lib/goxide.toml", "[package]\nformat=2\nmodule=\"example.org/lib\"\ngo=\"1.27\"\n[lib]\nname=\"lib\"\npath=\"lib.go\"\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\n")
	if _, e := projectRun(t, root, "build", "--package", "example.org/app", "--offline"); e == nil || !strings.Contains(e.Error(), "conflicting version/path sources") {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(root, "app", "goxide.toml"))
	workspaceWrite(t, root, "app/goxide.toml", string(b)+"\n[[go.replace]]\nmodule=\"example.org/lib\"\npath=\"../lib\"\n[dependencies]\nlib={module=\"example.org/lib\",path=\"../lib\"}\n")
	if out, e := projectRun(t, root, "build", "--package", "example.org/app", "--offline"); e != nil {
		t.Fatalf("explicit replacement: %s %v", out, e)
	}
}

func TestWorkspaceIndirectPinnedOffline(t *testing.T) {
	proxy, cache := versionProxy(t)
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		publishVersion(t, proxy, "example.org/d", v, map[string]string{"go.mod": "module example.org/d\n\ngo 1.27\n", "d.go": fmt.Sprintf("package d\nfunc Value()string{return %q}\n", v)})
	}
	publishVersion(t, proxy, "example.org/e", "v1.0.0", map[string]string{"go.mod": "module example.org/e\n\ngo 1.27\nrequire example.org/d v1.0.0\n", "e.go": "package e\nimport \"example.org/d\"\nfunc Value()string{return d.Value()}\n"})
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", "[workspace]\nformat=1\nmembers=[\"app\",\"server\"]\ndefault-members=[\"app\"]\n")
	for _, name := range []string{"app", "server"} {
		dep, v := "example.org/e", "v1.0.0"
		if name == "server" {
			dep, v = "example.org/d", "v1.1.0"
		}
		workspaceWrite(t, root, name+"/goxide.toml", fmt.Sprintf("[package]\nformat=2\nmodule=\"example.org/%s\"\ngo=\"1.27\"\n[[go.require]]\nmodule=%q\nversion=%q\n[[bin]]\nname=\"app\"\npath=\"main.go\"\n", name, dep, v))
		workspaceWrite(t, root, name+"/main.go", "package main\nimport(\"fmt\";d "+fmt.Sprintf("%q", dep)+")\nfunc main(){fmt.Println(d.Value())}\n")
	}
	if out, e := projectRun(t, root, "run"); e != nil || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("%s %v", out, e)
	}
	for _, ext := range []string{"mod", "info", "zip", "ziphash"} {
		os.Remove(filepath.Join(cache, "cache", "download", "example.org", "d", "@v", "v1.0.0."+ext))
	}
	if out, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("indirect pinned-only: %s %v", out, e)
	}
}

func TestWorkspaceNativeEdgesCannotInjectSource(t *testing.T) {
	proxy, _ := versionProxy(t)
	for _, mod := range []string{"d", "e"} {
		publishVersion(t, proxy, "example.org/"+mod, "v1.0.0", map[string]string{"go.mod": "module example.org/" + mod + "\ngo 1.27\n", mod + ".go": "package " + mod + "\nconst X=1\n"})
	}
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", "[workspace]\nformat=1\nmembers=['app','server']\n")
	for _, pair := range [][2]string{{"app", "d"}, {"server", "e"}} {
		m, d := pair[0], pair[1]
		workspaceWrite(t, root, m+"/goxide.toml", fmt.Sprintf("[package]\nformat=2\nmodule='example.org/%s'\ngo='1.27'\n[[go.require]]\nmodule='example.org/%s'\nversion='v1.0.0'\n[[bin]]\nname='%s'\npath='main.go'\n", m, d, m))
		workspaceWrite(t, root, m+"/main.go", "package main\nimport(\"fmt\";\"example.org/"+d+"\")\nfunc main(){fmt.Println("+d+".X)}\n")
	}
	if b, e := projectRun(t, root, "run", "--package", "example.org/app"); e != nil {
		t.Fatalf("initial %s %v", b, e)
	}
	lockPath := filepath.Join(root, "goxide.lock")
	b, _ := os.ReadFile(lockPath)
	var lock project.Lock
	json.Unmarshal(b, &lock)
	b, _ = json.Marshal(lock)
	var raw map[string]any
	json.Unmarshal(b, &raw)
	raw["unified"].(map[string]any)["native_requires"] = map[string]any{"example.org/d": []string{"example.org/e"}}
	b, _ = json.Marshal(raw)
	os.WriteFile(lockPath, b, 0644)
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\";\"example.org/e\")\nfunc main(){fmt.Println(d.X,e.X)}\n")
	if b, e := projectRun(t, root, "run", "--package", "example.org/app", "--offline", "--locked"); e == nil {
		t.Fatalf("forged edge injected undeclared E: %s", b)
	}
}

func TestWorkspaceNativeGoModTamper(t *testing.T) {
	proxy, cache := versionProxy(t)
	for _, mod := range []string{"d", "e"} {
		publishVersion(t, proxy, "example.org/"+mod, "v1.0.0", map[string]string{"go.mod": "module example.org/" + mod + "\ngo 1.27\n", mod + ".go": "package " + mod + "\nconst X=1\n"})
	}
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", "[workspace]\nformat=1\nmembers=['app','server']\n")
	for _, pair := range [][2]string{{"app", "d"}, {"server", "e"}} {
		m, d := pair[0], pair[1]
		workspaceWrite(t, root, m+"/goxide.toml", fmt.Sprintf("[package]\nformat=2\nmodule='example.org/%s'\ngo='1.27'\n[[go.require]]\nmodule='example.org/%s'\nversion='v1.0.0'\n[[bin]]\nname='%s'\npath='main.go'\n", m, d, m))
		workspaceWrite(t, root, m+"/main.go", "package main\nimport(\"fmt\";\"example.org/"+d+"\")\nfunc main(){fmt.Println("+d+".X)}\n")
	}
	if b, e := projectRun(t, root, "run", "--package", "example.org/app"); e != nil {
		t.Fatalf("initial %s %v", b, e)
	}
	modfile := filepath.Join(cache, "cache/download/example.org/d/@v/v1.0.0.mod")
	os.Chmod(modfile, 0644)
	os.WriteFile(modfile, []byte("module example.org/d\ngo 1.27\nrequire example.org/e v1.0.0\n"), 0644)
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\";\"example.org/e\")\nfunc main(){fmt.Println(d.X,e.X)}\n")
	if b, e := projectRun(t, root, "run", "--package", "example.org/app", "--offline", "--locked"); e == nil {
		t.Fatalf("forged edge injected undeclared E: %s", b)
	}
}

func TestWorkspaceLegacyNativeSyntheticMod(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/legacy", "v1.0.0", map[string]string{"legacy.go": "package legacy\nfunc Value()string{return \"legacy\"}\n"})
	workspaceWrite(t, proxy, "example.org/legacy/@v/v1.0.0.mod", "module example.org/legacy\n")
	root := t.TempDir()
	workspaceWrite(t, root, "goxide.toml", "[workspace]\nformat=1\nmembers=[\"app\"]\n")
	workspaceWrite(t, root, "app/goxide.toml", "[package]\nformat=2\nmodule=\"example.org/app\"\ngo=\"1.27\"\n[[go.require]]\nmodule=\"example.org/legacy\"\nversion=\"v1.0.0\"\n[[bin]]\nname=\"app\"\npath=\"main.go\"\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/legacy\")\nfunc main(){fmt.Println(legacy.Value())}\n")
	for _, args := range [][]string{{"run"}, {"run", "--offline", "--locked"}} {
		if out, e := projectRun(t, root, args...); e != nil || !strings.Contains(out, "legacy") {
			t.Fatalf("%s %v", out, e)
		}
	}
}
