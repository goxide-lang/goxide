package typecheck

import (
	"errors"
	"fmt"
	"go/token"
	"go/types"
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

func TestGeneratedDiagnosticUsesDisplayNameWithoutSourceMapping(t *testing.T) {
	set := token.NewFileSet()
	file := set.AddFile("logical/main.gox", -1, 100)
	file.AddLine(20)
	original := types.Error{Fset: set, Pos: file.Pos(35), Msg: "undefined: logical/main.gox"}
	wrapped := fmt.Errorf("validation for logical/main.gox: %w", original)
	sources := map[string]DiagnosticSource{"logical/main.gox": {Name: "main.gox", Text: []byte("package p\n")}}
	formatted := SourceError(wrapped, sources, nil)
	const want = "validation for logical/main.gox: main.gox:2:16: undefined: logical/main.gox"
	if formatted.Error() != want {
		t.Fatalf("diagnostic = %q, want %q", formatted.Error(), want)
	}
	var retained types.Error
	if !errors.As(formatted, &retained) || retained.Fset != set || retained.Pos != original.Pos || set.Position(original.Pos).Filename != "logical/main.gox" {
		t.Fatal("display formatting mutated or discarded the generated position")
	}
	if SourceError(formatted, sources, nil) != formatted {
		t.Fatal("display formatting is not idempotent")
	}
	if SourceError(wrapped, nil, nil) != wrapped {
		t.Fatal("unmapped generated filename changed")
	}
}
