package acceptance_test

import (
	"strings"
	"testing"
)

// Exercise the generated module under -race, not merely the compiler process.
// Sum payloads here are ordinary values; reference payloads remain a separate,
// explicitly rejected boundary below rather than an implied supported feature.
func TestT0ReferencePropagationDeriveMacroRace(t *testing.T) {
	macroRaceModule(t, t0CombinationSource, `package fixture
import("testing";"reflect";"errors")
func TestIdentityEvaluationAndCleanup(t *testing.T){
 for _,ok:=range []bool{false,true}{trace:=[]string{};got:=exercise(ok,&trace);want:=[]string{"load","cleanup"};n:=-1;if ok{want=[]string{"load","continued","cleanup"};n=14};if number(got)!=n||!reflect.DeepEqual(trace,want){t.Fatalf("ok=%t got=%d trace=%v",ok,number(got),trace)};if !ok&&!errors.Is(cause(got),sentinel){t.Fatal("derive From lost error identity")}}
}
func TestClosureBoundaryAndOption(t *testing.T){trace:=[]string{};if number(outer(&trace))!=7||!reflect.DeepEqual(trace,[]string{"load","inner-cleanup","outer-continued"}){t.Fatalf("closure trace=%v",trace)};if optionNumber(optionRead(true))!=5||optionNumber(optionRead(false))!=-1{t.Fatal("Option propagation with reference local")}}
func TestCloneBesideReferences(t *testing.T){if cloneRead()!=3{t.Fatal("Clone or read reference changed")};if PhantomRun()!=7{t.Fatal("phantom type or macro call changed")}}
func TestDirectDeferredReferenceArgument(t *testing.T){for _,ok:=range []bool{false,true}{trace:=[]string{};want:=2;if ok{want=6};if n:=runDeferred(ok,&trace);n!=want||!reflect.DeepEqual(trace,[]string{"load"}){t.Fatalf("ok=%t value=%d trace=%v",ok,n,trace)}}}
`)
}

const t0CombinationSource = `package fixture
use errors;
#[derive(Error)]
#[error("failed")]
type Failure struct {
 #[from]
 Cause error
}
#[derive(Clone)]
type Data struct { Values []int }
var sentinel=errors::New("sentinel")
func load(ok bool,trace *[]string)Result[int,error]{*trace=append(*trace,"load");if !ok{return Result[int,error]::Err(sentinel)};return Result[int,error]::Ok(4)}
func read(r &int)int{return *r}
func bump(w &mut int){(*w)++}
func capture(r &int)func()int{return func()int{return *r}}
func scalar(n int)int{return n}
macro_rules! access {($r:expr)=>{scalar(*$r)};}
macro_rules! take {($v:expr)=>{$v?};}
func exercise(ok bool,trace *[]string)Result[int,Failure]{
 n:=2;w:=&mut n;alias:=w;bump(alias)
 var r &int = &(*w)
 reader:=capture(r)
 defer func(){*trace=append(*trace,"cleanup")}()
 value:=take!(match load(ok,trace){Result[int,error]::Ok(value)=>Result[int,Failure]::Ok(value),Result[int,error]::Err(cause)=>Result[int,Failure]::Err(FromFailure(cause)),})
 bump(w)
 scalar:=func(int)int{return -100};_=scalar
 *trace=append(*trace,"continued")
 return Result[int,Failure]::Ok(access!(r)+reader()+value+n-2)
}
func outer(trace *[]string)Result[int,Failure]{
 n:=7;var r &int = &n
 inner:=func()Result[int,error]{defer func(){*trace=append(*trace,"inner-cleanup")}();_=load(false,trace)?;return Result[int,error]::Ok(read(r))}
 _=inner();*trace=append(*trace,"outer-continued");return Result[int,Failure]::Ok(read(r))
}
func optional(ok bool)Option[int]{if ok{return Option[int]::Some(3)};return Option[int]::None}
func optionRead(ok bool)Option[int]{n:=2;var r &int = &n;v:=optional(ok)?;return Option[int]::Some(v+read(r))}
func number(v Result[int,Failure])int{match v {Result[int,Failure]::Ok(n)=>{return n},Result[int,Failure]::Err(_)=>{return -1},};panic("unreachable")}
func cause(v Result[int,Failure])error{match v {Result[int,Failure]::Ok(_)=>{return nil},Result[int,Failure]::Err(e)=>{return e},};panic("unreachable")}
func optionNumber(v Option[int])int{match v {Option[int]::Some(n)=>{return n},Option[int]::None=>{return -1},};panic("unreachable")}
func cloneRead()int{n:=3;var r &int = &n;original:=Data{Values:[]int{read(r)}};copy:=CloneData(&original);copy.Values[0]=8;return original.Values[0]}
func deferBump(w &mut int,n int){*w+=n}
func registerDeferred(w &mut int,ok bool,trace *[]string)Result[int,error]{defer deferBump(w,load(ok,trace)?);return Result[int,error]::Ok(*w)}
func runDeferred(ok bool,trace *[]string)int{n:=2;w:=&mut n;_=registerDeferred(w,ok,trace);return n}
type PhantomBox[T any]struct{N int}
func phantomBox[T any](v T)PhantomBox[T]{return PhantomBox[T]{N:1}}
macro_rules! phantomAccess {($r:expr)=>{read($r)};}
func phantomExercise(r &int)Option[int]{b:=PhantomBox[&int]{N:2};g:=phantomBox(r);_=Option[int]::Some(1)?;return Option[int]::Some(phantomAccess!(r)+b.N+g.N)}
func PhantomRun()int{n:=4;return match phantomExercise(&n){Option[int]::Some(v)=>v,Option[int]::None=>-1}}
`

