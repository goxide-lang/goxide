package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestEnumValidNativeBoundaries(t *testing.T) {
	source := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(source, "go.mod"), []byte("module enumvalidfixture\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"model/model.gox": `package model
pub enum Event[T any] { Unit, Payload(T), Wrapped(Event[T]) }
pub enum Empty {}
enum hidden { hiddenUnit }
pub var HiddenCalls int
pub func Hidden() hidden { HiddenCalls++; return hidden::hiddenUnit }
enum secret[T any] { secretValue(T) }
type opaque struct { V int }
pub func Secret[T any](value T) secret[T] { return secret[T]::secretValue(value) }
pub func SecretOpaque() secret[opaque] { return secret[opaque]::secretValue(opaque{V: 8}) }
pub func PublicOpaque() Event[opaque] { return Event[opaque]::Payload(opaque{V: 9}) }
pub func PrivateOption() Option[opaque] { return Option[opaque]::Some(opaque{V: 10}) }
pub func PrivateResult() Result[opaque, opaque] { return Result[opaque, opaque]::Ok(opaque{V: 11}) }
pub type Alias = Event[int]
pub func Valid[T any](value Event[T]) bool { return enum_valid(value) }
trait Probe { func probe() int }
impl Probe for int { func probe() int { return 7 } }
func boundValid[T Probe](value Event[T]) bool { return enum_valid(value) }
pub func BoundValid(value Event[int]) bool { return boundValid[int](value) }
pub macro_rules! valid { ($value:expr) => { enum_valid($value) }; }
pub func Aliased(value Alias) bool { return enum_valid(value) }
pub func EmptyValid(value Empty) bool { return enum_valid(value) }
pub func Pointer(value *Event[int]) bool { return value != nil && enum_valid(*value) }
pub func Once(next func() Event[int]) bool { return enum_valid(next()) }
pub func OptionValid[T any](value Option[T]) bool { return enum_valid(value) }
pub func ResultValid[T, E any](value Result[T, E]) bool { return enum_valid(value) }
pub var ShadowValue any
pub func Shadowed(value Event[int]) bool {
    type bool int
    var local bool = 1
    nil := 2
    true := 3
    false := 4
    ShadowValue = local; _ = nil; _ = true; _ = false
    return enum_valid(value)
}

pub func LocalShadow() int {
    enum_valid := func(value int) int { return value + 1 }
    return enum_valid(4)
}
pub func Rematch(value, replacement Event[int]) int {
    if enum_valid(value) { value = replacement }
    return match value { _ => 1 }
}
`,
		"bridge/bridge.gox": `package bridge
use enumvalidfixture::model;
pub func Valid[T any](value model::Event[T]) bool { return enum_valid(value) }
pub func PrivateFactory() bool { return enum_valid(model::Hidden()) }
pub func PrivateGeneric() bool { return enum_valid(model::Secret(1)) && enum_valid(model::Secret("text")) && enum_valid(model::SecretOpaque()) && enum_valid(model::PublicOpaque()) && enum_valid(model::PrivateOption()) && enum_valid(model::PrivateResult()) }
`,
		"shadow/shadow.gox": `package shadow
use enumvalidfixture::model;
func enum_valid(value model::Event[int]) bool { panic("caller function executed") }
pub func MacroValid(value model::Event[int]) bool { return model::valid!(value) }
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
	module, err := lower.PrepareModule([]string{source}, "enumvalidfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{source}, "enumvalidfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for _, name := range []string{"model/model.go", "bridge/bridge.go", "shadow/shadow.go"} {
		first, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("enum_valid generation differs for %s: %v", name, err)
		}
		for _, forbidden := range []string{"reflect.", "recover(", "ValidateEvent"} {
			if strings.Contains(string(first), forbidden) {
				t.Fatalf("enum_valid generated unexpected %s in %s", forbidden, name)
			}
		}
	}
	const privateConsumer = `package model
// Embedding deliberately supplies the sealed marker without being a known variant.
type unknownHidden struct { hidden }
type unknownSecret struct { secret[opaque] }
func PrivateBoundaries() bool {
    return !GoxideInternalEnumValid_hidden(nil) &&
        !GoxideInternalEnumValid_hidden((*hiddenUnit)(nil)) &&
        !GoxideInternalEnumValid_hidden(unknownHidden{}) &&
        !GoxideInternalEnumValid_secret[opaque](nil) &&
        !GoxideInternalEnumValid_secret((*secretValue[opaque])(nil)) &&
        !GoxideInternalEnumValid_secret(unknownSecret{}) &&
        GoxideInternalEnumValid_secret(&secretValue[opaque]{Value: opaque{V: 1}})
}
`
	if err := os.WriteFile(filepath.Join(module, "model/private_consumer.go"), []byte(privateConsumer), 0600); err != nil {
		t.Fatal(err)
	}
	bridge, err := os.ReadFile(filepath.Join(module, "bridge/bridge.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"model.hidden", "model.secret", "model.opaque"} {
		if strings.Contains(string(bridge), private) {
			t.Fatalf("caller names private type %s:\n%s", private, bridge)
		}
	}
	const consumer = `package main
import (
    "fmt"
    "strings"
    "enumvalidfixture/model"
    "enumvalidfixture/bridge"
    "enumvalidfixture/shadow"
    "github.com/goxide-lang/std/sum"
)
type unknown struct { model.Event[int] }
type emptyUnknown struct { model.Empty }
func require(ok bool, label string) { if !ok { panic(label) } }
func main() {
    for _, value := range []model.Event[int]{nil, (*model.Unit[int])(nil), (*model.Payload[int])(nil), unknown{}, (*unknown)(nil)} {
        require(!model.Valid(value) && !bridge.Valid(value) && !model.Aliased(value) && !model.Shadowed(value) && !model.BoundValid(value) && !shadow.MacroValid(value), "invalid outer value")
    }
    for _, value := range []model.Event[int]{&model.Unit[int]{}, &model.Payload[int]{Value: 7}, &model.Wrapped[int]{Value: nil}} {
        require(model.Valid(value) && bridge.Valid(value) && model.Aliased(value) && model.Shadowed(value) && model.BoundValid(value) && shadow.MacroValid(value), "valid outer value")
        require(model.Pointer(&value), "pointer dereference")
    }
    require(!model.Pointer(nil), "nil pointer guard")
    require(!model.EmptyValid(nil) && !model.EmptyValid(emptyUnknown{}), "empty enum")
    require(bridge.PrivateFactory() && model.HiddenCalls == 1, "private enum factory evaluated once")
    require(bridge.PrivateGeneric() && model.PrivateBoundaries(), "private generic enum boundaries")
    count := 0
    require(!model.Once(func() model.Event[int] { count++; return nil }) && count == 1, "single evaluation")
    require(model.Valid(&model.Payload[string]{Value: "text"}) && bridge.Valid(&model.Payload[string]{Value: "text"}), "generic enum")
    require(model.OptionValid(sum.None[*int]()) && model.OptionValid(sum.NewSome[*int](nil)), "Option outer values")
    require(model.ResultValid(sum.NewOk[*int, *string](nil)) && model.ResultValid(sum.NewErr[*int, *string](nil)), "Result outer values")
    var invalidResult sum.Result[*int, *string]
    require(!model.ResultValid(invalidResult), "invalid Result tag")
    require(model.LocalShadow() == 5 && fmt.Sprint(model.ShadowValue) == "1", "builtin shadowing")
    message := ""
    func() { defer func() { message = fmt.Sprint(recover()) }(); model.Rematch(&model.Unit[int]{}, nil) }()
    require(strings.Contains(message, "match on nil Event"), "match validation retained")
    fmt.Println("enum validity boundaries passed")
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "enum validity boundaries passed\n" {
		t.Fatalf("enum validity output = %q", got)
	}
}

func TestEnumValidHelperAvoidsLocalName(t *testing.T) {
	source := t.TempDir()
	for name, contents := range map[string]string{
		"opaque/opaque.gox": `package opaque
type hidden struct { Value int }
pub func Value() hidden { return hidden{Value: 1} }
`,
		"main.gox": `package main
use enumvalidshadow::opaque;
pub enum Event[T any] { Payload(T) }
func Wrap[T any](value T) Event[T] { return Event[T]::Payload(value) }
func main() {
    GoxideInternalEnumValid_Event := 7
    println(enum_valid(Wrap(opaque::Value())), GoxideInternalEnumValid_Event)
}
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
	module, err := lower.PrepareModule([]string{source}, "enumvalidshadow", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "true 7\n" {
		t.Fatalf("enum helper local shadowing output = %q", got)
	}
}

func TestEnumValidPreservesDirectRecover(t *testing.T) {
	source := t.TempDir()
	const program = `package main
func option() {
    defer func() { println(enum_valid(recover().(Option[int]))) }()
    panic(Option[int]::Some(3))
}
func result() {
    defer func() { println(enum_valid(recover().(Result[int,string]))) }()
    panic(Result[int,string]::Ok(3))
}
func main() { option(); result() }
`
	if err := os.WriteFile(filepath.Join(source, "main.gox"), []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	module, err := lower.PrepareModule([]string{source}, "enumvalidrecover", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "true\ntrue\n" {
		t.Fatalf("enum validity changed direct recover: %q", got)
	}
}
