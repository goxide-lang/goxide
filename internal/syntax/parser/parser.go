// Package parser parses Goxide source without lowering it to Go.
package parser

import (
	"fmt"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/lexer"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type Severity uint8

const (
	Error Severity = iota
	Warning
)

type Diagnostic struct {
	Severity Severity
	Span     ast.Span
	Message  string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%d:%d: %s", d.Span.Start, d.Span.End, d.Message)
}

type parser struct {
	tokens      []token.Token
	pos         int
	end         uint32
	nextID      ast.NodeID
	diagnostics []Diagnostic
	noComposite int
	comments    []*ast.CommentGroup
	directives  []*ast.Directive
}

func newParser(src []byte) *parser {
	ts, ds := lexer.Lex(src)
	p := &parser{}
	for _, d := range ds {
		p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Error, Span: ast.Span{Start: d.Start, End: d.End}, Message: d.Message})
	}
	for _, t := range ts {
		if t.Kind == token.COMMENT {
			p.addComment(t, src)
		} else {
			p.tokens = append(p.tokens, t)
		}
	}
	return p
}

// ParseFile returns a recovered tree even when diagnostics contain errors.
func ParseFile(filename string, src []byte) (*ast.File, []Diagnostic) {
	p := newParser(src)
	f := &ast.File{LineStarts: []uint32{0}, Tokens: p.tokens}
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			f.LineStarts = append(f.LineStarts, uint32(i+1))
		}
	}
	p.expect(token.PACKAGE)
	f.Package = p.ident()
	p.endStmt()
	if p.errorCount() > 0 {
		f.Items = append(f.Items, &ast.BadItem{ItemBase: ast.ItemBase{Base: p.base(0)}, Text: "invalid package or token"})
	}
	f.Items = append(f.Items, p.items()...)
	f.Base = p.base(0)
	f.Comments = p.comments
	f.Directives = p.directives
	p.attachDocs(f, src)
	f.Items = FlattenUses(f.Items)
	renumber(f)
	return f, p.diagnostics
}

func (p *parser) items() []ast.Item {
	return p.itemList(p.item)
}

func (p *parser) itemList(parse func() ast.Item) []ast.Item {
	var items []ast.Item
	for !p.at(token.EOF) {
		p.semis()
		if p.at(token.EOF) {
			break
		}
		before := p.pos
		items = append(items, parse())
		if !p.endStmt() {
			items = append(items, &ast.BadItem{ItemBase: ast.ItemBase{Base: p.base(p.tok().Start)}, Text: "missing semicolon"})
		}
		p.progress(before)
	}
	return items
}

func ParseExpr(src []byte) (ast.Expr, []Diagnostic) {
	return parseExpr(newParser(src))
}

func parseExpr(p *parser) (ast.Expr, []Diagnostic) {
	n := p.expr()
	p.finish()
	if p.errorCount() > 0 && !hasBad(n) {
		n = &ast.BadExpr{Base: p.base(0), Text: "invalid expression"}
	}
	renumber(n)
	return n, p.diagnostics
}
func ParseType(src []byte) (ast.Type, []Diagnostic) {
	return parseType(newParser(src))
}

func parseType(p *parser) (ast.Type, []Diagnostic) {
	n := p.typ()
	p.finish()
	if p.errorCount() > 0 && !hasBad(n) {
		n = &ast.BadType{Base: p.base(0), Text: "invalid type"}
	}
	renumber(n)
	return n, p.diagnostics
}
func ParsePattern(src []byte) (ast.Pat, []Diagnostic) {
	p := newParser(src)
	n := p.pattern()
	p.finish()
	if p.errorCount() > 0 && !hasBad(n) {
		n = &ast.BadPat{Base: p.base(0), Text: "invalid pattern"}
	}
	renumber(n)
	return n, p.diagnostics
}
func ParseStmt(src []byte) (ast.Stmt, []Diagnostic) {
	return parseStmt(newParser(src))
}

func parseStmt(p *parser) (ast.Stmt, []Diagnostic) {
	n := p.stmt()
	p.finish()
	if p.errorCount() > 0 && !hasBad(n) {
		n = &ast.BadStmt{StmtBase: ast.StmtBase{Base: p.base(0)}, Text: "invalid statement"}
	}
	renumber(n)
	return n, p.diagnostics
}
func ParseItem(src []byte) (ast.Item, []Diagnostic) {
	return parseItem(newParser(src))
}

