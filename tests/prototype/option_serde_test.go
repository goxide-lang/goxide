package prototype_test

import (
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestOptionSerdePayloadNamesDoNotCaptureHelpers(t *testing.T) {
	r, err := testdeps.FormalSource(t, "option-names.gox", []byte(`package fixture; use serde;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type d struct { N int }
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type v struct { N int }
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type e struct { N int }
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type s struct { N int }
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type x struct { N int }
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Request struct {
 Direct Option[s]
 Switched Option[x]
 Values []Option[d]
 Lookup map[string]Option[v]
 Errors []Option[e]
}
impl Request {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, r, `package fixture
import("testing";"reflect";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestRoundTrip(t *testing.T) {
 input := "{\"Direct\":{\"N\":1},\"Values\":[null,{\"N\":2}],\"Lookup\":{\"x\":{\"N\":3}},\"Errors\":[{\"N\":4}]}"
 original,err:=serdejson.Unmarshal([]byte(input),(RequestOps{}).Decode);if err!=nil{t.Fatal(err)}
 wire,err:=serdejson.Encode(func(out serde.Serializer)error{return (RequestOps{}).Encode(out,&original)});if err!=nil{t.Fatal(err)}
 restored,err:=serdejson.Unmarshal(wire,(RequestOps{}).Decode);if err!=nil||!reflect.DeepEqual(original,restored){t.Fatalf("%s %v",wire,err)}
}
`)
}
