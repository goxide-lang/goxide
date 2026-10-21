package expand_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestUnconsumedAttributesReachSemanticChecking(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"source", "#[no_mangle] func value() {}"},
		{"generated", "macro_rules! emit { () => { #[no_mangle] func value() {} }; }\nemit!()"},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, diagnostics := parser.ParseFile("unconsumed.gox", []byte("package p\n"+test.source))
			if len(diagnostics) != 0 {
				t.Fatal(diagnostics)
			}
			expanded, err := expand.Expand([]*ast.File{file}, nil)
			if err != nil {
				t.Fatal(err)
			}
			declaration := expanded.Files[0].Items[0].(*ast.FuncDecl)
			if len(declaration.Attrs) != 1 || resolve.AttributeName(declaration.Attrs[0]) != "no_mangle" {
				t.Fatal("expansion consumed an attribute without a consumer")
			}
			resolved := resolve.ResolveExpanded(map[string]*ast.File{"unconsumed.gox": file}, expanded, nil)
			if len(resolved.Diagnostics) != 0 {
				t.Fatal(resolved.Diagnostics)
			}
			trees := map[string]*ast.File{}
			for name, source := range resolved.Files {
				trees[name] = source.AST
			}
			_, checked := check.Check(trees, resolved)
			if len(checked) != 1 || checked[0].Code != check.DeriveInvalid || !strings.Contains(checked[0].Message, "unsupported attribute or invalid attribute position: no_mangle") {
				t.Fatalf("unconsumed attribute was accepted or misdiagnosed: %v", checked)
			}
		})
	}
}
