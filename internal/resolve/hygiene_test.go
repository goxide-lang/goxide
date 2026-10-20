package resolve_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestResolveExpandedTemplatePreludeMember(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! some { () => { Option::Some(1) }; }
func run() { _ = some!() }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	var option *resolve.Symbol
	for _, name := range []string{"Option", "Some"} {
		ids := expansionIdents(file.AST, name)
		if len(ids) != 1 || ids[0].Ctx == 0 {
			t.Fatalf("template identifier %s = %#v", name, ids)
		}
		ref := file.References[ids[0].ID()]
		if ref.Symbol == nil || ref.Deferred {
			t.Fatalf("template %s did not resolve: %#v", name, ref)
		}
		if name == "Option" {
			option = ref.Symbol
		} else if option.Members == nil || option.Members.Bindings[resolve.Key{Name: "Some"}] != ref.Symbol || ref.Symbol.Kind != resolve.Variant {
			t.Fatalf("Some did not resolve to the predeclared Option member: %#v", ref)
		}
	}
}

func TestResolveExpandedNestedCallerIgnoresCapturedLocal(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! inner { ($n:ident) => { func() int { $n := 9; return $caller::value }() }; }
macro_rules! outer { () => { func() int { value := 3; return inner!(value) }() }; }
func run() { _ = outer!() }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	ids := expansionIdents(file.AST, "value")
	if len(ids) != 3 {
		t.Fatalf("outer binding, captured binding, caller reference = %d identifiers", len(ids))
	}
	outer, inner := file.Definitions[ids[0].ID()], file.Definitions[ids[1].ID()]
	if outer == nil || inner == nil || outer == inner || ids[0].Ctx == 0 || ids[0].Ctx != ids[1].Ctx || ids[1].Ctx != ids[2].Ctx {
		t.Fatalf("expected distinct declarations with one nonzero caller context: ids=%#v outer=%v inner=%v", ids, outer, inner)
	}
	if ref := file.References[ids[2].ID()]; ref.Symbol != outer {
		for _, id := range ids {
			t.Logf("value: Ctx=%d Expn=%d Scope=%#v Origin=%#v", id.Ctx, id.Expn, expanded.Scopes[id], expanded.Origins[id])
		}
		t.Fatalf("nested $caller::value resolved to %v, want caller binding %v (generated local %v)", ref.Symbol, outer, inner)
	}
}

func TestResolveExpandedCapturedExpressionIgnoresCapturedLocal(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		nested       bool
	}{
		{"source", `package p
macro_rules! inner { ($n:ident, $x:expr) => { func() int { $n := 9; return $x }() }; }
func run() { value := 3; _ = inner!(value, value) }
`, false},
		{"template", `package p
macro_rules! inner { ($n:ident, $x:expr) => { func() int { $n := 9; return $x }() }; }
macro_rules! outer { () => { func() int { value := 3; return inner!(value, value) }() }; }
func run() { _ = outer!() }
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p.gox": tc.source})
			expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
			result := resolve.ResolveExpanded(files, expanded, nil)
			noDiagnostics(t, result)
			file := result.Files["p.gox"]
			ids := expansionIdents(file.AST, "value")
			if len(ids) != 3 {
				t.Fatalf("caller binding, captured binding, expression reference = %d identifiers", len(ids))
			}
			outer, inner := file.Definitions[ids[0].ID()], file.Definitions[ids[1].ID()]
			if outer == nil || inner == nil || outer == inner || (ids[0].Ctx != 0) != tc.nested || ids[0].Ctx != ids[1].Ctx || ids[1].Ctx != ids[2].Ctx {
				t.Fatalf("expected distinct declarations with one caller context: ids=%#v outer=%v inner=%v", ids, outer, inner)
			}
			if ref := file.References[ids[2].ID()]; ref.Symbol != outer {
				for _, id := range ids {
					t.Logf("value: Ctx=%d Expn=%d Scope=%#v Origin=%#v", id.Ctx, id.Expn, expanded.Scopes[id], expanded.Origins[id])
				}
				t.Fatalf("captured expression resolved to %v, want caller binding %v (generated local %v)", ref.Symbol, outer, inner)
			}
		})
	}
}

func TestResolveExpandedTemplateNestedBlocksShadowAndRestore(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": `package p
macro_rules! blocks { () => { func() int {
 value := 1
 { value := 2; _ = value; { value := 3; _ = value }; _ = value }
 return value
}() }; }
func run() { value := 4; _ = blocks!(); _ = value }
`})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	var defs []*resolve.Symbol
	var refs []*resolve.Symbol
	var contexts []ast.SyntaxCtx
	for _, id := range expansionIdents(file.AST, "value") {
		if symbol := file.Definitions[id.ID()]; symbol != nil {
			defs = append(defs, symbol)
			contexts = append(contexts, id.Ctx)
		} else if ref, ok := file.References[id.ID()]; ok {
			refs = append(refs, ref.Symbol)
		}
	}
	if len(defs) != 4 || len(refs) != 5 {
		t.Fatalf("declarations/references = %d/%d", len(defs), len(refs))
	}
	if contexts[0] != 0 || contexts[1] == 0 || contexts[1] != contexts[2] || contexts[2] != contexts[3] {
		t.Fatalf("source/template declaration contexts = %v", contexts)
	}
	for i := range defs {
		for j := 0; j < i; j++ {
			if defs[i] == defs[j] {
				t.Fatalf("lexical declarations %d and %d shared a symbol", i, j)
			}
		}
	}
	want := []*resolve.Symbol{defs[2], defs[3], defs[2], defs[1], defs[0]}
	for i, symbol := range refs {
		if symbol != want[i] {
			t.Fatalf("nested block reference %d = %v, want %v", i, symbol, want[i])
		}
	}
}

func TestResolveExpandedCapturedFragmentKeepsLocalScopes(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"expression", `package p
var local = 7
macro_rules! identity { ($e:expr) => { $e }; }
func run() { _ = identity!(func() int { local := 2; return local }()) }
`},
		{"item", `package p
var local = 7
macro_rules! identity { ($i:item) => { $i }; }
identity!(func generated() int { local := 2; return local })
func run() { _ = generated() }
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p.gox": tc.source})
			expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
			result := resolve.ResolveExpanded(files, expanded, nil)
			noDiagnostics(t, result)
			file := result.Files["p.gox"]
			ids := expansionIdents(file.AST, "local")
			if len(ids) != 3 {
				t.Fatalf("package binding, fragment binding, fragment reference = %d identifiers", len(ids))
			}
			outer, inner := file.Definitions[ids[0].ID()], file.Definitions[ids[1].ID()]
			if outer == nil || inner == nil || outer == inner {
				t.Fatalf("package/fragment bindings are not distinct: %v/%v", outer, inner)
			}
			if ref := file.References[ids[2].ID()]; ref.Symbol != inner {
				t.Fatalf("captured fragment local resolved to %v, want own declaration %v (package declaration %v)", ref.Symbol, inner, outer)
			}
		})
	}
}

