package parser

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestEnumStructVariantAndPattern(t *testing.T) {
	source := []byte(`package fixture
pub enum E { V{x int, y, z string} }
func f(e E) int { return match e { E::V{x: 1, y, ..} => len(y), _ => 0 } }
`)
	file, diagnostics := ParseFile("struct_variant.gox", source)
	requireClean(t, diagnostics)
	variant := file.Items[0].(*ast.EnumDecl).Variants[0]
	if variant.Kind != ast.Struct || len(variant.Fields) != 2 || len(variant.Fields[1].Names) != 2 {
		t.Fatalf("unexpected struct variant: %#v", variant)
	}
	ret := file.Items[1].(*ast.FuncDecl).Body.List[0].(*ast.ReturnStmt)
	pattern := ret.Results[0].(*ast.MatchExpr).Arms[0].Pat.(*ast.StructPat)
	if !pattern.Rest || len(pattern.Fields) != 2 {
		t.Fatalf("unexpected struct pattern: %#v", pattern)
	}
	if _, ok := pattern.Fields[0].Pat.(*ast.LitPat); !ok {
		t.Fatalf("constant field is %T", pattern.Fields[0].Pat)
	}
	bound := pattern.Fields[1].Pat.(*ast.BindPat)
	if bound.Name.Name != "y" || bound.Name.ID() == pattern.Fields[1].Name.ID() {
		t.Fatal("shorthand field must contain its own binding node")
	}
	start, end := pattern.Span().Start, pattern.Span().End
	if string(source[start:end]) != "E::V{x: 1, y, ..}" {
		t.Fatalf("pattern span is %d:%d: %q", start, end, source[start:end])
	}
}
