package acceptance_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/frontend"
)

// enumGeneratedModule exercises the public frontend and instruments the generated
// module itself. Host race instrumentation does not propagate to child commands.
func enumGeneratedModule(t *testing.T, source, assertions string, wantFailure bool, verifyVet ...bool) {
	t.Helper()
	result, err := frontend.Compile("enum_acceptance.hgo", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	dir := t.TempDir()
	for name, data := range map[string]string{
		"go.mod":       "module enumfixture\n\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => " + root + "\n",
		"generated.go": string(result.Go), "generated_test.go": assertions,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "test", "-race", "-count=1", "-v", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	output, err := cmd.CombinedOutput()
	if (err != nil) != wantFailure {
		t.Fatalf("generated race module expected failure=%t: %v\n%s\n%s", wantFailure, err, output, result.Go)
	}
	if wantFailure && !strings.Contains(string(output), "does not implement E") {
		t.Fatalf("unexpected compile failure: %s", output)
	}
	t.Logf("generated go test -race -count=1 -v .:\n%s", output)
	if !wantFailure && len(verifyVet) > 0 && verifyVet[0] {
		vet := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "vet", "./...")
		vet.Dir, vet.Env = dir, cmd.Env
		if output, err := vet.CombinedOutput(); err != nil {
			t.Fatalf("generated go vet: %v\n%s", err, output)
		}
	}
}

func TestEnumControlFlowRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
enum State { Idle, Number(int), Text(string), }
enum Outer { Empty, Wrap(State), }
func makeNumber(n int) State {return State::Number(n)}
func ordered(next func() State, seen *[]int) int {
 mark:=func(n int,b bool)bool{*seen=append(*seen,n);return b}
 match next() {
  State::Number(n) if mark(1,false) => {return n},
  _ if mark(2,false) => {return -2},
  State::Number(n) if mark(3,true) => {return n+10},
  State::Number(n) => {return n},
  State::Idle => {return 0},
  State::Text(_) => {return -1},
 }
 panic("unreachable")
}
func deferred(done *int) int {
 match State::Number(4) {
  State::Number(n) => {defer func(){*done=n}();return n},
  _ => {return 0},
 }
 panic("unreachable")
}
func loop() int {
 total:=0
 for i:=0;i<5;i++ {
  match State::Number(i) {
   State::Number(n) if n==1 => {continue},
   State::Number(n) if n==3 => {break},
   _ => {total++},
  }
  total+=10
 }
 return total
}
func nested(x Outer) int {
 match x {
  Outer::Empty => {return 0},
  Outer::Wrap(inner) => {
   match inner {
    State::Number(n) => {return n},
    _ => {return -1},
   }
  },
 }
 panic("unreachable")
}
func freshBinding() int {
 match State::Number(7) {
  State::Number(n) if func()bool{n=99;return false}() => {return n},
  State::Number(n) => {return n},
  _ => {return -1},
 }
 panic("unreachable")
}
func ordinary() int {return nested(Outer::Wrap(State::Number(9)))}
`, `package fixture
import("testing";"reflect")
func TestOrderAndSingleEvaluation(t *testing.T){calls:=0;seen:=[]int{};got:=ordered(func()State{calls++;return makeNumber(5)},&seen);if got!=15||calls!=1||!reflect.DeepEqual(seen,[]int{1,2,3}){t.Fatalf("got %d calls %d seen %v",got,calls,seen)}}
func TestReturnDeferLoop(t *testing.T){done:=0;if deferred(&done)!=4||done!=4{t.Fatal("return/defer changed")};if n:=loop();n!=22{t.Fatalf("break/continue changed: %d",n)}}
func TestNested(t *testing.T){if ordinary()!=9{t.Fatal("nested mismatch")};if n:=freshBinding();n!=7{t.Fatalf("guard-local assignment polluted next binding: %d",n)}}
`, false)
}

func TestEnumInvalidBoundaryRace(t *testing.T) {
	source := `package fixture
enum State { Idle, Number(int), }
enum Outer { Empty, Wrap(State), }
func observe(x State, touched *bool) {
 match x {
  State::Idle if false => {*touched=true},
  _ => {*touched=true},
 }
}
func observeNested(x Outer,touched *bool) {
 match x {
  Outer::Empty if false => {*touched=true},
  _ => {*touched=true},
 }
}
`
	lowered, err := enums.Lower("enum_acceptance.hgo", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	idle, wrap := "", ""
	for _, d := range lowered.Enums {
		for _, v := range d.Variants {
			if d.Name == "State" && v.Name == "Idle" {
				idle = v.CaseName
			}
			if d.Name == "Outer" && v.Name == "Wrap" {
				wrap = v.CaseName
			}
		}
	}
	if idle == "" || wrap == "" {
		t.Fatal("missing public lowering metadata")
	}
	assertions := strings.NewReplacer("IDLE_CASE", idle, "WRAP_CASE", wrap).Replace(`package fixture
import "testing"
type forged struct{State}
func TestInvalidBeforeWildcard(t *testing.T){
 values:=[]State{nil,(*IDLE_CASE)(nil),&IDLE_CASE{},forged{}}
 for _,value:=range values{touched:=false;panicked:=false;func(){defer func(){panicked=recover()!=nil}();observe(value,&touched)}();if !panicked||touched{t.Fatalf("invalid %T: panic=%t sideEffect=%t",value,panicked,touched)}}
}
func TestNestedBeforeWildcard(t *testing.T){touched:=false;panicked:=false;func(){defer func(){panicked=recover()!=nil}();observeNested(WRAP_CASE{Value:nil},&touched)}();if !panicked||touched{t.Fatalf("nested invalid panic=%t sideEffect=%t",panicked,touched)}}
`)
	enumGeneratedModule(t, source, assertions, false)
}

func TestEnumExplicitRejections(t *testing.T) {
	cases := map[string]string{
		"missing":              `enum E { A, B, }; func f(x E){match x {E::A=>{},}}`,
		"guard not exhaustive": `enum E { A, B, }; func f(x E){match x {E::A if true=>{},E::B=>{},}}`,
		"unreachable":          `enum E { A, B, }; func f(x E){match x {E::A=>{},E::A if true=>{},E::B=>{},}}`,
		"generic":              `enum E[T] { A(T), }`,
		"pointer payload":      `enum E { A(*int), }`,
		"recursive":            `enum E { A(E), }`,
		"nested pattern":       `enum E { A, }; enum F { B(E), }; func f(x F){match x {F::B(E::A)=>{},}}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("enum_bad.hgo", []byte("package fixture\n"+source))
			if err == nil || !strings.Contains(err.Error(), "enum_bad.hgo") {
				t.Fatalf("expected located rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}

func TestEnumNominalIdentity(t *testing.T) {
	enumGeneratedModule(t, `package fixture
enum E { A, }
enum F { A, }
func wrong() E {return F::A}
`, "package fixture\n", true)
}

func TestEnumGeneratedNameHygieneRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
enum Choice { Empty, Item(int), }
func shadowFalse() int {
 false:=true;_ = false
 match Choice::Item(8) {
  Choice::Item(n)=>{return n},
  _=>{return -1},
 }
 return -2
}
func shadowTrue() int {
 true:=false;_ = true
 total:=0
 match Choice::Item(8) {
  Choice::Item(n)=>{total=n},
  _=>{total=-1},
 }
 return total
}
func shadowAny() int {
 any:=func(v interface{})interface{}{return nil};_ = any
 match Choice::Item(8) {
  Choice::Item(n)=>{return n},
  _=>{return -1},
 }
 return -2
}
`, `package fixture
import "testing"
func TestGeneratedNamesCannotBeCaptured(t *testing.T){for name,run:=range map[string]func()int{"false":shadowFalse,"true":shadowTrue,"any":shadowAny}{t.Run(name,func(t *testing.T){if got:=run();got!=8{t.Fatalf("source %s captured generated logic: %d",name,got)}})}}
`, false)
}

func TestEnumRejectsValidatorPanicShadow(t *testing.T) {
	source := `package fixture
enum Choice { Empty, Item(int), }
func panic(v interface{}) {}
func observe(x Choice,touched *bool) {
 match x {Choice::Empty if false=>{*touched=true},_=>{*touched=true},}
}
`
	_, err := frontend.Compile("panic_shadow.hgo", []byte(source))
	if err == nil || !strings.Contains(err.Error(), "panic_shadow.hgo") || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected located rejection of validator builtin shadow, got %v", err)
	}
	t.Log(err)
}
