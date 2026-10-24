package prototype_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

// These tests run generated output, not handwritten stand-ins for derive.
func TestSerdeGeneratedEndToEnd(t *testing.T) {
	source := `package fixture; use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Address struct { City string }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Order struct {
 #[serde(rename="id")]
 ID uint64
 Items []Address
 Counts map[string]int16
 Bytes []byte
 Pair [2]int
 Nick *string
 #[serde(default)]
 Attempts int
 #[serde(skip)]
 Secret string
}
impl Order {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s, value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`
	result, err := testdeps.FormalSource(t, "business.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if result.MacroCalls == 0 {
		t.Fatal("missing observable expansion")
	}
	if bytes.Contains(result.Go, []byte("encoding/json")) {
		t.Fatal("derive is coupled to JSON")
	}
	runFormalSerde(t, result, runtimeAssertions)
}

func runGenerated(t *testing.T, generated []byte, assertions string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string][]byte{
		"go.mod":            []byte("module fixture\n\ngo 1.27\n"),
		"generated.go":      generated,
		"generated_test.go": []byte(assertions),
	}
	for name, data := range files {
		if err := testdeps.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go failed: %v\n%s\n--- generated ---\n%s", err, output, generated)
	}
	t.Logf("generated Go compiled and executed:\n%s", output)
}

func TestDeriveRejectsInvalidInput(t *testing.T) {
	cases := []struct{ name, source string }{
		{"unknown attribute", `package fixture; use serde;
#[derive(serde::derive::Serialize)]
type A struct {
 #[serde(unknown)]
 X int
}`},
		{"rename collision", `package fixture; use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type A struct {
 #[serde(rename="same")]
 X int
 #[serde(rename="same")]
 Y int
}`},
		{"unsupported map key", `package fixture; use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type A struct { X map[bool]int }`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := testdeps.FormalSource(t, "invalid.gox", []byte(tt.source))
			if err == nil {
				t.Fatal("invalid input accepted")
			}
			if !strings.Contains(err.Error(), "invalid.gox") {
				t.Fatalf("diagnostic lacks source: %v", err)
			}
		})
	}
}

const runtimeAssertions = `package fixture
import (
 "bytes"
 "errors"
 "reflect"
 "strings"
 "testing"
 "github.com/goxide-lang/serde"
 "github.com/goxide-lang/serde/json"
)
func decode(input string)(Order,error){var result Order;err:=serdejson.Decode([]byte(input),func(d serde.Deserializer)error{var err error;result,err=(OrderOps{}).Decode(d);return err});return result,err}
const valid = "{\"id\":18446744073709551615,\"Items\":[{\"City\":\"Paris\"}],\"Counts\":{\"west\":-7},\"Bytes\":[0,255],\"Pair\":[3,4],\"Nick\":null}"
func TestRoundTrip(t *testing.T){
 v,err:=decode(valid);if err!=nil{t.Fatal(err)}
 if v.ID!=^uint64(0)||v.Items[0].City!="Paris"||v.Attempts!=0||v.Nick!=nil||v.Secret!=""{t.Fatalf("bad decode: %#v",v)}
 v.Secret="must-not-leak"
 out,err:=serdejson.Encode(func(s serde.Serializer)error{return (OrderOps{}).Encode(s,&v)});if err!=nil{t.Fatal(err)}
 if bytes.Contains(out,[]byte("must-not-leak"))||bytes.Contains(out,[]byte("Secret")){t.Fatalf("skip leaked: %s",out)}
 if !bytes.Contains(out,[]byte("[0,255]")){t.Fatalf("bytes must not be base64: %s",out)}
 v.Secret=""
 again,err:=decode(string(out));if err!=nil{t.Fatal(err)}
 if !reflect.DeepEqual(v,again){t.Fatalf("roundtrip mismatch: %#v / %#v",v,again)}
}
func TestErrors(t *testing.T){
 cases:=[]struct{name,input,path string}{
 {"missing",strings.Replace(valid,"\"id\":18446744073709551615,","",1),"id"},
 {"required null",strings.Replace(valid,"18446744073709551615","null",1),"id"},
 {"unsigned negative",strings.Replace(valid,"18446744073709551615","-1",1),"id"},
 {"uint overflow",strings.Replace(valid,"18446744073709551615","18446744073709551616",1),"id"},
 {"fraction integer",strings.Replace(valid,"18446744073709551615","1.0",1),"id"},
 {"narrow range",strings.Replace(valid,"-7","32768",1),"Counts"},
 {"nested path",strings.Replace(valid,"\"Paris\"","false",1),"City"},
 {"array too short",strings.Replace(valid,"[3,4]","[3]",1),"Pair"},
 {"array too long",strings.Replace(valid,"[3,4]","[3,4,5]",1),"Pair"},
 {"default null",strings.TrimSuffix(valid,"}")+",\"Attempts\":null}","Attempts"},
 {"duplicate key",strings.TrimSuffix(valid,"}")+",\"id\":1}",""},
 {"escaped duplicate",strings.TrimSuffix(valid,"}")+",\"\\u0069d\":1}",""},
 {"trailing value",valid+" {}",""},
 {"invalid unknown",strings.TrimSuffix(valid,"}")+",\"unknown\":[1,]}",""},
 }
 for _,tt:=range cases{t.Run(tt.name,func(t *testing.T){_,err:=decode(tt.input);if err==nil{t.Fatalf("accepted invalid input %s",tt.input)};if tt.path!=""&&!strings.Contains(err.Error(),tt.path){t.Fatalf("missing error path %q: %v",tt.path,err)}})}
}
func TestExactStructuredPath(t *testing.T){
 _,err:=decode(strings.Replace(valid,"\"Paris\"","false",1))
 var typed *serde.Error
 if !errors.As(err,&typed){t.Fatalf("no structured error: %v",err)}
 want:=[]serde.PathSegment{{Kind:"Field",Name:"Items"},{Kind:"Index",Index:0},{Kind:"Field",Name:"City"}}
 if !reflect.DeepEqual(typed.Path,want){t.Fatalf("wrong path: %#v; want %#v (%v)",typed.Path,want,err)}
}
func TestUnknownAndOwnedResults(t *testing.T){
 input:=strings.TrimSuffix(valid,"}")+",\"unknown\":{\"deep\":[null,true,{\"x\":1}]},\"Secret\":\"ignored\",\"Attempts\":0}"
 v,err:=decode(input);if err!=nil{t.Fatal(err)};if v.Secret!=""||v.Attempts!=0{t.Fatalf("wrong defaults: %#v",v)}
 buf:=[]byte(valid);var owned Order
 err=serdejson.Decode(buf,func(d serde.Deserializer)error{var e error;owned,e=(OrderOps{}).Decode(d);return e});if err!=nil{t.Fatal(err)}
 for i:=range buf{buf[i]='x'};if owned.Items[0].City!="Paris"{t.Fatal("result aliases input buffer")}
}
`

