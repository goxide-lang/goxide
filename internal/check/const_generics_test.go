package check_test

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestConstArrayLengthDefinitionsKeepElementGeneric(t *testing.T) {
	info := demandProgram(t, `
func Copy[T any, const N int](value [N]T) [N]T {
 var result [N]T
 for index, element := range value { result[index] = element }
 if N > 0 { _ = value[0]; _ = value[:] }
 return result
}
func run() { Copy([3]int{1,2,3}); Copy([3]string{"a","b","c"}); Copy([0]int{}) }
`)
	if len(info.Demands.Functions) != 2 {
		t.Fatalf("length definitions = %d, want 2", len(info.Demands.Functions))
	}
	for _, definition := range info.Demands.Functions {
		if len(definition.TypeArgs) != 1 {
			t.Fatalf("missing retained element type: %+v", definition)
		}
		if _, ok := definition.TypeArgs[0].(*types.TypeParam); !ok {
			t.Fatalf("element was specialized: %v", definition.TypeArgs)
		}
		if len(definition.ConstArgs) != 1 || definition.ConstArgs[0].Param != nil {
			t.Fatal("unresolved emitted length")
		}
		array, ok := definition.Body.Info.ArrayOf(definition.Body.Info.TypeOverrides[definition.Decl.Results[0].Type])
		if !ok || array.Length != definition.ConstArgs[0] {
			t.Fatalf("body result = %+v, want length %+v", array, definition.ConstArgs)
		}
	}
}

func TestConstArrayFunctionOnlyAndForwarding(t *testing.T) {
	info := demandProgram(t, `
func Size[const N int]() int { return N }
func Leaf[T any, const N int](value [N]T) int { return Size[N]() + len(value) }
func Middle[T any, const N int](value [N]T) int { return Leaf[T,N](value) }
func run() int { return Middle([2]int{20,22}) + Size[0]() }
`)
	if len(info.Demands.Functions) != 4 {
		t.Fatalf("closure definitions = %d, want 4", len(info.Demands.Functions))
	}
	for _, definition := range info.Demands.Functions {
		ast.Walk(definition.Decl.Body, func(node ast.Node) bool {
			if expression, ok := node.(ast.Expr); ok {
				if value, exists := definition.Body.Info.ConstValues[expression]; exists && value.Param != nil {
					t.Fatal("body retained unresolved const expression")
				}
			}
			return true
		})
	}
}

func TestConstArrayImplLengthDefinitions(t *testing.T) {
	info := demandProgram(t, `
trait Size { func size(self Self) int }
impl[T any, const N int] Size for [N]T { func size(self Self) int { return N } }
func run() { [2]int{1,2}.size(); [2]string{"a","b"}.size(); [3]int{1,2,3}.size() }
`)
	if len(info.Demands.GenericImpls) != 2 {
		t.Fatalf("array impl definitions = %d, want 2", len(info.Demands.GenericImpls))
	}
	for _, definition := range info.Demands.GenericImpls {
		array, ok := info.ArrayOf(definition.Target)
		if !ok || array.Length.Param != nil {
			t.Fatal("symbolic array reached emitted impl")
		}
		if _, ok := array.Elem.(*types.TypeParam); !ok {
			t.Fatalf("element was specialized: %s", definition.Target)
		}
	}
}

func TestConstArrayInvalidArguments(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"negative", `func F[const N int]() int { return N }; func run() { F[-1]() }`, "nonnegative"},
		{"runtime", `func F[const N int]() int { return N }; func run(n int) { F[n]() }`, "nonnegative"},
		{"growing", `func F[const N int](a [N]int) { F[N+1](a) }`, "unchanged const parameter"},
		{"pattern expression", `func F[const N int](a [N+1]int) {}`, "unchanged const parameter"},
		{"non integer", `func F[const N string]() {}`, "require type int"},
		{"named type", `type Array[const N int] [N]int`, "only supported on functions and impls"},
		{"repeated", `func F[const N int](a [N]int,b [N]int) {}; func run() { F([2]int{},[3]int{}) }`, "consistent const array arguments"},
		{"named float constant", `type Float float64; const N Float = 3; func F[const N int]() {}; func run() { F[N]() }`, "integer constant"},
		{"named float expression", `type Float float64; const N Float = 3; func F[const N int]() {}; func run() { F[N+0]() }`, "integer constant"},
		{"float constant", `const N float64 = 3; func F[const N int]() {}; func run() { F[N]() }`, "integer constant"},
		{"const assignment", `func F[const N int]() { N = 3 }; func run() { F[2]() }`, "assign"},
		{"readonly element", `func F[const N int](values [N]&int) { *values[0] = 3 }; func run() { var value int; F([1]&int{&value}) }`, "read-only"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p\n"+test.source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if strings.Contains(diagnostic.Message, test.want) {
					return
				}
			}
			t.Fatalf("diagnostics = %v, want %s", diagnostics, test.want)
		})
	}
}

