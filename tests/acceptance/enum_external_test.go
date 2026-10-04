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

func TestCrossModuleEnumArtifacts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "hgo")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/hgo")
	module := func(path, library string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => %s\n", path, filepath.ToSlash(root))
		if library != "" {
			s += fmt.Sprintf("require enum.example/library v0.0.0\nreplace enum.example/library => %s\n", filepath.ToSlash(library))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", module("enum.example/library", ""))
	packagesWrite(t, src, "model/model.hgo", `package model
#[derive(Clone,Debug,Serialize,Deserialize)]
type hidden struct{N int}
#[derive(Default,Clone,Debug,Serialize,Deserialize)]
pub enum E {
 #[default]
 A,
 Count(int),
 Batch([]int),
 Hidden(hidden),
}
func View(r &int)&int{return r}
func Secret()E{return E::Hidden(hidden{N:9})}
`)
	packagesWrite(t, src, "model/native.go", "//go:build !extra\n\npackage model\nfunc Native()int{return 1}\n")
	packagesWrite(t, src, "model/native_extra.go", "//go:build extra\n\npackage model\nfunc Native()int{return 2}\n")
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "enum.example/library/model"
type Alias=m.E
type New m.E
func Get()Alias{return m.NewECount(6)}
`)

	packagesWrite(t, src, "internal/hidden/hidden.hgo", `package hidden
pub enum E { A(int), }
`)
	packagesWrite(t, src, "api/api.go", `package api
import h "enum.example/library/internal/hidden"
type Public=h.E
func Get()Public{return h.NewEA(8)}
func Read(e Public)int{return e.(h.HgoCase1_E_A).Value}
`)
	packagesWrite(t, src, "other/other.hgo", `package other
pub enum E { A, Count(int), }
`)
	library := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", library, src)
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
				return fmt.Errorf("nondeterministic %s", rel)
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	again := filepath.Join(t.TempDir(), "library")
	packagesCommand(t, root, cli, true, "transpile", "-o", again, src)
	compare(library, again)
	appFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", module("enum.example/app", library))
		packagesWrite(t, dir, "app.hgo", body)
		return dir
	}
	app := appFor(`package app
import(r "enum.example/library/relay";m "enum.example/library/model";"github.com/goxide-lang/serde/json")
func Score(e r.Alias)int{return match e{r.Alias::A=>0,r.Alias::Count(n)=>n,r.Alias::Batch(xs)=>len(xs),r.Alias::Hidden(v)=>v.N,}}
func Run()int{return Score(r.Get())+Score(r.Alias::Count(7))+Score(m.Secret())}
func Rebuild(e r.Alias)r.Alias{return match e{r.Alias::Hidden(v)=>r.Alias::Hidden(v),_=>e,}}
func Decode()(int,error){e,err:=serdejson.Unmarshal([]byte("{\"Count\":4}"),m.DeserializeE);if err!=nil{return 0,err};return Score(e),nil}
func Ordered(e r.Alias,seen *bool)int{return match e{r.Alias::Count(n) if mark(seen)=>n,_=>0,}}
func mark(seen *bool)bool{*seen=true;return true}
func Read()int{n:=3;return *m.View(&n)}
func Cross(e r.Alias)int{return match e{m.E::A=>0,r.Alias::Count(n)=>n,m.E::Batch(xs)=>len(xs),r.Alias::Hidden(v)=>v.N,}}
`)
	packagesWrite(t, app, "other.hgo", `package app
import(renamed "enum.example/library/model";a "enum.example/library/api")
func Facade()int{return a.Read(a.Get())}
func Other()int{return match renamed.E::Count(2){renamed.E::Count(n)=>n,_=>0,}}
`)
	out := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, app)
	out2 := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", out2, app)
	compare(out, out2)
	packagesWrite(t, out, "app_test.go", `package app
import("testing";m "enum.example/library/model";r "enum.example/library/relay")
type forged struct{r.Alias}
func TestBusiness(t *testing.T){if Run()!=22||Other()!=2||Read()!=3||Facade()!=8||Cross(m.NewECount(4))!=4||Score(Rebuild(m.Secret()))!=9{t.Fatal("business")};if n,e:=Decode();e!=nil||n!=4{t.Fatal(n,e)}}
func TestInvalidBeforeGuard(t *testing.T){var typed *m.HgoCase1_E_Count;for _,e:=range []r.Alias{nil,typed,&m.HgoCase1_E_Count{Value:1},forged{}}{func(){seen:=false;defer func(){if recover()==nil||seen{t.Fatal("invalid before guard")}}();Ordered(e,&seen)}()}}
`)
	packagesWrite(t, library, "model/model_test.go", `package model
import "testing"
func TestProducer(t *testing.T){e:=DefaultE();if DebugE(&e)!="E::A"{t.Fatal(DebugE(&e))};_=CloneE(&e)}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", module("enum.example/plain", library))
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";m "enum.example/library/model";r "enum.example/library/relay")
func TestGo(t *testing.T){var e r.Alias=m.NewECount(5);m.ValidateE(e);if e.(m.HgoCase1_E_Count).Value!=5{t.Fatal(e)}}
`)
	for _, dir := range []string{library, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for name, body := range map[string]string{
		"non-exhaustive":  `func Bad(e r.Alias)int{return match e{r.Alias::A=>0,}}`,
		"unknown-variant": `func Bad()r.Alias{return r.Alias::Missing}`,
		"readonly":        `func Bad(r &int){m.View(r);*r=1}`,
	} {
		t.Run(name, func(t *testing.T) {
			src := appFor("package app\nimport(r \"enum.example/library/relay\";m \"enum.example/library/model\")\nvar _=m.Secret\n" + body)
			msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), src)
			if !strings.Contains(msg, ".hgo:") {
				t.Fatal(msg)
			}
		})
	}
	for name, body := range map[string]string{
		"new-defined-not-alias": `import r "enum.example/library/relay"
func Bad()r.New{return r.New::Count(1)}`,
		"distinct-package-identity": `import(r "enum.example/library/relay";o "enum.example/library/other")
func Bad(e r.Alias)int{return match e{r.Alias::A=>0,o.E::Count(n)=>n,_=>0,}}`,
		"internal-origin-pattern": `import a "enum.example/library/api"
func Bad()int{return match a.Get(){a.Public::A(n)=>n,}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := appFor("package app\n" + body)
			msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), source)
			if !strings.Contains(msg, ".hgo:") {
				t.Fatal(msg)
			}
			if name == "internal-origin-pattern" && !strings.Contains(msg, "internal-origin alias") {
				t.Fatal(msg)
			}
		})
	}

	valid := appFor("package app\nimport r \"enum.example/library/relay\"\nfunc Read()int{return match r.Get(){r.Alias::Count(n)=>n,_=>0,}}\n")
	metaPath := filepath.Join(library, "relay/.hgo.enums.json")
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
			msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
			if !strings.Contains(msg, want) || !strings.Contains(msg, "app.hgo:2:") {
				t.Fatal(msg)
			}
		})
	}
	mutate("wrong-package", "package", func(v map[string]any) { v["Metadata"].(map[string]any)["Path"] = "other.example/pkg" })
	mutate("version", "version", func(v map[string]any) { v["ArtifactVersion"] = 99 })
	mutate("variant-set", "marker", func(v map[string]any) {
		m := v["Metadata"].(map[string]any)
		m["Enums"].([]any)[0].(map[string]any)["Variants"] = []any{}
	})
	mutate("root-content", "changed", func(v map[string]any) { v["Content"] = "stale" })
	t.Run("missing-and-native", func(t *testing.T) {
		if e := os.Remove(metaPath); e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(metaPath, original, 0600)
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(msg, "metadata") {
			t.Fatal(msg)
		}
		packagesCommand(t, plain, tool, true, "test", "-race", "-count=1", "./...")
	})
	t.Run("stale-source", func(t *testing.T) {
		path := filepath.Join(library, "model/model.go")
		old, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(path, old, 0600)
		if e = testdeps.WriteFile(path, append(old, []byte("\n// changed source\n")...), 0600); e != nil {
			t.Fatal(e)
		}
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(msg, "mismatch") && !strings.Contains(msg, "changed") {
			t.Fatal(msg)
		}
	})
	t.Run("build-selection", func(t *testing.T) {
		msg := packagesCommand(t, root, cli, false, "transpile", "-tags", "extra", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(msg, "mismatch") && !strings.Contains(msg, "changed") {
			t.Fatal(msg)
		}
	})
	t.Run("regenerated-payload-drift", func(t *testing.T) {
		sourcePath := filepath.Join(src, "model/model.hgo")
		oldSource, e := os.ReadFile(sourcePath)
		if e != nil {
			t.Fatal(e)
		}
		defer testdeps.WriteFile(sourcePath, oldSource, 0600)
		changed := strings.Replace(string(oldSource), "struct{N int}", "struct{N int;Extra []int}", 1)
		if changed == string(oldSource) {
			t.Fatal("fixture replacement failed")
		}
		if e = testdeps.WriteFile(sourcePath, []byte(changed), 0600); e != nil {
			t.Fatal(e)
		}
		updated := filepath.Join(t.TempDir(), "library")
		packagesCommand(t, root, cli, true, "transpile", "-o", updated, src)
		var before, after map[string]any
		prior, e := os.ReadFile(filepath.Join(library, "model/.hgo.enums.json"))
		if e != nil {
			t.Fatal(e)
		}
		next, e := os.ReadFile(filepath.Join(updated, "model/.hgo.enums.json"))
		if e != nil {
			t.Fatal(e)
		}
		if json.Unmarshal(prior, &before) != nil || json.Unmarshal(next, &after) != nil {
			t.Fatal("metadata JSON")
		}
		x, _ := json.Marshal(before["Metadata"])
		y, _ := json.Marshal(after["Metadata"])
		if string(x) != string(y) {
			t.Fatal("probe must keep logical variant metadata unchanged")
		}
		for _, name := range []string{"model.go", ".hgo.enums.json", ".hgo.refs.json"} {
			path := filepath.Join(library, "model", name)
			old, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			defer testdeps.WriteFile(path, old, 0600)
			data, e := os.ReadFile(filepath.Join(updated, "model", name))
			if e != nil {
				t.Fatal(e)
			}
			if e = testdeps.WriteFile(path, data, 0600); e != nil {
				t.Fatal(e)
			}
		}
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), valid)
		if !strings.Contains(msg, "dependency source closure changed") {
			t.Fatal(msg)
		}
	})

}
