package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationValueContextsRace(t *testing.T) {
	enumGeneratedModule(t, propagationSource+`
func contextualShift(n uint)Option[uint64]{
 var a,b uint64=1<<n,Option[uint64]::Some(2)?
 a,b=1<<n,Option[uint64]::Some(3)?
 type Word uint64
 var c Word
 { Word:=4; _=Word; c,b=1<<n,Option[uint64]::Some(5)? }
 type Alias=uint64
 var d,e Alias=1<<n,Option[uint64]::Some(6)?
 _=d;_=e
 if a!=uint64(1)<<n||uint64(c)!=a||d!=a||e!=6||b!=5{panic("shift context")};return Option[uint64]::Some(a)
}
func closureShift(n uint)Option[uint64]{
 type Word uint64
 var a Word
 h:=func()Option[uint64]{
  Word:=0;_=Word
  var b uint64
  a,b=1<<n,Option[uint64]::Some(2)?
  return Option[uint64]::Some(uint64(a)+b)
 }
 return h()
}
func caseClosureShift(n uint)Option[uint64]{switch{default:
 type Word uint64;var a Word
 h:=func()Option[uint64]{var b uint64;a,b=1<<n,Option[uint64]::Some(2)?;return Option[uint64]::Some(uint64(a)+b)}
 return h()
}}
func selectClosureShift(n uint)Option[uint64]{select{default:
 type Word uint64;var a Word
 h:=func()Option[uint64]{var b uint64;a,b=1<<n,Option[uint64]::Some(2)?;return Option[uint64]::Some(uint64(a)+b)}
 return h()
}}
func callShift(n uint)Option[uint64]{return Option[uint64]::Some(addWide(1<<n,Option[uint64]::Some(2)?))}
func addWide(a,b uint64)uint64{return a+b}
func shiftValue(v Option[uint64])uint64{match v{Option[uint64]::Some(n)=>{return n},Option[uint64]::None=>{return 0},};panic("unreachable")}
func multiBind(ok bool,trace *[]string)Result[string,error]{
 defer func(){*trace=append(*trace,"cleanup")}()
 a,b,c:=mark("first",trace),load(ok,trace)?,mark("last",trace)
 if a!=1||b!=4||c!=1{panic("values")}
 return Result[string,error]::Ok("done")
}
func assignAll(ok bool,a,b *int,trace *[]string)Result[string,error]{
 x,y:=*a,*b
 defer func(){*a=x;*b=y}()
 x,y=y,load(ok,trace)?
 return Result[string,error]::Ok("done")
}
func declared(ok bool,trace *[]string)Option[int]{
 var a,b byte=255,Option[byte]::Some(7)?
 var p,q *int=nil,Option[*int]::Some(&beyond)?
 if a!=255||b!=7||p!=nil||*q!=23{panic("contextual type")}
 if x,y:=Option[int]::Some(3)?,2;x>y{return Option[int]::Some(x+y)}
 return Option[int]::None
}
var beyond=23
func manyQuestions(first,second bool,trace *[]string)Result[string,error]{
 a,b:=load(first,trace)?,load(second,trace)?
 if a+b!=8{panic("payload")};return Result[string,error]::Ok("done")
}
func nestedValues(trace *[]string)Result[string,error]{
 f:=func()Option[int]{defer func(){*trace=append(*trace,"inner-cleanup")}();a,b:=mark("inner",trace),Option[int]::None?;return Option[int]::Some(a+b)}
 a,b:=optionInt(f()),load(true,trace)?
 if a!=-1||b!=4{panic("closure target")};return Result[string,error]::Ok("done")
}
func pointer(ok bool,trace *[]string)Result[*int,error]{*trace=append(*trace,"pointer");if !ok{return Result[*int,error]::Err(errors::New("pointer failed"))};return Result[*int,error]::Ok(&beyond)}
func dereferenced(ok bool,trace *[]string)Result[int,error]{return Result[int,error]::Ok(*pointer(ok,trace)?)}
func boxed(ok bool,trace *[]string)Result[any,error]{*trace=append(*trace,"boxed");if !ok{return Result[any,error]::Err(errors::New("box failed"))};return Result[any,error]::Ok(23)}
func asserted(ok bool,trace *[]string)Result[int,error]{return Result[int,error]::Ok(boxed(ok,trace)?.(int))}
func checkedAssertion(trace *[]string)Result[int,error]{v,ok:=boxed(true,trace)?.(string);if ok||v!=""{panic("comma ok")};return Result[int,error]::Ok(5)}
func panickingAssertion(trace *[]string)Result[int,error]{v:=boxed(true,trace)?.(string);return Result[int,error]::Ok(len(v))}
func nilPointer()Option[int]{v:=*Option[*int]::Some(nil)?;return Option[int]::Some(v)}
func intResult(v Result[int,error])int{match v{Result[int,error]::Ok(n)=>{return n},Result[int,error]::Err(_)=>{return -1},};panic("unreachable")}
func sendChannel(ch chan int,trace *[]string)chan int{*trace=append(*trace,"channel");return ch}
func sendValue(ok bool,ch chan int,trace *[]string)Result[string,error]{defer func(){*trace=append(*trace,"cleanup")}();sendChannel(ch,trace)<-load(ok,trace)?;return Result[string,error]::Ok("sent")}
func channelResult(ok bool,ch chan int,trace *[]string)Result[chan int,error]{*trace=append(*trace,"channel-result");if !ok{return Result[chan int,error]::Err(errors::New("channel failed"))};return Result[chan int,error]::Ok(ch)}
func rebindDuringValue(ch *chan int,next chan int)Option[int]{*ch=next;return Option[int]::Some(8)}
func sendCaptured(ch *chan int,next chan int)Option[int]{*ch<-rebindDuringValue(ch,next)?;return Option[int]::Some(1)}
func sendBoth(channelOK,valueOK bool,ch chan int,trace *[]string)Result[string,error]{channelResult(channelOK,ch,trace)?<-load(valueOK,trace)?;return Result[string,error]::Ok("sent")}
func shortDereference(trace *[]string)Result[bool,error]{return Result[bool,error]::Ok(false&&*pointer(false,trace)?>0)}
`, propagationValuesAssertions, false)
}

