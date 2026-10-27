package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestEnumAssertionFiniteWalkAndPrivateFields(t *testing.T) {
	source := t.TempDir()
	for name, contents := range map[string]string{
		"model/model.gox": `package model
pub enum Child { Good }
pub struct Record { value Child; unused int }
pub enum Hidden { private(Record) }
pub func Wrap(value Child) Record { return Record{value:value} }
pub func Hide(value Child) Hidden { return Hidden::private(Wrap(value)) }
pub enum Recursive { Many([]Recursive), Through(Back) }
pub struct Back { value Recursive }
pub enum Wrapped { WrappedBack(Back) }
pub func BackValue(value Recursive) Back { return Back{value:value} }
`,
		"entry/entry.gox": `package entry
use enumassertfixture::model;
pub enum Envelope { Value(model::Record), Values([]model::Record), Table(map[string]model::Record), Fixed([1]model::Record), Indirect(*model::Record), Dynamic(any) }
pub func Check(value Envelope) { __goxide_enum_assert(value) }
pub func CheckHidden(value model::Hidden) { __goxide_enum_assert(value) }
pub func CheckRecursive(value model::Recursive) { __goxide_enum_assert(value) }
pub func CheckWrapped(value model::Wrapped) { __goxide_enum_assert(value) }
pub func Once(next func() Envelope) { __goxide_enum_assert(next()) }
pub func Frame() { defer func(){ __goxide_enum_assert(recover().(Envelope)) }(); panic(Envelope::Value(model::Wrap(model::Child::Good))) }
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
	module, err := lower.PrepareModule([]string{source}, "enumassertfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{source}, "enumassertfixture", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for _, name := range []string{"model/model.go", "entry/entry.go"} {
		first, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("enum assertion generation differs for %s: %v", name, err)
		}
	}
	const consumer = `package main
import("fmt"; "enumassertfixture/entry"; "enumassertfixture/model")
func require(value bool) { if !value { panic("assertion failed") } }
func fails(f func()) (result bool) { defer func(){result=recover()!=nil}(); f(); return }
func main(){
 good:=model.Wrap(&model.Good{}); bad:=model.Wrap(nil)
 entry.Check(&entry.Value{Value:good})
 for _, value:=range []entry.Envelope{nil,(*entry.Value)(nil),&entry.Value{Value:bad},&entry.Values{Value:[]model.Record{bad}},&entry.Table{Value:map[string]model.Record{"x":bad}},&entry.Fixed{Value:[1]model.Record{bad}}} { require(fails(func(){entry.Check(value)})) }
 require(fails(func(){entry.CheckHidden(model.Hide(nil))}))
 entry.CheckHidden(model.Hide(&model.Good{}))
 entry.Check(&entry.Indirect{Value:&bad});entry.Check(&entry.Dynamic{Value:bad})
 entry.CheckRecursive(&model.Many{Value:[]model.Recursive{nil}})
 entry.CheckRecursive(&model.Through{Value:model.BackValue(nil)})
 require(fails(func(){entry.CheckWrapped(&model.WrappedBack{Value:model.BackValue(nil)})}))
 entry.CheckWrapped(&model.WrappedBack{Value:model.BackValue(&model.Through{Value:model.BackValue(nil)})})
 calls:=0;entry.Once(func()entry.Envelope{calls++;return &entry.Value{Value:good}});require(calls==1)
 entry.Frame()
 fmt.Println("finite enum assertions passed")
}

`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "finite enum assertions passed\n" {
		t.Fatalf("enum assertion output = %q", got)
	}
}

func TestEnumAssertionPrivateGenericArguments(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module enumassertgeneric\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, source)
	for name, contents := range map[string]string{
		"model/model.gox": `package model
pub enum Box[T any] { private(T), Public(T) }
pub struct Record[T any] { value T; unused int }
pub func Wrap[T any](value T) Box[T] { return Box[T]::private(value) }
pub func RecordOf[T any](value T) Record[T] { return Record[T]{value:value} }
pub enum PublicBox[T any] { PublicValue(T) }
pub func PublicOf[T any](value T) PublicBox[T] { return PublicBox[T]::PublicValue(value) }
enum secret[T any] { secretValue(T) }
pub func Secret[T any](value T) secret[T] { return secret[T]::secretValue(value) }
pub const GoxideInternalEnumAssert_Box = 7
`,
		"leaf/leaf.gox": `package leaf
use enumassertgeneric::model;
enum hidden { validValue, cycle(model::Box[hidden]) }
type key int
func value(good bool) hidden { if good { return hidden::validValue }; var bad hidden; return bad }
pub func Hidden(good bool) hidden { return value(good) }
pub func Cycle() model::Box[hidden] { return model::Wrap(BareCycle()) }
pub func BareCycle() hidden { var bad model::Box[hidden]; return hidden::cycle(bad) }
pub func Box(good bool) model::Box[hidden] { return model::Wrap(value(good)) }
pub func Nested(good bool) model::Box[model::Box[hidden]] { return model::Wrap(Box(good)) }
pub func Record(good bool) model::Box[model::Record[hidden]] { return model::Wrap(model::RecordOf(value(good))) }
pub func Table(good bool) model::Box[map[key]hidden] { return model::Wrap(map[key]hidden{key(1):value(good)}) }
pub func Slice(good bool) model::Box[[]hidden] { return model::Wrap([]hidden{value(good)}) }
pub func Array(good bool) model::Box[[1]hidden] { return model::Wrap([1]hidden{value(good)}) }
pub func Public(good bool) model::PublicBox[hidden] { return model::PublicOf(value(good)) }
pub func GetOption(good bool) Option[hidden] { return Option[hidden]::Some(value(good)) }
pub func GetResult(good bool) Result[hidden,string] { return Result[hidden,string]::Ok(value(good)) }
`,
		"entry/entry.gox": `package entry
use enumassertgeneric::leaf;
use enumassertgeneric::model;
pub func Box(good bool) { __goxide_enum_assert(leaf::Box(good)) }
pub func Nested(good bool) { __goxide_enum_assert(leaf::Nested(good)) }
pub func Record(good bool) { __goxide_enum_assert(leaf::Record(good)) }
pub func Table(good bool) { __goxide_enum_assert(leaf::Table(good)) }
pub func Slice(good bool) { __goxide_enum_assert(leaf::Slice(good)) }
pub func Array(good bool) { __goxide_enum_assert(leaf::Array(good)) }
pub func Public(good bool) { __goxide_enum_assert(leaf::Public(good)) }
pub func CheckOption(good bool) { __goxide_enum_assert(leaf::GetOption(good)) }
pub func CheckResult(good bool) { __goxide_enum_assert(leaf::GetResult(good)) }
pub func Secret(good bool) { __goxide_enum_assert(model::Secret(leaf::Hidden(good))) }
pub func SecretBox(good bool) { __goxide_enum_assert(model::Wrap(model::Secret(leaf::Hidden(good)))) }
pub func Cycle() { __goxide_enum_assert(leaf::Cycle()) }
pub func BareCycle() { __goxide_enum_assert(leaf::BareCycle()) }
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
	module, err := lower.PrepareModule([]string{source}, "enumassertgeneric", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(module) })
	again, err := lower.PrepareModule([]string{source}, "enumassertgeneric", testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(again) })
	for _, name := range []string{"model/model.go", "leaf/leaf.go", "entry/entry.go"} {
		first, err := os.ReadFile(filepath.Join(module, name))
		if err != nil {
			t.Fatal(err)
		}
		second, err := os.ReadFile(filepath.Join(again, name))
		if err != nil || !bytes.Equal(first, second) {
			t.Fatalf("private generic assertion generation differs for %s: %v", name, err)
		}
		if name != "leaf/leaf.go" && (strings.Contains(string(first), "leaf.hidden") || strings.Contains(string(first), "leaf.key")) {
			t.Fatalf("private argument leaked into %s:\n%s", name, first)
		}
		if name == "model/model.go" && strings.Contains(string(first), `"enumassertgeneric/leaf"`) {
			t.Fatalf("generic owner imports its consumer:\n%s", first)
		}
	}
	const consumer = `package main
import("fmt"; "enumassertgeneric/entry")
func fails(f func()) (result bool) { defer func(){result=recover()!=nil}(); f(); return }
func main(){
 for _, check:=range []func(bool){entry.Box,entry.Nested,entry.Record,entry.Table,entry.Slice,entry.Array,entry.Public,entry.CheckOption,entry.CheckResult,entry.Secret,entry.SecretBox} {
  check(true)
  if !fails(func(){check(false)}) { panic("private generic payload was not validated") }
 }
 entry.Cycle()
 if !fails(entry.BareCycle) { panic("helper forgot its concrete containing path") }
 fmt.Println("private generic enum assertions passed")
}
`
	if err := os.WriteFile(filepath.Join(module, "main.go"), []byte(consumer), 0600); err != nil {
		t.Fatal(err)
	}
	pinP4Dependencies(t, module)
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "private generic enum assertions passed\n" {
		t.Fatalf("private generic enum assertion output = %q", got)
	}
}

