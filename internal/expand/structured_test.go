package expand

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func TestExpandKeepsImplicitMatchArmSemicolon(t *testing.T) {
	file := expandFile(t, "match.gox", `
macro_rules! choose { ($value:expr) => {
	match $value {
		_ => 42
	}
}; }
var answer = choose!(1)
`)
	result := expandFiles(t, file)
	match, ok := expandValue(t, result.Files[0], "answer").(*ast.MatchExpr)
	if !ok || len(match.Arms) != 1 {
		t.Fatalf("expected complete match expression, got %#v", match)
	}
	// An explicit semicolon has a different grammar role and remains rejected.
	_, err := adapterExpand(t, `macro_rules! choose { ($value:expr) => { match $value { _ => 42; } }; }
var answer = choose!(1)`)
	if err == nil {
		t.Fatal("explicit match-arm semicolon became implicit")
	}
}

func TestSkeletonPreservesLexicalUnits(t *testing.T) {
	input := []token.Token{
		{Kind: token.IDENT, Text: "type", Raw: true},
		{Kind: token.ADD_ASSIGN, Text: "+="},
		{Kind: token.SUB, Text: "-"},
		{Kind: token.INT, Text: "0x2a"},
		{Kind: token.SEMICOLON, Text: "\n", Implicit: true},
	}
	var tokens []syntaxToken
	for i := range input {
		tokens = append(tokens, syntaxToken{Token: &input[i]})
	}
	s := &skeleton{trees: map[uint32]*syntaxTree{}}
	s.write(tokens, false, Origin{}, Origin{})
	if len(s.tokens) != len(input) {
		t.Fatalf("lexical unit count changed: %v", s.tokens)
	}
	for i, got := range s.tokens {
		want := input[i]
		if got.Kind != want.Kind || got.Text != want.Text || got.Raw != want.Raw || got.Implicit != want.Implicit {
			t.Fatalf("token %d changed: got %#v, want %#v", i, got, want)
		}
	}
}

func TestGroupAndDelimiterLocationsAreIndependent(t *testing.T) {
	source, display := &ast.File{}, &ast.File{}
	origin := Origin{source, ast.Span{Start: 10, End: 13}}
	whole := Origin{display, ast.Span{Start: 100, End: 110}}
	open := Origin{display, ast.Span{Start: 20, End: 21}}
	close := Origin{display, ast.Span{Start: 30, End: 31}}
	literal := syntaxToken{Token: &token.Token{Kind: token.INT, Text: "1"}, Origin: Origin{source, ast.Span{Start: 11, End: 12}}}
	tree := &syntaxTree{Delim: ast.Paren, Origin: origin, Location: whole, Open: open, Close: close, Tokens: []syntaxToken{literal}}
	e := &engine{r: &Result{Origins: map[ast.Node]Origin{}, Scopes: map[*ast.Ident]Scope{}}}
	node, err := e.parse([]syntaxToken{{Tree: tree, Origin: origin, Location: whole}}, exprKind, 0, Scope{})
	if err != nil {
		t.Fatal(err)
	}
	paren := node.(*ast.ParenExpr)
	if e.location(paren) != whole || e.r.Origins[paren] != origin || e.location(paren.X) != literal.Origin {
		t.Fatal("whole-group span was replaced by its delimiter spans or changed a child")
	}
	// An error at the closing delimiter still uses its own display position.
	empty := *tree
	empty.Tokens = nil
	_, err = e.parse([]syntaxToken{{Tree: &empty, Origin: origin, Location: whole}}, exprKind, 0, Scope{})
	failure, ok := err.(*parseError)
	if !ok || failure.origin != close {
		t.Fatalf("closing delimiter diagnostic = %#v, want %#v", err, close)
	}
}
