package acceptance_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

// This independent acceptance suite executes actual frontend output under race.
func TestSerdeBlackBoxRace(t *testing.T) {
	result, err := testdeps.FormalSource(t, "acceptance.gox", []byte(acceptanceSource))
	if err != nil {
		t.Fatal(err)
	}
	runFormalSerdeRace(t, result, acceptanceAssertions)
}

func TestUnsupportedSerdeIsDiagnosed(t *testing.T) {
	cases := map[string]string{
		"skip-wire-collision": "package fixture; use serde;\n#[derive(serde::derive::Serialize, serde::derive::Deserialize)]\ntype Collision struct {\n #[serde(skip, rename=\"same\")]\n Secret string\n #[serde(rename=\"same\")]\n Public string\n}",
		"external":            "package fixture; use serde;\nuse time;\n#[derive(serde::derive::Serialize)]\ntype Event struct { At time::Time }",
		"interface":           "package fixture; use serde;\n#[derive(serde::derive::Deserialize)]\ntype Dynamic struct { Value any }",
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := testdeps.FormalSource(t, "unsupported.gox", []byte(source), "[dependencies.time]\ngo=\"time\"\n")
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
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Leaf struct { Name string; N int64 }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
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
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Numbers struct { I int64; U uint64; F float64; Small float32 }
#[derive(serde::derive::Serialize, serde::derive::Deserialize)]
type Node struct { Value int; Next *Node }
impl Record {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
impl Numbers {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
impl Node {
 pub func encode(s serde::Serializer, value &Self) error { return serde::Write(s,value) }
 pub func decode(d serde::Deserializer) (Self,error) { return serde::Read[Self](d) }
}
`

const acceptanceAssertions = `package fixture
import("errors";"math";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func record(s string)(Record,error){return serdejson.Unmarshal([]byte(s),(RecordOps{}).Decode)}
func number(s string)(Numbers,error){return serdejson.Unmarshal([]byte(s),(NumbersOps{}).Decode)}
const valid="{\"rows\":{\"east\":[{\"Name\":\"\\ud83d\\ude80\",\"N\":-9223372036854775808},null]},\"Optional\":null}"
func TestBusinessRoundtrip(t *testing.T){v,e:=record(valid);if e!=nil{t.Fatal(e)};if v.Rows["east"][0].Name!="🚀"||v.Rows["east"][1]!=nil||v.Retry!=0{t.Fatalf("wrong decode: %#v",v)};v.Token="secret";out,e:=serdejson.Encode(func(s serde.Serializer)error{return (RecordOps{}).Encode(s,&v)});if e!=nil{t.Fatal(e)};if strings.Contains(string(out),"secret"){t.Fatal("skip exposed secret")};v.Token="";w,e:=record(string(out));if e!=nil||!reflect.DeepEqual(v,w){t.Fatalf("roundtrip: %#v %v",w,e)}}
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
func TestEncodingErrorPath(t *testing.T){v:=Record{Rows:map[string][]*Leaf{"east":{{Name:string([]byte{255})}}}};_,e:=serdejson.Encode(func(s serde.Serializer)error{return (RecordOps{}).Encode(s,&v)});var se *serde.Error;if !errors.As(e,&se){t.Fatalf("no structured error: %v",e)};want:=[]serde.PathSegment{{Kind:"Field",Name:"rows"},{Kind:"MapKey",Name:"east"},{Kind:"Index",Index:0},{Kind:"Field",Name:"Name"}};if !reflect.DeepEqual(se.Path,want){t.Fatalf("path %#v != %#v (%v)",se.Path,want,e)}}
func TestEncodingRejectsInvalidValues(t *testing.T){for _,c:=range []struct{name string; value Numbers}{{"nan",Numbers{F:math.NaN()}},{"positive-infinity",Numbers{F:math.Inf(1)}},{"negative-infinity",Numbers{F:math.Inf(-1)}}}{t.Run(c.name,func(t *testing.T){if _,e:=serdejson.Encode(func(s serde.Serializer)error{return (NumbersOps{}).Encode(s,&c.value)});e==nil{t.Fatal("invalid float encoded")}})};bad:=Record{Rows:map[string][]*Leaf{"x":{{Name:string([]byte{255})}}}};if _,e:=serdejson.Encode(func(s serde.Serializer)error{return (RecordOps{}).Encode(s,&bad)});e==nil{t.Fatal("invalid UTF8 encoded")}}
func TestZeroAndTinyNumbers(t *testing.T){for _,lit:=range []string{"0e-999","-0e999","4.9406564584124654e-324"}{input:="{\"I\":0,\"U\":0,\"F\":"+lit+",\"Small\":0}";v,e:=number(input);if e!=nil{t.Fatalf("valid %s: %v",lit,e)};if lit=="4.9406564584124654e-324"&&v.F!=math.SmallestNonzeroFloat64{t.Fatalf("smallest float64 lost: %v",v.F)}}}
func TestCycleAndDepthBound(t *testing.T){v:=&Node{Value:1};v.Next=v;if _,e:=serdejson.Encode(func(s serde.Serializer)error{return (NodeOps{}).Encode(s,v)});e==nil{t.Fatal("cycle accepted")};deep:=strings.Repeat("{\"Value\":1,\"Next\":",serdejson.MaxDepth+1)+"null"+strings.Repeat("}",serdejson.MaxDepth+1);if _,e:=serdejson.Unmarshal([]byte(deep),(NodeOps{}).Decode);e==nil{t.Fatal("excess depth accepted")}}
func TestSkippedFieldAndWhitespace(t *testing.T){v,e:=record(" \n"+strings.TrimSuffix(valid,"}")+",\"Token\":{\"ignored\":[1,true,null]}} \t");if e!=nil||v.Token!=""{t.Fatalf("skip: %#v %v",v,e)}}
`

func runFormalSerdeRace(t *testing.T, result testdeps.FormalResult, assertions string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(result.Project.Root, "business_test.go"), []byte(assertions), 0600); err != nil {
		t.Fatal(err)
	}
	testdeps.RunFormalGo(t, result.Project, "build", "-mod=readonly", ".")
	testdeps.RunFormalGo(t, result.Project, "test", "-mod=readonly", "-race", "-count=1", ".")
}
