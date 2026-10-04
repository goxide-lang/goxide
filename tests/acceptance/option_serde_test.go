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
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	cli := filepath.Join(bin, "goxide")
	source := filepath.Join(root, "examples/option_serde")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "business_test.go", optionSerdeAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "option 7 -1 4 0\n" {
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
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), name+".gox")
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

// These container payloads used to be rejected before serde lowering. Exercise
// both actual codecs and the recursive match validator through the installed CLI.
func TestOptionSerdeNestedContainersRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin, outside, source := t.TempDir(), t.TempDir(), t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	packagesWrite(t, source, "go.mod", "module nestedoption.example/app\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => "+strconv.Quote(root)+"\n")
	packagesWrite(t, source, "nested.gox", `package nested
import("github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
#[derive(Serialize,Deserialize)]
type Request struct { List Option[[]Option[int]]; Lookup Option[map[string]Option[int]] }
func Decode(data []byte)(Request,error){return serdejson.Unmarshal(data,DeserializeRequest)}
func Encode(v Request)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeRequest(s,&v)})}
var Entered int
func ListLength(v Option[[]Option[int]])int{
 match v {Option[[]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[[]Option[int]]::None=>{return -1}}
 return -2
}
func MapLength(v Option[map[string]Option[int]])int{
 match v {Option[map[string]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[map[string]Option[int]]::None=>{return -1}}
 return -2
}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	deliveryCommand(t, outside, filepath.Join(bin, "goxide"), true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "nested_test.go", optionSerdeNestedAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
}

const optionSerdeNestedAssertions = `package nested
import("errors";"reflect";"testing";"github.com/goxide-lang/serde")
func TestNestedRoundTrip(t *testing.T){
 for _,input:=range []string{"{}","{\"List\":null,\"Lookup\":null}"}{
  v,e:=Decode([]byte(input));if e!=nil||v.List!=nil||v.Lookup!=nil{t.Fatal(input,v,e)}
 }
 for _,input:=range []string{"{\"List\":[],\"Lookup\":{}}","{\"List\":[null,7],\"Lookup\":{\"none\":null,\"some\":8}}"}{
  v,e:=Decode([]byte(input));if e!=nil{t.Fatal(e)}
  rows,ok:=v.List.(__goxide_enum_case_Option_Some[[]Option[int]])
  lookup,ok2:=v.Lookup.(__goxide_enum_case_Option_Some[map[string]Option[int]])
  if !ok||!ok2||rows.Value==nil||lookup.Value==nil{t.Fatal("present containers must be Some",v)}
  Entered=0;if ListLength(v.List)!=len(rows.Value)||MapLength(v.Lookup)!=len(lookup.Value)||Entered!=2{t.Fatal("match",v,Entered)}
  if len(rows.Value)>0{if rows.Value[0]!=nil||rows.Value[1].(__goxide_enum_case_Option_Some[int]).Value!=7||lookup.Value["none"]!=nil||lookup.Value["some"].(__goxide_enum_case_Option_Some[int]).Value!=8{t.Fatal(v)}}
  wire,e:=Encode(v);if e!=nil{t.Fatal(e)};back,e:=Decode(wire);if e!=nil||!reflect.DeepEqual(v,back){t.Fatal(string(wire),back,e)}
 }
}
func TestNestedErrors(t *testing.T){
 for _,tc:=range []struct{input string;path []serde.PathSegment}{
  {"{\"List\":[null,false]}",[]serde.PathSegment{{Kind:"Field",Name:"List"},{Kind:"Index",Index:1}}},
  {"{\"Lookup\":{\"bad\":false}}",[]serde.PathSegment{{Kind:"Field",Name:"Lookup"},{Kind:"MapKey",Name:"bad"}}},
 }{_,e:=Decode([]byte(tc.input));var detail *serde.Error;if !errors.As(e,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,tc.path){t.Fatal(tc.input,detail,e)}}
}
func TestInvalidInnerValueBeforeArm(t *testing.T){
 var bad *__goxide_enum_case_Option_Some[int]
 a:=__goxide_enum_case_Option_Some[[]Option[int]]{Value:[]Option[int]{nil,bad}}
 b:=__goxide_enum_case_Option_Some[map[string]Option[int]]{Value:map[string]Option[int]{"bad":bad}}
 for _,f:=range []func(){func(){ListLength(a)},func(){MapLength(b)}}{
  Entered=0;func(){defer func(){if recover()==nil{t.Fatal("invalid nested value accepted")}}();f()}()
  if Entered!=0{t.Fatal("invalid nested value reached arm")}
 }
 for _,tc:=range []struct{v Request;path []serde.PathSegment}{
  {Request{List:a},[]serde.PathSegment{{Kind:"Field",Name:"List"},{Kind:"Index",Index:1}}},
  {Request{Lookup:b},[]serde.PathSegment{{Kind:"Field",Name:"Lookup"},{Kind:"MapKey",Name:"bad"}}},
 }{_,e:=Encode(tc.v);var detail *serde.Error;if !errors.As(e,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,tc.path){t.Fatal(detail,e)}}
}
`

func TestOptionProtocolDoesNotGrantPayloadDefault(t *testing.T) {
	_, err := frontend.Compile("no-payload-default.gox", []byte(`package fixture
#[derive(Deserialize)]
type InputPayload struct { Port int }
#[derive(Default)]
type RequiresDefault struct { Payload InputPayload }
`))
	if err == nil || !strings.Contains(err.Error(), "requires Default") {
		t.Fatalf("payload unexpectedly gained Default: %v", err)
	}
}

const optionSerdeAssertions = `package main
import("errors";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
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
 v.Nested=__goxide_enum_case_Option_Some[Option[int]]{Value:nil}
 v.Pointer=__goxide_enum_case_Option_Some[*int]{Value:nil}
 wire,err:=writeRequest(v);if err!=nil{t.Fatal(err)}
 back,err:=readRequest(string(wire));if err!=nil||back.Nested!=nil||back.Pointer!=nil||!strings.Contains(string(wire),"\"Nested\":null"){t.Fatalf("null collapse: %s %#v %v",wire,back,err)}
}
type forgedOption struct{Option[int]}
func TestInvalidNativeValuesReturnTypeWithoutPanic(t *testing.T){
 for _,bad:=range []Option[int]{(*__goxide_enum_case_Option_Some[int])(nil),&__goxide_enum_case_Option_Some[int]{Value:2},forgedOption{}}{
  v,err:=readRequest("{"+required+"}");if err!=nil{t.Fatal(err)};v.Count=bad
  _,err=writeRequest(v);var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Count"}}){t.Fatalf("invalid native value: %#v %v",detail,err)}
 }
}
func TestOneDirectionDerives(t *testing.T){
 v,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":18}}"),DeserializeInputOnly);if err!=nil{t.Fatal(err)}
 payload,ok:=v.Value.(__goxide_enum_case_Option_Some[InputPayload]);if !ok||payload.Value.Port!=18{t.Fatalf("decode-only payload: %#v",v)}
 out:=OutputOnly{Value:__goxide_enum_case_Option_Some[OutputPayload]{Value:OutputPayload{Port:payload.Value.Port}}};wire,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeOutputOnly(s,&out)})
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
  if err!=nil{t.Fatal(err)};payload,ok:=v.Value.(__goxide_enum_case_Option_Some[PayloadWithDefault])
  if !ok||payload.Value.Config.Port!=tc.port||defaultCalls!=tc.calls{t.Fatalf("Some payload default: %#v calls=%d",v,defaultCalls)}
 }
 defaultCalls=0;_,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Config\":{\"Port\":false}}}"),DeserializeDefaultEnvelope);var detail *serde.Error
 if !errors.As(err,&detail)||detail.Kind!=serde.Type||defaultCalls!=0||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Value"},{Kind:"Field",Name:"Config"},{Kind:"Field",Name:"Port"}}){t.Fatalf("invalid Some fell back: %#v %v calls=%d",detail,err,defaultCalls)}
}


func TestOptionProtocolWithoutPayloadDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Plain\":null,\"SideEffects\":null,\"Explicit\":null,\"Ignored\":{\"anything\":[1,true]}}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),DeserializeProtocolEnvelope)
  if err!=nil||v.Plain!=nil||v.SideEffects!=nil||v.Explicit!=nil||v.Ignored!=nil||defaultCalls!=0{t.Fatalf("protocol None: %s %#v err=%v defaults=%d",input,v,err,defaultCalls)}
 }
 defaultCalls=0;v,err:=serdejson.Unmarshal([]byte("{\"Plain\":{\"Port\":7},\"Explicit\":{\"Port\":8}}"),DeserializeProtocolEnvelope)
 if err!=nil{t.Fatal(err)}
 plain,ok:=v.Plain.(__goxide_enum_case_Option_Some[InputPayload]);explicit,ok2:=v.Explicit.(__goxide_enum_case_Option_Some[InputPayload])
 if !ok||!ok2||plain.Value.Port!=7||explicit.Value.Port!=8||defaultCalls!=0{t.Fatalf("Some without Default: %#v defaults=%d",v,defaultCalls)}
 for _,input:=range []string{"{\"Plain\":false}","{\"Explicit\":false}","{\"Ignored\":[1,]}"}{
  if _,err:=serdejson.Unmarshal([]byte(input),DeserializeProtocolEnvelope);err==nil||defaultCalls!=0{t.Fatalf("bad input defaulted: %s %v defaults=%d",input,err,defaultCalls)}
 }
}
`