const propagationValuesAssertions = `package fixture
import("testing";"reflect")
func TestMultiRHSOrderAndNoPartialWrites(t *testing.T){
 for _,ok:=range []bool{false,true}{
 trace:=[]string{};want:=[]string{"first","load","cleanup"};text:="failed";if ok{want=[]string{"first","load","last","cleanup"};text="done"}
 if got:=resultText(multiBind(ok,&trace));got!=text||!reflect.DeepEqual(trace,want){t.Fatalf("got %s %v",got,trace)}
 a,b:=10,20;_=assignAll(ok,&a,&b,&trace);if (!ok&&(a!=10||b!=20))||(ok&&(a!=20||b!=4)){t.Fatalf("partial/simultaneous assignment %d %d",a,b)}
 }
}
func TestContextualTypesAndMultipleQuestions(t *testing.T){
 trace:=[]string{};if shiftValue(caseClosureShift(63))!=uint64(1)<<63+2||shiftValue(selectClosureShift(63))!=uint64(1)<<63+2||shiftValue(closureShift(63))!=uint64(1)<<63+2||shiftValue(callShift(63))!=uint64(1)<<63+2||shiftValue(contextualShift(63))!=uint64(1)<<63{t.Fatal("shift lost context")};if optionInt(declared(true,&trace))!=5{t.Fatal("contextual types")}
 for _,tc:=range []struct{a,b bool;calls int;text string}{{false,true,1,"failed"},{true,false,2,"failed"},{true,true,2,"done"}}{trace=nil;got:=resultText(manyQuestions(tc.a,tc.b,&trace));if got!=tc.text||len(trace)!=tc.calls{t.Fatalf("got %s %v",got,trace)}}
}
func TestNestedClosureAndShortCircuit(t *testing.T){trace:=[]string{};if resultText(nestedValues(&trace))!="done"||!reflect.DeepEqual(trace,[]string{"inner","inner-cleanup","load"}){t.Fatal(trace)};trace=nil;if boolResult(shortDereference(&trace))!=0||len(trace)!=0{t.Fatal(trace)}}
func TestDereferenceAndAssertions(t *testing.T){
 trace:=[]string{};if intResult(dereferenced(true,&trace))!=23||intResult(dereferenced(false,&trace))!=-1||intResult(asserted(true,&trace))!=23||intResult(asserted(false,&trace))!=-1||intResult(checkedAssertion(&trace))!=5{t.Fatal(trace)}
 for _,f:=range []func(){func(){_=panickingAssertion(&trace)},func(){_=nilPointer()}}{func(){defer func(){if recover()==nil{t.Error("Go panic lost")}}();f()}()}
}
func TestSendOrderFailureAndNil(t *testing.T){
 for _,ok:=range []bool{false,true}{trace:=[]string{};ch:=make(chan int,1);got:=resultText(sendValue(ok,ch,&trace));if !reflect.DeepEqual(trace,[]string{"channel","load","cleanup"}){t.Fatal(trace)};if ok{if got!="sent"||len(ch)!=1||<-ch!=4{t.Fatal(got)}}else if got!="failed"||len(ch)!=0{t.Fatal(got)}}
 trace:=[]string{};if resultText(sendValue(false,nil,&trace))!="failed"{t.Fatal("nil channel failed propagation")}
 for _,tc:=range []struct{ch,value bool;want []string}{{false,true,[]string{"channel-result"}},{true,false,[]string{"channel-result","load"}},{true,true,[]string{"channel-result","load"}}}{trace=nil;ch:=make(chan int,1);_=sendBoth(tc.ch,tc.value,ch,&trace);if !reflect.DeepEqual(trace,tc.want)||len(ch)!=btoi(tc.ch&&tc.value){t.Fatal(trace)}}
}
func TestCapturedChannel(t *testing.T){first,next:=make(chan int,1),make(chan int,1);current:=first;if optionInt(sendCaptured(&current,next))!=1||current!=next||len(next)!=0||len(first)!=1||<-first!=8{t.Fatal("channel not captured before value")}}
func TestClosedChannelAfterPropagation(t *testing.T){ch:=make(chan int);close(ch);trace:=[]string{};if resultText(sendValue(false,ch,&trace))!="failed"{t.Fatal("closed channel failed path")};defer func(){if recover()==nil{t.Error("closed channel panic lost")}}();_=sendValue(true,ch,&trace)}
func btoi(b bool)int{if b{return 1};return 0}
`

