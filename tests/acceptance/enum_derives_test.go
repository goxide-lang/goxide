package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestEnumDerivesApplicationRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../examples/prototype/enum_derives.hgo"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := frontend.Compile("enum_derives.hgo", source)
	if err != nil {
		t.Fatal(err)
	}
	idleCase := ""
	for _, decl := range result.Enums {
		if decl.Name == "Command" {
			for _, variant := range decl.Variants {
				if variant.Name == "Idle" {
					idleCase = variant.CaseName
				}
			}
		}
	}
	if idleCase == "" {
		t.Fatal("missing nominal Command metadata")
	}
	macroRaceModule(t, string(source), strings.ReplaceAll(enumDeriveAssertions, "COMMAND_IDLE_CASE", idleCase))
}

const enumDeriveAssertions = `package main
import("testing";"reflect";"errors";"github.com/hxsf/goxide/serde";"github.com/hxsf/goxide/serdejson")
func encodeCommand(v Command)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeCommand(s,&v)})}
func sample()WorkItem{return WorkItem{Name:"mail",Retries:[]int{1,2},Labels:map[string]string{"z":"last","a":"first"}}}
func TestStructAndContainerPayloadRoundTrips(t *testing.T){item:=sample();for _,value:=range []Command{idle(),submit(item),batch([]WorkItem{item}),indexed(map[string]WorkItem{"first":item}),window([2]WorkItem{item,item})}{data,err:=encodeCommand(value);if err!=nil{t.Fatal(err)};got,err:=serdejson.Unmarshal(data,DeserializeCommand);if err!=nil||!reflect.DeepEqual(got,value){t.Fatalf("roundtrip %s: %#v %v",data,got,err)}}}
func TestEnumCloneOwnsContainers(t *testing.T){item:=sample();original:=submit(item);clone:=CloneCommand(&original);copied:=submitted(clone);copied.Retries[0]=9;copied.Labels["a"]="changed";if submitted(original).Retries[0]!=1||submitted(original).Labels["a"]!="first"{t.Fatal("struct payload clone shares storage")};shared:=[]int{2};originalBatch:=batch([]WorkItem{{Retries:shared},{Retries:shared}});copiedBatch:=CloneCommand(&originalBatch);items:=batchItems(copiedBatch);items[0].Retries[0]=8;if batchItems(originalBatch)[0].Retries[0]!=2||items[1].Retries[0]!=2{t.Fatal("clone preserves mutable storage or alias topology")};originalIndex:=indexed(map[string]WorkItem{"a":item});copiedIndex:=CloneCommand(&originalIndex);indexedItems(copiedIndex)["a"].Retries[1]=8;if indexedItems(originalIndex)["a"].Retries[1]!=2{t.Fatal("map payload clone shares storage")};originalWindow:=window([2]WorkItem{item,item});copiedWindow:=CloneCommand(&originalWindow);windowItems(copiedWindow)[0].Retries[0]=7;if windowItems(originalWindow)[0].Retries[0]!=1{t.Fatal("array payload clone shares storage")}}
func TestEnumCloneNilEmptyDistinction(t *testing.T){for _,items:=range [][]WorkItem{nil,{}}{value:=batch(items);copy:=batchItems(CloneCommand(&value));if (items==nil)!=(copy==nil){t.Fatal("slice nil/empty changed")}};for _,items:=range []map[string]WorkItem{nil,{}}{value:=indexed(items);copy:=indexedItems(CloneCommand(&value));if (items==nil)!=(copy==nil){t.Fatal("map nil/empty changed")}}}
func TestEnumDebugOrder(t *testing.T){value:=submit(sample());want:="Command::Submit(WorkItem { Name: \"mail\", Retries: [1, 2], Labels: {\"a\": \"first\", \"z\": \"last\"} })";for i:=0;i<10;i++{if got:=DebugCommand(&value);got!=want{t.Fatalf("debug=%q want=%q",got,want)}};unit:=idle();if DebugCommand(&unit)!="Command::Idle"{t.Fatal("unit Debug")}}
func TestEnumStructuredPathsAndBadInputs(t *testing.T){_,err:=serdejson.Unmarshal([]byte("{\"Submit\":{\"Name\":\"mail\",\"Retries\":[1,false],\"Labels\":{}}}"),DeserializeCommand);var detail *serde.Error;if !errors.As(err,&detail){t.Fatalf("missing structured error: %v",err)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Submit"},{Kind:"Field",Name:"Retries"},{Kind:"Index",Index:1}};if !reflect.DeepEqual(detail.Path,want){t.Fatalf("path=%#v",detail.Path)};for name,input:=range map[string]string{"null":"null","missing-field":"{\"Submit\":{\"Name\":\"x\"}}","wrong-container":"{\"Batch\":{}}","wrong-map-value":"{\"Indexed\":{\"a\":false}}","array-length":"{\"Window\":[]}","duplicate-variant":"{\"Idle\":null,\"Idle\":null}","unknown":"\"Unknown\""}{t.Run(name,func(t *testing.T){if _,err:=serdejson.Unmarshal([]byte(input),DeserializeCommand);err==nil{t.Fatalf("accepted %s",input)}})};bad:=sample();bad.Name=string([]byte{255});_,err=encodeCommand(submit(bad));if !errors.As(err,&detail)||!reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Variant",Name:"Submit"},{Kind:"Field",Name:"Name"}}){t.Fatalf("encoding path: %v",err)}}
type pointerError struct{}
func(*pointerError)Error()string{return "typed nil"}
func TestEnumErrorFromAndPropagation(t *testing.T){good:=accepted(loadCommand([]byte("{\"Submit\":{\"Name\":\"mail\",\"Retries\":[],\"Labels\":{}}}")));if submitted(good).Name!="mail"{t.Fatal("success propagation")};err:=rejected(loadCommand([]byte("{")));var detail *serde.Error;if err==nil||err.Error()!="could not load jobs"||!errors.As(err,&detail){t.Fatalf("decode error chain: %v",err)};business:=rejected(loadCommand([]byte("{\"Batch\":[]}")));if business==nil||business.Error()!="invalid job count 0"{t.Fatalf("business classification: %v",business)};sentinel:=errors.New("sentinel");if errors.Unwrap(remote(sentinel))!=sentinel||missing().Error()!="missing job"||errors.Unwrap(missing())!=nil{t.Fatal("explicit source/unit error")};wrapped:=FromAppError(sentinel);if errors.Unwrap(wrapped)!=sentinel||!errors.Is(wrapped,sentinel){t.Fatal("From lost source identity")};var typed *pointerError;typedWrapped:=FromAppError(typed);var found *pointerError;if !errors.As(typedWrapped,&found)||found!=nil||errors.Unwrap(typedWrapped)!=error(typed){t.Fatal("typed nil collapsed")};nilWrapped:=FromAppError(nil);if nilWrapped==nil||nilWrapped.Error()!="could not load jobs"||errors.Unwrap(nilWrapped)!=nil{t.Fatal("nil input erased business variant")}}
func TestPropagationPreservesStaticClassification(t *testing.T){sentinel:=errors.New("remote");original:=remote(sentinel);wrapped:=failureValue(failWith(original));if errorKind(wrapped)!="input"||!errors.Is(wrapped,sentinel)||errors.Unwrap(wrapped)!=error(original){t.Fatal("dynamic error guessed or chain normalized")};var typed *pointerError;typedWrapped:=failureValue(failWith(typed));if errorKind(typedWrapped)!="input"||errors.Unwrap(typedWrapped)!=error(typed){t.Fatal("propagation collapsed typed nil")}}
func TestContainerPayloadErrorPaths(t *testing.T){for _,c:=range []struct{input string;path []serde.PathSegment}{
 {"{\"Batch\":[{\"Name\":false,\"Retries\":[],\"Labels\":{}}]}",[]serde.PathSegment{{Kind:"Variant",Name:"Batch"},{Kind:"Index",Index:0},{Kind:"Field",Name:"Name"}}},
 {"{\"Indexed\":{\"west\":{\"Name\":false,\"Retries\":[],\"Labels\":{}}}}",[]serde.PathSegment{{Kind:"Variant",Name:"Indexed"},{Kind:"MapKey",Name:"west"},{Kind:"Field",Name:"Name"}}},
 }{_,err:=serdejson.Unmarshal([]byte(c.input),DeserializeCommand);var detail *serde.Error;if !errors.As(err,&detail)||!reflect.DeepEqual(detail.Path,c.path){t.Fatalf("path for %s: %v",c.input,err)}}}
func TestStructAttributesInsideEnumPayload(t *testing.T){value,err:=serdejson.Unmarshal([]byte("{\"Change\":{\"id\":\"request\"}}"),DeserializeTaggedCommand);if err!=nil{t.Fatal(err)};if got:=taggedItem(value);got.ID!="request"||got.Attempts!=0{t.Fatalf("field attributes lost: %#v",got)};data,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeTaggedCommand(s,&value)});if err!=nil||string(data)!="{\"Change\":{\"id\":\"request\",\"Attempts\":0}}"{t.Fatalf("wire=%s err=%v",data,err)}}
type forgedCommand struct{Command}
func TestInvalidEnumsAtDeriveBoundary(t *testing.T){for _,value:=range []Command{nil,(*COMMAND_IDLE_CASE)(nil),&COMMAND_IDLE_CASE{},forgedCommand{}}{if _,err:=encodeCommand(value);err==nil{t.Fatalf("encoded invalid %T",value)};for name,run:=range map[string]func(){"debug":func(){_=DebugCommand(&value)},"clone":func(){_=CloneCommand(&value)}}{func(){defer func(){if recover()==nil{t.Errorf("%s accepted invalid %T",name,value)}}();run()}()}}}
`

