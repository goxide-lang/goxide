package lower_test

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestTraitApplicationAssignmentRetainsReferenceInputs(t *testing.T) {
	inputs := []string{"&int", "&mut int", "*int"}
	for _, source := range inputs {
		for _, destination := range inputs {
			t.Run(source+"_to_"+destination, func(t *testing.T) {
				program := fmt.Sprintf(`package main
trait Tag[T any] { func value(self Self) int }
func convert(value Tag[%s]) Tag[%s] { return value }
`, source, destination)
				_, err := lowerDeriveSources(map[string]string{"main.gox": program})
				if source == destination && err != nil {
					t.Fatal(err)
				}
				if source != destination && err == nil {
					t.Fatal("trait application assignment erased its reference input")
				}
			})
		}
	}
}

func TestTraitApplicationRejectsReferenceInputVariance(t *testing.T) {
	const source = `package main
trait Sink[T any] { func set(self Self, value T) }
struct Writer {}
impl Sink[&mut int] for Writer { func set(self Self, value &mut int) { *value = 9 } }
func readonlySink(value Sink[&mut int]) Sink[&int] { return value }
func main() {
    var writer Sink[&mut int] = Writer{}
    value := 1
    readonlySink(writer).set(&value)
}
`
	generated, err := lowerDeriveSources(map[string]string{"main.gox": source})
	if err == nil {
		t.Fatalf("mutable-input trait accepted as readonly-input trait:\n%s", generated["main.gox"])
	}
}

