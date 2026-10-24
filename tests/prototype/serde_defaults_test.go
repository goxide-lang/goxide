package prototype_test

import (
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestSerdeDefaultsDoNotCaptureBooleanNames(t *testing.T) {
	r, err := testdeps.FormalSource(t, "bool-defaults.gox", []byte(`package fixture; use serde; use std;
const true = (0 != 0)
const false = (0 == 0)
var calls int
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Settings struct { Port int }
impl std::Default for Settings { func r#default() Settings {calls++;return Settings{Port:8080}} }
#[derive(serde::derive::Deserialize)]
type Config struct {
 #[serde(default)]
 Value Settings
 #[serde(default)]
 Enabled bool
}
impl Config { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
`))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, r, `package fixture
import("testing";"github.com/goxide-lang/serde/json")
func TestBooleanShadowing(t *testing.T){
 calls=0
 v,e:=serdejson.Unmarshal([]byte("{}"),(ConfigOps{}).Decode)
 if e!=nil||v.Enabled||v.Value.Port!=8080||calls!=1{t.Fatalf("missing=%+v calls=%d err=%v",v,calls,e)}
 calls=0
 v,e=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":7},\"Enabled\":true}"),(ConfigOps{}).Decode)
 if e!=nil||!v.Enabled||v.Value.Port!=7||calls!=0{t.Fatalf("present=%+v calls=%d err=%v",v,calls,e)}
 _,e=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":7},\"Value\":{\"Port\":8}}"),(ConfigOps{}).Decode)
 if e==nil||calls!=0{t.Fatalf("duplicate=%v calls=%d",e,calls)}
}
`)
}
