package acceptance_test

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/frontend"
)

func TestPropagationAggregateContextsRace(t *testing.T) {
	enumGeneratedModule(t, aggregatePropagationSource, aggregatePropagationAssertions, false)
}

const aggregatePropagationSource = `package fixture
import "errors"
var failure=errors.New("failed")
func number(label string,n int,ok bool,trace *[]string)Result[int,error]{*trace=append(*trace,label);if !ok{return Result[int,error]::Err(failure)};return Result[int,error]::Ok(n)}
func mark(label string,n int,trace *[]string)int{*trace=append(*trace,label);return n}
func text(label,s string,ok bool,trace *[]string)Result[string,error]{*trace=append(*trace,label);if !ok{return Result[string,error]::Err(failure)};return Result[string,error]::Ok(s)}
func sliceBase(trace *[]string)[]int{*trace=append(*trace,"base");return []int{10,20,30,40}}
func mapBase(trace *[]string)map[string]int{*trace=append(*trace,"map");return map[string]int{"x":7}}
func indexed(ok bool,trace *[]string)Result[int,error]{defer func(){*trace=append(*trace,"cleanup")}();return Result[int,error]::Ok(sliceBase(trace)[number("index",1,ok,trace)?]+mark("after",1,trace))}
func mapIndex(key string,ok bool,trace *[]string)Result[int,error]{value,found:=mapBase(trace)[text("key",key,ok,trace)?];if !found{return Result[int,error]::Ok(-1)};return Result[int,error]::Ok(value)}
func nilMap(trace *[]string)Result[int,error]{var m map[int]int;value,ok:=m[number("key",0,true,trace)?];if ok||value!=0{panic("nil map")};return Result[int,error]::Ok(value)}
func stringIndex(trace *[]string)Result[int,error]{return Result[int,error]::Ok(int("abc"[number("index",1,true,trace)?]))}
func arrayBase(trace *[]string)[2]int{*trace=append(*trace,"array");return [2]int{5,6}}
func arrayValueIndex(trace *[]string)Result[int,error]{return Result[int,error]::Ok(arrayBase(trace)[number("index",1,true,trace)?])}
func pointerIndex(p *[2]int,ok bool,trace *[]string)Result[int,error]{return Result[int,error]::Ok(p[number("index",1,ok,trace)?])}
func sourceSlice(ok bool,trace *[]string)Result[[]int,error]{*trace=append(*trace,"source");if !ok{return Result[[]int,error]::Err(failure)};return Result[[]int,error]::Ok([]int{10,20,30,40})}
func bothIndex(baseOK,indexOK bool,trace *[]string)Result[int,error]{return Result[int,error]::Ok(sourceSlice(baseOK,trace)?[number("index",1,indexOK,trace)?])}
func bounds(lowOK,highOK,maxOK bool,trace *[]string)Result[[]int,error]{return Result[[]int,error]::Ok(sliceBase(trace)[number("low",1,lowOK,trace)?:number("high",3,highOK,trace)?:number("max",4,maxOK,trace)?])}
func changeArray(a *[4]int,trace *[]string)Result[int,error]{*trace=append(*trace,"low");a[1]=99;return Result[int,error]::Ok(1)}
func arraySlice(a *[4]int,trace *[]string)Result[[]int,error]{return Result[[]int,error]::Ok((*a)[changeArray(a,trace)?:3])}
func omittedBounds(trace *[]string)Result[[]int,error]{return Result[[]int,error]::Ok(sliceBase(trace)[number("low",1,true,trace)?:])}
func nilSlice(trace *[]string)Result[[]int,error]{var s []int;return Result[[]int,error]::Ok(s[:number("high",0,true,trace)?])}
func stringSlice(trace *[]string)Result[string,error]{return Result[string,error]::Ok("abcd"[number("low",1,true,trace)?:number("high",3,true,trace)?])}
func badBounds(ok bool,trace *[]string)Result[[]int,error]{return Result[[]int,error]::Ok(sliceBase(trace)[number("low",3,ok,trace)?:2])}
func badIndex(ok bool,trace *[]string)Result[int,error]{return Result[int,error]::Ok(sliceBase(trace)[number("index",9,ok,trace)?])}
type Record struct{First int;Wide uint64;Last int}
func structs(ok bool,n uint,trace *[]string)Result[Record,error]{r:=Record{First:mark("first",1,trace),Wide:1<<n,Last:number("last",3,ok,trace)?};return Result[Record,error]::Ok(r)}
func unkeyed(trace *[]string)Result[Record,error]{r:=Record{mark("first",1,trace),1<<63,number("last",3,true,trace)?};return Result[Record,error]::Ok(r)}
func arrays(ok bool,trace *[]string)Result[[]int,error]{a:=[...]int{2:mark("first",5,trace),number("second",6,ok,trace)?,0:mark("last",7,trace)};return Result[[]int,error]::Ok(a[:])}
func slices(ok bool,trace *[]string)Result[[]int,error]{return Result[[]int,error]::Ok([]int{mark("first",1,trace),number("middle",2,ok,trace)?,mark("last",3,trace)})}
func maps(keyOK,valueOK bool,trace *[]string)Result[map[int]int,error]{m:=map[int]int{mark("key1",1,trace):mark("value1",10,trace),number("key2",2,keyOK,trace)?:number("value2",20,valueOK,trace)?,mark("key3",3,trace):mark("value3",30,trace)};return Result[map[int]int,error]::Ok(m)}
func dynamicDuplicate(trace *[]string)Result[map[int]int,error]{key:=1;return Result[map[int]int,error]::Ok(map[int]int{key:mark("a",1,trace),key:number("b",2,true,trace)?})}
type Tiny struct{X int}
func nestedLiteral(trace *[]string)Result[int,error]{
 values:=[]*Tiny{{X:mark("first",1,trace)},{X:number("middle",2,true,trace)?},{X:mark("last",3,trace)}}
 keys:=map[*Tiny]int{{X:mark("key",4,trace)}:number("value",5,true,trace)?}
 p:=&(Tiny{X:number("ptr",6,true,trace)?})
 if values[0]==values[1]||values[1]==values[2]||len(keys)!=1{panic("pointer identity")}
 return Result[int,error]::Ok(values[0].X+values[1].X+values[2].X+p.X)
}
func badDynamicMap(ok bool,trace *[]string)Result[int,error]{m:=map[any]int{[]int{number("key",1,ok,trace)?}:mark("value",2,trace)};return Result[int,error]::Ok(len(m))}
func deferredLiteral(ok bool,trace *[]string)Result[int,error]{defer func(){*trace=append(*trace,"cleanup")}();defer func(values []int){*trace=append(*trace,"deferred") }([]int{mark("first",1,trace),number("middle",2,ok,trace)?,mark("last",3,trace)});return Result[int,error]::Ok(1)}
func nestedClosure(trace *[]string)Result[int,error]{
 inner:=func()Result[int,error]{defer func(){*trace=append(*trace,"inner-cleanup")}();s:=[]int{mark("first",1,trace),number("fail",0,false,trace)?,mark("never",0,trace)};return Result[int,error]::Ok(s[0])}
 _=inner();*trace=append(*trace,"outer");return Result[int,error]::Ok(8)
}
func shortCircuit(trace *[]string)Result[int,error]{if false&&([]int{number("never",0,false,trace)?}[0]>0){panic("bad")};return Result[int,error]::Ok(1)}
func lenLiteral(z int,trace *[]string)Result[int,error]{n:=len([2]int{number("value",1,true,trace)?,1/z});return Result[int,error]::Ok(n)}
func nilArray(ok bool,trace *[]string)Result[*[2]int,error]{*trace=append(*trace,"array-pointer");if !ok{return Result[*[2]int,error]::Err(failure)};return Result[*[2]int,error]::Ok(nil)}
func lenArrayPointer(ok bool,trace *[]string)Result[int,error]{n:=len(*nilArray(ok,trace)?);return Result[int,error]::Ok(n)}
func capArrayPointer(ok bool,trace *[]string)Result[int,error]{n:=cap(*nilArray(ok,trace)?);return Result[int,error]::Ok(n)}
func betweenReceives(ch <-chan int,trace *[]string)Result[int,error]{if len(ch)!=1{panic("receive order")};return number("value",2,true,trace)}
func receiveLiteral(ch <-chan int,trace *[]string)Result[[]int,error]{s:=[]int{<-ch,betweenReceives(ch,trace)?,<-ch};return Result[[]int,error]::Ok(s)}
func intValue(v Result[int,error])(int,error){match v{Result[int,error]::Ok(n)=>{return n,nil},Result[int,error]::Err(e)=>{return 0,e},};panic("unreachable")}
func sliceValue(v Result[[]int,error])([]int,error){match v{Result[[]int,error]::Ok(s)=>{return s,nil},Result[[]int,error]::Err(e)=>{return nil,e},};panic("unreachable")}
func mapValue(v Result[map[int]int,error])(map[int]int,error){match v{Result[map[int]int,error]::Ok(s)=>{return s,nil},Result[map[int]int,error]::Err(e)=>{return nil,e},};panic("unreachable")}
func recordValue(v Result[Record,error])(Record,error){match v{Result[Record,error]::Ok(s)=>{return s,nil},Result[Record,error]::Err(e)=>{return Record{},e},};panic("unreachable")}
func textValue(v Result[string,error])string{match v{Result[string,error]::Ok(s)=>{return s},Result[string,error]::Err(_)=>{return "failed"},};panic("unreachable")}
`

