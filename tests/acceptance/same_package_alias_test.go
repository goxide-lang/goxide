package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

const samePackageAliasHeader = "package fixture; use std; use std::PartialEq; use serde;\n"

const samePackageAliasTypes = samePackageAliasHeader + `#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Item struct{N int;Name string}
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
pub enum E{
#[default]
Entry(Item),
Empty,
}
impl std::Default for Item{func Default()Item{return Item{N:7,Name:"local"}}}
#[derive(std::derive::Default)]
type Recursive struct{V Local}

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

impl Recursive {
 pub func initial() Self { return std::default_value[Self]() }
}
`
const samePackageAliasWrappers = samePackageAliasHeader + samePackageAliasWrapperItems

const samePackageAliasWrapperItems = `type Local=Native
type Level=Local
type Event=E
#[derive(std::derive::Default,std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Wrap struct{V Level;E Event}
#[derive(std::derive::Clone,std::derive::Debug,serde::derive::Serialize,serde::derive::Deserialize,std::derive::PartialEq,std::derive::Eq)]
type Bag struct{Items []Level;Index map[string]Local}
func Equal(a,b Wrap)bool{Local:=1;_=Local;return a.equals(b)}
func ShadowDefault()Item{DefaultItem:=func()Item{return Item{N:9}};return DefaultItem()}

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
`
const samePackageAliasAssertions = `package fixture
import("testing";"reflect";"strings";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestSamePackageAliasProtocols(t *testing.T){
 if ShadowDefault().N!=9{t.Fatal("local helper spelling rebound")};v:=(WrapOps{}).Initial();if v.V.N!=7||v.V.Name!="local"{t.Fatal(v)};c:=(WrapOps{}).Copy(&v);if !Equal(v,c)||!strings.Contains((WrapOps{}).Describe(&v),"E::Entry(Item {"){t.Fatal(v,c)}
 c.V.N=9;if Equal(v,c){t.Fatal("equality lost")};if reflect.TypeOf(v.V)!=reflect.TypeOf(Item{}){t.Fatal("identity changed")}
 b,e:=serdejson.Encode(func(s serde.Serializer)error{return (WrapOps{}).Encode(s,&v)});w,e2:=serdejson.Unmarshal(b,(WrapOps{}).Decode);if e!=nil||e2!=nil||!Equal(v,w){t.Fatal(string(b),e,e2)}
 bag:=Bag{Items:[]Level{v.V},Index:map[string]Local{"x":v.V}};bc:=(BagOps{}).Copy(&bag);bc.Items[0].N=100;bc.Index["x"]=Local{};if bag.Items[0].N!=7||bag.Index["x"].N!=7{t.Fatal("alias clone")}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return (BagOps{}).Encode(s,&bag)});bg,e2:=serdejson.Unmarshal(b,(BagOps{}).Decode);if e!=nil||e2!=nil||!(BagOps{}).Equal(bag,bg){t.Fatal(string(b),e,e2)}
}
`

func TestSamePackageAliasModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	src := t.TempDir()
	writeStdFormalManifest(t, src, "local.example/fixture", "fixture", nil)

	packagesWrite(t, src, "types.gox", samePackageAliasTypes)
	packagesWrite(t, src, "wrap.gox", samePackageAliasWrappers)
	packagesWrite(t, src, "alias.go", "package fixture\ntype Native=Item\n")
	out := filepath.Join(t.TempDir(), "out")
	out = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", out, src))
	packagesWrite(t, out, "alias_test.go", samePackageAliasAssertions)
	packagesCommand(t, out, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, out, tool, true, "vet", "./...")
}
func TestSamePackageAliasSingleFileRace(t *testing.T) {
	source := samePackageAliasTypes + "\ntype Native=Item\n" + samePackageAliasWrapperItems
	enumFormalStdModule(t, source, samePackageAliasAssertions, false, true)
}
func TestSamePackageAliasRejections(t *testing.T) {
	for name, tc := range map[string]struct {
		source, location string
		want             []string
	}{
		"new-defined":             {"#[derive(std::derive::Clone)]\ntype Item struct{N int}\ntype Fresh Item\ntype Local=Fresh\n#[derive(std::derive::Clone)]\ntype Wrap struct{V Local}", "6:1:", []string{"missing trait implementation:", "Clone", "(fixture::Fresh)"}},
		"helper-not-registration": {"type Item struct{N int}\nfunc CloneItem(v *Item)Item{return *v}\ntype Local=Item\n#[derive(std::derive::Clone)]\ntype Wrap struct{V Local}", "5:1:", []string{"missing trait implementation:", "Clone", "(fixture::Item)"}},
		"derive-alias":            {"#[derive(std::derive::Clone)]\ntype Item struct{N int}\n#[derive(std::derive::Clone)]\ntype Local=Item", "5:1:", []string{"macro expansion failed: derive requires a defined struct or enum"}},
		"derive-manual-conflict":  {"#[derive(std::derive::Default)]\ntype Item struct{N int}\ntype Local=Item\nimpl std::Default for Item{func Default()Item{return Item{}}}", "5:1:", []string{"duplicate trait implementation: overlapping applications:"}},
		"wrong-direction":         {"#[derive(serde::derive::Serialize)]\ntype Item struct{N int}\ntype Local=Item\n#[derive(serde::derive::Deserialize)]\ntype Wrap struct{V Local}", "5:1:", []string{"type does not explicitly implement Deserialize:", "(fixture::Item)"}},
		"bad-manual-return":       {"type Item struct{N int}\ntype Local=Item\nimpl std::Default for Item{func Default()Item{return 1}}\n#[derive(std::derive::Default)]\ntype Wrap struct{V Local}", "4:54:", []string{"cannot use 1 (untyped int constant) as Item value in return statement"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := testdeps.FormalSource(t, name+".gox", []byte("package fixture; use std; use std::PartialEq; use serde;\n"+tc.source+"\n"))
			if err == nil || !strings.Contains(err.Error(), name+".gox:"+tc.location) {
				t.Fatalf("want protocol rejection at %s, got %v", tc.location, err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("want diagnostic containing %q, got %v", want, err)
				}
			}
		})
	}
}

func TestSamePackageAliasManualImplementation(t *testing.T) {
	enumFormalStdModule(t, `package fixture; use std;
type Item struct{N int}
type Local=Item
impl std::Default for Local{func Default()Self{return Self{N:7}}}
func Make()Item{return std::default_value[Item]()}
`, `package fixture
import "testing"
func TestAliasIdentity(t *testing.T){if Make().N!=7{t.Fatal("alias implementation lost target identity")}}
`, false)
}
