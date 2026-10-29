package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSerdeCausesInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := acceptanceCLI(t)
	source := t.TempDir()
	singleExternalCopyTree(t, filepath.Join(root, "examples/serde_causes"), source, "target")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	generated = strings.TrimSpace(deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source))
	packagesWrite(t, generated, "business_test.go", serdeCausesAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "accepted 1\n" {
		t.Fatalf("CLI output: %q", got)
	}
	executable := filepath.Join(t.TempDir(), "orders")
	deliveryCommand(t, generated, tool, true, nil, "build", "-buildvcs=false", "-o", executable, ".")
	for _, tc := range []struct{ input, output string }{
		{`{"Operations":[{"Charge":7}]}`, "accepted 1\n"},
		{`{"Operations":[{"Charge":9223372036854775808}]}`, "range\n"},
		{`@`, "syntax 1\n"},
		{`{"Operations":false}`, "rejected\n"},
	} {
		if got := deliveryCommand(t, outside, executable, true, nil, tc.input); got != tc.output {
			t.Fatalf("input %s: got %q want %q", tc.input, got, tc.output)
		}
	}
}

const serdeCausesAssertions = `package main
import("encoding/json";"errors";"io";"reflect";"strconv";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func detail(t *testing.T,err error,kind string,path []serde.PathSegment)*serde.Error{
 t.Helper();var e *serde.Error
 if !errors.As(err,&e)||e.Kind!=kind||!reflect.DeepEqual(e.Path,path){t.Fatalf("kind/path: %#v error=%v want=%s %v",e,err,kind,path)}
 return e
}
func nativeTokenError(input string)error{
 d:=json.NewDecoder(strings.NewReader(input));d.UseNumber()
 for {_,err:=d.Token();if err!=nil{return err}}
}
func TestSyntaxCauseOffsetsAndPaths(t *testing.T){
 for _,tc:=range []struct{input string;path []serde.PathSegment}{
  {"@",nil},
  {"{\"Operations\":[{\"Charge\":@}]}",[]serde.PathSegment{{Kind:"Field",Name:"Operations"},{Kind:"Index",Index:0},{Kind:"Variant",Name:"Charge"}}},
  {"{\"Operations\":[}",[]serde.PathSegment{{Kind:"Field",Name:"Operations"}}},
  {"{\"Operations\":[{\"Charge\":7]}",[]serde.PathSegment{{Kind:"Field",Name:"Operations"},{Kind:"Index",Index:0}}},
  {"{\"Operations\":[]} @",nil},
 }{
  _,err:=serdejson.Unmarshal([]byte(tc.input),(OrderOps{}).Decode);detail(t,err,serde.Syntax,tc.path)
  var got,want *json.SyntaxError
  if !errors.As(err,&got)||!errors.As(nativeTokenError(tc.input),&want)||got.Offset!=want.Offset||got.Error()!=want.Error(){t.Fatalf("syntax %s: got=%#v want=%#v error=%v",tc.input,got,want,err)}
 }
}
func TestEmptyInputRetainsEOF(t *testing.T){
 _,err:=serdejson.Unmarshal(nil,(OrderOps{}).Decode);detail(t,err,serde.Syntax,nil)
 if !errors.Is(err,io.EOF){t.Fatalf("EOF lost: %v",err)}
}
func TestNestedRangeCause(t *testing.T){
 input:="{\"Operations\":[{\"Charge\":9223372036854775808}]}"
 _,err:=serdejson.Unmarshal([]byte(input),(OrderOps{}).Decode)
 detail(t,err,serde.Range,[]serde.PathSegment{{Kind:"Field",Name:"Operations"},{Kind:"Index",Index:0},{Kind:"Variant",Name:"Charge"}})
 var cause *strconv.NumError
 if !errors.As(err,&cause)||!errors.Is(err,strconv.ErrRange)||cause.Func!="ParseInt"||cause.Num!="9223372036854775808" {t.Fatalf("range cause: %#v %v",cause,err)}
}
func TestOtherNumericCauses(t *testing.T){
 for _,tc:=range []struct{input,fn string;read func(serde.Deserializer)error;is error}{
  {"18446744073709551616","ParseUint",func(d serde.Deserializer)error{_,e:=serde.ReadUint64(d);return e},strconv.ErrRange},
  {"1e9999","ParseFloat",func(d serde.Deserializer)error{_,e:=serde.ReadFloat64(d);return e},strconv.ErrRange},
  {"1.5","ParseInt",func(d serde.Deserializer)error{_,e:=serde.ReadInt64(d);return e},strconv.ErrSyntax},
 }{
  err:=serdejson.Decode([]byte(tc.input),tc.read);detail(t,err,serde.Range,nil);var cause *strconv.NumError
  if !errors.As(err,&cause)||!errors.Is(err,tc.is)||cause.Func!=tc.fn||cause.Num!=tc.input{t.Fatalf("%s: cause=%#v err=%v",tc.input,cause,err)}
 }
}
func TestBackendChecksDoNotInventCause(t *testing.T){
 for _,tc:=range []struct{input,kind string;path []serde.PathSegment}{
  {"{\"Operations\":false}",serde.Type,[]serde.PathSegment{{Kind:"Field",Name:"Operations"}}},
  {"{\"Operations\":[],\"Operations\":[]}","Duplicate",[]serde.PathSegment{{Kind:"Field",Name:"Operations"}}},
  {"{\"Operations\":[]} {}",serde.Syntax,nil},
 }{
  _,err:=serdejson.Unmarshal([]byte(tc.input),(OrderOps{}).Decode);e:=detail(t,err,tc.kind,tc.path)
  for {inner,ok:=e.Cause.(*serde.Error);if !ok{break};e=inner};if e.Cause!=nil{t.Fatalf("fabricated cause %s: %#v",tc.input,e.Cause)}
 }
 err:=serdejson.Decode([]byte("1e-9999"),func(d serde.Deserializer)error{_,e:=serde.ReadFloat64(d);return e})
 if e:=detail(t,err,serde.Range,nil);e.Cause!=nil{t.Fatalf("underflow fabricated cause: %v",err)}
}
`
