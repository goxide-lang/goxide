package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func demandProgram(t *testing.T, source string) *check.Info {
	t.Helper()
	files, resolved := parse(t, "package p\n"+source)
	info, diagnostics := check.Check(files, resolved)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if info.Demands == nil || info.Demands.Info == nil {
		t.Fatal("missing demand plan")
	}
	return info
}

func TestDemandConcreteImplsWithoutCalls(t *testing.T) {
	info := demandProgram(t, `
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return 7 } }
type Box[T any] struct { value T }
impl[T P] P for Box[T] { func score(self Self) int { return self.value.score() } }
pub func Unused[T P](value T) int { return value.score() }
`)
	if len(info.Demands.Impls) != 1 || !types.Identical(info.Demands.Impls[0].Target, types.Typ[types.Int]) {
		t.Fatalf("concrete implementation was omitted or an unused template was emitted: %+v", info.Demands.Impls)
	}
	if len(info.Demands.Functions) != 0 || len(info.Demands.FunctionTemplates) != 1 {
		t.Fatalf("unused public template created instances: %+v", info.Demands)
	}
	member := info.Demands.Impls[0].Methods[0]
	if len(info.Demands.Bodies[member.Decl]) != 1 {
		t.Fatal("unused concrete impl member body was omitted")
	}
}

func TestDemandAssociatedBodyViewsAreIndependent(t *testing.T) {
	info := demandProgram(t, `
trait Source { type Item; func get(self Self) Self::Item }
impl Source for int { type Item = int; func get(self Self) Self::Item { return self } }
impl Source for string { type Item = string; func get(self Self) Self::Item { return self } }
func Read[T Source](value T) T::Source::Item { return value.get() }
func run() { Read[int](3); Read[string]("three") }
`)
	if len(info.Demands.Functions) != 2 {
		t.Fatalf("instances = %d, want two", len(info.Demands.Functions))
	}
	for _, instance := range info.Demands.Functions {
		body := instance.Body
		if body == nil || body.Info == nil || len(instance.TypeArgs) != 1 {
			t.Fatal("missing function body view")
		}
		want := instance.TypeArgs[0]
		if got := body.Info.TypeOverrides[instance.Decl.Results[0].Type]; !types.Identical(got, want) {
			t.Fatalf("specialized associated result = %v, want %v", got, want)
		}
		if _, ok := info.TypeOverrides[instance.Decl.Results[0].Type].(*types.TypeParam); !ok {
			t.Fatal("instance overwrote the template's associated result")
		}
		found := false
		ast.Walk(instance.Decl.Body, func(node ast.Node) bool {
			expression, ok := node.(ast.Expr)
			if !ok || info.Members[expression] == nil {
				return true
			}
			original, selected := info.Members[expression], body.Info.Members[expression]
			if original.Bound == nil || selected.Bound != nil || selected.Impl == nil || !types.Identical(selected.Impl.Target, want) || !types.Identical(selected.Signature.Results().At(0).Type(), want) {
				t.Fatalf("member evidence was not rebound independently: original=%+v instance=%+v", original, selected)
			}
			found = true
			return true
		})
		if !found {
			t.Fatal("missing selected source member")
		}
	}
}

func TestDemandNestedFunctionAndImplClosure(t *testing.T) {
	info := demandProgram(t, `
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return self } }
impl[T P] P for []T { func score(self Self) int { if len(self) == 0 { return 0 }; return self[0].score() } }
func Leaf[T P](value T) int { return value.score() }
func Middle[T P](value T) int { return Leaf[T](value) }
func run() int { return Middle[[]int]([]int{7}) }
`)
	if len(info.Demands.Functions) != 2 {
		t.Fatalf("function closure = %d, want Middle and Leaf", len(info.Demands.Functions))
	}
	if len(info.Demands.Impls) != 2 {
		t.Fatalf("impl closure = %d, want int and []int", len(info.Demands.Impls))
	}
	for _, instance := range info.Demands.Functions {
		if instance.Decl.Name.Name != "Middle" {
			continue
		}
		found := false
		ast.Walk(instance.Decl.Body, func(node ast.Node) bool {
			if child := instance.Body.Info.FunctionInstances[node]; child != nil {
				if child.Decl.Name.Name != "Leaf" || child.Body == nil || !types.Identical(child.TypeArgs[0], instance.TypeArgs[0]) {
					t.Fatalf("nested call retained symbolic evidence: %+v", child)
				}
				found = true
			}
			return true
		})
		if !found {
			t.Fatal("Middle's instantiated call edge is missing")
		}
	}
}

func TestDemandSameInstanceRecursion(t *testing.T) {
	info := demandProgram(t, `
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return self } }
func Recur[T P](value T, count int) int { if count == 0 { return value.score() }; return Recur[T](value, count-1) }
func run() int { return Recur[int](7, 2) }
`)
	if len(info.Demands.Functions) != 1 {
		t.Fatalf("recursive instance emitted %d times", len(info.Demands.Functions))
	}
	instance := info.Demands.Functions[0]
	found := false
	ast.Walk(instance.Decl.Body, func(node ast.Node) bool {
		if call := instance.Body.Info.FunctionInstances[node]; call != nil {
			if call != instance {
				t.Fatal("same-instance recursion did not reuse the registered instance")
			}
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("missing recursive call")
	}
}

func TestDemandTypeGrowthLimit(t *testing.T) {
	files, resolved := parse(t, `package p
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return self } }
impl[T any] P for []T { func score(self Self) int { return len(self) } }
func Grow[T P](value T) int { return Grow[[]T]([]T{}) }
func run() int { return Grow[int](1) }
`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "instance expansion limit exceeded") && strings.Contains(diagnostic.Message, "Grow[int]") && strings.Contains(diagnostic.Message, "Grow[[]int]") {
			return
		}
	}
	t.Fatalf("missing honest type-growth limit with instance chain: %v", diagnostics)
}

func TestDemandPreservesOrdinaryGoGenerics(t *testing.T) {
	info := demandProgram(t, `
type Box[T any] struct { value T }
impl[T any] Box[T] { func get(self Self) T { return self.value } }
func Identity[T any](value T) T { return value }
func Read[T any](value Box[T]) T { return Identity[T](value.get()) }
func run() int { return Read[int](Box[int]{value: 9}) }
`)
	if len(info.Demands.FunctionTemplates) != 0 || len(info.Demands.Functions) != 0 || len(info.Demands.GenericImpls) != 1 || len(info.Demands.Impls) != 0 {
		t.Fatalf("ordinary Go generics were specialized: %+v", info.Demands)
	}
}
