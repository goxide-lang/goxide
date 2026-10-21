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
declare impl Source[Item = int] for Native;
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
