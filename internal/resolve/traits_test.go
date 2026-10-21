package resolve_test

import (
	"reflect"
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestAssociatedMemberPathsKeepStructEnumAndParameterIdentity(t *testing.T) {
	const source = `package p
type Box struct {}
enum Choice { One }
trait Source { type Item; func make() Self }
func f[T Source]() T::Source::Item {
 _ = Box::make
 _ = Choice::make
 _ = T::make
 return 0
}
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	seen := 0
	ast.Walk(files["p.gox"], func(node ast.Node) bool {
		path, ok := node.(*ast.Path)
		if !ok || len(path.Segments) < 2 {
			return true
		}
		var names []string
		for _, segment := range path.Segments {
			names = append(names, segment.Name.Name)
		}
		resolution := file.References[path.ID()]
		if !resolution.Deferred || resolution.Symbol == nil || resolution.Symbol.Name.Name != names[0] || !reflect.DeepEqual(resolution.Path, names) {
			t.Errorf("path %v lost its type identity: %#v", names, resolution)
		}
		seen++
		return true
	})
	if seen != 4 {
		t.Fatalf("checked %d associated paths, want 4", seen)
	}
}

func TestAssociatedFunctionHasNoImplicitSelf(t *testing.T) {
	const source = `package p
type Box struct {}
impl Box {
 func make() Self { _ = self; return Self{} }
 func read(self &Self) { f := func() { _ = self }; f() }
}
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("want only missing static self, got %v", result.Diagnostics)
	}
	self := named(t, files["p.gox"], source, "_ = self; return", "self")
	diagnostic := diagnosticAt(t, result, "p.gox", self.Span())
	if diagnostic.Code != "unresolved" {
		t.Fatalf("static self diagnostic = %s", diagnostic.Code)
	}
}

func TestImportedTypeAssociatedPathsDeferToChecker(t *testing.T) {
	const source = `package p
use model;
func f() { _ = model::Box::make; _ = model::Choice::Source::Item }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, testImporter{"model": {
		{Name: resolve.Key{Name: "Box"}, Kind: resolve.Type, GoName: "Box", Visibility: resolve.Public},
		{Name: resolve.Key{Name: "Choice"}, Kind: resolve.Enum, GoName: "Choice", Visibility: resolve.Public},
	}})
	noDiagnostics(t, result)
	for _, path := range [][]string{{"model", "Box", "make"}, {"model", "Choice", "Source", "Item"}} {
		name := path[len(path)-1]
		id := named(t, files["p.gox"], source, "::"+name, name)
		resolution := result.Files["p.gox"].References[id.ID()]
		if !resolution.Deferred || resolution.Symbol == nil || resolution.Symbol.Name.Name != path[1] || !reflect.DeepEqual(resolution.Path, path) {
			t.Fatalf("imported path %v lost its type identity: %#v", path, resolution)
		}
	}
}

func TestImplPrerequisitesUseImplScope(t *testing.T) {
	const source = `package p
trait Copy {}
trait Show {}
struct Box[T any] { value T }
impl[T any] Copy for Box[T] where T: Copy + Show, Self: Show {}
impl Show for Box[int] where Self: Copy {}
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	for _, item := range files["p.gox"].Items[3:] {
		impl := item.(*ast.ImplDecl)
		pred := impl.Where.Predicates[len(impl.Where.Predicates)-1]
		self := pred.Type.(*ast.SelfType)
		resolution := file.References[self.ID()]
		if resolution.Symbol == nil || resolution.Symbol.Decl.ID != impl.ID() {
			t.Fatalf("where Self does not bind to its impl: %#v", resolution)
		}
	}
}

func TestTraitPrerequisiteSelfBinding(t *testing.T) {
	const source = `package p
trait Compare {}
trait Equal where Self: Compare {}
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	trait := files["p.gox"].Items[1].(*ast.TraitDecl)
	self := trait.Where.Predicates[0].Type.(*ast.SelfType)
	resolution := result.Files["p.gox"].References[self.ID()]
	if resolution.Symbol == nil || resolution.Symbol.Decl.ID != trait.ID() {
		t.Fatalf("trait prerequisite Self lost its owner: %#v", resolution)
	}
}
