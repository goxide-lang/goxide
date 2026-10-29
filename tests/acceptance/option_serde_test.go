package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

type nativeGoRejection struct {
	source string
	want   string
}

// Check the public Go representation through real native compilation.
func assertNativeGoRejected(t *testing.T, module, tool string, cases map[string]nativeGoRejection) {
	t.Helper()
	for name, test := range cases {
		t.Run("native-go-"+name, func(t *testing.T) {
			const filename = "native_rejected_test.go"
			path := filepath.Join(module, filename)
			packagesWrite(t, module, filename, test.source)
			t.Cleanup(func() {
				if err := os.Remove(path); err != nil {
					t.Error(err)
				}
			})
			output := packagesCommand(t, module, tool, false, "test", "-run", "^$", ".")
			if !strings.Contains(output, filename+":") || !strings.Contains(output, test.want) {
				t.Fatalf("expected native source rejection %q: %s", test.want, output)
			}
		})
	}
}

func TestOptionSerdeInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := acceptanceCLI(t)
	source := t.TempDir()
	singleExternalCopyTree(t, filepath.Join(root, "examples/option_serde"), source, "target")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	generated = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source))
	packagesWrite(t, generated, "business_test.go", optionSerdeAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	assertNativeGoRejected(t, generated, tool, map[string]nativeGoRejection{
		"nil-option":    {"package main\nimport s \"github.com/goxide-lang/std/sum\"\nvar _ s.Option[int]=nil\n", "cannot use nil"},
		"forged-option": {"package main\nimport s \"github.com/goxide-lang/std/sum\"\ntype forgedOption struct{s.Option[int]}\nvar _ s.Option[int]=forgedOption{}\n", "cannot use"},
	})
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "option 7 -1 4 0\n" {
		t.Fatalf("CLI output: %q", got)
	}
}

func TestOptionSerdeExplicitBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"serialize-only-payload-cannot-decode":   "#[derive(serde::derive::Serialize)]\ntype Payload struct { Port int }\n#[derive(serde::derive::Deserialize)]\ntype Request struct { Value Option[Payload] }",
		"deserialize-only-payload-cannot-encode": "#[derive(serde::derive::Deserialize)]\ntype Payload struct { Port int }\n#[derive(serde::derive::Serialize)]\ntype Request struct { Value Option[Payload] }",
		"channel-payload":                        "#[derive(serde::derive::Serialize,serde::derive::Deserialize)]\ntype Request struct { Value Option[chan int] }",
		"missing-payload-serde":                  "type Settings struct { Port int }\n#[derive(serde::derive::Serialize,serde::derive::Deserialize)]\ntype Request struct { Value Option[Settings] }",
		"default-cannot-mask-unsupported":        "#[derive(serde::derive::Deserialize)]\ntype Request struct {\n#[serde(default)]\nValue Option[chan int]\n}",
	} {
		t.Run(name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), name+".gox")
			_, err := testdeps.FormalSource(t, filename, []byte("package rejected; use serde;\n"+source+"\n"))
			if err == nil || !strings.Contains(err.Error(), filepath.Base(filename)+":") {
				t.Fatalf("want located rejection, got %v", err)
			}
			want := "does not explicitly implement"
			if strings.Contains(name, "-only-payload-cannot-") {
				want = "does not explicitly implement Serialize"
				if strings.Contains(name, "cannot-decode") {
					want = "does not explicitly implement Deserialize"
				}
			}

			if !strings.Contains(err.Error(), want) {
				t.Fatalf("want %q, got %v", want, err)
			}
			// The derive request owns the codec diagnostic in the new frontend.
			derive := strings.LastIndex(source, "#[derive(")
			line := strings.Count(source[:derive], "\n") + 2
			if !strings.Contains(err.Error(), filepath.Base(filename)+":"+strconv.Itoa(line)+":") {
				t.Fatalf("lost derive source line: %v", err)
			}
		})
	}
}