func TestT0ReferenceSumAndGenericBoundaries(t *testing.T) {
	cases := map[string]string{
		"generic-box-standalone-any": `type Box[T any]struct{Value T};func f(r &int)any{return Box[&int]{Value:r}}`,
		"option-payload-write":       `func f(r &int)Option[int]{v:=Option[&int]::Some(r)?;*v=9;return Option[int]::Some(*v)}`,
		"result-payload-write":       `func f(r &int)Result[int,error]{v:=Result[&int,error]::Ok(r)?;*v=9;return Result[int,error]::Ok(*v)}`,
		"option-payload-raw":         `func f(r &int)Option[*int]{v:=Option[&int]::Some(r)?;var raw *int=v;return Option[*int]::Some(raw)}`,
		"result-payload-any":         `func f(r &int)Result[any,error]{v:=Result[&int,error]::Ok(r)?;return Result[any,error]::Ok(v)}`,
		"generic-identity-write":     `func id[T any](v T)T{return v};func f(r &int)Option[int]{v:=id(r);*v=9;return Option[int]::Some(*v)}`,
		"generic-identity-raw":       `func id[T any](v T)T{return v};func f(r &int)Option[int]{var v *int=id(r);*v=9;return Option[int]::Some(*v)}`,
		"generic-box-any":            `type Box[T any]struct{Value T};func f(r &int)Option[any]{return Option[any]::Some(Box[&int]{Value:r})}`,
		"macro-write-after-question": `macro_rules! write {($r:expr)=>{*$r=9};}
func f(r &int)Option[int]{_=Option[int]::Some(1)?;write!(r);return Option[int]::Some(*r)}`,
		"macro-any-after-question": `macro_rules! box {($r:expr)=>{any($r)};}
func f(r &int)Option[any]{_=Option[int]::Some(1)?;return Option[any]::Some(box!(r))}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("t0_boundary.gox", []byte("package fixture\n"+body+"\n"))
			if err == nil || !strings.Contains(err.Error(), "t0_boundary.gox") {
				t.Fatalf("need located boundary rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}

// Generic instantiation must retain permissions when a reference value escapes.
func TestT0ReferenceGenericInstantiationBoundary(t *testing.T) {
	for name, body := range map[string]string{
		"boxed-any":      `type Box[T any]struct{Value T};func box[T any](v T)Box[T]{return Box[T]{Value:v}};func f(r &int)any{return box(r)}`,
		"identity-write": `func id[T any](v T)T{return v};func f(r &int){v:=id(r);*v=9}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("t0_generic.gox", []byte("package fixture\n"+body))
			if err == nil || !strings.Contains(err.Error(), "t0_generic.gox") {
				t.Fatalf("need located generic reference rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}
