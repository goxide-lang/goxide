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
	cli := filepath.Join(t.TempDir(), "goxide")
	packagesCommand(t, root, tool, true, "build", "-o", cli, "./cmd/goxide")
	mod := func(name, lib string) string {
		text := fmt.Sprintf("module %s\ngo 1.27\n", name)
		if lib != "" {
			text += fmt.Sprintf("require manual.example/lib v0.0.0\nreplace manual.example/lib => %s\n", filepath.ToSlash(lib))
		}
		return text
	}
	src := t.TempDir()
	packagesWrite(t, src, "go.mod", mod("manual.example/lib", ""))
	packagesWrite(t, src, "model/types.gox", `package model
type Person struct{ID int;Name string}
type Float float64
type Catch struct{}
type Counter int
#[derive(PartialEq,Eq)]
pub enum Leaf{Unit,}
pub enum Gate{Unit,Child(Leaf),}
#[derive(PartialEq,Eq)]
pub enum Event{User(Person),Closed,}
#[derive(PartialEq)]
type Stable struct{ID int}
#[derive(PartialEq,Eq)]
type Index map[Stable]int
#[derive(PartialEq)]
type OrdinaryFloat struct{Value float64}
`)
	packagesWrite(t, src, "model/impl.gox", `package model
import "reflect"
var PersonCalls int
var GateCalls int
var Recovered interface{}
impl PartialEq for Person {
 func equals(self Person,other Person)bool{PersonCalls++;return self.ID.equals(other.ID)}
}
impl Eq for Person {}
impl PartialEq for Float {
 func equals(self Float,other Float)bool{return self==other||(self!=self&&other!=other)}
}
impl Eq for Float {}
impl PartialEq for Gate {
 func equals(self Gate,other Gate)bool{GateCalls++;return true}
}
impl Eq for Gate {}
impl Eq for Stable {}
impl PartialEq for Catch {
 func equals(self Catch,other Catch)bool{Recovered=recover();return true}
}
func LocalTypeName()string{type EqualsPerson int;_=(Person{ID:1}).equals(Person{ID:1});return reflect.TypeOf(EqualsPerson(0)).Name()}
func RecoverDirect(){defer (Catch{}).equals(Catch{});panic("manual-recover")}
impl PartialEq for Counter {
 func equals(self Counter,other Counter)bool{if self==0||other==0{return self==other};EqualsCounter:=func(Counter,Counter)bool{return false};_=EqualsCounter;return descend(self-1,other-1)}
}
impl Eq for Counter {}
func descend(a,b Counter)bool{return a.equals(b)}
var RecursiveInitial=Counter(3).equals(Counter(3))
var Initial=(Person{ID:1,Name:"first"}).equals(Person{ID:1,Name:"second"})
`)
	packagesWrite(t, src, "relay/relay.go", `package relay
import m "manual.example/lib/model"
type Person=m.Person
type Event=m.Event
`)
	lib := filepath.Join(t.TempDir(), "lib")
	packagesCommand(t, root, cli, true, "transpile", "-o", lib, src)
	appsrc := t.TempDir()
	packagesWrite(t, appsrc, "go.mod", mod("manual.example/app", lib))
	packagesWrite(t, appsrc, "app.gox", `package app
import(m "manual.example/lib/model";r "manual.example/lib/relay")
#[derive(PartialEq,Eq)]
type Bundle struct{User r.Person;Event r.Event;Users []m.Person;Index map[string]m.Person}
func Same(a,b Bundle)bool{return a.equals(b)}
func People(a,b r.Person)bool{return a.equals(b)}
func FloatSame(a,b m.Float)bool{return a.equals(b)}
func DefaultFloat(a,b m.OrdinaryFloat)bool{return a.equals(b)}
var Trace string
func Left(a m.Gate)m.Gate{Trace+="L";return a}
func Right(a m.Gate)m.Gate{Trace+="R";return a}
func Ordered(a,b m.Gate)bool{return Left(a).equals(Right(b))}
`)
	out := filepath.Join(t.TempDir(), "out")
	packagesCommand(t, root, cli, true, "transpile", "-o", out, appsrc)
	for _, pair := range [][2]string{{src, lib}, {appsrc, out}} {
		repeated := filepath.Join(t.TempDir(), "repeat")
		packagesCommand(t, root, cli, true, "transpile", "-o", repeated, pair[0])
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
 if !m.Initial||!m.RecursiveInitial||!m.EqualsCounter(4,4)||m.EqualsCounter(4,3)||!People(a,b)||m.PersonCalls!=1{t.Fatal("manual person implementation")}
 x:=Bundle{a,m.NewEventUser(a),[]m.Person{a},map[string]m.Person{"a":a}}
 y:=Bundle{b,m.NewEventUser(b),[]m.Person{b},map[string]m.Person{"a":b}}
 m.PersonCalls=0;if !Same(x,y)||m.PersonCalls!=4{t.Fatal("derive skipped manual protocol",m.PersonCalls)}
 y.User.ID=8;if Same(x,y){t.Fatal("different IDs")}
 n:=math.NaN();if !FloatSame(m.Float(n),m.Float(n))||DefaultFloat(m.OrdinaryFloat{Value:n},m.OrdinaryFloat{Value:n}){t.Fatal("explicit NaN wrapper changed default floats")}
 if m.LocalTypeName()!="EqualsPerson"{t.Fatal("local type identity changed")}
 m.RecoverDirect();if m.Recovered!="manual-recover"{t.Fatal("manual body acquired another frame",m.Recovered)}
}
type forged struct{m.Gate}
func TestManualEnumValidation(t *testing.T){
 var typed *m.GoxideCase4_Gate_Unit
 for _,bad:=range []m.Gate{nil,typed,&m.GoxideCase4_Gate_Unit{},forged{}}{
  for _,right:=range []bool{false,true}{func(){m.GateCalls=0;Trace="";defer func(){if recover()==nil||m.GateCalls!=0||Trace!="LR"{t.Fatal("manual enum boundary",m.GateCalls,Trace)}}();if right{Ordered(m.NewGateUnit(),bad)}else{Ordered(bad,m.NewGateUnit())}}()}
 }
 m.GateCalls=0;Trace="";if !Ordered(m.NewGateUnit(),m.NewGateChild(m.NewLeafUnit()))||m.GateCalls!=1||Trace!="LR"{t.Fatal("valid enum manual body")}
 func(){defer func(){p:=recover();if p==nil||!strings.Contains(fmt.Sprint(p),"Leaf"){t.Fatal("left validator must run first",p)}}();Ordered(m.NewGateChild(nil),nil)}()
}
`)
	packagesWrite(t, lib, "model/model_test.go", `package model
