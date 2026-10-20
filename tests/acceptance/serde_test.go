package acceptance_test

import (
	"context"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This independent acceptance suite executes actual frontend output under race.
func TestSerdeBlackBoxRace(t *testing.T) {
	result, err := compileAcceptanceSerdeFixture("acceptance.gox", []byte(acceptanceSource))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		"go.mod":            []byte("module acceptancefixture\n\ngo 1.27\n"),
		"generated.go":      result.Go,
		"generated_test.go": []byte(acceptanceAssertions),
	} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	out, err := cmd.CombinedOutput()
	t.Logf("generated race module:\n%s", out)
	if err != nil {
		t.Fatalf("generated module: %v", err)
	}
}

func TestUnsupportedSerdeIsDiagnosed(t *testing.T) {
	cases := map[string]string{
		"skip-wire-collision": "package fixture; use serde;\n#[derive(serde::Serialize, serde::Deserialize)]\ntype Collision struct {\n #[serde(skip, rename=\"same\")]\n Secret string\n #[serde(rename=\"same\")]\n Public string\n}",
		"generic":             "package fixture; use serde;\n#[derive(serde::Serialize)]\ntype Generic[T any] struct { Value T }",
		"external":            "package fixture; use serde;\nuse time;\n#[derive(serde::Serialize)]\ntype Event struct { At time::Time }",
		"interface":           "package fixture; use serde;\n#[derive(serde::Deserialize)]\ntype Dynamic struct { Value any }",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceSerdeFixture("unsupported.gox", []byte(source))
			if err == nil {
				t.Fatal("unsupported derive accepted")
			}
			if !strings.Contains(err.Error(), "unsupported.gox") {
				t.Fatalf("no source location: %v", err)
			}
		})
	}
}

const acceptanceSource = `package fixture; use serde;
#[derive(serde::Serialize, serde::Deserialize)]
type Leaf struct { Name string; N int64 }
#[derive(serde::Serialize, serde::Deserialize)]
#[serde(deny_unknown_fields)]
type Record struct {
 #[serde(rename="rows")]
 Rows map[string][]*Leaf
 #[serde(default)]
 Retry uint8
 #[serde(skip)]
 Token string
 Optional *int
}
#[derive(serde::Serialize, serde::Deserialize)]
type Numbers struct { I int64; U uint64; F float64; Small float32 }
#[derive(serde::Serialize, serde::Deserialize)]
type Node struct { Value int; Next *Node }
`

