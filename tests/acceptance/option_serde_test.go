package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestOptionSerdeInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/hgo")
	cli := filepath.Join(bin, "hgo")
	source := filepath.Join(root, "examples/option_serde")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "business_test.go", optionSerdeAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "-go", tool, source); got != "option 7 -1 4 0\n" {
		t.Fatalf("CLI output: %q", got)
	}
}

func TestOptionSerdeExplicitBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"serialize-only-payload-cannot-decode":   "#[derive(Serialize)]\ntype Payload struct { Port int }\n#[derive(Deserialize)]\ntype Request struct { Value Option[Payload] }",
		"deserialize-only-payload-cannot-encode": "#[derive(Deserialize)]\ntype Payload struct { Port int }\n#[derive(Serialize)]\ntype Request struct { Value Option[Payload] }",
		"channel-payload":                        "#[derive(Serialize,Deserialize)]\ntype Request struct { Value Option[chan int] }",
		"missing-payload-serde":                  "type Settings struct { Port int }\n#[derive(Serialize,Deserialize)]\ntype Request struct { Value Option[Settings] }",
		"default-cannot-mask-unsupported":        "#[derive(Deserialize)]\ntype Request struct {\n#[serde(default)]\nValue Option[chan int]\n}",
		"ordinary-enum-option-payload":           "#[derive(Serialize,Deserialize)]\nenum Event { Empty, Data(Option[int]), }",
		"ordinary-enum-struct-option-payload":    "#[derive(Serialize,Deserialize)]\ntype Payload struct { Value Option[int] }\n#[derive(Serialize,Deserialize)]\nenum Event { Data(Payload), }",
		"option-enum-bearing-container":          "#[derive(Serialize,Deserialize)]\ntype Request struct { Value Option[[]Option[int]] }",
		"option-map-enum-bearing-container":      "#[derive(Serialize,Deserialize)]\ntype Request struct { Value Option[map[string]Option[int]] }",
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), name+".hgo")
			_, err := frontend.Compile(filename, []byte("package rejected\n"+source+"\n"))
			if err == nil || !strings.Contains(err.Error(), filepath.Base(filename)+":") {
				t.Fatalf("want located rejection, got %v", err)
			}
			want := "unsupported field type"
			if strings.Contains(name, "-only-payload-cannot-") {
				want = "nested type lacks required derive"
			}
			if strings.HasPrefix(name, "ordinary-enum-") {
				want = "unsupported enum payload"
			}
			if strings.HasPrefix(name, "option-") {
				want = "enum-bearing pointer/container projection"
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q, got %v", want, err)
			}
			// Frontend codec diagnostics must retain the actual source field line.
			if want == "unsupported field type" || want == "nested type lacks required derive" {
				fieldLine := strings.Count(source[:strings.Index(source, "Value Option")], "\n") + 2
				if !strings.Contains(err.Error(), filepath.Base(filename)+":"+strconv.Itoa(fieldLine)+":") {
					t.Fatalf("lost field source line: %v", err)
				}
			}
		})
	}
}

