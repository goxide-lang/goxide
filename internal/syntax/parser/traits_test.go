package parser_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestTraitReceiverAndAssociatedTypeSyntax(t *testing.T) {
	const source = `package p
pub trait Source {
 type Item
 func value(self Self) Self::Item
 func read(self &Self) Self::Item
 func write(self &mut Self, value Self::Item)
 func make() Self
}
type Box struct { Value int }
impl Source for Box {
 type Item = int
 func value(self Self) Self::Item { return self.Value }
 func read(self &Self) Self::Item { return self.Value }
 func write(self &mut Self, value Self::Item) { self.Value = value }
 func make() Self { return Self{} }
}
func Read[T Source](value T) T::Source::Item { return value.read() }
func ReadWhere[T](value T) T::Source::Item where T: Source { return value.read() }
enum Choice { some(int), empty }
impl Choice {
 func get(self Self) int {
  return match self { Self::some(value) => value, Self::empty => 0 }
 }
}
`
	file, diagnostics := parser.ParseFile("traits.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	trait := file.Items[0].(*ast.TraitDecl)
	value := trait.Items[1].(*ast.FuncDecl)
	if value.Params[0].Names[0].Name != "self" {
		t.Fatal("explicit self receiver was not preserved")
	}
	if _, ok := value.Params[0].Type.(*ast.SelfType); !ok {
		t.Fatalf("value receiver = %T", value.Params[0].Type)
	}
	for index, mutable := range map[int]bool{2: false, 3: true} {
		receiver := trait.Items[index].(*ast.FuncDecl).Params[0].Type.(*ast.RefType)
		if receiver.Mut != mutable {
			t.Fatalf("method %d receiver mutable = %v", index, receiver.Mut)
		}
	}
	projection := value.Results[0].Type.(*ast.ProjectionType)
	if projection.Dot || projection.Item.Name != "Item" {
		t.Fatalf("associated projection = %#v", projection)
	}
	assertRoundTrip(t, file)
}

func TestAssociatedTypeRejectsDottedSelf(t *testing.T) {
	_, diagnostics := parser.ParseType([]byte("Self.Item"))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error && strings.Contains(diagnostic.Message, "associated types use ::") {
			return
		}
	}
	t.Fatalf("missing projection syntax diagnostic: %v", diagnostics)
}

func TestReceiverShorthandSyntax(t *testing.T) {
	const source = `package p
trait Counter {
 func value(self, first, second int) int
 func read(&self) int
 func write(&mut self, value int)
}
impl Counter for Number {
 func value(self, first, second int) int { return self.N + first + second }
 func read(&self) int { return self.N }
 func write(&mut self, value int) { self.N = value }
}
impl Number { func copy(self) Self { return self } }
type Number struct { N int }
`
	file, diagnostics := parser.ParseFile("receivers.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	trait := file.Items[0].(*ast.TraitDecl)
	impl := file.Items[1].(*ast.ImplDecl)
	for index := range trait.Items {
		for _, method := range []*ast.FuncDecl{trait.Items[index].(*ast.FuncDecl), impl.Items[index].(*ast.FuncDecl)} {
			param := method.Params[0]
			if len(param.Names) != 1 || param.Names[0].Name != "self" {
				t.Fatalf("%s receiver names = %v", method.Name.Name, param.Names)
			}
			typ := param.Type
			if index > 0 {
				ref, ok := typ.(*ast.RefType)
				if !ok || ref.Mut != (index == 2) {
					t.Fatalf("%s reference receiver = %#v", method.Name.Name, typ)
				}
				typ = ref.Elem
			}
			if _, ok := typ.(*ast.SelfType); !ok {
				t.Fatalf("%s receiver type = %T", method.Name.Name, typ)
			}
			if index == 0 && (len(method.Params) != 2 || len(method.Params[1].Names) != 2) {
				t.Fatalf("value receiver consumed the following grouped parameters: %#v", method.Params)
			}
		}
	}
	assertRoundTrip(t, file)
}

func TestReceiverShorthandDoesNotChangeOtherParameters(t *testing.T) {
	const source = `package p
type self int
func regular(self, other int) {}
func borrowed(&self) {}
type Function func(&self)
type Interface interface { read(&self) }
impl Number {
 func static(value int, other self) &self { return nil }
 func read(self &Self, callback func(&self)) &self { return nil }
 func escaped(r#self) {}
}
`
	file, diagnostics := parser.ParseFile("parameters.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	count := 0
	ast.Walk(file, func(node ast.Node) bool {
		if _, ok := node.(*ast.SelfType); ok {
			count++
		}
		return true
	})
	if count != 1 {
		t.Fatalf("ordinary self types were rewritten: found %d Self nodes", count)
	}
	regular := file.Items[1].(*ast.FuncDecl)
	if len(regular.Params) != 1 || len(regular.Params[0].Names) != 2 {
		t.Fatalf("ordinary grouped parameter changed: %#v", regular.Params)
	}
	assertRoundTrip(t, file)
}

func TestConditionalImplSyntax(t *testing.T) {
	const source = `package p
impl[T any] Copy for Box[T] where T: Copy + Show {
 func copy(self &Self) Self { return Self{} }
}
impl Copy for Record where Box[int]: Copy, Self: Show {
 func copy(self &Self) Self { return Self{} }
}
`
	file, diagnostics := parser.ParseFile("conditions.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	generic := file.Items[0].(*ast.ImplDecl)
	concrete := file.Items[1].(*ast.ImplDecl)
	if generic.Generics == nil || len(generic.Where.Predicates) != 1 || len(generic.Where.Predicates[0].Bounds) != 2 {
		t.Fatal("generic impl lost its combined prerequisites")
	}
	if concrete.Generics != nil || concrete.Where == nil || len(concrete.Where.Predicates) != 2 {
		t.Fatal("concrete impl lost its prerequisites")
	}
	if _, ok := concrete.Where.Predicates[1].Type.(*ast.SelfType); !ok {
		t.Fatalf("Self prerequisite = %T", concrete.Where.Predicates[1].Type)
	}
	assertRoundTrip(t, file)
}

func TestTraitPrerequisiteSyntax(t *testing.T) {
	const source = `package p
trait Equal where Self: Compare + Display {}
`
	file, diagnostics := parser.ParseFile("traits.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	trait := file.Items[0].(*ast.TraitDecl)
	if trait.Generics != nil || trait.Where == nil || len(trait.Where.Predicates) != 1 || len(trait.Where.Predicates[0].Bounds) != 2 {
		t.Fatal("trait lost its Self prerequisites")
	}
	assertRoundTrip(t, file)
}

func TestQualifiedStaticTraitInputSyntax(t *testing.T) {
	const source = `package p
func convert[T any, S any](value S) T where T: From[S] {
 make := T::from::<From[S]>
 a := pkg::Box[T]::from::<protocol::From[S]>(value)
 b := ([]T)::from::<From[S]>(value)
 c := value.into::<Into[T]>()
 return make(value)
}
`
	file, diagnostics := parser.ParseFile("qualified.gox", []byte(source))
	if errors := errorsOnly(diagnostics); len(errors) != 0 {
		t.Fatal(errors)
	}
	body := file.Items[0].(*ast.FuncDecl).Body
	for index, statement := range body.List[:4] {
		value := statement.(*ast.AssignStmt).Rhs[0]
		if call, ok := value.(*ast.CallExpr); ok {
			value = call.Fun
		}
		selector, ok := value.(*ast.QualifiedSelectorExpr)
		if !ok || selector.Static != (index < 3) {
			t.Fatalf("statement %d selector = %#v", index, value)
		}
	}
	assertRoundTrip(t, file)
}
