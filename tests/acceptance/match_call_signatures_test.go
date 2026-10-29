package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMatchCallSignatures(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.go", `package model
 type secret uint64
 type Alias = secret
 type hidden uint64
 func Sink()func(secret)secret{return func(v secret)secret{return v}}
 func Hidden()func(hidden)int{return func(v hidden)int{return int(v)}}
 func Identity[T any](v T)T{return v}
 type S struct{N int;F func(int)int}
 func(s S)Value(n int)int{return s.N+n}
 func(s *S)Pointer(n int)int{if s==nil{return n};s.N+=n;return s.N}
 type I interface{Pointer(int)int}
 type Embedded struct{*S}
 func Value()S{return S{N:10}}
 func Array()[1]S{return [1]S{{N:10}}}
 `)
		packagesWrite(t, dir, "native.go", `package app
 func Generic[T any](v T)T{return v}
 `)
		packagesWrite(t, dir, "app.gox", "package app\nuse model as m;\npub enum E{A,B,}\n"+body)
		return dir
	}
	t.Run("execution", func(t *testing.T) {
		source := sourceFor(`var trace []int
func mark(n int)int{trace=append(trace,n);return n}
func choice()E{mark(2);return E::A}
func Static(n int)int{return n+1}
var savedStatic=Static
func StaticUsed()int{return Static(match E::A{E::A=>2,E::B=>0})}
func Sum(xs ...int)int{n:=0;for _,v:=range xs{n+=v};return n}
func StaticVariadic()int{return Sum(match E::A{E::A=>2,E::B=>0},3)}
func Factory()func(int)int{mark(1);return func(n int)int{mark(8);return n+10}}
func FactoryCall()int{return Factory()(match choice(){E::A=>mark(3),E::B=>0})}
func Fields()int{s:=m::S{F:func(n int)int{mark(8);return n+10}};get:=func()*m::S{mark(1);return &s};change:=func()int{s.F=func(n int)int{mark(99);return 99};return mark(3)};return get().F(match choice(){E::A=>change(),E::B=>0})}
func Methods()(int,int){s:=m::S{N:10};change:=func()int{s.N=20;return 3};v:=s.Value(match E::A{E::A=>change(),E::B=>0});s.N=10;p:=s.Pointer(match E::A{E::A=>change(),E::B=>0});return v,p}
func Bounds(){a:=[]m::S{};idx:=func()int{mark(1);return 1};_=a[idx()].Value(match E::A{E::A=>mark(3),E::B=>0})}
func AssertPointer(){var a any;_=a.(*m::S).Value(match E::A{E::A=>mark(3),E::B=>0})}
func Assert(){var a any=0;_=a.(m::S).Value(match E::A{E::A=>mark(3),E::B=>0})}
func NilValue(){var s *m::S;_=s.Value(match E::A{E::A=>mark(3),E::B=>0})}
func NilPointer()int{var s *m::S;return s.Pointer(match E::A{E::A=>mark(3),E::B=>0})}
func NilInterface(){var s m::I;_=s.Pointer(match E::A{E::A=>mark(3),E::B=>0})}
func TypedNilInterface()int{var p *m::S;var s m::I=p;return s.Pointer(match E::A{E::A=>mark(3),E::B=>0})}
func EmbeddedPointer()int{var s m::Embedded;return s.Pointer(match E::A{E::A=>mark(3),E::B=>0})}
func EmbeddedValue(){var s m::Embedded;_=s.Value(match E::A{E::A=>mark(3),E::B=>0})}
func MethodExpression()int{return (*m::S)::Pointer(nil,match E::A{E::A=>mark(3),E::B=>0})}
func NilFactory(){f:=func()func(int)int{mark(1);return nil};_=f()(match choice(){E::A=>mark(3),E::B=>0})}
func Variadic()int{g:=func(xs ...int)int{n:=0;for _,x:=range xs{n+=x};return n};return g(match E::A{E::A=>2,E::B=>0},3)}
func VariadicEmpty()bool{g:=func(n int,xs ...int)bool{return n==2&&xs==nil};return g(match E::A{E::A=>2,E::B=>0})}
func Spread(rest []int)int{g:=func(a,b int,xs ...int)int{xs[0]+=a+b;return len(xs)};return g(1,match E::A{E::A=>2,E::B=>0},rest...)}
func SpreadMatch(rest []int)bool{g:=func(xs ...int)bool{if len(xs)>0{xs[0]=9};return xs==nil};return g(match E::A{E::A=>rest,E::B=>nil}...)}
func WideSpread()m::Alias{g:=func(xs ...m::Alias)m::Alias{return xs[0]};return g(match E::A{E::A=>1<<63,E::B=>0})}
func PrivateAlias()m::Alias{return m::Sink()(match E::A{E::A=>1<<63,E::B=>0})}
func ExplicitGeneric()m::Alias{return m::Identity[m::Alias](match E::A{E::A=>1<<63,E::B=>0})}
func Own[T any](v T)T{return v}
func OwnGeneric()int{return Own[int](match E::A{E::A=>1,E::B=>0})}
func LocalGeneric()uint64{return Generic[uint64](match E::A{E::A=>1<<63,E::B=>0})}
func Shadow()uint64{type Count int;_ = Count(0);return Generic[uint64](match E::A{E::A=>1<<63,E::B=>0})}
func RecoverField()(ok bool){defer func(){s:=struct{F func(any)}{F:func(v any){ok=v=="boom"}};s.F(match E::A{E::A=>recover(),E::B=>nil})}();panic("boom")}
func Failed()Result[int,string]{mark(3);return Result[int,string]::Err("stop")}
func Failure()Result[int,string]{factory:=func()func(int,int)Result[int,string]{mark(1);return nil};defer func(){mark(9)}();return factory()(match choice(){E::A=>Failed()?,E::B=>0},mark(8))}
func IsErr(v Result[int,string])bool{return match v{Result[int,string]::Ok(_)=>false,Result[int,string]::Err(e)=>e=="stop"}}
func Jump(){if true{goto Done};m::Value().Value(match E::A{E::A=>1,E::B=>0});Done:return}
func BenchMethod(s *m::S)int{return s.Pointer(match E::A{E::A=>1,E::B=>0})}
func BenchSpread(rest []int)bool{return SpreadMatch(rest)}
func BenchGeneric()int{return m::Identity[int](match E::A{E::A=>1,E::B=>0})}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		first, err := os.ReadFile(filepath.Join(generated, "app.go"))
		if err != nil {
			t.Fatal(err)
		}
		again := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", again, source)
		second, _ := os.ReadFile(filepath.Join(again, "app.go"))
		if string(first) != string(second) {
			t.Fatal("nondeterministic")
		}
		packagesWrite(t, generated, "app_test.go", `package app
import("testing";"reflect";m "packages.example/app/model")
func want(t *testing.T,v []int){t.Helper();if !reflect.DeepEqual(trace,v){t.Fatal(trace,v)};trace=nil}
func caught(f func())(yes bool){defer func(){yes=recover()!=nil}();f();return}
func TestCallAndMethods(t *testing.T){trace=nil;if FactoryCall()!=13{t.Fatal("factory")};want(t,[]int{1,2,3,8});if Fields()!=13{t.Fatal("field")};want(t,[]int{1,2,3,8});a,b:=Methods();if a!=23||b!=23{t.Fatal(a,b)};Jump();if !RecoverField(){t.Fatal("recover")}}
func TestNativeTiming(t *testing.T){for i,pair:=range [][2]func(){
 {Bounds,func(){a:=[]m.S{};idx:=func()int{mark(1);return 1};_=a[idx()].Value(mark(3))}},
 {NilValue,func(){var s *m.S;_=s.Value(mark(3))}},
 {NilInterface,func(){var s m.I;_=s.Pointer(mark(3))}},
 {EmbeddedValue,func(){var s m.Embedded;_=s.Value(mark(3))}},
 {NilFactory,func(){f:=func()func(int)int{mark(1);return nil};_=f()(func()int{choice();return mark(3)}())}},
 }{trace=nil;p:=caught(pair[0]);actual:=append([]int(nil),trace...);trace=nil;q:=caught(pair[1]);if !p||!q||!reflect.DeepEqual(actual,trace){t.Fatal(i,p,q,actual,trace)}};trace=nil;for _,f:=range []func()int{NilPointer,TypedNilInterface,EmbeddedPointer,MethodExpression}{if f()!=3{t.Fatal("nil receiver")};want(t,[]int{3})}}
// Assertions versus argument calls are not ordered by the Go spec. Check our
// stable late-receiver rule separately, rather than pinning one optimizer trace.
func TestReceiverAssertions(t *testing.T){for _,f:=range []func(){Assert,AssertPointer}{trace=nil;if !caught(f){t.Fatal("no assertion panic")};want(t,[]int{3})};for _,f:=range []func(){func(){var a any=0;_=a.(m.S).Value(mark(3))},func(){var a any;_=a.(*m.S).Value(mark(3))}}{trace=nil;if !caught(f){t.Fatal("native assertion did not panic")};if len(trace)>1||(len(trace)==1&&trace[0]!=3){t.Fatal(trace)};t.Log("native assertion trace (unspecified relative order):",trace)};trace=nil}
func TestVariadicIdentity(t *testing.T){if OwnGeneric()!=1||StaticUsed()!=3||StaticVariadic()!=5||Variadic()!=5||!VariadicEmpty(){t.Fatal("variadic")};v:=[]int{4};if Spread(v)!=1||v[0]!=7{t.Fatal(v)};if !SpreadMatch(nil)||SpreadMatch([]int{})||SpreadMatch(v)||v[0]!=9{t.Fatal("slice identity")};if WideSpread()!=m.Alias(1)<<63||PrivateAlias()!=m.Alias(1)<<63||ExplicitGeneric()!=m.Alias(1)<<63||LocalGeneric()!=uint64(1)<<63||Shadow()!=uint64(1)<<63{t.Fatal("nominal/context")}}
func TestFailure(t *testing.T){trace=nil;if !IsErr(Failure()){t.Fatal("failure")};want(t,[]int{1,2,3,9})}
var sink int
func BenchmarkMethodMatch(b *testing.B){s:=m.S{};for i:=0;i<b.N;i++{sink=BenchMethod(&s)}}
func BenchmarkMethodGo(b *testing.B){s:=m.S{};for i:=0;i<b.N;i++{sink=s.Pointer(1)}}
func BenchmarkSpreadMatch(b *testing.B){v:=[]int{0};for i:=0;i<b.N;i++{BenchSpread(v)}}
func BenchmarkSpreadGo(b *testing.B){v:=[]int{0};g:=func(xs ...int)bool{if len(xs)>0{xs[0]=9};return xs==nil};for i:=0;i<b.N;i++{g(v...)}}
func BenchmarkGenericMatch(b *testing.B){for i:=0;i<b.N;i++{sink=BenchGeneric()}}
func BenchmarkGenericGo(b *testing.B){for i:=0;i<b.N;i++{sink=m.Identity[int](1)}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
		if os.Getenv("GOXIDE_MATCH_CALL_BENCH") != "" {
			t.Log(packagesCommand(t, generated, tool, true, "test", "-run=^$", "-bench=.", "-benchmem", "-benchtime=100ms", "-count=3", "-cpu=1", "."))
		}
	})
	for name, body := range map[string]string{
		"legacy-method-expression-dot":   `func Bad()int{return (*m::S).Pointer(nil,match E::A{E::A=>1,E::B=>0})}`,
		"static-type-namespace-conflict": `func Bad(){type m int;_ = m(0)}`,
		"multiple":                       `func Bad()(a,b int){return match E::A{E::A=>1,E::B=>0}}`,
		"unaddressable-array":            `func Bad()int{return m::Array()[0].Pointer(match E::A{E::A=>1,E::B=>0})}`,
		"readonly-method":                `func Bad(r &m::S){(*r).Pointer(match E::A{E::A=>1,E::B=>0})}`,
		"unaddressable":                  `func Bad()int{return m::Value().Pointer(match E::A{E::A=>1,E::B=>0})}`,
		"spread-arity":                   `func Bad(g func(int,...int)int,rest []int)int{return g(1,match E::A{E::A=>2,E::B=>0},rest...)}`,
		"spread-element":                 `func Bad(g func(...int)int)int{return g(match E::A{E::A=>2,E::B=>0}...)}`,
		"bad-generic":                    `func Bad()uint8{return m::Identity[uint8](match E::A{E::A=>256,E::B=>0})}`,
		"private-unnameable":             `func Bad()int{return m::Hidden()(match E::A{E::A=>1,E::B=>0})}`,
		"readonly-native":                `func native(p *int){};func Bad(r &int){native(match E::A{E::A=>r,E::B=>r})}`,
		"readonly-any":                   `func native(v any){};func Bad(r &int){native(match E::A{E::A=>r,E::B=>r})}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "app.gox:") && !strings.Contains(out, "app.go:") {
				t.Fatal("missing package diagnostic", out)
			}
		})
	}
}
