package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMatchBinaryAndIndexedModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.gox", `package model
pub enum E{A,B}
type Word uint64
type Flag bool
`)
		packagesWrite(t, dir, "app.gox", `package app
use model as m;
`+body)
		return dir
	}
	t.Run("execution", func(t *testing.T) {
		source := sourceFor(`use errors;
var trace []int
func mark(n int)int{trace=append(trace,n);return n}
func choice()m::E{mark(2);return m::E::A}
func Binary()int{return mark(1)+match choice(){m::E::A=>mark(3),m::E::B=>mark(99)}*mark(4)}
func Both()int{return (match m::E::A{m::E::A=>mark(1),m::E::B=>0}) + (match m::E::A{m::E::A=>mark(2),m::E::B=>0})}
func makeA()m::E{return m::E::A}
func Plain(e m::E)int{return 10+match e{m::E::A=>1,m::E::B=>2}}
func Baseline(e m::E)int{var x int;match e{m::E::A=>{x=1},m::E::B=>{x=2}};return 10+x}
func Short(b bool)bool{return b && match choice(){m::E::A=>mark(3)>0,m::E::B=>false}}
func Or(b bool)bool{return b || match choice(){m::E::A=>true,m::E::B=>false}}
func Wide(n uint)m::Word{return 0+match m::E::A{m::E::A=>1<<n,m::E::B=>1<<63}}
func identity(n int)int{return n}
func StaticCall()int{return identity(1+match m::E::A{m::E::A=>2,m::E::B=>3})}
func ReferenceCompare()bool{x:=1;r:=&x;return r==(match m::E::A{m::E::A=>r,m::E::B=>r})}
func Peer()m::Word{u:=m::Word(1);x:=u+match m::E::A{m::E::A=>1<<63,m::E::B=>2};return x}
func NestedPeer()m::Word{u:=m::Word(1);x:=u+(10+match m::E::A{m::E::A=>1<<63,m::E::B=>2});return x}
func TwoMatches()uint64{x:=(match m::E::A{m::E::A=>1<<63,m::E::B=>0})+(match m::E::A{m::E::A=>uint64(1),m::E::B=>uint64(0)});return x}
func NestedCompare()bool{u:=uint64(1)<<63;return (0+(match m::E::A{m::E::A=>1<<63,m::E::B=>0}))==u}
func PeerCompare()bool{u:=uint64(1)<<63;return (match m::E::A{m::E::A=>1<<63,m::E::B=>2})==u}
func LocalType()int{type N int;f:=func()N{return 10+match m::E::A{m::E::A=>1,m::E::B=>2}};return int(f())}
func GotoReturn()int{b:=true;if b{goto L};return 10+match m::E::A{m::E::A=>1,m::E::B=>2};L:return 8}
func GotoIndex()int{a:=[1]int{};b:=true;if b{goto L};a[0]=match m::E::A{m::E::A=>1,m::E::B=>2};L:return a[0]}
func Store(a []int,i int,e m::E){a[i]=match e{m::E::A=>1,m::E::B=>2}}
func StoreBaseline(a []int,i int,e m::E){base:=a;index:=i;var x int;match e{m::E::A=>{x=1},m::E::B=>{x=2}};base[index]=x}
func Any()any{return 10+match m::E::A{m::E::A=>1,m::E::B=>2}}
func Compare()bool{return (match m::E::A{m::E::A=>1,m::E::B=>2})==1}
func Shift()uint64{return uint64(1)<<match m::E::A{m::E::A=>2,m::E::B=>3}}
func NamedBool()m::Flag{return false || match m::E::A{m::E::A=>true,m::E::B=>false}}
func Locals()int{x:=10+match m::E::A{m::E::A=>2,m::E::B=>3};x=20+match m::E::A{m::E::A=>x,m::E::B=>0};var y int=1+match m::E::A{m::E::A=>x,m::E::B=>0};return y}
func Index()int{a:=[1]int{};a[mark(0)]=match choice(){m::E::A=>mark(3),m::E::B=>4};return a[0]}
func RebindSlice()(int,int){a:=[]int{1};old:=a;index:=func()int{mark(1);return 0};replace:=func()int{a=[]int{9};return mark(3)};a[index()]=match choice(){m::E::A=>replace(),m::E::B=>0};return old[0],a[0]}
func RebindMap()(int,int){a:=map[int]int{0:1};old:=a;replace:=func()int{a=map[int]int{0:9};return 3};a[0]=match choice(){m::E::A=>replace(),m::E::B=>0};return old[0],a[0]}
func RebindArray()int{a:=[1]int{1};replace:=func()int{a=[1]int{9};return 3};a[0]=match m::E::A{m::E::A=>replace(),m::E::B=>0};return a[0]}
func Bounds(a []int){a[mark(5)]=match choice(){m::E::A=>mark(3),m::E::B=>0}}
func NilMap(a map[int]int){a[mark(0)]=match choice(){m::E::A=>mark(3),m::E::B=>0}}
func NilArray(a *[1]int){a[mark(0)]=match choice(){m::E::A=>mark(3),m::E::B=>0}}
var stop=errors::New("stop")
func wideValue()Result[uint64,error]{return Result[uint64,error]::Ok(4)}
func TwoQuestions()Result[uint64,error]{v:=(match m::E::A{m::E::A=>1,m::E::B=>0})+(match m::E::A{m::E::A=>wideValue()?,m::E::B=>uint64(0)});return Result[uint64,error]::Ok(v)}
func CheckTwoQuestions()uint64{return match TwoQuestions(){Result[uint64,error]::Ok(v)=>v,Result[uint64,error]::Err(_)=>0}}
func value(ok bool)Result[int,error]{if ok{return Result[int,error]::Ok(mark(3))};return Result[int,error]::Err(stop)}
func Failure(ok bool,a []int)Result[int,error]{defer func(){mark(8)}();a[mark(0)]=match choice(){m::E::A=>value(ok)?,m::E::B=>99};var x int=mark(4)+match m::E::A{m::E::A=>value(ok)?,m::E::B=>0};mark(9);return Result[int,error]::Ok(x)}
func CheckFailure(ok bool,a []int)bool{return match Failure(ok,a){Result[int,error]::Ok(_)=>true,Result[int,error]::Err(_)=>false}}
func Skip()Result[int,error]{var b bool=false && match m::E::A{m::E::A=>value(false)?>0,m::E::B=>true};if b{panic("skip")};return Result[int,error]::Ok(1)}
func CheckSkip()bool{return match Skip(){Result[int,error]::Ok(_)=>true,Result[int,error]::Err(_)=>false}}
func Recovered()(out bool){defer func(){out=(match m::E::A{m::E::A=>recover(),m::E::B=>nil})!=nil}();panic("direct")}
func UserClosure()Result[int,error]{f:=func()Result[int,error]{return match m::E::A{m::E::A=>Result[int,error]::Ok(value(false)?),m::E::B=>Result[int,error]::Ok(0)}};_=f();return Result[int,error]::Ok(7)}
func CheckUserClosure()int{return match UserClosure(){Result[int,error]::Ok(n)=>n,Result[int,error]::Err(_)=>0}}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		twice := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
		err := filepath.WalkDir(generated, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(generated, path)
			a, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			b, e := os.ReadFile(filepath.Join(twice, rel))
			if e != nil {
				return e
			}
			if string(a) != string(b) {
				t.Fatalf("nondeterministic %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		data, e := os.ReadFile(filepath.Join(generated, "app.go"))
		if e != nil {
			t.Fatal(e)
		}
		for _, marker := range []string{"__goxide_match_expr_pending", "__goxide_match_expr_operand_context(", "__goxide_match_expr_zero[", "__goxide_match_expr_return(", "__goxide_match_expr_peers(", ".Catch(", ".Fail("} {
			if strings.Contains(string(data), marker) {
				t.Fatalf("residual %s", marker)
			}
		}
		packagesWrite(t, generated, "match_test.go", `package app
import("testing";"reflect";m "packages.example/app/model")
func want(t *testing.T,v []int){t.Helper();if !reflect.DeepEqual(trace,v){t.Fatal(trace,v)};trace=nil}
func TestBinaryAndContext(t *testing.T){trace=nil;if Binary()!=13{t.Fatal("binary")};want(t,[]int{1,2,3,4});if Both()!=3{t.Fatal("both")};want(t,[]int{1,2});if Plain(makeA())!=11||Wide(63)!=m.Word(1)<<63||Any()!=11||!Compare()||Shift()!=4||!bool(NamedBool())||Locals()!=33||Peer()!=(m.Word(1)<<63)+1||!PeerCompare()||LocalType()!=11||GotoReturn()!=8||GotoIndex()!=0||StaticCall()!=3||!ReferenceCompare()||NestedPeer()!=(m.Word(1)<<63)+11||TwoMatches()!=(uint64(1)<<63)+1||!NestedCompare(){t.Fatal("type/context")}}
func TestShortCircuit(t *testing.T){trace=nil;if Short(false)||!Or(true)||!CheckSkip(){t.Fatal("short")};want(t,nil);if !Short(true){t.Fatal("selected")};want(t,[]int{2,3})}
func TestStorage(t *testing.T){trace=nil;if Index()!=3{t.Fatal("array")};want(t,[]int{0,2,3});x,y:=RebindSlice();if x!=3||y!=9{t.Fatal(x,y)};want(t,[]int{1,2,3});x,y=RebindMap();if x!=3||y!=9{t.Fatal(x,y)};want(t,[]int{2});if RebindArray()!=3{t.Fatal("array copied")}}
func TestPanicAndFailureTiming(t *testing.T){for _,f:=range []func(){func(){Bounds(nil)},func(){NilMap(nil)},func(){NilArray(nil)}}{trace=nil;func(){defer func(){if recover()==nil{t.Fatal("missing panic")}}();f()}();if len(trace)!=3||trace[1]!=2||trace[2]!=3{t.Fatal(trace)}};trace=nil;if CheckFailure(false,nil){t.Fatal("failure")};want(t,[]int{0,2,8});a:=[]int{0};if !CheckFailure(true,a)||a[0]!=3{t.Fatal(a)};want(t,[]int{0,2,3,4,3,9,8});if !Recovered()||CheckUserClosure()!=7||CheckTwoQuestions()!=5{t.Fatal("function boundary")}}
var sink int
func BenchmarkMatch(b *testing.B){e:=makeA();for i:=0;i<b.N;i++{sink=Plain(e)}}
func BenchmarkExplicit(b *testing.B){e:=makeA();for i:=0;i<b.N;i++{sink=Baseline(e)}}
func BenchmarkIndexMatch(b *testing.B){e:=makeA();a:=make([]int,16);for i:=0;i<b.N;i++{Store(a,i&15,e)};sink=a[0]}
func BenchmarkIndexExplicit(b *testing.B){e:=makeA();a:=make([]int,16);for i:=0;i<b.N;i++{StoreBaseline(a,i&15,e)};sink=a[0]}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
		if os.Getenv("GOXIDE_MATCH_BENCH") != "" {
			out := packagesCommand(t, generated, tool, true, "test", "-run", "^$", "-bench", "Benchmark(Match|Explicit|IndexMatch|IndexExplicit)$", "-benchmem", "-benchtime=200ms", "-count=5")
			t.Log(out)
		}
	})
	for name, body := range map[string]string{
		"overflow":         `func Bad()uint8{return 1+match m::E::A{m::E::A=>256,m::E::B=>1}}`,
		"different-types":  `func Bad()int{return 1+match m::E::A{m::E::A=>"x",m::E::B=>1}}`,
		"named-types":      `type N int;func Bad()int{return 1+match m::E::A{m::E::A=>N(1),m::E::B=>N(2)}}`,
		"readonly-store":   `func Bad(r &[]int){a:=*r;a[0]=match m::E::A{m::E::A=>1,m::E::B=>2}}`,
		"readonly-erasure": `func Bad(r &int){a:=[]any{nil};a[0]=match m::E::A{m::E::A=>r,m::E::B=>r}}`,
		"unaddressable":    `func Bad(){makeArray()[0]=match m::E::A{m::E::A=>1,m::E::B=>2}};func makeArray()[1]int{return [1]int{}}`,
		"shift-count":      `func Bad()int{return 1<<match m::E::A{m::E::A=>1.5,m::E::B=>2.5}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
		})
	}
}