func TestEnumDerivesExplicitRejections(t *testing.T) {
	cases := map[string]string{
		"recursive-struct-payload":     "enum E{Value(Item),}\ntype Item struct{Loop E}",
		"message-without-error-derive": "enum E{\n#[error(\"oops\")]\nA,\n}",
		"source-without-error-derive":  "enum E{\n#[source]\nA(int),\n}",
		"from-without-error-derive":    "enum E{\n#[from]\nA(int),\n}",
		"clone-pointer":                "#[derive(Clone)]\nenum E{Value(*int),}",
		"clone-no-nested-protocol":     "type Item struct{N int}\n#[derive(Clone)]\nenum E{Value(Item),}",
		"debug-no-nested-protocol":     "type Item struct{N int}\n#[derive(Debug)]\nenum E{Value(Item),}",
		"serde-no-nested-protocol":     "type Item struct{N int}\n#[derive(Serialize)]\nenum E{Value(Item),}",
		"error-missing-message":        "#[derive(Error)]\nenum E{Value(int),}",
		"source-not-error":             "#[derive(Error)]\nenum E{\n#[error(\"bad\")]\n#[source]\nValue(int),\n}",
		"from-not-error":               "#[derive(Error)]\nenum E{\n#[error(\"bad\")]\n#[from]\nValue(int),\n}",
		"ambiguous-from":               "#[derive(Error)]\nenum E{\n#[error(\"a\")]\n#[from]\nA(error),\n#[error(\"b\")]\n#[from]\nB(error),\n}",
		"duplicate-from-attribute":     "#[derive(Error)]\nenum E{\n#[error(\"a\")]\n#[from]\n#[from]\nA(error),\n}",
		"duplicate-source-attribute":   "#[derive(Error)]\nenum E{\n#[error(\"a\")]\n#[source]\n#[source]\nA(error),\n}",
		"source-unit":                  "#[derive(Error)]\nenum E{\n#[error(\"a\")]\n#[source]\nA,\n}",
		"unknown-placeholder":          "#[derive(Error)]\nenum E{\n#[error(\"count {Missing}\")]\nCount(int),\n}",
		"source-is-not-from":           "#[derive(Error)]\nenum E{\n#[error(\"a\")]\n#[source]\nA(error),\n}\nfunc f()Result[int,E]{n:=Result[int,error]::Err(nil)?;return Result[int,E]::Ok(n)}",
		"no-record-syntax":             "enum E{Value{N int},}",
		"no-tuple-syntax":              "enum E{Value(int,string),}",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("enum_derives_bad.hgo", []byte("package fixture\n"+body))
			if err == nil || !strings.Contains(err.Error(), "enum_derives_bad.hgo") {
				t.Fatalf("expected located rejection: %v", err)
			}
			t.Log(err)
		})
	}
}

