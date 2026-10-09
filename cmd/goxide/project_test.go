package main

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func projectWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	fixtureAuthoring(t, root, files)
	for name, s := range files {
		p := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
}
func projectRun(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	argv := append([]string{args[0], "--go", filepath.Join(runtime.GOROOT(), "bin", "go")}, args[1:]...)
	argv = append(argv, root)
	e := run(argv, &b, &b)
	return b.String(), e
}

func TestProjectTargetsAndFeatures(t *testing.T) {
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/project\n\ngo 1.27\n",
		"goxide.toml": `[[bin]]
name = 'one'
path = 'src/one.gox'

[[bin]]
name = 'two'
path = 'src/two.gox'

[[example]]
name = 'demo'
path = 'examples/demo.gox'

[features]
default = ['pretty']
pretty = []

[lib]
name = 'app'
path = 'src/lib.gox'

[package]
format = 1
`,
		"src/lib.gox":       "package app\nfunc Value()int{return 42}\n",
		"src/one.gox":       "package main\nuse fmt;\nuse project as a;\nfunc main(){fmt::Println(\"one\",a::Value(),cfg!(feature(\"pretty\")))}\n",
		"src/two.gox":       "package main\nuse fmt;\nfunc main(){fmt::Println(\"two\")}\n",
		"examples/demo.gox": "package main\nuse fmt;\nfunc main(){fmt::Println(\"example\")}\n",
	})
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--bin", "one", "--offline"}, "one 42 true"},
		{[]string{"run", "--bin", "one", "--no-default-features", "--offline", "--locked"}, "one 42 false"},
		{[]string{"run", "--bin", "one", "--offline", "--locked"}, "one 42 true"},
		{[]string{"run", "--bin", "two", "--offline"}, "two"},
		{[]string{"run", "--example", "demo", "--offline"}, "example"},
		{[]string{"build", "--lib", "--offline"}, ""},
	} {
		b, e := projectRun(t, root, tc.args...)
		if e != nil || strings.TrimSpace(b) != tc.want {
			t.Fatalf("%v: %s %v", tc.args, b, e)
		}
	}
	if _, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "multiple bins") {
		t.Fatal(e)
	}
	if _, e := projectRun(t, root, "run", "--lib"); e == nil || !strings.Contains(e.Error(), "cannot be run") {
		t.Fatal(e)
	}
	if _, e := projectRun(t, root, "run", "--bin", "one", "--features", "missing"); e == nil || !strings.Contains(e.Error(), "unknown feature") {
		t.Fatal(e)
	}
	bundle := filepath.Join(t.TempDir(), "bundle")
	if b, e := projectRun(t, root, "transpile", "--bin", "one", "--offline", "-o", bundle); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	generated := filepath.Join(bundle, "module")
	for _, args := range [][]string{{"test", "-race", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
		cmd.Dir = generated
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-buildvcs=false")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%s %v", b, e)
		}
	}
	sm, e := os.ReadFile(filepath.Join(generated, "goxide_targets/bin/one/one.go.map.json"))
	if e != nil || !bytes.Contains(sm, []byte(filepath.Join(root, "src/one.gox"))) {
		t.Fatalf("original source mapping missing: %s %v", sm, e)
	}
	if _, e = os.Stat(filepath.Join(generated, "src/two.go")); !os.IsNotExist(e) {
		t.Fatal("unselected entry emitted")
	}
}

