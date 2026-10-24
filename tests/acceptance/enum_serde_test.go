package acceptance_test

import (
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

const enumSerdeSource = `package fixture; use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
pub enum Event { Idle, Item(int), Text(string), Optional(Option[int]), OptionalRecord(OptionalPayload), }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type OptionalPayload struct { Value Option[int] }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
pub enum Envelope { Empty, Nested(Event), }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Batch struct { Events []Event; Lookup map[string]Envelope; Maybe *Event }
func idle() Event {return Event::Idle}
func item(n int) Event {return Event::Item(n)}
func text(s string) Event {return Event::Text(s)}
func optional(n int) Event {return Event::Optional(Option[int]::Some(n))}
func optionalRecord(n int) Event {return Event::OptionalRecord(OptionalPayload{Value:Option[int]::Some(n)})}
func nested(v Event) Envelope {return Envelope::Nested(v)}
impl Event {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
impl Envelope {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
impl Batch {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`

func TestEnumSerdeBusinessRace(t *testing.T) {
	result, err := testdeps.FormalSource(t, "enum_serde.gox", []byte(enumSerdeSource))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.Go), "encoding/json") || strings.Contains(string(result.Go), "serdejson") {
		t.Fatal("derive depends on JSON backend")
	}
	runFormalSerdeRace(t, result, enumSerdeAssertions)
}

const enumSerdeAssertions = `package fixture
import("testing";"reflect";"errors";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func enc(v Event)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return (EventOps{}).Encode(s,&v)})}
func dec(s string)(Event,error){return serdejson.Unmarshal([]byte(s),(EventOps{}).Decode)}
func TestEnumWireAndRoundTrip(t *testing.T){for _,c:=range []struct{value Event;wire string}{{idle(),"\"Idle\""},{item(42),"{\"Item\":42}"},{text("hello"),"{\"Text\":\"hello\"}"},{optional(5),"{\"Optional\":5}"},{optionalRecord(8),"{\"OptionalRecord\":{\"Value\":8}}"}}{out,e:=enc(c.value);if e!=nil||string(out)!=c.wire{t.Fatalf("encode %T: %s %v",c.value,out,e)};v,e:=dec(c.wire);if e!=nil||!reflect.DeepEqual(v,c.value){t.Fatalf("roundtrip: %#v %v",v,e)}}}
func TestEnumContainers(t *testing.T){value:=item(8);v:=Batch{Events:[]Event{idle(),value,text("x")},Lookup:map[string]Envelope{"a":nested(text("z"))},Maybe:&value};out,e:=serdejson.Encode(func(s serde.Serializer)error{return (BatchOps{}).Encode(s,&v)});if e!=nil{t.Fatal(e)};got,e:=serdejson.Unmarshal(out,(BatchOps{}).Decode);if e!=nil||!reflect.DeepEqual(v,got){t.Fatalf("batch %s %#v %v",out,got,e)}}
func TestEnumInvalidInput(t *testing.T){cases:=map[string]string{"unknown-unit":"\"Nope\"","unknown-payload":"{\"Nope\":1}","payload-wrong":"{\"Item\":\"x\"}","payload-null":"{\"Item\":null}","unit-payload":"{\"Idle\":null}","missing-payload":"\"Item\"","null":"null","multi":"{\"Item\":1,\"Text\":\"a\"}","duplicate":"{\"Item\":1,\"Item\":2}","trailing":"\"Idle\" null","empty":"{}"};for name,input:=range cases{t.Run(name,func(t *testing.T){if _,e:=dec(input);e==nil{t.Fatalf("accepted %s",input)}})}}
func TestEnumVariantErrorPath(t *testing.T){_,e:=serdejson.Unmarshal([]byte("{\"Nested\":{\"Item\":false}}"),(EnvelopeOps{}).Decode);var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Item"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumEncodingVariantPath(t *testing.T){v:=nested(text(string([]byte{255})));_,e:=serdejson.Encode(func(s serde.Serializer)error{return (EnvelopeOps{}).Encode(s,&v)});var typed *serde.Error;if !errors.As(e,&typed){t.Fatalf("no typed encoding error: %v",e)};want:=[]serde.PathSegment{{Kind:"Variant",Name:"Nested"},{Kind:"Variant",Name:"Text"}};if !reflect.DeepEqual(typed.Path,want){t.Fatalf("encoding path %#v want %#v (%v)",typed.Path,want,e)}}
func TestEnumUnknownCategory(t *testing.T){for _,input:=range []string{"\"Nope\"","{\"Nope\":1}"}{_,e:=dec(input);var typed *serde.Error;if !errors.As(e,&typed)||typed.Kind!=serde.UnknownVariant{t.Fatalf("unknown variant category: %v",e)}}}
func TestEnumInvalidGoBoundaryReturnsError(t *testing.T){for _,v:=range []Event{nil}{if _,e:=enc(v);e==nil{t.Fatalf("accepted invalid %T",v)}};if _,e:=serdejson.Encode(func(s serde.Serializer)error{return (EventOps{}).Encode(s,nil)});e==nil{t.Fatal("nil input pointer accepted")};v:=nested(nil);if _,e:=serdejson.Encode(func(s serde.Serializer)error{return (EnvelopeOps{}).Encode(s,&v)});e==nil{t.Fatal("invalid nested enum accepted")}}
`

