package acceptance_test

import (
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCrossModuleReferenceArtifacts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "hgo")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/hgo")
	module := func(path, library string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => %s\n", path, filepath.ToSlash(root))
		if library != "" {
			s += fmt.Sprintf("require library.example/refs v0.0.0\nreplace library.example/refs => %s\n", filepath.ToSlash(library))
		}
		return s
	}
	const api = `package api
type Cell struct{N int}
func Read(p &int)int{return *p}
var global int=6
func Global()&int{return &global}
func View(p &int)&int{return p}
func Edit(p mut& int)mut& int{return p}
func Inc(p mut& int){*p+=1}
func CellView(p &Cell)&Cell{return p}
`
	makeLibrary := func(relay, drift bool) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("library.example/refs", ""))
		src := api
		if drift {
			src = strings.Replace(src, "func Read(p &int)int{return *p}", "func Read(p mut& int)int{*p+=1;return *p}", 1)
		}
		packagesWrite(t, dir, "api/api.hgo", src)
		packagesWrite(t, dir, "api/native_default.go", "//go:build !extra\n\npackage api\nfunc Native()int{return 1}\n")
		packagesWrite(t, dir, "api/native_extra.go", "//go:build extra\n\npackage api\nfunc Native()int{return 2}\n")
		if relay {
			packagesWrite(t, dir, "relay/relay.hgo", `package relay
import a "library.example/refs/api"
type Cell=a.Cell
func Global()&int{return a.Global()}
func Read(p &int)int{return a.Read(p)}
func View(p &int)&int{return a.View(p)}
func Edit(p mut& int)mut& int{return a.Edit(p)}
func CellView(p &Cell)&Cell{return a.CellView(p)}
`)
		}
		return dir
	}
	source := makeLibrary(true, false)
	library := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", library, source)
	compare := func(a, b string) {
		t.Helper()
		e := filepath.WalkDir(a, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(a, path)
			x, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			y, e := os.ReadFile(filepath.Join(b, rel))
			if e != nil {
				return e
			}
			if string(x) != string(y) {
				t.Fatalf("nondeterministic %s", rel)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	twice := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
	compare(library, twice)
	makeApp := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("consumer.example/app", library))
		packagesWrite(t, dir, "app.hgo", "package app\nimport r \"library.example/refs/relay\"\n"+body)
		return dir
	}
	body := `import a "library.example/refs/api"
func Run()int{n:=1;w:=r.Edit(mut& n);*w+=2;a.Inc(w);c:=a.Cell{N:5};v:=r.CellView(&c);c.N=7;return r.Read(&n)+v.N}
func Again(p &int)&int{return r.View(p)}
`
	appSource := makeApp(body)
	packagesWrite(t, appSource, "extra.hgo", "package app\nimport renamed \"library.example/refs/api\"\nfunc Extra()int{n:=2;renamed.Inc(mut& n);return renamed.Read(&n)}\n")
	packagesWrite(t, appSource, "client/client.hgo", "package client\nimport app \"consumer.example/app\"\nfunc Read()int{n:=8;return *app.Again(&n)}\n")
	app := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", app, appSource)
	appTwice := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", appTwice, appSource)
	compare(app, appTwice)
	packagesWrite(t, library, "api/api_test.go", `package api
import "testing"
func TestNativeView(t *testing.T){n:=1;p:=View(&n);*p=4;if Read(&n)!=4{t.Fatal(n)}}
`)
	packagesWrite(t, app, "app_test.go", `package app_test
import("testing";a "consumer.example/app";c "consumer.example/app/client")
func TestCrossModule(t *testing.T){if a.Run()!=11||a.Extra()!=3||c.Read()!=8{t.Fatal("identity/permission metadata")}}
`)
	for _, dir := range []string{library, app} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", module("plain.example/app", library))
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";r "library.example/refs/relay")
func TestGoIgnoresHgoPermissions(t *testing.T){n:=1;p:=r.View(&n);*p=9;if r.Read(&n)!=9{t.Fatal(n)}}
`)
	packagesCommand(t, plain, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, plain, tool, true, "vet", "./...")
	for name, bad := range map[string]string{
		"readonly-return-only": `func Bad(){p:=r.Global();*p=9}`,
		"readonly-write":       `func Bad(){n:=1;p:=r.View(&n);*p=9}`,
		"readonly-upgrade":     `func Bad(){n:=1;p:=r.View(&n);_=r.Edit(p)}`,
		"readonly-field":       `func Bad(){c:=r.Cell{N:1};p:=r.CellView(&c);p.N=9}`,
		"readonly-interface":   `func Bad(){n:=1;p:=r.View(&n);var x any=p;_=x}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := makeApp(bad)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), dir)
			if !strings.Contains(out, "readonly") && !strings.Contains(out, "reference") && !strings.Contains(out, "cannot use") {
				t.Fatal(out)
			}
		})
	}
	t.Run("single-file-permissions", func(t *testing.T) {
		good := makeApp("func Read()int{return *r.Global()}")
		out := filepath.Join(t.TempDir(), "read.go")
		packagesCommand(t, root, cli, true, "transpile", "-o", out, filepath.Join(good, "app.hgo"))
		b, e := os.ReadFile(out)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(string(b), "__hgo_permission_call_") {
			t.Fatal("binding stub leaked")
		}
		bad := makeApp("func Bad(){p:=r.Global();*p=9}")
		diagnostic := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad.go"), filepath.Join(bad, "app.hgo"))
		if !strings.Contains(diagnostic, "readonly") {
			t.Fatal(diagnostic)
		}
	})
	valid := makeApp("func Read()int{n:=1;return r.Read(&n)}")
	metaPath := filepath.Join(library, "relay/.hgo.refs.json")
	original, e := os.ReadFile(metaPath)
	if e != nil {
		t.Fatal(e)
	}
	mutate := func(name, want string, change func(map[string]any)) {
		t.Run(name, func(t *testing.T) {
			var v map[string]any
			if e := json.Unmarshal(original, &v); e != nil {
				t.Fatal(e)
			}
			change(v)
			data, _ := json.Marshal(v)
			if e := testdeps.WriteFile(metaPath, data, 0600); e != nil {
				t.Fatal(e)
			}
			defer testdeps.WriteFile(metaPath, original, 0600)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
			if !strings.Contains(out, want) || !strings.Contains(out, "app.hgo:2:") {
				t.Fatal(out)
			}
		})
	}
	mutate("root-content", "package source closure or build configuration changed", func(v map[string]any) { v["Content"] = "stale" })
	mutate("wrong-package", "package path mismatch", func(v map[string]any) { v["Path"] = "other.example/relay" })
	mutate("format-version", "unsupported metadata version", func(v map[string]any) { v["ArtifactVersion"] = 99 })
	mutate("schema-version", "unsupported metadata version", func(v map[string]any) { v["Version"] = 99 })
	mutate("stale-permissions", "marker digest mismatch", func(v map[string]any) { v["Functions"] = map[string]any{} })
	mutate("missing-dependency", "dependency set mismatch", func(v map[string]any) { delete(v, "Dependencies") })
	mutate("missing-file-set", "source set mismatch", func(v map[string]any) { v["Files"] = map[string]any{} })
	mutate("unknown-field", "invalid metadata JSON", func(v map[string]any) { v["Unexpected"] = 1 })
	t.Run("missing-and-plain-go", func(t *testing.T) {
		if e := os.Remove(metaPath); e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(metaPath, original, 0600)
		out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(out, "metadata missing") {
			t.Fatal(out)
		}
		packagesCommand(t, plain, tool, true, "test", "-race", "-count=1", "./...")
	})
	t.Run("stale-content", func(t *testing.T) {
		path := filepath.Join(library, "api/api.go")
		old, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = testdeps.WriteFile(path, append(old, []byte("\n// stale body artifact\n")...), 0600); e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(path, old, 0600)
		out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(out, "Go source content mismatch") || !strings.Contains(out, "dependency library.example/refs/api") {
			t.Fatal(out)
		}
	})
	t.Run("build-selection", func(t *testing.T) {
		out := packagesCommand(t, root, cli, false, "transpile", "--tags", "extra", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(out, "source set mismatch") {
			t.Fatal(out)
		}
	})
	t.Run("dependency-permission-drift", func(t *testing.T) {
		drift := filepath.Join(t.TempDir(), "library")
		packagesCommand(t, root, cli, true, "transpile", "-o", drift, makeLibrary(false, true))
		for _, name := range []string{"api.go", ".hgo.refs.json"} {
			path := filepath.Join(library, "api", name)
			old, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			defer testdeps.WriteFile(path, old, 0600)
			next, e := os.ReadFile(filepath.Join(drift, "api", name))
			if e != nil {
				t.Fatal(e)
			}
			if e = testdeps.WriteFile(path, next, 0600); e != nil {
				t.Fatal(e)
			}
		}
		out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(out, "enhanced dependency marker changed") {
			t.Fatal(out)
		}
	})
	t.Run("named-aggregate-drift", func(t *testing.T) {
		source := makeLibrary(false, false)
		packagesWrite(t, source, "api/api.hgo", strings.Replace(api, "struct{N int}", "struct{N int;Items []int}", 1))
		drift := filepath.Join(t.TempDir(), "out")
		packagesCommand(t, root, cli, true, "transpile", "-o", drift, source)
		var before, after map[string]any
		oldMeta, _ := os.ReadFile(filepath.Join(library, "api/.hgo.refs.json"))
		newMeta, _ := os.ReadFile(filepath.Join(drift, "api/.hgo.refs.json"))
		json.Unmarshal(oldMeta, &before)
		json.Unmarshal(newMeta, &after)
		oldFunctions, _ := json.Marshal(before["Functions"])
		newFunctions, _ := json.Marshal(after["Functions"])
		if string(oldFunctions) != string(newFunctions) {
			t.Fatal("probe must preserve function permissions")
		}
		for _, name := range []string{"api.go", ".hgo.refs.json"} {
			path := filepath.Join(library, "api", name)
			old, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			defer testdeps.WriteFile(path, old, 0600)
			next, e := os.ReadFile(filepath.Join(drift, "api", name))
			if e != nil {
				t.Fatal(e)
			}
			if e = testdeps.WriteFile(path, next, 0600); e != nil {
				t.Fatal(e)
			}
		}
		out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(out, "dependency source closure changed") {
			t.Fatal(out)
		}
	})
	t.Run("native-named-aggregate-drift", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", module("library.example/refs", ""))
		packagesWrite(t, source, "model/model.go", "package model\ntype Cell struct{N int}\nfunc Consume(c Cell){}\n")
		packagesWrite(t, source, "relay/relay.hgo", `package relay
import n "library.example/refs/model"
func Touch(p &n.Cell){v:=*p;n.Consume(v)}
`)
		generated := filepath.Join(t.TempDir(), "library")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "model/model.go", "package model\ntype Cell struct{N int;Items []int}\nfunc Consume(c Cell){c.Items[0]=9}\n")
		app := t.TempDir()
		packagesWrite(t, app, "go.mod", module("consumer.example/app", generated))
		packagesWrite(t, app, "app.hgo", `package app
import(n "library.example/refs/model";r "library.example/refs/relay")
func Run()int{c:=n.Cell{Items:[]int{1}};r.Touch(&c);return c.Items[0]}
`)
		out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), app)
		if !strings.Contains(out, "dependency source closure changed: library.example/refs/model") {
			t.Fatal(out)
		}
	})

}
