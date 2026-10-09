package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExternalSerdeModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		s := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			s += fmt.Sprintf("require wire.example/lib v0.0.0\nreplace wire.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return s
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("wire.example/lib", ""))
	writeAcceptanceSerdeRoots(t, src, "")
	packagesWrite(t, src, "model/item.gox", `package model; use serde;
#[derive(serde::Serialize,serde::Deserialize)]
#[serde(deny_unknown_fields)]
type Item struct{N int;Name string}
#[derive(serde::Serialize)]
type Output struct{N int}
#[derive(serde::Deserialize)]
type Input struct{N int}
`)
	packagesWrite(t, src, "model/event.gox", `package model; use serde;
#[derive(serde::Serialize,serde::Deserialize)]
pub enum E{Empty,Item(Item),}
func Event(n int)E{return E::Item(Item{N:n,Name:"business"})}
`)
	packagesWrite(t, src, "other/item.gox", `package different; use serde;
#[derive(serde::Serialize,serde::Deserialize)]
type Item struct{Text string}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "wire.example/lib/model"
type Item=m.Item
type E=m.E
type New m.Item
`)
	packagesWrite(t, src, "fake/fake.go", `package fake
import "github.com/goxide-lang/serde"
type Item struct{N int}
func SerializeItem(s serde.Serializer,v *Item)error{return s.Int64(int64(v.N))}
func DeserializeItem(d serde.Deserializer)(Item,error){n,e:=serde.ReadInt64(d);return Item{N:int(n)},e}
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("wire.example/app", lib))
	writeAcceptanceSerdeRoots(t, appsrc, "wire.example/lib", "model", "other", "relay", "fake")
	packagesWrite(t, appsrc, "app.gox", `package app; use serde;
use library::relay as v;
#[derive(serde::Serialize,serde::Deserialize)]
type Wrap struct{V v::E;Items []v::Item;Index map[string]v::Item;Pair [1]v::Item;Maybe *v::Item;Opt Option[v::Item]}
#[derive(serde::Serialize,serde::Deserialize)]
type Nested struct{Items [][]v::E}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use serde;
use library::other as v;
#[derive(serde::Serialize,serde::Deserialize)]
type Other struct{Items []v::Item}
`)
	packagesWrite(t, appsrc, "directions.gox", `package app; use serde;
use library::model as m;
#[derive(serde::Serialize)]
type Out struct{V m::Output}
#[derive(serde::Deserialize)]
type In struct{V m::Input}
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
		if strings.HasSuffix(path, ".go") && (strings.Contains(string(a), `"reflect"`) || strings.Contains(string(a), `"encoding/json"`)) {
			return fmt.Errorf("reflection/JSON fallback in %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, out, "app_test.go", externalSerdeAssertions)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("wire.example/plain", lib)+fmt.Sprintf("require wire.example/app v0.0.0\nreplace wire.example/app => %s\n", filepath.ToSlash(out)))
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";a "wire.example/app";m "wire.example/lib/model")
func TestGoHelpers(t *testing.T){v:=a.Out{V:m.Output{N:9}};b,e:=serdejson.Encode(func(s serde.Serializer)error{return a.SerializeOut(s,&v)});if e!=nil||string(b)!="{\"V\":{\"N\":9}}"{t.Fatal(string(b),e)};x,e:=serdejson.Unmarshal(b,a.DeserializeIn);if e!=nil||x.V.N!=9{t.Fatal(x,e)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, tc := range []struct{ kind, pkg, typ string }{{"Serialize", "fake", "Item"}, {"Deserialize", "fake", "Item"}, {"Serialize", "relay", "New"}, {"Deserialize", "relay", "New"}, {"Serialize", "model", "Input"}, {"Deserialize", "model", "Output"}} {
		bad := t.TempDir()
		packagesWrite(t, bad, "go.mod", mod("wire.example/bad", lib))
		writeAcceptanceSerdeRoots(t, bad, "wire.example/lib", "model", "other", "relay", "fake")
		packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; use serde;\nuse library::%s as m;\n#[derive(serde::%s)]\ntype Wrap struct{V m::%s}\n", tc.pkg, tc.kind, tc.typ))
		log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "bad"), bad)
		if !strings.Contains(log, "registered serde protocol") || !strings.Contains(log, "bad.gox:") {
			t.Fatal(log)
		}
	}
	artifact := filepath.Join(lib, "model", ".goxide.enums.json")
	saved, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(saved), `"Source": "derive"`, `"Source": "manual"`, 1)
	if changed == string(saved) {
		t.Fatal("tamper unchanged")
	}
	packagesWrite(t, lib, "model/.goxide.enums.json", changed)
	log := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "tamper"), appsrc)
	if !strings.Contains(log, ".gox:") || !strings.Contains(log, "metadata") {
		t.Fatal(log)
	}
	packagesWrite(t, lib, "model/.goxide.enums.json", string(saved))
	packagesWrite(t, lib, "model/stale.go", "package model\nvar Stale int\n")
	log = packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "stale"), appsrc)
	if !strings.Contains(log, ".gox:") || !strings.Contains(log, "metadata") {
		t.Fatal(log)
	}
}

