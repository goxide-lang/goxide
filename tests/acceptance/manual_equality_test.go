package acceptance_test

import (
	"fmt"

	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestManualEqualityModules(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	cli := acceptanceCLI(t)
	src := formalDependencyDir(t)
	writeStdFormalManifest(t, src, "manual.example/lib", "library", nil)
	packagesWrite(t, src, "lib.gox", "package library; use std; use std::PartialEq; pub mod model; pub mod relay; \n")

	packagesWrite(t, src, "model/types.gox", `package model; use std; use std::PartialEq;
type Person struct{ID int;Name string}
type Float float64
type Catch struct{}
type Counter int
#[derive(std::derive::PartialEq,std::derive::Eq)]
pub enum Leaf{LeafUnit,}
pub enum Gate{Unit,Child(Leaf),}
#[derive(std::derive::PartialEq,std::derive::Eq)]
pub enum Event{User(Person),Closed,}
#[derive(std::derive::PartialEq)]
type Stable struct{ID int}
#[derive(std::derive::PartialEq,std::derive::Eq)]
type Index map[Stable]int
#[derive(std::derive::PartialEq)]
type OrdinaryFloat struct{Value float64}

impl Leaf {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Event {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Stable {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Index {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl OrdinaryFloat {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}
`)
	packagesWrite(t, src, "model/impl.gox", `package model; use std; use std::PartialEq;
use reflect;
var PersonCalls int
var GateCalls int
var Recovered interface{}
impl std::PartialEq for Person {
 func equals(self Self,other Self)bool{PersonCalls++;return self.ID.equals(other.ID)}
}
impl std::Eq for Person {}
impl std::PartialEq for Float {
 func equals(self Self,other Self)bool{return self==other||(self!=self&&other!=other)}
}
impl std::Eq for Float {}
impl std::PartialEq for Gate {
 func equals(self Self,other Self)bool{std::enum_assert!(self);std::enum_assert!(other);GateCalls++;return true}
}
impl std::Eq for Gate {}
impl std::Eq for Stable {}
impl std::NativeKey for Stable {}
impl std::PartialEq for Catch {
 func equals(self Self,other Self)bool{Recovered=recover();return true}
}
func LocalTypeName()string{type EqualsPerson int;_=(Person{ID:1}).equals(Person{ID:1});return reflect::TypeOf(EqualsPerson(0)).Name()}
func RecoverDirect(){defer (Catch{}).equals(Catch{});panic("manual-recover")}
impl std::PartialEq for Counter {
 func equals(self Self,other Self)bool{if self==0||other==0{return self==other};EqualsCounter:=func(Counter,Counter)bool{return false};_=EqualsCounter;return descend(self-1,other-1)}
}
impl std::Eq for Counter {}
func descend(a,b Counter)bool{return a.equals(b)}
var RecursiveInitial=Counter(3).equals(Counter(3))
var Initial=(Person{ID:1,Name:"first"}).equals(Person{ID:1,Name:"second"})

impl Person {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Float {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Gate {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Catch {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}

impl Counter {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "manual.example/lib/model"
type Person=m.Person
type Event=m.Event
`)
	lib := filepath.Join(t.TempDir(), "lib")
	lib = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", lib, src))
	appsrc := t.TempDir()
	writeStdFormalManifest(t, appsrc, "manual.example/app", "app", map[string]string{"library": src})

	packagesWrite(t, appsrc, "app.gox", `package app; use std; use std::PartialEq;
use library::model as m;use library::relay as r;
#[derive(std::derive::PartialEq,std::derive::Eq)]
type Bundle struct{User r::Person;Event r::Event;Users []m::Person;Index map[string]m::Person}
func Same(a,b Bundle)bool{return a.equals(b)}
func People(a,b r::Person)bool{return a.equals(b)}
func FloatSame(a,b m::Float)bool{return a.equals(b)}
func DefaultFloat(a,b m::OrdinaryFloat)bool{return a.equals(b)}
var Trace string
func Left(a m::Gate)m::Gate{Trace+="L";return a}
func Right(a m::Gate)m::Gate{Trace+="R";return a}
func Ordered(a,b m::Gate)bool{return Left(a).equals(Right(b))}

impl Bundle {
 pub func equal(left Self, right Self) bool { return std::PartialEq::equals(left,right) }
}
`)
	out := filepath.Join(t.TempDir(), "out")
	compileStdFormalGraph(t, true, appsrc, out)
	for _, pair := range [][2]string{{src, lib}, {appsrc, out}} {
		repeated := filepath.Join(t.TempDir(), "repeat")
		if pair[0] == src {
			repeated = strings.TrimSpace(packagesCommand(t, root, cli, true, "transpile", "--lib", "-o", repeated, src))
		} else {
			compileStdFormalGraph(t, true, pair[0], repeated)
		}
		if err := filepath.WalkDir(pair[1], func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(pair[1], path)
			a, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			b, e := os.ReadFile(filepath.Join(repeated, rel))
			if e != nil {
				return e
			}
			if string(a) != string(b) {
				return fmt.Errorf("nondeterministic %s", rel)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	packagesWrite(t, out, "app_test.go", `package app
import("testing";"math";"fmt";"strings";m "manual.example/lib/model")
func TestBusinessEquality(t *testing.T){
 a:=m.Person{ID:7,Name:"old"};b:=m.Person{ID:7,Name:"new"}
 m.PersonCalls=0
 if !m.Initial||!m.RecursiveInitial||!(m.CounterOps{}).Equal(4,4)||(m.CounterOps{}).Equal(4,3)||!People(a,b)||m.PersonCalls!=1{t.Fatal("manual person implementation")}
 x:=Bundle{a,&m.User{Value: a},[]m.Person{a},map[string]m.Person{"a":a}}
 y:=Bundle{b,&m.User{Value: b},[]m.Person{b},map[string]m.Person{"a":b}}
 m.PersonCalls=0;if !Same(x,y)||m.PersonCalls!=4{t.Fatal("derive skipped manual protocol",m.PersonCalls)}
 y.User.ID=8;if Same(x,y){t.Fatal("different IDs")}
 n:=math.NaN();if !FloatSame(m.Float(n),m.Float(n))||DefaultFloat(m.OrdinaryFloat{Value:n},m.OrdinaryFloat{Value:n}){t.Fatal("explicit NaN wrapper changed default floats")}
 if m.LocalTypeName()!="EqualsPerson"{t.Fatalf("local type identity changed: %q",m.LocalTypeName())}
 m.RecoverDirect();if m.Recovered!="manual-recover"{t.Fatal("manual body acquired another frame",m.Recovered)}
}
func TestManualEnumValidation(t *testing.T){
 for _,bad:=range []m.Gate{nil}{
  for _,right:=range []bool{false,true}{func(){m.GateCalls=0;Trace="";defer func(){if recover()==nil||m.GateCalls!=0||Trace!="LR"{t.Fatal("manual enum boundary",m.GateCalls,Trace)}}();if right{Ordered(&m.Unit{},bad)}else{Ordered(bad,&m.Unit{})}}()}
 }
 m.GateCalls=0;Trace="";if !Ordered(&m.Unit{},&m.Child{Value: &m.LeafUnit{}})||m.GateCalls!=1||Trace!="LR"{t.Fatal("valid enum manual body")}
 func(){defer func(){p:=recover();if p==nil||!strings.Contains(fmt.Sprint(p),"Leaf"){t.Fatal("left validator must run first",p)}}();Ordered(&m.Child{Value: nil},nil)}()
}
`)
	packagesWrite(t, lib, "model/model_test.go", `package model
import "testing"
func TestNativeConsistentDerivedKeys(t *testing.T){if !(IndexOps{}).Equal(Index{{ID:1}:4},Index{{ID:1}:4}){t.Fatal("manual Eq does not replace structural key proof")}}
`)
	plain := t.TempDir()
	writeStdNativeConsumerManifest(t, plain, "manual.example/plain", lib)
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";m "manual.example/lib/model")
func TestPublicManualHelpers(t *testing.T){if !(m.PersonOps{}).Equal(m.Person{ID:1,Name:"a"},m.Person{ID:1,Name:"b"}){t.Fatal("Go helper")};m.GateCalls=0;defer func(){if recover()==nil||m.GateCalls!=0{t.Fatal("Go helper bypassed enum validation")}}();(m.GateOps{}).Equal(nil,&m.Unit{})}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-mod=mod", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for name, code := range map[string]string{
		"missing-partial":        `type T struct{};impl std::Eq for T {}`,
		"missing-self":           `type T struct{};impl std::PartialEq for T {func equals(a T,b T)bool{return true}}`,
		"wrong-parameter":        `type T struct{};impl std::PartialEq for T {func equals(self Self,other int)bool{return true}}`,
		"wrong-result":           `type T struct{};impl std::PartialEq for T {func equals(self Self,other Self)int{return 1}}`,
		"static":                 `type T struct{};impl std::PartialEq for T {static func equals(self Self,other Self)bool{return true}}`,
		"empty-partial":          `type T struct{};impl std::PartialEq for T {}`,
		"eq-member":              `type T struct{};impl std::Eq for T {func equals(self Self,other Self)bool{return true}}`,
		"extra-member":           `type T struct{};impl std::PartialEq for T {func equals(self Self,other Self)bool{return true};func extra(){}}`,
		"alias-target":           `type T=struct{};impl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}`,
		"body-type":              `type T struct{};impl std::PartialEq for T {func equals(self Self,other Self)bool{return 7}}`,
		"duplicate-partial":      `type T struct{};impl std::PartialEq for T {func equals(self Self,other Self)bool{return true}};impl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}`,
		"manual-derived-partial": "#[derive(std::derive::PartialEq)]\ntype T struct{}\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}",
		"manual-derived-eq":      "#[derive(std::derive::Eq)]\ntype T struct{}\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}",
		"duplicate-eq":           `type T struct{};impl std::PartialEq for T {func equals(self Self,other Self)bool{return true}};impl std::Eq for T {};impl std::Eq for T {}`,
		"manual-native-key":      "type T struct{ID int}\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}\nimpl std::Eq for T {}\n#[derive(std::derive::PartialEq,std::derive::Eq)]\ntype Index map[T]int",
		"wrapped-manual-key":     "type T int\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}\nimpl std::Eq for T {}\n#[derive(std::derive::PartialEq,std::derive::Eq)]\ntype K struct{Value T}\n#[derive(std::derive::PartialEq,std::derive::Eq)]\ntype Index map[K]int",
		"imported-manual-key":    "use library::model as m;\n#[derive(std::derive::PartialEq,std::derive::Eq)]\ntype Index map[m::Person]int",
		"partial-only-field":     "type T struct{}\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}\n#[derive(std::derive::PartialEq,std::derive::Eq)]\ntype Outer struct{Value T}",
	} {
		t.Run(name, func(t *testing.T) {
			bad := t.TempDir()
			writeStdFormalManifest(t, bad, "manual.example/bad", "bad", map[string]string{"library": src})

			packagesWrite(t, bad, "bad.gox", "package bad; use std; use std::PartialEq;\n"+code)
			compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "out"))
		})
	}
	t.Run("duplicate-across-files", func(t *testing.T) {
		bad := t.TempDir()
		writeStdFormalManifest(t, bad, "manual.example/bad", "bad", map[string]string{"library": src})

		packagesWrite(t, bad, "type.gox", "package bad; use std; use std::PartialEq;\ntype T struct{}")
		for _, n := range []string{"a.gox", "b.gox"} {
			packagesWrite(t, bad, n, "package bad; use std; use std::PartialEq;\nimpl std::PartialEq for T {func equals(self Self,other Self)bool{return true}}\n")
		}
		msg := compileStdFormalGraph(t, false, bad, filepath.Join(t.TempDir(), "out"))
		if !strings.Contains(msg, "duplicate") {
			t.Fatal(msg)
		}
	})
}
