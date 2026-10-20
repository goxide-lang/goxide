package parser_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/lexer"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type fragmentParser struct {
	name   string
	text   func([]byte) (ast.Node, []parser.Diagnostic)
	tokens func([]token.Token) (ast.Node, []parser.Diagnostic)
}

func fragmentParsers() []fragmentParser {
	return []fragmentParser{
		{
			name:   "expr",
			text:   func(src []byte) (ast.Node, []parser.Diagnostic) { return parser.ParseExpr(src) },
			tokens: func(ts []token.Token) (ast.Node, []parser.Diagnostic) { return parser.ParseExprTokens(ts) },
		},
		{
			name:   "type",
			text:   func(src []byte) (ast.Node, []parser.Diagnostic) { return parser.ParseType(src) },
			tokens: func(ts []token.Token) (ast.Node, []parser.Diagnostic) { return parser.ParseTypeTokens(ts) },
		},
		{
			name:   "stmt",
			text:   func(src []byte) (ast.Node, []parser.Diagnostic) { return parser.ParseStmt(src) },
			tokens: func(ts []token.Token) (ast.Node, []parser.Diagnostic) { return parser.ParseStmtTokens(ts) },
		},
		{
			name:   "item",
			text:   func(src []byte) (ast.Node, []parser.Diagnostic) { return parser.ParseItem(src) },
			tokens: func(ts []token.Token) (ast.Node, []parser.Diagnostic) { return parser.ParseItemTokens(ts) },
		},
	}
}

func lexicalTokens(t *testing.T, source string) []token.Token {
	t.Helper()
	tokens, diagnostics := lexer.Lex([]byte(source))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	return tokens
}

func TestStructuredTokensMatchTextParsing(t *testing.T) {
	sources := map[string][]string{
		"expr": {
			"pkg::Value(-42) >> 1",
			"r#type + r#match",
			"name!(r#type, [a + b], {$value})",
			"match value { _ => 42\n}",
		},
		"type": {"map[string][]*pkg::Value", "&mut r#type", "func(int, ...string) (int, error)"},
		"stmt": {"value >>= 2", "values = append(values, rest...)", "{ value++; return -value }"},
		"item": {"pub func r#type(value int) int { return -value }", "pub enum State { Empty, Value(int) }"},
	}
	for _, entry := range fragmentParsers() {
		for _, source := range sources[entry.name] {
			t.Run(entry.name+"/"+source, func(t *testing.T) {
				want, wantDiagnostics := entry.text([]byte(source))
				if len(errorsOnly(wantDiagnostics)) != 0 {
					t.Fatal(wantDiagnostics)
				}
				tokens := lexicalTokens(t, source)
				for _, length := range []int{len(tokens), len(tokens) - 1} {
					got, diagnostics := entry.tokens(tokens[:length])
					if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(diagnostics, wantDiagnostics) {
						t.Fatalf("structured parse with %d tokens differs from source: diagnostics=%v; want=%v", length, diagnostics, wantDiagnostics)
					}
				}
			})
		}
	}
}

func TestStructuredTokensPreserveCoordinatesAndRawName(t *testing.T) {
	tokens := []token.Token{
		{Kind: token.IDENT, Text: "type", Raw: true, Start: 100, End: 106},
		{Kind: token.ADD, Text: "+", Start: 110, End: 111},
		{Kind: token.SUB, Text: "-", Start: 120, End: 121},
		{Kind: token.INT, Text: "42", Start: 121, End: 123},
	}
	expr, diagnostics := parser.ParseExprTokens(tokens)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	binary := expr.(*ast.BinaryExpr)
	name := binary.X.(*ast.Ident)
	negative := binary.Y.(*ast.UnaryExpr)
	literal := negative.X.(*ast.BasicLit)
	if name.Name != "type" || !name.Raw || name.Span() != (ast.Span{Start: 100, End: 106}) ||
		negative.Op != token.SUB || literal.Kind != token.INT || literal.Value != "42" ||
		negative.Span() != (ast.Span{Start: 120, End: 123}) || expr.Span() != (ast.Span{Start: 100, End: 123}) {
		t.Fatalf("token spelling, kind or coordinates changed: %#v / %#v / %#v", name, negative, literal)
	}
}

