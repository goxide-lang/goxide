package acceptance_test

import (
	"fmt"

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
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	src := formalDependencyDir(t)
	writeStdFormalManifest(t, src, "fields.example/lib", "library", nil)

	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq;\npub mod model;\npub mod other;\npub mod relay;\npub mod fake;\n")
	packagesWrite(t, src, "model/item.gox", `package model; use std; use std::PartialEq;
#[derive(std::derive::Clone,std::derive::Debug)]
type Item struct{Name string;secret string;Values []int;Index map[string][]int}
func Make(name string)Item{return Item{Name:name,secret:"token\n",Values:[]int{1},Index:map[string][]int{"z":[]int{2}}}}

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, src, "model/event.gox", `package model; use std; use std::PartialEq;
#[derive(std::derive::Clone,std::derive::Debug)]
pub enum E{Empty,Entry(Item),}
func Event(v Item)E{return E::Entry(v)}
func Data(v E)Item{return match v{E::Entry(n)=>n,E::Empty=>Item{},}}

impl E {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, src, "other/item.gox", `package different; use std; use std::PartialEq;
#[derive(std::derive::Clone,std::derive::Debug)]
type Item struct{N int}

impl Item {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
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
	lib = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", lib, src))
	appsrc := t.TempDir()
	writeStdFormalManifest(t, appsrc, "fields.example/app", "app", map[string]string{"library": src})

	packagesWrite(t, appsrc, "app.gox", `package app; use std; use std::PartialEq;
use library::relay as src;
#[derive(std::derive::Clone,std::derive::Debug)]
type Wrap struct{V src::E;Items []src::Renamed;Index map[string]src::Renamed;Pair [2]src::Renamed;Nested [][]src::Renamed}
func Assign(v Wrap)Wrap{return v}

impl Wrap {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use std; use std::PartialEq;
use library::other as src;
#[derive(std::derive::Clone,std::derive::Debug)]
type Other struct{Items []src::Item}

impl Other {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
}
`)
	out := filepath.Join(t.TempDir(), "app")
	compileStdFormalGraph(t, true, appsrc, out)
	repeat := filepath.Join(t.TempDir(), "repeat")
	compileStdFormalGraph(t, true, appsrc, repeat)
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
 c:=(WrapOps{}).Copy(&v);m.Data(c.V).Values[0]=9;c.Items[0].Values[0]=8;c.Index["z\""].Index["z"][0]=7;c.Pair[0].Values[0]=6;c.Nested[0][0].Values[0]=5
 if m.Data(v.V).Values[0]!=1||v.Items[0].Values[0]!=1||v.Index["z\""].Index["z"][0]!=2||v.Pair[0].Values[0]!=1||v.Nested[0][0].Values[0]!=1{t.Fatal("producer/container Clone not applied")}
 if c.Pair[1].Values[0]!=1{t.Fatal("Clone unexpectedly preserved alias topology")}
 text:=(WrapOps{}).Describe(&v)
 for _,part:=range []string{"V: E::Entry(Item {","Name: "+strconv.Quote("a\"b"),"secret: "+strconv.Quote("token\n"),strconv.Quote("z\"")+": Item {"}{if !strings.Contains(text,part){t.Fatalf("missing %q in %s",part,text)}}
 z:=(WrapOps{}).Copy(&Wrap{V:m.Event(m.Item{})});if z.Items!=nil||z.Index!=nil||z.Nested!=nil{t.Fatal("nil containers changed")}
 e:=(WrapOps{}).Copy(&Wrap{V:m.Event(m.Item{}),Items:[]r.Renamed{},Index:map[string]r.Renamed{}});if e.Items==nil||e.Index==nil{t.Fatal("empty collapsed to nil")}
 o:=Other{Items:[]other.Item{{N:9}}};oc:=(OtherOps{}).Copy(&o);oc.Items[0].N=7;if o.Items[0].N!=9||(OtherOps{}).Describe(&o)!="Other { Items: [Item { N: 9 }] }"{t.Fatal("cross-file package identity")}
}
func TestInvalidEnumStillRejected(t *testing.T){for _,f:=range []func(){func(){_=(WrapOps{}).Copy(&Wrap{})},func(){_=(WrapOps{}).Describe(&Wrap{})}}{func(){defer func(){if recover()==nil{t.Fatal("invalid enum accepted")}}();f()}()}}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "fields.example/plain", out)
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";"strings";a "fields.example/app";m "fields.example/lib/model")
func TestGoHelpers(t *testing.T){v:=a.Wrap{V:m.Event(m.Make("go"))};c:=(a.WrapOps{}).Copy(&v);m.Data(c.V).Values[0]=8;if m.Data(v.V).Values[0]!=1||!strings.Contains((a.WrapOps{}).Describe(&v),"Name: \"go\""){t.Fatal("Go helper consumption")}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, kind := range []string{"Clone", "Debug"} {
		for _, tc := range []struct{ pkg, typ string }{{"fake", "Item"}, {"relay", "New"}} {
			bad := t.TempDir()
			writeStdFormalManifest(t, bad, "fields.example/bad", "bad", map[string]string{"library": src})

			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; use std; use std::PartialEq;\nuse library::%s as m;\n#[derive(std::derive::%s)]\ntype Wrap struct{V m::%s}\n", tc.pkg, kind, tc.typ))
			log := compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "bad"))
			if !strings.Contains(log, kind) || !strings.Contains(log, "bad.gox:") {
				t.Fatal(log)
			}
		}
	}
}