func TestProjectNoLibAndDiagnostics(t *testing.T) {
	root := t.TempDir()
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/nolib\n\ngo 1.27\n", "goxide.toml": `[[bin]]
name = 'main'
path = 'app.gox'

[package]
format = 1
`, "app.gox": "package main\nfunc main(){}\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	if _, e := projectRun(t, root, "build", "--lib"); e == nil || !strings.Contains(e.Error(), "no lib") {
		t.Fatal(e)
	}
	projectWrite(t, root, map[string]string{"app.gox": "package main\nfunc main(){_ = missing}\n"})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), filepath.Join(root, "app.gox")+":2:") {
		t.Fatalf("source location lost: %v", e)
	}
}

func TestProjectLocalFeatureUnion(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	shared := filepath.Join(base, "shared")
	projectWrite(t, shared, map[string]string{
		"go.mod": "module example.org/shared\n\ngo 1.27\n",
		"goxide.toml": `[features]
a = []
b = []
d = []
default = ['d']

[lib]
name = 'shared'
path = 'lib.gox'

[package]
format = 1
`,
		"lib.gox": "package shared\nfunc Flags()string{s:=\"\";if cfg!(feature(\"a\")){s+=\"a\"};if cfg!(feature(\"b\")){s+=\"b\"};if cfg!(feature(\"d\")){s+=\"d\"};return s}\n",
	})
	for _, name := range []string{"left", "right"} {
		feature := "a"
		if name == "right" {
			feature = "b"
		}
		projectWrite(t, filepath.Join(base, name), map[string]string{
			"go.mod":      "module example.org/" + name + "\n\ngo 1.27\n",
			"goxide.toml": manifestJSON(`{"version":1,"lib":{"name":"lib","path":"lib.gox"},"dependencies":{"shared":{"module":"example.org/shared","path":"../shared","default_features":false,"features":["` + feature + `"]}}}`),
			"lib.gox":     "package " + name + "\nuse shared as s;\nfunc Flags()string{return s::Flags()}\n",
		})
	}
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/root\n\ngo 1.27\n",
		"goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.left]
module = 'example.org/left'
path = '../left'

[dependencies.optional]
module = 'example.org/missing'
optional = true
path = '../not-present'

[dependencies.right]
module = 'example.org/right'
path = '../right'

[features]
missing = ['dep:optional']

[package]
format = 1
`,
		"main.gox":     "package main\nuse fmt;\nuse left as l;\nuse right as r;\nfunc main(){fmt::Println(l::Flags(),r::Flags())}\n",
		"disabled.gox": "//goxide:cfg feature(\"missing\")\npackage main\nuse missing_dependency;\nfunc Never()Unknown{return nonexistent()}\n",
	})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil || strings.TrimSpace(b) != "ab ab" {
		t.Fatalf("union: %s %v", b, e)
	}
	if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "ab ab" {
		t.Fatalf("offline lock: %s %v", b, e)
	}
	if _, e := projectRun(t, root, "run", "--features", "missing", "--offline"); e == nil {
		t.Fatal("missing optional source accepted")
	}
}

func TestProjectGitRevisionLock(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	root := filepath.Join(base, "root")
	projectWrite(t, repo, map[string]string{"go.mod": "module example.org/gitdep\n\ngo 1.27\n", "lib.go": "package gitdep\nfunc Value()int{return 1}\n"})
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git: %s %v", b, e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "main")
	git("add", ".")
	git("commit", "-m", "one")
	first := git("rev-parse", "HEAD")
	manifest := func(rev string) string {
		return projectManifest(map[string]any{"version": 1, "bins": []any{map[string]string{"name": "app", "path": "main.gox"}}, "dependencies": map[string]any{"dep": map[string]string{"module": "example.org/gitdep", "git": repo, "rev": rev}}})
	}
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\n", "goxide.toml": manifest("main"), "main.gox": "package main\nuse fmt;\nuse dep as d;\nfunc main(){fmt::Println(d::Value())}\n"})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "not cached") {
		t.Fatal(e)
	}
	if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "1" {
		t.Fatalf("%s %v", b, e)
	}
	lock, e := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if e != nil || !bytes.Contains(lock, []byte(first)) {
		t.Fatalf("commit not locked: %s %v", lock, e)
	}
	projectWrite(t, repo, map[string]string{"lib.go": "package gitdep\nfunc Value()int{return 2}\n"})
	git("add", ".")
	git("commit", "-m", "two")
	second := git("rev-parse", "HEAD")
	if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "1" {
		t.Fatalf("moving branch changed lock: %s %v", b, e)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest(second)})
	if _, e := projectRun(t, root, "run", "--locked"); e == nil || !strings.Contains(e.Error(), "absent from lock") {
		t.Fatal(e)
	}
	if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "2" {
		t.Fatalf("explicit revision: %s %v", b, e)
	}
	if b, e := projectRun(t, root, "run", "--locked", "--offline"); e != nil || strings.TrimSpace(b) != "2" {
		t.Fatalf("offline revision: %s %v", b, e)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("main")})
	if b, e := projectRun(t, root, "run", "--locked", "--offline"); e != nil || strings.TrimSpace(b) != "1" {
		t.Fatalf("revision A/B/A: %s %v", b, e)
	}
}

