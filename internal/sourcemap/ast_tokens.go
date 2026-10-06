package sourcemap

import (
	"go/ast"
	"go/scanner"
	"go/token"
	"strings"
)

// TokenSlot identifies syntax owned by a node, never a descendant's Pos or an
// inferred nearby token. Unsupported syntax has no slot and remains unmapped.
type TokenSlot string

const (
	IdentToken        TokenSlot = "identifier"
	LiteralToken      TokenSlot = "literal"
	OperatorToken     TokenSlot = "operator"
	KeywordToken      TokenSlot = "keyword"
	LeftParenToken    TokenSlot = "left parenthesis"
	RightParenToken   TokenSlot = "right parenthesis"
	LeftBraceToken    TokenSlot = "left brace"
	RightBraceToken   TokenSlot = "right brace"
	LeftBracketToken  TokenSlot = "left bracket"
	RightBracketToken TokenSlot = "right bracket"
	EllipsisToken     TokenSlot = "ellipsis"
	ColonToken        TokenSlot = "colon"
	RangeToken        TokenSlot = "range keyword"
	typeParamsLeft    TokenSlot = "type parameters left bracket"
	typeParamsRight   TokenSlot = "type parameters right bracket"
	resultsLeft       TokenSlot = "results left parenthesis"
	resultsRight      TokenSlot = "results right parenthesis"
)

type astToken struct {
	slot TokenSlot
	pos  token.Pos
	kind token.Token
	text string
}

