package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const samePackageAliasTypes = `package fixture; use serde;
#[derive(Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Item struct{N int;Name string}
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
pub enum E{
#[default]
Item(Item),
Empty,
}
impl Default for Item{func default()Item{return Item{N:7,Name:"local"}}}
#[derive(Default)]
type Recursive struct{V Local}
`
const samePackageAliasWrappers = `package fixture; use serde;
type Local=Native
type Level=Local
type Event=E
#[derive(Default,Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Wrap struct{V Level;E Event}
#[derive(Clone,Debug,serde::Serialize,serde::Deserialize,PartialEq,Eq)]
type Bag struct{Items []Level;Index map[string]Local}
func Equal(a,b Wrap)bool{Local:=1;_=Local;return a.equals(b)}
func ShadowDefault()Item{DefaultItem:=func()Item{return Item{N:9}};return DefaultItem()}
`
const samePackageAliasAssertions = `package fixture
import("testing";"reflect";"strings";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestSamePackageAliasProtocols(t *testing.T){
 if ShadowDefault().N!=9{t.Fatal("local helper spelling rebound")};v:=DefaultWrap();if v.V.N!=7||v.V.Name!="local"{t.Fatal(v)};c:=CloneWrap(&v);if !Equal(v,c)||!strings.Contains(DebugWrap(&v),"E::Item(Item {"){t.Fatal(v,c)}
 c.V.N=9;if Equal(v,c){t.Fatal("equality lost")};if reflect.TypeOf(v.V)!=reflect.TypeOf(Item{}){t.Fatal("identity changed")}
 b,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeWrap(s,&v)});w,e2:=serdejson.Unmarshal(b,DeserializeWrap);if e!=nil||e2!=nil||!Equal(v,w){t.Fatal(string(b),e,e2)}
 bag:=Bag{Items:[]Level{v.V},Index:map[string]Local{"x":v.V}};bc:=CloneBag(&bag);bc.Items[0].N=100;bc.Index["x"]=Local{};if bag.Items[0].N!=7||bag.Index["x"].N!=7{t.Fatal("alias clone")}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeBag(s,&bag)});bg,e2:=serdejson.Unmarshal(b,DeserializeBag);if e!=nil||e2!=nil||!EqualsBag(bag,bg){t.Fatal(string(b),e,e2)}
}
`

func TestSamePackageAliasModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", "module local.example/fixture\ngo 1.27\n")
	writeAcceptanceSerdeRoots(t, src, "")
	packagesWrite(t, src, "types.gox", samePackageAliasTypes)
	packagesWrite(t, src, "wrap.gox", samePackageAliasWrappers)
	packagesWrite(t, src, "alias.go", "package fixture\ntype Native=Item\n")
	out := filepath.Join(t.TempDir(), "out")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, src)
	packagesWrite(t, out, "alias_test.go", samePackageAliasAssertions)
	packagesCommand(t, out, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, out, tool, true, "vet", "./...")
}
func TestSamePackageAliasSingleFileRace(t *testing.T) {
	source := strings.Replace(samePackageAliasTypes, "pub enum", "enum", 1) + "\ntype Native=Item\n" + strings.TrimPrefix(samePackageAliasWrappers, "package fixture; use serde;\n")
	enumGeneratedModule(t, source, samePackageAliasAssertions, false, true)
}
func TestSamePackageAliasRejections(t *testing.T) {
	for name, body := range map[string]string{
		"new-defined":             "#[derive(Clone)]\ntype Item struct{N int}\ntype Fresh Item\ntype Local=Fresh\n#[derive(Clone)]\ntype Wrap struct{V Local}",
		"helper-not-registration": "type Item struct{N int}\nfunc CloneItem(v *Item)Item{return *v}\ntype Local=Item\n#[derive(Clone)]\ntype Wrap struct{V Local}",
		"derive-alias":            "#[derive(Clone)]\ntype Item struct{N int}\n#[derive(Clone)]\ntype Local=Item",
		"manual-alias":            "type Item struct{N int}\ntype Local=Item\nimpl Default for Local{func default()Local{return Local{}}}",
		"derive-manual-conflict":  "#[derive(Default)]\ntype Item struct{N int}\ntype Local=Item\nimpl Default for Item{func default()Item{return Item{}}}",
		"wrong-direction":         "#[derive(serde::Serialize)]\ntype Item struct{N int}\ntype Local=Item\n#[derive(serde::Deserialize)]\ntype Wrap struct{V Local}",
		"bad-manual-return":       "type Item struct{N int}\ntype Local=Item\nimpl Default for Item{func default()Item{return 1}}\n#[derive(Default)]\ntype Wrap struct{V Local}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceSerdeFixture(name+".gox", []byte("package fixture; use serde;\n"+body+"\n"))
			if err == nil || !strings.Contains(err.Error(), name+".gox") {
				t.Fatal(err)
			}
		})
	}
}
