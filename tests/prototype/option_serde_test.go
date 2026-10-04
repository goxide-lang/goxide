package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"testing"
)

func TestOptionSerdePayloadNamesDoNotCaptureHelpers(t *testing.T) {
	r, err := frontend.Compile("option-names.gox", []byte(`package fixture
#[derive(Serialize,Deserialize)]
type d struct { N int }
#[derive(Serialize,Deserialize)]
type v struct { N int }
#[derive(Serialize,Deserialize)]
type e struct { N int }
#[derive(Serialize,Deserialize)]
type s struct { N int }
#[derive(Serialize,Deserialize)]
type x struct { N int }
#[derive(Serialize,Deserialize)]
type Request struct {
 Direct Option[s]
 Switched Option[x]
 Values []Option[d]
 Lookup map[string]Option[v]
 Errors []Option[e]
}
`))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, r.Go, `package fixture
import("testing";"reflect";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestRoundTrip(t *testing.T) {
 input := "{\"Direct\":{\"N\":1},\"Values\":[null,{\"N\":2}],\"Lookup\":{\"x\":{\"N\":3}},\"Errors\":[{\"N\":4}]}"
 original,err:=serdejson.Unmarshal([]byte(input),DeserializeRequest);if err!=nil{t.Fatal(err)}
 wire,err:=serdejson.Encode(func(out serde.Serializer)error{return SerializeRequest(out,&original)});if err!=nil{t.Fatal(err)}
 restored,err:=serdejson.Unmarshal(wire,DeserializeRequest);if err!=nil||!reflect.DeepEqual(original,restored){t.Fatalf("%s %v",wire,err)}
}
`)
}
