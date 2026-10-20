package typecheck

import (
	"errors"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestSourceDiagnosticPositions(t *testing.T) {
	source := []byte("package p\r\n// 日本語\r\nfunc broken() {}\n")
	span := syntax.Span{Start: uint32(strings.Index(string(source), "broken"))}
	tree, diagnostics := parser.ParseFile("main.gox", source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	sources := map[string]DiagnosticSource{"logical/main.gox": {Name: "main.gox", Text: source, AST: tree}}
	cases := []error{
		ParserError("main.gox", source, parser.Diagnostic{Span: span, Message: "broken"}),
		SourceError(check.Diagnostic{File: "expanded", Origin: resolve.Origin{File: "logical/main.gox", Span: span}, Message: "broken"}, sources, nil),
		SourceError(&expand.Diagnostic{Code: "macro", Message: "broken", Call: expand.Origin{File: tree, Span: span}}, sources, nil),
	}
	for _, err := range cases {
		if !strings.HasPrefix(err.Error(), "main.gox:3:6:") || !strings.Contains(err.Error(), "broken") {
			t.Fatal(err)
		}
	}
	var diagnostic check.Diagnostic
	if !errors.As(cases[1], &diagnostic) {
		t.Fatal("formatted source error lost its original diagnostic")
	}
	generated := errors.New("generated.go:17:4: native type error")
	if SourceError(generated, sources, nil) != generated {
		t.Fatal("invented a source mapping for generated Go")
	}
}
