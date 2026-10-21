package parser_test

import (
	"reflect"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/lexer"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func TestFileRetainsSourceTokensForProceduralInputs(t *testing.T) {
	source := []byte("package p\n// source comment\nuse pkg::r#type;\nfunc run() { value >>= 1 /* body comment */\ncall(rest...)\n}\n")
	file, diagnostics := parser.ParseFile("tokens.gox", source)
	if len(errorsOnly(diagnostics)) != 0 {
		t.Fatal(diagnostics)
	}
	lexed, lexicalDiagnostics := lexer.Lex(source)
	if len(lexicalDiagnostics) != 0 {
		t.Fatal(lexicalDiagnostics)
	}
	var expected []token.Token
	for _, tok := range lexed {
		if tok.Kind != token.COMMENT {
			expected = append(expected, tok)
		}
	}
	if !reflect.DeepEqual(file.Tokens, expected) {
		t.Fatal("file source token view changed token spelling, flags, coordinates or EOF")
	}
	var raw, implicit, explicit bool
	compound := map[token.Kind]bool{token.DCOLON: false, token.SHR_ASSIGN: false, token.ELLIPSIS: false}
	for _, tok := range file.Tokens {
		if tok.Raw && tok.Kind == token.IDENT && tok.Text == "type" {
			raw = string(source[tok.Start:tok.End]) == "r#type"
		}
		if tok.Kind == token.SEMICOLON {
			implicit = implicit || tok.Implicit
			explicit = explicit || !tok.Implicit
		}
		if _, ok := compound[tok.Kind]; ok {
			compound[tok.Kind] = tok.Text == string(source[tok.Start:tok.End])
		}
	}
	for kind, found := range compound {
		if !found {
			t.Errorf("compound token %s was not retained as one unit", kind)
		}
	}
	eof := file.Tokens[len(file.Tokens)-1]
	if !raw || !implicit || !explicit || eof.Kind != token.EOF || eof.Start != uint32(len(source)) || eof.End != eof.Start || len(file.Comments) == 0 {
		t.Fatalf("raw=%v implicit=%v explicit=%v EOF=%+v comments=%d", raw, implicit, explicit, eof, len(file.Comments))
	}
}

func TestProceduralItemTokenLists(t *testing.T) {
	for _, input := range [][]token.Token{nil, {{Kind: token.EOF, Start: 30, End: 30}}} {
		items, diagnostics := parser.ParseItemsTokens(input)
		if len(items) != 0 || len(errorsOnly(diagnostics)) != 0 {
			t.Fatalf("empty item list = %v / %v", items, diagnostics)
		}
	}
	tokens := lexicalTokens(t, "type r#type int; func run() {}; const Count = 2")
	for _, length := range []int{len(tokens), len(tokens) - 1} {
		items, diagnostics := parser.ParseItemsTokens(tokens[:length])
		if len(errorsOnly(diagnostics)) != 0 || len(items) != 3 {
			t.Fatalf("multi-item list = %d / %v", len(items), diagnostics)
		}
		seen := map[ast.NodeID]bool{}
		for _, item := range items {
			ast.Walk(item, func(node ast.Node) bool {
				if node.ID() == 0 || seen[node.ID()] {
					t.Errorf("item-list IDs overlap: %T has %d", node, node.ID())
				}
				seen[node.ID()] = true
				return true
			})
		}
	}
	items, diagnostics := parser.ParseItemsTokens(lexicalTokens(t, "type Good int; return 1"))
	if len(errorsOnly(diagnostics)) == 0 || len(items) < 2 {
		t.Fatalf("trailing non-item syntax was accepted or erased: %d / %v", len(items), diagnostics)
	}
	if name := items[0].(*ast.TypeDecl).Name.Name; name != "Good" {
		t.Fatalf("recovery discarded the preceding item: %s", name)
	}
}

func TestProceduralMemberTokenLists(t *testing.T) {
	for _, test := range []struct {
		name, source         string
		inTrait, defaultImpl bool
		valid                bool
	}{
		{"empty", "", true, false, true},
		{"trait associated type", "type Item; func value(self Self) Item", true, false, true},
		{"impl associated binding", "type Item = int; func value(self Self) int { return 1 }", false, false, true},
		{"default method", "func default() Self { return Self{} }", false, true, true},
		{"default outside default impl", "func default() Self { return Self{} }", false, false, false},
		{"trait visibility", "pub func value(self Self)", true, false, false},
		{"impl missing binding", "type Item", false, false, false},
		{"variable member", "var value = 1", true, false, false},
		{"declaration member", "trait Nested {}", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			items, diagnostics := parser.ParseMembersTokens(lexicalTokens(t, test.source), test.inTrait, test.defaultImpl)
			if valid := len(errorsOnly(diagnostics)) == 0; valid != test.valid {
				t.Fatalf("member validity = %v, want %v: %v", valid, test.valid, diagnostics)
			}
			if test.name == "trait associated type" && (len(items) != 2 || items[0].(*ast.TypeDecl).Type != nil) {
				t.Fatal("trait associated type acquired a definition or lost a member")
			}
			if test.name == "default method" && items[0].(*ast.FuncDecl).Name.Name != "default" {
				t.Fatal("Default-specific method name was changed")
			}
		})
	}
}
