package resolve_test

import (
	"testing"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func TestControlFlowAndClosureScopes(t *testing.T) {
	source := `package p
func f(input any, values []int, ch chan int) {
 goto finish
 if x := len(values); x > 0 { _ = x } else { _ = x }
 for i := 0; i < len(values); i++ { _ = i }
 for index, value := range values { _, _ = index, value }
 switch n := len(values); n { case 0: v := n; _ = v; default: v := n; _ = v }
 switch value := input.(type) {
 case int: _ = value // integer
 case string: _ = value // string
 }
 select { case value := <-ch: _ = value; default: }
 closure := |x| x + len(values)
 _ = closure
finish:
 return
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	intRef := named(t, files["p.gox"], source, "value // integer", "value")
	stringRef := named(t, files["p.gox"], source, "value // string", "value")
	if file.References[intRef.ID()].Symbol == file.References[stringRef.ID()].Symbol {
		t.Fatal("type switch cases share an implicit variable")
	}
	ast.Walk(files["p.gox"], func(node ast.Node) bool {
		if clause, ok := node.(*ast.CaseClause); ok && len(clause.Types) != 0 {
			if file.Definitions[clause.ID()] == nil {
				t.Error("type switch case lacks its implicit definition")
			}
		}
		return true
	})
}

func TestLocalDeclarationTiming(t *testing.T) {
	source := `package p
var outer = 1
func f() {
 var outer = outer
 const constant = constant
 type (
  Early *Later
  Later *Later
 )
 _ = outer
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %v, want two forward-reference errors", result.Diagnostics)
	}
	constant := named(t, files["p.gox"], source, "constant\n", "constant")
	later := named(t, files["p.gox"], source, "Later\n", "Later")
	diagnosticAt(t, result, "p.gox", constant.Span())
	diagnosticAt(t, result, "p.gox", later.Span())
	file := result.Files["p.gox"]
	ast.Walk(files["p.gox"], func(node ast.Node) bool {
		switch node.(type) {
		case *ast.ValueSpec, *ast.VarDecl, *ast.ConstDecl:
			if _, ok := file.Visibility[node.ID()]; !ok {
				t.Errorf("declaration %T has no visibility", node)
			}
		}
		return true
	})
}

func TestGenericReceiversAndSelfMembers(t *testing.T) {
	source := `package p
type Box[T any] struct { value T }
func (b Box[T]) Value() T { return b.value }
trait Reader[T any] {
 type Item
 func read(x T) Self.Item { return Self::make(x) }
 func make(x T) Self.Item
}
impl Reader[int] for Box[int] {
 type Item = int
 func read(x int) Self.Item { _ = self; return Self::make(x) }
 func make(x int) Self.Item { return x }
}
func identity[T any](value T) T where T: any { return value }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
}

func TestPatternsReferenceConstantsAndShareBindings(t *testing.T) {
	source := `package p
const sentinel = 1
enum E { A(int), B(int), }
func f(value int) int {
 return match value {
  sentinel => 0,
  Some(x) | E::A(x) => x,
  None => 0,
  _ => 1,
 }
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	constant := named(t, files["p.gox"], source, "sentinel =>", "sentinel")
	if file.References[constant.ID()].Symbol == nil || file.Definitions[constant.ID()] != nil {
		t.Fatal("constant pattern was treated as a new binding")
	}
	first := named(t, files["p.gox"], source, "Some(x)", "x")
	second := named(t, files["p.gox"], source, "E::A(x)", "x")
	body := named(t, files["p.gox"], source, "=> x", "x")
	if file.Definitions[first.ID()] != file.Definitions[second.ID()] || file.Definitions[first.ID()] != file.References[body.ID()].Symbol {
		t.Fatal("or-pattern alternatives and body do not share a binding")
	}
}

func TestOrPatternsRequireMatchingBindings(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
func f(value int) int { return match value { Some(x) | Some(y) => 0 } }
`})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "pattern-bindings" {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
}

func TestAnonymousFieldsAndCompositeKeys(t *testing.T) {
	source := `package p
type Numbers[K comparable] map[K]int
var data struct { Upper int; lower struct { Nested int } }
func f() {
 _ = Numbers[string]{missing: 1}
 _ = struct { Field int }{Field: 1}
 _ = data.Upper
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
	missing := named(t, files["p.gox"], source, "missing:", "missing")
	diagnosticAt(t, result, "p.gox", missing.Span())
	for _, field := range []string{"Upper", "Nested", "Field"} {
		name := named(t, files["p.gox"], source, field+" int", field)
		if result.Files["p.gox"].Visibility[name.ID()] != resolve.Public {
			t.Errorf("Go anonymous field %s is not public", field)
		}
	}
}

func TestLabelsDoNotCrossFunctions(t *testing.T) {
	source := `package p
func f() {
 target := 1
 _ = target
 _ = func() { goto target }
target:
 return
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
	name := named(t, files["p.gox"], source, "goto target", "target")
	diagnosticAt(t, result, "p.gox", name.Span())
}

func TestDotRequiresAValueReceiver(t *testing.T) {
	source := `package p
use strings;
func f() {
 _ = strings.Repeat
 strings := struct { Repeat int }{}
 _ = strings.Repeat
 _ = strings::Repeat
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "namespace-as-value" {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
}

func TestMacroAndValueNamespacesCoexist(t *testing.T) {
	source := `package p
macro_rules! emit { () => {}; }
func f() { emit := 1; _ = emit; emit!() }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	name := named(t, files["p.gox"], source, "emit!()", "emit")
	if symbol := result.Files["p.gox"].References[name.ID()].Symbol; symbol == nil || symbol.Kind != resolve.Macro {
		t.Fatalf("macro call resolved to %v", symbol)
	}
}

func TestCompositeKeyClassificationUsesDeclarationScope(t *testing.T) {
	source := `package p
type Base map[string]int
func f() {
 type Base struct { Key int }
 type Alias Base
 _ = Alias{Key: 1}
}
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	noDiagnostics(t, resolve.Resolve(files, nil))
}

func TestStaticPathsDoNotSelectInstanceFields(t *testing.T) {
	source := "package p\ntype S struct { Field int }\nfunc f() { _ = S::Field }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	name := named(t, files["p.gox"], source, "S::Field", "Field")
	diagnosticAt(t, result, "p.gox", name.Span())
}

func TestModuleTypesRequireStaticPaths(t *testing.T) {
	source := "package p\nuse remote;\nvar x remote.Type"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "namespace-as-value" {
		t.Fatalf("diagnostics = %v", result.Diagnostics)
	}
}
