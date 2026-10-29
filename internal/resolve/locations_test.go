package resolve_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestResolveDisplayLocationPreservesDefinitionBinding(t *testing.T) {
	sources := map[string]string{
		"defs/macros.gox": `package definitions
var helper = 7
macro_rules! inner { () => { helper + missing }; }
macro_rules! outer { () => { inner!() }; }
`,
		"app/main.gox": "package app\nvar helper = 1\nvar missing = 2\nvar result = defs::outer!()\n",
	}
	files := parseFiles(t, sources)
	expanded := expandForResolution(t, files, []string{"app/main.gox"}, sourceMacroResolver{
		"defs::outer": files["defs/macros.gox"], "inner": files["defs/macros.gox"],
	})
	start := uint32(strings.Index(sources["app/main.gox"], "defs::outer!()"))
	display := expand.Origin{File: files["app/main.gox"], Span: ast.Span{Start: start, End: start + 13}}
	expanded.Locations = map[ast.Node]expand.Origin{}
	var helper, missing *ast.Ident
	originals := map[ast.Node]expand.Origin{}
	ast.Walk(expanded.Files[0], func(node ast.Node) bool {
		id, ok := node.(*ast.Ident)
		if !ok || id.Expn == 0 {
			return true
		}
		switch id.Name {
		case "helper":
			helper = id
		case "missing":
			missing = id
		default:
			return true
		}
		originals[id] = expanded.Origins[id]
		expanded.Locations[id] = display
		return true
	})
	if helper == nil || missing == nil {
		t.Fatal("missing expanded identifiers")
	}
	result := resolve.ResolveExpanded(files, expanded, nil)
	if len(result.Diagnostics) != 1 {
		t.Fatalf("expected unresolved definition name: %v", result.Diagnostics)
	}
	d := result.Diagnostics[0]
	if d.Code != "unresolved" || d.File != "app/main.gox" || d.Span != display.Span ||
		d.Definition != originals[missing] || !reflect.DeepEqual(d.Origin.Stack, []ast.ExpnID{1, 2}) {
		t.Fatalf("display location changed semantic diagnostic provenance: %#v", d)
	}
	file := result.Files["app/main.gox"]
	if symbol := file.References[helper.ID()].Symbol; symbol == nil || symbol.Decl.File != "defs/macros.gox" {
		t.Fatalf("display span changed definition binding: %#v", symbol)
	}
	for node, want := range originals {
		if expanded.Origins[node] != want {
			t.Fatal("display location replaced trusted origin")
		}
		got := result.Origins[resolve.Ref{File: file.Name, ID: node.ID()}]
		if got.File != "app/main.gox" || got.Span != display.Span || got.Expn == 0 {
			t.Fatalf("downstream location not preserved: %#v", got)
		}
	}
}

func TestResolveDisplayLocationPreservesCapturedBlockScope(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"defs/macros.gox": "package definitions\nmacro_rules! identity { ($x:expr) => { $x }; }\n",
		"app/main.gox": `package app
func run() {
	value := 1
	_ = defs::identity!(func() int { value := 2; return value }())
}
`,
	})
	expanded := expandForResolution(t, files, []string{"app/main.gox"}, sourceMacroResolver{
		"defs::identity": files["defs/macros.gox"],
	})
	expanded.Locations = map[ast.Node]expand.Origin{}
	originals := map[ast.Node]expand.Origin{}
	ast.Walk(expanded.Files[0], func(node ast.Node) bool {
		originals[node] = expanded.Origins[node]
		expanded.Locations[node] = expand.Origin{File: files["defs/macros.gox"], Span: ast.Span{Start: 0, End: 7}}
		return true
	})
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["app/main.gox"]
	ids := expansionIdents(file.AST, "value")
	if len(ids) != 3 {
		t.Fatalf("expected two bindings and one reference, got %d", len(ids))
	}
	inner := file.Definitions[ids[1].ID()]
	if inner == nil || inner == file.Definitions[ids[0].ID()] || file.References[ids[2].ID()].Symbol != inner {
		t.Fatal("diagnostic relocation changed captured block binding")
	}
	for node, want := range originals {
		if expanded.Origins[node] != want {
			t.Fatal("diagnostic relocation changed captured source containment")
		}
	}
}

func TestResolveAllocationDiagnosticUsesDisplayLocation(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"p.gox": `package p
macro_rules! allocate { ($x:expr) => { new($x) }; }
func run() { _ = allocate!(int) }
`,
	})
	expanded := expandForResolution(t, files, []string{"p.gox"}, nil)
	display := expand.Origin{File: files["p.gox"], Span: ast.Span{Start: 0, End: 7}}
	expanded.Locations = map[ast.Node]expand.Origin{}
	var argument ast.Node
	ast.Walk(expanded.Files[0], func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && expanded.Allocations[call] {
			argument = call.Args[0]
			expanded.Locations[argument] = display
		}
		return true
	})
	if argument == nil {
		t.Fatal("missing template allocation")
	}
	origin := expanded.Origins[argument]
	result := resolve.ResolveExpanded(files, expanded, nil)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "macro-allocation" {
		t.Fatalf("expected allocation category rejection: %v", result.Diagnostics)
	}
	d := result.Diagnostics[0]
	if d.Span != display.Span || d.Origin.Span != display.Span || expanded.Origins[argument] != origin {
		t.Fatalf("allocation diagnostic ignored display span or changed source: %#v", d)
	}
}
