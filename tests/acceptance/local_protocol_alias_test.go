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
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	src := t.TempDir()
	writeStdFormalManifest(t, src, "aliases.example/lib", "library", nil)

	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq;\npub mod model;\npub mod other;\npub mod relay;\npub mod fake;\n")
	packagesWrite(t, src, "model/item.gox", `package model; use std; use std::PartialEq; use serde;
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Item struct{N int;Name string}
impl std::Default for Item{func r#default()Item{return Item{N:7,Name:"producer"}}}
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
pub enum E{
#[default]
Entry(Item),
Empty,
}

impl Item {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl E {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	packagesWrite(t, src, "other/item.gox", `package different; use std; use std::PartialEq; use serde;
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Item struct{Text string}

impl Item {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
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
	lib = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", lib, src))
	appsrc := t.TempDir()
	writeStdFormalManifest(t, appsrc, "aliases.example/app", "app", map[string]string{"library": src})

	// Both canonical imports exist only in native Go: no goxide selector is available.
	packagesWrite(t, appsrc, "aliases.go", `package app
import m "aliases.example/lib/relay"
import again "aliases.example/lib/relay"
type Native=m.Item
type NativeE=m.E
type Duplicate=again.Item
`)
	packagesWrite(t, appsrc, "chain.gox", `package app; use std; use std::PartialEq; use serde;
type Local=Native
type Level=Local
type Event=NativeE
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Wrap struct{V Level;E Event;Again Duplicate}
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Bag struct{Items []Level;Lookup map[string]Local}
#[derive(serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Optional struct{Maybe Option[Level]}
func Equal(a,b Wrap)bool{Local:=3;_=Local;return a.equals(b)}

impl Wrap {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Bag {
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Optional {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use std; use std::PartialEq; use serde;
use library::other as m;
type Different=m::Item
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Other struct{V Different}

impl Other {
 pub func initial() Self { return std::default_value[Self]() }
 pub func copy(value &Self) Self { return std::Clone::clone(value) }
 pub func describe(value &Self) string { return std::Debug::debug(value) }
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
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
import("testing";"reflect";"strings";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";sum "github.com/goxide-lang/std/sum";m "aliases.example/lib/model")
func TestAllProtocols(t *testing.T){
 v:=(WrapOps{}).Initial();if v.V.N!=7||v.Again.Name!="producer"{t.Fatal(v)}
 c:=(WrapOps{}).Copy(&v);if !Equal(v,c)||!strings.Contains((WrapOps{}).Describe(&v),"E::Entry(Item {"){t.Fatal(v,c,(WrapOps{}).Describe(&v))};c.V.N=8;if Equal(v,c){t.Fatal("wrong equality")}
 b,e:=serdejson.Encode(func(s serde.Serializer)error{return (WrapOps{}).Encode(s,&v)});if e!=nil{t.Fatal(e)};d,e:=serdejson.Unmarshal(b,(WrapOps{}).Decode);if e!=nil||!reflect.DeepEqual(d,v){t.Fatal(string(b),d,e)}
 if reflect.TypeOf(v.V)!=reflect.TypeOf(m.Item{}){t.Fatal("alias identity changed")}
 bag:=Bag{Items:[]Level{v.V},Lookup:map[string]Local{"x":v.V}};bc:=(BagOps{}).Copy(&bag);bc.Items[0].N=12;bc.Lookup["x"]=Local{N:13};if bag.Items[0].N!=7||bag.Lookup["x"].N!=7{t.Fatal("clone shared containers")}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return (BagOps{}).Encode(s,&bag)});bd,e2:=serdejson.Unmarshal(b,(BagOps{}).Decode);if e!=nil||e2!=nil||!(BagOps{}).Equal(bag,bd){t.Fatal(string(b),e,e2)}
 o:=(OtherOps{}).Initial();o.V.Text="other";oc:=(OtherOps{}).Copy(&o);if !(OtherOps{}).Equal(o,oc)||(OtherOps{}).Describe(&o)!="Other { V: Item { Text: \"other\" } }"{t.Fatal(o,oc)}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return (OtherOps{}).Encode(s,&o)});od,e2:=serdejson.Unmarshal(b,(OtherOps{}).Decode);if e!=nil||e2!=nil||od.V.Text!="other"{t.Fatal(string(b),e,e2)}
 opt,e:=serdejson.Unmarshal([]byte("{}"),(OptionalOps{}).Decode);if e!=nil||sum.TagOption(opt.Maybe)!=0{t.Fatal(opt,e)}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return (OptionalOps{}).Encode(s,&opt)});opt2,e2:=serdejson.Unmarshal(b,(OptionalOps{}).Decode);if e!=nil||e2!=nil||!(OptionalOps{}).Equal(opt,opt2){t.Fatal(string(b),e,e2)}
}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "aliases.example/plain", out)
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";a "aliases.example/app")
func TestPublicGoHelpers(t *testing.T){v:=(a.WrapOps{}).Initial();c:=(a.WrapOps{}).Copy(&v);if !(a.WrapOps{}).Equal(v,c)||v.V.N!=7||(a.WrapOps{}).Describe(&v)==""{t.Fatal(v,c)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, kind := range []string{"Default", "Clone", "Debug", "Serialize", "Deserialize", "PartialEq", "Eq"} {
		if kind == "Eq" {
			kind = "PartialEq,Eq"
		}
		derive, ownerUse := "std::derive::"+strings.ReplaceAll(kind, ",", ",std::derive::"), ""
		want := "missing trait implementation:"
		if kind == "Serialize" || kind == "Deserialize" {
			derive, ownerUse = "serde::derive::"+kind, "use serde;"
			want = "does not explicitly implement " + kind
		}
		for _, definition := range []string{"type Local m::Item", "type Fresh m::Item;type Local=Fresh"} {
			bad := t.TempDir()
			writeStdFormalManifest(t, bad, "aliases.example/bad", "bad", map[string]string{"library": src})

			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; use std; use std::PartialEq; %s\nuse library::model as m;\n%s\n#[derive(%s)]\ntype W struct{V Local}\n", ownerUse, definition, derive))
			log := compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "bad"))
			if !strings.Contains(log, "bad.gox") || !strings.Contains(log, want) {
				t.Fatal(log)
			}
		}
	}
	shadow := t.TempDir()
	writeStdFormalManifest(t, shadow, "aliases.example/shadow", "shadow", map[string]string{"library": src})

	packagesWrite(t, shadow, "shadow.gox", `package shadow; use std; use std::PartialEq;
use library::model as m;
type Local=m::Item
func Compare()bool{type Local struct{N int};a:=Local{N:1};return a.equals(a)}
`)
	logShadow := compileStdFormalGraph(t, false, shadow, filepath.Join(t.TempDir(), "shadow"))
	if !strings.Contains(logShadow, "shadow.gox:4:") || !strings.Contains(logShadow, "no explicit implementation for selected trait application") {
		t.Fatal(logShadow)
	}
	// Matching shape and helper spellings do not provide a trait implementation.
	bad := t.TempDir()
	writeStdFormalManifest(t, bad, "aliases.example/bad", "bad", map[string]string{"library": src})

	packagesWrite(t, bad, "bad.gox", "package bad; use std; use std::PartialEq;\nuse library::fake as m;\ntype Local=m::Item\n#[derive(std::derive::Clone)]\ntype W struct{V Local}\n\nimpl W {\n pub func copy(value &Self) Self { return std::Clone::clone(value) }\n}\n")
	compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "fake"))
}
