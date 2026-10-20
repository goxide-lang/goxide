package parser

import (
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// newTokenParser accepts compiler-validated lexical tokens without comments.
// It preserves their spelling, flags and coordinates, and owns its token slice.
// A terminal EOF is optional; a missing one is placed at the last token's end.
func newTokenParser(tokens []token.Token) *parser {
	owned := make([]token.Token, len(tokens), len(tokens)+1)
	copy(owned, tokens)
	if len(owned) == 0 || owned[len(owned)-1].Kind != token.EOF {
		var end uint32
		if len(owned) != 0 {
			end = owned[len(owned)-1].End
		}
		owned = append(owned, token.Token{Kind: token.EOF, Start: end, End: end})
	}
	return &parser{tokens: owned}
}

// ParseExprTokens parses one expression from compiler-validated lexical tokens
// without comments. It preserves token coordinates, Raw and Implicit flags,
// accepts an optional terminal EOF, and does not modify the input slice.
func ParseExprTokens(tokens []token.Token) (ast.Expr, []Diagnostic) {
	return parseExpr(newTokenParser(tokens))
}

// ParseTypeTokens parses one type with the same token contract as ParseExprTokens.
func ParseTypeTokens(tokens []token.Token) (ast.Type, []Diagnostic) {
	return parseType(newTokenParser(tokens))
}

// ParseStmtTokens parses one statement with the same token contract as ParseExprTokens.
func ParseStmtTokens(tokens []token.Token) (ast.Stmt, []Diagnostic) {
	return parseStmt(newTokenParser(tokens))
}

// ParseItemTokens parses one declaration with the same token contract as ParseExprTokens.
func ParseItemTokens(tokens []token.Token) (ast.Item, []Diagnostic) {
	return parseItem(newTokenParser(tokens))
}