func TestPropagationValueContextRejections(t *testing.T) {
	for name, source := range map[string]string{
		"tuple-count":     `func f()Option[int]{a,b,c:=1,Option[int]::Some(2)?;_,_,_=a,b,c;return Option[int]::None}`,
		"typed-overflow":  `func f()Option[int]{var a,b byte=256,Option[byte]::Some(1)?;_,_=a,b;return Option[int]::None}`,
		"cross-family":    `func f(ch chan int)Option[int]{ch<-Result[int,error]::Ok(1)?;return Option[int]::None}`,
		"receive-only":    `func f(ch <-chan int)Option[int]{ch<-Option[int]::Some(1)?;return Option[int]::None}`,
		"bad-dereference": `func f()Option[int]{v:=*Option[int]::Some(1)?;return Option[int]::Some(v)}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileAcceptanceLanguageFixture("value_bad.gox", []byte("package fixture\n"+source))
			if err == nil || !strings.Contains(err.Error(), "value_bad.gox") {
				t.Fatalf("expected located rejection: %v", err)
			}
		})
	}
}

func TestPropagationValuesInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	cli := filepath.Join(bin, "goxide")
	outside := t.TempDir()
	source := filepath.Join(root, "examples/propagate_values")
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "business_test.go", `package main
import("errors";"strconv";"testing")
func TestJobDelivery(t *testing.T){jobs:=make(chan string,1);if err:=resultError(deliver("8080",jobs));err!=nil||len(jobs)!=1||<-jobs!="port 8080 priority 7"{t.Fatal(err)};err:=resultError(deliver("invalid",jobs));var native *strconv.NumError;if !errors.As(err,&native)||len(jobs)!=0{t.Fatalf("lost error/no-send: %v",err)}}
func TestCrossFileContextualTypes(t *testing.T){if valueOf(wide(63))!=uint64(1)<<63{t.Fatal("wide shift")}}
`)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, source); got != "port 8080 priority 7\ninvalid port\n" {
		t.Fatalf("business: %q", got)
	}
}
