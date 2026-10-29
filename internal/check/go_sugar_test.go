package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestGoSugarDoesNotChangeSourceMethodSets(t *testing.T) {
	const source = `package p
pub struct Cell { pub N int; next *Cell }
trait Observe { func inspect(self &Self) int }
impl Observe for Cell { func inspect(self &Self) int { return self.N } }
func Want[T interface{ Inspect() int }](value T) int { return value.Inspect() }
func run(value *Cell) int { return Want[*Cell](value) }
`
	files, resolved := parse(t, source)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(info.Sugar) != 1 {
		t.Fatalf("sugar count = %d", len(info.Sugar))
	}
	for _, implementation := range info.Impls {
		if method, _, _ := types.LookupFieldOrMethod(types.NewPointer(implementation.Target), true, implementation.Owner, "Inspect"); method != nil {
			t.Fatal("Go-only sugar changed the source named type's method set")
		}
	}
	disabled := false
	configuration := map[string]config.Config{}
	for name := range files {
		configuration[name] = config.Config{GoSugar: &disabled}
	}
	_, diagnostics = check.CheckWithOptions(files, resolved, check.Options{ConfigByFile: configuration})
	if len(diagnostics) == 0 {
		t.Fatal("disabled sugar still satisfies the ordinary Go method constraint")
	}
	found := false
	for _, diagnostic := range diagnostics {
		found = found || strings.Contains(diagnostic.Message, "missing method Inspect")
	}
	if !found {
		t.Fatal(diagnostics)
	}
}

func TestGoSugarGenericAndPromotedMethodSets(t *testing.T) {
	files, resolved := parse(t, `package p
pub struct Box[T any] { pub Value T; next *Box[T] }
trait Store { type Item; func get(self Self) Self::Item }
impl[T any] Store for Box[T] { type Item = T; func get(self Self) Self::Item { return self.Value } }
type Outer struct { Box[int] }
type Getter interface { Get() int }
func Want[T Getter](value T) int { return value.Get() }
func run(value Outer) int { return Want[Box[int]](Box[int]{Value: 3}) + Want[Outer](value) }
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(info.Sugar) != 1 || len(info.Demands.GenericImpls) != 1 {
		t.Fatalf("generic sugar did not retain its operation template: sugar=%d generic=%d", len(info.Sugar), len(info.Demands.GenericImpls))
	}
}

func TestGoSugarCannotEstablishSourceTrait(t *testing.T) {
	files, resolved := parse(t, `package p
pub struct Cell {}
trait First { func inspect(self Self) int }
trait Second { func inspect(self Self) int }
impl First for Cell { func inspect(self Self) int { return 1 } }
func Want[T Second](value T) int { return value.inspect() }
func run() int { return Want[Cell](Cell{}) }
`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "does not explicitly implement Second") {
			return
		}
	}
	t.Fatalf("sugar established an unrelated source trait: %v", diagnostics)
}

func TestGoSugarExplicitMethodRetainsGoConstraint(t *testing.T) {
	files, resolved := parse(t, `package p
pub struct Cell {}
trait First { func inspect(self Self) int }
trait Second { func inspect(self Self) int }
impl First for Cell { func inspect(self Self) int { return 1 } }
impl Second for Cell { func inspect(self Self) int { return 2 } }
impl Cell { pub func inspect(self Self) int { return First::inspect(self) } }
func Want[T interface{ Inspect() int }](value T) int { return value.Inspect() }
func run() int { return Want[Cell](Cell{}) }
`)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(info.Sugar) != 0 {
		t.Fatal("explicit Go method did not suppress competing sugar")
	}
}

func TestGoReceiverUsesNamedTypeConstraints(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         bool
	}{
		{"narrower", `
type Box[T any] struct { value T }
trait Number { func number(self Self) int }
impl[T ~int] Number for Box[T] { func number(self Self) int { return int(self.value) } }
`, false},
		{"same", `
type Box[T ~int] struct { value T }
trait Number { func number(self Self) int }
impl[T ~int] Number for Box[T] { func number(self Self) int { return int(self.value) } }
`, true},
		{"dependent", `
type Box[T any, S ~[]T] struct { value S }
trait Count { func count(self Self) int }
impl[A any, B ~[]A] Count for Box[A, B] { func count(self Self) int { return len(self.value) } }
`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p\n"+test.source)
			info, diagnostics := check.Check(files, resolved)
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			if got := len(info.Sugar) != 0; got != test.want {
				t.Fatalf("sugar selected = %v, want %v", got, test.want)
			}
			for _, implementation := range info.Impls {
				if got := check.CanUseGoReceiver(implementation); got != test.want {
					t.Fatalf("Go receiver selected = %v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestGoSugarNativeGenericPreservesReadonly(t *testing.T) {
	files := map[string]*ast.File{}
	for name, source := range map[string]string{
		"main.gox": `package p
pub struct Cell { pub N int }
trait Change { func add(self &mut Self) }
impl Change for Cell { func add(self &mut Self) { self.N++ } }
func run(value &Cell) { Native(value) }
`,
		"native.go": `package p
func Native[T interface{ Add() }](value T) { value.Add() }
`,
	} {
		file, diagnostics := parser.ParseFile(name, []byte(source))
		if len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
		files[name] = file
	}
	_, diagnostics := check.Check(files, resolve.Resolve(files, nil))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == check.ReferencePermission {
			return
		}
	}
	t.Fatalf("native generic call discarded readonly permission: %v", diagnostics)
}