func TestIndependentDerivesAndFloat32(t *testing.T) {
	source := `package fixture; use serde;
#[derive(serde::derive::Serialize)]
type OutChild struct { Value string }
#[derive(serde::derive::Serialize)]
type Output struct { Child OutChild }
#[derive(serde::derive::Deserialize)]
type Input struct { Value float32 }
impl Output { pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) } }
impl Input { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
`
	result, err := testdeps.FormalSource(t, "directions.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestDirections(t *testing.T){
 _,err:=serdejson.Encode(func(s serde.Serializer)error{return (OutputOps{}).Encode(s,&Output{Child:OutChild{Value:"ok"}})});if err!=nil{t.Fatal(err)}
 for _,bad:=range []string{"1e-50","1e39"}{
 err=serdejson.Decode([]byte("{\"Value\":"+bad+"}"),func(d serde.Deserializer)error{_,e:=(InputOps{}).Decode(d);return e})
 if err==nil{t.Fatalf("accepted out of float32 range: %s",bad)}
 }
}
`)
}

func TestAttributesInsideGoLiteralsAreNotSyntax(t *testing.T) {
	source := "package fixture; use serde;\nconst Template = `\n#[derive(NotAMacro)]\nstruct literal {\n`\n/*\n#[serde(unsupported)]\n*/\n#[derive(serde::derive::Serialize)]\ntype Actual struct { Value string }\n"
	result, err := testdeps.FormalSource(t, "literal.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, "package fixture\nimport \"testing\"\nfunc TestLiteral(t *testing.T){if Template != \"\\n#[derive(NotAMacro)]\\nstruct literal {\\n\" {t.Fatalf(\"literal corrupted: %q\",Template)}}\n")
}

func TestWireNamesAndUnknownFieldPolicy(t *testing.T) {
	source := `package fixture; use serde;
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
#[serde(deny_unknown_fields)]
type Names struct {
 #[serde(rename="")]
 Empty string
 #[serde(rename="a,b")]
 Comma int
}
impl Names { pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) } }
`
	result, err := testdeps.FormalSource(t, "names.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
import("testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func TestNames(t *testing.T){
 var value Names
 err:=serdejson.Decode([]byte("{\"\":\"ok\",\"a,b\":2}"),func(d serde.Deserializer)error{var e error;value,e=(NamesOps{}).Decode(d);return e});if err!=nil{t.Fatal(err)}
 if value.Empty!="ok"||value.Comma!=2{t.Fatalf("wrong values: %#v",value)}
 err=serdejson.Decode([]byte("{\"\":\"ok\",\"a,b\":2,\"extra\":1}"),func(d serde.Deserializer)error{_,e:=(NamesOps{}).Decode(d);return e});if err==nil{t.Fatal("accepted unknown field")}
}
`)
}

func TestGenericSerdeDerive(t *testing.T) {
	result, err := testdeps.FormalSource(t, "generic.gox", []byte(`package fixture; use serde;
#[derive(serde::derive::Serialize,serde::derive::Deserialize)]
type Packet[T any] struct { X T }
func RoundTrip() bool {
 var value = Packet[int]{X:7}
 data,err := serde::json::Marshal(&value,serde::Write[Packet[int]])
 if err != nil { return false }
 result,err := serde::json::Unmarshal(data,serde::Read[Packet[int]])
 return err == nil && result.X == 7
}
`))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerde(t, result, `package fixture
import "testing"
func TestGeneric(t *testing.T) { if !RoundTrip() { t.Fatal("generic serde roundtrip") } }
`)
}

func runFormalSerde(t *testing.T, result testdeps.FormalResult, assertions string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(result.Project.Root, "generated_test.go"), []byte(assertions), 0600); err != nil {
		t.Fatal(err)
	}
	testdeps.RunFormalGo(t, result.Project, "build", "-mod=readonly", ".")
	testdeps.RunFormalGo(t, result.Project, "test", "-mod=readonly", "-count=1", ".")
}
