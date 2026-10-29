package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMatchNamedDynamicModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		prefix := "package app\n"
		if strings.HasPrefix(body, "use ") {
			line, rest, _ := strings.Cut(body, "\n")
			prefix += line + "\n"
			body = rest
		}
		packagesWrite(t, dir, "app.gox", prefix+"pub enum E{A(int),B,}\n"+body)
		return dir
	}
	t.Run("execution", func(t *testing.T) {
		source := sourceFor(`use errors;
var trace []int
func mark(n int)int{trace=append(trace,n);return n}
func choice()E{mark(2);return E::A(3)}
func guard(n int)bool{mark(n);return n==4}
func Named()(out int){out=10;defer func(){mark(out);out++}();return match choice(){E::A(out) if guard(4)=>out,E::A(_)=>99,E::B=>0}}
func NamedOld()(out int){out=10;defer func(){out++}();return match E::B{E::A(_)=>0,E::B=>out+1}}
func PanicNamed()(out int){out=10;defer func(){if recover()!=nil{mark(out);out=20}}();return match E::A(1){E::A(_)=>explode(),E::B=>0}}
func explode()int{panic("boom")}
type Wide uint64
func NamedShadow()(out Wide){type Wide string;_ = Wide("x");return match E::B{E::A(_)=>1,E::B=>1<<63}}
func Multi()(a,b int){return 0,0}
func Goto()(out int){if true{goto L};return match E::B{E::A(_)=>1,E::B=>2};L:return 9}
func RecoverDirect()(ok bool){defer func()(saved any){defer func(){ok=saved=="boom"}();return match E::B{E::A(_)=>nil,E::B=>recover()}}();panic("boom")}
func RecoverDynamic()(ok bool){defer func(){g:=func(v any){ok=v=="boom"};g(match E::B{E::A(_)=>nil,E::B=>recover()})}();panic("boom")}
type Handler func(int,int,int)int
func Dynamic()(out int){g:=Handler(func(a,b,c int)int{mark(6);return a+b+c});swap:=func()int{g=func(a,b,c int)int{mark(99);return 99};return mark(3)};defer func(){mark(out)}();return g(mark(1),match choice(){E::A(_) if guard(4)=>swap(),E::A(_)=>0,E::B=>0},mark(5))}
func Param(e E,g func(uint64)uint64)uint64{return g(match e{E::A(_)=>1<<63,E::B=>0})}
func shadow(n uint64)uint64{return n}
func Shadow(e E,shadow func(int)uint64)uint64{return shadow(match e{E::A(_)=>1,E::B=>2})}
func ParenShadow(e E,shadow (func(int)uint64))uint64{return shadow(match e{E::A(_)=>1,E::B=>2})}
func ParenLocal()uint64{var shadow (func(int)uint64);shadow=func(n int)uint64{return uint64(n+10)};return shadow(match E::B{E::A(_)=>1,E::B=>2})}
func GroupLocal()uint64{var (shadow (func(int)uint64));shadow=func(n int)uint64{return uint64(n+10)};return shadow(match E::B{E::A(_)=>1,E::B=>2})}
func Locals()int{g:=func(n int)int{return n+1};x:=g(match E::A(2){E::A(n)=>n,E::B=>0});x=g(match E::B{E::A(_)=>0,E::B=>x});var y int=g(match E::B{E::A(_)=>0,E::B=>x});return y}
func Statement(){g:=func(n int){mark(n)};g(match E::B{E::A(_)=>0,E::B=>7})}
func Binary(g func(uint64)uint64,n uint)uint64{return g(0+match E::B{E::A(_)=>1,E::B=>1<<n})}
func Nested(g func(int)int)int{return g(match E::A(2){E::A(n)=>g(match E::B{E::A(_)=>0,E::B=>n}),E::B=>0})}
func NilCall(){var g func(int,int,int);g(mark(1),match choice(){E::A(n)=>mark(n),E::B=>0},mark(5))}
func NilArgPanic(){var g func(int,int);g(match E::B{E::A(_)=>0,E::B=>mark(1)},explode())}
var stop=errors::New("stop")
func payload(ok bool)Result[int,error]{mark(3);if !ok{return Result[int,error]::Err(stop)};return Result[int,error]::Ok(7)}
func Failure(ok bool)(out Result[int,error]){g:=func(a,b int)Result[int,error]{mark(8);return Result[int,error]::Ok(a+b)};defer func(){match out{Result[int,error]::Ok(_)=>{mark(10)},Result[int,error]::Err(_)=>{mark(11)}}}();return g(match choice(){E::A(_)=>payload(ok)?,E::B=>0},mark(9))}
func NilFailure()(out Result[int,error]){var g func(int)Result[int,error];return g(match E::B{E::A(_)=>0,E::B=>payload(false)?})}
func ClosureTarget()int{f:=func()(out Result[int,error]){g:=func(n int)Result[int,error]{return Result[int,error]::Ok(n)};return g(match E::B{E::A(_)=>0,E::B=>payload(false)?})};v:=f();match v{Result[int,error]::Ok(_)=>{return 0},Result[int,error]::Err(_)=>{return 7}}}
func IsErr(v Result[int,error])bool{return match v{Result[int,error]::Ok(_)=>false,Result[int,error]::Err(e)=>errors::Is(e,stop)}}
func MakeA()E{return E::A(1)}
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
import("testing";"reflect")
func want(t *testing.T,v []int){t.Helper();if !reflect.DeepEqual(trace,v){t.Fatal(trace,v)};trace=nil}
func TestNamed(t *testing.T){trace=nil;if Named()!=4{t.Fatal("named")};want(t,[]int{2,4,3});if PanicNamed()!=20{t.Fatal("panic")};want(t,[]int{10});if NamedOld()!=12||NamedShadow()!=Wide(1)<<63||Goto()!=9||!RecoverDirect()||!RecoverDynamic(){t.Fatal("signature/frame")}}
func TestDynamic(t *testing.T){trace=nil;if Dynamic()!=9{t.Fatal("callee capture")};want(t,[]int{1,2,4,3,5,6,9});if Param(MakeA(),func(n uint64)uint64{return n})!=uint64(1)<<63||Locals()!=5||Binary(func(n uint64)uint64{return n},63)!=uint64(1)<<63||Nested(func(n int)int{return n+1})!=4{t.Fatal("context")};Statement();want(t,[]int{7})}
func TestShadowAndParenthesized(t *testing.T){h:=func(n int)uint64{return uint64(n+10)};if Shadow(MakeA(),h)!=11||ParenShadow(MakeA(),h)!=11||ParenLocal()!=12||GroupLocal()!=12{t.Fatal("shadowed function type")}}
func TestNilTiming(t *testing.T){trace=nil;func(){defer func(){if recover()==nil{t.Fatal("no nil panic")}}();NilCall()}();want(t,[]int{1,2,3,5});func(){defer func(){if recover()!="boom"{t.Fatal("nil function panicked before argument")}}();NilArgPanic()}();want(t,[]int{1})}
func TestFailure(t *testing.T){trace=nil;if !IsErr(Failure(false)){t.Fatal("failure")};want(t,[]int{2,3,11});if IsErr(Failure(true)){t.Fatal("success")};want(t,[]int{2,3,9,8,10});if !IsErr(NilFailure()){t.Fatal("nil failure")};want(t,[]int{3});if ClosureTarget()!=7{t.Fatal("closure target")};want(t,[]int{3})}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	for name, body := range map[string]string{
		"multiple-results": `func Bad()(a,b int){return match E::B{E::A(_)=>1,E::B=>0}}`,
		"arity":            `func Bad(g func(int,int)int)int{return g(match E::B{E::A(_)=>1,E::B=>0})}`,
		"overflow":         `func Bad(g func(uint8)int)int{return g(match E::B{E::A(_)=>256,E::B=>0})}`,
		"wrong-type":       `func Bad(g func(int)int)int{return g(match E::B{E::A(_)=>"bad",E::B=>0})}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			want := map[string]string{"multiple-results": "not enough return values", "arity": "not enough arguments", "overflow": "overflows", "wrong-type": "match arm has type"}[name]
			if (!strings.Contains(out, "app.gox:") && !strings.Contains(out, "app.go:")) || !strings.Contains(out, want) {
				t.Fatalf("missing package/type diagnostic: %s", out)
			}
		})
	}
}
