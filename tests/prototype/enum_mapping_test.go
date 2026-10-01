package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestClosedEnumWireMapping(t *testing.T) {
	r, e := frontend.Compile("wire.hgo", []byte(`package fixture
#[derive(Serialize, Deserialize, Clone, Debug, Default)]
enum Status {
 #[default]
 #[serde(rename="pending")]
 Pending,
 #[serde(rename="paid")]
 Paid,
}
#[derive(Serialize, Deserialize)]
enum Code {
 #[serde(value=-9223372036854775808)]
 Min,
 #[serde(value=0)]
 Zero,
 #[serde(value=9223372036854775807)]
 Max,
}
#[derive(Serialize)]
enum Output { #[serde(value=9)] Done }
#[derive(Deserialize)]
enum Input { #[serde(rename="received")] Received }
#[derive(Serialize, Deserialize)]
struct Envelope { Code Code }
func PendingStatus() Status {return Status::Pending}
func MaxCode() Code {return Code::Max}
func OutputValue() Output{return Output::Done}
`))
	if e != nil {
		t.Fatal(e)
	}
	runGenerated(t, r.Go, `package fixture
import("errors";"testing";"github.com/hxsf/goxide/serde";"github.com/hxsf/goxide/serdejson")
type forgedCode struct { Code }
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
 invalidForged:=Code(forgedCode{Code:MaxCode()});_,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,&invalidForged)});if e==nil{t.Fatal("embedded foreign case accepted")}
 var invalid Code;_,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,&invalid)});if e==nil{t.Fatal("nil enum accepted")}
 var typedNil *__hgo_enum_case_4_Code_Max;invalid=typedNil;_,e=serdejson.Encode(func(s serde.Serializer)error{return SerializeCode(s,&invalid)});if e==nil{t.Fatal("typed nil enum accepted")}
}
`)
}

func TestClosedEnumWireMappingRejects(t *testing.T) {
	cases := []struct{ body, want string }{
		{`#[derive(Serialize)]
enum E { #[serde(value=1)] A, #[serde(value=1)] B }`, "duplicate enum serde mapping"},
		{`#[derive(Deserialize)]
enum E { #[serde(rename="same")] A, #[serde(rename="same")] B }`, "duplicate enum serde mapping"},
		{`#[derive(Deserialize)]
enum E { #[serde(rename="B")] A, B }`, "duplicate enum serde mapping"},
		{`#[derive(Serialize)]
enum E { #[serde(value=1)] A, B }`, "explicit value on every variant"},
		{`#[derive(Serialize)]
enum E { #[serde(value=1)] A, #[serde(rename="b")] B }`, "cannot mix"},
		{`#[derive(Serialize)]
enum E { #[serde(value=1)] A(int) }`, "no payload"},
		{`#[derive(Serialize)]
enum E { #[serde(rename="a")] A, B(int) }`, "no payload"},
		{`enum E { #[serde(value=1)] A }`, "require Serialize or Deserialize"},
		{`#[derive(Serialize)]
enum E { #[serde(value=9223372036854775808)] A }`, "int64 range"},
		{`#[derive(Serialize)]
enum E { #[serde(value=-9223372036854775809)] A }`, "int64 range"},
		{`#[derive(Serialize)]
enum E { #[serde(value=1.5)] A }`, "decimal literal"},
		{`#[derive(Serialize)]
enum E { #[serde(skip)] A }`, "requires rename"},
		{`#[derive(Serialize)]
enum E { #[serde(rename="x")] #[serde(value=1)] A }`, "mixed variant"},
	}
	for _, tc := range cases {
		_, e := frontend.Compile("invalid-wire.hgo", []byte("package fixture\n"+tc.body+"\n"))
		if e == nil || !strings.Contains(e.Error(), tc.want) || !strings.Contains(e.Error(), "invalid-wire.hgo") {
			t.Errorf("want %s, got %v", tc.want, e)
		}
	}
}
