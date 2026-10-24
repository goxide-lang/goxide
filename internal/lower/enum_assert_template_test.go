package lower_test

import (
	"bytes"
	"github.com/goxide-lang/goxide/internal/lower"
	"os"
	"path/filepath"
	"testing"
)

func TestEnumAssertionConditionalForeignPayload(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module carrierfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, source)
	for name, contents := range map[string]string{
		"protocol/equal.gox": `package protocol
pub trait Equal { func equals(self Self, other Self) bool }
pub trait Inspect { func inspect(self Self) bool }
`,
		"library/box.gox": `package library
use carrierfixture::protocol;
pub enum Box[T any] { Empty, Value(T) }
pub func validate[T protocol::Equal](value Box[T]) bool { __goxide_enum_assert(value);return true }
pub func relay[T protocol::Equal](value Box[T]) bool { return validate(value) }
impl[T protocol::Equal] Box[T] {
 pub func checked(self Self) bool { return relay(self) }
 pub func caught(self Self, hit *bool) { __goxide_enum_assert(self);*hit=recover()!=nil }
}
impl[T protocol::Equal] protocol::Inspect for Box[T] { func inspect(self Self)bool {return relay(self)} }

impl[T protocol::Equal] protocol::Equal for Box[T] {
    func equals(self Self, other Self) bool {
        __goxide_enum_assert(self)
        __goxide_enum_assert(other)
        return match self {
            Box::Empty => match other { Box::Empty => true, _ => false },
            Box::Value(a) => match other { Box::Value(b) => protocol::Equal::equals(a,b), _ => false },
        }
    }
}
`,
		"entry/main.gox": `package entry
use carrierfixture::model;
use carrierfixture::library;
use carrierfixture::protocol;
pub func direct(good bool)bool {return library::relay(model::privateBox(good))}
pub func saved(good bool)func()bool {return model::privateBox(good).checked}
pub func view(good bool)protocol::Inspect {return model::privateBox(good)}
pub func cycle()bool {return library::relay(model::cycleBox())}
`,
		"model/value.gox": `package model
use carrierfixture::protocol;
use carrierfixture::library;
pub enum Leaf { Good }
impl protocol::Equal for Leaf { func equals(self Self, other Self) bool { return true } }
pub func compare(value Leaf) bool {
    return protocol::Equal::equals(library::Box[Leaf]::Value(value),library::Box[Leaf]::Empty)
}
pub func direct(value Leaf) bool { return library::Box[Leaf]::Value(value).checked() }
pub func saved(value Leaf) func()bool {
 box:=library::Box[Leaf]::Value(value)
 callback:=box.checked
 box=library::Box[Leaf]::Empty
 return callback
}
pub func view(value Leaf) protocol::Inspect { return library::Box[Leaf]::Value(value) }
pub func caught(value Leaf)(hit bool) {
 box:=library::Box[Leaf]::Value(value)
 callback:=box.caught
 defer callback(&hit)
 panic("conditional business frame")
}
enum hidden { alive, cycle(library::Box[hidden]) }
impl protocol::Equal for hidden {func equals(self Self,other Self)bool{return true}}
pub func privateBox(good bool) library::Box[hidden] {
 var value hidden
 if good {value=hidden::alive}
 return library::Box[hidden]::Value(value)
}
pub func cycleBox()library::Box[hidden] { var nested library::Box[hidden];return library::Box[hidden]::Value(hidden::cycle(nested)) }

`,
	} {
		filename := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	module, err := lower.PrepareModule([]string{source}, "carrierfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{source}, "carrierfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for _, name := range []string{"protocol/equal.go", "library/box.go", "model/value.go", "entry/main.go"} {
		a, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(again, name))
		if err != nil || !bytes.Equal(a, b) {
			t.Fatalf("unstable %s: %v", name, err)
		}
	}
	const consumer = `package main
import("fmt";"carrierfixture/model";"carrierfixture/entry")
func rejects(f func())(yes bool){defer func(){yes=recover()!=nil}();f();return false}
func main(){
 if model.Compare(&model.Good{}){panic("variant comparison")}
 var typed *model.Good
 if !rejects(func(){model.Compare(nil)})||!rejects(func(){model.Compare(typed)}){panic("nested invalid enum escaped")}
 for _,check:=range []func(model.Leaf)bool{model.Direct,func(v model.Leaf)bool{return model.Saved(v)()},func(v model.Leaf)bool{return model.View(v).Inspect()}} {
  if !check(&model.Good{}) {panic("valid conditional check")}
  if !rejects(func(){check(nil)})||!rejects(func(){check(typed)}) {panic("missing transitive check")}
 }
 if !model.Caught(&model.Good{}) {panic("recover lost direct business frame")}
 for _,check:=range []func(bool)bool{entry.Direct,func(g bool)bool{return entry.Saved(g)()},func(g bool)bool{return entry.View(g).Inspect()}} {
  if !check(true)||!rejects(func(){check(false)}) {panic("private typed callback")}
 }
 if !entry.Cycle() {panic("finite recursive path")}
 fmt.Println("conditional enum assertion passed")
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "conditional enum assertion passed\n" {
		t.Fatal(got)
	}
}