func TestResolveExpandedEnumMembersAcrossContexts(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		templateRef  bool
	}{
		{"generated declaration", `package p
macro_rules! emit_enum { () => { enum E { A } }; }
emit_enum!()
func run() { _ = E::A }
`, false},
		{"template reference", `package p
enum E { A }
macro_rules! get { () => { E::A }; }
func run() { _ = get!() }
`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p.gox": tc.source})
			expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
			result := resolve.ResolveExpanded(files, expanded, nil)
			noDiagnostics(t, result)
			file := result.Files["p.gox"]
			for _, name := range []string{"E", "A"} {
				ids := expansionIdents(file.AST, name)
				if len(ids) != 2 || (ids[1].Ctx != 0) != tc.templateRef {
					t.Fatalf("enum %s declaration/reference = %#v", name, ids)
				}
				symbol := file.Definitions[ids[0].ID()]
				if ref := file.References[ids[1].ID()]; symbol == nil || ref.Symbol != symbol || ref.Deferred {
					t.Fatalf("enum %s reference = %#v, want declaration %v", name, ref, symbol)
				}
			}
		})
	}
}

func TestResolveExpandedMethodsAttachToWrittenType(t *testing.T) {
	for _, tc := range []struct{ name, declaration, method, receiver string }{
		{"impl", "impl S { pub func build(self Self) { _ = self; _ = Self::build } }", "build", "self"},
		{"Go receiver", "func (s S) Build() { _ = s; _ = S::Build }", "Build", "s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "package p\nstruct S {}\nmacro_rules! methods { () => { " + tc.declaration + " }; }\nmethods!()\nfunc run() { _ = S::" + tc.method + " }\n"
			files := parseFiles(t, map[string]string{"p.gox": source})
			expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
			result := resolve.ResolveExpanded(files, expanded, nil)
			noDiagnostics(t, result)
			file := result.Files["p.gox"]
			ids := expansionIdents(file.AST, tc.method)
			if len(ids) != 3 {
				t.Fatalf("method declaration, template reference, caller reference = %d identifiers", len(ids))
			}
			method := file.Definitions[ids[0].ID()]
			owner := result.Packages[0].Scope.Bindings[resolve.Key{Name: "S"}]
			if method == nil || method.Kind != resolve.Function || owner == nil || owner.Members == nil || owner.Members.Bindings[resolve.Key{Name: tc.method}] != method {
				t.Fatalf("generated method %v did not attach to written type %v", method, owner)
			}
			for _, id := range ids[1:] {
				if ref := file.References[id.ID()]; ref.Symbol != method || ref.Deferred {
					t.Fatalf("method reference at context %d = %#v, want generated method %v", id.Ctx, ref, method)
				}
			}
			var receivers int
			for _, id := range expansionIdents(file.AST, tc.receiver) {
				if ref, ok := file.References[id.ID()]; ok {
					receivers++
					if ref.Symbol == nil || ref.Symbol.Kind != resolve.Variable || ref.Deferred {
						t.Fatalf("method receiver %s did not resolve: %#v", tc.receiver, ref)
					}
				}
			}
			if receivers != 1 {
				t.Fatalf("method receiver references = %d", receivers)
			}
		})
	}
}
