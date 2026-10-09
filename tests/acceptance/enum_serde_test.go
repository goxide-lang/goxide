package acceptance_test

import (
	"strings"
	"testing"
)

const enumSerdeSource = `package fixture; use serde;
#[derive(serde::Serialize, serde::Deserialize)]
enum Event { Idle, Item(int), Text(string), }
#[derive(serde::Serialize, serde::Deserialize)]
enum Envelope { Empty, Nested(Event), }
#[derive(serde::Serialize, serde::Deserialize)]
type Batch struct { Events []Event; Lookup map[string]Envelope; Maybe *Event }
func idle() Event {return Event::Idle}
func item(n int) Event {return Event::Item(n)}
func text(s string) Event {return Event::Text(s)}
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
	foundIdle := false
	for _, d := range result.Enums {
		for _, v := range d.Variants {
			if d.Name == "Event" && v.Name == "Idle" {
				foundIdle = true
			}
		}
	}
	if !foundIdle {
		t.Fatal("missing enum metadata")
	}
	enumGeneratedModule(t, enumSerdeSource, enumSerdeAssertions, false)
}

const enumSerdeAssertions = `package fixture
import("testing";"reflect";"errors";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func enc(v Event)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeEvent(s,&v)})}
func dec(s string)(Event,error){return serdejson.Unmarshal([]byte(s),DeserializeEvent)}
func TestEnumWireAndRoundTrip(t *testing.T){for _,c:=range []struct{value Event;wire string}{{idle(),"\"Idle\""},{item(42),"{\"Item\":42}"},{text("hello"),"{\"Text\":\"hello\"}"}}{out,e:=enc(c.value);if e!=nil||string(out)!=c.wire{t.Fatalf("encode %T: %s %v",c.value,out,e)};v,e:=dec(c.wire);if e!=nil||!reflect.DeepEqual(v,c.value){t.Fatalf("roundtrip: %#v %v",v,e)}}}
func TestEnumContainers(t *testing.T){value:=item(8);v:=Batch{Events:[]Event{idle(),value,text("x")},Lookup:map[string]Envelope{"a":nested(text("z"))},Maybe:&value};out,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeBatch(s,&v)});if e!=nil{t.Fatal(e)};got,e:=serdejson.Unmarshal(out,DeserializeBatch);if e!=nil||!reflect.DeepEqual(v,got){t.Fatalf("batch %s %#v %v",out,got,e)}}
func TestEnumInvalidInput(t *testing.T){cases:=map[string]string{"unknown-unit":"\"Nope\"","unknown-payload":"{\"Nope\":1}","payload-wrong":"{\"Item\":\"x\"}","payload-null":"{\"Item\":null}","unit-payload":"{\"Idle\":null}","missing-payload":"\"Item\"","null":"null","multi":"{\"Item\":1,\"Text\":\"a\"}","duplicate":"{\"Item\":1,\"Item\":2}","trailing":"\"Idle\" null","empty":"{}"};for name,input:=range cases{t.Run(name,func(t *testing.T){if _,e:=dec(input);e==nil{t.Fatalf("accepted %s",input)}})}}
func TestEnumVariantErrorPath(t *testing.T){_,e:=serdejson.Unmarshal([]byte("{\"Nested\":{\"Item\":false}}"),DeserializeEnvelope);var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Item"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumEncodingVariantPath(t *testing.T){v:=nested(text(string([]byte{255})));_,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEnvelope(s,&v)});var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed encoding error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Text"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("encoding path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumUnknownCategory(t *testing.T){for _,input:=range []string{"\"Nope\"","{\"Nope\":1}"}{_,e:=dec(input);var typed *serde.Error;if !errors.As(e,&typed)||typed.Kind!=serde.UnknownVariant{t.Fatalf("unknown variant category: %v",e)}}}
func TestEnumInvalidGoBoundaryReturnsError(t *testing.T){for _,v:=range []Event{{}}{if _,e:=enc(v);e==nil{t.Fatalf("accepted invalid %T",v)}};if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEvent(s,nil)});e==nil{t.Fatal("nil input pointer accepted")};v:=nested(Event{});if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeEnvelope(s,&v)});e==nil{t.Fatal("invalid nested enum accepted")}}
`

func TestEnumSerdeIndependentDirectionsRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture; use serde;
#[derive(serde::Serialize)]
enum Out { Empty, Value(int), }
#[derive(serde::Serialize)]
enum OuterOut { Nested(Out), }
#[derive(serde::Deserialize)]
enum In { Empty, Value(int), }
#[derive(serde::Deserialize)]
enum OuterIn { Nested(In), }
func output() OuterOut {return OuterOut::Nested(Out::Value(3))}
`, `package fixture
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestDirectionOnly(t *testing.T){v:=output();out,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeOuterOut(s,&v)});if e!=nil{t.Fatal(e)};if string(out)!="{\"Nested\":{\"Value\":3}}"{t.Fatalf("wrong wire: %s",out)};if _,e:=serdejson.Unmarshal(out,DeserializeOuterIn);e!=nil{t.Fatal(e)}}
`, false)
}

func TestEnumSerdeUnsupportedAttributes(t *testing.T) {
	cases := map[string]string{
		"container-rename":  "#[derive(serde::Serialize)]\n#[serde(rename=\"Other\")]\nenum E {A,}",
		"container-unknown": "#[derive(serde::Deserialize)]\n#[serde(deny_unknown_fields)]\nenum E {A,}",
		"variant-alias":     "#[derive(serde::Serialize)]\nenum E {\n#[serde(alias=\"Other\")]\nA,}",
		"variant-skip":      "#[derive(serde::Serialize)]\nenum E {\n#[serde(skip)]\nA,}",
		"generic":           "#[derive(serde::Serialize)]\nenum E[T] {A(T),}",
		"pointer-payload":   "#[derive(serde::Serialize)]\nenum E {A(*int),}",
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