func TestProjectNativeModuleVersionAndReplace(t *testing.T) {
	proxy := t.TempDir()
	versionDir := filepath.Join(proxy, "example.org", "native", "@v")
	if e := os.MkdirAll(versionDir, 0755); e != nil {
		t.Fatal(e)
	}
	files := map[string]string{"go.mod": "module example.org/native\n\ngo 1.27\n", "native.go": "package native\nfunc Value()int{return 17}\n"}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, data := range files {
		w, e := zw.Create("example.org/native@v1.0.0/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(data)); e != nil {
			t.Fatal(e)
		}
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	projectWrite(t, versionDir, map[string]string{"list": "v1.0.0\n", "v1.0.0.info": `{"Version":"v1.0.0","Time":"2024-01-01T00:00:00Z"}`, "v1.0.0.mod": files["go.mod"], "v1.0.0.zip": archive.String()})
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	modcache := t.TempDir()
	t.Setenv("GOMODCACHE", modcache)
	t.Cleanup(func() {
		filepath.Walk(modcache, func(path string, info os.FileInfo, e error) error {
			if e == nil && info.IsDir() {
				os.Chmod(path, 0755)
			}
			return nil
		})
	})
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/app\n\ngo 1.27\n",
		"goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.native]
module = 'example.org/native'
version = 'v1.0.0'

[package]
format = 1
`,
		"main.gox": "package main\nuse fmt;\nuse native as n;\nfunc main(){fmt::Println(n::Value())}\n",
	})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil {
		t.Fatal("offline cold cache accepted")
	}
	if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "17" {
		t.Fatalf("native version: %s %v", b, e)
	}
	if e := os.RemoveAll(proxy); e != nil {
		t.Fatal(e)
	}
	if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "17" {
		t.Fatalf("offline cached native: %s %v", b, e)
	}
	sourceMod, e := os.ReadFile(filepath.Join(root, "goxide.toml"))
	if e != nil || bytes.Contains(sourceMod, []byte("require")) {
		t.Fatalf("source go.mod modified: %s %v", sourceMod, e)
	}
	base := t.TempDir()
	native := filepath.Join(base, "native")
	projectWrite(t, native, files)
	app := filepath.Join(base, "app")
	projectWrite(t, app, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/native v1.0.0\nreplace example.org/native => ../native\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[package]
format = 1

[dependencies.native]
module = 'example.org/native'
package = 'example.org/native'
version = 'v1.0.0'
`, "main.gox": "package main\nuse fmt;\nuse native as n;\nfunc main(){fmt::Println(n::Value())}\n"})
	if b, e := projectRun(t, app, "run", "--offline"); e != nil || strings.TrimSpace(b) != "17" {
		t.Fatalf("native local replace: %s %v", b, e)
	}
}

func TestProjectSelectionAndOutputConflicts(t *testing.T) {
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/selection\n\ngo 1.27\n",
		"goxide.toml": `[features]
extra = []

[lib]
name = 'lib'
path = '.'

[package]
format = 1
`,
		"lib.gox":           "package lib\nfunc Value()int{return 3}\n",
		"off.gox":           "//goxide:cfg feature(\"extra\")\npackage lib\nuse no;\nfunc Missing()Unknown{return nope()}\n",
		"tagged.gox":        "//go:build unavailable\n\npackage lib\nvar X=cfg!(feature(\"unknown\"))\n",
		"unrelated/bad.gox": "package other\nvar X=cfg!(feature(\"unknown\"))\n",
		"lib_test.go":       "package lib_test\nimport \"testing\"\nimport h \"example.org/selection/helper\"\nfunc TestValue(t *testing.T){if h.Value()!=3{t.Fatal(\"bad\")}}\n",
		"helper/helper.gox": "package helper\nuse selection as l;\nfunc Value()int{return l::Value()}\n",
	})
	if b, e := projectRun(t, root, "build", "--lib", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := projectRun(t, root, "test", "--lib", "--offline"); e != nil {
		t.Fatalf("test closure: %s %v", b, e)
	}
	if _, e := projectRun(t, root, "build", "--lib", "--offline", "--features", "extra"); e == nil {
		t.Fatal("selected invalid import accepted")
	}
	projectWrite(t, root, map[string]string{"goxide.toml": `[features]
extra = []

[lib]
name = 'lib'
path = 'src/lib.gox'

[package]
format = 1
`, "src/lib.gox": "package lib\nfunc X(){}\n", "lib.go": "package lib\nfunc Existing(){}\n"})
	if _, e := projectRun(t, root, "build", "--lib", "--offline"); e == nil || !strings.Contains(e.Error(), "filename conflicts") {
		t.Fatalf("output collision: %v", e)
	}
}

func TestProjectTransitiveReplacementConflict(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	for _, name := range []string{"real", "override"} {
		projectWrite(t, filepath.Join(base, name), map[string]string{"go.mod": "module example.org/shared\n\ngo 1.27\n", "lib.go": "package shared\nfunc Value()int{return 1}\n"})
	}
	projectWrite(t, filepath.Join(base, "dep"), map[string]string{
		"go.mod": "module example.org/dep\n\ngo 1.27\n", "goxide.toml": `[dependencies]
[dependencies.shared]
module = 'example.org/shared'
path = '../real'

[lib]
name = 'lib'
path = '.'

[package]
format = 1
`, "lib.gox": "package dep\nuse shared as s;\nfunc Value()int{return s::Value()}\n"})
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/root\n\ngo 1.27\nreplace example.org/shared => ../override\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.dep]
module = 'example.org/dep'
path = '../dep'