const externalSerdeAssertions = `package app
import __borrow_std "github.com/goxide-lang/std/sum"
import("testing";"errors";"reflect";"strconv";"strings";"encoding/json";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";m "wire.example/lib/model";other "wire.example/lib/other")
const valid="{\"V\":{\"Item\":{\"N\":7,\"Name\":\"business\"}},\"Items\":[],\"Index\":{},\"Pair\":[{\"N\":1,\"Name\":\"pair\"}],\"Maybe\":null}"
func enc(v Wrap)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeWrap(s,&v)})}
func detail(t *testing.T,e error,kind string,path []serde.PathSegment){t.Helper();var d *serde.Error;if !errors.As(e,&d)||d.Kind!=kind||!reflect.DeepEqual(d.Path,path){t.Fatalf("error %v %#v want %s %#v",e,d,kind,path)}}
func TestExternalRoundTripAndOption(t *testing.T){
 v,e:=serdejson.Unmarshal([]byte(valid),DeserializeWrap);if e!=nil||__borrow_std.TagOption(v.Opt)!=0||v.Items==nil||v.Index==nil{t.Fatal(v,e)}
 b,e:=enc(v);if e!=nil{t.Fatal(e)};w,e:=serdejson.Unmarshal(b,DeserializeWrap);if e!=nil||!reflect.DeepEqual(v,w){t.Fatal(string(b),w,e)}
 for _,value:=range []string{"null","{\"N\":3,\"Name\":\"option\"}"}{input:=strings.TrimSuffix(valid,"}")+",\"Opt\":"+value+"}";v,e:=serdejson.Unmarshal([]byte(input),DeserializeWrap);if e!=nil{t.Fatal(e)};b,e:=enc(v);if e!=nil||!strings.Contains(string(b),"\"Opt\":"+value){t.Fatal(string(b),e)}}
 // Existing serde sequence/map projection emits empty for nil, then decodes nonnil empty.
 v.Items=nil;v.Index=nil;b,e=enc(v);if e!=nil||!strings.Contains(string(b),"\"Items\":[]")||!strings.Contains(string(b),"\"Index\":{}"){t.Fatal(string(b),e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"Items\":[]","\"Items\":null",1)),DeserializeWrap);if e==nil{t.Fatal("null slice accepted")}
 x:=Other{Items:[]other.Item{{Text:"different"}}};b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeOther(s,&x)});y,e2:=serdejson.Unmarshal(b,DeserializeOther);if e!=nil||e2!=nil||!reflect.DeepEqual(x,y){t.Fatal(string(b),e,e2)}
 n:=Nested{Items:[][]m.E{{m.Event(2)}}};b,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeNested(s,&n)});nn,e2:=serdejson.Unmarshal(b,DeserializeNested);if e!=nil||e2!=nil||!reflect.DeepEqual(n,nn){t.Fatal(string(b),e,e2)}
}
func TestExternalErrors(t *testing.T){
 _,e:=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"N\":9223372036854775808",1)),DeserializeWrap)
 detail(t,e,serde.Range,[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Item"},{Kind:"Field",Name:"N"}})
 var cause *strconv.NumError;if !errors.As(e,&cause)||!errors.Is(e,strconv.ErrRange){t.Fatal("cause lost",e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"N\":@",1)),DeserializeWrap)
 detail(t,e,serde.Syntax,[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Item"},{Kind:"Field",Name:"N"}})
 var syntax *json.SyntaxError;if !errors.As(e,&syntax)||syntax.Offset<=0{t.Fatal("syntax cause lost",e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"Item\":","\"Unknown\":",1)),DeserializeWrap);detail(t,e,serde.UnknownVariant,[]serde.PathSegment{{Kind:"Field",Name:"V"}})
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"extra\":0,\"N\":7",1)),DeserializeWrap);detail(t,e,"UnknownField",[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Item"},{Kind:"Field",Name:"extra"}})
 _,e=serdejson.Unmarshal([]byte(strings.TrimSuffix(valid,"}")+",\"extra\":{\"ignored\":[1,2]}}"),DeserializeWrap);if e!=nil{t.Fatal(e)}
 _,e=serdejson.Unmarshal([]byte(strings.TrimSuffix(valid,"}")+",\"extra\":"+strings.Repeat("[",serdejson.MaxDepth+2)+"0"+strings.Repeat("]",serdejson.MaxDepth+2)+"}"),DeserializeWrap);var d *serde.Error;if !errors.As(e,&d)||d.Kind!="Limit"{t.Fatal("skip budget lost",e)}
}
func TestExternalInvalidBoundary(t *testing.T){v,e:=serdejson.Unmarshal([]byte(valid),DeserializeWrap);if e!=nil{t.Fatal(e)};for _,x:=range []m.E{{}}{v.V=x;_,e:=enc(v);detail(t,e,serde.Type,[]serde.PathSegment{{Kind:"Field",Name:"V"}})}}
`
