package acceptance_test

import (
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// enumGeneratedModule exercises the public frontend and instruments the generated
// module itself. Host race instrumentation does not propagate to child commands.
func enumGeneratedModule(t *testing.T, source, assertions string, wantFailure bool, verifyVet ...bool) {
	t.Helper()
	result, err := compileAcceptanceLanguageFixture("enum_acceptance.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range map[string]string{
		"go.mod":       "module enumfixture\n\ngo 1.27\n",
		"generated.go": string(result.Go), "generated_test.go": assertions,
	} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
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
	if wantFailure && (!strings.Contains(string(output), "cannot use") || !strings.Contains(string(output), "type F") || !strings.Contains(string(output), "as E value in return statement")) {
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
pub enum State { Idle, Number(int), Text(string), }
pub enum Outer { Empty, Wrap(State), }
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
pub enum State { Idle, Number(int), }
pub enum Outer { Empty, Wrap(State), }
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
func markNested(seen *int)bool{*seen+=1;return true}
func observePattern(x Outer,seen *int) {
 match x {Outer::Wrap(State::Idle) if markNested(seen)=>{},_=>{},}
}
func observePayloadPattern(x Outer,seen *int)int {
 return match x {Outer::Wrap(State::Number(n)) if markNested(seen)=>n,_=>0,}
}
func bindOuter(x Outer,seen *int)State {
 match x {Outer::Wrap(p)=>{*seen+=1;return p},Outer::Empty=>{return State::Idle},}
}
func bindBeforeNested(x Outer,seen *int)State {
 match x {Outer::Wrap(p) if markNested(seen)=>{return p},Outer::Wrap(State::Idle)=>{return State::Idle},_=>{return State::Idle},}
}
func orderedPatterns(x Outer,first,inner *int) {
 match x {Outer::Wrap(_) if func()bool{*first+=1;return false}()=>{},Outer::Wrap(State::Idle) if markNested(inner)=>{},_=>{},}
}
func dispatchState(x State,seen *int) {
 match x {State::Idle if markNested(seen)=>{},_=>{*seen+=1},}
}
`
	assertions := `package fixture
import("testing";"fmt";"strings")
func TestInvalidBeforeWildcard(t *testing.T){
 values:=[]State{nil,(*Idle)(nil)}
 for _,value:=range values{touched:=false;var failure any;func(){defer func(){failure=recover()}();observe(value,&touched)}();if !strings.Contains(fmt.Sprint(failure),"nil State")||touched{t.Fatalf("invalid %T: panic=%v sideEffect=%t",value,failure,touched)}}
}
func TestNestedBeforeWildcard(t *testing.T){
 for _,inner:=range []State{nil,(*Idle)(nil),(*Number)(nil)}{
  value:=&Wrap{Value:inner}
  touched:=false;observeNested(value,&touched);if !touched{t.Fatal("outer wildcard did not execute")}
  seen:=0;var failure any
  func(){defer func(){failure=recover()}();observePattern(value,&seen)}()
  if !strings.Contains(fmt.Sprint(failure),"nil State")||seen!=0{t.Fatalf("nested pattern panic=%v guard=%d",failure,seen)}
  failure=nil;func(){defer func(){failure=recover()}();_=observePayloadPattern(value,&seen)}()
  if !strings.Contains(fmt.Sprint(failure),"nil State")||seen!=0{t.Fatalf("nested payload pattern panic=%v guard=%d",failure,seen)}
  bound:=bindOuter(value,&seen);if bound!=inner||seen!=1{t.Fatalf("binding changed payload or skipped arm: %T count=%d",bound,seen)}
  seen=0;failure=nil
  func(){defer func(){failure=recover()}();dispatchState(bound,&seen)}()
  if !strings.Contains(fmt.Sprint(failure),"nil State")||seen!=0{t.Fatalf("bound payload dispatch panic=%v guard=%d",failure,seen)}
  seen=0;if got:=bindBeforeNested(value,&seen);got!=inner||seen!=1{t.Fatal("later nested pattern changed earlier binding",seen)}
  first,innerGuard:=0,0;failure=nil
  func(){defer func(){failure=recover()}();orderedPatterns(value,&first,&innerGuard)}()
  if !strings.Contains(fmt.Sprint(failure),"nil State")||first!=1||innerGuard!=0{t.Fatalf("ordered dispatch panic=%v first=%d inner=%d",failure,first,innerGuard)}
 }
 seen:=0;observePattern(&Wrap{Value:&Idle{}},&seen);if seen!=1{t.Fatal("valid nested pattern skipped guard",seen)}
 seen=0;observePattern(&Wrap{Value:&Number{Value:3}},&seen);if seen!=0{t.Fatal("unmatched nested pattern evaluated guard",seen)}
 seen=0;if got:=observePayloadPattern(&Wrap{Value:&Number{Value:3}},&seen);got!=3||seen!=1{t.Fatal("nested pattern lost payload or guard",got,seen)}
}
`
	enumGeneratedModule(t, source, assertions, false)
}

func TestEnumExplicitRejections(t *testing.T) {
	cases := map[string]string{
		"missing":              `enum E { A, B, }; func f(x E){match x {E::A=>{},}}`,
		"guard not exhaustive": `enum E { A, B, }; func f(x E){match x {E::A if true=>{},E::B=>{},}}`,
		"unreachable":          `enum E { A, B, }; func f(x E){match x {E::A=>{},E::A if true=>{},E::B=>{},}}`,
		"nested nonexhaustive": `enum E { A, C, }; enum F { B(E), }; func f(x F){match x {F::B(E::A)=>{},}}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("enum_bad.gox", []byte("package fixture\n"+source))
			if err == nil || !strings.Contains(err.Error(), "enum_bad.gox") {
				t.Fatalf("expected located rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}

func TestEnumNominalIdentity(t *testing.T) {
	_, err := compileAcceptanceLanguageFixture("nominal.gox", []byte(`package fixture
pub enum E { EA }
pub enum F { FA }
func wrong() E { return F::FA }
`))
	if err == nil || !strings.Contains(err.Error(), "nominal.gox") || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("distinct enum identities accepted: %v", err)
	}
}

func TestEnumGeneratedNameHygieneRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
pub enum Choice { Empty, Item(int), }
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
pub enum Choice { Empty, Item(int), }
func panic(v interface{}) {}
func observe(x Choice,touched *bool) {
 match x {Choice::Empty if false=>{*touched=true},_=>{*touched=true},}
}
`
	_, err := compileAcceptanceLanguageFixture("panic_shadow.gox", []byte(source))
	if err == nil || !strings.Contains(err.Error(), "panic_shadow.gox") || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("expected located rejection of validator builtin shadow, got %v", err)
	}
	t.Log(err)
}