const aggregatePropagationAssertions = `package fixture
import("testing";"reflect";"errors")
func traceIs(t *testing.T,got,want []string){t.Helper();if !reflect.DeepEqual(got,want){t.Fatalf("trace %v want %v",got,want)}}
func mustPanic(t *testing.T,f func()){t.Helper();defer func(){if recover()==nil{t.Error("Go panic lost")}}();f()}
func TestIndexOrderAndFailures(t *testing.T){for _,ok:=range []bool{false,true}{trace:=[]string{};n,e:=intValue(indexed(ok,&trace));want:=[]string{"base","index","cleanup"};if ok{want=[]string{"base","index","after","cleanup"};if n!=21||e!=nil{t.Fatal(n,e)}}else if !errors.Is(e,failure){t.Fatal(e)};traceIs(t,trace,want)};trace:=[]string{};_,e:=intValue(bothIndex(false,true,&trace));if !errors.Is(e,failure){t.Fatal(e)};traceIs(t,trace,[]string{"source"});trace=nil;n,e:=intValue(bothIndex(true,true,&trace));if n!=20||e!=nil{t.Fatal(n,e)};traceIs(t,trace,[]string{"source","index"})}
func TestMapStringArrayAndPointer(t *testing.T){for _,key:=range []string{"x","absent"}{trace:=[]string{};n,e:=intValue(mapIndex(key,true,&trace));want:=7;if key=="absent"{want=-1};if n!=want||e!=nil{t.Fatal(n,e)};traceIs(t,trace,[]string{"map","key"})};trace:=[]string{};if n,e:=intValue(nilMap(&trace));n!=0||e!=nil{t.Fatal(n,e)};if n,e:=intValue(stringIndex(&trace));n!=98||e!=nil{t.Fatal(n,e)};if n,e:=intValue(arrayValueIndex(&trace));n!=6||e!=nil{t.Fatal(n,e)};a:=[2]int{4,5};if n,e:=intValue(pointerIndex(&a,true,&trace));n!=5||e!=nil{t.Fatal(n,e)};if _,e:=intValue(pointerIndex(nil,false,&trace));!errors.Is(e,failure){t.Fatal(e)};mustPanic(t,func(){_=pointerIndex(nil,true,&trace)})}
func TestSliceOrderBoundsAndStorage(t *testing.T){for _,tc:=range []struct{lo,hi,max bool;want []string}{{false,true,true,[]string{"base","low"}},{true,false,true,[]string{"base","low","high"}},{true,true,false,[]string{"base","low","high","max"}},{true,true,true,[]string{"base","low","high","max"}}}{trace:=[]string{};s,e:=sliceValue(bounds(tc.lo,tc.hi,tc.max,&trace));traceIs(t,trace,tc.want);if tc.lo&&tc.hi&&tc.max{if e!=nil||len(s)!=2||cap(s)!=3||s[0]!=20{t.Fatal(s,e)}}else if !errors.Is(e,failure){t.Fatal(e)}};trace:=[]string{};a:=[4]int{1,2,3,4};s,e:=sliceValue(arraySlice(&a,&trace));if e!=nil||s[0]!=99||len(s)!=2||cap(s)!=3{t.Fatal(s,e)};s[0]=77;if a[1]!=77{t.Fatal("array storage copied")};s,e=sliceValue(omittedBounds(&trace));if e!=nil||len(s)!=3||cap(s)!=3{t.Fatal(s,e)};s,e=sliceValue(nilSlice(&trace));if e!=nil||s!=nil{t.Fatal(s,e)};if textValue(stringSlice(&trace))!="bc"{t.Fatal("substring")}}
func TestBoundsPanicAndPropagation(t *testing.T){trace:=[]string{};if _,e:=sliceValue(badBounds(false,&trace));!errors.Is(e,failure){t.Fatal(e)};if _,e:=intValue(badIndex(false,&trace));!errors.Is(e,failure){t.Fatal(e)};mustPanic(t,func(){_=badBounds(true,&trace)});mustPanic(t,func(){_=badIndex(true,&trace)})}
func TestStructArraySliceLiterals(t *testing.T){trace:=[]string{};r,e:=recordValue(structs(true,63,&trace));if e!=nil||r.First!=1||r.Wide!=uint64(1)<<63||r.Last!=3{t.Fatal(r,e)};traceIs(t,trace,[]string{"first","last"});trace=nil;r,e=recordValue(unkeyed(&trace));if e!=nil||r.Wide!=uint64(1)<<63{t.Fatal(r,e)};trace=nil;s,e:=sliceValue(arrays(true,&trace));if e!=nil||!reflect.DeepEqual(s,[]int{7,0,5,6}){t.Fatal(s,e)};traceIs(t,trace,[]string{"first","second","last"});trace=nil;s,e=sliceValue(slices(false,&trace));if s!=nil||!errors.Is(e,failure){t.Fatal(s,e)};traceIs(t,trace,[]string{"first","middle"})}
func TestMapKeyValueOrder(t *testing.T){for _,tc:=range []struct{k,v bool;want []string}{{false,true,[]string{"key1","value1","key2"}},{true,false,[]string{"key1","value1","key2","value2"}},{true,true,[]string{"key1","value1","key2","value2","key3","value3"}}}{trace:=[]string{};m,e:=mapValue(maps(tc.k,tc.v,&trace));traceIs(t,trace,tc.want);if tc.k&&tc.v{if e!=nil||m[1]!=10||m[2]!=20||m[3]!=30{t.Fatal(m,e)}}else if !errors.Is(e,failure){t.Fatal(e)}};trace:=[]string{};m,e:=mapValue(dynamicDuplicate(&trace));if e!=nil||len(m)!=1||(m[1]!=1&&m[1]!=2){t.Fatal(m,e)};traceIs(t,trace,[]string{"a","b"})}
func TestNestedImplicitPointersAndClosures(t *testing.T){trace:=[]string{};n,e:=intValue(nestedLiteral(&trace));if n!=12||e!=nil{t.Fatal(n,e)};traceIs(t,trace,[]string{"first","middle","last","key","value","ptr"});trace=nil;n,e=intValue(nestedClosure(&trace));if n!=8||e!=nil{t.Fatal(n,e)};traceIs(t,trace,[]string{"first","fail","inner-cleanup","outer"});trace=nil;_,_=intValue(shortCircuit(&trace));if len(trace)!=0{t.Fatal(trace)}}
func TestLenCapStillEvaluate(t *testing.T){trace:=[]string{};if n,e:=intValue(lenLiteral(1,&trace));n!=2||e!=nil{t.Fatal(n,e)};mustPanic(t,func(){_=lenLiteral(0,&trace)});mustPanic(t,func(){_=lenArrayPointer(true,&trace)});mustPanic(t,func(){_=capArrayPointer(true,&trace)});if _,e:=intValue(lenArrayPointer(false,&trace));!errors.Is(e,failure){t.Fatal(e)}}
func TestDynamicMapAndDeferredLiteral(t *testing.T){trace:=[]string{};if _,e:=intValue(badDynamicMap(false,&trace));!errors.Is(e,failure){t.Fatal(e)};traceIs(t,trace,[]string{"key"});mustPanic(t,func(){_=badDynamicMap(true,&trace)});for _,ok:=range []bool{false,true}{trace=nil;_,e:=intValue(deferredLiteral(ok,&trace));want:=[]string{"first","middle","cleanup"};if ok{want=[]string{"first","middle","last","deferred","cleanup"};if e!=nil{t.Fatal(e)}}else if !errors.Is(e,failure){t.Fatal(e)};traceIs(t,trace,want)}}
func TestReceiveOrder(t *testing.T){trace:=[]string{};ch:=make(chan int,2);ch<-1;ch<-3;s,e:=sliceValue(receiveLiteral(ch,&trace));if e!=nil||!reflect.DeepEqual(s,[]int{1,2,3})||len(ch)!=0{t.Fatal(s,e)};traceIs(t,trace,[]string{"value"})}
`

