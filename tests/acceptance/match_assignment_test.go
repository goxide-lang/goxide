package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMatchAssignmentInferenceModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "hgo")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/hgo")
	sourceFor := func(body string) string {
		dir := filepath.Join(t.TempDir(), "source")
		packagesWrite(t, dir, "go.mod", packagesModule(root))
		packagesWrite(t, dir, "model/model.hgo", `package model
pub enum Mode{Idle,Value(int)}
type Count uint64
func Number()Count{return Count(7)}
`)
		packagesWrite(t, dir, "app.hgo", "package app\nimport m \"packages.example/app/model\"\n"+body)
		return dir
	}
	t.Run("business-race-determinism", func(t *testing.T) {
		source := sourceFor(`import alias "packages.example/app/model"
import "errors"
var calls,arms int
func next(b bool)m.Mode{calls++;if b{return m.Mode::Value(7)};return m.Mode::Idle}
func effect(n int)int{arms++;return n}
func Basic(b bool)int{
 calls=0;arms=0
 x:=match next(b){m.Mode::Value(v)=>effect(v),m.Mode::Idle=>effect(2)}
 x=match next(b){m.Mode::Value(x)=>effect(x+1),m.Mode::Idle=>effect(x+2)}
 if calls!=2||arms!=2{panic("evaluation order")};return x
}
func Shadow()int{x:=40;{x:=match m.Mode::Idle{m.Mode::Idle=>x+2,m.Mode::Value(v)=>v};return x}}
func Wide(b bool,n uint)uint64{
 x:=match next(b){m.Mode::Value(_)=>uint64(1),m.Mode::Idle=>1<<n}
 var y uint64
 y=match next(b){m.Mode::Value(_)=>x,m.Mode::Idle=>1<<63}
 return y
}
func Named(b bool)alias.Count{
 x:=match next(b){m.Mode::Value(_)=>m.Number(),m.Mode::Idle=>alias.Count(9)}
 {type Count bool;_ = Count(false);m:=1;_=m;x=match next(b){alias.Mode::Value(_)=>1,alias.Mode::Idle=>11}}
 return x
}
func Nil(b bool)*int{x:=match next(b){m.Mode::Value(_)=>(*int)(nil),m.Mode::Idle=>nil};return x}
func Local()int{type N int;n:=N(0);n=match m.Mode::Idle{m.Mode::Idle=>3,m.Mode::Value(_)=>4};return int(n)}
func InferredVar()int{var x=match m.Mode::Idle{m.Mode::Idle=>5,m.Mode::Value(_)=>6};return x}
var sentinel=errors.New("stop")
var after,cleanup int
func value(ok bool)Result[int,error]{if ok{return Result[int,error]::Ok(7)};return Result[int,error]::Err(sentinel)}
func Propagate(ok bool)Result[int,error]{
 defer func(){cleanup++}()
 x:=match m.Mode::Idle{m.Mode::Idle=>match m.Mode::Idle{m.Mode::Idle=>value(ok)?,m.Mode::Value(_)=>2},m.Mode::Value(_)=>3}
 y:=match m.Mode::Idle{m.Mode::Idle=>Result[int,error]::Ok(x+1),m.Mode::Value(_)=>Result[int,error]::Err(sentinel)}
 z:=y?;after++;return Result[int,error]::Ok(z)
}
func CheckPropagation(ok bool)(int,error){r:=Propagate(ok);match r{Result[int,error]::Ok(v)=>{return v,nil},Result[int,error]::Err(e)=>{return 0,e}};return -1,nil}
func Closure()Result[int,error]{
 f:=func()Result[int,error]{x:=match m.Mode::Idle{m.Mode::Idle=>value(false)?,m.Mode::Value(_)=>3};return Result[int,error]::Ok(x)}
 _=f();return Result[int,error]::Ok(10)
}
func CheckClosure()int{return match Closure(){Result[int,error]::Ok(v)=>v,Result[int,error]::Err(_)=>-1}}
func AssignmentExit()Result[int,error]{x:=99;x=match m.Mode::Idle{m.Mode::Idle=>value(false)?,m.Mode::Value(_)=>1};after+=x;return Result[int,error]::Ok(x)}
func References(a &int,b &int)&int{r:=match m.Mode::Idle{m.Mode::Idle=>a,m.Mode::Value(_)=>b};r=match m.Mode::Idle{m.Mode::Idle=>b,m.Mode::Value(_)=>a};return r}
func ReferenceValue()int{a:=1;b:=2;return *References(&a,&b)}
func Goto()int{x:=1;if x==1{goto L};x=match m.Mode::Idle{m.Mode::Idle=>2,m.Mode::Value(_)=>3};L:return x}
func Recover() (out int){defer func(){x:=match m.Mode::Idle{m.Mode::Idle=>recover(),m.Mode::Value(_)=>nil};if x!=nil{out=1}}();panic("ordinary")}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		twice := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
		filepath.WalkDir(generated, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				t.Fatal(err)
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(generated, path)
			a, _ := os.ReadFile(path)
			b, e := os.ReadFile(filepath.Join(twice, rel))
			if e != nil || string(a) != string(b) {
				t.Fatalf("nondeterministic %s: %v", rel, e)
			}
			if strings.Contains(string(a), "__hgo_match_expr_infer(") || strings.Contains(string(a), "__hgo_match_expr_context(") {
				t.Fatalf("typing marker leaked in %s", rel)
			}
			return nil
		})
		packagesWrite(t, generated, "inference_test.go", `package app
import("testing";"errors")
func TestValuesAndIdentity(t *testing.T){if Basic(true)!=8||Basic(false)!=4||Shadow()!=42||Named(true)!=1||Named(false)!=11||Nil(true)!=nil||Nil(false)!=nil||Local()!=3||InferredVar()!=5||Goto()!=1||Recover()!=1||ReferenceValue()!=2{t.Fatal("value/scope changed")}}
func TestContextualConstants(t *testing.T){if Wide(false,63)!=uint64(1)<<63||Wide(true,63)!=1{t.Fatal("constant/shift context")}}
func TestNearestFailure(t *testing.T){after=0;cleanup=0;if v,e:=CheckPropagation(true);v!=8||e!=nil{t.Fatal(v,e)};if v,e:=CheckPropagation(false);v!=0||!errors.Is(e,sentinel){t.Fatal(v,e)};_=AssignmentExit();if after!=1||cleanup!=2||CheckClosure()!=10{t.Fatal(after,cleanup)}}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	cases := map[string]string{
		"uninitialized reference": `func Bad(r &int){var x &int;_=x;_=r}`,
		"forged temporary":        `func Bad(r &int){var __hgo_match_expr_1 &int;_=__hgo_match_expr_1;_=r}`,
		"mixed types":             `func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>1,m.Mode::Value(_)=>"bad"};_=x}`,
		"defined identity":        `type A int;type B int;func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>A(1),m.Mode::Value(_)=>B(1)};_=x}`,
		"nil only":                `func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>nil,m.Mode::Value(_)=>nil};_=x}`,
		"overflow":                `func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>uint8(1),m.Mode::Value(_)=>256};_=x}`,
		"negative unsigned":       `func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>uint64(1),m.Mode::Value(_)=>-1};_=x}`,
		"no common interface":     `import "errors";func Bad(){var e error;x:=match m.Mode::Idle{m.Mode::Idle=>e,m.Mode::Value(_)=>errors.New("x")};_=x}`, // Both are already error: replaced below with concrete pointer.
		"context mismatch":        `func Bad(){x:=1;x=match m.Mode::Idle{m.Mode::Idle=>1,m.Mode::Value(_)=>"bad"};_=x}`,
		"no new variables":        `func Bad(){x:=1;x:=match m.Mode::Idle{m.Mode::Idle=>1,m.Mode::Value(_)=>2};_=x}`,
		"complex destination":     `func Bad(){x:=[]int{1};x[0]=match m.Mode::Idle{m.Mode::Idle=>1,m.Mode::Value(_)=>2}}`,
		"control header":          `func Bad(){if x:=match m.Mode::Idle{m.Mode::Idle=>1,m.Mode::Value(_)=>2};x>0{}}`,
		"missing arm":             `func Bad(){x:=match m.Mode::Idle{m.Mode::Idle=>1};_=x}`,
		"readonly write":          `func Bad(r &int){x:=match m.Mode::Idle{m.Mode::Idle=>r,m.Mode::Value(_)=>r};*x=2}`,
		"readonly any":            `func Bad(r &int){x:=match m.Mode::Idle{m.Mode::Idle=>r,m.Mode::Value(_)=>r};var erased any=x;_=erased}`,
	}
	cases["no common interface"] = `type E struct{};func(*E)Error()string{return "x"};func Bad(){var e error;x:=match m.Mode::Idle{m.Mode::Idle=>e,m.Mode::Value(_)=>&E{}};_=x}`
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if !strings.Contains(out, "app.hgo") {
				t.Fatal(out)
			}
			if name == "mixed types" || name == "nil only" || name == "defined identity" || name == "overflow" {
				if !strings.Contains(out, "explicit typed local initializer") || !strings.Contains(out, "app.hgo:3:") {
					t.Fatalf("missing source/type guidance: %s", out)
				}
			}
		})
	}
}