func TestConstArrayConstantsAndProtocolCallbacks(t *testing.T) {
	info := demandProgram(t, `
const Width = 1 + 1
trait Read { func read(self Self) int }
impl Read for int { func read(self Self) int { return self } }
impl Read for string { func read(self Self) int { return len(self) } }
impl[T Read, const N int] Read for [N]T { func read(self Self) int { var result int; for _, value := range self { result += value.read() }; return result } }
func ReadArray[T Read, const N int](value [N]T) int { return value.read() }
func run() { ReadArray[int,Width]([Width]int{20,22}); ReadArray[string,Width]([Width]string{"a","bb"}) }
`)
	if len(info.Demands.Functions) != 1 || len(info.Demands.GenericImpls) != 1 {
		t.Fatalf("protocol generic definitions were duplicated: funcs=%d impls=%d", len(info.Demands.Functions), len(info.Demands.GenericImpls))
	}
	definition := info.Demands.GenericImpls[0]
	if len(definition.RequirementEvidence) != 1 || definition.RequirementEvidence[0].Bound == nil {
		t.Fatal("generic array impl lost its source protocol callback")
	}
}

func TestConstArrayImplOverlapAndLengthIdentity(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"different concrete lengths", `trait P { func get(self Self) int }; impl P for [2]int { func get(self Self) int { return 2 } }; impl P for [3]int { func get(self Self) int { return 3 } }`, false},
		{"generic overlaps concrete zero", `trait P { func get(self Self) int }; impl[const N int] P for [N]int { func get(self Self) int { return N } }; impl P for [0]int { func get(self Self) int { return 0 } }`, true},
		{"independent same name", `func First[const N int](v [N]int) int { return N }; func Second[const N int](v [N]int) int { return First[N](v) }; func run() { First([2]int{}); Second([3]int{}) }`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p\n"+test.source)
			_, diagnostics := check.Check(files, resolved)
			if (len(diagnostics) != 0) != test.reject {
				t.Fatalf("diagnostics=%v, reject=%v", diagnostics, test.reject)
			}
		})
	}
}

func TestConstArrayNestedGenericAndAssociatedResult(t *testing.T) {
	info := demandProgram(t, `
type Box[T any] struct { value T }
trait First { type Item; func first(self Self) Self::Item }
impl[T any, const N int] First for [N]T { type Item = T; func first(self Self) Self::Item { return self[0] } }
func Wrap[T any, const N int](value [N]T) Box[[N]T] { return Box[[N]T]{value: value} }
func Head[T any, const N int](value [N]T) T { return value.first() }
func run() { Wrap([2]int{20,22}); Head([2]int{20,22}); Head([2]string{"a","b"}) }
`)
	if len(info.Demands.Functions) != 2 || len(info.Demands.GenericImpls) != 1 {
		t.Fatal("nested array requests did not share length definitions")
	}
	for _, definition := range info.Demands.Functions {
		if definition.Decl.Name.Name != "Wrap" {
			continue
		}
		result := definition.Body.Info.TypeOverrides[definition.Decl.Results[0].Type].(*types.Named)
		array, ok := info.ArrayOf(result.TypeArgs().At(0))
		if !ok || array.Length.Param != nil || array.Length.Value != 2 {
			t.Fatalf("nested named type retained symbolic length: %v", result)
		}
	}
}

func TestConstArraySymbolicGoConstraintDiagnostic(t *testing.T) {
	files, resolved := parse(t, `package p
func F[T ~[N]int, const N int](v T) T { return v }
func run() { F[[3]int,3]([3]int{}) }
`)
	_, diagnostics := check.Check(files, resolved)
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "const array patterns in Go type constraints") {
			return
		}
	}
	t.Fatalf("diagnostics = %v", diagnostics)
}

func TestConstArrayIntegerConstantOperations(t *testing.T) {
	info := demandProgram(t, `
const Width = (1 << 3) >> 1
const Half = 3 / 2
func Size[const N int](v [N]int) int { return N }
func run() { Size[Width]([Width]int{}); Size[Half]([Half]int{}); Size([... ]int{0:1,2:3}) }
`)
	lengths := map[int64]bool{}
	for _, definition := range info.Demands.Functions {
		lengths[definition.ConstArgs[0].Value] = true
	}
	if len(lengths) != 3 || !lengths[4] || !lengths[1] || !lengths[3] {
		t.Fatalf("constant lengths=%v", lengths)
	}
}
