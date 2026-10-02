package acceptance_test

import (
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestPropagationControlFlowRace(t *testing.T) {
	enumGeneratedModule(t, propagationSource, propagationAssertions, false)
}

const propagationSource = `package fixture
import "errors"
func load(ok bool,trace *[]string) Result[int,error] {*trace=append(*trace,"load");if !ok{return Result[int,error]::Err(errors.New("failed"))};return Result[int,error]::Ok(4)}
func mark(s string,trace *[]string) int {*trace=append(*trace,s);return 1}
func combine(a,b,c int)int{return a+b+c}
func sequence(ok bool,trace *[]string) Result[string,error] {
 defer func(){*trace=append(*trace,"cleanup")}()
 n:=combine(mark("first",trace),load(ok,trace)?,mark("last",trace))
 defer func(){*trace=append(*trace,"late")}()
 if n==6{return Result[string,error]::Ok("six")};return Result[string,error]::Ok("other")
}
func callee(trace *[]string)func(int,int,int)int{*trace=append(*trace,"callee");return func(a,b,c int)int{*trace=append(*trace,"body");return a+b+c}}
func calleeSequence(ok bool,trace *[]string)Result[int,error]{n:=callee(trace)(mark("first",trace),load(ok,trace)?,mark("last",trace));return Result[int,error]::Ok(n)}
func conditional(choose bool,trace *[]string)Result[string,error]{if choose{_=load(false,trace)?}else{*trace=append(*trace,"else")};return Result[string,error]::Ok("continued")}
func optional(ok bool) Option[string] {if !ok{return Option[string]::None};return Option[string]::Some("abc")}
func optionalLength(ok bool) Option[int] {s:=optional(ok)?;return Option[int]::Some(len(s))}
func optionInt(v Option[int])int{match v {Option[int]::Some(n)=>{return n},Option[int]::None=>{return -1},};panic("unreachable")}
func resultText(v Result[string,error])string{match v {Result[string,error]::Ok(s)=>{return s},Result[string,error]::Err(e)=>{return e.Error()},};panic("unreachable")}
func truth(ok bool,trace *[]string)Result[bool,error]{n:=load(ok,trace)?;return Result[bool,error]::Ok(n>0)}
func shortCircuit(left bool,trace *[]string)Result[bool,error]{v:=left&&truth(false,trace)?;return Result[bool,error]::Ok(v)}
func boolResult(v Result[bool,error])int{match v {Result[bool,error]::Ok(x)=>{if x{return 1};return 0},Result[bool,error]::Err(_)=>{return -1},};panic("unreachable")}
func closure(trace *[]string)Result[string,error]{
 inner:=func()Result[int,error]{defer func(){*trace=append(*trace,"inner-cleanup")}();return Result[int,error]::Ok(load(false,trace)?)}
 _=inner();*trace=append(*trace,"outer-continued");return Result[string,error]::Ok("outer")
}
func deferredArgument(ok bool,trace *[]string)Result[int,error]{defer func(){*trace=append(*trace,"cleanup")}();defer func(n int){*trace=append(*trace,"deferred-body")}(load(ok,trace)?);return Result[int,error]::Ok(1)}
func invalidResult()Result[int,error]{return nil}
func invalidPropagation()Result[string,error]{_=invalidResult()?;return Result[string,error]::Ok("incorrect")}
type NilError struct{}
func (*NilError)Error()string{panic("typed nil error must not be formatted")}
func nativePartial()(int,error){var e *NilError;return 3,e}
type ReadOutcome struct{N int;Err error}
func explicitPartial()Result[ReadOutcome,error]{n,e:=nativePartial();return Result[ReadOutcome,error]::Ok(ReadOutcome{N:n,Err:e})}
func preservedPartial()Result[ReadOutcome,error]{value:=explicitPartial()?;return Result[ReadOutcome,error]::Ok(value)}
func explicitFailure()Result[int,error]{n,e:=nativePartial();if e!=nil{return Result[int,error]::Err(e)};return Result[int,error]::Ok(n)}
func preservedFailure()Result[string,error]{_=explicitFailure()?;return Result[string,error]::Ok("bad")}
func readOutcome(v Result[ReadOutcome,error])ReadOutcome{match v {Result[ReadOutcome,error]::Ok(x)=>{return x},Result[ReadOutcome,error]::Err(_)=>{panic("bad")},};panic("unreachable")}
func failure(v Result[string,error])error{match v {Result[string,error]::Ok(_)=>{return nil},Result[string,error]::Err(e)=>{return e},};panic("unreachable")}
`

const propagationAssertions = `package fixture
import("testing";"reflect")
func TestDifferentSuccessTypesAndOrder(t *testing.T){for _,ok:=range []bool{false,true}{trace:=[]string{};v:=sequence(ok,&trace);want:=[]string{"first","load","cleanup"};text:="failed";if ok{want=[]string{"first","load","last","late","cleanup"};text="six"};if resultText(v)!=text||!reflect.DeepEqual(trace,want){t.Fatalf("ok=%t text=%s trace=%v",ok,resultText(v),trace)}};if optionInt(optionalLength(true))!=3||optionInt(optionalLength(false))!=-1{t.Fatal("Option propagation")}}
func TestCalleeBeforeArguments(t *testing.T){for _,ok:=range []bool{false,true}{trace:=[]string{};_=calleeSequence(ok,&trace);want:=[]string{"callee","first","load"};if ok{want=[]string{"callee","first","load","last","body"}};if !reflect.DeepEqual(trace,want){t.Fatalf("callee order ok=%t: %v",ok,trace)}}}
func TestUnselectedBranchStaysUnevaluated(t *testing.T){trace:=[]string{};if resultText(conditional(false,&trace))!="continued"||!reflect.DeepEqual(trace,[]string{"else"}){t.Fatalf("unselected branch ran: %v",trace)};trace=nil;if resultText(conditional(true,&trace))!="failed"||!reflect.DeepEqual(trace,[]string{"load"}){t.Fatalf("selected failure did not return: %v",trace)}}
func TestShortCircuit(t *testing.T){trace:=[]string{};if boolResult(shortCircuit(false,&trace))!=0{t.Fatal("short circuit result")};if len(trace)!=0{t.Fatalf("evaluated skipped branch: %v",trace)};if boolResult(shortCircuit(true,&trace))!=-1{t.Fatal("short circuit failed propagation")};if !reflect.DeepEqual(trace,[]string{"load"}){t.Fatalf("wrong branch order: %v",trace)}}
func TestNearestClosureAndDefer(t *testing.T){trace:=[]string{};if resultText(closure(&trace))!="outer"||!reflect.DeepEqual(trace,[]string{"load","inner-cleanup","outer-continued"}){t.Fatalf("closure trace %v",trace)};for _,ok:=range []bool{false,true}{trace=nil;_=deferredArgument(ok,&trace);want:=[]string{"load","cleanup"};if ok{want=[]string{"load","deferred-body","cleanup"}};if !reflect.DeepEqual(trace,want){t.Fatalf("defer ok=%t trace %v",ok,trace)}}}
func TestNativePartialAndTypedNil(t *testing.T){out:=readOutcome(preservedPartial());if out.N!=3||out.Err==nil{t.Fatal("lost partial result")};if p,ok:=out.Err.(*NilError);!ok||p!=nil{t.Fatal("lost typed nil identity")};err:=failure(preservedFailure());if p,ok:=err.(*NilError);!ok||p!=nil{t.Fatal("typed nil was normalized or reclassified")}}
func TestInvalidResultIsNotBusinessFailure(t *testing.T){defer func(){if recover()==nil{t.Fatal("nil Result silently accepted")}}();_=invalidPropagation()}
`

func TestPropagationExplicitRejections(t *testing.T) {
	cases := map[string]string{
		"nested-enum-slice":                `func f()Result[[]Option[int],error]{return Result[[]Option[int],error]::Ok(nil)}`,
		"hgo-holder-enum":                  `enum E {A,};struct Holder { Bad *E };func f(v Holder)Option[Holder]{return Option[Holder]::Some(v)}`,
		"go-holder-enum":                   `enum E {A,};type Holder struct { Bad *E };func f(v Holder)Option[Holder]{return Option[Holder]::Some(v)}`,
		"option-to-result":                 `func f()Result[int,error]{x:=Option[int]::None?;return Result[int,error]::Ok(x)}`,
		"result-to-option":                 `func f()Option[int]{x:=Result[int,error]::Ok(1)?;return Option[int]::Some(x)}`,
		"ordinary-closure":                 `func f()Result[int,error]{x:=func()int{return Result[int,error]::Ok(1)?}();return Result[int,error]::Ok(x)}`,
		"native-multireturn":               `func native()(int,error){return 1,nil};func f()Result[int,error]{x:=native()?;return Result[int,error]::Ok(x)}`,
		"effectful-assignment-destination": `func index()int{return 0};func f(a []int)Result[int,error]{a[index()]=Result[int,error]::Ok(1)?;return Result[int,error]::Ok(1)}`,
		"loop-context":                     `func f()Result[int,error]{for Result[bool,error]::Ok(false)?{};return Result[int,error]::Ok(1)}`,
		"no-dynamic-error-guess":           `type Domain struct{};func (Domain)Error()string{return "domain"};func f()Result[int,Domain]{x:=Result[int,error]::Err(Domain{})?;return Result[int,Domain]::Ok(x)}`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("propagation_bad.hgo", []byte("package fixture\n"+source))
			if err == nil || !strings.Contains(err.Error(), "propagation_bad.hgo") {
				t.Fatalf("need located rejection: %v", err)
			}
		})
	}
}

func TestPropagationFromRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
import "errors"
#[derive(Error)]
#[error("wrapped")]
type AppError struct {
 #[from]
 Cause error
}
var sentinel=errors.New("sentinel")
func source()Result[int,error]{return Result[int,error]::Err(sentinel)}
func converted()Result[string,AppError]{_=source()?;return Result[string,AppError]::Ok("wrong")}
func assignable()Result[string,error]{_=converted()?;return Result[string,error]::Ok("wrong")}
func cause(v Result[string,AppError])error{match v {Result[string,AppError]::Err(e)=>{return e.Cause},Result[string,AppError]::Ok(_)=>{return nil},};panic("unreachable")}
func boxed(v Result[string,error])error{match v {Result[string,error]::Err(e)=>{return e},Result[string,error]::Ok(_)=>{return nil},};panic("unreachable")}
`, `package fixture
import("errors";"testing")
func TestOneStepFromAndAssignableFirst(t *testing.T){if cause(converted())!=sentinel{t.Fatal("From lost source")};err:=boxed(assignable());if !errors.Is(err,sentinel){t.Fatalf("assignment lost chain: %v",err)};if _,ok:=err.(AppError);!ok{t.Fatalf("assignment changed concrete error: %T",err)}}
`, false)
}

func TestPropagationMacroClosureRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
import "errors"
func source()Result[int,error]{return Result[int,error]::Err(errors.New("failed"))}
macro_rules! inside { ($v:expr) => {func()Result[int,error]{return Result[int,error]::Ok($v?)}()} }
func macroClosure()Result[string,error]{_=inside!(source());return Result[string,error]::Ok("continued")}
func text(v Result[string,error])string{match v {Result[string,error]::Ok(s)=>{return s},Result[string,error]::Err(_)=>{return "failed"},};panic("unreachable")}
`, `package fixture
import "testing"
func TestExplicitMacroClosureOwnsReturn(t *testing.T){if text(macroClosure())!="continued"{t.Fatal("macro closure returned outer function")}}
`, false)
}
