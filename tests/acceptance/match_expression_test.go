package acceptance_test

import (
	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/frontend"
	"strings"
	"testing"
)

func TestMatchExpressionBusinessRace(t *testing.T) {
	source := `package fixture
macro_rules! twice {($x:expr)=>{$x+$x};}
#[derive(Serialize, Deserialize)]
enum Response { Empty, Number(int), Text(string), }
func number(n int) Response {return Response::Number(n)}
func business(next func()Response,trace *[]int) int {
 mark:=func(n int,b bool)bool{*trace=append(*trace,n);return b}
 base:=10
 {
  var base int = match next() {
   Response::Number(n) if mark(1,false) => n+100,
   _ if mark(2,false) => 200,
   Response::Number(n) => twice!(n)+base,
   _ => base,
  }
  return base
 }
}
func returning(x Response,finished *bool) int {
 defer func(){*finished=true}()
 return match x {Response::Number(n)=>n+1,_=>0,}
}
func shadowBuiltins() int {
 false:=true;true:=false;any:=func(interface{})interface{}{return nil}
 _,_,_=false,true,any
 var value int = match Response::Number(7) {Response::Number(n)=>n,_=>0,}
 return value
}
func outerClosure() int {
 f:=func(x Response) int {return match x {Response::Number(n)=>n+2,_=>0,}}
 return f(Response::Number(8))+3
}
`
	enumGeneratedModule(t, source, `package fixture
import("testing";"reflect";"github.com/hxsf/goxide/serde";"github.com/hxsf/goxide/serdejson")
func TestScopeEffectsAndGuards(t *testing.T){calls:=0;trace:=[]int{};got:=business(func()Response{calls++;return number(4)},&trace);if got!=18||calls!=1||!reflect.DeepEqual(trace,[]int{1,2}){t.Fatalf("got %d calls %d trace %v",got,calls,trace)}}
func TestReturnClosureAndHygiene(t *testing.T){done:=false;if returning(number(2),&done)!=3||!done{t.Fatal("defer or return changed")};if outerClosure()!=13||shadowBuiltins()!=7{t.Fatal("closure target or generated hygiene changed")}}
func TestSerdeMacroMatchIntegration(t *testing.T){v:=number(11);data,e:=serdejson.Encode(func(s serde.Serializer)error{return SerializeResponse(s,&v)});if e!=nil{t.Fatal(e)};decoded,e:=serdejson.Unmarshal(data,DeserializeResponse);if e!=nil{t.Fatal(e)};done:=false;if returning(decoded,&done)!=12||!done{t.Fatal("integration changed value")}}
`, false)
}

func TestMatchExpressionRawCRLFRace(t *testing.T) {
	source := "package fixture\r\nenum E { A, B, }\r\nfunc raw() string {\r\nvar s string = match E::A {E::A=>`first\r\nsecond`, E::B=>`third\r\nfourth`,}\r\nreturn s\r\n}\r\n"
	enumGeneratedModule(t, source, `package fixture
import "testing"
func TestLiteral(t *testing.T){if raw()!="first\nsecond"{t.Fatalf("raw=%q",raw())}}
`, false)
}

