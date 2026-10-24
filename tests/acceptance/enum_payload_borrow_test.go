package acceptance_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Enum payload slots are immutable in Goxide. Ordinary variable rebinding and
// the capabilities of values copied from a payload remain separate operations.
func TestEnumPayloadBorrowModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "run", "./scripts/build.go", "-o", cli)
	const header = `package app
pub enum E { A(int), B(int), }
func Value(e E)int{return match e{E::A(n)=>n,E::B(n)=>n}}
`
	sourceFor := func(t *testing.T, source string) string {
		t.Helper()
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", packagesModule())
		writeAcceptanceFixtureRoots(t, dir, "packages.example/app", nil)
		packagesWrite(t, dir, "app.gox", source)
		return dir
	}
	run := func(t *testing.T, source, assertions string) {
		t.Helper()
		input := sourceFor(t, source)
		generated := filepath.Join(t.TempDir(), "generated")
		repeated := filepath.Join(t.TempDir(), "repeated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, input)
		packagesCommand(t, root, cli, true, "transpile", "-o", repeated, input)
		first, err := os.ReadFile(filepath.Join(generated, "app.go"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(repeated, "app.go"))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("enum payload generation changed: %v", err)
		}
		packagesWrite(t, generated, "borrow_test.go", assertions)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	}

	t.Run("readonly-bindings-and-variable-rebinding", func(t *testing.T) {
		run(t, header+`
func Borrow(e &E)&int{match *e{E::A(ref p)=>{return p},E::B(ref p)=>{return p}}}
func Replace(e &mut E,value int){*e=E::B(value)}
func Read()int{e:=E::A(3);return match &e{E::A(p)=>*p,E::B(p)=>*p}}
func Copies()[4]int{
 e:=E::A(1);before:=e;p:=Borrow(&e);Replace(&mut e,9)
 return [4]int{Value(e),Value(before),*p,Read()}
}
func Escaped()func()int{e:=E::A(10);p:=Borrow(&e);e=E::B(20);return func()int{return *p}}
`, `package app
import("runtime";"testing")
func TestReadBindings(t *testing.T){if got:=Copies();got!=([4]int{9,1,1,3}){t.Fatal(got)}}
func TestEscaped(t *testing.T){read:=Escaped();runtime.GC();if got:=read();got!=10{t.Fatal(got)}}
`)
	})

	t.Run("payload-containers-and-value-copies", func(t *testing.T) {
		run(t, header+`
type Payload struct{Number int;Array [1]int;Items []int;Index map[string]int;Pointer *int}
pub enum Wrapped{Item(Payload),Empty,}
func Containers()[6]int{
 number:=3;index:=map[string]int{"n":2}
 w:=Wrapped::Item(Payload{Number:1,Array:[1]int{2},Items:[]int{1},Index:index,Pointer:&number});copy:=w
 match w{Wrapped::Item(p)=>{p.Number=90;p.Array[0]=91;p.Items[0]=8;p.Index["n"]=9;*p.Pointer=10},Wrapped::Empty=>{}}
 return match copy{Wrapped::Item(p)=>[6]int{p.Number,p.Array[0],p.Items[0],p.Index["n"],index["n"],number},Wrapped::Empty=>[6]int{}}
}
`, `package app
import "testing"
func TestContainers(t *testing.T){if got:=Containers();got!=([6]int{1,2,8,9,9,10}){t.Fatal(got)}}
`)
	})

	t.Run("place-evaluated-once", func(t *testing.T) {
		run(t, header+`
var indexes,owners,pointers int
type Holder struct{Items []E;Value E}
func index()int{indexes++;return 0}
func owner(h &mut Holder)&mut Holder{owners++;return h}
func pointer(e &mut E)&mut E{pointers++;return e}
func Places()[6]int{
 indexes=0;owners=0;pointers=0
 h:=Holder{Items:[]E{E::A(1)},Value:E::B(2)}
 a:=match &(*owner(&mut h)).Items[index()]{E::A(p)=>*p,E::B(p)=>*p}
 b:=match &(*owner(&mut h)).Value{E::A(p)=>*p,E::B(p)=>*p}
 c:=match &*pointer(&mut h.Value){E::A(p)=>*p,E::B(p)=>*p}
 return [6]int{a,b+c,indexes,owners,pointers,len(h.Items)}
}
`, `package app
import "testing"
func TestPlaceEvaluation(t *testing.T){if got:=Places();got!=([6]int{1,4,1,2,1,1}){t.Fatal(got)}}
`)
	})

	t.Run("guards-retain-selected-object", func(t *testing.T) {
		run(t, header+`
func Borrow(e &E)&int{match *e{E::A(ref p)=>{return p},E::B(ref p)=>{return p}}}
func SameVariant()[2]int{
 e:=E::A(1);replace:=func()bool{e=E::A(5);return true}
 selected:=match e{E::A(p) if replace()=>p,_=>-1}
 return [2]int{Value(e),selected}
}
func FalseGuardSnapshot()int{
 e:=E::A(1);replace:=func()bool{e=E::B(20);return false}
 return match e{E::B(_)=>-20,E::A(ref p) if replace()=>*p,E::A(v)=>v}
}
func TrueGuardReplacement()[3]int{
 e:=E::A(1);p:=Borrow(&e);replace:=func()bool{e=E::B(20);return true}
 selected:=match e{E::A(ref q) if replace()=>*q,_=>-1}
 return [3]int{Value(e),selected,*p}
}
`, `package app
import "testing"
func TestGuardSnapshots(t *testing.T){if got:=SameVariant();got!=([2]int{5,1}){t.Fatal(got)};if got:=FalseGuardSnapshot();got!=1{t.Fatal(got)};if got:=TrueGuardReplacement();got!=([3]int{20,1,1}){t.Fatal(got)}}
`)
	})

	t.Run("std-option-result", func(t *testing.T) {
		run(t, `package app
func Builtins()[4]int{
 o:=Option[int]::Some(2);before:=o;o=Option[int]::Some(6)
 r:=Result[[]int,string]::Ok([]int{4});copy:=r
 match r{Result::Ok(value)=>{value[0]=8},Result::Err(_)=>{}}
 s:=Result[int,string]::Err("old");s=Result[int,string]::Err("new")
 a:=match o{Option::Some(v)=>v,Option::None=>-1}
 b:=match before{Option::Some(v)=>v,Option::None=>-1}
 c:=match copy{Result::Ok(v)=>v[0],Result::Err(_)=>-1}
 d:=match s{Result::Ok(_)=>0,Result::Err(v)=>len(v)}
 return [4]int{a,b,c,d}
}
`, `package app
import "testing"
func TestStdPayloads(t *testing.T){if got:=Builtins();got!=([4]int{6,2,8,3}){t.Fatal(got)}}
`)
	})

	t.Run("cross-module-native-go-readonly-projection", func(t *testing.T) {
		module := func(name, library string) string {
			text := fmt.Sprintf("module %s\ngo 1.27\n", name)
			if library != "" {
				text += fmt.Sprintf("require borrow.example/library v0.0.0\nreplace borrow.example/library => %s\n", filepath.ToSlash(library))
			}
			return text
		}
		producer := t.TempDir()
		packagesWrite(t, producer, "go.mod", module("borrow.example/library", ""))
		writeAcceptanceFixtureRoots(t, producer, "borrow.example/library", nil)
		packagesWrite(t, producer, "lib.gox", "package library\npub mod model;\npub mod relay;\n")
		packagesWrite(t, producer, "model/model.gox", `package model
pub enum Packet{Empty,A(int),B(int),}
func View(e &Packet)&int{match *e{Packet::A(ref p)=>{return p},Packet::B(ref p)=>{return p},_=>{panic("no integer payload")}}}
`)
		packagesWrite(t, producer, "relay/relay.go", `package relay
import "borrow.example/library/model"
type Alias=model.Packet
func New()Alias{return &model.A{Value:2}}
`)
		library := filepath.Join(t.TempDir(), "library")
		packagesCommand(t, root, cli, true, "transpile", "-o", library, producer)
		consumer := t.TempDir()
		packagesWrite(t, consumer, "go.mod", module("borrow.example/app", library))
		writeAcceptanceSerdeRoots(t, consumer, "borrow.example/library")
		packagesWrite(t, consumer, "app.gox", `package app
use library::model as m;
use library::relay as r;
func Value(e m::Packet)int{return match e{m::Packet::Empty=>0,m::Packet::A(n)=>n,m::Packet::B(n)=>n}}
func Cross()[4]int{
 e:=r::New();before:=e;p:=m::View(&e)
 read:=match e{r::Alias::A(ref q)=>*q,_=>-1};e=m::Packet::B(20)
 return [4]int{read,Value(before),Value(e),*p}
}
`)
		generated := filepath.Join(t.TempDir(), "consumer")
		compileAcceptanceGraph(t, true, consumer, generated, producer)
		packagesWrite(t, generated, "cross_test.go", `package app
import "testing"
func TestCrossPackageModule(t *testing.T){if got:=Cross();got!=([4]int{2,2,20,2}){t.Fatal(got)}}
`)
		plain := t.TempDir()
		packagesWrite(t, plain, "go.mod", module("borrow.example/plain", library))
		packagesWrite(t, plain, "native_test.go", `package plain
import("testing";m "borrow.example/library/model")
func TestOriginalPayloadAddressAndReplacement(t *testing.T){
 var e m.Packet=&m.A{Value:3};p:=&e.(*m.A).Value
 if p!=m.View(&e){t.Fatal("read projection changed the selected payload address")}
 before:=e;e=&m.B{Value:20}
 if *p!=3||before.(*m.A).Value!=3||e.(*m.B).Value!=20{t.Fatal("variable rebinding changed a prior object")}
 // The ordinary Go API intentionally retains its public variant fields.
 *p=7;if before.(*m.A).Value!=7||e.(*m.B).Value!=20{t.Fatal("native field layout changed")}
}
func TestInvalidValuesRemainRejected(t *testing.T){
 for _,bad:=range []m.Packet{nil,(*m.A)(nil)}{func(){defer func(){if recover()==nil{t.Fatal("invalid enum accepted")}}();m.View(&bad)}()}
}
`)
		for _, dir := range []string{library, generated, plain} {
			packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
			packagesCommand(t, dir, tool, true, "vet", "./...")
		}
	})

	for name, body := range map[string]string{
		"explicit-writable-payload": `func Bad(){e:=E::A(1);match e{E::A(ref mut p)=>{*p=2},E::B(_)=>{}}}`,
		"default-writable-payload":  `func Bad(){e:=E::A(1);match &mut e{E::A(p)=>{*p=2},E::B(_)=>{}}}`,
		"writable-option":           `func Bad(){e:=Option[int]::Some(1);match &mut e{Option::Some(p)=>{*p=2},Option::None=>{}}}`,
		"writable-result":           `func Bad(){e:=Result[int,string]::Ok(1);match e{Result::Ok(ref mut p)=>{*p=2},Result::Err(_)=>{}}}`,
		"explicit-readonly-write":   `func Bad(){e:=E::A(1);match e{E::A(ref p)=>{*p=2},E::B(_)=>{}}}`,
		"default-readonly-write":    `func Bad(){e:=E::A(1);match &e{E::A(p)=>{*p=2},E::B(_)=>{}}}`,
		"readonly-upgrade":          `func Bad(){e:=E::A(1);match e{E::A(ref p)=>{w:=&mut *p;*w=2},E::B(_)=>{}}}`,
		"readonly-owner-upgrade":    `func Bad(e &E){match *e{E::A(ref mut p)=>{*p=2},E::B(_)=>{}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(t, header+body+"\n")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			line := strings.Count(header, "\n") + 1
			if !strings.Contains(out, fmt.Sprintf("app.gox:%d:", line)) {
				t.Fatalf("missing original permission-error location: %s", out)
			}
			if !strings.Contains(out, "immutable") && !strings.Contains(out, "read-only") && !strings.Contains(out, "reference") {
				t.Fatalf("expected reference permission rejection: %s", out)
			}
		})
	}
}