func TestStructuredImplicitSemicolonControlsMatchArm(t *testing.T) {
	tokens := lexicalTokens(t, "match value { _ => 42\n}")
	semicolon := -1
	for i, tok := range tokens[:len(tokens)-1] {
		if tok.Kind == token.SEMICOLON && tokens[i+1].Kind == token.RBRACE {
			semicolon = i
			break
		}
	}
	if semicolon < 0 || !tokens[semicolon].Implicit {
		t.Fatal("fixture has no implicit final-arm semicolon")
	}
	node, diagnostics := parser.ParseExprTokens(tokens)
	if len(diagnostics) != 0 || len(node.(*ast.MatchExpr).Arms) != 1 {
		t.Fatalf("implicit semicolon should end the final match arm: %v", diagnostics)
	}
	tokens[semicolon].Implicit = false
	_, diagnostics = parser.ParseExprTokens(tokens)
	if len(diagnostics) == 0 || diagnostics[0].Message != "expected comma between match arms" ||
		diagnostics[0].Span != (ast.Span{Start: tokens[semicolon].Start, End: tokens[semicolon].End}) {
		t.Fatalf("explicit semicolon must not acquire newline semantics: %v", diagnostics)
	}
}

func TestStructuredTokenDiagnosticsAndEOF(t *testing.T) {
	incomplete := map[string]string{"expr": "call(", "type": "map[", "stmt": "if", "item": "func"}
	for _, entry := range fragmentParsers() {
		t.Run(entry.name, func(t *testing.T) {
			for _, tokens := range [][]token.Token{nil, {{Kind: token.EOF, Start: 77, End: 77}}} {
				_, diagnostics := entry.tokens(tokens)
				var end uint32
				if len(tokens) != 0 {
					end = 77
				}
				if len(errorsOnly(diagnostics)) == 0 || diagnostics[0].Span != (ast.Span{Start: end, End: end}) {
					t.Fatalf("empty input must diagnose at EOF %d: %v", end, diagnostics)
				}
			}
			tokens := lexicalTokens(t, incomplete[entry.name])
			tokens = tokens[:len(tokens)-1]
			for i := range tokens {
				tokens[i].Start += 200
				tokens[i].End += 200
			}
			end := tokens[len(tokens)-1].End
			_, diagnostics := entry.tokens(tokens)
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Severity == parser.Error && diagnostic.Span == (ast.Span{Start: end, End: end}) {
					found = true
				}
			}
			if !found {
				t.Fatalf("incomplete input must diagnose at appended EOF %d: %v", end, diagnostics)
			}
		})
	}

	trailing := []token.Token{
		{Kind: token.IDENT, Text: "first", Start: 10, End: 15},
		{Kind: token.IDENT, Text: "second", Start: 40, End: 46},
	}
	_, diagnostics := parser.ParseExprTokens(trailing)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "after syntax fragment") ||
		diagnostics[0].Span != (ast.Span{Start: 40, End: 46}) {
		t.Fatalf("trailing token diagnostic lost its coordinates: %v", diagnostics)
	}
}

func TestStructuredTokenInputIsNotModified(t *testing.T) {
	for _, entry := range fragmentParsers() {
		t.Run(entry.name, func(t *testing.T) {
			source := map[string]string{
				"expr": "name!(r#type)", "type": "[]r#type", "stmt": "value >>= 2", "item": "type r#type int",
			}[entry.name]
			tokens := lexicalTokens(t, source)
			for _, length := range []int{len(tokens), len(tokens) - 1} {
				// A spare backing-array element detects EOF append writes even
				// when the caller's input slice does not include that element.
				backing := make([]token.Token, len(tokens)+1)
				copy(backing, tokens)
				backing[length] = token.Token{Kind: token.IDENT, Text: "sentinel", Start: 999, End: 1007}
				before := append([]token.Token(nil), backing...)
				_, diagnostics := entry.tokens(backing[:length])
				if len(errorsOnly(diagnostics)) != 0 {
					t.Fatal(diagnostics)
				}
				if !reflect.DeepEqual(backing, before) {
					t.Fatal("parser modified caller tokens or spare capacity")
				}
			}
		})
	}
}
