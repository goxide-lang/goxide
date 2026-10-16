package parser

import (
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (p *parser) pattern() ast.Pat {
	x := p.patternAtom()
	if p.at(token.OR) {
		o := &ast.OrPat{Alts: []ast.Pat{x}}
		for p.accept(token.OR) {
			o.Alts = append(o.Alts, p.patternAtom())
		}
		o.Base = p.base(x.Span().Start)
		return o
	}
	return x
}
func (p *parser) patternAtom() ast.Pat {
	start := p.tok().Start
	if p.context("ref") {
		p.next()
		mut := p.context("mut")
		if mut {
			p.next()
		}
		name := p.ident()
		return &ast.BindPat{Base: p.base(start), Name: name, Ref: true, Mut: mut}
	}
	switch p.tok().Kind {
	case token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING:
		v := p.operand()
		return &ast.LitPat{Base: p.base(start), Value: v}
	case token.ADD, token.SUB:
		op := p.next().Kind
		if p.at(token.INT) || p.at(token.FLOAT) || p.at(token.IMAG) {
			v := p.operand()
			u := &ast.UnaryExpr{Base: p.base(start), Op: op, X: v}
			return &ast.LitPat{Base: p.base(start), Value: u}
		}
		p.error("expected numeric literal in pattern")
		return &ast.BadPat{Base: p.base(start)}
	case token.IDENT, token.SELF:
		if p.at(token.SELF) && p.peek(1).Kind != token.DCOLON {
			p.error("Self pattern requires a variant path")
		}
		if !p.tok().Raw && p.tok().Text == "_" {
			p.next()
			return &ast.WildPat{Base: p.base(start)}
		}
		if !p.tok().Raw && (p.tok().Text == "true" || p.tok().Text == "false" || p.tok().Text == "nil") {
			v := p.ident()
			return &ast.LitPat{Base: p.base(start), Value: v}
		}
		path := p.path()
		if p.accept(token.LPAREN) {
			t := &ast.TuplePat{Path: path}
			for !p.at(token.RPAREN) && !p.at(token.EOF) {
				pos := p.pos
				t.Elts = append(t.Elts, p.pattern())
				if !p.accept(token.COMMA) || p.pos == pos {
					break
				}
			}
			p.expect(token.RPAREN)
			t.Base = p.base(start)
			return t
		}
		if p.accept(token.LBRACE) {
			s := &ast.StructPat{Path: path}
			for !p.at(token.RBRACE) && !p.at(token.EOF) {
				p.semis()
				if p.at(token.RBRACE) {
					break
				}
				if p.accept(token.PERIOD) {
					p.expect(token.PERIOD)
					s.Rest = true
					p.accept(token.COMMA)
					p.semis()
					break
				}
				before, fieldStart := p.pos, p.tok().Start
				field := &ast.PatField{Name: p.ident()}
				if p.accept(token.COLON) {
					field.Pat = p.pattern()
				} else {
					name := *field.Name
					name.Base = p.base(fieldStart)
					field.Pat = &ast.BindPat{Base: p.base(fieldStart), Name: &name}
				}
				field.Base = p.base(fieldStart)
				s.Fields = append(s.Fields, field)
				if !p.accept(token.COMMA) {
					break
				}
				p.progress(before)
			}
			p.semis()
			p.expect(token.RBRACE)
			s.Base = p.base(start)
			return s
		}
		if len(path.Segments) == 1 && path.Segments[0].Args == nil {
			return &ast.BindPat{Base: p.base(start), Name: path.Segments[0].Name}
		}
		return &ast.PathPat{Base: p.base(start), Path: path}
	default:
		t := p.tok()
		p.error("expected pattern")
		switch t.Kind {
		case token.EOF, token.RBRACE, token.RPAREN, token.COMMA, token.FATARROW, token.IF:
		default:
			p.next()
		}
		return &ast.BadPat{Base: p.base(start), Text: t.Text}
	}
}
func (p *parser) attrs() []*ast.Attr {
	var attrs []*ast.Attr
	for p.at(token.HASH) {
		start := p.next().Start
		inner := p.accept(token.NOT)
		if inner {
			p.error("file-level inner attributes are reserved")
		}
		p.expect(token.LBRACK)
		pathStart := p.tok().Start
		path := &ast.Path{}
		for {
			t := p.tok()
			var name *ast.Ident
			if t.Kind == token.IDENT {
				name = p.ident()
			} else if t.Kind == token.DEFAULT {
				p.next()
				name = &ast.Ident{Base: p.base(t.Start), Name: t.Text}
			} else {
				p.error("expected attribute name")
				if !p.at(token.EOF) && !p.at(token.RBRACK) {
					p.next()
				}
				name = &ast.Ident{Base: p.base(t.Start), Name: t.Text}
			}
			path.Segments = append(path.Segments, &ast.PathSegment{Base: p.base(t.Start), Name: name})
			if !p.accept(token.DCOLON) {
				break
			}
		}
		path.Base = p.base(pathStart)
		a := &ast.Attr{Inner: inner, Path: path}
		if p.at(token.LPAREN) || p.at(token.LBRACK) || p.at(token.LBRACE) {
			a.Args = p.tokenTree()
		}
		if !p.at(token.RBRACK) {
			// Attribute payloads can also be ungrouped, such as name = "value".
			if a.Args == nil {
				a.Args = &ast.TokenTree{Delim: ast.NoDelim}
			} else {
				a.Args = &ast.TokenTree{Delim: ast.NoDelim, Tokens: []ast.TokenOrTree{{Tree: a.Args}}}
			}
			s := p.tok().Start
			for !p.at(token.RBRACK) && !p.at(token.EOF) {
				if p.at(token.LPAREN) || p.at(token.LBRACK) || p.at(token.LBRACE) {
					a.Args.Tokens = append(a.Args.Tokens, ast.TokenOrTree{Tree: p.tokenTree()})
				} else {
					if p.at(token.RPAREN) || p.at(token.RBRACE) {
						p.error("mismatched delimiter in attribute arguments")
					}
					t := p.next()
					a.Args.Tokens = append(a.Args.Tokens, ast.TokenOrTree{Token: &t})
				}
			}
			a.Args.Base = p.base(s)
		}
		p.expect(token.RBRACK)
		a.Base = p.base(start)
		attrs = append(attrs, a)
	}
	return attrs
}
func (p *parser) tokenTree() *ast.TokenTree {
	start := p.tok().Start
	tree := &ast.TokenTree{}
	var close token.Kind
	switch p.tok().Kind {
	case token.LPAREN:
		tree.Delim = ast.Paren
		close = token.RPAREN
	case token.LBRACK:
		tree.Delim = ast.Bracket
		close = token.RBRACK
	case token.LBRACE:
		tree.Delim = ast.Brace
		close = token.RBRACE
	default:
		p.error("expected delimited token tree")
		tree.Base = p.base(start)
		return tree
	}
	p.next()
	for !p.at(close) && !p.at(token.EOF) {
		switch p.tok().Kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			tree.Tokens = append(tree.Tokens, ast.TokenOrTree{Tree: p.tokenTree()})
		case token.RPAREN, token.RBRACK, token.RBRACE:
			p.error("mismatched delimiter in token tree")
			tree.Base = p.base(start)
			return tree
		default:
			t := p.next()
			tree.Tokens = append(tree.Tokens, ast.TokenOrTree{Token: &t})
		}
	}
	p.expect(close)
	tree.Base = p.base(start)
	return tree
}