func TestPropagationAggregateRejections(t *testing.T) {
	for name, source := range map[string]string{
		"unaddressable-array-slice": `func f()Option[int]{s:=[2]int{Option[int]::Some(1)?}[:];return Option[int]::Some(len(s))}`,
		"returned-array-slice":      `func f()Option[int]{s:=Option[[2]int]::Some([2]int{})?[:];return Option[int]::Some(len(s))}`,
		"array-constant-index":      `func f()Option[int]{a:=[2]int{Option[int]::Some(1)?};return Option[int]::Some(a[3])}`,
		"array-nonconstant-key":     `func f()Option[int]{a:=[2]int{Option[int]::Some(0)?:1};return Option[int]::Some(a[0])}`,
		"slice-nonconstant-key":     `func f()Option[int]{a:=[]int{Option[int]::Some(0)?:1};return Option[int]::Some(a[0])}`,
		"duplicate-key":             `func f()Option[int]{m:=map[int]int{1:Option[int]::Some(1)?,1:2};return Option[int]::Some(m[1])}`,
		"duplicate-field":           `type S struct{X int};func f()Option[int]{s:=S{X:Option[int]::Some(1)?,X:2};return Option[int]::Some(s.X)}`,
		"constant-bounds":           `func f(a [2]int)Option[int]{s:=a[Option[int]::Some(0)?:3];return Option[int]::Some(len(s))}`,
		"ordered-constant-bounds":   `func f(a [4]int)Option[int]{s:=a[2:1:Option[int]::Some(4)?];return Option[int]::Some(len(s))}`,
		"string-full-slice":         `func f()Option[int]{s:="abc"[Option[int]::Some(0)?:1:2];return Option[int]::Some(len(s))}`,
		"rvalue-array-method":       `type S struct{};func(*S)M()int{return 1};func f()Option[int]{n:=Option[[1]S]::Some([1]S{})?[Option[int]::Some(0)?].M();return Option[int]::Some(n)}`,
		"address-of-query":          `func f()Option[int]{p:=&(Option[int]::Some(1)?);return Option[int]::Some(*p)}`,
		"readonly-literal":          `type State struct{Items []int};func f(r &State)Option[int]{v:=[][]int{r.Items,[]int{Option[int]::Some(1)?}};return Option[int]::Some(len(v))}`,
		"unsafe-unevaluated":        `func f()Option[int]{n:=unsafe.Sizeof([1]int{Option[int]::Some(1)?});return Option[int]::Some(int(n))}`,
	} {
		t.Run(name, func(t *testing.T) {
			prefix := "package fixture\n"
			if strings.HasPrefix(name, "unsafe-") {
				prefix += "import \"unsafe\"\n"
			}
			_, err := frontend.Compile("aggregate_bad.hgo", []byte(prefix+source))
			if err == nil || !strings.Contains(err.Error(), "aggregate_bad.hgo") {
				t.Fatalf("expected located rejection: %v", err)
			}
		})
	}
}

func TestPropagationAggregateInstalledBusinessRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/hgo")
	cli := filepath.Join(bin, "hgo")
	outside := t.TempDir()
	source := filepath.Join(root, "examples/propagate_aggregate")
	generated := filepath.Join(t.TempDir(), "module")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	packagesWrite(t, generated, "business_test.go", `package main
import("errors";"strconv";"testing")
func TestBatch(t *testing.T){s,e:=resultText(batch("1","3"));if e!=nil||s!="batch 2 9223372036854775808"{t.Fatal(s,e)};_,e=resultText(batch("invalid","3"));var native *strconv.NumError;if !errors.As(e,&native){t.Fatal(e)}}
func TestHiddenContextualTypes(t *testing.T){if n,e:=resultWide(hiddenTypes(63));e!=nil||n!=uint64(1)<<63{t.Fatal(n,e)}}
`)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	if got := deliveryCommand(t, outside, cli, true, nil, "run", "-go", tool, source); got != "batch 2 9223372036854775808\ninvalid bounds\n" {
		t.Fatalf("business: %q", got)
	}
}

func TestPropagationAggregatePermissionsRace(t *testing.T) {
	enumGeneratedModule(t, `package fixture
func read(r &[4]int)Option[int]{return Option[int]::Some((*r)[Option[int]::Some(1)?])}
func value(v Option[int])int{match v{Option[int]::Some(n)=>{return n},Option[int]::None=>{return -1},};panic("unreachable")}
func observe()int{a:=[4]int{1,2,3,4};var r &[4]int=&a;w:=mut& a;(*w)[1]=9;return value(read(r))}
`, `package fixture
import "testing"
func TestReadonlyArrayIndex(t *testing.T){if observe()!=9{t.Fatal("array storage/permission")}}
`, false)
}