const acceptanceAssertions = `package fixture
import("errors";"math";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func record(s string)(Record,error){return serdejson.Unmarshal([]byte(s),DeserializeRecord)}
func number(s string)(Numbers,error){return serdejson.Unmarshal([]byte(s),DeserializeNumbers)}
const valid="{\"rows\":{\"east\":[{\"Name\":\"\\ud83d\\ude80\",\"N\":-9223372036854775808},null]},\"Optional\":null}"
func TestBusinessRoundtrip(t *testing.T){v,e:=record(valid);if e!=nil{t.Fatal(e)};if v.Rows["east"][0].Name!="🚀"||v.Rows["east"][1]!=nil||v.Retry!=0{t.Fatalf("wrong decode: %#v",v)};v.Token="secret";out,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeRecord(s,&v)});if e!=nil{t.Fatal(e)};if strings.Contains(string(out),"secret"){t.Fatal("skip exposed secret")};v.Token="";w,e:=record(string(out));if e!=nil||!reflect.DeepEqual(v,w){t.Fatalf("roundtrip: %#v %v",w,e)}}
func TestInputRejections(t *testing.T){cases:=map[string]string{
"missing-required":"{\"rows\":{}}",
"null-required":"{\"rows\":null,\"Optional\":null}",
"null-default":"{\"rows\":{},\"Optional\":null,\"Retry\":null}",
"duplicate":"{\"rows\":{},\"Optional\":null,\"rows\":{}}",
"escaped-duplicate":"{\"rows\":{},\"Optional\":null,\"\\u0072ows\":{}}",
"unknown":"{\"rows\":{},\"Optional\":null,\"extra\":1}",
"nested-duplicate":"{\"rows\":{\"a\":[],\"a\":[]},\"Optional\":null}",
"unpaired-high":strings.Replace(valid,"\\ud83d\\ude80","\\ud83d",1),
"unpaired-low":strings.Replace(valid,"\\ud83d\\ude80","\\ude80",1),
"invalid-utf8":strings.Replace(valid,"\\ud83d\\ude80",string([]byte{255}),1),
"trailing":valid+" null",
};for name,input:=range cases{t.Run(name,func(t *testing.T){if _,e:=record(input);e==nil{t.Fatalf("accepted %s",input)}})}}
func TestNumericBoundaries(t *testing.T){base:="{\"I\":-9223372036854775808,\"U\":18446744073709551615,\"F\":1.7976931348623157e308,\"Small\":1.401298464324817e-45}";v,e:=number(base);if e!=nil{t.Fatal(e)};if v.I!=math.MinInt64||v.U!=math.MaxUint64||v.F!=math.MaxFloat64||v.Small!=math.SmallestNonzeroFloat32{t.Fatalf("boundary changed: %#v",v)};for _,c:=range []struct{name,old,new string}{{"signed-underflow","-9223372036854775808","-9223372036854775809"},{"unsigned-overflow","18446744073709551615","18446744073709551616"},{"float-overflow","1.7976931348623157e308","1e309"},{"float-underflow","1.7976931348623157e308","1e-400"},{"float32-underflow","1.401298464324817e-45","1e-50"}}{t.Run(c.name,func(t *testing.T){if _,e:=number(strings.Replace(base,c.old,c.new,1));e==nil{t.Fatal("accepted out-of-range number")}})}}
func TestNestedErrorPath(t *testing.T){_,e:=record(strings.Replace(valid,"-9223372036854775808","false",1));var se *serde.Error;if !errors.As(e,&se){t.Fatalf("no structured error: %v",e)};want:=[]serde.PathSegment{{Kind:"Field",Name:"rows"},{Kind:"MapKey",Name:"east"},{Kind:"Index",Index:0},{Kind:"Field",Name:"N"}};if !reflect.DeepEqual(se.Path,want){t.Fatalf("path %#v != %#v (%v)",se.Path,want,e)}}
func TestEncodingErrorPath(t *testing.T){v:=Record{Rows:map[string][]*Leaf{"east":{{Name:string([]byte{255})}}}};_,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeRecord(s,&v)});var se *serde.Error;if !errors.As(e,&se){t.Fatalf("no structured error: %v",e)};want:=[]serde.PathSegment{{Kind:"Field",Name:"rows"},{Kind:"MapKey",Name:"east"},{Kind:"Index",Index:0},{Kind:"Field",Name:"Name"}};if !reflect.DeepEqual(se.Path,want){t.Fatalf("path %#v != %#v (%v)",se.Path,want,e)}}
func TestEncodingRejectsInvalidValues(t *testing.T){for _,c:=range []struct{name string; value Numbers}{{"nan",Numbers{F:math.NaN()}},{"positive-infinity",Numbers{F:math.Inf(1)}},{"negative-infinity",Numbers{F:math.Inf(-1)}}}{t.Run(c.name,func(t *testing.T){if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeNumbers(s,&c.value)});e==nil{t.Fatal("invalid float encoded")}})};bad:=Record{Rows:map[string][]*Leaf{"x":{{Name:string([]byte{255})}}}};if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeRecord(s,&bad)});e==nil{t.Fatal("invalid UTF8 encoded")}}
func TestZeroAndTinyNumbers(t *testing.T){for _,lit:=range []string{"0e-999","-0e999","4.9406564584124654e-324"}{input:="{\"I\":0,\"U\":0,\"F\":"+lit+",\"Small\":0}";v,e:=number(input);if e!=nil{t.Fatalf("valid %s: %v",lit,e)};if lit=="4.9406564584124654e-324"&&v.F!=math.SmallestNonzeroFloat64{t.Fatalf("smallest float64 lost: %v",v.F)}}}
func TestCycleAndDepthBound(t *testing.T){v:=&Node{Value:1};v.Next=v;if _,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeNode(s,v)});e==nil{t.Fatal("cycle accepted")};deep:=strings.Repeat("{\"Value\":1,\"Next\":",serdejson.MaxDepth+1)+"null"+strings.Repeat("}",serdejson.MaxDepth+1);if _,e:=serdejson.Unmarshal([]byte(deep),DeserializeNode);e==nil{t.Fatal("excess depth accepted")}}
func TestSkippedFieldAndWhitespace(t *testing.T){v,e:=record(" \n"+strings.TrimSuffix(valid,"}")+",\"Token\":{\"ignored\":[1,true,null]}} \t");if e!=nil||v.Token!=""{t.Fatalf("skip: %#v %v",v,e)}}
`
