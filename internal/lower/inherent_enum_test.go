package lower_test

import (
	"strings"
	"testing"
)

func TestEnumInherentValueMethodsGoBoundary(t *testing.T) {
	sources := map[string]string{
		"carrierfixture/library/types.gox": `package library
pub enum Failure { Missing, Cause(error) }
pub enum Choice[T any] { Empty, Value(T) }
`,
		"carrierfixture/library/methods.gox": `package library
use errors;
impl Failure {
    pub func Error(self Self) string { return "failure" }
    pub func Unwrap(self Self) error { return match self { Failure::Cause(value)=>value, Failure::Missing=>nil } }
    pub func copy(self Self) Self { return self }
    pub func replaced(self Self) Self { self=Failure::Missing; return self }
    pub func catch(self Self, caught *bool) { *caught=recover()!=nil }
}

impl[U any] Choice[U] {
    pub func get(self Self, fallback U) U { return match self { Choice[U]::Value(value)=>value, Choice[U]::Empty=>fallback } }
    pub func copy(self Self) Self { return self }
    pub func catch(self Self, caught *bool) { *caught=recover()!=nil }
}
pub func check(value Failure, cause error) bool { return errors::Is(value,cause) }
pub func saved(value Failure) func() Failure { return value.copy }
pub func expression(value Failure) Failure { copy:=Failure::copy; return copy(value) }
pub func generic(value Choice[int]) int { get:=value.get; return get(4) }
`,
	}
	generated := carrierSources(t, sources)
	for _, source := range generated {
		if strings.Contains(string(source), "func FailureError_") || strings.Contains(string(source), "func ChoiceGet_") {
			t.Fatalf("value enum business body remains a package function:\n%s", source)
		}
	}
	carrierGoConsumer(t, generated, `package main
import("fmt"; "errors"; "carrierfixture/library")
func main(){
    cause:=errors.New("cause")
    var value library.Failure=&library.Cause{Value:cause}
    var native error=value
    saved:=library.Saved(value); value=&library.Missing{}
    caught,genericCaught,savedCaught:=false,false,false
    func(){defer value.Catch(&caught);panic("x")}()
    var choice library.Choice[int]=&library.Value[int]{Value:9}
    func(){defer choice.Catch(&genericCaught);panic("x")}()
    catch:=choice.Catch;func(){defer catch(&savedCaught);panic("x")}()
    fmt.Println(native.Error(),errors.Is(native,cause),library.Check(saved(),cause),library.Expression(saved()).Unwrap()==cause,saved().Replaced().Unwrap()==nil,library.Generic(choice),choice.Copy().Get(2),caught,genericCaught,savedCaught)
}
`, "failure true true true true 9 9 true true true\n")
}

func TestEnumInherentReadMethodsBindObject(t *testing.T) {
	sources := map[string]string{
		"carrierfixture/library/types.gox": `package library
pub enum Choice[T any] { Empty, Value(int) }
`,
		"carrierfixture/library/methods.gox": `package library
impl[U any] Choice[U] {
    pub func get(self &Self, fallback int) int { return match *self { Choice[U]::Value(value)=>value, Choice[U]::Empty=>fallback } }
    pub func catch(self &Self, caught *bool) { *caught=recover()!=nil }
}
pub func saved(value &Choice[int]) func(int) int { return value.get }
pub func expression() func(&Choice[int],int)int { return Choice[int]::get }
pub func catching() func(&Choice[int],*bool) { return Choice[int]::catch }
pub func direct(value &Choice[int])int { return value.get(2) }
pub func directExpression(value &Choice[int])int { return Choice[int]::get(value,3) }
pub func flow(value &Choice[int], choose bool) int { return Choice[int]::get(value,match *value {Choice[int]::Value(n)=>n,Choice[int]::Empty=>2}) }
`,
	}
	generated := carrierSources(t, sources)
	carrierGoConsumer(t, generated, `package main
import("fmt"; "carrierfixture/library")
func rejected(f func())(yes bool){defer func(){yes=recover()!=nil}();f();return}
func main(){
    var value library.Choice[int]=&library.Value[int]{Value:9}
    saved:=library.Saved(&value); value=&library.Empty[int]{}
    get:=library.Expression(); catch:=library.Catching()
    caught,nativeCaught,savedCaught:=false,false,false
    func(){defer catch(&value,&caught);panic("x")}()
    func(){defer value.Catch(&nativeCaught);panic("x")}()
    bound:=value.Catch;func(){defer bound(&savedCaught);panic("x")}()
    fmt.Println(saved(1),get(&value,7),library.Direct(&value),library.DirectExpression(&value),library.Flow(&value,true),caught,nativeCaught,savedCaught)
    var invalid library.Choice[int]
    var nilVariant *library.Value[int]
    var typedNil library.Choice[int]=nilVariant
    fmt.Println(rejected(func(){library.Direct(&invalid)}),rejected(func(){get(&invalid,0)}),rejected(func(){library.Direct(&typedNil)}),rejected(func(){get(&typedNil,0)}))
}
`, "9 7 2 3 2 true true true\ntrue true true true\n")
}

func TestConditionalEnumInherentDemandAndSnapshot(t *testing.T) {
	sources := map[string]string{"carrierfixture/library/main.gox": `package library
trait Mark { func mark(self Self) int }
trait Unused { func unused(self Self) int }
impl Mark for int {func mark(self Self)int{return self}}
pub enum Choice[T any] { Empty, Value(int) }
impl[T any] Choice[T] where T: Mark {
 pub func get(self &Self)int{return match *self{Choice[T]::Value(n)=>n,Choice[T]::Empty=>0}}
 pub func catch(self &Self,caught *bool){*caught=recover()!=nil}
}
impl[T any] Choice[T] where T: Unused {
 pub func never(self Self)string{return "unrequested-conditional-body"}
}
pub func saved(value &Choice[int])func()int{return value.get}
pub func read(value &Choice[int])int{return value.get()}
pub func expression()func(&Choice[int])int{return Choice[int]::get}
pub func catching(value &Choice[int],caught *bool){defer value.catch(caught);panic("direct")}
pub func savedCatching(value &Choice[int],caught *bool){f:=value.catch;defer f(caught);panic("saved")}
`}
	generated := carrierSources(t, sources)
	for _, source := range generated {
		if strings.Contains(string(source), "unrequested-conditional-body") {
			t.Fatalf("unused conditional body emitted:\n%s", source)
		}
	}
	carrierGoConsumer(t, generated, `package main
import("fmt";"carrierfixture/library")
func rejected(f func())(yes bool){defer func(){yes=recover()!=nil}();f();return}
func main(){
 var value library.Choice[int]=&library.Value[int]{Value:8}
 saved:=library.Saved(&value); value=&library.Empty[int]{}
 caught,savedCaught:=false,false
 library.Catching(&value,&caught);library.SavedCatching(&value,&savedCaught)
 read:=library.Expression()
 var invalid library.Choice[int]
 var typedNil library.Choice[int]=(*library.Value[int])(nil)
 fmt.Println(saved(),library.Read(&value),read(&value),caught,savedCaught,rejected(func(){library.Read(&invalid)}),rejected(func(){library.Read(&typedNil)}))
}
`, "8 0 0 true true true true\n")
}
