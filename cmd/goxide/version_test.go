package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
)

func versionProxy(t *testing.T) (string, string) {
	t.Helper()
	proxy, cache := t.TempDir(), t.TempDir()
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", cache)
	t.Cleanup(func() {
		filepath.Walk(cache, func(path string, info os.FileInfo, e error) error {
			if e == nil && info.IsDir() {
				os.Chmod(path, 0755)
			}
			return nil
		})
	})
	return proxy, cache
}
func publishVersion(t *testing.T, proxy, module, version string, files map[string]string) {
	t.Helper()
	if text, ok := files["goxide.toml"]; ok {
		files["goxide.toml"] = fixtureManifest(t, text, files["go.mod"])
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, name := range project.Keys(files) {
		w, e := zw.Create(module + "@" + version + "/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(files[name])); e != nil {
			t.Fatal(e)
		}
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(proxy, filepath.FromSlash(module), "@v")
	projectWrite(t, dir, map[string]string{version + ".mod": files["go.mod"], version + ".info": `{"Version":"` + version + `","Time":"2024-01-01T00:00:00Z"}`, version + ".zip": b.String()})
	f, e := os.OpenFile(filepath.Join(dir, "list"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if e != nil {
		t.Fatal(e)
	}
	fmt.Fprintln(f, version)
	f.Close()
}
func versionLibrary(version string) map[string]string {
	return map[string]string{"go.mod": "module example.org/goxidelib\n\ngo 1.27\n", "goxide.toml": `[features]
a = []
b = []
d = []
default = ['d']

[lib]
name = 'lib'
path = '.'

[go_mod]
`, "lib.gox": "package lib\nuse fmt;\nfunc Value()string{return fmt::Sprint(\"" + version + " \",cfg!(feature(\"d\")),\" \",cfg!(feature(\"a\")),\" \",cfg!(feature(\"b\")))}\n"}
}
func plainVersionLibrary(version string) map[string]string {
	return map[string]string{"go.mod": "module example.org/goxidelib\ngo 1.27\n", "goxide.toml": "[go_mod]\n[lib]\nname='lib'\npath='.'\n", "lib.gox": "package lib\nfunc Value()string{return " + strconv.Quote(version) + "}\n"}
}

func versionApp(t *testing.T, dependency string) string {
	t.Helper()
	root := t.TempDir()
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\n", "goxide.toml": manifestJSON(`{"bins":[{"name":"app","path":"main.gox"}],"dependencies":{"lib":` + dependency + `}}`), "main.gox": "package main\nuse fmt;\nuse lib as l;\nfunc main(){fmt::Println(l::Value())}\n"})
	return root
}
func sourceHash(t *testing.T, root string) string {
	t.Helper()
	h := sha256.New()
	e := filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(root, path)
		fmt.Fprintln(h, rel, info.Mode())
		if !info.IsDir() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			h.Write(b)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
func TestVersionSourceDefaultsAndReadonlyCache(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI feature-selected dependency versions deferred by P6")
	proxy, cache := versionProxy(t)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", versionLibrary("v1"))
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	if _, e := projectRun(t, root, "run", "--offline"); e == nil {
		t.Fatal("cold offline accepted")
	}
	b, e := projectRun(t, root, "run")
	if e != nil || strings.TrimSpace(b) != "v1 true false false" {
		t.Fatalf("%s %v", b, e)
	}
	cached := filepath.Join(cache, "example.org/goxidelib@v1.0.0")
	before := sourceHash(t, cached)
	if b, e = projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "v1 true false false" {
		t.Fatalf("%s %v", b, e)
	}
	// Native require is a version constraint, not a second defaults request.
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/goxidelib v1.0.0\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.lib]
default_features = false
features = ['a']
go = 'example.org/goxidelib'
version = 'v1.0.0'

[go_mod]
`})
	if b, e = projectRun(t, root, "run", "--offline"); e != nil || strings.TrimSpace(b) != "v1 false true false" {
		t.Fatalf("%s %v", b, e)
	}
	if got := sourceHash(t, cached); got != before {
		t.Fatal("downloaded source changed")
	}
	// Existing expanded cache pollution must fail before its manifest is loaded.
	manifest := filepath.Join(cached, "goxide.toml")
	if e = os.Chmod(manifest, 0644); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(manifest, []byte("not json"), 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "source verification") {
		t.Fatal(e)
	}
}
func TestVersionSourceUnifiedMVSAndDiamond(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI feature-selected dependency versions deferred by P6")
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", versionLibrary("v1"))
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", versionLibrary("v2"))
	publishVersion(t, proxy, "example.org/native", "v1.0.0", map[string]string{"go.mod": "module example.org/native\n\ngo 1.27\nrequire example.org/goxidelib v1.1.0\n", "lib.go": "package native\n"})
	base := t.TempDir()
	root := filepath.Join(base, "root")
	for _, name := range []string{"left", "right"} {
		f := "a"
		if name == "right" {
			f = "b"
		}
		projectWrite(t, filepath.Join(base, name), map[string]string{"go.mod": "module example.org/" + name + "\n\ngo 1.27\n", "goxide.toml": manifestJSON(`{"lib":{"name":"lib","path":"."},"dependencies":{"lib":{"go":"example.org/goxidelib","version":"v1.0.0","default_features":false,"features":["` + f + `"]}}}`), "lib.gox": "package lib\nuse lib as l;\nfunc Value()string{return l::Value()}\n"})
	}
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/native v1.0.0\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.left]
module = 'example.org/left'
path = '../left'

[dependencies.right]
module = 'example.org/right'
path = '../right'

[go_mod]
`, "main.gox": "package main\nuse fmt;\nuse left as l;\nuse right as r;\nfunc main(){fmt::Println(l::Value(),r::Value())}\n"})
	if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "v2 false true true v2 false true true" {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
}
func TestVersionSourceNativeAndMixedDiagnostics(t *testing.T) {
	for _, kind := range []string{"native", "metadata", "same-name"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "native" {
				t.Skip("MVP-DEFERRED: MVP CLI native dependency feature selection deferred by P6")
			}
			proxy, _ := versionProxy(t)
			files := versionLibrary("v1")
			dep := `{"go":"example.org/goxidelib","version":"v1.0.0"}`
			want := "mixes generated"
			switch kind {
			case "native":
				delete(files, "goxide.toml")
				delete(files, "lib.gox")
				files["lib.go"] = "package lib\nfunc Value()string{return \"fixed\"}\n"
				dep = `{"go":"example.org/goxidelib","version":"v1.0.0","features":["a"]}`
				want = "cannot receive goxide features"
			case "metadata":
				files[".goxide.enums.json"] = "{}"
			case "same-name":
				files["lib.go"] = "package lib\n"
				want = "duplicate"
			}
			publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", files)
			root := versionApp(t, dep)
			if b, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), want) {
				t.Fatalf("%s %v", b, e)
			}
		})
	}
}
func TestVersionSourceGraphChangeAndOscillation(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI feature-selected dependency versions deferred by P6")
	for _, oscillates := range []bool{false, true} {
		t.Run(fmt.Sprint(oscillates), func(t *testing.T) {
			proxy, _ := versionProxy(t)
			first := versionLibrary("v1")
			first["goxide.toml"] = `[dependencies]
[dependencies.bridge]
go = 'example.org/bridge'
version = 'v1.0.0'

[lib]
name = 'lib'
path = '.'

[go_mod]
`
			first["lib.gox"] = "package lib\nfunc Value()string{return \"old\"}\n"
			second := versionLibrary("v2")
			if !oscillates {
				second["goxide.toml"] = `[dependencies]
[dependencies.new]
go = 'example.org/newbridge'
version = 'v1.0.0'

[features]
a = []
b = []
d = []
default = ['d']

[lib]
name = 'lib'
path = '.'

[go_mod]
`
				publishVersion(t, proxy, "example.org/newbridge", "v1.0.0", map[string]string{"go.mod": "module example.org/newbridge\n\ngo 1.27\nrequire example.org/goxidelib v1.1.0\n", "lib.go": "package newbridge\n"})
			}
			publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", first)
			publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", second)
			publishVersion(t, proxy, "example.org/bridge", "v1.0.0", map[string]string{"go.mod": "module example.org/bridge\n\ngo 1.27\nrequire example.org/goxidelib v1.1.0\n", "lib.go": "package bridge\n"})
			root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0","features":["a"]}`)
			b, e := projectRun(t, root, "run")
			if oscillates {
				if e == nil || !strings.Contains(e.Error(), "oscillates") {
					t.Fatalf("%s %v", b, e)
				}
			} else if e != nil || strings.TrimSpace(b) != "v2 true true false" {
				t.Fatalf("temporary missing feature should resolve: %s %v", b, e)
			}
			if !oscillates {
				g, err := project.Load(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, true, true, true)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := g.Plan["example.org/bridge"]; ok {
					t.Fatal("old manifest dependency survived stable rebuild")
				}
				if g.Plan["example.org/newbridge"].Version != "v1.0.0" {
					t.Fatal("new manifest dependency missing")
				}
			}
		})
	}
}
func TestVersionSourceUnusedNativeDoesNotActivateScript(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/unused", "v1.0.0", map[string]string{"go.mod": "module example.org/unused\n\ngo 1.27\n", "goxide.toml": `[build]
entry = 'missing/script.go'

[go_mod]
`, "lib.go": "package unused\n"})
	root := t.TempDir()
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/unused v1.0.0 // indirect\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[go_mod]
`, "main.gox": "package main\nfunc main(){}\n"})
	if b, e := projectRun(t, root, "run"); e != nil {
		t.Fatalf("unused module activated: %s %v", b, e)
	}
}
func TestVersionSourceRootReplacement(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI feature-selected dependency versions deferred by P6")
	versionProxy(t)
	base := t.TempDir()
	local := filepath.Join(base, "lib")
	projectWrite(t, local, versionLibrary("local"))
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0","default_features":false,"features":["a"]}`)
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nreplace example.org/goxidelib => " + filepath.ToSlash(local) + "\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil || strings.TrimSpace(b) != "local false true false" {
		t.Fatalf("%s %v", b, e)
	}
	g, e := project.Load(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, true, true, true)
	if e != nil {
		t.Fatal(e)
	}
	if g.Plan["example.org/goxidelib"].Replace.Path != local {
		t.Fatal(g.Plan)
	}
}

