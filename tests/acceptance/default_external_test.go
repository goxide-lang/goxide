package acceptance_test

import (
	"fmt"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExternalDefaultModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require defaults.example/lib v0.0.0\nreplace defaults.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("defaults.example/lib", ""))
	packagesWrite(t, src, "model/types.gox", `package model
type Manual struct{value int}
var Calls int
func Value(v Manual)int{return v.value}
#[derive(Default)]
pub enum E {
 #[default]
 Ready(Manual),
 Other([]int),
}
func Read(v E)int{return match v {E::Ready(n)=>Value(n),E::Other(_)=>-1,}}
`)
	packagesWrite(t, src, "model/impl.gox", `package model
impl Default for Manual {func default()Manual{Calls++;return Manual{value:7}}}
`)
	packagesWrite(t, src, "other/model.gox", `package other
#[derive(Default)]
type Manual struct{N int}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "defaults.example/lib/model"
type E=m.E
type Manual=m.Manual
type New m.Manual
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
type Value struct{N int}
func DefaultValue()Value{return Value{N:99}}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("defaults.example/app", lib))
	packagesWrite(t, appsrc, "app.gox", `package app
import(out "defaults.example/lib/relay";m "defaults.example/lib/model")
#[derive(Default)]
type Wrap struct{V out.E;M m.Manual}

`)
	packagesWrite(t, appsrc, "other.gox", `package app
import m "defaults.example/lib/other"
#[derive(Default)]
type Other struct{V m.Manual}
`)
	packagesWrite(t, appsrc, "ops/macros.gox", `package ops
pub macro_rules! size {($x:expr)=>{len($x)};}
`)
	packagesWrite(t, appsrc, "builtin.gox", `package app
import len "defaults.example/lib/model"
import ops "defaults.example/app/ops"
#[derive(Default)]
type Both struct{M len.Manual}
func Count()int{return ops.size!("abc")}
`)
	packagesWrite(t, appsrc, "skip.gox", `package app
import m "defaults.example/lib/model"
#[derive(Deserialize)]
type Skipped struct {
 #[serde(skip)]
 V m.Manual
}
`)
	out := filepath.Join(t.TempDir(), "out")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, appsrc)
	repeat := filepath.Join(t.TempDir(), "repeat")
	packagesCommand(t, root, cli, true, "transpile", "-o", repeat, appsrc)
	err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(out, path)
		a, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(repeat, rel))
		if e != nil {
			return e
		}
		if string(a) != string(b) {
			return fmt.Errorf("nondeterministic %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, out, "app_test.go", `package app
import("testing";m "defaults.example/lib/model";"github.com/goxide-lang/serde/json")
func TestExternalDefaults(t *testing.T){m.Calls=0;v:=DefaultWrap();if m.Read(v.V)!=7||m.Value(v.M)!=7||m.Calls!=2{t.Fatal(v,m.Calls)};if DefaultOther().V.N!=0{t.Fatal("cross-file alias bound to wrong package")};s,e:=serdejson.Unmarshal([]byte("{}"),DeserializeSkipped);if e!=nil||m.Value(s.V)!=7||m.Calls!=3{t.Fatal(s,e,m.Calls)};b:=DefaultBoth();if m.Value(b.M)!=7||Count()!=3||m.Calls!=4{t.Fatal(b,m.Calls)}}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("defaults.example/plain", lib)+fmt.Sprintf("require defaults.example/app v0.0.0\nreplace defaults.example/app => %s\n", filepath.ToSlash(out)))
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";a "defaults.example/app";m "defaults.example/lib/model")
func TestGoConsumer(t *testing.T){v:=a.DefaultWrap();if m.Read(v.V)!=7||m.Value(v.M)!=7{t.Fatal(v)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, tc := range []struct{ path, typ string }{{"fake", "Value"}, {"relay", "New"}} {
		bad := t.TempDir()
		packagesWrite(t, bad, "go.mod", mod("defaults.example/bad", lib))
		packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad\nimport m %q\n#[derive(Default)]\ntype Wrap struct{V m.%s}\n", "defaults.example/lib/"+tc.path, tc.typ))
		log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), bad)
		if !strings.Contains(log, "Default") {
			t.Fatal(log)
		}
	}
	artifact := filepath.Join(lib, "model", ".goxide.enums.json")
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err = testdeps.WriteFile(artifact, []byte(strings.Replace(string(data), `"Source": "manual"`, `"Source": "fake"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "tampered"), appsrc)
	if !strings.Contains(log, "metadata") {
		t.Fatal(log)
	}
}
