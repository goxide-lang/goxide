package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

func TestTraitTemplatePrivateAssociatedType(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Source { type Item; func get(self Self) Self::Item }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Source;
pub func Read[T Source](value T) T::Source::Item { return value.get() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Source;
struct secret { N int }
pub struct Cell { pub N int }
impl Source for Cell { type Item = secret; func get(self Self) Self::Item { return secret{N:self.N} } }
pub func Extract(value secret) int { return value.N }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(model::Extract(ops::Read(model::Cell{N:42}))) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}

func TestTraitTemplatePrivateAssociatedTypeWithoutMethod(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Source { type Item }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Source;
pub func Zero[T Source]() T::Source::Item { var result T::Source::Item; return result }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Source;
struct secret { N int }
pub struct Cell {}
impl Source for Cell { type Item = secret }
pub func Extract(value secret) int { return value.N }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(model::Extract(ops::Zero[model::Cell]())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "0\n")
}

func runPrivateAssociatedFixture(t *testing.T, sources map[string]string, want string) {
	t.Helper()
	input := t.TempDir()
	for name, source := range sources {
		path := filepath.Join(input, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	module, err := lower.PrepareModule([]string{input}, "associatedfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{input}, "associatedfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for name := range sources {
		name = strings.TrimSuffix(name, ".gox") + ".go"
		first, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("same input changed %s", name)
		}
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTraitTemplateInferredPrivateTypeArgument(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Read[T Value](value T) int { return value.value() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func Make() secret { return secret{N:42} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { value:=model::Make(); fmt::Println(ops::Read(value)) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}
func TestTraitTemplateInferredPrivateUnboundedTypeArgument(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Pair[A any, T Value](left A, right T) (A, int) { return left, right.value() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
pub struct Cell { pub N int }
impl Value for Cell { func value(self Self) int { return self.N } }
pub func Make() secret { return secret{N:7} }
pub func Extract(value secret) int { return value.N }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { value:=model::Make(); left,count:=ops::Pair(value, model::Cell{N:42});fmt::Println(model::Extract(left),count) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "7 42\n")
}

func TestTraitTemplateInferredPrivateSliceElement(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
impl[T Value] Value for []T {
    func value(self Self) int {
        total := 0
        for _, value := range self { total += value.value() }
        return total
    }
}
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Read[T Value](value T) int { return value.value() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func MakeSlice() []secret { return []secret{{N:40},{N:2}} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(ops::Read(model::MakeSlice())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}

func TestTraitTemplatePrivateInferencePreservesCallSemantics(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
impl[T Value] Value for []T {
    func value(self Self) int {
        total := 0
        for _, value := range self { total += value.value() }
        return total
    }
}
`,
		"ops/lib.gox": `package ops
use fmt;
use associatedfixture::protocol::Value;
pub func Pair[A any, T Value](left A, right T, replacement A) (A, int) {
    peek := func() A { return left }
    left, count := replacement, right.value()
    return peek(), count
}
pub func Catch[T Value](value T) { fmt::Println(recover() != nil, value.value()) }
`,
		"model/lib.gox": `package model
use fmt;
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func Make(label string, value int) secret { fmt::Print(label); return secret{N:value} }
pub func MakeSlice(label string) []secret { fmt::Print(label); return []secret{{N:40},{N:2}} }
pub func Extract(value secret) int { return value.N }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() {
    left, count := ops::Pair(model::Make("A",7), model::MakeSlice("B"), model::Make("C",9))
    fmt::Println(model::Extract(left), count)
    defer ops::Catch(model::MakeSlice("D"))
    panic("caught")
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "ABC9 42\nDtrue 42\n")
}

func TestTraitTemplatePrivateBlanketImplArgument(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
pub struct Pair[A any, T any] { pub Left A; pub Right T }
impl[A any, T Value] Value for Pair[A,T] {
    func value(self Self) int { return self.Right.value() }
}
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Read[T Value](value T) int { return value.value() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol;
struct hidden { N int }
struct secret { N int }
impl protocol::Value for secret { func value(self Self) int { return self.N } }
pub func MakePair() protocol::Pair[hidden,secret] {
    return protocol::Pair[hidden,secret]{Left:hidden{N:7},Right:secret{N:42}}
}
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(ops::Read(model::MakePair())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}

func TestTraitTemplatePrivateCompositeWhereArgument(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
impl[T Value] Value for []T {
    func value(self Self) int {
        total := 0
        for _, value := range self { total += value.value() }
        return total
    }
}
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Read[A any](value A) int where []A: Value { return Value::value([]A{value}) }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func Make() secret { return secret{N:42} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(ops::Read(model::Make())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}

func TestTraitTemplatePrivateMarkerAndStaticEvidence(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Marker {}
pub trait Code { func code() int }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol;
pub func Read[T any](value T) int where T: protocol::Marker + protocol::Code { return T::code() }
`,
		"model/lib.gox": `package model
use associatedfixture::protocol;
struct secret {}
impl protocol::Marker for secret {}
impl protocol::Code for secret { func code() int { return 42 } }
pub func Make() secret { return secret{} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() { fmt::Println(ops::Read(model::Make())) }
`,
	}
	runPrivateAssociatedFixture(t, sources, "42\n")
}

func TestTraitTemplatePrivateGenericValues(t *testing.T) {
	for _, test := range []struct {
		name, body string
	}{
		{"method-value", `values:=model::MakeSlice(); read:=values.value; fmt::Println(read())`},
		{"runtime-view", `var view Value = model::MakeSlice(); fmt::Println(view.value())`},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := map[string]string{
				"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
impl[T Value] Value for []T {
    func value(self Self) int {
        total := 0
        for _, value := range self { total += value.value() }
        return total
    }
}
`,
				"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func MakeSlice() []secret { return []secret{{N:40},{N:2}} }
`,
				"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::protocol::Value;
func main() { ` + test.body + ` }
`,
			}
			runPrivateAssociatedFixture(t, sources, "42\n")
		})
	}
}

func TestTraitTemplatePrivateVariadicArguments(t *testing.T) {
	sources := map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Value { func value(self Self) int }
`,
		"ops/lib.gox": `package ops
use associatedfixture::protocol::Value;
pub func Total[T Value](values ...T) int {
    total := 0
    for _, value := range values { total += value.value() }
    return total
}
`,
		"model/lib.gox": `package model
use associatedfixture::protocol::Value;
struct secret { N int }
impl Value for secret { func value(self Self) int { return self.N } }
pub func Make(n int) secret { return secret{N:n} }
pub func MakeSlice() []secret { return []secret{{N:20},{N:22}} }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::model;
use associatedfixture::ops;
func main() {
    fmt::Println(ops::Total(model::Make(20), model::Make(22)), ops::Total(model::MakeSlice()...))
}
`,
	}
	runPrivateAssociatedFixture(t, sources, "42 42\n")
}
