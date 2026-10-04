package acceptance_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnumWireInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/hgo")
	cli := filepath.Join(bin, "hgo")
	source := filepath.Join(root, "examples/enum_wire")
	outside := t.TempDir()
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "business_test.go", enumWireAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	binary := filepath.Join(t.TempDir(), "dispatch")
	deliveryCommand(t, generated, tool, true, nil, "build", "-buildvcs=false", "-o", binary, ".")
	for _, tc := range []struct{ input, want string }{
		{`{"Delivery":"sent","Urgency":9,"Note":"webhook"}`, "dispatch delivered true 1\n"},
		{`{"Delivery":"queued","Urgency":2,"Note":"webhook"}`, "dispatch waiting false 1\n"},
		{`{"Delivery":"future","Urgency":9,"Note":"webhook"}`, "rejected 0\n"},
		{`{"Delivery":"sent","Urgency":99,"Note":"webhook"}`, "rejected 0\n"},
	} {
		if got := deliveryCommand(t, outside, binary, true, nil, tc.input); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "dispatch delivered true 1\n" {
		t.Fatal(got)
	}
	// The example itself, not a synthetic source string, is the delivery artifact.
	if _, err := os.Stat(filepath.Join(source, "bridge.go")); err != nil {
		t.Fatal(err)
	}
}

func TestEnumWireExplicitBoundaries(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"open-string-enum-withdrawn", `enum State string { A="a", }`, "enum"},
		{"open-integer-enum-withdrawn", `enum State int { A=1, }`, "enum"},
		{"field-default-missing", "type Socket struct { C chan int }\n#[derive(Default)]\ntype Config struct { Value Socket }", "default"},
		{"channel-default-missing", "#[derive(Default)]\ntype Config struct { Value chan int }", "default"},
		{"enum-default-not-selected", "#[derive(Default)]\nenum E { A, B, }", "default"},
		{"two-default-variants", "#[derive(Default)]\nenum E {\n#[default]\nA,\n#[default]\nB,\n}", "default"},
		{"duplicate-string-mapping", "#[derive(Serialize,Deserialize)]\nenum E {\n#[serde(rename=\"wire\")]\nA,\n#[serde(rename=\"wire\")]\nB,\n}", "duplicate"},
		{"duplicate-number-mapping", "#[derive(Serialize,Deserialize)]\nenum E {\n#[serde(value=1)]\nA,\n#[serde(value=1)]\nB,\n}", "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), tc.name+".hgo")
			_, err := frontend.Compile(filename, []byte("package rejected\n"+tc.source+"\n"))
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) || !strings.Contains(err.Error(), filepath.Base(filename)+":") {
				t.Fatalf("want located %s rejection, got %v", tc.want, err)
			}
		})
	}
}

const enumWireAssertions = `package main
import("errors";"reflect";"strings";"testing";"github.com/goxide-lang/serde";"github.com/goxide-lang/serde/json")
func encodeTicket(v Ticket)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return SerializeTicket(s,&v)})}
func TestKnownWireRoundTripAndNativeConstructors(t *testing.T){
 for _,tc:=range []struct{status Delivery;urgency Urgency;wire string}{
  {NewDeliveryWaiting(),NewUrgencyNormal(),"{\"Delivery\":\"queued\",\"Urgency\":2,\"Note\":\"native\"}"},
  {NewDeliveryDelivered(),NewUrgencyRush(),"{\"Delivery\":\"sent\",\"Urgency\":9,\"Note\":\"native\"}"},
 }{
  original:=Ticket{Delivery:tc.status,Urgency:tc.urgency,Note:"native"}
  data,err:=encodeTicket(original);if err!=nil||string(data)!=tc.wire{t.Fatalf("wire=%s err=%v",data,err)}
  calls:=0;got,err:=NativeDispatch(data,&calls)
  if err!=nil||calls!=1||!reflect.DeepEqual(got,original){t.Fatalf("native round trip: %#v %d %v",got,calls,err)}
 }
}
func TestUnknownWireFailsBeforeBusiness(t *testing.T){
 for _,tc:=range []struct{wire,field string}{
  {"{\"Delivery\":\"future\",\"Urgency\":9,\"Note\":\"x\"}","Delivery"},
  {"{\"Delivery\":\"\",\"Urgency\":9,\"Note\":\"x\"}","Delivery"},
  {"{\"Delivery\":\"sent\",\"Urgency\":0,\"Note\":\"x\"}","Urgency"},
  {"{\"Delivery\":\"sent\",\"Urgency\":99,\"Note\":\"x\"}","Urgency"},
 }{
  calls:=0;_,err:=NativeDispatch([]byte(tc.wire),&calls)
  var typed *serde.Error
  if !errors.As(err,&typed)||typed.Kind!=serde.UnknownVariant||calls!=0{t.Fatalf("unknown reached business or wrong category: calls=%d err=%v",calls,err)}
  if len(typed.Path)!=1||typed.Path[0].Kind!="Field"||typed.Path[0].Name!=tc.field{t.Fatalf("lost field source: %#v",typed.Path)}
 }
 for _,wire:=range []string{"{\"Delivery\":17,\"Urgency\":9,\"Note\":\"x\"}","{\"Delivery\":\"sent\",\"Urgency\":\"9\",\"Note\":\"x\"}","{\"Delivery\":\"sent\",\"Urgency\":1.5,\"Note\":\"x\"}"}{
  calls:=0;_,err:=NativeDispatch([]byte(wire),&calls);if err==nil||calls!=0{t.Fatalf("wrong wire kind accepted: %s %v",wire,err)}
 }
}
func TestDefaultAndDebugAreNotWireMapping(t *testing.T){
 v:=DefaultTicket()
 if Describe(v.Delivery)!="waiting"||IsRush(v.Urgency)||v.Note!=""{t.Fatalf("wrong explicit field defaults: %#v",v)}
 // Urgency's default is the second declaration; Go nil is not a default constructor.
 var zero Ticket;if zero.Delivery!=nil||zero.Urgency!=nil{t.Fatal("Go zero changed")}
 v.Note="private business text"
 debug:=DebugTicket(&v)
 for _,part:=range []string{"Waiting","Normal","private business text"}{if !strings.Contains(debug,part){t.Fatalf("debug omitted declared content %q: %s",part,debug)}}
 wire,err:=encodeTicket(v);if err!=nil{t.Fatal(err)}
 if debug==string(wire)||!strings.Contains(string(wire),"queued")||!strings.Contains(string(wire),":2"){t.Fatalf("Debug and wire confused: %s / %s",debug,wire)}
 cloned:=CloneTicket(&v);cloned.Note="changed";if v.Note!="private business text"{t.Fatal("value Clone changed source")}
 if _,err:=encodeTicket(zero);err==nil{t.Fatal("invalid Go enum nil serialized")}
 for _,action:=range []func(){func(){_=DebugTicket(&zero)},func(){_=CloneTicket(&zero)}}{
  panicked:=false;func(){defer func(){panicked=recover()!=nil}();action()}();if !panicked{t.Fatal("invalid Go value passed derived operation")}
 }
}
`