// astTokens is deliberately an explicit syntax table. Reflecting over Pos
// fields would mistake FileEnd, BasicLit.ValueEnd and FieldList boundaries for
// owned tokens. Delimiters in FieldLists are owned by their syntactic parent.
func astTokens(node ast.Node) []astToken {
	var out []astToken
	add := func(slot TokenSlot, pos token.Pos, kind token.Token) {
		out = append(out, astToken{slot, pos, kind, kind.String()})
	}
	parens := func(left, right token.Pos) {
		add(LeftParenToken, left, token.LPAREN)
		add(RightParenToken, right, token.RPAREN)
	}
	braces := func(left, right token.Pos) {
		add(LeftBraceToken, left, token.LBRACE)
		add(RightBraceToken, right, token.RBRACE)
	}
	brackets := func(left, right token.Pos) {
		add(LeftBracketToken, left, token.LBRACK)
		add(RightBracketToken, right, token.RBRACK)
	}
	typeParams := func(list *ast.FieldList) {
		if list != nil {
			add(typeParamsLeft, list.Opening, token.LBRACK)
			add(typeParamsRight, list.Closing, token.RBRACK)
		}
	}
	switch n := node.(type) {
	case *ast.Ident:
		out = append(out, astToken{IdentToken, n.NamePos, token.IDENT, n.Name})
	case *ast.BasicLit:
		out = append(out, astToken{LiteralToken, n.ValuePos, n.Kind, n.Value})
	case *ast.Ellipsis:
		add(EllipsisToken, n.Ellipsis, token.ELLIPSIS)
	case *ast.CompositeLit:
		braces(n.Lbrace, n.Rbrace)
	case *ast.ParenExpr:
		parens(n.Lparen, n.Rparen)
	case *ast.IndexExpr:
		brackets(n.Lbrack, n.Rbrack)
	case *ast.IndexListExpr:
		brackets(n.Lbrack, n.Rbrack)
	case *ast.SliceExpr:
		brackets(n.Lbrack, n.Rbrack)
	case *ast.TypeAssertExpr:
		parens(n.Lparen, n.Rparen)
	case *ast.CallExpr:
		parens(n.Lparen, n.Rparen)
		if n.Ellipsis.IsValid() {
			add(EllipsisToken, n.Ellipsis, token.ELLIPSIS)
		}
	case *ast.StarExpr:
		add(OperatorToken, n.Star, token.MUL)
	case *ast.UnaryExpr:
		add(OperatorToken, n.OpPos, n.Op)
	case *ast.BinaryExpr:
		add(OperatorToken, n.OpPos, n.Op)
	case *ast.KeyValueExpr:
		add(ColonToken, n.Colon, token.COLON)
	case *ast.ArrayType:
		add(LeftBracketToken, n.Lbrack, token.LBRACK)
	case *ast.StructType:
		add(KeywordToken, n.Struct, token.STRUCT)
		if n.Fields != nil {
			braces(n.Fields.Opening, n.Fields.Closing)
		}
	case *ast.FuncType:
		add(KeywordToken, n.Func, token.FUNC)
		typeParams(n.TypeParams)
		if n.Params != nil {
			parens(n.Params.Opening, n.Params.Closing)
		} else {
			parens(token.NoPos, token.NoPos)
		}
		if n.Results != nil {
			add(resultsLeft, n.Results.Opening, token.LPAREN)
			add(resultsRight, n.Results.Closing, token.RPAREN)
		}
	case *ast.InterfaceType:
		add(KeywordToken, n.Interface, token.INTERFACE)
		if n.Methods != nil {
			braces(n.Methods.Opening, n.Methods.Closing)
		}
	case *ast.MapType:
		add(KeywordToken, n.Map, token.MAP)
	case *ast.ChanType:
		// Begin denotes the arrow for receive-only channels, not "chan".
		if n.Dir != ast.RECV {
			add(KeywordToken, n.Begin, token.CHAN)
		}
		if n.Dir != ast.SEND|ast.RECV {
			add(OperatorToken, n.Arrow, token.ARROW)
		}
	case *ast.LabeledStmt:
		add(ColonToken, n.Colon, token.COLON)
	case *ast.SendStmt:
		add(OperatorToken, n.Arrow, token.ARROW)
	case *ast.IncDecStmt:
		add(OperatorToken, n.TokPos, n.Tok)
	case *ast.AssignStmt:
		add(OperatorToken, n.TokPos, n.Tok)
	case *ast.GoStmt:
		add(KeywordToken, n.Go, token.GO)
	case *ast.DeferStmt:
		add(KeywordToken, n.Defer, token.DEFER)
	case *ast.ReturnStmt:
		add(KeywordToken, n.Return, token.RETURN)
	case *ast.BranchStmt:
		add(KeywordToken, n.TokPos, n.Tok)
	case *ast.BlockStmt:
		braces(n.Lbrace, n.Rbrace)
	case *ast.IfStmt:
		add(KeywordToken, n.If, token.IF)
	case *ast.CaseClause:
		kind := token.CASE
		if len(n.List) == 0 {
			kind = token.DEFAULT
		}
		add(KeywordToken, n.Case, kind)
		add(ColonToken, n.Colon, token.COLON)
	case *ast.SwitchStmt:
		add(KeywordToken, n.Switch, token.SWITCH)
	case *ast.TypeSwitchStmt:
		add(KeywordToken, n.Switch, token.SWITCH)
	case *ast.CommClause:
		kind := token.CASE
		if n.Comm == nil {
			kind = token.DEFAULT
		}
		add(KeywordToken, n.Case, kind)
		add(ColonToken, n.Colon, token.COLON)
	case *ast.SelectStmt:
		add(KeywordToken, n.Select, token.SELECT)
	case *ast.ForStmt:
		add(KeywordToken, n.For, token.FOR)
	case *ast.RangeStmt:
		add(KeywordToken, n.For, token.FOR)
		add(RangeToken, n.Range, token.RANGE)
		if n.Key != nil {
			add(OperatorToken, n.TokPos, n.Tok)
		}
	case *ast.TypeSpec:
		typeParams(n.TypeParams)
		if n.Assign.IsValid() {
			add(OperatorToken, n.Assign, token.ASSIGN)
		}
	case *ast.GenDecl:
		add(KeywordToken, n.TokPos, n.Tok)
		if n.Lparen.IsValid() || len(n.Specs) != 1 {
			parens(n.Lparen, n.Rparen)
		}
	case *ast.FuncDecl:
		if n.Recv != nil {
			parens(n.Recv.Opening, n.Recv.Closing)
		}
	case *ast.File:
		add(KeywordToken, n.Package, token.PACKAGE)
	}
	return out
}

type scannedASTToken struct {
	start, end int
	kind       token.Token
	text       string
}

func scanASTTokens(source []byte) map[int]scannedASTToken {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, source, func(token.Position, string) {}, 0)
	out := map[int]scannedASTToken{}
	for {
		pos, kind, text := scan.Scan()
		if kind == token.EOF {
			return out
		}
		if kind == token.SEMICOLON && text == "\n" {
			continue
		}
		if text == "" {
			text = kind.String()
		}
		start := f.Offset(pos)
		end := start + len(text)
		if kind == token.STRING && start < len(source) && source[start] == '`' {
			if close := strings.IndexByte(string(source[start+1:]), '`'); close >= 0 {
				end = start + close + 2
			}
		}
		if end <= len(source) {
			out[start] = scannedASTToken{start, end, kind, text}
		}
	}
}
