package prototype_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"testing"
)

func TestSerdeDefaultsDoNotCaptureBooleanNames(t *testing.T) {
	r, err := frontend.Compile("bool-defaults.gox", []byte(`package fixture
const true = (0 != 0)
const false = (0 == 0)
var calls int
#[derive(Serialize,Deserialize)]
type Settings struct { Port int }
impl Default for Settings { func default() Settings {calls++;return Settings{Port:8080}} }
#[derive(Deserialize)]
type Config struct {
 #[serde(default)]
 Value Settings
 #[serde(default)]
 Enabled bool
}
`))
	if err != nil {
		t.Fatal(err)
	}
	runGenerated(t, r.Go, `package fixture
import("testing";"github.com/goxide-lang/serde/json")
func TestBooleanShadowing(t *testing.T){
 calls=0
 v,e:=serdejson.Unmarshal([]byte("{}"),DeserializeConfig)
 if e!=nil||v.Enabled||v.Value.Port!=8080||calls!=1{t.Fatalf("missing=%+v calls=%d err=%v",v,calls,e)}
 calls=0
 v,e=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":7},\"Enabled\":true}"),DeserializeConfig)
 if e!=nil||!v.Enabled||v.Value.Port!=7||calls!=0{t.Fatalf("present=%+v calls=%d err=%v",v,calls,e)}
 _,e=serdejson.Unmarshal([]byte("{\"Value\":{\"Port\":7},\"Value\":{\"Port\":8}}"),DeserializeConfig)
 if e==nil||calls!=0{t.Fatalf("duplicate=%v calls=%d",e,calls)}
}
`)
}
