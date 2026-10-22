package compile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// Every route checks the same still-generic library before any concrete caller
// exists. The Go consumer later exercises distinct defined int and string types.
func TestGenericCoherenceSourceAndWASIMacroParity(t *testing.T) {
	sdk := compilerSDKForTest(t)
	standard := pinnedStandardManifest(t)
	for _, route := range []string{"source", "declarative", "function", "attribute", "derive"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			macros, protocol, model := filepath.Join(root, "macros"), filepath.Join(root, "protocol"), filepath.Join(root, "model")
			copyFixture := func(directory, target, name string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join("testdata", "generic_coherence", name))
				if err != nil {
					t.Fatal(err)
				}
				writeProjectFile(t, directory, target, string(data))
			}
			writeProcBuildManifest(t, macros, sdk, "")
			copyFixture(macros, "lib.gox", "macros.gox")
			copyFixture(macros, "macros.go", "macros.go")
			writeProjectFile(t, protocol, project.ManifestName, `[go_mod]
module="example.org/coherence-protocol"
go="1.27"
[lib]
name="api"
path="."
`+standard)
			copyFixture(protocol, "lib.gox", "protocol.gox")
			writeProjectFile(t, model, project.ManifestName, `[go_mod]
module="example.org/coherence-model"
go="1.27"
[lib]
name="model"
path="."
[features]
conflict=[]
[dependencies.api]
path="../protocol"
[dependencies.macros]
path="../macros"
`+standard)
			writeProjectFile(t, model, "lib.gox", genericCoherenceModel(route))
			calls := map[pm.Kind]int{}
			run := testWASI(t, func(request pm.Request) { calls[request.Kind()]++ })
			library, err := PrepareProject(model, Options{Lib: true, Offline: true, RunProcMacro: run})
			if err != nil {
				t.Fatalf("generic library before concrete callers: %v", err)
			}
			configuredGo(t, library, "build", "-mod=readonly", library.Entry)
			if kind := map[string]pm.Kind{"function": pm.Function, "attribute": pm.Attribute, "derive": pm.Derive}[route]; kind != "" {
				if len(calls) != 1 || calls[kind] != 2 {
					t.Fatalf("WASI executions before concrete callers = %v; want two %s macros", calls, kind)
				}
			} else if len(calls) != 0 {
				t.Fatalf("source route invoked procedural macros: %v", calls)
			}
			// A different feature plan must recheck the same symbolic heads. No
			// int/string instantiation can rescue these intersecting declarations.
			rejected, err := PrepareProject(model, Options{Lib: true, Offline: true, Features: []string{"conflict"}, RunProcMacro: run})
			var diagnostic check.Diagnostic
			if rejected != nil || !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid || !strings.Contains(diagnostic.Message, "overlap") {
				t.Fatalf("overlapping generic output accepted or misdiagnosed: %v, %v", rejected, err)
			}
			if route != "source" && len(diagnostic.Origin.Stack) == 0 {
				t.Fatalf("macro overlap lost its expansion origin: %+v", diagnostic)
			}
			if !strings.Contains(diagnostic.Origin.File, "coherence-model/lib.gox") {
				t.Fatalf("overlap does not point to the consuming source: %+v", diagnostic)
			}

			business, consumer := filepath.Join(root, "business"), filepath.Join(root, "consumer")
			writeProjectFile(t, business, project.ManifestName, `[go_mod]
module="example.org/coherence-business"
go="1.27"
[lib]
name="business"
path="."
[dependencies.model]
path="../model"
`+standard)
			writeProjectFile(t, business, "lib.gox", genericCoherenceBusiness)
			writeProjectFile(t, consumer, project.ManifestName, `[go_mod]
module="example.org/coherence-consumer"
go="1.27"
[[bin]]
name="consumer"
path="."
[dependencies.business]
path="../business"
[dependencies.model]
path="../model"
`)
			writeProjectFile(t, consumer, "main.go", `package main
import (
 "fmt"
 business "example.org/coherence-business"
 model "example.org/coherence-model"
)
func verify() {
 if got:=business.Verify();got!="number 2 text 102 number 3 text 101" {panic(got)}
 unused:=model.Packet[func()]{Values: []func(){func(){}}}
 unused.Values[0]()
 if _,ok:=any(unused).(interface{Label()string});ok {panic("conditional implementation escaped into every Go method set")}
}
func main(){verify();fmt.Println(business.Verify())}
`)
			writeProjectFile(t, consumer, "main_test.go", "package main\nimport \"testing\"\nfunc TestGenericBusiness(t *testing.T){verify()}\n")
			first, err := PrepareProject(consumer, Options{Offline: true, RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			configuredGo(t, first, "build", "-mod=readonly", "-o", filepath.Join(root, "consumer-bin"), first.Entry)
			if output := configuredGo(t, first, "run", "-mod=readonly", first.Entry); output != "number 2 text 102 number 3 text 101\n" {
				t.Fatalf("generic business output = %q", output)
			}
			configuredGo(t, first, "test", "-mod=readonly", "-count=1", first.Entry)
			before := projectSnapshot(t, filepath.Dir(first.Root))
			lock := unchangedConfigLock(t, consumer, nil)
			second, err := PrepareProject(consumer, Options{Offline: true, Locked: true, RunProcMacro: run})
			if err != nil {
				t.Fatal(err)
			}
			if first.Root != second.Root || !reflect.DeepEqual(before, projectSnapshot(t, filepath.Dir(second.Root))) {
				t.Fatal("locked replay changed generic macro output or selected implementations")
			}
			unchangedConfigLock(t, consumer, lock)
		})
	}
}

