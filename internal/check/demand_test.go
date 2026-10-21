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
	const source = `package p
trait P { func score(self Self) int }
impl P for int { func score(self Self) int { return self } }
impl[T any] P for []T { func score(self Self) int { return len(self) } }
func Grow[T P](value T) int { return Grow[[]T]([]T{}) }
func run() int { return Grow[int](1) }
`
	files, resolved := parse(t, source)
	var declaration ast.Span
	for _, item := range files["main.gox"].Items {
		if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == "Grow" {
			declaration = function.Span()
		}
	}
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "instance expansion limit exceeded") && strings.Contains(diagnostic.Message, "Grow[int]") && strings.Contains(diagnostic.Message, "Grow[[]int]") {
			if diagnostic.File != "main.gox" || diagnostic.Span != declaration || declaration.Start >= declaration.End {
				t.Fatalf("type-growth diagnostic lost its source declaration: %+v, want %v", diagnostic, declaration)
			}
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

func TestDemandMutuallyRecursiveFunctionsReuseConcreteInstances(t *testing.T) {
	info := demandProgram(t, `
trait Score { func score(self Self) int }
impl Score for int { func score(self Self) int { return self } }
impl Score for string { func score(self Self) int { return len(self) } }
func Even[T Score](value T, count int) int { if count == 0 { return value.score() }; return Odd[T](value, count-1) }
func Odd[T Score](value T, count int) int { if count == 0 { return value.score() }; return Even[T](value, count-1) }
func run() { Even[int](7, 3); Odd[int](9, 2); Even[string]("seven", 3) }
`)
	if len(info.Demands.Functions) != 4 {
		t.Fatalf("mutual recursion produced %d instances, want Even and Odd for int and string", len(info.Demands.Functions))
	}
	instances := map[string]*check.FunctionInstance{}
	for _, instance := range info.Demands.Functions {
		if instance.Body == nil || len(instance.TypeArgs) != 1 {
			t.Fatalf("incomplete recursive instance: %+v", instance)
		}
		key := instance.Decl.Name.Name + "/" + instance.TypeArgs[0].String()
		if instances[key] != nil {
			t.Fatalf("duplicate recursive instance %s", key)
		}
		instances[key] = instance
	}
	for _, instance := range info.Demands.Functions {
		next := "Odd"
		if instance.Decl.Name.Name == "Odd" {
			next = "Even"
		}
		want := instances[next+"/"+instance.TypeArgs[0].String()]
		if want == nil {
			t.Fatal("recursive partner is missing")
		}
		found := false
		ast.Walk(instance.Decl.Body, func(node ast.Node) bool {
			if child := instance.Body.Info.FunctionInstances[node]; child != nil {
				if child != want || !types.Identical(child.TypeArgs[0], instance.TypeArgs[0]) {
					t.Fatalf("recursive edge did not reuse its concrete partner: got %+v, want %+v", child, want)
				}
				found = true
			}
			return true
		})
		if !found {
			t.Fatalf("%s has no recursive edge", instance.Decl.Name.Name)
		}
	}
}

func TestDemandMutuallyRecursiveImplsReuseConcreteTarget(t *testing.T) {
	info := demandProgram(t, `
trait Score { func score(self Self, count int) int }
trait Other { func other(self Self, count int) int }
impl Score for int { func score(self Self, count int) int { return self } }
type Box[T any] struct { value T }
impl[T Score] Score for Box[T] {
 func score(self Self, count int) int { if count == 0 { return self.value.score(0) }; return Other::other(self, count-1) }
}
impl[T Score] Other for Box[T] {
 func other(self Self, count int) int { if count == 0 { return self.value.score(0) }; return Score::score(self, count-1) }
}
func run() { value := Box[int]{value:7}; Score::score(value,3); Other::other(value,2) }
`)
	if len(info.Demands.Impls) != 3 {
		t.Fatalf("mutual impl closure has %d targets, want int:Score and Box[int]:Score+Other", len(info.Demands.Impls))
	}
	selected := map[string]*check.Impl{}
	for _, implementation := range info.Demands.Impls {
		target, ok := implementation.Target.(*types.Named)
		if !ok || target.Obj().Name() != "Box" {
			continue
		}
		if target.TypeArgs().Len() != 1 || !types.Identical(target.TypeArgs().At(0), types.Typ[types.Int]) {
			t.Fatalf("mutually recursive impl retained a symbolic target: %s", target)
		}
		name := implementation.Trait.Symbol.Name.Name
		if selected[name] != nil {
			t.Fatalf("duplicate Box[int]:%s", name)
		}
		selected[name] = implementation
	}
	if len(selected) != 2 {
		t.Fatalf("missing mutually recursive Box[int] impls: %v", selected)
	}
	for name, implementation := range selected {
		partner := "Other"
		if name == "Other" {
			partner = "Score"
		}
		member := implementation.Methods[0]
		bodies := info.Demands.Bodies[member.Decl]
		if len(bodies) != 1 {
			t.Fatalf("Box[int]:%s has %d emitted body instances", name, len(bodies))
		}
		found := false
		ast.Walk(member.Decl.Body, func(node ast.Node) bool {
			expression, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			selection := bodies[0].Info.Members[expression]
			if selection != nil && selection.Member.Trait != nil && selection.Member.Trait.Symbol.Name.Name == partner && selection.Impl != nil && types.Identical(selection.Impl.Target, implementation.Target) {
				if selection.Impl.Decl != selected[partner].Decl {
					t.Fatalf("Box[int]:%s did not reuse its recursive partner", name)
				}
				found = true
			}
			return true
		})
		if !found {
			t.Fatalf("Box[int]:%s has no edge to %s", name, partner)
		}
	}
}

func TestDemandProofCycleDiagnosticAtSourceCall(t *testing.T) {
	const source = `package p
trait P {}
trait Q {}
type Box[T any] struct {}
impl[T any] P for Box[T] where Box[T]:Q {}
impl[T any] Q for Box[T] where Box[T]:P {}
func Accept[T P](value T) {}
func run() { Accept[Box[int]](Box[int]{}) }
`
	files, resolved := parse(t, source)
	_, diagnostics := check.Check(files, resolved)
	const call = "Accept[Box[int]](Box[int]{})"
	start := uint32(strings.Index(source, call))
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == check.GenericConstraint && strings.Contains(diagnostic.Message, "does not explicitly implement") {
			if diagnostic.File != "main.gox" || diagnostic.Span.Start < start || diagnostic.Span.End > start+uint32(len(call)) || diagnostic.Span.Start >= diagnostic.Span.End {
				t.Fatalf("proof cycle diagnostic is not attached to the source call: %+v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("missing cyclic proof rejection: %v", diagnostics)
}
