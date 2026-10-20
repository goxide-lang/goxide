package lower_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestExpansionDiagnosticDisplayLocationKeepsTrace(t *testing.T) {
	files := parseDiagnosticSources(t, map[string]string{
		"defs.gox": "package p\nmacro_rules! value { () => { 1 }; }\n",
		"call.gox": "package p\nvar value = 0\n",
	})
	definition := expand.Origin{File: files["defs.gox"], Span: ast.Span{Start: 10, End: 43}}
	call := expand.Origin{File: files["call.gox"], Span: ast.Span{Start: 10, End: 23}}
	display := expand.Origin{File: files["call.gox"], Span: ast.Span{Start: 14, End: 19}}
	for _, tc := range []struct {
		name     string
		location expand.Origin
		want     string
	}{
		{"override", display, "call.gox:2:5: rejected"},
		{"fallback", expand.Origin{}, "defs.gox:2:1: rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagnostic := &expand.Diagnostic{
				Code: "category", Message: "rejected", Definition: definition, Call: call, Location: tc.location,
			}
			formatted := lower.FormatSourceDiagnostic(diagnostic, files, nil, nil)
			if !strings.HasPrefix(formatted.Error(), tc.want) ||
				!strings.Contains(formatted.Error(), "macro definition defs.gox:2:1; call call.gox:2:1") {
				t.Fatalf("wrong display or lost trace: %v", formatted)
			}
			var original *expand.Diagnostic
			if !errors.As(formatted, &original) || original != diagnostic ||
				original.Definition != definition || original.Call != call {
				t.Fatal("formatting changed trusted expansion provenance")
			}
		})
	}
}

func TestLowerFailureUsesResolvedDisplayLocation(t *testing.T) {
	files := parseDiagnosticSources(t, map[string]string{
		"source.gox":  "package p\nfunc f() { r#type := 1; _ = r#type }\n",
		"display.gox": "package p\nvar shown = 1\n",
	})
	expanded, err := expand.Expand([]*ast.File{files["source.gox"], files["display.gox"]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	display := expand.Origin{File: files["display.gox"], Span: ast.Span{Start: 14, End: 19}}
	expanded.Locations = map[ast.Node]expand.Origin{}
	var source *ast.Ident
	for _, file := range expanded.Files {
		ast.Walk(file, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && id.Raw && id.Name == "type" {
				expanded.Locations[id] = display
				source = id
			}
			return true
		})
	}
	if source == nil {
		t.Fatal("missing raw Go keyword")
	}
	origin := expanded.Origins[source]
	resolved := resolve.ResolveExpanded(files, expanded, nil)
	trees := map[string]*ast.File{}
	for name, file := range resolved.Files {
		trees[name] = file.AST
	}
	output, err := lower.Lower(trees, checked(t, trees, resolved))
	var diagnostic *lower.Diagnostic
	if output != nil || !errors.As(err, &diagnostic) {
		t.Fatalf("expected located lower failure: output=%v, error=%v", output, err)
	}
	if diagnostic.File != "display.gox" || diagnostic.Span != display.Span ||
		diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != display.Span {
		t.Fatalf("lower failure ignored resolved display location: %#v", diagnostic)
	}
	if expanded.Origins[source] != origin {
		t.Fatal("lower failure changed trusted source")
	}
}

func TestLowerRejectsDiagnosticDisplayedOutsideOutputFiles(t *testing.T) {
	files := parseDiagnosticSources(t, map[string]string{"p.gox": "package p\nfunc okay() {}\n"})
	resolved := resolve.Resolve(files, nil)
	info := checked(t, files, resolved)
	location := resolve.Origin{File: "macro-definition.gox", Span: ast.Span{Start: 3, End: 8}}
	resolved.Diagnostics = []resolve.Diagnostic{{
		File: location.File, Span: location.Span, Origin: location,
		Code: "unresolved", Message: "missing generated name", Severity: resolve.Error,
	}}
	output, err := lower.Lower(files, info)
	var diagnostic *lower.Diagnostic
	if output != nil || !errors.As(err, &diagnostic) || diagnostic.Origin.File != location.File || diagnostic.Origin.Span != location.Span {
		t.Fatalf("diagnostic in a non-output file was lost: output=%v, error=%v", output, err)
	}
}
