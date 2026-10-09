package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGoResultMacros(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	source := t.TempDir()
	packagesWrite(t, source, "go.mod", packagesModule())
	packagesWrite(t, source, "ops/macros.gox", `package ops
pub macro_rules! ordinary { ($x:expr) => { go_result!(int, $x) }; }
pub macro_rules! retained { ($x:expr) => { go_result_keep!(int, $x) }; }
pub macro_rules! typed { ($t:ident, $x:expr) => { go_result!($t, $x) }; }
`)
	packagesWrite(t, source, "native.go", `package app
type Number int
type ErrorAlias = error
func AliasCall()(Number,ErrorAlias){return 7,nil}
func Generic[T any](v T)(T,error){return v,nil}
`)
	packagesWrite(t, source, "app.gox", `package app
import("strconv";"io";"errors";m "packages.example/app/ops")
type Partial = struct{Value int; Err error}
var stop=errors.New("stop")
var trace []int
func mark(n int)int{trace=append(trace,n);return n}
func pair(v int)(int,error){mark(3);return v,stop}
func good(v int)(int,error){mark(3);return v,nil}
func unit(err error)error{mark(3);return err}
var Initial=go_result!(int,strconv.Atoi("42"))
func Parse(s string)Result[int,error]{return go_result!(int,strconv.Atoi(s))}
func ParseNext(s string)Result[string,error]{v:=go_result!(int,strconv.Atoi(s))?;return Result[string,error]::Ok(strconv.Itoa(v+1))}
func Read(r io.Reader,b []byte)Result[int,Partial]{return go_result_keep!(int,r.Read(b))}
func ReadNext(r io.Reader,b []byte)Result[string,Partial]{n:=go_result_keep!(int,r.Read(b))?;return Result[string,Partial]::Ok(strconv.Itoa(n))}
func Drop(r io.Reader,b []byte)Result[int,error]{return go_result!(int,r.Read(b))}
func Unit(e error)Result[struct{},error]{return go_result!(unit(e))}
func KeepUnit(e error)Result[struct{},struct{Value struct{}; Err error}]{return go_result_keep!(unit(e))}
func NativeAlias()Result[Number,error]{return go_result!(Number,AliasCall())}
func NativeGeneric()Result[int,error]{return go_result!(int,Generic(9))}
func Factory()Result[int,error]{factory:=func()func(int)(int,error){mark(1);return pair};return go_result!(int,factory()(mark(2)))}
func Methods(r io.Reader,b []byte)Result[int,Partial]{return go_result_keep!(int,r.Read(b))}
func Variadic()Result[int,error]{f:=func(xs ...int)(int,error){mark(3);return xs[0]+xs[1],nil};v:=[]int{4,5};return go_result!(int,f(v...))}
func Nested(ok bool)Result[int,error]{defer func(){mark(9)}();return go_result!(int,good(Parse(func()string{mark(1);if ok{return "8"};return "bad"}())?))}
func Recover()(got any){defer func(){f:=func(v any)(any,error){got=v;return v,nil};_=go_result!(any,f(recover()))}();panic("boom")}
func Panic(){defer func(){mark(9)}();f:=func()(int,error){mark(3);panic(stop)};_=go_result!(int,f())}
func Deferred(){defer go_result!(unit(nil));mark(4)}
func Concurrent(done chan int){go go_result!(int,func()(int,error){done<-1;return 1,nil}())}
func Hygienic()Result[int,error]{error:=99;nil:=1;goxideMacroGenerated:=2;_=error;_=nil;_=goxideMacroGenerated;return m.ordinary!(strconv.Atoi("11"))}
func FamilyShadow()any{Result:=99;_=Result;return go_result!(int,strconv.Atoi("13"))}
func FamilyTypeShadow()any{type Result int;_=Result(99);return go_result!(int,strconv.Atoi("14"))}
func MacroKeep()Result[int,Partial]{return m.retained!(pair(2))}
func MacroType()Result[Number,error]{return m.typed!(Number,AliasCall())}
func OrdinaryQuestion()Result[int,error]{return Result[int,error]::Ok(m.ordinary!(strconv.Atoi("12"))?)}
#[derive(Error)]
#[error("parse failure")]
type Failure struct {
 #[from]
 Cause error
}
func Convert(s string)Result[int,Failure]{n:=go_result!(int,strconv.Atoi(s))?;return Result[int,Failure]::Ok(n)}
`)
	generated := filepath.Join(t.TempDir(), "generated")
	packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
	again := filepath.Join(t.TempDir(), "generated")
	packagesCommand(t, root, cli, true, "transpile", "-o", again, source)
	first, err := os.ReadFile(filepath.Join(generated, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(again, "app.go"))
	if err != nil || string(first) != string(second) {
		t.Fatal("nondeterministic adapter expansion", err)
	}
	packagesWrite(t, generated, "adapter_test.go", `package app
import("testing";"io";"errors";"strconv";"reflect")
type reader struct{calls int}
func(r *reader)Read(p []byte)(int,error){r.calls++;return copy(p,"abc"),io.EOF}
type typedError struct{}
func(*typedError)Error()string{panic("Error must not run")}
type typedReader struct{}
func(typedReader)Read(p []byte)(int,error){var e *typedError;return 1,e}
func okInt(t *testing.T,v Result[int,error],want int){t.Helper();x,ok:=v.(GoxideResultOk[int,error]);if !ok||x.Value!=want{t.Fatal(v)}}
func TestParse(t *testing.T){okInt(t,Initial,42);okInt(t,Parse("42"),42);okInt(t,Hygienic(),11);okInt(t,FamilyShadow().(Result[int,error]),13);okInt(t,FamilyTypeShadow().(Result[int,error]),14);okInt(t,OrdinaryQuestion(),12);v:=Parse("bad").(GoxideResultErr[int,error]).Value;var n *strconv.NumError;if !errors.As(v,&n)||n.Num!="bad"{t.Fatal(v)};e:=Convert("bad").(GoxideResultErr[int,Failure]).Value;if !errors.As(e,&n){t.Fatal(e)};if x:=ParseNext("9").(GoxideResultOk[string,error]).Value;x!="10"{t.Fatal(x)};if _,ok:=ParseNext("bad").(GoxideResultErr[string,error]);!ok{t.Fatal("propagation")}}
func TestPartial(t *testing.T){r:=new(reader);b:=make([]byte,8);e:=Read(r,b).(GoxideResultErr[int,Partial]).Value;if e.Value!=3||e.Err!=io.EOF||!errors.Is(e.Err,io.EOF)||r.calls!=1||string(b[:3])!="abc"{t.Fatal(e,r,b)};e=ReadNext(r,b).(GoxideResultErr[string,Partial]).Value;if e.Value!=3||e.Err!=io.EOF||r.calls!=2{t.Fatal(e,r)};if e:=Drop(r,b).(GoxideResultErr[int,error]).Value;e!=io.EOF{t.Fatal(e)};e=Methods(r,b).(GoxideResultErr[int,Partial]).Value;if e.Value!=3||e.Err!=io.EOF{t.Fatal(e)};if _,ok:=any(e).(error);ok{t.Fatal("partial must not implement error")};e=Read(typedReader{},b).(GoxideResultErr[int,Partial]).Value;var typed *typedError;if e.Value!=1||e.Err==nil||!errors.As(e.Err,&typed)||typed!=nil{t.Fatal("typed nil")};if v:=Drop(typedReader{},b).(GoxideResultErr[int,error]).Value;v==nil||!errors.As(v,&typed){t.Fatal("typed nil ordinary")}}
func TestUnitsAndTypes(t *testing.T){trace=nil;var ptr *typedError;var err error=ptr;if v:=Unit(err).(GoxideResultErr[struct{},error]).Value;v!=err{t.Fatal("unit typed nil")};if v:=KeepUnit(err).(GoxideResultErr[struct{},struct{Value struct{}; Err error}]).Value;v.Err!=err{t.Fatal("keep unit typed nil")};if _,ok:=KeepUnit(nil).(GoxideResultOk[struct{},struct{Value struct{}; Err error}]);!ok{t.Fatal("keep unit success")};if _,ok:=Unit(nil).(GoxideResultOk[struct{},error]);!ok{t.Fatal("unit")};if e:=Unit(stop).(GoxideResultErr[struct{},error]).Value;e!=stop{t.Fatal(e)};if e:=KeepUnit(stop).(GoxideResultErr[struct{},struct{Value struct{}; Err error}]).Value;e.Err!=stop{t.Fatal(e)};if n:=NativeAlias().(GoxideResultOk[Number,error]).Value;n!=7{t.Fatal(n)};if n:=MacroType().(GoxideResultOk[Number,error]).Value;n!=7{t.Fatal(n)};okInt(t,NativeGeneric(),9);okInt(t,Variadic(),9)}
func TestOrderAndFrames(t *testing.T){trace=nil;e:=Factory().(GoxideResultErr[int,error]).Value;if e!=stop||!reflect.DeepEqual(trace,[]int{1,2,3}){t.Fatal(e,trace)};trace=nil;if _,ok:=Nested(false).(GoxideResultErr[int,error]);!ok||!reflect.DeepEqual(trace,[]int{1,9}){t.Fatal(trace)};trace=nil;okInt(t,Nested(true),8);if !reflect.DeepEqual(trace,[]int{1,3,9}){t.Fatal(trace)};if Recover()!="boom"{t.Fatal("recover frame")};trace=nil;func(){defer func(){if recover()!=stop{t.Fatal("panic identity")}}();Panic()}();if !reflect.DeepEqual(trace,[]int{3,9}){t.Fatal(trace)};trace=nil;Deferred();if !reflect.DeepEqual(trace,[]int{3,4}){t.Fatal("defer arguments",trace)};done:=make(chan int,1);Concurrent(done);if <-done!=1{t.Fatal("go arguments")};e2:=MacroKeep().(GoxideResultErr[int,Partial]).Value;if e2.Value!=2||e2.Err!=stop{t.Fatal(e2)}}
`)
	packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, generated, tool, true, "vet", "./...")
	for name, body := range map[string]string{
		"discard-partial":       `func bad()Result[int,error]{n:=go_result_keep!(int,strconv.Atoi("bad"))?;return Result[int,error]::Ok(n)}`,
		"wrong-T":               `func bad(){_=go_result!(any,strconv.Atoi("1"))}`,
		"multiple-values":       `func f()(int,int,error){return 1,2,nil};func bad(){_=go_result!(int,f())}`,
		"wrong-arity":           `func f()error{return nil};func bad(){_=go_result!(int,f())}`,
		"missing-type":          `func bad(){_=go_result!()}`,
		"not-call":              `func bad(e error){_=go_result!(e)}`,
		"concrete-error":        `type E struct{};func(E)Error()string{return "e"};func f()(int,E){return 1,E{}};func bad(){_=go_result!(int,f())}`,
		"unit-concrete":         `type E struct{};func(E)Error()string{return "e"};func f()E{return E{}};func bad(){_=go_result!(f())}`,
		"native-tuple-question": `func bad()Result[int,error]{n:=strconv.Atoi("1")?;return Result[int,error]::Ok(n)}`,
		"reserved-name":         `macro_rules! go_result {($x:expr)=>{$x};}`,
		"error-shadow":          `type error int;func bad(){_=go_result!(int,strconv.Atoi("1"))}`,
		"local-type":            `func bad(){type Local int;f:=func()(Local,error){return 1,nil};_=go_result!(Local,f())}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			packagesWrite(t, dir, "go.mod", packagesModule())
			packagesWrite(t, dir, "bad.gox", "package bad\nimport \"strconv\"\nvar _=strconv.Atoi\n"+body)
			output := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), dir)
			if !strings.Contains(output, "bad.gox") {
				t.Fatal("lost diagnostic location", output)
			}
			if name == "local-type" && !strings.Contains(output, "T must be nameable at package scope") {
				t.Fatal("missing concrete instance scope diagnostic", output)
			}
		})
	}
}

func TestGoResultIndependentModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	producer := t.TempDir()
	packagesWrite(t, producer, "go.mod", "module adapter.example/producer\ngo 1.27\n")
	packagesWrite(t, producer, "native.go", `package producer
import "io"
type Count = int
type ErrorAlias = error
func Read()(Count,ErrorAlias){return 3,io.EOF}
`)
	packagesWrite(t, producer, "adapter.gox", `package producer
pub macro_rules! retained {($call:expr)=>{go_result_keep!(int,$call)};}
type Partial = struct{Value int; Err error}
func Wrapped()Result[int,Partial]{return go_result_keep!(int,Read())}
`)
	prodOut := filepath.Join(t.TempDir(), "producer")
	packagesCommand(t, root, cli, true, "transpile", "-o", prodOut, producer)
	consumer := t.TempDir()
	packagesWrite(t, consumer, "go.mod", fmt.Sprintf("module adapter.example/consumer\ngo 1.27\nrequire adapter.example/producer v0.0.0\nreplace adapter.example/producer => %q\n", prodOut))
	packagesWrite(t, consumer, "consumer.gox", `package consumer
import(p "adapter.example/producer";"strconv")
func Native()Result[p.Count,p.Partial]{return go_result_keep!(p.Count,p.Read())}
func Forward()Result[string,p.Partial]{n:=p.Wrapped()?;return Result[string,p.Partial]::Ok(strconv.Itoa(n))}
func Match()int{return match p.Wrapped(){p.Result[int,p.Partial]::Ok(n)=>n,p.Result[int,p.Partial]::Err(e)=>e.Value}}
`)
	consOut := filepath.Join(t.TempDir(), "consumer")
	packagesCommand(t, root, cli, true, "transpile", "-o", consOut, consumer)
	packagesWrite(t, consOut, "consumer_test.go", `package consumer
import("testing";"io";p "adapter.example/producer")
func TestExternal(t *testing.T){a:=Native().(GoxideResultErr[p.Count,p.Partial]).Value;b:=Forward().(GoxideResultErr[string,p.Partial]).Value;if a.Value!=3||a.Err!=io.EOF||b!=a||Match()!=3{t.Fatal(a,b)}}
`)
	packagesCommand(t, prodOut, tool, true, "test", "-race", "-count=1", "./...")
	packagesCommand(t, prodOut, tool, true, "vet", "./...")
	packagesCommand(t, consOut, tool, true, "test", "-race", "-count=1", "-v", "./...")
	packagesCommand(t, consOut, tool, true, "vet", "./...")
	packagesWrite(t, consumer, "macro.gox", `package consumer
import p "adapter.example/producer"
func ImportedMacro(){_=p.retained!(p.Read())}
`)
	out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "negative"), consumer)
	if !strings.Contains(out, "cross-package macros require a compiled source package") {
		t.Fatal("expected existing precompiled macro boundary", out)
	}
}