func pinnedStandardManifest(t *testing.T) string {
	t.Helper()
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, dependency := range module.Require {
		if dependency.Path == "github.com/goxide-lang/std" {
			return fmt.Sprintf("[dependencies.standard]\ngo=\"github.com/goxide-lang/std\"\nversion=%q\n", dependency.Version)
		}
	}
	t.Fatal("compiler go.mod must pin std")
	return ""
}

func genericCoherenceModel(route string) string {
	var source strings.Builder
	source.WriteString(`package model
use api;
use api::Source;
use macros;
pub trait Batch { type Item; func first(self Self) Self::Item; func size(self Self) int }
`)
	for _, item := range []struct{ name, declaration string }{
		{"Packet", "pub struct Packet[T any] { pub Values []T }"},
		{"Event", "pub enum Event[T any] { Data([]T), Empty }"},
	} {
		for _, conflict := range []bool{false, true} {
			predicate := `not(feature("conflict"))`
			if conflict {
				predicate = `feature("conflict")`
			}
			attribute := "#[cfg(" + predicate + ")]\n"
			switch route {
			case "source":
				source.WriteString(attribute + item.declaration + "\n")
				for _, implementation := range genericCoherenceImpls(item.name, conflict) {
					source.WriteString(attribute + implementation + "\n")
				}
			case "declarative":
				declarations := append([]string{item.declaration}, genericCoherenceImpls(item.name, conflict)...)
				for index, declaration := range declarations {
					name := fmt.Sprintf("define%s%t%d", item.name, conflict, index)
					source.WriteString("macro_rules! " + name + " { () => { " + declaration + " }; }\n")
					source.WriteString(attribute + name + "!();\n")
				}
			case "function":
				name := "classify"
				if conflict {
					name = "overlap"
				}
				source.WriteString(attribute + "macros::" + name + "!{" + item.declaration + "}\n")
			case "attribute":
				name := "classifyAttribute"
				if conflict {
					name = "overlapAttribute"
				}
				source.WriteString(attribute + "#[macros::" + name + "]\n" + item.declaration + "\n")
			case "derive":
				name := "Classify"
				if conflict {
					name = "Overlap"
				}
				source.WriteString(attribute + "#[derive(macros::" + name + ")]\n" + item.declaration + "\n")
			}
		}
	}
	source.WriteString(`pub func Describe[T api::Source](value T) string { return value.label() }
pub func Values[T api::Source](value T) []T::api::Source::Item { return value.values() }
pub func First[T Batch](value T) T::Batch::Item { return value.first() }
pub func Count[T Batch](value T) int { return value.size() }
pub func PacketLabel[T interface {~int|~string}](value Packet[T]) string { return value.label() }
pub func EventValues[T interface {~int|~string}](value Event[T]) []T { return value.values() }
pub func PacketCount[T interface {~int|~string},const N int](value [N]Packet[T]) int { return value.size() }
`)
	return source.String()
}

func genericCoherenceImpls(name string, overlapping bool) []string {
	values := "return self.Values"
	if name == "Event" {
		values = "return match self { Self::Data(values) => values, Self::Empty => nil }"
	}
	var declarations []string
	for _, branch := range []struct{ constraint, label, size string }{{"~int", "number", "N"}, {"~string", "text", "N+100"}} {
		if overlapping {
			branch.constraint = "~int"
		}
		declarations = append(declarations,
			fmt.Sprintf(`impl[T %s] api::Source for %s[T] { type Item = T; func values(self Self) []Self::Item { %s }; func label(self Self) string { return %q } }`, branch.constraint, name, values, branch.label),
			fmt.Sprintf(`impl[T %s, const N int] Batch for [N]%s[T] { type Item = %s[T]; func first(self Self) Self::Item { return self[0] }; func size(self Self) int { return %s } }`, branch.constraint, name, name, branch.size),
		)
	}
	return declarations
}

const genericCoherenceBusiness = `package business
use model;
use fmt;
type number int
type text string
pub func Verify() string {
 numbers:=model::Packet[number]{Values:[]number{3,5}}
 words:=model::Packet[text]{Values:[]text{"a","b"}}
 numericBatch:=[2]model::Packet[number]{numbers,numbers}
 textBatch:=[2]model::Packet[text]{words,words}
 if model::Values(numbers)[0]!=3 || model::Values(model::First(numericBatch))[1]!=5 {panic("numeric associated output")}
 if model::Values(words)[0]!="a" || model::Values(model::First(textBatch))[1]!="b" {panic("text associated output")}
 shared:=model::Values(numbers)
 shared[0]=11
 if numbers.Values[0]!=11 {panic("ordinary slice sharing changed")}
 numericEvent:=model::Event[number]::Data([]number{7,9})
 textEvent:=model::Event[text]::Data([]text{"x","y"})
 numericEvents:=[3]model::Event[number]{numericEvent,numericEvent,numericEvent}
 textEvents:=[1]model::Event[text]{textEvent}
 if model::Values(model::First(numericEvents))[1]!=9 || model::Values(model::First(textEvents))[0]!="x" {panic("enum or array associated output")}
 if model::Values(model::Event[text]::Empty)!=nil {panic("empty enum nested slice")}
 if model::PacketLabel(numbers)!="number" || model::PacketLabel(words)!="text" {panic("generic conditional label")}
 if model::EventValues(numericEvent)[0]!=7 || model::EventValues(textEvent)[0]!="x" {panic("generic conditional values")}
 if model::PacketCount(numericBatch)!=2 || model::PacketCount(textBatch)!=102 {panic("generic conditional const array")}
 return fmt::Sprintf("%s %d %s %d %s %d %s %d",model::Describe(numbers),model::Count(numericBatch),model::Describe(words),model::Count(textBatch),model::Describe(numericEvent),model::Count(numericEvents),model::Describe(textEvent),model::Count(textEvents))
}
`
