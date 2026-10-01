package acceptance_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestRawEnumInstalledCLIAndGoInteropRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/hgo")
	cli := filepath.Join(bin, "hgo")
	source := filepath.Join(root, "examples/raw_enum")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	repeated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeated, source)
	if err := filepath.WalkDir(generated, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || (!strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".map.json")) {
			return nil
		}
		rel, err := filepath.Rel(generated, path)
		if err != nil {
			return err
		}
		a, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(filepath.Join(repeated, rel))
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			t.Errorf("non-deterministic generated raw enum artifact: %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "raw_test.go", rawEnumAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	binary := filepath.Join(t.TempDir(), "webhook")
	deliveryCommand(t, generated, tool, true, nil, "build", "-buildvcs=false", "-o", binary, ".")
	for _, tc := range []struct{ input, want string }{
		{`{"State":"deferred-v2","Attempt":7}`, "webhook review:deferred-v2/unknown:7\n"},
		{`{"State":"sent","Attempt":1}`, "webhook archive/accepted\n"},
		{`{}`, "webhook review:/unknown:0\n"},
	} {
		if got := deliveryCommand(t, outside, binary, true, nil, tc.input); got != tc.want {
			t.Fatalf("wire value changed: got %q want %q", got, tc.want)
		}
	}
	got := deliveryCommand(t, outside, cli, true, nil, "run", "-go", tool, source)
	if got != "webhook review:deferred-v2/unknown:7\n" {
		t.Fatal(got)
	}
}

func TestRawEnumExplicitBoundaries(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"known-is-not-exhaustive", `enum Status string { Pending="pending", Paid="paid", };func f(v Status){match v {Status::Pending=>{},Status::Paid=>{},}}`, "wildcard"},
		{"guarded-fallback-is-not-exhaustive", `enum Status string { Pending="pending", };func f(v Status){match v {Status::Pending=>{},_ if true=>{},}}`, "wildcard"},
		{"duplicate-string-pending", `enum Status string { A="same", B="\x73ame", }`, "pending"},
		{"duplicate-integer-pending", `enum Code uint8 { A=1, B=0x1, }`, "pending"},
		{"overflow-uint8", `enum Code uint8 { TooLarge=256, }`, "overflow"},
		{"overflow-int8", `enum Code int8 { TooLow=-129, }`, "overflow"},
		{"negative-unsigned", `enum Code uint8 { Negative=-1, }`, "overflow"},
		{"not-constant", `var n=1;enum Code int { Dynamic=n, }`, "constant"},
		{"non-integer-underlying", `enum Code float64 { Value=1.0, }`, "builtin integer or string"},
		{"missing-explicit-value", `enum Code int { First, }`, "explicit"},
		{"typed-nominal-constant", `enum A string { Same="same", }
enum B string { Same="same", }
var wrong B=A::Same`, "cannot"},
		{"mixed-match-identity", `enum A string { Same="same", }
enum B string { Same="same", }
func wrong(v B){match v {A::Same=>{},_=>{},}}`, "cannot"},
		{"unreachable-known", `enum Status string { Pending="pending", };func f(v Status){match v {_=>{},Status::Pending=>{},}}`, "unreachable"},
		{"raw-default-not-payload-default", "#[derive(Default)]\nenum Status string { Pending=\"pending\", }", "derive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), tc.name+".hgo")
			_, err := frontend.Compile(filename, []byte("package rejected\n"+tc.source+"\n"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) || !strings.Contains(err.Error(), filepath.Base(filename)+":") {
				t.Fatalf("want located %q rejection, got %v", tc.want, err)
			}
		})
	}
}

const rawEnumAssertions = `package main
import("reflect";"testing";p "rawenum.example/delivery/protocol")
const acceptedTyped p.AttemptCode=p.HgoConst11_AttemptCode_Accepted
const acceptedWire uint8=uint8(acceptedTyped)
type StateAlias=p.DeliveryState
func TestFutureAndZeroWireValues(t *testing.T){
 for _,tc:=range []struct{wire,want string}{
  {"{\"State\":\"sent\",\"Attempt\":1}","archive/accepted"},
  {"{\"State\":\"future\",\"Attempt\":17}","review:future/unknown:17"},
  {"{}","review:/unknown:0"},
 }{got,err:=Interpret([]byte(tc.wire));if err!=nil||got!=tc.want{t.Fatalf("%s: %q %v",tc.wire,got,err)}}
 if _,err:=Interpret([]byte("{"));err==nil{t.Fatal("invalid JSON accepted")}
 var zero p.DeliveryState;if string(zero)!=""||p.KnownState(zero)||Route(zero)!="review:"{t.Fatal("zero changed to first member")}
 if zero:=(map[string]p.AttemptCode{})["missing"];uint8(zero)!=0||Action(zero)!="unknown:0"{t.Fatal("numeric zero changed")}
 if Route(StateAlias("future"))!="review:future"||acceptedWire!=1||p.KnownAttempt()!=acceptedTyped{t.Fatal("Go aliases/typed constants lost")}
}
func TestIntegerRawConversions(t *testing.T){
 for _,raw:=range []uint8{0,1,2,7,255}{value:=p.AttemptCode(raw);if uint8(value)!=raw{t.Fatal("raw conversion changed",raw)}}
 wide:=257;if uint8(p.AttemptCode(wide))!=1{t.Fatal("runtime conversion lost Go truncation")};signedWide:=130;if int8(p.SignedCode(signedWide))!=-126{t.Fatal("runtime signed conversion changed")}
 if int8(p.HgoConst10_SignedCode_Low)!=-128||int8(p.HgoConst10_SignedCode_High)!=127{t.Fatal("signed boundary constants")}
 if Action(p.AttemptCode(2))!="retry"||Action(p.AttemptCode(255))!="denied"{t.Fatal("constant expression/value match failed")}
 unknown:=p.DeliveryState("unseen");if p.KnownState(unknown)||string(unknown)!="unseen"{t.Fatal("unknown value rejected or normalized")}
}
func TestRawMatchEvaluationAndControlFlow(t *testing.T){
 for _,tc:=range []struct{state p.DeliveryState;guard bool;want string;trace []string}{
  {p.DeliveryState("queued"),false,"queued",[]string{"next","guard","cleanup"}},
  {p.DeliveryState("queued"),true,"guarded",[]string{"next","guard","cleanup"}},
  {p.DeliveryState("future"),true,"other",[]string{"next","cleanup"}},
 }{trace:=[]string{};got:=Observe(func()p.DeliveryState{trace=append(trace,"next");return tc.state},func()bool{trace=append(trace,"guard");return tc.guard},&trace);if got!=tc.want||!reflect.DeepEqual(trace,tc.trace){t.Fatalf("%q %v",got,trace)}}
 if count:=Sweep([]p.DeliveryState{"queued","sent","rejected","future"});count!=11{t.Fatalf("break/continue changed: %d",count)}
}
`
