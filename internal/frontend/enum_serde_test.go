package frontend

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnumSerdeGeneratedRuntime(t *testing.T) {
	r, e := compileSerdeFixture("event.gox", []byte(`package main
use fmt;
use serde;
use serde::json as serdejson;
#[derive(serde::Serialize, serde::Deserialize)]
enum Status { Idle, Code(int8), }
#[derive(serde::Serialize, serde::Deserialize)]
enum Event { Start, Status(Status), Text(string), }
#[derive(serde::Serialize, serde::Deserialize)]
type Envelope struct { Event Event }
func main(){
 values:=[]Event{Event::Start,Event::Text("ready"),Event::Status(Status::Code(12))}
 for _,v:=range values {
  data,e:=serdejson::Encode(func(s serde::Serializer)error{return SerializeEvent(s,&v)});if e!=nil{panic(e)}
  var out Event;e=serdejson::Decode(data,func(d serde::Deserializer)error{var e error;out,e=DeserializeEvent(d);return e});if e!=nil{panic(e)}
  again,e:=serdejson::Encode(func(s serde::Serializer)error{return SerializeEvent(s,&out)});if e!=nil||string(data)!=string(again){panic("roundtrip")};fmt::Println(string(data))
 }
 var bad Event
 if _,e:=serdejson::Encode(func(s serde::Serializer)error{return SerializeEvent(s,&bad)});e==nil{panic("accepted nil")}
 nested:=Event::Status(nil)
 if _,e:=serdejson::Encode(func(s serde::Serializer)error{return SerializeEvent(s,&nested)});e==nil{panic("accepted invalid nested enum")}
 for _,text:=range []string{"null","\"Unknown\"","{\"Text\":null}","{\"Status\":{\"Code\":128}}"}{if e:=serdejson::Decode([]byte(text),func(d serde::Deserializer)error{_,e:=DeserializeEvent(d);return e});e==nil{panic("accepted invalid JSON")}}
}
`))
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = testdeps.WriteFile(filepath.Join(dir, "go.mod"), []byte("module generated\n\ngo 1.27\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = testdeps.WriteFile(filepath.Join(dir, "main.go"), r.Go, 0600); e != nil {
		t.Fatal(e)
	}
	goexe, e := exec.LookPath("go")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(goexe, "run", ".")
	cmd.Dir = dir
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("%v\n%s", e, out)
	}
	if string(out) != "\"Start\"\n{\"Text\":\"ready\"}\n{\"Status\":{\"Code\":12}}\n" {
		t.Fatal(string(out))
	}
}
func TestEnumSerdeDiagnostics(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"#[derive(Error)]\nenum E { A, }", "requires error message"},
		{"#[derive(serde::Serialize)]\n#[serde(deny_unknown_fields)]\nenum E { A, }", "enum serde"},
		{"enum Inner { A, }\n#[derive(serde::Serialize)]\nenum Outer { Item(Inner), }", "required derive"},
		{"#[derive(serde::Serialize)]\nenum E {\n#[serde(unsupported=\"other\")]\nA, }", "unsupported enum variant serde attribute"},
	} {
		header := "package p\n"
		if strings.Contains(tc.src, "serde::") {
			header = "package p; use serde;\n"
		}
		_, e := compileSerdeFixture("bad.gox", []byte(header+tc.src))
		if e == nil || !strings.Contains(e.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.src, e)
		}
	}
}