func TestVersionSourceTargetABAAndGeneratedGo(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI feature-selected dependency versions deferred by P6")
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", versionLibrary("v1"))
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", versionLibrary("v2"))
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0","default_features":false}`)
	manifest := func(version string) string {
		return manifestJSON(`{"features":{"a":["lib/a"]},"bins":[{"name":"app","path":"main.gox"}],"dependencies":{"lib":{"go":"example.org/goxidelib","version":"` + version + `","default_features":false}}}`)
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("v1.0.0")})
	build := func(args ...string) string {
		t.Helper()
		b, e := projectRun(t, root, append([]string{"build"}, args...)...)
		if e != nil {
			t.Fatalf("%s %v", b, e)
		}
		return strings.TrimSpace(b)
	}
	a := build()
	runLayoutBinary(t, a, "v1 false false false")
	b := build("--features", "a", "--offline")
	if a == b {
		t.Fatal("feature output collided")
	}
	runLayoutBinary(t, b, "v1 false true false")
	if again := build("--offline", "--locked"); again != a {
		t.Fatal("A/B/A configuration path changed")
	}
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("v1.1.0")})
	v2 := build()
	if v2 == a {
		t.Fatal("selected versions collided")
	}
	runLayoutBinary(t, v2, "v2 false false false")
	projectWrite(t, root, map[string]string{"goxide.toml": manifest("v1.0.0")})
	if again := build("--offline", "--locked"); again != a {
		t.Fatal("version A/B/A path changed")
	}
	generated, e := projectRun(t, root, "transpile", "--offline", "--locked")
	if e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"test", "-race", "-mod=readonly", "./..."}, {"vet", "-mod=readonly", "./..."}} {
		c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
		c.Dir = strings.TrimSpace(generated)
		c.Env = project.GoEnv(true)
		if out, e := c.CombinedOutput(); e != nil {
			t.Fatalf("generated Go %v: %s %v", args, out, e)
		}
	}
}
func TestVersionSourceScriptPermissionTracksSelectedVersion(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI build-script permission by selected version deferred by P6")
	proxy, cache := versionProxy(t)
	fixture := buildScriptFixture(t, false, false)
	raw, e := os.ReadFile(filepath.Join(fixture, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = decodeManifestFixture(raw, &m); e != nil {
		t.Fatal(e)
	}
	delete(m, "bins")
	m["lib"] = map[string]string{"name": "lib", "path": "lib/lib.gox"}
	files := map[string]string{}
	if e = filepath.Walk(fixture, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(fixture, path)
		files[filepath.ToSlash(rel)] = string(b)
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	files["goxide.toml"] = projectManifest(m)
	files["go.mod"] = "module example.org/goxidelib\n\ngo 1.27\n"
	files["lib/lib.gox"] = "package lib\nuse gen;\nfunc Value()string{return gen::Value}\n"
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", files)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", files)
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	scriptAction(t, root, "trust-project")
	plan := scriptPlan(t, root)
	if len(plan.Subjects) != 1 {
		t.Fatal(plan)
	}
	before := sourceHash(t, filepath.Join(cache, "example.org/goxidelib@v1.0.0"))
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("unlicensed script: %s %v", b, e)
	}
	scriptAction(t, root, "allow", "--subject", plan.Subjects[0].ID)
	for i := 0; i < 2; i++ {
		b, e := projectRun(t, root, "run", "--offline")
		if e != nil || !strings.Contains(b, "schema-one/template-one/alpha") {
			t.Fatalf("%s %v", b, e)
		}
		if i == 1 && !strings.Contains(b, "cache hit") {
			t.Fatal("script cache not reused", b)
		}
	}
	if got := sourceHash(t, filepath.Join(cache, "example.org/goxidelib@v1.0.0")); got != before {
		t.Fatal("script changed downloaded module")
	}
	manifest := `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.lib]
go = 'example.org/goxidelib'
version = 'v1.1.0'

[go_mod]
`
	projectWrite(t, root, map[string]string{"goxide.toml": manifest})
	changed := scriptPlan(t, root)
	if len(changed.Subjects) != 1 || changed.Subjects[0].ID == plan.Subjects[0].ID || changed.Subjects[0].Trusted {
		t.Fatal("version upgrade reused permission", changed)
	}
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "FIXTURE_GENERATOR_EXECUTED") {
		t.Fatalf("version auto-authorized: %s %v", b, e)
	}
	scriptAction(t, root, "allow", "--subject", changed.Subjects[0].ID)
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	scriptAction(t, root, "revoke", "--subject", changed.Subjects[0].ID)
	if b, e := projectRun(t, root, "run", "--offline"); e == nil || strings.Contains(b, "schema-one") {
		t.Fatalf("revoked cache executed: %s %v", b, e)
	}
}