func TestEnumDerivesNestedValidationRace(t *testing.T) {
	source := `package fixture
#[derive(Serialize, Deserialize, Debug, Clone)]
enum Phase { Ready, }
#[derive(Serialize, Deserialize, Debug, Clone)]
type Detail struct { Phase Phase }
#[derive(Serialize, Deserialize, Debug, Clone)]
enum Packet { Data(Detail), }
#[derive(Serialize, Deserialize, Debug, Clone)]
enum Bundle { Items([]Phase), Indexed(map[string]Phase), }
func bundleItems(values []Phase)Bundle{return Bundle::Items(values)}
func bundleIndex(values map[string]Phase)Bundle{return Bundle::Indexed(values)}
func readyPhase()Phase{return Phase::Ready}
func observeBundle(value Bundle,hit *bool){
 mark:=func()bool{*hit=true;return true}
 match value {Bundle::Items(_) if mark()=>{},Bundle::Indexed(_) if mark()=>{},_=>{},}
}
func valid() Packet {return Packet::Data(Detail{Phase:Phase::Ready})}
func invalid() Packet {return Packet::Data(Detail{})}
func observe(value Packet,hit *bool) {
 mark:=func()bool{*hit=true;return true}
 match value {Packet::Data(_) if mark()=>{},_=>{},}
}
`
	enumGeneratedModule(t, source, `package fixture
import("testing";"errors";"reflect";"github.com/hxsf/goxide/serde";"github.com/hxsf/goxide/serdejson")
func TestNestedValueDerives(t *testing.T){value:=valid();clone:=ClonePacket(&value);if !reflect.DeepEqual(clone,value)||DebugPacket(&clone)!="Packet::Data(Detail { Phase: Phase::Ready })"{t.Fatal("nested value derive")}}
func TestInvalidNestedEnumBeforeEffects(t *testing.T){value:=invalid();hit:=false;for name,run:=range map[string]func(){"clone":func(){_=ClonePacket(&value)},"debug":func(){_=DebugPacket(&value)},"guard":func(){observe(value,&hit)}}{func(){defer func(){if recover()==nil{t.Errorf("%s accepted invalid nested enum",name)}}();run()}()};if hit{t.Fatal("guard ran before nested enum validation")}}
func TestNestedEnumContainerDerives(t *testing.T){
 for _,v:=range []Bundle{bundleItems(nil),bundleItems([]Phase{}),bundleItems([]Phase{readyPhase()}),bundleIndex(nil),bundleIndex(map[string]Phase{}),bundleIndex(map[string]Phase{"first":readyPhase()})}{
  hit:=false;observeBundle(v,&hit);if !hit{t.Fatal("valid container did not match")};copy:=CloneBundle(&v);if !reflect.DeepEqual(v,copy)||DebugBundle(&v)==""{t.Fatal("container derive")}
 }
 // JSON canonicalizes nil containers to empty; exact roundtrip applies to nonnil containers.
 for _,v:=range []Bundle{bundleItems([]Phase{}),bundleItems([]Phase{readyPhase()}),bundleIndex(map[string]Phase{}),bundleIndex(map[string]Phase{"first":readyPhase()})}{
  data,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeBundle(s,&v)});if err!=nil{t.Fatal(err)};decoded,err:=serdejson.Unmarshal(data,DeserializeBundle);if err!=nil||!reflect.DeepEqual(v,decoded){t.Fatalf("container roundtrip %s: %#v %v",data,decoded,err)}
 }
}
func TestInvalidNestedContainerBeforeEffects(t *testing.T){
 var typed * __hgo_enum_case_5_Phase_Ready
 for _,bad:=range []Phase{nil,typed}{
  for _,tc:=range []struct{value Bundle;path []serde.PathSegment}{
   {bundleItems([]Phase{bad}),[]serde.PathSegment{{Kind:"Variant",Name:"Items"},{Kind:"Index",Index:0}}},
   {bundleIndex(map[string]Phase{"bad":bad}),[]serde.PathSegment{{Kind:"Variant",Name:"Indexed"},{Kind:"MapKey",Name:"bad"}}},
  }{
   value:=tc.value;hit:=false
   for name,run:=range map[string]func(){"match":func(){observeBundle(value,&hit)},"debug":func(){_=DebugBundle(&value)},"clone":func(){_=CloneBundle(&value)}}{func(){defer func(){if recover()==nil{t.Errorf("%s accepted invalid nested container element %T",name,bad)}}();run()}()}
   if hit{t.Fatal("container guard ran before validation")}
   _,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializeBundle(s,&value)});var detail *serde.Error;if !errors.As(err,&detail)||!reflect.DeepEqual(detail.Path,tc.path){t.Fatalf("container error path want %#v: %v",tc.path,err)}
  }
 }
}
func TestNestedEnumEncodingPath(t *testing.T){value:=invalid();_,err:=serdejson.Encode(func(s serde.Serializer)error{return SerializePacket(s,&value)});var detail *serde.Error;want:=[]serde.PathSegment{{Kind:"Variant",Name:"Data"},{Kind:"Field",Name:"Phase"}};if !errors.As(err,&detail)||!reflect.DeepEqual(detail.Path,want){t.Fatalf("path=%v",err)}}
`, false, true)
}