func parseItem(p *parser) (ast.Item, []Diagnostic) {
	n := p.item()
	p.finish()
	if p.errorCount() > 0 && !hasBad(n) {
		n = &ast.BadItem{ItemBase: ast.ItemBase{Base: p.base(0)}, Text: "invalid declaration"}
	}
	renumber(n)
	return n, p.diagnostics
}
func (p *parser) finish() {
	p.semis()
	if !p.at(token.EOF) {
		p.error("unexpected token after syntax fragment")
	}
}
func renumber(n ast.Node) {
	var id ast.NodeID
	ast.Walk(n, func(n ast.Node) bool { id++; n.(interface{ SetID(ast.NodeID) }).SetID(id); return true })
}
func (p *parser) tok() token.Token { return p.peek(0) }
func (p *parser) peek(n int) token.Token {
	i := p.pos + n
	if i >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[i]
}
func (p *parser) at(k token.Kind) bool { return p.tok().Kind == k }
func (p *parser) next() token.Token {
	t := p.tok()
	p.end = t.End
	if t.Kind != token.EOF {
		p.pos++
	}
	return t
}
func (p *parser) accept(k token.Kind) bool {
	if p.at(k) {
		p.next()
		return true
	}
	return false
}
func (p *parser) expect(k token.Kind) token.Token {
	if p.at(k) {
		return p.next()
	}
	p.missing("expected " + k.String() + ", found " + p.tok().Kind.String())
	t := p.tok()
	t.Kind = k
	t.Text = k.String()
	t.End = t.Start
	return t
}
func (p *parser) error(msg string) {
	t := p.tok()
	p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Error, Span: ast.Span{Start: t.Start, End: t.End}, Message: msg})
}
func (p *parser) missing(msg string) {
	start := p.tok().Start
	p.end = start
	p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Error, Span: ast.Span{Start: start, End: start}, Message: msg})
}
func (p *parser) base(start uint32) ast.Base {
	end := p.end
	if end < start {
		end = start
	}
	p.nextID++
	return ast.Base{NodeID: p.nextID, Loc: ast.Span{Start: start, End: end}}
}
func (p *parser) ident() *ast.Ident {
	t := p.tok()
	if !p.at(token.IDENT) {
		const message = "expected identifier (reserved words require r#)"
		if p.recoveryBoundary() {
			p.missing(message)
		} else {
			p.error(message)
			p.next()
		}
		base := p.base(t.Start)
		base.Loc.End = t.Start
		return &ast.Ident{Base: base, Missing: true}
	}
	p.next()
	if !t.Raw && token.IsFutureReserved(t.Text) {
		p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Warning, Span: ast.Span{Start: t.Start, End: t.End}, Message: fmt.Sprintf("%q is reserved for future use; rename it or write r#%s", t.Text, t.Text)})
	}
	return &ast.Ident{Base: p.base(t.Start), Name: t.Text, Raw: t.Raw}
}
func (p *parser) semis() {
	for p.accept(token.SEMICOLON) {
	}
}
func (p *parser) endStmt() bool {
	if p.accept(token.SEMICOLON) || p.at(token.RBRACE) || p.at(token.EOF) {
		return true
	}
	if p.declarationStart() && p.pos > 0 && p.tokens[p.pos-1].Kind == token.SEMICOLON {
		return true // A recovered block already consumed the preceding newline.
	}
	p.error("expected semicolon or newline")
	return false
}

func (p *parser) errorCount() int {
	n := 0
	for _, d := range p.diagnostics {
		if d.Severity == Error {
			n++
		}
	}
	return n
}
func hasBad(n ast.Node) bool {
	bad := false
	ast.Walk(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BadItem, *ast.BadStmt, *ast.BadExpr, *ast.BadType, *ast.BadPat:
			bad = true
		case *ast.Ident:
			bad = bad || n.Missing
		case *ast.TokenTree:
			bad = bad || n.Missing
		}
		return !bad
	})
	return bad
}
func (p *parser) recoveryBoundary() bool {
	switch p.tok().Kind {
	case token.EOF, token.SEMICOLON, token.COMMA, token.RBRACE, token.RPAREN, token.RBRACK, token.LBRACE, token.FATARROW:
		return true
	}
	return p.declarationStart()
}

// Named functions and package-only items cannot begin a block statement.
func (p *parser) declarationStart() bool {
	switch p.tok().Kind {
	case token.FUNC:
		return p.peek(1).Kind == token.IDENT
	case token.PACKAGE, token.PUB, token.USE, token.MOD, token.ENUM, token.TRAIT, token.IMPL, token.DECLARE:
		return true
	}
	return p.context("macro_rules") && p.peek(1).Kind == token.NOT && p.peek(2).Kind == token.IDENT
}
func (p *parser) progress(before int) {
	if p.pos == before && !p.at(token.EOF) {
		p.error("unexpected token")
		p.next()
	}
}
func (p *parser) sync() {
	depth := 0
	for !p.at(token.EOF) {
		if depth == 0 && (p.at(token.SEMICOLON) || p.at(token.RBRACE)) {
			return
		}
		switch p.next().Kind {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			if depth > 0 {
				depth--
			}
		}
	}
}

func adjacent(src []byte, a, b uint32) bool {
	return a <= b && int(b) <= len(src) && strings.Count(string(src[a:b]), "\n") <= 1
}
