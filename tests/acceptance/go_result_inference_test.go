package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/rootconfig"
)

func TestGoResultInference(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", packagesModule()+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, source, map[string]rootconfig.Mapping{
		"std":   {Module: "github.com/goxide-lang/std"},
		"ops":   {Module: "packages.example/app", Package: "packages.example/app/ops"},
		"model": {Module: "packages.example/app", Package: "packages.example/app/model"},
	})
	packagesWrite(t, source, "ops/macros.gox", `package ops
pub macro_rules! infer {($x:expr)=>{go_result!($x)};}
pub macro_rules! retain {($x:expr)=>{go_result_keep!($x)};}
`)
	packagesWrite(t, source, "native.go", `package app
func Generic[T any](v T)(T,error){return v,nil}
func invalid()([]Event,error){return []Event{nil},nil}
`)
	packagesWrite(t, source, "a.gox", `package app
use io;
use strconv;
use strings;
use ops as m;
type Failure struct{N int}
func(e *Failure)Error()string{return "failure"}
func(e *Failure)Code()int{if e==nil{return 0};return e.N}
type NamedError interface{error;Code()int}
type Alias = NamedError
type Partial = struct{Value int; Err *Failure}
var sentinel=&Failure{N:5}
var trace []int
func mark(n int)int{trace=append(trace,n);return n}
func pointer(ok bool)(int,*Failure){mark(3);if ok{return 7,nil};return 4,sentinel}
func named(mode int)(int,NamedError){if mode==0{return 7,nil};if mode==1{var e *Failure;return 4,e};return 4,sentinel}
func erased()(int,error){var e *Failure;return 4,e}
func closeError()NamedError{return nil}
func Pointer(ok bool)Result[int,*Failure]{return go_result!(pointer(ok))}
func Explicit(ok bool)Result[int,*Failure]{return go_result!(int,pointer(ok))}
func Retained(ok bool)Result[int,Partial]{return m::retain!(pointer(ok))}
func Named(mode int)Result[int,Alias]{return go_result!(named(mode))}
func Erased()Result[int,error]{return go_result!(erased())}
func Unit()Result[struct{},NamedError]{return go_result!(closeError())}
func UnitKeep()Result[struct{},struct{Value struct{};Err NamedError}]{return go_result_keep!(closeError())}
func Parse(s string)Result[int,error]{return m::infer!(strconv::Atoi(s))}
func Nested(ok bool)Result[int,error]{defer func(){mark(9)}();n:=go_result!(Generic(Pointer(ok)?))?;return Result[int,error]::Ok(n)}
func Propagate(ok bool)Result[string,Partial]{n:=go_result_keep!(pointer(ok))?;return Result[string,Partial]::Ok(strconv::Itoa(n))}
func BoxPartial()Result[string,any]{_=go_result_keep!(pointer(false))?;return Result[string,any]::Ok("")}
func AnyOrder()Result[int,*Failure]{f:=func()func(int)(int,*Failure){mark(1);return func(n int)(int,*Failure){mark(3);return n,nil}};return go_result!(f()(mark(2)))}
func Variadic()Result[int,*Failure]{f:=func(xs ...int)(int,*Failure){return xs[0]+xs[1],nil};return go_result!(f([]int{3,4}...))}
func Reader()Result[int,struct{Value int;Err error}]{r:=strings::NewReader("abc");return go_result_keep!(io::ReadFull(r,make([]byte,8)))}
func Recover()(v any){defer func(){f:=func(x any)(any,*Failure){v=x;return x,nil};_=go_result!(f(recover()))}();panic("boom")}
func Hygiene()Result[int,*Failure]{nil:=42;error:=5;Result:=9;_=nil;_=error;_=Result;return go_result!(pointer(true))}
func Cross()Result[int,*Failure]{n:=InferredGlobal?;return Result[int,*Failure]::Ok(n+1)}
func NestedAdapter()Result[int,error]{value:=go_result!(Generic(go_result!(strconv::Atoi("8"))?))?;return Result[int,error]::Ok(value)}
type SliceError []int
func(SliceError)Error()string{return "slice"}
func sliceError(empty bool)(int,SliceError){if empty{return 4,SliceError{}};return 7,nil}
func Slice(empty bool)Result[int,SliceError]{return go_result!(sliceError(empty))}
type MapError map[string]int
func(MapError)Error()string{return "map"}
func Map(v MapError)Result[struct{},MapError]{f:=func()MapError{return v};return go_result!(f())}
type FuncError func()
func(FuncError)Error()string{return "func"}
func Func(v FuncError)Result[struct{},FuncError]{f:=func()FuncError{return v};return go_result!(f())}
type ChanError chan int
func(ChanError)Error()string{return "chan"}
func Chan(v ChanError)Result[struct{},ChanError]{f:=func()ChanError{return v};return go_result!(f())}
pub enum Event{A,B,}
func BindNested(seen *int)Result[[]Event,error]{values:=go_result!(invalid())?;*seen+=1;return Result[[]Event,error]::Ok(values)}
func ValidateNested(seen *int){r:=go_result!(invalid());match r {Result[[]Event,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[[]Event,error]::Err(_)=>{panic("unexpected error")},}}
`)
	packagesWrite(t, source, "z.gox", `package app
var InferredGlobal=go_result!(pointer(true))
`)
	generated := filepath.Join(t.TempDir(), "generated")
	packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
	again := filepath.Join(t.TempDir(), "generated")
	packagesCommand(t, root, cli, true, "transpile", "-o", again, source)
	one, _ := os.ReadFile(filepath.Join(generated, "a.go"))
	two, _ := os.ReadFile(filepath.Join(again, "a.go"))
	if string(one) != string(two) {
		t.Fatal("unstable typed adapter output")
	}
	packagesWrite(t, generated, "inference_test.go", `package app
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"errors";"fmt";"io";"reflect";"strings")
func TestPointerAndNamed(t *testing.T){if v:=__borrow_std.ValueOk(Pointer(true));v!=7{t.Fatal(v)};if e:=__borrow_std.ValueErr(Pointer(false));e!=sentinel{t.Fatal(e)};if e:=__borrow_std.ValueErr(Explicit(false));e!=sentinel{t.Fatal(e)};if ok := __borrow_std.TagResult(Named(0))==1;!ok{t.Fatal("named nil")};e:=__borrow_std.ValueErr(Named(1));var ptr *Failure;if e==nil||!errors.As(e,&ptr)||ptr!=nil{t.Fatal("interface typed nil")};if e:=__borrow_std.ValueErr(Named(2));e!=sentinel{t.Fatal(e)};if e:=__borrow_std.ValueErr(Erased());e==nil||!errors.As(e,&ptr){t.Fatal("erased")};if ok := __borrow_std.TagResult(Unit())==1;!ok{t.Fatal("unit")};if ok := __borrow_std.TagResult(UnitKeep())==1;!ok{t.Fatal("keep unit")};if v:=__borrow_std.ValueOk(Hygiene());v!=7{t.Fatal(v)}}
func TestPreserve(t *testing.T){e:=__borrow_std.ValueErr(Retained(false));if e.Value!=4||e.Err!=sentinel{t.Fatal(e)};v:=__borrow_std.ValueErr(Propagate(false));if v!=e{t.Fatal(v)};boxed:=__borrow_std.ValueErr(BoxPartial()).(Partial);if boxed!=e{t.Fatal(boxed)};p:=__borrow_std.ValueErr(Reader());if p.Value!=3||!errors.Is(p.Err,io.ErrUnexpectedEOF){t.Fatal(p)}}
func TestFrames(t *testing.T){trace=nil;if e:=__borrow_std.ValueErr(Nested(false));e!=sentinel||!reflect.DeepEqual(trace,[]int{3,9}){t.Fatal(e,trace)};trace=nil;if n:=__borrow_std.ValueOk(Nested(true));n!=7||!reflect.DeepEqual(trace,[]int{3,9}){t.Fatal(n,trace)};trace=nil;if n:=__borrow_std.ValueOk(AnyOrder());n!=2||!reflect.DeepEqual(trace,[]int{1,2,3}){t.Fatal(n,trace)};if n:=__borrow_std.ValueOk(Variadic());n!=7{t.Fatal(n)};if Recover()!="boom"{t.Fatal("recover")};if n:=__borrow_std.ValueOk(Cross());n!=8{t.Fatal(n)};if n:=__borrow_std.ValueOk(NestedAdapter());n!=8{t.Fatal(n)};if n:=__borrow_std.ValueOk(Parse("9"));n!=9{t.Fatal(n)}}
func TestNilable(t *testing.T){if ok := __borrow_std.TagResult(Slice(false))==1;!ok{t.Fatal("nil slice")};if e:=__borrow_std.ValueErr(Slice(true));e==nil||len(e)!=0{t.Fatal(e)};if ok := __borrow_std.TagResult(Map(nil))==1;!ok{t.Fatal("nil map")};if e:=__borrow_std.ValueErr(Map(MapError{}));e==nil{t.Fatal(e)};if ok := __borrow_std.TagResult(Func(nil))==1;!ok{t.Fatal("nil func")};if e:=__borrow_std.ValueErr(Func(func() {}));e==nil{t.Fatal("nonnull func")};if ok := __borrow_std.TagResult(Chan(nil))==1;!ok{t.Fatal("nil chan")};if e:=__borrow_std.ValueErr(Chan(make(ChanError)));e==nil{t.Fatal("nonnull chan")}}
func TestNestedValidation(t *testing.T){seen:=0;values:=__borrow_std.ValueOk(BindNested(&seen));if seen!=1||len(values)!=1||values[0]!=nil{t.Fatal("adapter changed Go payload or inspected its elements",seen,values)};seen=0;defer func(){p:=recover();if !strings.Contains(fmt.Sprint(p),"nil Event")||seen!=1{t.Fatalf("element dispatch panic=%v continuation=%d",p,seen)}}();ValidateNested(&seen)}
`)
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, generated, tool, true, "vet", "./...")
	// Reverse which file is visited first: package initialization must expose the
	// same real Result identity to question lowering in either order.
	if err := os.Rename(filepath.Join(source, "a.gox"), filepath.Join(source, "zz.gox")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(source, "z.gox"), filepath.Join(source, "aa.gox")); err != nil {
		t.Fatal(err)
	}
	reversed := filepath.Join(t.TempDir(), "reversed")
	packagesCommand(t, root, cli, true, "transpile", "-o", reversed, source)
	assertions, err := os.ReadFile(filepath.Join(generated, "inference_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, reversed, "inference_test.go", string(assertions))
	packagesCommand(t, reversed, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, reversed, tool, true, "vet", "./...")
	for name, tc := range map[string]struct{ body, want string }{
		"nonerror":        {`func f()(int,string){return 1,""};func Bad(){_=go_result!(f())}`, "must implement"},
		"non-nilable":     {`type E struct{};func(E)Error()string{return "e"};func f()(int,E){return 1,E{}};func Bad(){_=go_result!(f())}`, "must be nilable"},
		"arity":           {`func f()(int,int,error){return 1,2,nil};func Bad(){_=go_result!(f())}`, "wrap multiple values"},
		"zero":            {`func f(){};func Bad(){_=go_result!(f())}`, "expected E"},
		"not-call":        {`func Bad(e error){_=go_result!(e)}`, "operand must be a call"},
		"partial-discard": {`func f()(int,error){return 1,nil};func Bad()Result[int,error]{n:=go_result_keep!(f())?;return Result[int,error]::Ok(n)}`, "requires GoxideTraitFrom_"},
		"from-chain": {`type E struct{};func(*E)Error()string{return "e"};func f()(int,*E){return 1,&E{}}
#[derive(std::derive::Error)]
#[error("failure")]
type Failure struct{
#[from]
Cause error
}
func Bad()Result[int,Failure]{n:=go_result!(f())?;return Result[int,Failure]::Ok(n)}`, "requires GoxideTraitFrom_"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			packagesWrite(t, dir, "go.mod", packagesModule()+stdFixtureGoRequirements(t))
			writeMacroFixtureRoots(t, dir, map[string]rootconfig.Mapping{
				"std":   {Module: "github.com/goxide-lang/std"},
				"ops":   {Module: "packages.example/app", Package: "packages.example/app/ops"},
				"model": {Module: "packages.example/app", Package: "packages.example/app/model"},
			})
			packagesWrite(t, dir, "bad.gox", "package bad; use std;\n"+tc.body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), dir)
			if !strings.Contains(out, tc.want) {
				t.Fatal(out)
			}
		})
	}
}

func TestGoResultInferenceModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	producer := t.TempDir()
	packagesWrite(t, producer, "go.mod", strings.Replace(packagesModule(), "packages.example/app", "inferred.example/producer", 1)+stdFixtureGoRequirements(t))
	writeMacroFixtureRoots(t, producer, map[string]rootconfig.Mapping{
		"std":      {Module: "github.com/goxide-lang/std"},
		"producer": {Module: "inferred.example/producer", Package: "inferred.example/producer"},
		"other":    {Module: "inferred.example/producer", Package: "inferred.example/producer/other"},
	})
	packagesWrite(t, producer, "native.go", `package producer
 type secret int
 type Count = secret
 type hiddenError struct{N int}
 func(*hiddenError)Error()string{return "error"}
 type Error = hiddenError
 var Sentinel=&hiddenError{5}
 func Read(ok bool)(secret,*hiddenError){if ok{return 7,nil};return 4,Sentinel}
 type inaccessible int
 func HiddenT()(inaccessible,error){return 1,nil}
 type inaccessibleError struct{}
 func(*inaccessibleError)Error()string{return "hidden"}
 func HiddenE()(int,*inaccessibleError){return 1,nil}
 func InvalidEvents()([]Event,error){return []Event{nil},nil}
 `)
	packagesWrite(t, producer, "adapter.gox", `package producer
 type Partial = struct{Value Count; Err *Error}
 func Wrapped(ok bool)Result[Count,Partial]{return go_result_keep!(Read(ok))}
 pub enum Event{Ready,Failed,}
 `)
	packagesWrite(t, producer, "other/event.gox", `package other
 pub enum Event{Other,Failed,}
 func Events()([]Event,error){return []Event{Event::Other},nil}
 `)
	prodOut := filepath.Join(t.TempDir(), "producer")
	packagesCommand(t, root, cli, true, "transpile", "-o", prodOut, producer)
	consumer := t.TempDir()
	writeMacroFixtureRoots(t, consumer, map[string]rootconfig.Mapping{
		"std":      {Module: "github.com/goxide-lang/std"},
		"producer": {Module: "inferred.example/producer", Package: "inferred.example/producer"},
		"other":    {Module: "inferred.example/producer", Package: "inferred.example/producer/other"},
	})
	mod := strings.Replace(packagesModule(), "packages.example/app", "inferred.example/consumer", 1) + stdFixtureGoRequirements(t) + "\nrequire inferred.example/producer v0.0.0\nreplace inferred.example/producer => " + filepath.ToSlash(prodOut) + "\n"
	packagesWrite(t, consumer, "go.mod", mod)
	packagesWrite(t, consumer, "a.gox", `package consumer
 use producer as p;
 func Native(ok bool)Result[p::Count,*p::Error]{return go_result!(p::Read(ok))}
 func Keep()Result[p::Count,p::Partial]{return go_result_keep!(p::Read(false))}
 func Forward()Result[string,p::Partial]{_=p::Wrapped(false)?;return Result[string,p::Partial]::Ok("unexpected")}
 func Match()int{return match p::Wrapped(false){Result[p::Count,p::Partial]::Ok(n)=>int(n),Result[p::Count,p::Partial]::Err(e)=>int(e.Value)}}
 func Cross()Result[p::Count,*p::Error]{n:=Inferred?;return Result[p::Count,*p::Error]::Ok(n)}
 func NamedForeignInvalid(seen *int){r:=go_result!(localEvents());match r {Result[Events,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[Events,error]::Err(_)=>{panic("unexpected error")},}}
 func ExplicitForeignInvalid(seen *int){r:=Result[Events,error]::Ok(explicitEvents());match r {Result[Events,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[Events,error]::Err(_)=>{panic("unexpected error")},}}
 func ForeignInvalid(seen *int){r:=go_result!(p::InvalidEvents());match r {Result[[]p::Event,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[[]p::Event,error]::Err(_)=>{panic("unexpected error")},}}
 `)
	packagesWrite(t, consumer, "z.gox", `package consumer
 use producer as p;
 type Events []p::Event
 var Inferred=go_result!(p::Read(true))
 `)
	packagesWrite(t, consumer, "left.gox", `package consumer
 use producer as m;
 type Left []m::Event
 `)
	packagesWrite(t, consumer, "right.gox", `package consumer
 use other as m;
 type Right []m::Event
 func right()(Right,error){return m::Events()}
 `)
	packagesWrite(t, consumer, "scope.gox", `package consumer
 func LeftInvalid(seen *int){r:=go_result!(invalidLeft());match r {Result[Left,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[Left,error]::Err(_)=>{panic("unexpected error")},}}
 func RightInvalid(seen *int){r:=go_result!(invalidRight());match r {Result[Right,error]::Ok(values)=>{*seen+=1;match values[0]{_=>{*seen+=1},}},Result[Right,error]::Err(_)=>{panic("unexpected error")},}}
 func RightValid(){r:=go_result!(right());match r {_=>{},}}
 `)
	packagesWrite(t, consumer, "native.go", `package consumer
func localEvents()(Events,error){return Events{nil},nil}
func explicitEvents()Events{return Events{nil}}
func invalidLeft()(Left,error){return Left{nil},nil}
func invalidRight()(Right,error){return Right{nil},nil}
`)
	out := filepath.Join(t.TempDir(), "consumer")
	compileAcceptanceGraph(t, true, consumer, out, producer)
	packagesWrite(t, out, "consumer_test.go", `package consumer
import __borrow_std "github.com/goxide-lang/std/sum"

 import("testing";"fmt";"strings";p "inferred.example/producer")
 func TestIdentity(t *testing.T){if n:=__borrow_std.ValueOk(Native(true));n!=7{t.Fatal(n)};if e:=__borrow_std.ValueErr(Native(false));e!=p.Sentinel{t.Fatal(e)};a:=__borrow_std.ValueErr(Keep());b:=__borrow_std.ValueErr(Forward());if a.Value!=4||a.Err!=p.Sentinel||a!=b||Match()!=4{t.Fatal(a,b)};if n:=__borrow_std.ValueOk(Cross());n!=7{t.Fatal(n)}}
 func TestForeignPayload(t *testing.T){RightValid();for _, f:=range []func(*int){ForeignInvalid,NamedForeignInvalid,ExplicitForeignInvalid,LeftInvalid,RightInvalid}{seen:=0;func(){defer func(){p:=recover();if !strings.Contains(fmt.Sprint(p),"nil Event")||seen!=1{t.Fatalf("foreign element dispatch panic=%v outer arm=%d",p,seen)}}();f(&seen)}()}}
 `)
	packagesCommand(t, prodOut, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, prodOut, tool, true, "vet", "./...")
	packagesCommand(t, out, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, out, tool, true, "vet", "./...")
	for _, tc := range []struct{ name, call, want string }{{"T", "p::HiddenT()", "T must be nameable"}, {"E", "p::HiddenE()", "E must be nameable"}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			packagesWrite(t, dir, "go.mod", mod)
			writeMacroFixtureRoots(t, dir, map[string]rootconfig.Mapping{"producer": {Module: "inferred.example/producer", Package: "inferred.example/producer"}})
			packagesWrite(t, dir, "bad.gox", "package consumer\nuse producer as p;\nfunc Bad(){_=go_result!("+tc.call+")}\n")
			message := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "negative"), dir)
			if !strings.Contains(message, tc.want) {
				t.Fatal(message)
			}
		})
	}
}
