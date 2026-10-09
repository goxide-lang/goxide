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
	packagesWrite(t, src, "model/item.gox", `package model
#[derive(Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Item struct{N int;Name string}
impl Default for Item{func default()Item{return Item{N:7,Name:"producer"}}}
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
pub enum E{
#[default]
Item(Item),
Empty,
}
`)
	packagesWrite(t, src, "other/item.gox", `package different
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
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
	// Both canonical imports exist only in native Go: no goxide selector is available.
	packagesWrite(t, appsrc, "aliases.go", `package app
import m "aliases.example/lib/relay"
import again "aliases.example/lib/relay"
type Native=m.Item
type NativeE=m.E
type Duplicate=again.Item
`)
	packagesWrite(t, appsrc, "chain.gox", `package app
type Local=Native
type Level=Local
type Event=NativeE
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Wrap struct{V Level;E Event;Again Duplicate}
#[derive(Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Bag struct{Items []Level;Lookup map[string]Local}
#[derive(Serialize,Deserialize,PartialEq,Eq)]
type Optional struct{Maybe Option[Level]}
func Equal(a,b Wrap)bool{Local:=3;_=Local;return a.equals(b)}
`)
	packagesWrite(t, appsrc, "other.gox", `package app
import m "aliases.example/lib/other"
type Different=m.Item
#[derive(Default,Clone,Debug,Serialize,Deserialize,PartialEq,Eq)]
type Other struct{V Different}
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
import("testing";"reflect";"strings";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";m "aliases.example/lib/model")
func TestAllProtocols(t *testing.T){
 v:=DefaultWrap();if v.V.N!=7||v.Again.Name!="producer"{t.Fatal(v)}
 c:=CloneWrap(&v);if !Equal(v,c)||!strings.Contains(DebugWrap(&v),"E::Item(Item {"){t.Fatal(v,c,DebugWrap(&v))};c.V.N=8;if Equal(v,c){t.Fatal("wrong equality")}
 b,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeWrap(s,&v)});if e!=nil{t.Fatal(e)};d,e:=serdejson.Unmarshal(b,DeserializeWrap);if e!=nil||!reflect.DeepEqual(d,v){t.Fatal(string(b),d,e)}
 if reflect.TypeOf(v.V)!=reflect.TypeOf(m.Item{}){t.Fatal("alias identity changed")}
 bag:=Bag{Items:[]Level{v.V},Lookup:map[string]Local{"x":v.V}};bc:=CloneBag(&bag);bc.Items[0].N=12;bc.Lookup["x"]=Local{N:13};if bag.Items[0].N!=7||bag.Lookup["x"].N!=7{t.Fatal("clone shared containers")}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeBag(s,&bag)});bd,e2:=serdejson.Unmarshal(b,DeserializeBag);if e!=nil||e2!=nil||!EqualsBag(bag,bd){t.Fatal(string(b),e,e2)}
 o:=DefaultOther();o.V.Text="other";oc:=CloneOther(&o);if !EqualsOther(o,oc)||DebugOther(&o)!="Other { V: Item { Text: \"other\" } }"{t.Fatal(o,oc)}
 b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeOther(s,&o)});od,e2:=serdejson.Unmarshal(b,DeserializeOther);if e!=nil||e2!=nil||od.V.Text!="other"{t.Fatal(string(b),e,e2)}
 opt,e:=serdejson.Unmarshal([]byte("{}"),DeserializeOptional);if e!=nil||opt.Maybe!=nil{t.Fatal(opt,e)}
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
		for _, definition := range []string{"type Local m.Item", "type Fresh m.Item;type Local=Fresh"} {
			bad := t.TempDir()
			packagesWrite(t, bad, "go.mod", mod("aliases.example/bad", lib))
			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad\nimport m \"aliases.example/lib/model\"\n%s\n#[derive(%s)]\ntype W struct{V Local}\n", definition, kind))
			log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), bad)
			if !strings.Contains(log, "bad.gox") && !strings.Contains(log, "requires an explicit ") {
				t.Fatal(log)
			}
		}
	}
	shadow := t.TempDir()
	packagesWrite(t, shadow, "go.mod", mod("aliases.example/shadow", lib))
	packagesWrite(t, shadow, "shadow.gox", `package shadow
import m "aliases.example/lib/model"
type Local=m.Item
func Compare()bool{type Local struct{N int};a:=Local{N:1};return a.equals(a)}
`)
	logShadow := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "shadow"), shadow)
	if !strings.Contains(logShadow, "equals undefined") {
		t.Fatal(logShadow)
	}
	// Matching shape/helper spellings in another package do not convey registration.
	bad := t.TempDir()
	packagesWrite(t, bad, "go.mod", mod("aliases.example/bad", lib))
	packagesWrite(t, bad, "bad.gox", "package bad\nimport m \"aliases.example/lib/fake\"\ntype Local=m.Item\n#[derive(Clone)]\ntype W struct{V Local}\n")
	packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "fake"), bad)
	artifact := filepath.Join(lib, "model", ".goxide.enums.json")
	saved, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(saved), `"Source": "derive"`, `"Source": "unknown"`, 1)
	if changed == string(saved) {
		t.Fatal("no tamper")
	}
	packagesWrite(t, lib, "model/.goxide.enums.json", changed)
	log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "tamper"), appsrc)
	if !strings.Contains(log, "aliases.go:") || !strings.Contains(log, "metadata") {
		t.Fatal(log)
	}
}