func TestEnumSerdeIndependentDirectionsRace(t *testing.T) {
	generated := t.TempDir()
	packagesWrite(t, generated, "goxide.toml", testdeps.FormalManifest(t, "enumdirections.example/fixture", "fixture"))
	packagesWrite(t, generated, "lib.gox", "package fixture; pub mod encode; pub mod decode;\n")
	for _, fixture := range []struct{ name, source string }{
		{"encode", `package encode; use serde;
#[derive(serde::derive::Serialize)]
pub enum Out { Empty, Value(int) }
#[derive(serde::derive::Serialize)]
pub enum OuterOut { Nested(Out) }
func Output() OuterOut { return OuterOut::Nested(Out::Value(3)) }
impl OuterOut { pub func encode(s serde::Serializer,value &Self) error { return serde::Write(s,value) } }
`},
		{"decode", `package decode; use serde;
#[derive(serde::derive::Deserialize)]
pub enum In { Empty, Value(int) }
#[derive(serde::derive::Deserialize)]
pub enum OuterIn { Nested(In) }
impl OuterIn { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
`},
	} {
		packagesWrite(t, generated, fixture.name+"/types.gox", fixture.source)
	}
	result, _, err := testdeps.PrepareFormal(t, generated)
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, result.Root, "directions_test.go", `package fixture
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json";"enumdirections.example/fixture/encode";"enumdirections.example/fixture/decode")
func TestDirectionOnly(t *testing.T){v:=encode.Output();out,e:=serdejson.Encode(func(s serde.Serializer)error{return (encode.OuterOutOps{}).Encode(s,&v)});if e!=nil{t.Fatal(e)};if string(out)!="{\"Nested\":{\"Value\":3}}"{t.Fatalf("wrong wire: %s",out)};if _,e:=serdejson.Unmarshal(out,(decode.OuterInOps{}).Decode);e!=nil{t.Fatal(e)}}
`)
	testdeps.RunFormalGo(t, result, "test", "-mod=readonly", "-race", "-count=1", "./...")
}

func TestEnumSerdeUnsupportedAttributes(t *testing.T) {
	cases := map[string]string{
		"container-rename":  "#[derive(serde::derive::Serialize)]\n#[serde(rename=\"Other\")]\nenum E {A,}",
		"container-unknown": "#[derive(serde::derive::Deserialize)]\n#[serde(deny_unknown_fields)]\nenum E {A,}",
		"variant-alias":     "#[derive(serde::derive::Serialize)]\nenum E {\n#[serde(alias=\"Other\")]\nA,}",
		"variant-skip":      "#[derive(serde::derive::Serialize)]\nenum E {\n#[serde(skip)]\nA,}",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := testdeps.FormalSource(t, "enum_attrs.gox", []byte("package fixture; use serde;\n"+source))
			if err == nil || !strings.Contains(err.Error(), "enum_attrs.gox") {
				t.Fatalf("need explicit located rejection: %v", err)
			}
		})
	}
}
