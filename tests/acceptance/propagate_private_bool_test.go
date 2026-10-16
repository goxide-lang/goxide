package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPropagationPrivateBoolContexts(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	sourceFor := func(body string) string {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
		packagesWrite(t, dir, "model/model.go", `package model
type flag bool
var Calls int
func Flag(v bool)flag{Calls++;return flag(v)}
func FlagArray(v flag)[1]flag{return [1]flag{v}}
func Kind(v any)int{f,ok:=v.(flag);if !ok{return -1};if f{return 1};return 0}
func Consume(v flag)int{return Kind(v)}
`)
		packagesWrite(t, dir, "app.gox", "package app\nuse model as m;\n"+body)
		return dir
	}
	t.Run("private-type-and-short-circuit", func(t *testing.T) {
		source := sourceFor(`use errors;
var Stop=errors::New("stop")
var Loads,Cleanup,After int
func load(ok bool)Result[int,error]{Loads++;if ok{return Result[int,error]::Ok(7)};return Result[int,error]::Err(Stop)}
func And(enabled,ok bool)Result[int,error]{defer func(){Cleanup++}();x:=m::Kind((enabled==true)&&m::Flag(load(ok)?==7));After++;return Result[int,error]::Ok(x)}
func Or(enabled,ok bool)Result[int,error]{x:=m::Kind((enabled==true)||m::Flag(load(ok)?==7));return Result[int,error]::Ok(x)}
func Length(enabled,ok bool)Result[int,error]{x:=len(m::FlagArray((enabled==true)&&m::Flag(load(ok)?==7)));return Result[int,error]::Ok(x)}
func Capacity(enabled,ok bool)Result[int,error]{x:=cap(m::FlagArray((enabled==true)||m::Flag(load(ok)?==7)));return Result[int,error]::Ok(x)}
func Local(enabled,ok bool)Result[int,error]{x:=(enabled==true)&&m::Flag(load(ok)?==7);var v any=x;return Result[int,error]::Ok(m::Kind(v))}
func Nested(enabled,ok bool)Result[int,error]{x:=m::Consume(!((enabled==true)&&m::Flag(load(ok)?==7)));return Result[int,error]::Ok(x)}
func Closure()Result[int,error]{f:=func()Result[int,error]{x:=m::Kind((1==1)&&m::Flag(load(false)?==7));return Result[int,error]::Ok(x)};_=f();return Result[int,error]::Ok(9)}
func NativeRecover() (out int){defer func()Result[int,error]{r:=Result[int,error]::Ok(7);x:=m::Kind((1==1)&&m::Flag((recover()!=nil)&&(r?==7)));out=x;return Result[int,error]::Ok(x)}();panic("native")}
func Input(enabled bool,r Result[int,error])Result[int,error]{x:=m::Consume((enabled==true)&&m::Flag(r?==7));return Result[int,error]::Ok(x)}
func Read(r Result[int,error])(int,error){match r{Result[int,error]::Ok(v)=>{return v,nil},Result[int,error]::Err(e)=>{return 0,e}}}
`)
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		twice := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", twice, source)
		first, err := os.ReadFile(filepath.Join(generated, "app.go"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(twice, "app.go"))
		if err != nil || string(first) != string(second) {
			t.Fatal("unstable lowering", err)
		}
		for _, marker := range []string{".Catch(", ".Fail(", "Assignment["} {
			if strings.Contains(string(first), marker) {
				t.Fatalf("new guard %s", marker)
			}
		}
		packagesWrite(t, generated, "private_bool_test.go", `package app
import __borrow_std "github.com/goxide-lang/std/sum"

import("testing";"errors";m "packages.example/app/model")
func reset(){Loads=0;Cleanup=0;After=0;m.Calls=0}
func check(t *testing.T,r __borrow_std.Result[int,error],want int){t.Helper();v,e:=Read(r);if e!=nil||v!=want{t.Fatal(v,e,want)}}
func TestPrivateIdentityAndSkippedArms(t *testing.T){
 reset();check(t,And(false,false),0);if Loads!=0||m.Calls!=0||After!=1||Cleanup!=1{t.Fatal(Loads,m.Calls,After,Cleanup)}
 reset();check(t,And(true,true),1);if Loads!=1||m.Calls!=1{t.Fatal(Loads,m.Calls)}
 reset();check(t,Or(true,false),1);if Loads!=0||m.Calls!=0{t.Fatal(Loads,m.Calls)}
 reset();check(t,Or(false,true),1);check(t,Local(true,true),1);check(t,Nested(true,true),0);if Loads!=3||m.Calls!=3{t.Fatal(Loads,m.Calls)}
}
func TestArrayMeasurementEvaluation(t *testing.T){
 reset();check(t,Length(false,false),1);check(t,Capacity(true,false),1);if Loads!=0||m.Calls!=0{t.Fatal("unselected")}
 check(t,Length(true,true),1);check(t,Capacity(false,true),1);if Loads!=2||m.Calls!=2{t.Fatal("len/cap operand skipped",Loads,m.Calls)}
}
func TestNearestFailureAndCleanup(t *testing.T){
 reset();_,e:=Read(And(true,false));if !errors.Is(e,Stop)||After!=0||Cleanup!=1||Loads!=1||m.Calls!=0{t.Fatal(e,After,Cleanup,Loads,m.Calls)}
 _,e=Read(Length(true,false));if !errors.Is(e,Stop)||m.Calls!=0{t.Fatal(e,m.Calls)}
 check(t,Input(false,__borrow_std.Result[int,error]{}),0);check(t,Closure(),9);if NativeRecover()!=1{t.Fatal("recover changed")}
}
`)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	})
	for name, body := range map[string]string{
		"typed-bool-not-convertible": `func Bad(b bool,r Result[int,error])Result[int,error]{x:=m::Consume(b&&m::Flag(r?==7));return Result[int,error]::Ok(x)}`,
		"private-to-builtin":         `func Bool(v bool){};func Bad(r Result[int,error])Result[int,error]{Bool((1==1)&&m::Flag(r?==7));return r}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(body)
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			if (!strings.Contains(out, "app.gox:") && !strings.Contains(out, "app.go:")) || !strings.Contains(out, "bool") {
				t.Fatal(out)
			}
		})
	}
}