[package]
format = 1
`, "main.gox": "package main\nuse dep as d;\nfunc main(){_=d::Value()}\n"})
	if _, e := projectRun(t, root, "build", "--offline", "-o", filepath.Join(base, "app")); e == nil || !strings.Contains(e.Error(), "conflicts with go.mod replace") {
		t.Fatal(e)
	}
}

func TestProjectGitCheckoutBoundaries(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	projectWrite(t, outside, map[string]string{"go.mod": "module example.org/outside\n\ngo 1.27\n", "lib.go": "package outside\n"})
	projectWrite(t, repo, map[string]string{"go.mod": "module example.org/gitdep\n\ngo 1.27\n", "lib.go": "package gitdep\n"})
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("%s %v", b, e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "main")
	for _, kind := range []string{"manifest", "gomod"} {
		if kind == "manifest" {
			projectWrite(t, repo, map[string]string{"goxide.toml": projectManifest(map[string]any{"version": 1, "lib": map[string]string{"name": "lib", "path": "."}, "dependencies": map[string]any{"outside": map[string]string{"module": "example.org/outside", "path": outside}}})})
		} else {
			os.Remove(filepath.Join(repo, "goxide.toml"))
			projectWrite(t, repo, map[string]string{"go.mod": "module example.org/gitdep\n\ngo 1.27\nrequire example.org/outside v0.0.0\nreplace example.org/outside => " + outside + "\n"})
		}
		git("add", ".")
		git("commit", "-m", kind)
		rev := git("rev-parse", "HEAD")
		projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\n", "goxide.toml": projectManifest(map[string]any{"version": 1, "bins": []any{map[string]string{"name": "app", "path": "main.gox"}}, "dependencies": map[string]any{"dep": map[string]string{"module": "example.org/gitdep", "git": repo, "rev": rev}}}), "main.gox": "package main\nfunc main(){}\n"})
		if _, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "escapes checkout") {
			t.Fatalf("%s: %v", kind, e)
		}
	}
}

func TestProjectNativeBuildListConsistency(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	dep := filepath.Join(base, "dep")
	native := filepath.Join(base, "native")
	projectWrite(t, native, map[string]string{"go.mod": "module example.org/native\n\ngo 1.27\n", "lib.go": "package native\nfunc Value()int{return 1}\n"})
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/dep\n\ngo 1.27\nrequire example.org/native v1.0.0\nreplace example.org/native => ../native\n", "goxide.toml": `[lib]
name = 'lib'
path = '.'

[package]
format = 1

[dependencies.native]
module = 'example.org/native'
package = 'example.org/native'
version = 'v1.0.0'
`, "lib.gox": "package dep\nuse native as n;\nfunc Value()int{return n::Value()}\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\nrequire example.org/native v1.1.0\nreplace example.org/native => ../native\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.dep]
module = 'example.org/dep'
path = '../dep'

[package]
format = 1
`, "main.gox": "package main\nuse dep as d;\nfunc main(){_=d::Value()}\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("shared plan should align native selections: %s %v", b, e)
	}
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\nrequire example.org/native v1.0.0\nreplace example.org/native => ../native\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("aligned: %s %v", b, e)
	}
}

