package prototype_test

import (
	"strings"
	"testing"
)

func TestClosedEnumWireMapping(t *testing.T) {
	r, e := compilePrototypeFixture("wire.gox", []byte(`package fixture; use serde;
#[derive(serde::Serialize, serde::Deserialize, Clone, Debug, Default)]
enum Status {
 #[default]
 #[serde(rename="pending")]
 Pending,
 #[serde(rename="paid")]
 Paid,
}
#[derive(serde::Serialize, serde::Deserialize)]
enum Code {
 #[serde(value=-9223372036854775808)]
 Min,
 #[serde(value=0)]
 Zero,
 #[serde(value=9223372036854775807)]
 Max,
}
#[derive(serde::Serialize)]
enum Output { #[serde(value=9)] Done }
#[derive(serde::Deserialize)]
enum Input { #[serde(rename="received")] Received }
#[derive(serde::Serialize, serde::Deserialize)]
struct Envelope { Code Code }
func PendingStatus() Status {return Status::Pending}
func MaxCode() Code {return Code::Max}
func OutputValue() Output{return Output::Done}
`))
	if e != nil {
		t.Fatal(e)
	}
	runGenerated(t, r.Go, `package fixture
import("errors";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func readCode(s string)(Code,error){var v Code;e:=serdejson.Decode([]byte(s),func(d serde.Deserializer)error{var e error;v,e=DeserializeCode(d);return e});return v,e}
func TestMapping(t *testing.T){
 for _,wire:=range []string{"-9223372036854775808","0","9223372036854775807"}{v,e:=readCode(wire);if e!=nil{t.Fatal(e)};got,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,&v)});if e!=nil||string(got)!=wire{t.Fatalf("numeric roundtrip: %s %v",got,e)}}
 var status Status;e:=serdejson.Decode([]byte("\"paid\""),func(d serde.Deserializer)error{var e error;status,e=DeserializeStatus(d);return e});if e!=nil{t.Fatal(e)}
 got,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeStatus(s,&status)});if e!=nil||string(got)!="\"paid\""{t.Fatalf("string mapping: %s %v",got,e)}
 if DebugStatus(&status)!="Status::Paid"{t.Fatal("wire rename changed Debug identity")}
 initial:=DefaultStatus();if DebugStatus(&initial)!="Status::Pending"{t.Fatal("default changed")}
 cloned:=CloneStatus(&status);if DebugStatus(&cloned)!="Status::Paid"{t.Fatal("Clone identity changed")}
 out:=OutputValue();got,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeOutput(s,&out)});if e!=nil||string(got)!="9"{t.Fatal("Serialize-only mapping failed")}
 e=serdejson.Decode([]byte("\"received\""),func(d serde.Deserializer)error{_,e:=DeserializeInput(d);return e});if e!=nil{t.Fatal(e)}
}
func TestUnknownAndMalformed(t *testing.T){
 for _,tc:=range []struct{wire,kind string}{{"7",serde.UnknownVariant},{"\"0\"",serde.Type},{"null",serde.Type},{"1.0",serde.Range},{"9223372036854775808",serde.Range}}{_,e:=readCode(tc.wire);var typed *serde.Error;if !errors.As(e,&typed)||typed.Kind!=tc.kind{t.Fatalf("%s: want %s got %v",tc.wire,tc.kind,e)}}
 e:=serdejson.Decode([]byte("\"future\""),func(d serde.Deserializer)error{_,e:=DeserializeStatus(d);return e});var typed *serde.Error;if !errors.As(e,&typed)||typed.Kind!=serde.UnknownVariant{t.Fatalf("unknown string: %v",e)}
 e=serdejson.Decode([]byte("{\"Code\":99}"),func(d serde.Deserializer)error{_,e:=DeserializeEnvelope(d);return e});if !errors.As(e,&typed)||typed.Kind!=serde.UnknownVariant||len(typed.Path)!=1||typed.Path[0].Name!="Code"{t.Fatalf("missing contextual path: %v",e)}
 for _,invalid:=range []Code{{},{__goxide_tag:-1},{__goxide_tag:99}}{_,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,&invalid)});if !errors.As(e,&typed)||typed.Kind!=serde.Type{t.Fatalf("invalid zero/forged enum tag accepted: %#v %v",invalid,e)}}
 var absent *Code;_,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,absent)});if !errors.As(e,&typed)||typed.Kind!=serde.Type{t.Fatalf("nil enum pointer accepted: %v",e)}
}
`)
}

func TestClosedEnumWireMappingRejects(t *testing.T) {
	cases := []struct{ body, want string }{
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=1)] A, #[serde(value=1)] B }`, "duplicate enum serde mapping"},
		{`#[derive(serde::Deserialize)]
enum E { #[serde(rename="same")] A, #[serde(rename="same")] B }`, "duplicate enum serde mapping"},
		{`#[derive(serde::Deserialize)]
enum E { #[serde(rename="B")] A, B }`, "duplicate enum serde mapping"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=1)] A, B }`, "explicit value on every variant"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=1)] A, #[serde(rename="b")] B }`, "cannot mix"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=1)] A(int) }`, "no payload"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(rename="a")] A, B(int) }`, "no payload"},
		{`enum E { #[serde(value=1)] A }`, "require Serialize or Deserialize"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=9223372036854775808)] A }`, "int64 range"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=-9223372036854775809)] A }`, "int64 range"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(value=1.5)] A }`, "decimal literal"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(skip)] A }`, "requires rename"},
		{`#[derive(serde::Serialize)]
enum E { #[serde(rename="x")] #[serde(value=1)] A }`, "mixed variant"},
	}
	for _, tc := range cases {
		header := "package fixture\n"
		if strings.Contains(tc.body, "serde::") {
			header = "package fixture; use serde;\n"
		}
		_, e := compilePrototypeFixture("invalid-wire.gox", []byte(header+tc.body+"\n"))
		if e == nil || !strings.Contains(e.Error(), tc.want) || !strings.Contains(e.Error(), "invalid-wire.gox") {
			t.Errorf("want %s, got %v", tc.want, e)
		}
	}
}
