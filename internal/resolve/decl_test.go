package resolve_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestTypeCarrierAnchorReservedInSourceAndMixedPackages(t *testing.T) {
	for _, tc := range []struct {
		name, declaration string
		native, mixed     bool
		want              int
	}{
		{"source", "func GoxideInternalTypeCarrierForSelf() {}", false, false, 1},
		{"method", "type T struct{}; func (t T) GoxideInternalTypeCarrierForSelf() {}", false, false, 1},
		{"mixed", "func GoxideInternalTypeCarrierForSelf() {}", true, true, 1},
		{"independent-go", "func GoxideInternalTypeCarrierForSelf() {}", true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p/anchor.gox": "package p; " + tc.declaration})
			files["p/anchor.gox"].NativeGo = tc.native
			if tc.mixed {
				for name, file := range parseFiles(t, map[string]string{"p/source.gox": "package p; func Run() {}"}) {
					files[name] = file
				}
			}
			result := resolve.Resolve(files, nil)
			count := 0
			for _, diagnostic := range result.Diagnostics {
				if diagnostic.Code == "reserved" {
					count++
				}
			}
			if count != tc.want || len(result.Diagnostics) != tc.want {
				t.Fatalf("reserved diagnostics = %v, want %d", result.Diagnostics, tc.want)
			}
		})
	}
}

func TestReceiverAndImplMembersAcrossFiles(t *testing.T) {
	sources := map[string]string{
		"a.gox": `package p
func (x *Target) Method() {}
impl Target { pub func build() {} }
func inspect() { _ = Target::Method; _ = Target::build }
`,
		"b.gox": "package p\nstruct Target {}\n",
	}
	files := parseFiles(t, sources)
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	target := named(t, files["b.gox"], sources["b.gox"], "Target {}", "Target")
	symbol := result.Files["b.gox"].Definitions[target.ID()]
	for _, method := range []string{"Method", "build"} {
		member := symbol.Members.Bindings[resolve.Key{Name: method}]
		if member == nil || member.Visibility != resolve.Public {
			t.Fatalf("missing public receiver member %q: %#v", method, member)
		}
	}
}

func TestTraitImplMembersInheritAndHaveSeparateScopes(t *testing.T) {
	source := `package p
trait Hidden { func Read(); type Value }
pub trait visible { func Read(); type Value }
struct Target {}
impl Hidden for Target { func Read() {}; type Value = int }
impl visible for Target { func Read() {}; type Value = int }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	for _, tc := range []struct {
		marker     string
		visibility resolve.Visibility
		goName     string
	}{
		{"impl Hidden for Target { func Read", resolve.Private, "_Read"},
		{"impl visible for Target { func Read", resolve.Public, "Read"},
	} {
		name := named(t, files["p.gox"], source, tc.marker, "Read")
		if file.Visibility[name.ID()] != tc.visibility || file.GoNames[name.ID()] != tc.goName {
			t.Fatalf("trait impl visibility/name for %q = %v/%q", tc.marker, file.Visibility[name.ID()], file.GoNames[name.ID()])
		}
	}
}

func TestGoMembersAndPrivateNewMembersConflict(t *testing.T) {
	for _, body := range []string{
		"struct Target { Same int }; func (x Target) Same() {}",
		"struct Target {}; impl Target { pub func lower() {}; func Lower() {} }",
		"pub enum Kind { lower, Lower }",
		"struct Target { pub lower int; Lower int }",
	} {
		t.Run(body, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p.gox": "package p\n" + body})
			result := resolve.Resolve(files, nil)
			if len(result.Diagnostics) == 0 {
				t.Fatal("member name conflict accepted")
			}
		})
	}
}

func TestGroupedDeclarationsPropagatePub(t *testing.T) {
	source := `package p
pub type ( first int; Second int )
pub var ( value int; Other int )
const ( pub count = 1; private = 2 )
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	for _, name := range []string{"first", "Second", "value", "Other", "count"} {
		symbol := result.Packages[0].Scope.Bindings[resolve.Key{Name: name}]
		if symbol == nil || symbol.Visibility != resolve.Public {
			t.Fatalf("grouped declaration %q is not public", name)
		}
	}
	if result.Packages[0].Scope.Bindings[resolve.Key{Name: "private"}].Visibility != resolve.Private {
		t.Fatal("a pub specification leaked visibility to the next specification")
	}
}

func TestTupleFieldsDoNotDeclareNames(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": "package p\npub enum Pair { two(int, int) }"})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	variant := result.Packages[0].Scope.Bindings[resolve.Key{Name: "Pair"}].Members.Bindings[resolve.Key{Name: "two"}]
	if variant.Members != nil && len(variant.Members.Bindings) != 0 {
		t.Fatal("unnamed tuple fields were treated as embedded struct fields")
	}
}

func TestInitFunctionsDoNotBindPackageNames(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"a.gox": "package p\nfunc init() {}\n",
		"b.gox": "package p\nfunc init() {}\n",
	})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	if result.Packages[0].Scope.Bindings[resolve.Key{Name: "init"}] != nil {
		t.Fatal("init must not introduce a referencable name")
	}
}

func TestLocalValueDeclarationsRemainPrivate(t *testing.T) {
	source := "package p\nfunc f() { var ( Upper int; Other int ); const Value = 1 }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	ast.Walk(files["p.gox"], func(node ast.Node) bool {
		switch node.(type) {
		case *ast.VarDecl, *ast.ConstDecl, *ast.ValueSpec:
			if visibility, ok := file.Visibility[node.ID()]; !ok || visibility != resolve.Private {
				t.Errorf("local declaration %T has visibility %v (present %v)", node, visibility, ok)
			}
		}
		return true
	})
}

func TestTraitImplThroughLocalUseInheritsTargetVisibility(t *testing.T) {
	source := `package p
trait Hidden { func Read() }
use Hidden as Alias;
struct Target {}
impl Alias for Target { func Read() {} }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	method := named(t, files["p.gox"], source, "impl Alias for Target { func Read", "Read")
	file := result.Files["p.gox"]
	if file.Visibility[method.ID()] != resolve.Private || file.GoNames[method.ID()] != "_Read" {
		t.Fatal("local trait alias lost the trait's inherited visibility")
	}
}

func TestModMemberLookupDefersToImporter(t *testing.T) {
	source := "package p\nmod child;\nfunc f() { _ = child::Member }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	member := named(t, files["p.gox"], source, "child::Member", "Member")
	reference := result.Files["p.gox"].References[member.ID()]
	if !reference.Deferred || len(reference.Path) != 2 || reference.Path[0] != "child" || reference.Path[1] != "Member" {
		t.Fatalf("submodule member did not defer: %#v", reference)
	}
}