func TestVersionSourceNamespaceAndNativeBackedge(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", versionLibrary("v1"))
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire goxide-planner.invalid/requirements/r0 v1.0.0\n"})
	if _, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "reserved planner") {
		t.Fatal(e)
	}
	publishVersion(t, proxy, "example.org/native", "v1.0.0", map[string]string{"go.mod": "module example.org/native\n\ngo 1.27\nrequire goxide-planner.invalid/requirements/r0 v1.0.0\n", "lib.go": "package native\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/native v1.0.0\n"})
	if _, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "reserved planner") {
		t.Fatal(e)
	}
	base := t.TempDir()
	dep := filepath.Join(base, "dep")
	app := filepath.Join(base, "app")
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/native\n\ngo 1.27\nrequire example.org/app v1.0.0\n", "lib.go": "package native\nfunc Value()int{return 42}\n"})
	projectWrite(t, app, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nrequire example.org/native v0.0.0\nreplace example.org/native => ../dep\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.go'

[dependencies]
[dependencies.native]
module = 'example.org/native'
path = '../dep'

[go_mod]
`, "main.go": "package main\nimport \"fmt\"\nimport n \"example.org/native\"\nfunc main(){fmt.Println(n.Value())}\n"})
	if b, e := projectRun(t, app, "run", "--offline"); e != nil || strings.TrimSpace(b) != "42" {
		t.Fatalf("native module backedge: %s %v", b, e)
	}
}
func TestVersionSourceForkAndPathIdentity(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/fork", "v1.0.0", map[string]string{"go.mod": "module example.org/fork\n\ngo 1.27\n", "lib.go": "package lib\nfunc Value()string{return \"native fork\"}\n"})
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nreplace example.org/goxidelib => example.org/fork v1.0.0\n"})
	if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "native fork" {
		t.Fatalf("native renamed fork: %s %v", b, e)
	}
	renamed := versionLibrary("renamed")
	renamed["go.mod"] = "module example.org/fork\n\ngo 1.27\n"
	publishVersion(t, proxy, "example.org/fork", "v1.1.0", renamed)
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nreplace example.org/goxidelib => example.org/fork v1.1.0\n"})
	if _, err := projectRun(t, root, "run"); err == nil || !strings.Contains(err.Error(), "module path mismatch") {
		t.Fatal("renamed goxide source contract", err)
	}
	t.Run("feature-merge", func(t *testing.T) {
		t.Skip("MVP-DEFERRED: MVP CLI features merged across version and path sources deferred by P6")
		local := t.TempDir()
		projectWrite(t, local, versionLibrary("local"))
		manifest := projectManifest(map[string]any{"bins": []map[string]string{{"name": "app", "path": "main.gox"}}, "dependencies": map[string]any{"lib": map[string]any{"go": "example.org/goxidelib", "version": "v1.0.0", "default_features": false, "features": []string{"a"}}, "local": map[string]any{"module": "example.org/goxidelib", "path": local, "features": []string{"b"}}}})
		projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\n", "goxide.toml": manifest})
		if _, e := projectRun(t, root, "run", "--offline"); e == nil || !strings.Contains(e.Error(), "conflicting version/path") {
			t.Fatal(e)
		}
		projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\nreplace example.org/goxidelib => " + filepath.ToSlash(local) + "\n"})
		if b, e := projectRun(t, root, "run", "--offline"); e != nil || strings.TrimSpace(b) != "local true true true" {
			t.Fatalf("same-source merge: %s %v", b, e)
		}
	})
}

func TestVersionSourceHigherBridgeConstraintClosure(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", plainVersionLibrary("v1"))
	high := plainVersionLibrary("v2")
	high["go.mod"] += "require example.org/onlyhigh v1.0.0\n"
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", high)
	publishVersion(t, proxy, "example.org/onlyhigh", "v1.0.0", map[string]string{"go.mod": "module example.org/onlyhigh\n\ngo 1.27\n", "lib.go": "package onlyhigh\n"})
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	projectWrite(t, root, map[string]string{"goxide.toml": `[[bin]]
name = 'app'
path = 'main.gox'

[dependencies]
[dependencies.a]
go = 'example.org/goxidelib'
version = 'v1.0.0'

[dependencies.z]
go = 'example.org/goxidelib'
version = 'v1.1.0'

[go_mod]
`, "main.gox": "package main\nuse fmt;\nuse a as l;\nfunc main(){fmt::Println(l::Value())}\n"})
	g, e := project.Load(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, true, false, false)
	if e != nil {
		t.Fatal(e)
	}
	if g.Plan["example.org/goxidelib"].Version != "v1.1.0" || g.Plan["example.org/onlyhigh"].Version != "v1.0.0" {
		t.Fatal(g.Plan)
	}
	if b, e := projectRun(t, root, "run", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
}

func TestVersionSourceMetadataAgreementAndLegacyGo(t *testing.T) {
	t.Run("goxide mod agreement", func(t *testing.T) {
		proxy, _ := versionProxy(t)
		files := versionLibrary("v1")
		publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", files)
		projectWrite(t, filepath.Join(proxy, "example.org/goxidelib/@v"), map[string]string{"v1.0.0.mod": files["go.mod"] + "// mismatched proxy metadata\n"})
		root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
		if _, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "differs between selected metadata") {
			t.Fatal(e)
		}
	})
	t.Run("native synthetic mod", func(t *testing.T) {
		proxy, _ := versionProxy(t)
		files := map[string]string{"go.mod": "module example.org/goxidelib\n", "lib.go": "package lib\nfunc Value()string{return \"legacy\"}\n"}
		publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", files)
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		w, e := z.Create("example.org/goxidelib@v1.0.0/lib.go")
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(files["lib.go"]))
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
		projectWrite(t, filepath.Join(proxy, "example.org/goxidelib/@v"), map[string]string{"v1.0.0.zip": b.String()})
		root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
		if b, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(b) != "legacy" {
			t.Fatalf("native synthetic go.mod: %s %v", b, e)
		}
	})
}
func TestVersionSourceDormantAndFeatureErrors(t *testing.T) {
	for _, kind := range []string{"optional", "unknown", "cycle", "path-escape", "missing-lib"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "optional" || kind == "unknown" || kind == "cycle" {
				t.Skip("MVP-DEFERRED: MVP CLI optional dependencies and feature graphs deferred by P6")
			}
			proxy, cache := versionProxy(t)
			files := versionLibrary("v1")
			dep := `{"go":"example.org/goxidelib","version":"v1.0.0"}`
			want := ""
			switch kind {
			case "optional":
				files["goxide.toml"] = `[dependencies]
[dependencies.missing]
optional = true
go = 'example.org/missing'
version = 'v1.0.0'

[features]
a = []
b = []
d = []
default = ['d']

[lib]
name = 'lib'
path = '.'

[go_mod]
`
			case "missing-lib":
				files["goxide.toml"] = `[features]
a = []
b = []
d = []
default = ['d']

[go_mod]
`
				want = "must declare lib"
			case "unknown":
				dep = `{"go":"example.org/goxidelib","version":"v1.0.0","features":["missing"]}`
				want = "unknown feature"
			case "cycle":
				files["goxide.toml"] = `[features]
a = ['b']
b = ['a']

[lib]
name = 'lib'
path = '.'

[go_mod]
`
				want = "feature cycle"
			case "path-escape":
				outside := t.TempDir()
				projectWrite(t, outside, map[string]string{"go.mod": "module example.org/outside\n\ngo 1.27\n", "lib.go": "package outside\n"})
				files["goxide.toml"] = projectManifest(map[string]any{"lib": map[string]string{"name": "lib", "path": "."}, "dependencies": map[string]any{"outside": map[string]string{"module": "example.org/outside", "path": outside}}})
				want = "escapes checkout/acquired"
			}
			publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", files)
			root := versionApp(t, dep)
			b, e := projectRun(t, root, "run")
			if want != "" {
				if e == nil || !strings.Contains(e.Error(), want) {
					t.Fatalf("%s %v", b, e)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if _, e = os.Stat(filepath.Join(cache, "example.org/missing@v1.0.0")); !os.IsNotExist(e) {
					t.Fatal("dormant optional acquired")
				}
			}
		})
	}
}

func TestVersionSourceLocalGoMajorPath(t *testing.T) {
	versionProxy(t)
	base := t.TempDir()
	dep := filepath.Join(base, "dep")
	root := filepath.Join(base, "root")
	projectWrite(t, dep, map[string]string{"go.mod": "module gopkg.in/goxidefixture.v2\n\ngo 1.27\n", "lib.go": "package lib\nfunc Value()int{return 2}\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\n", "goxide.toml": `[[bin]]
name = 'app'
path = 'main.go'

[dependencies]
[dependencies.lib]
module = 'gopkg.in/goxidefixture.v2'
path = '../dep'

[go_mod]
`, "main.go": "package main\nimport \"fmt\"\nimport l \"gopkg.in/goxidefixture.v2\"\nfunc main(){fmt.Println(l.Value())}\n"})
	if b, e := projectRun(t, root, "run", "--offline"); e != nil || strings.TrimSpace(b) != "2" {
		t.Fatalf("Go module major path: %s %v", b, e)
	}
}

func TestVersionSourceLateGitBoundary(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	repo := filepath.Join(base, "repo")
	outside := filepath.Join(base, "outside")
	bdir := filepath.Join(base, "b")
	zdir := filepath.Join(base, "z")
	manifest := func(deps map[string]any) string {
		return projectManifest(map[string]any{"lib": map[string]string{"name": "lib", "path": "."}, "dependencies": deps})
	}
	projectWrite(t, outside, map[string]string{"go.mod": "module example.org/c\n\ngo 1.27\n", "lib.go": "package c\n"})
	projectWrite(t, repo, map[string]string{"go.mod": "module example.org/a\n\ngo 1.27\n", "lib.go": "package a\n", "goxide.toml": manifest(map[string]any{"outside": map[string]string{"module": "example.org/c", "path": outside}})})
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		c.Dir = repo
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("%s %v", out, e)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-b", "main")
	git("add", ".")
	git("commit", "-m", "boundary fixture")
	rev := git("rev-parse", "HEAD")
	gitdep := map[string]any{"module": "example.org/a", "git": repo, "rev": rev}
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/root\n\ngo 1.27\n", "lib.go": "package root\n", "goxide.toml": manifest(map[string]any{"a": gitdep})})
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, e := project.Load(root, tool, nil, true, false, false); e == nil || !strings.Contains(e.Error(), "escapes checkout/acquired") {
		t.Fatal(e)
	}
	cached := filepath.Join(root, "target", "cache", "git", project.Digest([]string{repo, rev, "", "example.org/a"}))
	projectWrite(t, bdir, map[string]string{"go.mod": "module example.org/b\n\ngo 1.27\n", "lib.go": "package b\n", "goxide.toml": manifest(map[string]any{"z": map[string]string{"module": "example.org/z", "path": zdir}})})
	projectWrite(t, zdir, map[string]string{"go.mod": "module example.org/z\n\ngo 1.27\n", "lib.go": "package z\n", "goxide.toml": manifest(map[string]any{"a": gitdep})})
	// a/c were already traversed as path nodes before z supplies the stronger
	// Git boundary for the same a source, with no new feature requests.
	projectWrite(t, root, map[string]string{"goxide.toml": manifest(map[string]any{"a": map[string]string{"module": "example.org/a", "path": cached}, "b": map[string]string{"module": "example.org/b", "path": bdir}})})
	if _, e := project.Load(root, tool, nil, true, true, false); e == nil || !strings.Contains(e.Error(), "escapes checkout/acquired") {
		t.Fatalf("late Git boundary was not propagated: %v", e)
	}
}