func TestEnumAssertionConstantArrayAndUndeterminedPayload(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
enum Leaf { Live }
enum Box[T any] { Stored(T) }
func Check[const N int](value Box[[N]Leaf]) { __goxide_enum_assert(value) }
func fails(value Box[[2]Leaf]) (result bool) { defer func(){ result = recover() != nil }(); Check[2](value); return }
func main() {
    Check[0](Box[[0]Leaf]::Stored([0]Leaf{}))
    Check[2](Box[[2]Leaf]::Stored([2]Leaf{Leaf::Live,Leaf::Live}))
    fmt::Println(fails(Box[[2]Leaf]::Stored([2]Leaf{})))
}
`}, "true\n")
	for _, source := range []string{
		"enum Box[T any]{Stored(T)}; func check[T any](value Box[T]){ __goxide_enum_assert(value) }",
		"enum Box[T any]{Stored(T)}; func check[T any,const N int](value Box[[N]T]){ __goxide_enum_assert(value) }; func main(){check[int,1](Box[[1]int]::Stored([1]int{}))}",
	} {
		_, err := lowerDeriveSources(map[string]string{"main.gox": "package main\n" + source})
		if err == nil || !strings.Contains(err.Error(), "enum_assert requires statically determined payload types") {
			t.Fatalf("undetermined enum payload accepted: %v", err)
		}
	}
}

func TestEnumAssertionPrivateScalarNeedsNoSource(t *testing.T) {
	sources := map[string]string{
		"go.mod": "module associatedfixture\n\ngo 1.27\n",
		"native/native.go": `package native
type scalar int
func Scalar() scalar { return 3 }
type reader struct{ bytes []byte }
func (reader) Read([]byte) (int,error) { panic("business method must not execute") }
func Reader() reader { return reader{} }
`,
		"main.gox": `package main
use fmt;
use io;
use associatedfixture::native;
enum Box[T any] { Payload(T) }
enum ReaderBox[T io::Reader] { ReaderPayload(T) }
func Wrap[T any](value T) Box[T] { return Box[T]::Payload(value) }
func WrapReader[T io::Reader](value T) ReaderBox[T] { return ReaderBox[T]::ReaderPayload(value) }
func WrapOption[T any](value T) Option[T] { return Option[T]::Some(value) }
func main() {
    __goxide_enum_assert(Wrap(native::Scalar()))
    __goxide_enum_assert(WrapReader(native::Reader()))
    __goxide_enum_assert(WrapOption(native::Scalar()))
    fmt::Println("private scalar arguments passed")
}
`,
	}
	var previous []byte
	for attempt := 0; attempt < 2; attempt++ {
		directory := t.TempDir()
		for name, source := range sources {
			writeDiagnosticModuleFile(t, directory, name, []byte(source))
		}
		pinP4Dependencies(t, directory)
		if err := lower.CompileModules([]analysis.Module{{Path: "associatedfixture", Dir: directory, Config: testConfig(t)}}); err != nil {
			t.Fatal(err)
		}
		output, err := os.ReadFile(filepath.Join(directory, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if attempt != 0 && !bytes.Equal(previous, output) {
			t.Fatalf("same private scalar input generated different Go:\n%s\n%s", previous, output)
		}
		previous = output
		if strings.Contains(string(output), "native.scalar") || strings.Contains(string(output), "native.reader") {
			t.Fatalf("native private argument leaked into generated Go:\n%s", output)
		}
		unchanged, err := os.ReadFile(filepath.Join(directory, "native/native.go"))
		if err != nil || string(unchanged) != sources["native/native.go"] {
			t.Fatalf("native Go changed: %q, %v", unchanged, err)
		}
		if attempt == 0 {
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != "private scalar arguments passed\n" {
				t.Fatalf("private scalar argument output = %q", got)
			}
		}
	}
}
