package frontend

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestInlineEnumBorrowPipeline(t *testing.T) {
	dir := sharedStdModule(t, "example.org/borrows")
	sharedStdWrite(t, dir, "value.gox", `package borrows
pub enum Value { Number(int), Other(int), Empty, }
func Slot(owner &mut Value)&mut int {
 match &mut *owner { Value::Number(p)=>{return p}, _=>{panic("wrong variant")}, }
 panic("unreachable")
}
func Read(owner &Value)int {
 return match &*owner {Value::Number(p)=>*p,Value::Other(p)=>*p,Value::Empty=>0,}
}
func Explicit(owner &mut Value)int {
 match *owner {Value::Number(ref mut p)=>{*p+=1},_=>{},}
 return Read(&*owner)
}
func CopyBinding()int {
 value:=Value::Number(3)
 match value {Value::Number(n)=>{n=7;_=n},_=>{},}
 return Read(&value)
}
`)
	out := filepath.Join(t.TempDir(), "generated")
	if _, err := CompileModule(dir, out); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, out, "value_test.go", `package borrows
import("testing";"runtime")
func TestSlots(t *testing.T){
 v:=NewValueNumber(1);copy:=v;p:=Slot(&v);*p=2
 if Read(&v)!=2||Read(&copy)!=1||CopyBinding()!=3{t.Fatal("copy or projection")}
 v=NewValueNumber(5);if *p!=5{t.Fatal("same-variant replacement lost slot")}
 v=NewValueOther(8);if *p!=0{t.Fatal("inactive slot was not reset")};*p=11
 if Read(&v)!=8{t.Fatal("old slot retargeted to new variant")}
 v=NewValueNumber(13);if *p!=13||Explicit(&v)!=14{t.Fatal("ABA or auto mode")}
 escaped:=func()*int{local:=NewValueNumber(21);return Slot(&local)}();runtime.GC();*escaped=22;if *escaped!=22{t.Fatal("returned slot lifetime")}
}
`)
	sharedStdGo(t, out, "test", "-race", "./...")
	sharedStdGo(t, out, "vet", "./...")
}

func TestInlineEnumBorrowReadonlyBoundary(t *testing.T) {
	for _, body := range []string{
		`func Bad(v &E){match &*v{E::Item(ref mut p)=>{*p=2},}}`,
		`func Bad(v &E){match &*v{E::Item(p)=>{*p=2},}}`,
	} {
		_, err := Compile("readonly.gox", []byte("package p\nenum E{Item(int),}\n"+body))
		if err == nil || !(strings.Contains(err.Error(), "read") || strings.Contains(err.Error(), "write") || strings.Contains(err.Error(), "ref mut")) {
			t.Fatalf("readonly enum projection accepted: %v", err)
		}
	}
}

func TestInlineEnumBorrowCrossModule(t *testing.T) {
	producer := sharedStdModule(t, "example.org/borrowproducer")
	sharedStdWrite(t, producer, "value.gox", `package producer
pub enum Value { Number(int), Empty, }
func Slot(v &mut Value)&mut int {match &mut *v{Value::Number(p)=>{return p},Value::Empty=>{panic("empty")},};panic("unreachable")}
`)
	library := filepath.Join(t.TempDir(), "library")
	if _, err := CompileModule(producer, library); err != nil {
		t.Fatal(err)
	}
	consumer := sharedStdModule(t, "example.org/borrowconsumer")
	sharedStdWrite(t, consumer, "go.mod", "module example.org/borrowconsumer\ngo 1.27\nrequire example.org/borrowproducer v0.0.0\nreplace example.org/borrowproducer => "+filepath.ToSlash(library)+"\n")
	sharedStdWrite(t, consumer, "read.gox", `package consumer
use producer as p;
func Change(v &mut p::Value)int {match &mut *v{p::Value::Number(n)=>{*n+=4},_=>{},};return *p::Slot(v)}
`)
	out := filepath.Join(t.TempDir(), "consumer")
	if _, err := CompileModuleWithOptions(consumer, out, ModuleOptions{Roots: frontendFixtureRoots(map[string]string{"producer": "example.org/borrowproducer"})}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, out, "read_test.go", `package consumer
import("testing";p "example.org/borrowproducer")
func TestIndependent(t *testing.T){value:=p.NewValueNumber(6);copy:=value;if Change(&value)!=10||*p.Slot(&value)!=10||*p.Slot(&copy)!=6{t.Fatal("cross-module slot or copy")}}
`)
	sharedStdGo(t, out, "test", "-race", "./...")
	sharedStdGo(t, out, "vet", "./...")
}

func TestInlineSumErrorReferenceAcrossModules(t *testing.T) {
	producer := sharedStdModule(t, "example.org/errorproducer")
	sharedStdWrite(t, producer, "value.gox", `package producer
func Slot(v &mut Result[int,error])&mut error {
 match &mut *v {Result::Err(p)=>{return p},Result::Ok(_)=>{panic("ok")},};panic("unreachable")
}
func View(v &Result[int,error])&error {
 match &*v {Result::Err(p)=>{return p},Result::Ok(_)=>{panic("ok")},};panic("unreachable")
}
`)
	library := filepath.Join(t.TempDir(), "library")
	if _, err := CompileModule(producer, library); err != nil {
		t.Fatal(err)
	}
	consumer := sharedStdModule(t, "example.org/errorconsumer")
	sharedStdWrite(t, consumer, "go.mod", "module example.org/errorconsumer\ngo 1.27\nrequire example.org/errorproducer v0.0.0\nreplace example.org/errorproducer => "+filepath.ToSlash(library)+"\n")
	roots := frontendFixtureRoots(map[string]string{"producer": "example.org/errorproducer"})
	sharedStdWrite(t, consumer, "change.gox", `package consumer
use producer as p;
func Change(v &mut Result[int,error], next error){slot:=p::Slot(v);*slot=next}
func Read(v &Result[int,error])&error{return p::View(v)}
`)
	out := filepath.Join(t.TempDir(), "consumer")
	if _, err := CompileModuleWithOptions(consumer, out, ModuleOptions{Roots: roots}); err != nil {
		t.Fatal(err)
	}
	sharedStdWrite(t, out, "change_test.go", `package consumer
import("testing";"errors";s "github.com/goxide-lang/std/sum")
func TestErrorSlot(t *testing.T){old,next:=errors.New("old"),errors.New("next");v:=s.NewErr[int,error](old);copy:=v;Change(&v,next);if *Read(&v)!=next||s.ValueErr(copy)!=old{t.Fatal("error slot identity or copy")}}
`)
	sharedStdGo(t, out, "test", "-race", "./...")
	sharedStdGo(t, out, "vet", "./...")
	sharedStdWrite(t, consumer, "erase.gox", `package consumer
use producer as p;
func Erase(v &Result[int,error])error{return *p::View(v)}
`)
	if _, err := CompileModuleWithOptions(consumer, filepath.Join(t.TempDir(), "blocked"), ModuleOptions{Roots: roots}); err == nil {
		t.Fatal("readonly error reference erased across module boundary")
	} else if !strings.Contains(err.Error(), "erase.gox:3:") || strings.Contains(err.Error(), "generated/unmapped") {
		t.Fatalf("imported readonly rejection lost its copied source origin: %v", err)
	}
}