func TestMatchExpressionUnsupportedContexts(t *testing.T) {
	cases := map[string]string{
		"mixed inferred var":  `func f(x E){var value = match x {E::A=>1,E::B=>"no",};_=value}`,
		"nil inference":       `func f(x E){value := match x {E::A=>nil,E::B=>nil,};_=value}`,
		"void return":         `func f(x E){return match x {E::A=>1,E::B=>2,}}`,
		"multiple return":     `func f(x E)(int,int){return match x {E::A=>1,E::B=>2,}}`,
		"named return":        `func f(x E)(value int){return match x {E::A=>1,E::B=>2,}}`,
		"package initializer": `var value int = match E::A {E::A=>1,E::B=>2,}`,
		"dynamic argument":    `func f(x E,g func(int)int)int{return g(match x {E::A=>1,E::B=>2,})}`,
		"short circuit":       `func f(x E)bool{return false && match x {E::A=>true,E::B=>false,}}`,
		"missing arm":         `func f(x E)int{return match x {E::A=>1,}}`,
		"unreachable arm":     `func f(x E)int{return match x {E::A=>1,E::A=>2,E::B=>3,}}`,
		"reserved name":       `func f(x E)int{__hgo_match_result_1:=9;return match x {E::A=>__hgo_match_result_1,E::B=>0,}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := frontend.Compile("expression_bad.hgo", []byte("package fixture\nenum E {A,B,}\n"+body))
			if err == nil || !strings.Contains(err.Error(), "expression_bad.hgo") {
				t.Fatalf("expected located rejection, got %v", err)
			}
			t.Log(err)
		})
	}
}

func TestBuiltinMatchExpressionRace(t *testing.T) {
	source := `package fixture
func optionValue(x Option[int]) int {return match x {Option[int]::None=>-1,Option[int]::Some(n)=>n,}}
func nestedValue(x Option[Option[int]]) int {
 var inner Option[int] = match x {Option[Option[int]]::None=>Option[int]::Some(-2),Option[Option[int]]::Some(v)=>v,}
 return optionValue(inner)
}
func optionPointer(x Option[*int]) int {return match x {Option[*int]::None=>0,Option[*int]::Some(v)=>pointerState(v),}}
func pointerState(v *int)int {if v==nil{return 1};return 2}
func resultValue(x Result[int,error],seen *bool) int {
 var value int = match x {Result[int,error]::Ok(n)=>n,Result[int,error]::Err(e)=>errorState(e),}
 *seen=true
 return value
}
func errorState(e error)int {if e==nil{return -1};return -2}
func cases() [6]int {
 return [6]int{optionValue(Option[int]::None),optionValue(Option[int]::Some(4)),nestedValue(Option[Option[int]]::None),nestedValue(Option[Option[int]]::Some(Option[int]::None)),nestedValue(Option[Option[int]]::Some(Option[int]::Some(7))),optionPointer(Option[*int]::Some(nil))}
}
func typedResult(e error,seen *bool)int{return resultValue(Result[int,error]::Err(e),seen)}
`
	opt, err := enums.Builtin("Option", []string{"int"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := enums.Builtin("Result", []string{"int", "error"})
	if err != nil {
		t.Fatal(err)
	}
	some, ok := "", ""
	for _, v := range opt.Variants {
		if v.Name == "Some" {
			some = v.CaseName
		}
	}
	for _, v := range res.Variants {
		if v.Name == "Ok" {
			ok = v.CaseName
		}
	}
	assertions := strings.NewReplacer("SOME_CASE", some, "OK_CASE", ok).Replace(`package fixture
import "testing"
type typedError struct{}
func(*typedError)Error()string{return "typed"}
type forgedResult struct{Result[int,error]}
func TestBuiltinDistinctions(t *testing.T){if got:=cases();got!=([6]int{-1,4,-2,-1,7,1}){t.Fatalf("states=%v",got)};var typed *typedError;seen:=false;if typedResult(typed,&seen)!=-2||!seen{t.Fatal("typednil error payload collapsed")}}
func TestInvalidBuiltinBeforeEffect(t *testing.T){for _,v:=range []Result[int,error]{nil,(*OK_CASE)(nil),&OK_CASE{Value:3},forgedResult{}}{seen:=false;panicked:=false;func(){defer func(){panicked=recover()!=nil}();resultValue(v,&seen)}();if !panicked||seen{t.Fatalf("invalid %T panic=%t sideEffect=%t",v,panicked,seen)}};panicked:=false;func(){defer func(){panicked=recover()!=nil}();optionValue((*SOME_CASE)(nil))}();if !panicked{t.Fatal("typednil Some treated as None")}}
`)
	enumGeneratedModule(t, source, assertions, false)
}
