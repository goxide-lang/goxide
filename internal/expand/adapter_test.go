package expand

import (
	"errors"
	"fmt"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func TestAdapterTypeSlots(t *testing.T) {
	for _, test := range []struct{ name, template string }{
		{"variable", `var value $T`},
		{"parameters and results", `func f(value $T) $T { return value }`},
		{"field", `type Alias struct { value $T }`},
		{"generic argument", `type Alias = Box[$T]`},
		{"pointer target", `type Alias = *$T`},
		{"assertion", `func f(value any) { _ = value.($T) }`},
		{"type switch", `func f(value any) { switch value.(type) { case $T: } }`},
		{"composite literal", `var _ = []$T{}`},
		{"array length subtree", `var _ = [len([2]$T{})]int{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "macro_rules! typed { ($T:type) => { " + test.template + " }; }\ntyped!(map[string][]int)\n"
			result, err := adapterExpand(t, source)
			if err != nil {
				t.Fatal(err)
			}
			seen := 0
			ast.Walk(result.Files[0], func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Name == "string" {
					seen++
					if id.Ctx != 0 {
						t.Fatalf("captured type context changed: %#v", id)
					}
				}
				return true
			})
			if seen == 0 {
				t.Fatal("captured type missing from output")
			}
		})
	}
}

func TestAdapterRejectsNonTypeSlots(t *testing.T) {
	for _, category := range []string{"expr", "ident", "tt"} {
		for _, template := range []string{
			`var value $T`,
			`func f(value $T) $T { return value }`,
			`type Alias struct { value $T }`,
			`type Alias = Box[$T]`,
			`var _ = []$T{}`,
			`var _ = [len([2]$T{})]int{}`,
		} {
			t.Run(category+"/"+template, func(t *testing.T) {
				source := "macro_rules! invalid { ($T:" + category + ") => { " + template + " }; }\ninvalid!(int)\n"
				_, err := adapterExpand(t, source)
				adapterCategoryError(t, err)
			})
		}
	}
}

func TestAdapterRejectsCompositeTTType(t *testing.T) {
	for _, input := range []string{"(struct{})", "(interface{})", "(func())"} {
		t.Run(input, func(t *testing.T) {
			_, err := adapterExpand(t, "macro_rules! invalid { ($T:tt) => { type Alias $T }; }\ninvalid!("+input+")\n")
			adapterCategoryError(t, err)
		})
	}
}

func TestAdapterWholeTTExpressionOwnsItsTypes(t *testing.T) {
	result, err := adapterExpand(t, `macro_rules! pass { ($x:tt)=>{$x}; }
var value = pass!(([]Local{{}}))`)
	if err != nil {
		t.Fatal(err)
	}
	ids := expandIdents(result.Files[0], "Local")
	if len(ids) != 1 || ids[0].Ctx != 0 {
		t.Fatal("whole tt expression lost its type identity")
	}
}

func TestAdapterIdentConcreteFields(t *testing.T) {
	result, err := adapterExpand(t, `
macro_rules! named { ($name:ident, $T:type) => { type $name struct { $name $T } }; }
named!(Local, []int)
macro_rules! method { ($name:ident) => { func (x *S) $name() { _ = x.$name } }; }
method!(Field)
`)
	if err != nil {
		t.Fatal(err)
	}
	decl := result.Files[0].Items[0].(*ast.TypeDecl)
	field := decl.Type.(*ast.StructType).Fields[0]
	method := result.Files[0].Items[1].(*ast.FuncDecl)
	selection := method.Body.List[0].(*ast.AssignStmt).Rhs[0].(*ast.SelectorExpr)
	if decl.Name.Name != "Local" || field.Names[0].Name != "Local" || decl.Name == field.Names[0] || method.Name.Name != "Field" || selection.Sel.Name != "Field" {
		t.Fatalf("identifier slots were not replaced independently: %#v %#v", decl, method)
	}
	if decl.Name.Ctx != 0 || field.Names[0].Ctx != 0 || method.Name.Ctx != 0 || selection.Sel.Ctx != 0 {
		t.Fatal("identifier fragments lost their call-site context")
	}
}

func TestAdapterRejectsExpressionsInIdentFields(t *testing.T) {
	for _, template := range []string{`func $name() {}`, `var _ = object.$name`, `type T struct { $name int }`} {
		t.Run(template, func(t *testing.T) {
			_, err := adapterExpand(t, "macro_rules! invalid { ($name:expr) => { "+template+" }; }\ninvalid!(a + b)\n")
			adapterCategoryError(t, err)
		})
	}
}

func TestAdapterItemInDeclarationSlots(t *testing.T) {
	for _, test := range []struct{ name, template, input string }{
		{"package declaration", `$declaration`, `type Local struct { value int }`},
		{"impl declaration", `$declaration`, `impl Local { func value() int { return 1 } }`},
		{"local declaration", `func generated() { $declaration }`, `var local int`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := adapterExpand(t, "macro_rules! emit { ($declaration:item) => { "+test.template+" }; }\nemit!("+test.input+")\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Files[0].Items) != 1 {
				t.Fatalf("expected one complete declaration, got %d", len(result.Files[0].Items))
			}
			ast.Walk(result.Files[0], func(n ast.Node) bool {
				switch n.(type) {
				case *ast.MacroItem, *ast.MacroExpr:
					t.Fatalf("unexpanded item placeholder: %T", n)
				}
				return true
			})
		})
	}
}

func TestAdapterNestedOpaqueExpression(t *testing.T) {
	result, err := adapterExpand(t, `
macro_rules! outer { ($x:expr) => { inner!($x) }; }
macro_rules! inner { ($value:expr) => { $value * 2 }; }
var answer = outer!(left + right)
`)
	if err != nil {
		t.Fatal(err)
	}
	expr := result.Files[0].Items[0].(*ast.VarDecl).Specs[0].Values[0].(*ast.BinaryExpr)
	left, ok := expr.X.(*ast.BinaryExpr)
	if !ok || expr.Op != token.MUL || left.Op != token.ADD {
		t.Fatalf("nested fragment was reparsed with template precedence: %#v", expr)
	}
	if left.X.(*ast.Ident).Ctx != 0 || left.Y.(*ast.Ident).Ctx != 0 || len(result.Expansions) != 2 {
		t.Fatal("nested fragment lost call-site identity or expansion stack")
	}
	_, err = adapterExpand(t, `
macro_rules! outer { ($T:type) => { inner!($T) }; }
macro_rules! inner { ($value:expr) => { $value }; }
var answer = outer!(int)
`)
	adapterCategoryError(t, err)
}

func TestAdapterNestedCapturedItemIdentity(t *testing.T) {
	result, err := adapterExpand(t, `
macro_rules! outer { ($declaration:item) => { inner!($declaration) }; }
macro_rules! inner { ($declaration:item) => { $declaration }; }
outer!(type Local int)
`)
	if err != nil {
		t.Fatal(err)
	}
	name := result.Files[0].Items[0].(*ast.TypeDecl).Name
	if name.Name != "Local" || name.Ctx != 0 {
		t.Fatalf("nested declaration capture changed identity: %#v", name)
	}
}

func TestAdapterGenericSlots(t *testing.T) {
	// Argument count cannot distinguish type slots from mixed type/const slots.
	// TestMacroGenericTypeCaptureCategories rejects these captures after lookup.
	for _, test := range []struct{ name, matcher, template, input string }{
		{"single type", `$T:type`, `Box[$T]`, `[]int`},
		{"single index expression", `$i:expr`, `values[$i]`, `offset + 1`},
		{"single unresolved identifier", `$i:ident`, `values[$i]`, `offset`},
		{"multiple types", `$A:type, $B:type`, `Pair[$A, $B]`, `[]int, map[string]int`},
		{"multiple expressions", `$A:expr, $B:expr`, `Pair[$A, $B]`, `int, string`},
		{"multiple identifiers", `$A:ident, $B:ident`, `Pair[$A, $B]`, `int, string`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := adapterExpand(t, fmt.Sprintf("macro_rules! generic { (%s) => { %s }; }\nvar _ = generic!(%s)\n", test.matcher, test.template, test.input))
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAdapterCapturedPathArguments(t *testing.T) {
	for _, test := range []struct{ kind, input string }{
		{"expr", "Count + 1"}, {"ident", "Count"}, {"tt", "Count"}, {"type", "[]int"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			result, err := adapterExpand(t, fmt.Sprintf("macro_rules! generic { ($value:%s) => { Ops[$value]::make }; }\nvar _ = generic!(%s)\n", test.kind, test.input))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			ast.Walk(result.Files[0], func(node ast.Node) bool {
				path, ok := node.(*ast.PathExpr)
				if !ok || len(path.Path.Segments) != 2 || path.Path.Segments[0].Name.Name != "Ops" {
					return true
				}
				argument := path.Path.Segments[0].Args.Args[0]
				captured := ast.Node(argument.Value)
				if test.kind == "type" {
					if argument.Type == nil || argument.Value != nil {
						t.Fatalf("type argument = %#v", argument)
					}
					captured = argument.Type
				} else if argument.Type != nil || argument.Value == nil {
					t.Fatalf("value argument = %#v", argument)
				}
				if got := result.Captures[captured].Kind; got != test.kind {
					t.Fatalf("capture kind = %q, want %q", got, test.kind)
				}
				found = true
				return false
			})
			if !found {
				t.Fatal("generic path argument missing")
			}
		})
	}
}

func TestAdapterAllocationCallsDoNotResolveNames(t *testing.T) {
	for _, template := range []string{
		`make($value, 2)`,
		`new($value)`,
		`func(new func(int) int) int { return new($value) }(func(x int) int { return x })`,
	} {
		t.Run(template, func(t *testing.T) {
			_, err := adapterExpand(t, "macro_rules! call { ($value:expr) => { "+template+" }; }\nvar _ = call!(input)\n")
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	_, err := adapterExpand(t, `
macro_rules! typed { ($T:type) => { make([]$T, 2) }; }
var _ = typed!(int)
`)
	if err != nil {
		t.Fatal(err)
	}
}

func adapterExpand(t *testing.T, source string) (result *Result, err error) {
	t.Helper()
	defer func() {
		if failure := recover(); failure != nil {
			t.Fatalf("AST slot substitution panicked: %v", failure)
		}
	}()
	file, diagnostics := parser.ParseFile("adapter.gox", []byte("package p\n"+source))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			t.Fatalf("invalid fixture: %v", diagnostics)
		}
	}
	return Expand([]*ast.File{file}, nil)
}

func adapterCategoryError(t *testing.T, err error) {
	t.Helper()
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Code != "category" {
		t.Fatalf("expected fragment category diagnostic, got %v", err)
	}
	if diagnostic.Definition.File == nil || diagnostic.Call.File == nil || diagnostic.Definition.Span.Start >= diagnostic.Definition.Span.End || diagnostic.Call.Span.Start >= diagnostic.Call.Span.End {
		t.Fatalf("category diagnostic lost definition or call span: %#v", diagnostic)
	}
}
