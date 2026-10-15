package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const enumSerdeSource = `package fixture; use serde;
#[derive(serde::Serialize, serde::Deserialize)]
pub enum Event { Idle, Item(int), Text(string), Optional(Option[int]), OptionalRecord(OptionalPayload), }
#[derive(serde::Serialize, serde::Deserialize)]
type OptionalPayload struct { Value Option[int] }
#[derive(serde::Serialize, serde::Deserialize)]
pub enum Envelope { Empty, Nested(Event), }
#[derive(serde::Serialize, serde::Deserialize)]
type Batch struct { Events []Event; Lookup map[string]Envelope; Maybe *Event }
func idle() Event {return Event::Idle}
func item(n int) Event {return Event::Item(n)}
func text(s string) Event {return Event::Text(s)}
func optional(n int) Event {return Event::Optional(Option[int]::Some(n))}
func optionalRecord(n int) Event {return Event::OptionalRecord(OptionalPayload{Value:Option[int]::Some(n)})}
func nested(v Event) Envelope {return Envelope::Nested(v)}
`

func TestEnumSerdeBusinessRace(t *testing.T) {
	result, err := compileAcceptanceSerdeFixture("enum_serde.gox", []byte(enumSerdeSource))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Go), "encoding/json") || strings.Contains(string(result.Go), "serdejson") {
		t.Fatal("derive depends on JSON backend")
	}
	enumGeneratedModule(t, enumSerdeSource, enumSerdeAssertions, false)
}

const enumSerdeAssertions = `package fixture
import("testing";"reflect";"errors";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func enc(v Event)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeEvent(s,&v)})}
func dec(s string)(Event,error){return serdejson.Unmarshal([]byte(s),DeserializeEvent)}
func TestEnumWireAndRoundTrip(t *testing.T){for _,c:=range []struct{value Event;wire string}{{idle(),"\"Idle\""},{item(42),"{\"Item\":42}"},{text("hello"),"{\"Text\":\"hello\"}"},{optional(5),"{\"Optional\":5}"},{optionalRecord(8),"{\"OptionalRecord\":{\"Value\":8}}"}}{out,e:=enc(c.value);if e!=nil||string(out)!=c.wire{t.Fatalf("encode %T: %s %v",c.value,out,e)};v,e:=dec(c.wire);if e!=nil||!reflect.DeepEqual(v,c.value){t.Fatalf("roundtrip: %#v %v",v,e)}}}
func TestEnumContainers(t *testing.T){value:=item(8);v:=Batch{Events:[]Event{idle(),value,text("x")},Lookup:map[string]Envelope{"a":nested(text("z"))},Maybe:&value};out,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeBatch(s,&v)});if e!=nil{t.Fatal(e)};got,e:=serdejson.Unmarshal(out,DeserializeBatch);if e!=nil||!reflect.DeepEqual(v,got){t.Fatalf("batch %s %#v %v",out,got,e)}}
func TestEnumInvalidInput(t *testing.T){cases:=map[string]string{"unknown-unit":"\"Nope\"","unknown-payload":"{\"Nope\":1}","payload-wrong":"{\"Item\":\"x\"}","payload-null":"{\"Item\":null}","unit-payload":"{\"Idle\":null}","missing-payload":"\"Item\"","null":"null","multi":"{\"Item\":1,\"Text\":\"a\"}","duplicate":"{\"Item\":1,\"Item\":2}","trailing":"\"Idle\" null","empty":"{}"};for name,input:=range cases{t.Run(name,func(t *testing.T){if _,e:=dec(input);e==nil{t.Fatalf("accepted %s",input)}})}}
func TestEnumVariantErrorPath(t *testing.T){_,e:=serdejson.Unmarshal([]byte("{\"Nested\":{\"Item\":false}}"),DeserializeEnvelope);var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Item"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumEncodingVariantPath(t *testing.T){v:=nested(text(string([]byte{255})));_,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEnvelope(s,&v)});var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed encoding error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Text"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("encoding path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumUnknownCategory(t *testing.T){for _,input:=range []string{"\"Nope\"","{\"Nope\":1}"}{_,e:=dec(input);var typed *serde.Error;if !errors.As(e,&typed)||typed.Kind!=serde.UnknownVariant{t.Fatalf("unknown variant category: %v",e)}}}
func TestEnumInvalidGoBoundaryReturnsError(t *testing.T){for _,v:=range []Event{nil}{if _,e:=enc(v);e==nil{t.Fatalf("accepted invalid %T",v)}};if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEvent(s,nil)});e==nil{t.Fatal("nil input pointer accepted")};v:=nested(nil);if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEnvelope(s,&v)});e==nil{t.Fatal("invalid nested enum accepted")}}
`

func TestEnumSerdeIndependentDirectionsRace(t *testing.T) {
	generated := t.TempDir()
	packagesWrite(t, generated, "go.mod", "module enumdirections.example/fixture\ngo 1.27\n")
	for _, fixture := range []struct{ name, source string }{
		{"encode", `package encode; use serde;
#[derive(serde::Serialize)]
pub enum Out { Empty, Value(int) }
#[derive(serde::Serialize)]
pub enum OuterOut { Nested(Out) }
func Output() OuterOut { return OuterOut::Nested(Out::Value(3)) }
`},
		{"decode", `package decode; use serde;
#[derive(serde::Deserialize)]
pub enum In { Empty, Value(int) }
#[derive(serde::Deserialize)]
pub enum OuterIn { Nested(In) }
`},
	} {
		result, err := compileAcceptanceSerdeFixture(fixture.name+".gox", []byte(fixture.source))
		if err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, generated, fixture.name+"/types.go", string(result.Go))
	}
	packagesWrite(t, generated, "directions_test.go", `package fixture
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";"enumdirections.example/fixture/encode";"enumdirections.example/fixture/decode")
func TestDirectionOnly(t *testing.T){v:=encode.Output();out,e:=serdejson.Encode(func(s serde.Serializer)error{return encode.SerializeOuterOut(s,&v)});if e!=nil{t.Fatal(e)};if string(out)!="{\"Nested\":{\"Value\":3}}"{t.Fatalf("wrong wire: %s",out)};if _,e:=serdejson.Unmarshal(out,decode.DeserializeOuterIn);e!=nil{t.Fatal(e)}}
`)
	packagesCommand(t, generated, filepath.Join(runtime.GOROOT(), "bin/go"), true, "test", "-race", "-count=1", "./...")
}

func TestEnumSerdeUnsupportedAttributes(t *testing.T) {
	cases := map[string]string{
		"container-rename":  "#[derive(serde::Serialize)]\n#[serde(rename=\"Other\")]\nenum E {A,}",
		"container-unknown": "#[derive(serde::Deserialize)]\n#[serde(deny_unknown_fields)]\nenum E {A,}",
		"variant-alias":     "#[derive(serde::Serialize)]\nenum E {\n#[serde(alias=\"Other\")]\nA,}",
		"variant-skip":      "#[derive(serde::Serialize)]\nenum E {\n#[serde(skip)]\nA,}",
		"generic":           "#[derive(serde::Serialize)]\nenum E[T] {A(T),}",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceSerdeFixture("enum_attrs.gox", []byte("package fixture; use serde;\n"+source))
			if err == nil || !strings.Contains(err.Error(), "enum_attrs.gox") {
				t.Fatalf("need explicit located rejection: %v", err)
			}
		})
	}
}