// These container payloads used to be rejected before serde lowering. Exercise
// actual codecs and enum checks at each dispatch through the installed CLI.
func TestOptionSerdeNestedContainersRace(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	outside, source := t.TempDir(), t.TempDir()
	packagesWrite(t, source, "goxide.toml", testdeps.FormalManifest(t, "nestedoption.example/app", "nested"))
	packagesWrite(t, source, "nested.gox", `package nested; use serde;
use serde::json as serdejson;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Request struct { List Option[[]Option[int]]; Lookup Option[map[string]Option[int]] }
func Decode(data []byte)(Request,error){return serdejson::Unmarshal(data,serde::Read[Request])}
func Encode(v Request)([]byte,error){return serdejson::Encode(func(s serde::Serializer)error{return serde::Write(s,&v)})}
var Entered int
func ListLength(v Option[[]Option[int]])int{
 match v {Option[[]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[[]Option[int]]::None=>{return -1}}
}
func MapLength(v Option[map[string]Option[int]])int{
 match v {Option[map[string]Option[int]]::Some(rows)=>{Entered++;return len(rows)},Option[map[string]Option[int]]::None=>{return -1}}
}
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
pub enum State{Good,}
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type StateRequest struct{List Option[[]State];Lookup Option[map[string]State]}
func GoodState()State{return State::Good}
func EncodeState(v StateRequest)([]byte,error){return serdejson::Encode(func(s serde::Serializer)error{return serde::Write(s,&v)})}
func StateListLength(v Option[[]State])int{
 match v{Option[[]State]::Some(rows)=>{Entered++;return len(rows)},Option[[]State]::None=>{return -1}}
}
func StateMapLength(v Option[map[string]State])int{
 match v{Option[map[string]State]::Some(rows)=>{Entered++;return len(rows)},Option[map[string]State]::None=>{return -1}}
}
func StateDispatch(value State,seen *int){match value{State::Good=>{*seen+=1},}}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	generated = strings.TrimSpace(deliveryCommand(t, outside, acceptanceCLI(t), true, nil, "transpile", "--lib", "-o", generated, source))
	packagesWrite(t, generated, "nested_test.go", optionSerdeNestedAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
}

const optionSerdeNestedAssertions = `package nested
import __borrow_std "github.com/goxide-lang/std/sum"

import("errors";"fmt";"reflect";"strings";"testing";"github.com/goxide-lang/serde")
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
 a:=__borrow_std.NewSome([]State{GoodState(),nil})
 b:=__borrow_std.NewSome(map[string]State{"bad":nil})
 for _,tc:=range []struct{run func()int;want int}{{func()int{return StateListLength(a)},2},{func()int{return StateMapLength(b)},1}}{
  Entered=0;if got:=tc.run();got!=tc.want||Entered!=1{t.Fatal("outer match inspected container elements",got,Entered)}
 }
 for _,value:=range []State{__borrow_std.ValueSome(a)[1],__borrow_std.ValueSome(b)["bad"]}{
  seen:=0;func(){defer func(){if p:=recover();!strings.Contains(fmt.Sprint(p),"nil State")||seen!=0{t.Fatalf("element dispatch panic=%v effect=%d",p,seen)}}();StateDispatch(value,&seen)}()
 }
 for _,tc:=range []struct{v StateRequest;path []serde.PathSegment}{
  {StateRequest{List:a},[]serde.PathSegment{{Kind:"Field",Name:"List"},{Kind:"Index",Index:1}}},
  {StateRequest{Lookup:b},[]serde.PathSegment{{Kind:"Field",Name:"Lookup"},{Kind:"MapKey",Name:"bad"}}},
 }{_,e:=EncodeState(tc.v);var detail *serde.Error;if !errors.As(e,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,tc.path){t.Fatal(detail,e)}}
}
`

func TestOptionProtocolDoesNotGrantPayloadDefault(t *testing.T) {
	_, err := testdeps.FormalSource(t, "no-payload-default.gox", []byte(`package fixture; use serde; use std;
#[derive(serde::derive::Deserialize)]
type InputPayload struct { Port int }
#[derive(std::derive::Default)]
type RequiresDefault struct { Payload InputPayload }
`))
	if err == nil || !strings.Contains(err.Error(), "missing trait implementation") || !strings.Contains(err.Error(), "Default") || !strings.Contains(err.Error(), "InputPayload") || !strings.Contains(err.Error(), "no-payload-default.gox:4:1") {
		t.Fatalf("payload unexpectedly gained Default: %v", err)
	}
}

const optionSerdeAssertions = `package main
import __borrow_std "github.com/goxide-lang/std/sum"
import("errors";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func readRequest(input string)(Request,error){return serdejson.Unmarshal([]byte(input),(RequestOps{}).Decode)}
func writeRequest(v Request)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return (RequestOps{}).Encode(s,&v)})}
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
 // The old dynamic Option cases now fail Go compilation. Some(nil) still
 // contains an invalid value, and the codec must return its structured path.
 for _,bad:=range []__borrow_std.Option[Mode]{__borrow_std.NewSome[Mode](nil)}{
  v,err:=readRequest("{"+required+"}");if err!=nil{t.Fatal(err)};v.State=bad
  _,err=writeRequest(v);var detail *serde.Error
  if !errors.As(err,&detail)||detail.Kind!=serde.Type||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"State"}}){t.Fatalf("invalid native value: %#v %v",detail,err)}
 }
}
func TestOneDirectionDerives(t *testing.T){
 v,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":18}}"),(InputOnlyOps{}).Decode);if err!=nil{t.Fatal(err)}
 if __borrow_std.TagOption(v.Value)!=1{t.Fatalf("decode-only payload: %#v",v)};payload:=__borrow_std.ValueSome(v.Value);if payload.Port!=18{t.Fatal(payload)}
 out:=OutputOnly{Value:__borrow_std.NewSome(OutputPayload{Port:payload.Port})};wire,err:=serdejson.Encode(func(s serde.Serializer)error{return (OutputOnlyOps{}).Encode(s,&out)})
 if err!=nil||string(wire)!="{\"Value\":{\"Port\":18}}"{t.Fatalf("one direction: %s %v",wire,err)}
}
func TestSomePayloadDefaultsAreSeparateFromOptionDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Value\":null}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),(DefaultEnvelopeOps{}).Decode)
  if err!=nil||__borrow_std.TagOption(v.Value)!=0||defaultCalls!=0{t.Fatalf("Option default constructed payload: %s %#v %v calls=%d",input,v,err,defaultCalls)}
 }
 for _,tc:=range []struct{input string;port,calls int}{
  {"{\"Value\":{}}",8080,1},
  {"{\"Value\":{\"Config\":{\"Port\":5}}}",5,0},
 }{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(tc.input),(DefaultEnvelopeOps{}).Decode)
  if err!=nil{t.Fatal(err)};if __borrow_std.TagOption(v.Value)!=1{t.Fatal("expected Some")};payload:=__borrow_std.ValueSome(v.Value)
  if payload.Config.Port!=tc.port||defaultCalls!=tc.calls{t.Fatalf("Some payload default: %#v calls=%d",v,defaultCalls)}
 }
 defaultCalls=0;_,err:=serdejson.Unmarshal([]byte("{\"Value\":{\"Config\":{\"Port\":false}}}"),(DefaultEnvelopeOps{}).Decode);var detail *serde.Error
 if !errors.As(err,&detail)||detail.Kind!=serde.Type||defaultCalls!=0||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Value"},{Kind:"Field",Name:"Config"},{Kind:"Field",Name:"Port"}}){t.Fatalf("invalid Some fell back: %#v %v calls=%d",detail,err,defaultCalls)}
}


