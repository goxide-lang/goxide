package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSerdeStructDefaultsAndFieldValidation(t *testing.T) {
	module, err := supportModule(t, `package main
use serde;
use serde::json as json;
var trace string
type (
    #[derive(serde::Serialize, serde::Deserialize)]
    Setting struct { Value int }
)
impl Default for Setting {
    func default() Setting { trace += "D"; return Setting{Value:7} }
}
type Local struct { Value string }
impl Default for Local {
    func default() Local { trace += "S"; return Local{Value:"local"} }
}
#[derive(serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
type Record struct {
    #[serde(rename="id")]
    Required int
    #[serde(default)]
    First Setting
    #[serde(skip)]
    Hidden Local
    #[serde(default)]
    Second Setting
    Optional Option[int]
}
func main() {
    value,err := json::Unmarshal([]byte("{\"id\":2,\"Hidden\":{\"ignored\":1}}"),DeserializeRecord)
    println!("{} {} {} {} {} {}", value.Required, value.First.Value, value.Hidden.Value, value.Second.Value, trace, err==nil)
    data,err := json::Marshal(&value,SerializeRecord)
    println!("{} {}", string(data), err==nil)
    trace=""
    _,missing := json::Unmarshal([]byte("{}"),DeserializeRecord)
    _,duplicate := json::Unmarshal([]byte("{\"id\":1,\"id\":2}"),DeserializeRecord)
    _,unknown := json::Unmarshal([]byte("{\"id\":1,\"extra\":2}"),DeserializeRecord)
    println!("{} {} {} {:?}",missing!=nil,duplicate!=nil,unknown!=nil,trace)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	want := "2 7 local 7 DSD true\n{\"id\":2,\"First\":{\"Value\":7},\"Second\":{\"Value\":7},\"Optional\":null} true\ntrue true true \"\"\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("serde defaults output = %q, want %q", got, want)
	}
}

func TestSerdeContainersEnumsAndRanges(t *testing.T) {
	module, err := supportModule(t, `package main
use serde;
use serde::json as json;
#[derive(serde::Serialize, serde::Deserialize)]
enum Event { Idle, Count(int), }
#[derive(serde::Serialize, serde::Deserialize)]
enum Status { #[serde(value=-1)] Cold, #[serde(value=2)] Hot, }
#[derive(serde::Serialize, serde::Deserialize)]
type Record struct {
    Array [2]int
    Slice []int
    Map map[string]int
    Pointer *int
    Optional Option[int]
    Small int8
    Event Event
}
func main() {
    value,err := json::Unmarshal([]byte("{\"Array\":[1,2],\"Slice\":[],\"Map\":{},\"Pointer\":3,\"Optional\":4,\"Small\":127,\"Event\":{\"Count\":5}}"),DeserializeRecord)
    println!("{} {} {} {} {}",err==nil,value.Array[1],value.Slice!=nil,value.Map!=nil,*value.Pointer)
    data,err := json::Marshal(&value,SerializeRecord)
    println!("{} {}",string(data),err==nil)
    status,err := json::Unmarshal([]byte("-1"),DeserializeStatus)
    encoded,err := json::Marshal(&status,SerializeStatus)
    println!("{} {}",string(encoded),err==nil)
    var invalid Event
    _,nilError := json::Marshal(&invalid,SerializeEvent)
    _,rangeError := json::Unmarshal([]byte("{\"Array\":[1,2],\"Slice\":[],\"Map\":{},\"Pointer\":null,\"Small\":128,\"Event\":\"Idle\"}"),DeserializeRecord)
    _,lengthError := json::Unmarshal([]byte("{\"Array\":[1],\"Slice\":[],\"Map\":{},\"Pointer\":null,\"Small\":1,\"Event\":\"Idle\"}"),DeserializeRecord)
    println!("{} {} {}",nilError!=nil,rangeError!=nil,lengthError!=nil)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	want := "true 2 true true 3\n{\"Array\":[1,2],\"Slice\":[],\"Map\":{},\"Pointer\":3,\"Optional\":4,\"Small\":127,\"Event\":{\"Count\":5}} true\n-1 true\ntrue true true\n"
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("serde container output = %q, want %q", got, want)
	}
}

func TestSerdeRejectsMissingProtocolAndInvalidMapping(t *testing.T) {
	for _, test := range []struct{ name, declaration, want string }{
		{"missing-codec", `type Inner struct{}; #[derive(serde::Serialize)] type Outer struct{ Value Inner }`, "no registered serde codec"},
		{"wrong-direction", `#[derive(serde::Deserialize)] type Inner struct{}; #[derive(serde::Serialize)] type Outer struct{ Value Inner }`, "lacks registered Serialize"},
		{"default-unregistered", `type Inner struct{}; #[derive(serde::Deserialize)] type Outer struct{ #[serde(skip)] Value Inner }`, "Default"},
		{"duplicate-wire", `#[derive(serde::Serialize)] type Outer struct { #[serde(rename="id")] First int; #[serde(rename="id")] Second int }`, "duplicate serde field name"},
		{"mapped-payload", `#[derive(serde::Serialize)] enum E { #[serde(rename="n")] Number(int), }`, "requires unit variants"},
		{"partial-number", `#[derive(serde::Deserialize)] enum E { #[serde(value=1)] One, Two, }`, "value on every variant"},
		{"variant-field", `#[derive(serde::Serialize)] enum E { Item { #[serde(skip)] Value int }, }`, "invalid attribute position"},
	} {
		t.Run(test.name, func(t *testing.T) {
			module, err := supportModule(t, "package main\nuse serde;\n"+test.declaration+"\nfunc main(){}\n")
			if module != "" || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("module=%q error=%v, want %q", module, err, test.want)
			}
		})
	}
}

func TestSerdeEmptyEnumRejectsRuntimeValues(t *testing.T) {
	module, err := supportModule(t, `package main
use serde;
use serde::json as json;
#[derive(serde::Serialize, serde::Deserialize)]
enum Empty {}
func main() {
    var value Empty
    _,writeError := json::Marshal(&value,SerializeEmpty)
    _,readError := json::Unmarshal([]byte("\"Unknown\""),DeserializeEmpty)
    println!("{} {}",writeError!=nil,readError!=nil)
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if got := runGo(t, module, "run", "."); got != "true true\n" {
		t.Fatalf("empty enum serde output = %q", got)
	}
}

func TestMacroGeneratedErrorAndSerdeAreDeterministic(t *testing.T) {
	const source = `package main
use serde;
macro_rules! named {
    ($owner:ident) => {
        #[derive(Error,serde::Serialize,serde::Deserialize)]
        #[error("failure")]
        type $owner struct{}
    };
}
named!(Alpha)
named!(Beta)
named!(Gamma)
named!(Delta)
named!(Epsilon)
named!(Zeta)
func main(){}
`
	var first []byte
	for attempt := range 4 {
		module, err := supportModule(t, source)
		if err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(module, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			first = output
			runGo(t, module, "build", "./...")
		} else if !bytes.Equal(first, output) {
			t.Fatalf("generation %d changed output for identical macro derives", attempt)
		}
	}
}
