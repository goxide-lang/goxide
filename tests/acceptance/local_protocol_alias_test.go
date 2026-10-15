package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalProtocolAliasModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require aliases.example/lib v0.0.0\nreplace aliases.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("aliases.example/lib", ""))
	writeAcceptanceSerdeRoots(t, src, "")
	packagesWrite(t, src, "model/item.gox", `package model; use serde;
#[derive(Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Item struct{N int;Name string}
impl Default for Item{func default()Item{return Item{N:7,Name:"producer"}}}
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
pub enum E{
#[default]
Entry(Item),
Empty,
}
`)
	packagesWrite(t, src, "other/item.gox", `package different; use serde;
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Item struct{Text string}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "aliases.example/lib/model"
type Item=m.Item
type E=m.E
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
type Item struct{N int;Name string}
func DefaultItem()Item{return Item{N:99}}
func CloneItem(v *Item)Item{return *v}
func DebugItem(v *Item)string{return "fake"}
func EqualsItem(a,b Item)bool{return true}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("aliases.example/app", lib))
	writeAcceptanceSerdeRoots(t, appsrc, "aliases.example/lib", "model", "other", "relay", "fake")
	// Both canonical imports exist only in native Go: no goxide selector is available.
	packagesWrite(t, appsrc, "aliases.go", `package app
import m "aliases.example/lib/relay"
import again "aliases.example/lib/relay"
type Native=m.Item
type NativeE=m.E
type Duplicate=again.Item
`)
	packagesWrite(t, appsrc, "chain.gox", `package app; use serde;
type Local=Native
type Level=Local
type Event=NativeE
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Wrap struct{V Level;E Event;Again Duplicate}
#[derive(Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Bag struct{Items []Level;Lookup map[string]Local}
#[derive(serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Optional struct{Maybe Option[Level]}
func Equal(a,b Wrap)bool{Local:=3;_=Local;return a.equals(b)}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use serde;
use library::other as m;
type Different=m::Item
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Other struct{V Different}
`)
	out := filepath.Join(t.TempDir(), "app")
	compileAcceptanceGraph(t, true, appsrc, out, src)
	repeat := filepath.Join(t.TempDir(), "repeat")
	compileAcceptanceGraph(t, true, appsrc, repeat, src)
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
import("testing";"reflect";"strings";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";sum "github.com/goxide-lang/std/sum";m "aliases.example/lib/model")
func TestAllProtocols(t *testing.T){
 v:=DefaultWrap();if v.V.N!=7||v.Again.Name!="producer"{t.Fatal(v)}
 c:=CloneWrap(&v);if !Equal(v,c)||!strings.Contains(DebugWrap(&v),"E::Entry(Item {"){t.Fatal(v,c,DebugWrap(&v))};c.V.N=8;if Equal(v,c){t.Fatal("wrong equality")}
 b,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeWrap(s,&v)});if e!=nil{t.Fatal(e)};d,e:=serdejson.Unmarshal(b,DeserializeWrap);if e!=nil||!reflect.DeepEqual(d,v){t.Fatal(string(b),d,e)}
 if reflect.TypeOf(v.V)!=reflect.TypeOf(m.Item{}){t.Fatal("alias identity changed")}
 bag:=Bag{Items:[]Level{v.V},Lookup:map[string]Local{"x":v.V}};bc:=CloneBag(&bag);bc.Items[0].N=12;bc.Lookup["x"]=Local{N:13};if bag.Items[0].N!=7||bag.Lookup["x"].N!=7{t.Fatal("clone shared containers")}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeBag(s,&bag)});bd,e2:=serdejson.Unmarshal(b,DeserializeBag);if e!=nil||e2!=nil||!EqualsBag(bag,bd){t.Fatal(string(b),e,e2)}
 o:=DefaultOther();o.V.Text="other";oc:=CloneOther(&o);if !EqualsOther(o,oc)||DebugOther(&o)!="Other { V: Item { Text: \"other\" } }"{t.Fatal(o,oc)}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeOther(s,&o)});od,e2:=serdejson.Unmarshal(b,DeserializeOther);if e!=nil||e2!=nil||od.V.Text!="other"{t.Fatal(string(b),e,e2)}
 opt,e:=serdejson.Unmarshal([]byte("{}"),DeserializeOptional);if e!=nil||sum.TagOption(opt.Maybe)!=0{t.Fatal(opt,e)}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeOptional(s,&opt)});opt2,e2:=serdejson.Unmarshal(b,DeserializeOptional);if e!=nil||e2!=nil||!EqualsOptional(opt,opt2){t.Fatal(string(b),e,e2)}
}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("aliases.example/plain", lib)+fmt.Sprintf("require aliases.example/app v0.0.0\nreplace aliases.example/app => %s\n", filepath.ToSlash(out)))
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";a "aliases.example/app")
func TestPublicGoHelpers(t *testing.T){v:=a.DefaultWrap();c:=a.CloneWrap(&v);if !a.EqualsWrap(v,c)||v.V.N!=7||a.DebugWrap(&v)==""{t.Fatal(v,c)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, kind := range []string{"Default", "Clone", "Debug", "Serialize", "Deserialize", "PartialEq", "Eq"} {
		if kind == "Eq" {
			kind = "PartialEq,Eq"
		}
		derive, ownerUse := kind, ""
		if kind == "Serialize" || kind == "Deserialize" {
			derive, ownerUse = "serde::"+kind, "use serde;"
		}
		for _, definition := range []string{"type Local m::Item", "type Fresh m::Item;type Local=Fresh"} {
			bad := t.TempDir()
			packagesWrite(t, bad, "go.mod", mod("aliases.example/bad", lib))
			writeAcceptanceSerdeRoots(t, bad, "aliases.example/lib", "model", "other", "relay", "fake")
			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; %s\nuse library::model as m;\n%s\n#[derive(%s)]\ntype W struct{V Local}\n", ownerUse, definition, derive))
			log := compileAcceptanceGraph(t, false, bad, filepath.Join(t.TempDir(), "bad"), src)
			if !strings.Contains(log, "bad.gox") && !strings.Contains(log, "requires an explicit ") {
				t.Fatal(log)
			}
		}
	}
	shadow := t.TempDir()
	packagesWrite(t, shadow, "go.mod", mod("aliases.example/shadow", lib))
	writeAcceptanceSerdeRoots(t, shadow, "aliases.example/lib", "model")
	packagesWrite(t, shadow, "shadow.gox", `package shadow
use library::model as m;
type Local=m::Item
func Compare()bool{type Local struct{N int};a:=Local{N:1};return a.equals(a)}
`)
	logShadow := compileAcceptanceGraph(t, false, shadow, filepath.Join(t.TempDir(), "shadow"), src)
	if !strings.Contains(logShadow, "shadow.gox:4:") || !strings.Contains(logShadow, "equals requires a primitive or an explicitly registered PartialEq type") {
		t.Fatal(logShadow)
	}
	// Matching shape/helper spellings in another package do not convey registration.
	bad := t.TempDir()
	packagesWrite(t, bad, "go.mod", mod("aliases.example/bad", lib))
	writeAcceptanceSerdeRoots(t, bad, "aliases.example/lib", "model", "other", "relay", "fake")
	packagesWrite(t, bad, "bad.gox", "package bad\nuse library::fake as m;\ntype Local=m::Item\n#[derive(Clone)]\ntype W struct{V Local}\n")
	compileAcceptanceGraph(t, false, bad, filepath.Join(t.TempDir(), "fake"), src)
}
