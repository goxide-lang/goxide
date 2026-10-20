package acceptance_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These are compiler acceptance tests: each positive source is transpiled by
// the real CLI and its generated, independent Go module runs race and vet.
// Representation-only studies live separately under tests/research.
func TestEnumPayloadBorrowModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
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
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, input)
		packagesWrite(t, generated, "borrow_test.go", assertions)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, generated, tool, true, "vet", "./...")
	}

	t.Run("explicit-and-default-bindings", func(t *testing.T) {
		run(t, header+`
func Immediate()int{
 e:=E::A(1)
 match e{E::A(ref mut p)=>{*p=7},E::B(_)=>{}}
 return Value(e)
}
func Read()int{
 e:=E::A(3)
 match e{E::A(ref p)=>{return *p},E::B(_)=>{return -1}}
}
func Defaults()int{
 e:=E::A(2)
 match &mut e{E::A(p)=>{*p+=3},E::B(_)=>{}}
 match &e{E::A(p)=>{return *p},E::B(_)=>{return -1}}
}
`, `package app
import "testing"
func TestBindings(t *testing.T){if got:=Immediate();got!=7{t.Fatal(got)};if got:=Read();got!=3{t.Fatal(got)};if got:=Defaults();got!=5{t.Fatal(got)}}
`)
	})

	t.Run("copies-aliases-return-capture", func(t *testing.T) {
		run(t, header+`
func Borrow(e &mut E)&mut int{
 match *e{E::A(ref mut p)=>{return p},E::B(ref mut p)=>{return p}}
}
func Copies()[3]int{
 e:=E::A(1);before:=e;p:=Borrow(&mut e);after:=e;*p=9
 return [3]int{Value(e),Value(before),Value(after)}
}
func Aliases()int{
 e:=E::A(2)
 match e{E::A(ref mut p)=>{q:=p;*q=4;*p+=3},E::B(_)=>{}}
 return Value(e)
}
func Escaped()func()int{
 e:=E::A(10);p:=Borrow(&mut e);saved:=p
 return func()int{*saved+=1;return *p}
}
type Payload struct{Items []int;Index map[string]int}
pub enum Wrapped{Item(Payload),Empty,}
func Shallow()[3]int{
 index:=map[string]int{"n":2};w:=Wrapped::Item(Payload{Items:[]int{1},Index:index});copy:=w
 match w{Wrapped::Item(ref mut p)=>{p.Items[0]=8;p.Index["n"]=9},Wrapped::Empty=>{}}
 match copy{Wrapped::Item(p)=>{return [3]int{p.Items[0],p.Index["n"],index["n"]}},Wrapped::Empty=>{}}
 panic("exhaustive")
}
`, `package app
import("runtime";"testing")
func TestCopies(t *testing.T){if got:=Copies();got!=([3]int{9,9,9}){t.Fatal(got)};if got:=Aliases();got!=7{t.Fatal(got)};if got:=Shallow();got!=([3]int{8,9,9}){t.Fatal(got)}}
func TestEscaped(t *testing.T){next:=Escaped();runtime.GC();if got:=next();got!=11{t.Fatal(got)};runtime.GC();if got:=next();got!=12{t.Fatal(got)}}
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
 match (*owner(&mut h)).Items[index()]{E::A(ref mut p)=>{*p=7},E::B(_)=>{}}
 match (*owner(&mut h)).Value{E::A(_)=>{},E::B(ref mut p)=>{*p=8}}
 match *pointer(&mut h.Value){E::A(_)=>{},E::B(ref mut p)=>{*p+=1}}
 return [6]int{Value(h.Items[0]),Value(h.Value),indexes,owners,pointers,len(h.Items)}
}
`, `package app
import "testing"
func TestPlaceEvaluation(t *testing.T){if got:=Places();got!=([6]int{7,9,1,2,1,1}){t.Fatal(got)}}
`)
	})

	t.Run("guards-and-owner-replacement", func(t *testing.T) {
		run(t, header+`
func Borrow(e &mut E)&mut int{
 match *e{E::A(ref mut p)=>{return p},E::B(ref mut p)=>{return p}}
}
func SameVariant()int{
 e:=E::A(1);replace:=func()bool{e=E::A(5);return true}
 match e{E::A(ref mut p) if replace()=>{*p+=3},_=>{}}
 return Value(e)
}
func DifferentVariant()[5]int{
 e:=E::A(7);p:=Borrow(&mut e);e=E::B(20);zero:=*p;*p=9;activeB:=Value(e)
 e=E::A(5);seen:=*p;*p=6
 return [5]int{zero,activeB,seen,Value(e),*p}
}
func FalseGuardSnapshot()int{
 e:=E::A(1);replace:=func()bool{e=E::B(20);return false}
 match e{E::B(_)=>{return -20},E::A(ref mut p) if replace()=>{return *p},E::A(v)=>{return v}}
}
func writeFalse(p &mut int)bool{*p=5;return false}
func CopyBindingSnapshot()int{
 e:=E::A(1)
 match e{E::A(ref mut p) if writeFalse(p)=>{return -1},E::A(v)=>{return v},E::B(_)=>{return -2}}
}
func TrueGuardReplacement()[2]int{
 e:=E::A(1);p:=Borrow(&mut e);replace:=func()bool{e=E::B(20);return true}
 match e{E::A(ref mut q) if replace()=>{*q=9},_=>{}}
 return [2]int{Value(e),*p}
}
`, `package app
import "testing"
func TestReplacement(t *testing.T){if got:=SameVariant();got!=5{t.Fatal(got)};if got:=DifferentVariant();got!=([5]int{7,20,9,5,6}){t.Fatal(got)}}
func TestGuardSnapshots(t *testing.T){if got:=FalseGuardSnapshot();got!=1{t.Fatal(got)};if got:=CopyBindingSnapshot();got!=1{t.Fatal(got)};if got:=TrueGuardReplacement();got!=([2]int{20,9}){t.Fatal(got)}}
`)
	})

	t.Run("std-option-result", func(t *testing.T) {
		run(t, `package app
func Builtins()[4]int{
 o:=Option[int]::Some(2)
 match &mut o{Option[int]::Some(p)=>{*p+=3},Option[int]::None=>{}}
 before:=o
 match o{Option[int]::Some(ref mut p)=>{*p+=1},Option[int]::None=>{}}
 r:=Result[int,string]::Ok(4)
 match r{Result[int,string]::Ok(ref mut p)=>{*p+=4},Result[int,string]::Err(_)=>{}}
 s:=Result[int,string]::Err("old")
 match &mut s{Result[int,string]::Ok(_)=>{},Result[int,string]::Err(p)=>{*p="new"}}
 a:=match o{Option[int]::Some(v)=>v,Option[int]::None=>-1}
 b:=match before{Option[int]::Some(v)=>v,Option[int]::None=>-1}
 c:=match r{Result[int,string]::Ok(v)=>v,Result[int,string]::Err(_)=>-1}
 d:=match s{Result[int,string]::Ok(_)=>0,Result[int,string]::Err(v)=>len(v)}
 return [4]int{a,b,c,d}
}
`, `package app
import "testing"
func TestStdPayloads(t *testing.T){if got:=Builtins();got!=([4]int{6,5,8,3}){t.Fatal(got)}}
`)
	})

	t.Run("cross-module-derives-and-native-go", func(t *testing.T) {
		module := func(name, library string) string {
			text := fmt.Sprintf("module %s\ngo 1.27\n", name)
			if library != "" {
				text += fmt.Sprintf("require borrow.example/library v0.0.0\nreplace borrow.example/library => %s\n", filepath.ToSlash(library))
			}
			return text
		}
		producer := t.TempDir()
		packagesWrite(t, producer, "go.mod", module("borrow.example/library", ""))
		writeAcceptanceSerdeRoots(t, producer, "")
		packagesWrite(t, producer, "lib.gox", "package library\npub mod model;\npub mod relay;\n")
		packagesWrite(t, producer, "model/model.gox", `package model
use serde;
#[derive(Clone,Debug,PartialEq,Eq,serde::Serialize,serde::Deserialize)]
pub enum Inner{Value(int),}
#[derive(Default,Clone,Debug,PartialEq,Eq,serde::Serialize,serde::Deserialize)]
pub enum Packet{
 #[default]
 Empty,
 A(int),
 B(int),
 Child(Inner),
}
func Edit(e &mut Packet)&mut int{
 match *e{Packet::A(ref mut p)=>{return p},Packet::B(ref mut p)=>{return p},_=>{panic("no integer payload")}}
}
func View(e &Packet)&int{
 match *e{Packet::A(ref p)=>{return p},Packet::B(ref p)=>{return p},_=>{panic("no integer payload")}}
}
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
func Value(e m::Packet)int{return match e{m::Packet::Empty=>0,m::Packet::A(n)=>n,m::Packet::B(n)=>n,m::Packet::Child(_)=>-1}}
func Cross()[5]int{
 e:=r::New();before:=e;p:=m::Edit(&mut e);after:=e;*p=9
 match e{r::Alias::A(ref mut q)=>{*q+=1},_=>{}}
 read:=*m::View(&e);e=m::Packet::B(20);*p=44
 return [5]int{read,Value(before),Value(after),Value(e),*p}
}
`)
		generated := filepath.Join(t.TempDir(), "consumer")
		compileAcceptanceGraph(t, true, consumer, generated, producer)
		packagesWrite(t, generated, "cross_test.go", `package app
import "testing"
func TestCrossPackageModule(t *testing.T){if got:=Cross();got!=([5]int{10,44,44,20,44}){t.Fatal(got)}}
`)
		plain := t.TempDir()
		packagesWrite(t, plain, "go.mod", module("borrow.example/plain", library))
		packagesWrite(t, plain, "native_test.go", `package plain
import(
 "testing"
 m "borrow.example/library/model"
 "github.com/goxide-lang/serde"
 serdejson "github.com/goxide-lang/serde/json"
 sum "github.com/goxide-lang/std/sum"
)
func encode(e m.Packet)([]byte,error){return serdejson.Encode(func(s serde.Serializer)error{return m.SerializePacket(s,&e)})}
func TestOriginalPayloadAddressAndReplacement(t *testing.T){
 var e m.Packet=&m.A{Value:3};p:=&e.(*m.A).Value
 if p!=m.Edit(&e)||p!=m.View(&e){t.Fatal("cross-module reference is not original payload address")}
 before:=e;*p=7;after:=e
 if before.(*m.A).Value!=7||after.(*m.A).Value!=7{t.Fatal("native Go copies must share the selected variant")}
 e=&m.B{Value:20};if *p!=7{t.Fatal("owner replacement changed the detached variant")};*p=99
 if e.(*m.B).Value!=20{t.Fatal("old projection changed the replacement variant")}
 // Ordinary Go compares variant pointers; Goxide equality is an explicit protocol.
 if e==(&m.B{Value:20}){t.Fatal("distinct variant objects compared equal in Go")}
 if !m.EqualsPacket(e,&m.B{Value:20}){t.Fatal("derived equality used pointer identity")}
 e=&m.A{Value:5};if *p!=99||p==&e.(*m.A).Value{t.Fatal("old reference followed owner replacement")}
 *p=8;if e.(*m.A).Value!=5||before.(*m.A).Value!=8{t.Fatal("old reference lost its original variant")}
}
func TestDerivesIgnoreDetachedVariants(t *testing.T){
 var e m.Packet=&m.A{Value:1};p:=&e.(*m.A).Value;e=&m.B{Value:20};*p=99
 clone:=m.ClonePacket(&e)
 if clone.(*m.B).Value!=20||m.DebugPacket(&e)!="Packet::B(20)"{t.Fatal("derive used detached payload")}
 data,err:=encode(e);if err!=nil||string(data)!="{\"B\":20}"{t.Fatalf("wire=%s err=%v",data,err)}
 decoded,err:=serdejson.Unmarshal(data,m.DeserializePacket);if err!=nil||decoded.(*m.B).Value!=20{t.Fatal(decoded,err)}
 empty:=m.DefaultPacket();if _,ok:=empty.(*m.Empty);!ok||m.DebugPacket(&empty)!="Packet::Empty"{t.Fatal("default")}
 var nested m.Packet=&m.Child{Value:&m.Value{Value:12}};data,err=encode(nested);if err!=nil{t.Fatal(err)}
 decoded,err=serdejson.Unmarshal(data,m.DeserializePacket);if err!=nil||decoded.(*m.Child).Value.(*m.Value).Value!=12{t.Fatal(decoded,err)}
}
func TestZeroAndInvalidNestedEnumRemainRejected(t *testing.T){
 var bad m.Packet
 if _,err:=encode(bad);err==nil{t.Fatal("serialized invalid zero enum")}
 for name,run:=range map[string]func(){"match":func(){m.View(&bad)},"clone":func(){_=m.ClonePacket(&bad)},"debug":func(){_=m.DebugPacket(&bad)},"nested":func(){var value m.Packet=&m.Child{Value:nil};_=m.DebugPacket(&value)}}{
  t.Run(name,func(t *testing.T){defer func(){if recover()==nil{t.Fatal("invalid enum accepted")}}();run()})
 }
}
func TestNativeStdSlots(t *testing.T){
 o:=sum.NewSome(1);p:=sum.ProjectSome(&o);copy:=o;*p=4
 if sum.TagOption(o)!=1||sum.ValueSome(o)!=4||sum.ValueSome(copy)!=1{t.Fatal("Option copy/slot")}
 r:=sum.NewOk[int,string](2);ok:=sum.ProjectOk(&r);r=sum.NewErr[int,string]("bad");*ok=99
 if sum.TagResult(r)!=2||sum.ValueErr(r)!="bad"{t.Fatal("Result inactive slot changed active payload")}
 err:=sum.ProjectErr(&r);*err="changed";if sum.ValueErr(r)!="changed"{t.Fatal("Result Err slot")}
}
`)
		for _, dir := range []string{library, generated, plain} {
			packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
			packagesCommand(t, dir, tool, true, "vet", "./...")
		}
	})

	for name, body := range map[string]string{
		"explicit-readonly-write": `func Bad(){e:=E::A(1);match e{E::A(ref p)=>{*p=2},E::B(_)=>{}}}`,
		"default-readonly-write":  `func Bad(){e:=E::A(1);match &e{E::A(p)=>{*p=2},E::B(_)=>{}}}`,
		"readonly-upgrade":        `func Bad(){e:=E::A(1);match e{E::A(ref p)=>{w:=&mut *p;*w=2},E::B(_)=>{}}}`,
		"readonly-owner-upgrade":  `func Bad(e &E){match *e{E::A(ref mut p)=>{*p=2},E::B(_)=>{}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := sourceFor(t, header+body+"\n")
			out := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), source)
			line := strings.Count(header, "\n") + 1
			if !strings.Contains(out, fmt.Sprintf("app.gox:%d:", line)) {
				t.Fatalf("missing original permission-error location: %s", out)
			}
			if !strings.Contains(out, "read-only") && !strings.Contains(out, "read-only") && !strings.Contains(out, "reference") {
				t.Fatalf("expected reference permission rejection: %s", out)
			}
		})
	}
}
