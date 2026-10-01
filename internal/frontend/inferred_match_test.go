package frontend

import (
	"os/exec"
	"strings"
	"testing"
)

func TestInferredMatchNominalTargets(t *testing.T) {
	for name, source := range map[string]string{
		"payload": `enum Event {Value(int),}
type Alias=Event
func f(s Alias)int {match s {_ if false=>{return 0},whole=>{_=whole;return 1},};return 2}
`,
		"option-result": `func f(o Option[int],r Result[int,string])int {match o {_=>{},};match r {v=>{_=v},};return 1}
`,
		"propagation": `enum Event {Value(int),}
func load()Result[Event,string]{return Result[Event,string]::Ok(Event::Value(1))}
func f()Result[int,string]{match load()?{whole=>{_=whole},};return Result[int,string]::Ok(1)}
`,
		"macro": `enum Event {Value(int),}
macro_rules! identity {($v:expr)=>{$v};}
func f(v Event){match identity!(v){_=>{},}}
`,
	} {
		t.Run(name, func(t *testing.T) {
			r, err := Compile("infer.hgo", []byte("package p\n"+source))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(r.Go), "__hgo_enum_infer") {
				t.Fatal("inference IR escaped")
			}
		})
	}
}
func TestInferredMatchRejectsUnprovenIdentity(t *testing.T) {
	for name, source := range map[string]string{
		"native": `func f(s string){match s {_=>{},}}`,
		"defined-scalar": `type Status string
type Other Status
func f(s Other){match s {_=>{},}}`,
		"any": `enum Event {Value(int),}
func f(s Event){var erased any=s;match erased {_=>{},}}`,
		"nil":            `func f(){match nil {_=>{},}}`,
		"type-parameter": `func f[T any](v T){match v {_=>{},}}`,
		"shadowed-type": `enum Event {Value(int),}
func f(){type Event int;var v Event;match v {_=>{},}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Compile("bad-infer.hgo", []byte("package p\n"+source))
			want := "known nominal enum"
			if name == "type-parameter" {
				want = "user generic functions unsupported"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected identity diagnostic, got %v", err)
			}
		})
	}
	for _, arms := range []string{`_ if true=>{},`, `_=>{},v=>{},`} {
		_, err := Compile("coverage.hgo", []byte("package p\nenum Event {Value(int),}\nfunc f(v Event){match v{"+arms+"}}"))
		if err == nil || (!strings.Contains(err.Error(), "non-exhaustive") && !strings.Contains(err.Error(), "unreachable")) {
			t.Fatalf("coverage: %v", err)
		}
	}
}
func TestInferredMatchPackageFunctionTarget(t *testing.T) {
	_, err := CompilePackage("example/p", map[string][]byte{
		"defs.hgo":     []byte("package p\nenum event {Value(int),}\nfunc makeEvent()event{return event::Value(7)}\n"),
		"consumer.hgo": []byte("package p\nfunc Run()int{match makeEvent(){value=>{_=value;return 7},};return 0}\n"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestInferredMatchImportedAndSumValidation(t *testing.T) {
	out, err := compileImportedFixture(t, map[string]string{
		"producer/types.hgo": `package producer
pub enum Event {Value(int),}
`,
		"consumer.hgo": `package app
import p "example.org/qualified/producer"
var Effects int
func Payload(v p.Event) {p:=0;_=p;match v {_ if false=>{Effects++},whole=>{_=whole;Effects++},}}
func OptionZero() {var v Option[int];match v {_=>{Effects++},}}
func ResultZero() {var v Result[int,string];match v {_=>{Effects++},}}
`,
		"consumer_test.go": `package app
import("testing";p "example.org/qualified/producer")
func TestInferred(t *testing.T){
 Payload(p.NewEventValue(3));OptionZero()
 if Effects!=2 {t.Fatal("valid sum",Effects)}
 for _, f:=range []func(){func(){Payload(nil)},ResultZero}{
  func(){defer func(){if recover()==nil {t.Error("invalid sum accepted")}}();f()}()
  if Effects!=2 {t.Fatal("body before validation")}
 }
}
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-race", "./...")
	cmd.Dir = out
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated module: %v\n%s", err, data)
	}
}
