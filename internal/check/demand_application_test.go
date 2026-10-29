package check_test

import (
	"go/types"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestDemandKeepsDifferentInputsForOneReceiver(t *testing.T) {
	info := demandProgram(t, `
trait Make[S any] { func make(value S) Self }
struct Count { value int }
impl Make[int] for Count { func make(value int) Self { return Self{value:value} } }
impl Make[string] for Count { func make(value string) Self { return Self{value:len(value)} } }
func Pair[T any](number int, text string) (T,T) where T:Make[int]+Make[string] {
    return T::make::<Make[int]>(number),T::make::<Make[string]>(text)
}

func run() { Pair[Count](3,"text") }
`)
	if len(info.Demands.Functions) != 1 {
		t.Fatalf("Pair instances = %d, want 1", len(info.Demands.Functions))
	}
	instance := info.Demands.Functions[0]
	if len(instance.Evidence) != 2 || instance.Evidence[0].Key() == instance.Evidence[1].Key() {
		t.Fatalf("distinct source inputs collapsed into one prerequisite: %+v", instance.Evidence)
	}
	inputs := map[types.Type]bool{}
	ast.Walk(instance.Decl.Body, func(node ast.Node) bool {
		if expression, ok := node.(ast.Expr); ok {
			if member := instance.Body.Info.Members[expression]; member != nil {
				if member.Impl == nil || member.Bound != nil || len(member.Evidence.Cases) != 0 || len(member.Impl.Inputs) != 1 {
					t.Fatalf("member has no concrete application evidence: %+v", member)
				}
				inputs[member.Impl.Inputs[0]] = true
			}
		}
		return true
	})
	if len(inputs) != 2 || !inputs[types.Typ[types.Int]] || !inputs[types.Typ[types.String]] {
		t.Fatalf("demanded member inputs = %v, want int and string", inputs)
	}
}

func TestDemandCombinesMethodCasesInOneParameterEnvironment(t *testing.T) {
	info := demandProgram(t, `
trait Label { func label(self Self) string }
struct Cell[T any] { value T }
impl[T ~int] Label for Cell[T] { func label(self Self) string { return "number" } }
impl[T ~string] Label for Cell[T] { func label(self Self) string { return "text" } }
trait Pair { func first(self Self) string; func second(self Self) string }
struct Envelope[T any] { cell Cell[T] }
impl[T interface{~int|~string}] Pair for Envelope[T] {
    func first(self Self) string { return self.cell.label() }
    func second(self Self) string { return self.cell.label() }
}
`)
	if len(info.ImplCases) != 1 {
		t.Fatalf("partitioned implementation count = %d, want 1", len(info.ImplCases))
	}
	for _, branches := range info.ImplCases {
		if len(branches) != 2 {
			t.Fatalf("reachable method combinations = %d, want the two correlated branches", len(branches))
		}
		for _, branch := range branches {
			if len(branch.TypeParams) != 1 || len(branch.Methods) != 2 {
				t.Fatalf("incomplete shared branch: %+v", branch)
			}
			for _, method := range branch.Methods {
				if len(method.TypeParams) != 1 || method.TypeParams[0] != branch.TypeParams[0] {
					t.Fatal("method predicates were quantified with independent parameters")
				}
				ast.Walk(method.Decl.Body, func(node ast.Node) bool {
					if expression, ok := node.(ast.Expr); ok {
						if selected := method.Info.Members[expression]; selected != nil && len(selected.Evidence.Cases) != 0 {
							t.Fatal("frozen method combination retained an unresolved evidence partition")
						}
					}
					return true
				})
			}
		}
	}
}
