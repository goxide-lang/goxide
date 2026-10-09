package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

type legacySumGoCase struct {
	source string
	want   string
}

// Closed typed sums make the former interface-shaped cases unassignable.
// Keep those old construction attempts as real Go compile rejections instead
// of restoring compatibility aliases or forging private tags with unsafe.
func assertLegacySumGoRejected(t *testing.T, module, tool string, cases map[string]legacySumGoCase) {
	t.Helper()
	for name, test := range cases {
		t.Run("legacy-go-"+name, func(t *testing.T) {
			const filename = "legacy_sum_rejected_test.go"
			path := filepath.Join(module, filename)
			packagesWrite(t, module, filename, test.source)
			t.Cleanup(func() {
				if err := os.Remove(path); err != nil {
					t.Error(err)
				}
			})
			output := packagesCommand(t, module, tool, false, "test", "-run", "^$", ".")
			if !strings.Contains(output, filename+":") || !strings.Contains(output, test.want) {
				t.Fatalf("expected legacy source rejection %q: %s", test.want, output)
			}
		})
	}
}

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
	assertLegacySumGoRejected(t, generated, tool, map[string]legacySumGoCase{
		"nil-option":     {"package main\nvar _ Option[int]=nil\n", "cannot use nil"},
		"typed-nil-case": {"package main\nvar _ Option[int]=(*__goxide_enum_case_Option_Some[int])(nil)\n", "undefined: __goxide_enum_case_Option_Some"},
		"pointer-case":   {"package main\nvar _ Option[int]=&__goxide_enum_case_Option_Some[int]{Value:2}\n", "undefined: __goxide_enum_case_Option_Some"},
		"forged-option":  {"package main\ntype forgedOption struct{Option[int]}\nvar _ Option[int]=forgedOption{}\n", "cannot use"},
	})
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "option 7 -1 4 0\n" {
		t.Fatalf("CLI output: %q", got)
	}
}

func TestOptionSerdeExplicitBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"serialize-only-payload-cannot-decode":   "#[derive(serde::Serialize)]\ntype Payload struct { Port int }\n#[derive(serde::Deserialize)]\ntype Request struct { Value Option[Payload] }",
		"deserialize-only-payload-cannot-encode": "#[derive(serde::Deserialize)]\ntype Payload struct { Port int }\n#[derive(serde::Serialize)]\ntype Request struct { Value Option[Payload] }",
		"channel-payload":                        "#[derive(serde::Serialize,serde::Deserialize)]\ntype Request struct { Value Option[chan int] }",
		"missing-payload-serde":                  "type Settings struct { Port int }\n#[derive(serde::Serialize,serde::Deserialize)]\ntype Request struct { Value Option[Settings] }",
		"default-cannot-mask-unsupported":        "#[derive(serde::Deserialize)]\ntype Request struct {\n#[serde(default)]\nValue Option[chan int]\n}",
		"ordinary-enum-option-payload":           "#[derive(serde::Serialize,serde::Deserialize)]\nenum Event { Empty, Data(Option[int]), }",
		"ordinary-enum-struct-option-payload":    "#[derive(serde::Serialize,serde::Deserialize)]\ntype Payload struct { Value Option[int] }\n#[derive(serde::Serialize,serde::Deserialize)]\nenum Event { Data(Payload), }",
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), name+".gox")
			_, err := compileAcceptanceSerdeFixture(filename, []byte("package rejected; use serde;\n"+source+"\n"))
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
	packagesWrite(t, source, "go.mod", "module nestedoption.example/app\ngo 1.27\n")
	writeAcceptanceSerdeRoots(t, source, "")
	packagesWrite(t, source, "nested.gox", `package nested; use serde;
use serde::json as serdejson;
#[derive(serde::Serialize,serde::Deserialize)]
type Request struct { List Option[[]Option[int]]; Lookup Option[map[string]Option[int]] }
func Decode(data []byte)(Request,error){return serdejson::Unmarshal(data,DeserializeRequest)}
func Encode(v Request)([]byte,error){return serdejson::Encode(func(s serde::Serializer)error{return SerializeRequest(s,&v)})}
var Entered int
func ListLength(v Option[[]Option[int]])int{
 match v {Option[[]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[[]Option[int]]::None=>{return -1}}
 return -2
}
func MapLength(v Option[map[string]Option[int]])int{
 match v {Option[map[string]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[map[string]Option[int]]::None=>{return -1}}
 return -2
}
#[derive(serde::Serialize,serde::Deserialize)]
pub enum State{Good,}
#[derive(serde::Serialize,serde::Deserialize)]
type StateRequest struct{List Option[[]State];Lookup Option[map[string]State]}
func GoodState()State{return State::Good}
func EncodeState(v StateRequest)([]byte,error){return serdejson::Encode(func(s serde::Serializer)error{return SerializeStateRequest(s,&v)})}
func StateListLength(v Option[[]State])int{
 match v{Option[[]State]::Some(rows)=>{Entered++;return len(rows)},Option[[]State]::None=>{return -1}}
 return -2
}
func StateMapLength(v Option[map[string]State])int{
 match v{Option[map[string]State]::Some(rows)=>{Entered++;return len(rows)},Option[map[string]State]::None=>{return -1}}
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
import __borrow_std "github.com/goxide-lang/std/sum"

import("errors";"reflect";"testing";"github.com/goxide-lang/serde")
func TestNestedRoundTrip(t *testing.T){
 for _,input:=range []string{"{}","{\"List\":null,\"Lookup\":null}"}{
  v,e:=Decode([]byte(input));if e!=nil||__borrow_std.TagOption(v.List)!=0||__borrow_std.TagOption(v.Lookup)!=0{t.Fatal(input,v,e)}
 }
 for _,input:=range []string{"{\"List\":[],\"Lookup\":{}}","{\"List\":[null,7],\"Lookup\":{\"none\":null,\"some\":8}}"}{
  v,e:=Decode([]byte(input));if e!=nil{t.Fatal(e)}
  if __borrow_std.TagOption(v.List)!=1||__borrow_std.TagOption(v.Lookup)!=1{t.Fatal("present containers must be Some",v)}
  rows:=__borrow_std.ValueSome(v.List);lookup:=__borrow_std.ValueSome(v.Lookup)
  if rows==nil||lookup==nil{t.Fatal("present containers must have nonnil payload",v)}
  Entered=0;if ListLength(v.List)!=len(rows)||MapLength(v.Lookup)!=len(lookup)||Entered!=2{t.Fatal("match",v,Entered)}
  if len(rows)>0{if __borrow_std.TagOption(rows[0])!=0||__borrow_std.ValueSome(rows[1])!=7||__borrow_std.TagOption(lookup["none"])!=0||__borrow_std.ValueSome(lookup["some"])!=8{t.Fatal(v)}}
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
 // An invalid Option[int] is no longer constructible using its public API.
 // A zero user enum inside a valid Some remains a real nested invalid value.
 a:=__borrow_std.NewSome([]State{GoodState(),{}})
 b:=__borrow_std.NewSome(map[string]State{"bad":{}})
 for _,f:=range []func(){func(){StateListLength(a)},func(){StateMapLength(b)}}{
  Entered=0;func(){defer func(){if recover()==nil{t.Fatal("invalid nested value accepted")}}();f()}()
  if Entered!=0{t.Fatal("invalid nested value reached arm")}
 }
 for _,tc:=range []struct{v StateRequest;path []serde.PathSegment}{
  {StateRequest{List:a},[]serde.PathSegment{{Kind:"Field",Name:"List"},{Kind:"Index",Index:1}}},
  {StateRequest{Lookup:b},[]serde.PathSegment{{Kind:"Field",Name:"Lookup"},{Kind:"MapKey",Name:"bad"}}},
 }{_,e:=EncodeState(tc.v);var detail *serde.Error;if !errors.As(e,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,tc.path){t.Fatal(detail,e)}}
}
`

func TestOptionProtocolDoesNotGrantPayloadDefault(t *testing.T) {
	_, err := compileAcceptanceSerdeFixture("no-payload-default.gox", []byte(`package fixture; use serde;
#[derive(serde::Deserialize)]
type InputPayload struct { Port int }
#[derive(Default)]
type RequiresDefault struct { Payload InputPayload }
`))
	if err == nil || !strings.Contains(err.Error(), "requires Default") {
		t.Fatalf("payload unexpectedly gained Default: %v", err)
	}
}

const optionSerdeAssertions = `package main
import __borrow_std "github.com/goxide-lang/std/sum"
import("errors";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func readRequest(input string)(Request,error){return serdejson.Unmarshal([]byte(input),DeserializeRequest)}
func writeRequest(v Request)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeRequest(s,&v)})}
const required = "\"Items\":[],\"Lookup\":{},\"Fixed\":[null,null]"
func TestMissingAndNullAreNoneWithoutPayloadDefault(t *testing.T){
 for _,extra:=range []string{"",",\"Count\":null,\"Config\":null,\"State\":null,\"Nested\":null,\"Pointer\":null,\"Fallback\":null"}{
  defaultCalls=0;v,err:=readRequest("{"+required+extra+"}")
  if err!=nil||__borrow_std.TagOption(v.Count)!=0||__borrow_std.TagOption(v.Config)!=0||__borrow_std.TagOption(v.State)!=0||__borrow_std.TagOption(v.Nested)!=0||__borrow_std.TagOption(v.Pointer)!=0||__borrow_std.TagOption(v.Fallback)!=0||__borrow_std.TagOption(v.Secret)!=0||defaultCalls!=0{t.Fatalf("missing/null: %#v calls=%d err=%v",v,defaultCalls,err)}
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
  if err!=nil||__borrow_std.TagOption(v.Secret)!=0||defaultCalls!=0{t.Fatalf("skip %s %#v %v",secret,v,err)}
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
 v.Nested=__borrow_std.NewSome(__borrow_std.None[int]())
 v.Pointer=__borrow_std.NewSome[*int](nil)
 if __borrow_std.TagOption(v.Nested)!=1||__borrow_std.TagOption(v.Pointer)!=1{t.Fatal("Some(None)/Some(nil) collapsed before encoding")}
 wire,err:=writeRequest(v);if err!=nil{t.Fatal(err)}
 back,err:=readRequest(string(wire));if err!=nil||__borrow_std.TagOption(back.Nested)!=0||__borrow_std.TagOption(back.Pointer)!=0||!strings.Contains(string(wire),"\"Nested\":null"){t.Fatalf("null collapse: %s %#v %v",wire,back,err)}
}
func TestInvalidNativeValuesReturnTypeWithoutPanic(t *testing.T){
 // The old dynamic Option cases now fail Go compilation. Some(Mode{}) still
 // contains an invalid value, and the codec must return its structured path.
 for _,bad:=range []Option[Mode]{__borrow_std.NewSome(Mode{})}{
  v,err:=readRequest("{"+required+"}");if err!=nil{t.Fatal(err)};v.State=bad
  _,err=writeRequest(v);var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"State"}}){t.Fatalf("invalid native value: %#v %v",detail,err)}
 }
}
func TestOneDirectionDerives(t *testing.T){
 v,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":18}}"),DeserializeInputOnly);if err!=nil{t.Fatal(err)}
 if __borrow_std.TagOption(v.Value)!=1{t.Fatalf("decode-only payload: %#v",v)};payload:=__borrow_std.ValueSome(v.Value);if payload.Port!=18{t.Fatal(payload)}
 out:=OutputOnly{Value:__borrow_std.NewSome(OutputPayload{Port:payload.Port})};wire,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeOutputOnly(s,&out)})
 if err!=nil||string(wire)!="{\"Value\":{\"Port\":18}}"{t.Fatalf("one direction: %s %v",wire,err)}
}
func TestSomePayloadDefaultsAreSeparateFromOptionDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Value\":null}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),DeserializeDefaultEnvelope)
  if err!=nil||__borrow_std.TagOption(v.Value)!=0||defaultCalls!=0{t.Fatalf("Option default constructed payload: %s %#v %v calls=%d",input,v,err,defaultCalls)}
 }
 for _,tc:=range []struct{input string;port,calls int}{
  {"{\"Value\":{}}",8080,1},
  {"{\"Value\":{\"Config\":{\"Port\":5}}}",5,0},
 }{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(tc.input),DeserializeDefaultEnvelope)
  if err!=nil{t.Fatal(err)};if __borrow_std.TagOption(v.Value)!=1{t.Fatal("expected Some")};payload:=__borrow_std.ValueSome(v.Value)
  if payload.Config.Port!=tc.port||defaultCalls!=tc.calls{t.Fatalf("Some payload default: %#v calls=%d",v,defaultCalls)}
 }
 defaultCalls=0;_,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Config\":{\"Port\":false}}}"),DeserializeDefaultEnvelope);var detail *serde.Error
 if !errors.As(err,&detail)||detail.Kind!=serde.Type||defaultCalls!=0||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Value"},{Kind:"Field",Name:"Config"},{Kind:"Field",Name:"Port"}}){t.Fatalf("invalid Some fell back: %#v %v calls=%d",detail,err,defaultCalls)}
}


func TestOptionProtocolWithoutPayloadDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Plain\":null,\"SideEffects\":null,\"Explicit\":null,\"Ignored\":{\"anything\":[1,true]}}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),DeserializeProtocolEnvelope)
  if err!=nil||__borrow_std.TagOption(v.Plain)!=0||__borrow_std.TagOption(v.SideEffects)!=0||__borrow_std.TagOption(v.Explicit)!=0||__borrow_std.TagOption(v.Ignored)!=0||defaultCalls!=0{t.Fatalf("protocol None: %s %#v err=%v defaults=%d",input,v,err,defaultCalls)}
 }
 defaultCalls=0;v,err:=serdejson.Unmarshal([]byte("{\"Plain\":{\"Port\":7},\"Explicit\":{\"Port\":8}}"),DeserializeProtocolEnvelope)
 if err!=nil{t.Fatal(err)}
 if __borrow_std.TagOption(v.Plain)!=1||__borrow_std.TagOption(v.Explicit)!=1{t.Fatal("expected Some")};plain:=__borrow_std.ValueSome(v.Plain);explicit:=__borrow_std.ValueSome(v.Explicit)
 if plain.Port!=7||explicit.Port!=8||defaultCalls!=0{t.Fatalf("Some without Default: %#v defaults=%d",v,defaultCalls)}
 for _,input:=range []string{"{\"Plain\":false}","{\"Explicit\":false}","{\"Ignored\":[1,]}"}{
  if _,err:=serdejson.Unmarshal([]byte(input),DeserializeProtocolEnvelope);err==nil||defaultCalls!=0{t.Fatalf("bad input defaulted: %s %v defaults=%d",input,err,defaultCalls)}
 }
}
`