func TestOptionProtocolWithoutPayloadDefault(t *testing.T){
 for _,input:=range []string{"{}","{\"Plain\":null,\"SideEffects\":null,\"Explicit\":null,\"Ignored\":{\"anything\":[1,true]}}"}{
  defaultCalls=0;v,err:=serdejson.Unmarshal([]byte(input),(ProtocolEnvelopeOps{}).Decode)
  if err!=nil||__borrow_std.TagOption(v.Plain)!=0||__borrow_std.TagOption(v.SideEffects)!=0||__borrow_std.TagOption(v.Explicit)!=0||__borrow_std.TagOption(v.Ignored)!=0||defaultCalls!=0{t.Fatalf("protocol None: %s %#v err=%v defaults=%d",input,v,err,defaultCalls)}
 }
 defaultCalls=0;v,err:=serdejson.Unmarshal([]byte("{\"Plain\":{\"Port\":7},\"Explicit\":{\"Port\":8}}"),(ProtocolEnvelopeOps{}).Decode)
 if err!=nil{t.Fatal(err)}
 if __borrow_std.TagOption(v.Plain)!=1||__borrow_std.TagOption(v.Explicit)!=1{t.Fatal("expected Some")};plain:=__borrow_std.ValueSome(v.Plain);explicit:=__borrow_std.ValueSome(v.Explicit)
 if plain.Port!=7||explicit.Port!=8||defaultCalls!=0{t.Fatalf("Some without Default: %#v defaults=%d",v,defaultCalls)}
 for _,input:=range []string{"{\"Plain\":false}","{\"Explicit\":false}","{\"Ignored\":[1,]}"}{
  if _,err:=serdejson.Unmarshal([]byte(input),(ProtocolEnvelopeOps{}).Decode);err==nil||defaultCalls!=0{t.Fatalf("bad input defaulted: %s %v defaults=%d",input,err,defaultCalls)}
 }
}
`
