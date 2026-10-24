package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestSerdeDefaultsInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", filepath.Join(bin, "goxide"))
	cli := filepath.Join(bin, "goxide")
	source := t.TempDir()
	singleExternalCopyTree(t, filepath.Join(root, "examples/serde_defaults"), source, "target")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	generated = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source))
	packagesWrite(t, generated, "business_test.go", serdeDefaultsAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "demo 8080 8080 STSS\n" {
		t.Fatalf("CLI output: %q", got)
	}
}

func TestSerdeDefaultsMissingProtocolDiagnostics(t *testing.T) {
	for _, attribute := range []string{"default", "skip"} {
		for _, kind := range []string{"named", "pointer", "container", "ordinary-function"} {
			t.Run(attribute+"/"+kind, func(t *testing.T) {
				typ := "Settings"
				extra := ""
				switch kind {
				case "pointer":
					typ = "*int"
				case "container":
					typ = "[]int"
				case "ordinary-function":
					extra = "func DefaultSettings() Settings { return Settings{Port:99} }\n"
				}
				source := "package rejected; use serde;\n#[derive(serde::derive::Serialize,serde::derive::Deserialize)]\ntype Settings struct { Port int }\n" + extra +
					"#[derive(serde::derive::Deserialize)]\ntype Request struct {\n#[serde(" + attribute + ")]\nValue " + typ + "\n}\n"
				filename := filepath.Join(t.TempDir(), "missing_protocol.gox")
				_, err := testdeps.FormalSource(t, filename, []byte(source))
				wantLocation := "missing_protocol.gox:" + strconv.Itoa(strings.Count(source[:strings.Index(source, "#[derive(serde::derive::Deserialize)]")], "\n")+1) + ":"
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), "default") || !strings.Contains(err.Error(), wantLocation) {
					t.Fatalf("want source-located missing Default diagnostic: %v", err)
				}
			})
		}
	}
}

const serdeDefaultsAssertions = `package main
import("errors";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func request(input string)(Request,error){return serdejson.Unmarshal([]byte(input),(RequestOps{}).Decode)}
func encodeRequest(v Request)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return (RequestOps{}).Encode(s,&v)})}
func TestMissingDefaultsOrderAndRoundTrip(t *testing.T){
 defaultTrace=""
 v,err:=request("{\"Required\":\"job\",\"Optional\":null}")
 if err!=nil||defaultTrace!="STSS"||v.First.Port!=8080||v.Child.Value.Port!=8080||v.Secret.Value!="private"||v.Optional!=nil{t.Fatalf("default values/order: %#v trace=%q err=%v",v,defaultTrace,err)}
 state,err:=serdejson.Encode(func(s serde.Serializer)error{return (ModeOps{}).Encode(s,&v.State)})
 if err!=nil||string(state)!="{\"Ready\":{\"Port\":8080}}"{t.Fatalf("selected payload: %s %v",state,err)}
 wire,err:=encodeRequest(v);if err!=nil{t.Fatal(err)}
 if strings.Contains(string(wire),"Secret")||strings.Contains(string(wire),"private"){t.Fatalf("skip leaked: %s",wire)}
 defaultTrace=""
 back,err:=request(string(wire));if err!=nil||!reflect.DeepEqual(v,back)||defaultTrace!="T"{t.Fatalf("roundtrip: %#v trace=%s err=%v",back,defaultTrace,err)}
}
func TestExplicitValuesAndIgnoredSkip(t *testing.T){
 for _,secret:=range []string{"",",\"Secret\":null",",\"Secret\":{\"arbitrary\":[1,true,{}]}"}{
  defaultTrace=""
  v,err:=request("{\"Child\":{\"Value\":{\"Port\":4}},\"State\":{\"Disabled\":{\"Name\":\"off\"}},\"First\":{\"Port\":3},\"Optional\":null,\"Required\":\"job\""+secret+"}")
  if err!=nil||defaultTrace!="T"||v.First.Port!=3||v.Child.Value.Port!=4||v.Secret.Value!="private"{t.Fatalf("explicit/skip: %#v trace=%q err=%v",v,defaultTrace,err)}
 }
 v:=OutputOnly{Name:"ok",Internal:make(chan int)}
 out,err:=serdejson.Encode(func(s serde.Serializer)error{return (OutputOnlyOps{}).Encode(s,&v)})
 if err!=nil||string(out)!="{\"Name\":\"ok\"}"{t.Fatalf("output-only skip: %s %v",out,err)}
}
func TestNestedDefaultsPrecedeOuterAndNoRollbackPromise(t *testing.T){
 defaultTrace=""
 _,err:=request("{\"Child\":{},\"Required\":\"ok\",\"Optional\":null}")
 if err!=nil||defaultTrace!="SSTS"{t.Fatalf("nested read before outer defaults: %q %v",defaultTrace,err)}
 defaultTrace=""
 _,err=request("{\"Child\":{},\"Optional\":null}")
 if err==nil||defaultTrace!="S"{t.Fatalf("required failure should retain only completed nested default: %q %v",defaultTrace,err)}
}
func TestBadInputNeverFallsBackAndRetainsPath(t *testing.T){
 for _,tc:=range []struct{part string;path []serde.PathSegment;kind string}{
  {"\"First\":null",[]serde.PathSegment{{Kind:"Field",Name:"First"}},serde.Type},
  {"\"State\":null",[]serde.PathSegment{{Kind:"Field",Name:"State"}},serde.Type},
  {"\"State\":\"Future\"",[]serde.PathSegment{{Kind:"Field",Name:"State"}},serde.UnknownVariant},
  {"\"State\":{\"Ready\":{\"Port\":false}}",[]serde.PathSegment{{Kind:"Field",Name:"State"},{Kind:"Variant",Name:"Ready"},{Kind:"Field",Name:"Port"}},serde.Type},
  {"\"Child\":{\"Value\":{\"Port\":false}}",[]serde.PathSegment{{Kind:"Field",Name:"Child"},{Kind:"Field",Name:"Value"},{Kind:"Field",Name:"Port"}},serde.Type},
 }{
  defaultTrace=""
  _,err:=request("{\"Required\":\"job\",\"Optional\":null,"+tc.part+"}")
  var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=tc.kind||!reflect.DeepEqual(detail.Path,tc.path)||defaultTrace!=""{t.Fatalf("bad input %s: trace=%q err=%v detail=%#v",tc.part,defaultTrace,err,detail)}
 }
 for _,input:=range []string{"{\"Optional\":null}","{\"Required\":\"job\",\"Optional\":null,\"Secret\":[1,]}","{\"Required\":\"job\",\"Optional\":null,\"Secret\":{\"x\":1,\"x\":2}}"}{
  defaultTrace="";_,err:=request(input)
  if err==nil||defaultTrace!=""{t.Fatalf("invalid container/required/skip triggered defaults: %s trace=%q err=%v",input,defaultTrace,err)}
 }
}
`