func TestProjectDependencyAndTargetDiagnostics(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	dep := filepath.Join(base, "dep")
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/dep\n\ngo 1.27\n", "goxide.toml": `[features]
on = []

[lib]
name = 'lib'
path = '.'
required_features = ['on']

[package]
format = 1
`, "lib.gox": "package dep\n"})
	manifest := func(module string, features []string) string {
		return projectManifest(map[string]any{"version": 1, "features": map[string][]string{"demo": {}}, "bins": []any{map[string]string{"name": "app", "path": "main.gox"}}, "examples": []any{map[string]any{"name": "demo", "path": "demo.gox", "required_features": []string{"demo"}}}, "dependencies": map[string]any{"dep": map[string]any{"module": module, "path": "../dep", "features": features}}})
	}
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\n", "goxide.toml": manifest("example.org/wrong", nil), "main.gox": "package main\nfunc main(){}\n", "demo.gox": "package main\nfunc main(){}\n"})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "module path mismatch") {
		t.Fatal(e)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("example.org/dep", []string{"unknown"})})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "unknown feature") {
		t.Fatal(e)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("example.org/dep", nil)})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "requires feature on") {
		t.Fatal(e)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("example.org/dep", []string{"on"})})
	if _, e := projectRun(t, root, "run", "--example", "demo", "--offline"); e == nil || !strings.Contains(e.Error(), "requires feature demo") {
		t.Fatal(e)
	}
	if b, e := projectRun(t, root, "run", "--example", "demo", "--features", "demo", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	projectWrite(t, dep, map[string]string{"goxide.toml": `[dependencies]
[dependencies.root]
module = 'example.org/root'
path = '../root'

[features]
on = []

[lib]
name = 'lib'
path = '.'

[package]
format = 1
`})
	if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "dependency cycle") {
		t.Fatal(e)
	}
}

func TestProjectConditionalGoxideAcrossTargets(t *testing.T) {
	root := t.TempDir()
	// Inline enum discriminants use std/sum.Tag for type-name hygiene. This
	// project selects that public dependency explicitly, including offline runs.
	projectWrite(t, root, map[string]string{
		"go.mod": "module example.org/conditional\n\ngo 1.27\nrequire github.com/goxide-lang/std v0.0.0-20261009104839-c02a09d8c417\n",
		"goxide.toml": `[[bin]]
name = 'one'
path = 'cmd/one'

[[bin]]
name = 'two'
path = 'cmd/two'

[[example]]
name = 'demo'
path = 'examples/demo'

[features]
default = ['extra']
extra = []
never = []

[lib]
name = 'lib'
path = '.'

[package]
format = 1
`,
		"enabled.gox": `//goxide:cfg feature("extra")
package lib
#[derive(Debug)]
type Item struct { Value int }
pub enum State { Empty, Count(int), }
macro_rules! twice { ($x:expr) => { $x + $x }; }
func Value() int {
 return match State::Count(twice!(21)) {State::Empty => 0, State::Count(n) => n,}
}
`,
		"disabled.gox": "//goxide:cfg not(feature(\"extra\"))\npackage lib\nfunc Value()int{return 0}\n",
		"never.gox": `//goxide:cfg feature("never")
package lib
use never as absent;
#[derive(Debug)]
type Missing struct { Value absent::Unknown }
enum Unresolved { Case(absent::Unknown), }
macro_rules! query { ($x:expr) => { cfg!(feature($x)) }; }
func Never(){unknown_macro!(unresolved)}
`,
	})
	for _, entry := range []string{"cmd/one", "cmd/two", "examples/demo"} {
		projectWrite(t, root, map[string]string{entry + "/main.gox": "package main\nuse fmt;\nuse conditional as lib;\nfunc main(){fmt::Println(lib::Value())}\n"})
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--bin", "one", "--offline"}, "42"},
		{[]string{"run", "--bin", "one", "--offline", "--locked", "--no-default-features"}, "0"},
		{[]string{"run", "--bin", "two", "--offline", "--locked"}, "42"},
		{[]string{"run", "--example", "demo", "--offline", "--locked", "--no-default-features", "--features", "extra"}, "42"},
		{[]string{"run", "--bin", "one", "--offline", "--locked"}, "42"},
	} {
		b, e := projectRun(t, root, tc.args...)
		if e != nil || strings.TrimSpace(b) != tc.want {
			t.Fatalf("%v: %s %v", tc.args, b, e)
		}
	}
	// Balanced invalid expression grammar is deferred, not silently accepted when enabled.
	projectWrite(t, root, map[string]string{"never.gox": "//goxide:cfg feature(\"never\")\npackage lib\nfunc Broken(){x:=}\n"})
	if _, e := projectRun(t, root, "run", "--bin", "one", "--offline"); e != nil {
		t.Fatal(e)
	}
	if _, e := projectRun(t, root, "run", "--bin", "one", "--offline", "--features", "never"); e == nil {
		t.Fatal("enabled malformed expression accepted")
	}
}

func TestProjectDependencyRequiresOwnGoMod(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	projectWrite(t, filepath.Join(base, "dep"), map[string]string{"go.mod": "module example.org/dep\n\ngo 1.27\n", "lib.go": "package dep\nfunc Value()int{return 1}\n", "sub/lib.go": "package dep\nfunc Value()int{return 2}\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.dep]
module = 'example.org/dep'
path = '../dep/sub'

[package]
format = 1
`, "main.gox": "package main\nuse dep as d;\nfunc main(){_=d::Value()}\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "own regular go.mod") {
		t.Fatalf("parent module silently adopted: %s %v", b, e)
	}
}