const optionSerdeAssertions = `package main
import("errors";"reflect";"strings";"testing";"github.com/hxsf/goxide/serde";"github.com/hxsf/goxide/serdejson")
func readRequest(input string)(Request,error){return serdejson.Unmarshal([]byte(input),DeserializeRequest)}
func writeRequest(v Request)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeRequest(s,&v)})}
const required = "\"Items\":[],\"Lookup\":{},\"Fixed\":[null,null]"
func TestMissingAndNullAreNoneWithoutPayloadDefault(t *testing.T){
 for _,extra:=range []string{"",",\"Count\":null,\"Config\":null,\"State\":null,\"Nested\":null,\"Pointer\":null,\"Fallback\":null"}{
  defaultCalls=0;v,err:=readRequest("{"+required+extra+"}")
  if err!=nil||v.Count!=nil||v.Config!=nil||v.State!=nil||v.Nested!=nil||v.Pointer!=nil||v.Fallback!=nil||v.Secret!=nil||defaultCalls!=0{t.Fatalf("missing/null: %#v calls=%d err=%v",v,defaultCalls,err)}
  wire,err:=writeRequest(v);if err!=nil||!strings.Contains(string(wire),"\"Count\":null")||strings.Contains(string(wire),"Secret"){t.Fatalf("None encoding/skip: %s %v",wire,err)}
 }
}
func TestConcretePayloadsAndCollectionsRoundTrip(t *testing.T){
 input:="{\"Count\":7,\"Config\":{\"Port\":9},\"State\":{\"Ready\":{\"Port\":8}},\"Nested\":3,\"Pointer\":4,\"List\":[1,2],\"Map\":{\"a\":5},\"Array\":[6,7],\"Items\":[null,2],\"Lookup\":{\"none\":null,\"some\":{\"Port\":10}},\"Fixed\":[null,4],\"Fallback\":{\"Port\":11}}"
 defaultCalls=0;v,err:=readRequest(input);if err!=nil{t.Fatal(err)}
 if count(v.Count)!=7||count(v.Items[0])!=-1||count(v.Items[1])!=2||count(v.Fixed[1])!=4||defaultCalls!=0{t.Fatalf("values %#v calls=%d",v,defaultCalls)}
 wire,err:=writeRequest(v);if err!=nil{t.Fatal(err)}
 back,err:=readRequest(string(wire));if err!=nil||!reflect.DeepEqual(v,back)||defaultCalls!=0{t.Fatalf("roundtrip: %s %#v %v calls=%d",wire,back,err,defaultCalls)}
}
func TestSkipConsumesFormatWithoutTargetType(t *testing.T){
 for _,secret:=range []string{"null","false","23","\"anything\"","{\"x\":[1,true,{}]}"}{
  defaultCalls=0;v,err:=readRequest("{"+required+",\"Secret\":"+secret+"}")
  if err!=nil||v.Secret!=nil||defaultCalls!=0{t.Fatalf("skip %s %#v %v",secret,v,err)}
 }
 for _,secret:=range []string{"[1,]","{\"x\":1,\"x\":2}"}{
  if _,err:=readRequest("{"+required+",\"Secret\":"+secret+"}");err==nil{t.Fatalf("invalid format accepted: %s",secret)}
 }
}
func TestPresentErrorsNeverDefaultAndRetainPath(t *testing.T){
 for _,tc:=range []struct{part string;path []serde.PathSegment;kind string}{
  {"\"Count\":false",[]serde.PathSegment{{Kind:"Field",Name:"Count"}},serde.Type},
  {"\"Fallback\":{\"Port\":false}",[]serde.PathSegment{{Kind:"Field",Name:"Fallback"},{Kind:"Field",Name:"Port"}},serde.Type},
  {"\"State\":\"Future\"",[]serde.PathSegment{{Kind:"Field",Name:"State"}},serde.UnknownVariant},
  {"\"State\":{\"Ready\":{\"Port\":false}}",[]serde.PathSegment{{Kind:"Field",Name:"State"},{Kind:"Variant",Name:"Ready"},{Kind:"Field",Name:"Port"}},serde.Type},
  {"\"List\":[1,false]",[]serde.PathSegment{{Kind:"Field",Name:"List"},{Kind:"Index",Index:1}},serde.Type},
  {"\"Map\":{\"bad\":false}",[]serde.PathSegment{{Kind:"Field",Name:"Map"},{Kind:"MapKey",Name:"bad"}},serde.Type},
 }{
  defaultCalls=0;_,err:=readRequest("{"+required+","+tc.part+"}");var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=tc.kind||!reflect.DeepEqual(detail.Path,tc.path)||defaultCalls!=0{t.Fatalf("%s: %#v %v calls=%d",tc.part,detail,err,defaultCalls)}
 }
 for _,tc:=range []struct{input string;path []serde.PathSegment}{
  {"{\"Items\":[null,false],\"Lookup\":{},\"Fixed\":[null,null]}",[]serde.PathSegment{{Kind:"Field",Name:"Items"},{Kind:"Index",Index:1}}},
  {"{\"Items\":[],\"Lookup\":{\"bad\":{\"Port\":false}},\"Fixed\":[null,null]}",[]serde.PathSegment{{Kind:"Field",Name:"Lookup"},{Kind:"MapKey",Name:"bad"},{Kind:"Field",Name:"Port"}}},
 }{
  _,err:=readRequest(tc.input);var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,tc.path){t.Fatalf("collection path: %#v %v",detail,err)}
 }
}
func TestNestedOptionIsLossyNotPatch(t *testing.T){
 v,err:=readRequest("{"+required+"}");if err!=nil{t.Fatal(err)}
 v.Nested=__hgo_enum_case_Option_Some[Option[int]]{Value:nil}
 v.Pointer=__hgo_enum_case_Option_Some[*int]{Value:nil}
 wire,err:=writeRequest(v);if err!=nil{t.Fatal(err)}
 back,err:=readRequest(string(wire));if err!=nil||back.Nested!=nil||back.Pointer!=nil||!strings.Contains(string(wire),"\"Nested\":null"){t.Fatalf("null collapse: %s %#v %v",wire,back,err)}
}
type forgedOption struct{}
func(forgedOption)__hgo_enum_marker_Option(int){}
func TestInvalidNativeValuesReturnTypeWithoutPanic(t *testing.T){
 for _,bad:=range []Option[int]{(*__hgo_enum_case_Option_Some[int])(nil),&__hgo_enum_case_Option_Some[int]{Value:2},forgedOption{}}{
  v,err:=readRequest("{"+required+"}");if err!=nil{t.Fatal(err)};v.Count=bad
  _,err=writeRequest(v);var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Count"}}){t.Fatalf("invalid native value: %#v %v",detail,err)}
 }
}
func TestOneDirectionDerives(t *testing.T){
 v,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":18}}"),DeserializeInputOnly);if err!=nil{t.Fatal(err)}
 payload,ok:=v.Value.(__hgo_enum_case_Option_Some[InputPayload]);if !ok||payload.Value.Port!=18{t.Fatalf("decode-only payload: %#v",v)}
 out:=OutputOnly{Value:__hgo_enum_case_Option_Some[OutputPayload]{Value:OutputPayload{Port:payload.Value.Port}}};wire,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeOutputOnly(s,&out)})
 if err!=nil||string(wire)!="{\"Value\":{\"Port\":18}}"{t.Fatalf("one direction: %s %v",wire,err)}
}
func TestSomePayloadDefaultsAreSeparateFromOptionDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Value\":null}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),DeserializeDefaultEnvelope)
  if err!=nil||v.Value!=nil||defaultCalls!=0{t.Fatalf("Option default constructed payload: %s %#v %v calls=%d",input,v,err,defaultCalls)}
 }
 for _,tc:=range []struct{input string;port,calls int}{
  {"{\"Value\":{}}",8080,1},
  {"{\"Value\":{\"Config\":{\"Port\":5}}}",5,0},
 }{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(tc.input),DeserializeDefaultEnvelope)
  if err!=nil{t.Fatal(err)};payload,ok:=v.Value.(__hgo_enum_case_Option_Some[PayloadWithDefault])
  if !ok||payload.Value.Config.Port!=tc.port||defaultCalls!=tc.calls{t.Fatalf("Some payload default: %#v calls=%d",v,defaultCalls)}
 }
 defaultCalls=0;_,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Config\":{\"Port\":false}}}"),DeserializeDefaultEnvelope);var detail *serde.Error
 if !errors.As(err,&detail)||detail.Kind!=serde.Type||defaultCalls!=0||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Value"},{Kind:"Field",Name:"Config"},{Kind:"Field",Name:"Port"}}){t.Fatalf("invalid Some fell back: %#v %v calls=%d",detail,err,defaultCalls)}
}

`