import "testing"
func TestNativeConsistentDerivedKeys(t *testing.T){if !EqualsIndex(Index{{ID:1}:4},Index{{ID:1}:4}){t.Fatal("manual Eq does not replace structural key proof")}}
`)
	plain := t.TempDir()
	packagesWrite(t, plain, "go.mod", mod("manual.example/plain", lib))
	packagesWrite(t, plain, "plain_test.go", `package plain
import("testing";m "manual.example/lib/model")
func TestPublicManualHelpers(t *testing.T){if !m.EqualsPerson(m.Person{ID:1,Name:"a"},m.Person{ID:1,Name:"b"}){t.Fatal("Go helper")};m.GateCalls=0;defer func(){if recover()==nil||m.GateCalls!=0{t.Fatal("Go helper bypassed enum validation")}}();m.EqualsGate(nil,m.NewGateUnit())}
`)
	for _, dir := range []string{lib, out, plain} {
		packagesCommand(t, dir, tool, true, "test", "-race", "-count=1", "-v", "./...")
		packagesCommand(t, dir, tool, true, "vet", "./...")
	}
	for name, code := range map[string]string{
		"missing-partial":        `type T struct{};impl Eq for T {}`,
		"missing-self":           `type T struct{};impl PartialEq for T {func equals(a T,b T)bool{return true}}`,
		"wrong-parameter":        `type T struct{};impl PartialEq for T {func equals(self T,other int)bool{return true}}`,
		"wrong-result":           `type T struct{};impl PartialEq for T {func equals(self T,other T)int{return 1}}`,
		"static":                 `type T struct{};impl PartialEq for T {static func equals(self T,other T)bool{return true}}`,
		"empty-partial":          `type T struct{};impl PartialEq for T {}`,
		"eq-member":              `type T struct{};impl Eq for T {func equals(self T,other T)bool{return true}}`,
		"extra-member":           `type T struct{};impl PartialEq for T {func equals(self T,other T)bool{return true};func extra(){}}`,
		"alias-target":           `type T=struct{};impl PartialEq for T {func equals(self T,other T)bool{return true}}`,
		"body-type":              `type T struct{};impl PartialEq for T {func equals(self T,other T)bool{return 7}}`,
		"helper-collision":       `type T struct{};func EqualsT(a,b T)bool{return true};impl PartialEq for T {func equals(self T,other T)bool{return true}}`,
		"manual-derived-partial": "#[derive(PartialEq)]\ntype T struct{}\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}",
		"manual-derived-eq":      "#[derive(Eq)]\ntype T struct{}\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}",
		"duplicate-eq":           `type T struct{};impl PartialEq for T {func equals(self T,other T)bool{return true}};impl Eq for T {};impl Eq for T {}`,
		"manual-native-key":      "type T struct{ID int}\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}\nimpl Eq for T {}\n#[derive(PartialEq,Eq)]\ntype Index map[T]int",
		"wrapped-manual-key":     "type T int\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}\nimpl Eq for T {}\n#[derive(PartialEq,Eq)]\ntype K struct{Value T}\n#[derive(PartialEq,Eq)]\ntype Index map[K]int",
		"imported-manual-key":    "import m \"manual.example/lib/model\"\n#[derive(PartialEq,Eq)]\ntype Index map[m.Person]int",
		"partial-only-field":     "type T struct{}\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}\n#[derive(PartialEq,Eq)]\ntype Outer struct{Value T}",
	} {
		t.Run(name, func(t *testing.T) {
			bad := t.TempDir()
			packagesWrite(t, bad, "go.mod", mod("manual.example/bad", lib))
			packagesWrite(t, bad, "bad.gox", "package bad\n"+code)
			packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), bad)
		})
	}
	t.Run("duplicate-across-files", func(t *testing.T) {
		bad := t.TempDir()
		packagesWrite(t, bad, "go.mod", mod("manual.example/bad", lib))
		packagesWrite(t, bad, "type.gox", "package bad\ntype T struct{}")
		for _, n := range []string{"a.gox", "b.gox"} {
			packagesWrite(t, bad, n, "package bad\nimpl PartialEq for T {func equals(self T,other T)bool{return true}}")
		}
		msg := packagesCommand(t, root, cli, false, "transpile", "-o", filepath.Join(t.TempDir(), "out"), bad)
		if !strings.Contains(msg, "duplicate") {
			t.Fatal(msg)
		}
	})
}
