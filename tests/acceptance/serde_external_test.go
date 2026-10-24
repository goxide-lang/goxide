package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestExternalSerdeModules(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	src := t.TempDir()
	packagesWrite(t, src, "goxide.toml", testdeps.FormalManifest(t, "wire.example/lib", "library"))
	packagesWrite(t, src, "lib.gox", "package library; pub mod model; pub mod other; pub mod relay; pub mod fake;\n")
	packagesWrite(t, src, "model/item.gox", `package model; use serde;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
#[serde(deny_unknown_fields)]
type Item struct{N int;Name string}
#[derive(serde::derive::Serialize)]
type Output struct{N int}
#[derive(serde::derive::Deserialize)]
type Input struct{N int}
`)
	packagesWrite(t, src, "model/event.gox", `package model; use serde;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
pub enum E{Empty,Entry(Item),}
func Event(n int)E{return E::Entry(Item{N:n,Name:"business"})}
`)
	packagesWrite(t, src, "other/item.gox", `package different; use serde;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
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
	appsrc := t.TempDir()
	dependency := fmt.Sprintf("[dependencies.library]\npath=%q\n", filepath.ToSlash(src))
	packagesWrite(t, appsrc, "goxide.toml", testdeps.FormalManifest(t, "wire.example/app", "app")+dependency)
	packagesWrite(t, appsrc, "app.gox", `package app; use serde;
use library::relay as v;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Wrap struct{V v::E;Items []v::Item;Index map[string]v::Item;Pair [1]v::Item;Maybe *v::Item;Opt Option[v::Item]}
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Nested struct{Items [][]v::E}

impl Wrap {
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}

impl Nested {
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	packagesWrite(t, appsrc, "other.gox", `package app; use serde;
use library::other as v;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Other struct{Items []v::Item}

impl Other {
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	packagesWrite(t, appsrc, "directions.gox", `package app; use serde;
use library::model as m;
#[derive(serde::derive::Serialize)]
type Out struct{V m::Output}
#[derive(serde::derive::Deserialize)]
type In struct{V m::Input}

impl Out {
 pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) }
}

impl In {
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`)
	result, _, err := testdeps.PrepareFormal(t, appsrc)
	if err != nil {
		t.Fatal(err)
	}
	out := result.Root
	lib := strings.TrimSpace(testdeps.RunFormalGo(t, result, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", "wire.example/lib"))
	snapshot := func(root string) map[string]string {
		files := map[string]string{}
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files[name] = string(data)
			if strings.HasSuffix(path, ".go") && (strings.Contains(string(data), `"reflect"`) || strings.Contains(string(data), `"encoding/json"`)) {
				return fmt.Errorf("reflection/JSON fallback in %s", name)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return files
	}
	beforeApp, beforeLib := snapshot(out), snapshot(lib)
	repeated, _, err := testdeps.PrepareFormal(t, appsrc)
	if err != nil {
		t.Fatal(err)
	}
	compare := func(label string, before, after map[string]string) {
		if len(before) != len(after) {
			t.Fatalf("nondeterministic %s file count", label)
		}
		for path, data := range before {
			if after[path] != data {
				t.Fatalf("nondeterministic %s/%s", label, path)
			}
		}
	}
	compare("app", beforeApp, snapshot(repeated.Root))
	compare("library", beforeLib, snapshot(lib))
	packagesWrite(t, out, "app_test.go", externalSerdeAssertions)
	plain := t.TempDir()
	// Rebase the exported graph's local replacements for a separate native module.
	generatedMod, err := project.ImportMod(tool, out)
	if err != nil {
		t.Fatal(err)
	}
	generatedMod.Module = "wire.example/plain"
	for i := range generatedMod.Native.Replace {
		replacement := &generatedMod.Native.Replace[i]
		if replacement.Path != "" && !filepath.IsAbs(replacement.Path) {
			replacement.Path = filepath.Join(out, replacement.Path)
		}
	}
	generatedMod.Native.Require = append(generatedMod.Native.Require, project.GoRequire{Module: "wire.example/app", Version: "v0.0.0"})
	generatedMod.Native.Replace = append(generatedMod.Native.Replace, project.GoReplace{Module: "wire.example/app", Path: out})
	nativeMod, err := project.ModuleText(generatedMod)
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, plain, "go.mod", string(nativeMod))
	packagesWrite(t, plain, "consumer_test.go", `package plain
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";a "wire.example/app";m "wire.example/lib/model")
func TestGoAPIs(t *testing.T){v:=a.Out{V:m.Output{N:9}};b,e:=serdejson.Encode(func(s serde.Serializer)error{return (a.OutOps{}).Encode(s,&v)});if e!=nil||string(b)!="{\"V\":{\"N\":9}}"{t.Fatal(string(b),e)};x,e:=serdejson.Unmarshal(b,(a.InOps{}).Decode);if e!=nil||x.V.N!=9{t.Fatal(x,e)}}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for _, test := range []struct{ kind, pkg, typ string }{{"Serialize", "fake", "Item"}, {"Deserialize", "fake", "Item"}, {"Serialize", "relay", "New"}, {"Deserialize", "relay", "New"}, {"Serialize", "model", "Input"}, {"Deserialize", "model", "Output"}} {
		t.Run(test.kind+"-"+test.pkg+"-"+test.typ, func(t *testing.T) {
			bad := t.TempDir()
			packagesWrite(t, bad, "goxide.toml", testdeps.FormalManifest(t, "wire.example/bad", "bad")+dependency)
			packagesWrite(t, bad, "bad.gox", fmt.Sprintf("package bad; use serde;\nuse library::%s as m;\n#[derive(serde::derive::%s)]\ntype Wrap struct{V m::%s}\n", test.pkg, test.kind, test.typ))
			_, _, err := testdeps.PrepareFormal(t, bad)
			if err == nil || !strings.Contains(err.Error(), "does not explicitly implement "+test.kind) || !strings.Contains(err.Error(), "bad.gox:3:1:") {
				t.Fatalf("missing source-located protocol rejection: %v", err)
			}
		})
	}
}

const externalSerdeAssertions = `package app
import __borrow_std "github.com/goxide-lang/std/sum"
import("testing";"errors";"reflect";"strconv";"strings";"encoding/json";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";m "wire.example/lib/model";other "wire.example/lib/other")
const valid="{\"V\":{\"Entry\":{\"N\":7,\"Name\":\"business\"}},\"Items\":[],\"Index\":{},\"Pair\":[{\"N\":1,\"Name\":\"pair\"}],\"Maybe\":null}"
func enc(v Wrap)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return (WrapOps{}).Encode(s,&v)})}
func detail(t *testing.T,e error,kind string,path []serde.PathSegment){t.Helper();var d *serde.Error;if !errors.As(e,&d)||d.Kind!=kind||!reflect.DeepEqual(d.Path,path){t.Fatalf("error %v %#v want %s %#v",e,d,kind,path)}}
func TestExternalRoundTripAndOption(t *testing.T){
 v,e:=serdejson.Unmarshal([]byte(valid),(WrapOps{}).Decode);if e!=nil||__borrow_std.TagOption(v.Opt)!=0||v.Items==nil||v.Index==nil{t.Fatal(v,e)}
 b,e:=enc(v);if e!=nil{t.Fatal(e)};w,e:=serdejson.Unmarshal(b,(WrapOps{}).Decode);if e!=nil||!reflect.DeepEqual(v,w){t.Fatal(string(b),w,e)}
 for _,value:=range []string{"null","{\"N\":3,\"Name\":\"option\"}"}{input:=strings.TrimSuffix(valid,"}")+",\"Opt\":"+value+"}";v,e:=serdejson.Unmarshal([]byte(input),(WrapOps{}).Decode);if e!=nil{t.Fatal(e)};b,e:=enc(v);if e!=nil||!strings.Contains(string(b),"\"Opt\":"+value){t.Fatal(string(b),e)}}
 // Existing serde sequence/map projection emits empty for nil, then decodes nonnil empty.
 v.Items=nil;v.Index=nil;b,e=enc(v);if e!=nil||!strings.Contains(string(b),"\"Items\":[]")||!strings.Contains(string(b),"\"Index\":{}"){t.Fatal(string(b),e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"Items\":[]","\"Items\":null",1)),(WrapOps{}).Decode);if e==nil{t.Fatal("null slice accepted")}
 x:=Other{Items:[]other.Item{{Text:"different"}}};b,e=serdejson.Encode(func(s serde.Serializer)error{return (OtherOps{}).Encode(s,&x)});y,e2:=serdejson.Unmarshal(b,(OtherOps{}).Decode);if e!=nil||e2!=nil||!reflect.DeepEqual(x,y){t.Fatal(string(b),e,e2)}
 n:=Nested{Items:[][]m.E{{m.Event(2)}}};b,e=serdejson.Encode(func(s serde.Serializer)error{return (NestedOps{}).Encode(s,&n)});nn,e2:=serdejson.Unmarshal(b,(NestedOps{}).Decode);if e!=nil||e2!=nil||!reflect.DeepEqual(n,nn){t.Fatal(string(b),e,e2)}
}
func TestExternalErrors(t *testing.T){
 _,e:=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"N\":9223372036854775808",1)),(WrapOps{}).Decode)
 detail(t,e,serde.Range,[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Entry"},{Kind:"Field",Name:"N"}})
 var cause *strconv.NumError;if !errors.As(e,&cause)||!errors.Is(e,strconv.ErrRange){t.Fatal("cause lost",e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"N\":@",1)),(WrapOps{}).Decode)
 detail(t,e,serde.Syntax,[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Entry"},{Kind:"Field",Name:"N"}})
 var syntax *json.SyntaxError;if !errors.As(e,&syntax)||syntax.Offset<=0{t.Fatal("syntax cause lost",e)}
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"Entry\":","\"Unknown\":",1)),(WrapOps{}).Decode);detail(t,e,serde.UnknownVariant,[]serde.PathSegment{{Kind:"Field",Name:"V"}})
 _,e=serdejson.Unmarshal([]byte(strings.Replace(valid,"\"N\":7","\"extra\":0,\"N\":7",1)),(WrapOps{}).Decode);detail(t,e,"UnknownField",[]serde.PathSegment{{Kind:"Field",Name:"V"},{Kind:"Variant",Name:"Entry"},{Kind:"Field",Name:"extra"}})
 _,e=serdejson.Unmarshal([]byte(strings.TrimSuffix(valid,"}")+",\"extra\":{\"ignored\":[1,2]}}"),(WrapOps{}).Decode);if e!=nil{t.Fatal(e)}
 _,e=serdejson.Unmarshal([]byte(strings.TrimSuffix(valid,"}")+",\"extra\":"+strings.Repeat("[",serdejson.MaxDepth+2)+"0"+strings.Repeat("]",serdejson.MaxDepth+2)+"}"),(WrapOps{}).Decode);var d *serde.Error;if !errors.As(e,&d)||d.Kind!="Limit"{t.Fatal("skip budget lost",e)}
}
func TestExternalInvalidBoundary(t *testing.T){v,e:=serdejson.Unmarshal([]byte(valid),(WrapOps{}).Decode);if e!=nil{t.Fatal(e)};for _,x:=range []m.E{nil}{v.V=x;_,e:=enc(v);detail(t,e,serde.Type,[]serde.PathSegment{{Kind:"Field",Name:"V"}})}}
`
