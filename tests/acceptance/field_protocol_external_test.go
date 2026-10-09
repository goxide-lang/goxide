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

func TestExternalCloneDebugModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require fields.example/lib v0.0.0\nreplace fields.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("fields.example/lib", ""))
	writeAcceptanceSerdeRoots(t, src, "")
	packagesWrite(t, src, "model/item.gox", `package model
#[derive(Clone,Debug)]
type Item struct{Name string;secret string;Values []int;Index map[string][]int}
func Make(name string)Item{return Item{Name:name,secret:"token\n",Values:[]int{1},Index:map[string][]int{"z":[]int{2}}}}
`)
	packagesWrite(t, src, "model/event.gox", `package model
#[derive(Clone,Debug)]
pub enum E{Empty,Item(Item),}
func Event(v Item)E{return E::Item(v)}
func Data(v E)Item{return match v{E::Item(n)=>n,E::Empty=>Item{},}}
`)
	packagesWrite(t, src, "other/item.gox", `package different
#[derive(Clone,Debug)]
type Item struct{N int}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "fields.example/lib/model"
type Renamed=m.Item
type E=m.E
type New m.Item
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
type Item struct{N int}
func CloneItem(v *Item)Item{return *v}
func DebugItem(v *Item)string{return "fake"}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("fields.example/app", lib))
	writeAcceptanceSerdeRoots(t, appsrc, "fields.example/lib", "model", "relay", "other", "fake")
	packagesWrite(t, appsrc, "app.gox", `package app
use library::relay as src;
#[derive(Clone,Debug)]
type Wrap struct{V src::E;Items []src::Renamed;Index map[string]src::Renamed;Pair [2]src::Renamed;Nested [][]src::Renamed}
func Assign(v Wrap)Wrap{return v}
`)
	packagesWrite(t, appsrc, "other.gox", `package app
use library::other as src;
#[derive(Clone,Debug)]
type Other struct{Items []src::Item}
`)
	out := filepath.Join(t.TempDir(), "app")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, appsrc)
	repeat := filepath.Join(t.TempDir(), "repeat")
	packagesCommand(t, root, cli, true, "transpile", "-o", repeat, appsrc)
	if err := filepath.WalkDir(out, func(path string, d os.DirEntry, err error) error {
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
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, out, "app_test.go", `package app
import("testing";"strings";"strconv";m "fields.example/lib/model";r "fields.example/lib/relay";other "fields.example/lib/other")
func TestCloneContainersAndOrdinaryAssignment(t *testing.T){
 item:=m.Make("a\"b");v:=Wrap{V:m.Event(item),Items:[]r.Renamed{item},Index:map[string]r.Renamed{"z\"":item},Pair:[2]r.Renamed{item,item},Nested:[][]r.Renamed{[]r.Renamed{item}}}
 ordinary:=Assign(v);ordinary.Items[0].Values[0]=3;if v.Items[0].Values[0]!=3{t.Fatal("ordinary Go assignment changed")};v.Items[0].Values[0]=1
 c:=CloneWrap(&v);m.Data(c.V).Values[0]=9;c.Items[0].Values[0]=8;c.Index["z\""].Index["z"][0]=7;c.Pair[0].Values[0]=6;c.Nested[0][0].Values[0]=5
 if m.Data(v.V).Values[0]!=1||v.Items[0].Values[0]!=1||v.Index["z\""].Index["z"][0]!=2||v.Pair[0].Values[0]!=1||v.Nested[0][0].Values[0]!=1{t.Fatal("producer/container Clone not applied")}
 if c.Pair[1].Values[0]!=1{t.Fatal("Clone unexpectedly preserved alias topology")}
 text:=DebugWrap(&v)
 for _,part:=range []string{"V: E::Item(Item {","Name: "+strconv.Quote("a\"b"),"secret: "+strconv.Quote("token\n"),strconv.Quote("z\"")+": Item {"}{if !strings.Contains(text,part){t.Fatalf("missing %q in %s",part,text)}}
 z:=CloneWrap(&Wrap{V:m.Event(m.Item{})});if z.Items!=nil||z.Index!=nil||z.Nested!=nil{t.Fatal("nil containers changed")}
 e:=CloneWrap(&Wrap{V:m.Event(m.Item{}),Items:[]r.Renamed{},Index:map[string]r.Renamed{}});if e.Items==nil||e.Index==nil{t.Fatal("empty collapsed to nil")}
 o:=Other{Items:[]other.Item{{N:9}}};oc:=CloneOther(&o);oc.Items[0].N=7;if o.Items[0].N!=9||DebugOther(&o)!="Other { Items: [Item { N: 9 }] }"{t.Fatal("cross-file package identity")}
}
func TestInvalidEnumStillRejected(t *testing.T){for _,f:=range []func(){func(){_=CloneWrap(&Wrap{})},func(){_=DebugWrap(&Wrap{})}}{func(){defer func(){if recover()==nil{t.Fatal("invalid enum accepted")}}();f()}()}}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("fields.example/plain", lib)+fmt.Sprintf("require fields.example/app v0.0.0\nreplace fields.example/app => %s\n", filepath.ToSlash(out)))
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";"strings";a "fields.example/app";m "fields.example/lib/model")
func TestGoHelpers(t *testing.T){v:=a.Wrap{V:m.Event(m.Make("go"))};c:=a.CloneWrap(&v);m.Data(c.V).Values[0]=8;if m.Data(v.V).Values[0]!=1||!strings.Contains(a.DebugWrap(&v),"Name: \"go\""){t.Fatal("Go helper consumption")}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, kind := range []string{"Clone", "Debug"} {
		for _, tc := range []struct{ pkg, typ string }{{"fake", "Item"}, {"relay", "New"}} {
			bad := t.TempDir()
			packagesWrite(t, bad, "go.mod", mod("fields.example/bad", lib))
			writeAcceptanceSerdeRoots(t, bad, "fields.example/lib", "model", "relay", "other", "fake")
			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad\nuse library::%s as m;\n#[derive(%s)]\ntype Wrap struct{V m::%s}\n", tc.pkg, kind, tc.typ))
			log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), bad)
			if !strings.Contains(log, kind) || !strings.Contains(log, "bad.gox:") {
				t.Fatal(log)
			}
		}
	}
	artifact := filepath.Join(lib, "model", ".goxide.enums.json")
	saved, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{`"Source": "manual"`, `"Source": "unknown"`} {
		data := strings.Replace(string(saved), `"Source": "derive"`, replacement, 1)
		if data == string(saved) {
			t.Fatal("tamper did not change metadata")
		}
		if err = testdeps.WriteFile(artifact, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), appsrc)
		if !strings.Contains(log, "metadata") || !strings.Contains(log, "app.gox:") {
			t.Fatal(log)
		}
	}
	if err = testdeps.WriteFile(artifact, saved, 0600); err != nil {
		t.Fatal(err)
	}
	producer := filepath.Join(lib, "model", "item.go")
	file, err := os.OpenFile(producer, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("\n// changed producer after artifact\n")
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "stale"), appsrc)
	if !strings.Contains(log, "metadata") || !strings.Contains(log, "app.gox:") {
		t.Fatal(log)
	}
}