func TestTraitRuntimeApplicationRetainsPhantomInputs(t *testing.T) {
	const source = `package library
pub trait Phantom[T any] { func value(self Self) int }
pub trait Read[T any] { type Item; func get(self Self) Self::Item }
pub struct Number { pub N int }
impl Phantom[int] for Number { func value(self Self) int { return self.N } }
impl Phantom[string] for Number { func value(self Self) int { return self.N + 100 } }
impl Read[int] for Number { type Item = string; func get(self Self) string { return "first" } }
impl Read[string] for Number { type Item = int; func get(self Self) int { return self.N } }
pub func intView(value Number) Phantom[int] { return value }
pub func stringView(value Number) Phantom[string] { return value }
pub func textView(value Number) Read[int, Item=string] { return value }
pub func numberView(value Number) Read[string, Item=int] { return value }
`
	const name = "applicationfixture/library/library.gox"
	generated, err := lowerDeriveSources(map[string]string{name: source})
	if err != nil {
		t.Fatal(err)
	}
	again, err := lowerDeriveSources(map[string]string{name: source})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated[name], again[name]) {
		t.Fatal("same trait applications changed generated Go")
	}
	module := t.TempDir()
	for file, contents := range map[string][]byte{
		"go.mod":             []byte("module applicationfixture\n\ngo 1.27\n"),
		"library/library.go": generated[name],
		"main.go": []byte(`package main
import ("fmt"; "applicationfixture/library")
func main() {
 value := library.Number{N:7}
 first, second := library.IntView(value), library.StringView(value)
 text, number := library.TextView(value), library.NumberView(value)
 saved := first.Value
 value.N = 9
 _, wrongInput := any(first).(library.Phantom[string])
 _, wrongRead := any(text).(library.Read[string, string])
 _, originalTrait := any(value).(library.Phantom[int])
 fmt.Println(first.Value(), second.Value(), saved(), text.Get(), number.Get(), wrongInput, wrongRead, originalTrait)
}
`),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(module, file)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := testdeps.WriteFile(filepath.Join(module, file), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "7 107 7 first 7 false false false\n" {
		t.Fatalf("trait application identity/value capture = %q", got)
	}
}

func TestTraitPairOperationNameStableAcrossApplications(t *testing.T) {
	const declaration = `package library
pub trait From[S any] { func from(value S) Self }
pub type Number int
`
	const integer = `impl From[int] for Number { func from(value int) Self { return Self(value + 1) } }
`
	const text = `impl From[string] for Number { func from(value string) Self { return Self(len(value)) } }
`
	const name = "applicationfixture/library/library.gox"
	first, err := lowerDeriveSources(map[string]string{name: declaration + integer})
	if err != nil {
		t.Fatal(err)
	}
	second, err := lowerDeriveSources(map[string]string{name: declaration + text + integer})
	if err != nil {
		t.Fatal(err)
	}
	operation := func(data []byte, input string) string {
		t.Helper()
		file, err := parser.ParseFile(token.NewFileSet(), "library.go", data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			method, ok := declaration.(*goast.FuncDecl)
			if !ok || method.Recv == nil || method.Name.Name != "From" || len(method.Type.Params.List) != 1 {
				continue
			}
			parameter, ok := method.Type.Params.List[0].Type.(*goast.Ident)
			if !ok || parameter.Name != input {
				continue
			}
			if receiver, ok := method.Recv.List[0].Type.(*goast.Ident); ok {
				return receiver.Name
			}
		}
		t.Fatalf("missing concrete %s pair operation:\n%s", input, data)
		return ""
	}
	var relocatedName string
	for range 2 {
		path := filepath.Join(t.TempDir(), "library", "library.gox")
		generated, err := lowerDeriveSources(map[string]string{path: declaration + integer})
		if err != nil {
			t.Fatal(err)
		}
		name := operation(generated[path], "int")
		if relocatedName != "" && name != relocatedName {
			t.Fatalf("relocating source changed canonical pair ops: %s -> %s", relocatedName, name)
		}
		relocatedName = name
	}
	integerOps := operation(first[name], "int")
	if got := operation(second[name], "int"); got != integerOps {
		t.Fatalf("adding another input renamed existing pair ops: %s -> %s", integerOps, got)
	}
	textOps := operation(second[name], "string")
	if textOps == integerOps {
		t.Fatal("different trait inputs share their operation receiver")
	}
	module := t.TempDir()
	for file, contents := range map[string][]byte{
		"go.mod":             []byte("module applicationfixture\n\ngo 1.27\n"),
		"library/library.go": second[name],
		"main.go": []byte(strings.NewReplacer("INTEGER_OPS", integerOps, "TEXT_OPS", textOps).Replace(`package main
import ("fmt"; "applicationfixture/library")
func main() { fmt.Println((library.INTEGER_OPS{}).From(7), (library.TEXT_OPS{}).From("text")) }
`)),
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(module, file)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := testdeps.WriteFile(filepath.Join(module, file), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runGo(t, module, "build", "./...")
	if got := runGo(t, module, "run", "."); got != "8 4\n" {
		t.Fatalf("Go pair operations = %q", got)
	}
}

func TestTraitGenericPairNamesRetainConditionsAcrossRelocation(t *testing.T) {
	const declaration = `package library
pub trait From[S any] { func from(value S) Self }
pub struct Box[T any] { pub Value T }
`
	const integer = `impl[T interface{~int}] From[T] for Box[T] { func from(value T) Self { return Self{Value:value} } }
`
	const text = `impl[T interface{~string}] From[T] for Box[T] { func from(value T) Self { return Self{Value:value} } }
`
	operations := func(data []byte) []string {
		t.Helper()
		file, err := parser.ParseFile(token.NewFileSet(), "library.go", data, 0)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, declaration := range file.Decls {
			group, ok := declaration.(*goast.GenDecl)
			if !ok || group.Tok != token.TYPE {
				continue
			}
			for _, spec := range group.Specs {
				typ := spec.(*goast.TypeSpec)
				if strings.HasPrefix(typ.Name.Name, "GoxideBoxFrom") && strings.Contains(typ.Name.Name, "Ops_") {
					names = append(names, typ.Name.Name)
				}
			}
		}
		sort.Strings(names)
		return names
	}
	var first []string
	for _, source := range []string{declaration + integer + text, declaration + text + integer} {
		path := filepath.Join(t.TempDir(), "library", "library.gox")
		generated, err := lowerDeriveSources(map[string]string{path: source})
		if err != nil {
			t.Fatal(err)
		}
		names := operations(generated[path])
		if len(names) != 2 || names[0] == names[1] {
			t.Fatalf("distinct conditions did not produce independent generic pair ops: %v", names)
		}
		if first != nil && strings.Join(names, ",") != strings.Join(first, ",") {
			t.Fatalf("relocating and reordering generic impls renamed their operations: %v -> %v", first, names)
		}
		first = names
	}
}

func TestTraitPairNamesRetainAnonymousPrivateMembersAcrossRelocation(t *testing.T) {
	for _, test := range []struct{ name, input, body string }{
		{"field", "struct{number int}", "value.number"},
		{"method", "interface{number()int}", "value.number()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `package p
trait From[S any] {func from(value S)Self}
type Value int
impl From[` + test.input + `] for Value {func from(value ` + test.input + `)Self{return Value(` + test.body + `)}}
`
			var first string
			for range 2 {
				path := filepath.Join(t.TempDir(), "p", "lib.gox")
				output, err := lowerDeriveSources(map[string]string{path: source})
				if err != nil {
					t.Fatal(err)
				}
				name := regexp.MustCompile(`GoxideValueFrom(?:Record|Interface)Ops_[[:alnum:]]+`).FindString(string(output[path]))
				if name == "" {
					t.Fatalf("missing pair operation:\n%s", output[path])
				}
				if first != "" && name != first {
					t.Fatalf("private anonymous member changed operation across relocation: %s -> %s", first, name)
				}
				first = name
			}
		})
	}
}
