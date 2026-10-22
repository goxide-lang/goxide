package parser

import (
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (p *parser) context(s string) bool {
	return p.at(token.IDENT) && !p.tok().Raw && p.tok().Text == s
}
func (p *parser) exprHead() ast.Expr {
	old := p.noComposite
	p.noComposite++
	x := p.expr()
	p.noComposite = old
	return x
}
func (p *parser) expr() ast.Expr { return p.binary(1) }
func precedence(k token.Kind) int {
	switch k {
	case token.LOR:
		return 1
	case token.LAND:
		return 2
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return 3
	case token.ADD, token.SUB, token.OR, token.XOR:
		return 4
	case token.MUL, token.QUO, token.REM, token.SHL, token.SHR, token.AND, token.AND_NOT:
		return 5
	}
	return 0
}
func (p *parser) binary(min int) ast.Expr {
	x := p.unary()
	for prec := precedence(p.tok().Kind); prec >= min; prec = precedence(p.tok().Kind) {
		op := p.next().Kind
		y := p.binary(prec + 1)
		x = &ast.BinaryExpr{Base: p.base(x.Span().Start), X: x, Op: op, Y: y}
	}
	return x
}
func operandStart(k token.Kind) bool {
	switch k {
	case token.IDENT, token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING, token.LPAREN, token.LBRACK, token.FUNC, token.STRUCT, token.MAP, token.CHAN, token.INTERFACE, token.SELF, token.MATCH, token.UNSAFE, token.ADD, token.SUB, token.NOT, token.XOR, token.MUL, token.AND, token.ARROW, token.OR, token.LOR:
		return true
	}
	return false
}
func (p *parser) unary() ast.Expr {
	start := p.tok().Start
	switch p.tok().Kind {
	case token.ARROW:
		if p.peek(1).Kind == token.CHAN {
			t := p.typ()
			return p.postfix(&ast.TypeExpr{Base: p.base(start), Type: t})
		}
		op := p.next().Kind
		x := p.unary()
		return &ast.UnaryExpr{Base: p.base(start), Op: op, X: x}
	case token.ADD, token.SUB, token.NOT, token.XOR:
		op := p.next().Kind
		x := p.unary()
		return &ast.UnaryExpr{Base: p.base(start), Op: op, X: x}
	case token.MUL:
		p.next()
		x := p.unary()
		return &ast.StarExpr{Base: p.base(start), X: x}
	case token.AND:
		p.next()
		mut := p.context("mut") && operandStart(p.peek(1).Kind)
		if mut {
			p.next()
		}
		x := p.unary()
		return &ast.RefExpr{Base: p.base(start), X: x, Mut: mut}
	case token.UNSAFE:
		p.next()
		x := p.unary()
		return &ast.UnsafeExpr{Base: p.base(start), X: x}
	case token.OR, token.LOR:
		return p.postfix(p.barFunc())
	}
	return p.postfix(p.operand())
}
func (p *parser) operand() ast.Expr {
	start := p.tok().Start
	switch p.tok().Kind {
	case token.IDENT:
		return p.ident()
	case token.INT, token.FLOAT, token.IMAG, token.CHAR, token.STRING:
		t := p.next()
		return &ast.BasicLit{Base: p.base(start), Kind: t.Kind, Value: t.Text}
	case token.LPAREN:
		p.next()
		old := p.noComposite
		p.noComposite = 0
		x := p.expr()
		p.expect(token.RPAREN)
		p.noComposite = old
		return &ast.ParenExpr{Base: p.base(start), X: x}
	case token.FUNC:
		p.next()
		params, results, paren := p.signature()
		if p.at(token.LBRACE) {
			body := p.block()
			return &ast.FuncLit{Base: p.base(start), Form: ast.GoFunc, Params: params, Results: results, ResultParen: paren, Body: body}
		}
		t := &ast.FuncType{Base: p.base(start), Params: params, Results: results, ResultParen: paren}
		return &ast.TypeExpr{Base: p.base(start), Type: t}
	case token.MATCH:
		return p.matchExpr()
	case token.SELF:
		p.next()
		t := &ast.SelfType{Base: p.base(start)}
		return &ast.TypeExpr{Base: p.base(start), Type: t}
	case token.LBRACK, token.MAP, token.STRUCT, token.INTERFACE, token.CHAN:
		t := p.typ()
		return &ast.TypeExpr{Base: p.base(start), Type: t}
	default:
		p.error("expected expression")
		t := p.tok()
		if !p.at(token.EOF) && !p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.RPAREN) && !p.at(token.RBRACK) && !p.at(token.COMMA) && !p.at(token.FATARROW) {
			p.next()
		}
		return &ast.BadExpr{Base: p.base(start), Text: t.Text}
	}
}
func (p *parser) postfix(x ast.Expr) ast.Expr {
	for {
		start := x.Span().Start
		switch p.tok().Kind {
		case token.LPAREN:
			p.next()
			old := p.noComposite
			p.noComposite = 0
			c := &ast.CallExpr{Fun: x}
			for !p.at(token.RPAREN) && !p.at(token.EOF) {
				pos := p.pos
				c.Args = append(c.Args, p.expr())
				if p.accept(token.ELLIPSIS) {
					c.Ellipsis = true
					p.accept(token.COMMA)
					break
				}
				if !p.accept(token.COMMA) || p.pos == pos {
					break
				}
			}
			p.expect(token.RPAREN)
			p.noComposite = old
			c.Base = p.base(start)
			x = c
		case token.LBRACK:
			if p.bindingTypeArgs() {
				path := p.exprPath(x)
				if path == nil {
					p.error("associated type arguments require a path")
					return x
				}
				path.Segments[len(path.Segments)-1].Args = p.genericArgs()
				path.Base = p.base(start)
				x = &ast.PathExpr{Base: p.base(start), Path: path}
				continue
			}
			p.next()
			old := p.noComposite
			p.noComposite = 0
			var low ast.Expr
			if !p.at(token.COLON) && !p.at(token.RBRACK) {
				low = p.expr()
			}
			if p.accept(token.COLON) {
				s := &ast.SliceExpr{X: x, Low: low}
				if !p.at(token.COLON) && !p.at(token.RBRACK) {
					s.High = p.expr()
				}
				if p.accept(token.COLON) {
					s.Slice3 = true
					if !p.at(token.RBRACK) {
						s.Max = p.expr()
					}
				}
				p.expect(token.RBRACK)
				s.Base = p.base(start)
				x = s
			} else {
				idx := &ast.IndexExpr{X: x}
				if low != nil {
					idx.Indices = append(idx.Indices, low)
				} else {
					p.error("expected index or type argument")
				}
				for p.accept(token.COMMA) && !p.at(token.RBRACK) && !p.at(token.EOF) {
					idx.Indices = append(idx.Indices, p.expr())
				}
				p.expect(token.RBRACK)
				idx.Base = p.base(start)
				x = idx
			}
			p.noComposite = old
		case token.PERIOD:
			p.next()
			if p.accept(token.LPAREN) {
				var t ast.Type
				if !p.accept(token.TYPE) {
					t = p.typ()
				}
				p.expect(token.RPAREN)
				x = &ast.TypeAssertExpr{Base: p.base(start), X: x, Type: t}
			} else {
				sel := p.ident()
				x = &ast.SelectorExpr{Base: p.base(start), X: x, Sel: sel}
			}
		case token.DCOLON:
			if p.peek(1).Kind == token.LSS {
				var receiver ast.Expr
				var member *ast.Ident
				static := false
				switch selected := x.(type) {
				case *ast.SelectorExpr:
					receiver, member, static = selected.X, selected.Sel, selected.Static
				case *ast.PathExpr:
					segments := selected.Path.Segments
					if len(segments) > 1 && segments[len(segments)-1].Args == nil {
						prefix := *selected.Path
						prefix.Segments = append([]*ast.PathSegment(nil), segments[:len(segments)-1]...)
						prefix.Base = ast.Base{Loc: ast.Span{Start: selected.Span().Start, End: segments[len(segments)-2].Span().End}}
						receiver = &ast.TypeExpr{Base: prefix.Base, Type: &ast.PathType{Base: prefix.Base, Path: &prefix}}
						member, static = segments[len(segments)-1].Name, true
					}
				}
				if member == nil {
					p.error("::<Trait> requires a member selector")
					return x
				}
				p.next()
				p.next()
				trait := p.typ()
				p.expect(token.GTR)
				switch trait.(type) {
				case *ast.PathType, *ast.ProjectionType:
				default:
					p.error("qualified selector requires one trait path")
				}
				x = &ast.QualifiedSelectorExpr{Base: p.base(start), X: receiver, Sel: member, Trait: trait, Static: static}
			} else {
				path := p.exprPath(x)
				if path == nil {
					if typ := p.exprType(x); typ != nil {
						p.next()
						name := p.ident()
						x = &ast.SelectorExpr{Base: p.base(start), X: &ast.TypeExpr{Base: ast.Base{Loc: x.Span()}, Type: typ}, Sel: name, Static: true}
						continue
					}
					p.error("static path requires a name or generic path")
					return x
				}
				first := path.Segments[0].Name
				if !first.Raw && first.Name == "super" {
					p.error("super:: paths are not supported yet")
				}
				p.next()
				name := p.ident()
				seg := &ast.PathSegment{Base: p.base(name.Span().Start), Name: name}
				path.Segments = append(path.Segments, seg)
				path.Base = p.base(start)
				x = &ast.PathExpr{Base: p.base(start), Path: path}
			}
		case token.NOT:
			if !p.macroAdjacent() {
				return x
			}
			path := p.exprPath(x)
			if path == nil {
				return x
			}
			p.next()
			args := p.tokenTree()
			x = &ast.MacroExpr{Base: p.base(start), Path: path, Args: args}
		case token.QUESTION:
			p.next()
			x = &ast.TryExpr{Base: p.base(start), X: x}
		case token.LBRACE:
			if p.noComposite > 0 && !anonymousCompositeType(x) {
				return x
			}
			t := p.exprType(x)
			if t == nil {
				return x
			}
			x = p.composite(t, start)
		default:
			return x
		}
	}
}
func (p *parser) macroAdjacent() bool {
	if !p.at(token.NOT) || p.pos == 0 {
		return false
	}
	next := p.peek(1)
	return p.tokens[p.pos-1].End == p.tok().Start && p.tok().End == next.Start && (next.Kind == token.LPAREN || next.Kind == token.LBRACK || next.Kind == token.LBRACE)
}
func (p *parser) composite(t ast.Type, start uint32) ast.Expr {
	p.expect(token.LBRACE)
	old := p.noComposite
	p.noComposite = 0
	c := &ast.CompositeLit{Type: t}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		pos := p.pos
		x := p.compositeElement()
		if p.accept(token.COLON) {
			y := p.compositeElement()
			x = &ast.KeyValue{Base: p.base(x.Span().Start), Key: x, Value: y}
		}
		c.Elts = append(c.Elts, x)
		if !p.accept(token.COMMA) {
			break
		}
		if p.pos == pos {
			break
		}
	}
	p.expect(token.RBRACE)
	p.noComposite = old
	c.Base = p.base(start)
	return c
}
func (p *parser) compositeElement() ast.Expr {
	if p.at(token.LBRACE) {
		return p.composite(nil, p.tok().Start)
	}
	return p.expr()
}
func (p *parser) exprPath(x ast.Expr) *ast.Path {
	switch n := x.(type) {
	case *ast.Ident:

		return &ast.Path{Base: ast.Base{Loc: n.Span()}, Segments: []*ast.PathSegment{{Base: ast.Base{Loc: n.Span()}, Name: n}}}
	case *ast.TypeExpr:
		if _, ok := n.Type.(*ast.SelfType); ok {
			id := &ast.Ident{Base: ast.Base{Loc: n.Span()}, Name: "Self"}
			return &ast.Path{Base: ast.Base{Loc: n.Span()}, Segments: []*ast.PathSegment{{Base: ast.Base{Loc: n.Span()}, Name: id}}}
		}
	case *ast.PathExpr:
		return n.Path
	case *ast.IndexExpr:
		path := p.exprPath(n.X)
		if path == nil {
			return nil
		}
		args := &ast.GenericArgs{Base: ast.Base{Loc: n.Span()}}
		for _, x := range n.Indices {
			args.Args = append(args.Args, p.genericArgExpr(x))
		}
		path.Segments[len(path.Segments)-1].Args = args
		path.Base = ast.Base{Loc: n.Span()}
		return path
	}
	return nil
}
func (p *parser) genericArgExpr(x ast.Expr) *ast.GenericArg {
	arg := &ast.GenericArg{Base: ast.Base{Loc: x.Span()}, Type: p.exprType(x)}
	if arg.Type == nil {
		arg.Value = x
	}
	return arg
}
func (p *parser) exprType(x ast.Expr) ast.Type {
	switch n := x.(type) {
	case *ast.IndexExpr:
		if projection, ok := p.exprType(n.X).(*ast.ProjectionType); ok {
			projection.Args = &ast.GenericArgs{Base: ast.Base{Loc: n.Span()}}
			for _, x := range n.Indices {
				projection.Args.Args = append(projection.Args.Args, p.genericArgExpr(x))
			}
			projection.Base = ast.Base{Loc: n.Span()}
			return projection
		}
		path := p.exprPath(x)
		if path == nil {
			return nil
		}
		return &ast.PathType{Base: ast.Base{Loc: x.Span()}, Path: path}
	case *ast.Ident, *ast.PathExpr:
		path := p.exprPath(x)
		if path == nil {
			return nil
		}
		return &ast.PathType{Base: ast.Base{Loc: x.Span()}, Path: path}
	case *ast.TypeExpr:
		return n.Type
	case *ast.StarExpr:
		t := p.exprType(n.X)
		if t != nil {
			return &ast.PointerType{Base: ast.Base{Loc: x.Span()}, Elem: t}
		}
	case *ast.RefExpr:
		t := p.exprType(n.X)
		if t != nil {
			return &ast.RefType{Base: ast.Base{Loc: x.Span()}, Elem: t, Mut: n.Mut}
		}
	case *ast.ParenExpr:
		t := p.exprType(n.X)
		if t != nil {
			return &ast.ParenType{Base: ast.Base{Loc: x.Span()}, Type: t}
		}
	case *ast.SelectorExpr:
		t := p.exprType(n.X)
		if t != nil {
			return &ast.ProjectionType{Base: ast.Base{Loc: x.Span()}, X: t, Item: n.Sel, Dot: true}
		}
	case *ast.MacroExpr:
		return &ast.MacroType{Base: ast.Base{Loc: x.Span()}, Path: n.Path, Args: n.Args}
	}
	return nil
}
func (p *parser) barFunc() ast.Expr {
	start := p.tok().Start
	empty := p.accept(token.LOR)
	f := &ast.FuncLit{Form: ast.Bar}
	if !empty {
		p.expect(token.OR)
		for !p.at(token.OR) && !p.at(token.EOF) {
			s := p.tok().Start
			attrs := p.attrs()
			name := p.ident()
			param := &ast.Param{Attrs: attrs, Names: []*ast.Ident{name}}
			if typeStart(p.tok().Kind) {
				param.Type = p.typeAtom()
			}
			param.Base = p.base(s)
			f.Params = append(f.Params, param)
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.OR)
	}
	if p.at(token.LBRACE) {
		f.Body = p.block()
	} else {
		// Prefer a valid expression body, keeping T{} a composite literal.
		before := len(p.diagnostics)
		q := *p
		q.diagnostics = append([]Diagnostic(nil), p.diagnostics...)
		x := q.expr()
		if hasNewErrors(q.diagnostics, before) && typeStart(p.tok().Kind) {
			r := *p
			r.diagnostics = append([]Diagnostic(nil), p.diagnostics...)
			result := r.typ()
			if r.at(token.LBRACE) {
				body := r.block()
				if !hasNewErrors(r.diagnostics, before) {
					*p = r
					f.Result = result
					f.Body = body
				} else {
					*p = q
					f.Expr = x
				}
			} else {
				*p = q
				f.Expr = x
			}
		} else {
			*p = q
			f.Expr = x
		}
	}
	f.Base = p.base(start)
	return f
}
func (p *parser) matchExpr() ast.Expr {
	start := p.next().Start
	x := p.exprHead()
	p.expect(token.LBRACE)
	m := &ast.MatchExpr{X: x}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		pos := p.pos
		s := p.tok().Start
		a := &ast.MatchArm{Attrs: p.attrs()}
		a.Pat = p.pattern()
		if p.accept(token.IF) {
			a.Guard = p.exprHead()
		}
		p.expect(token.FATARROW)
		if p.at(token.LBRACE) {
			a.Block = p.block()
		} else {
			a.Body = p.expr()
		}
		a.Base = p.base(s)
		m.Arms = append(m.Arms, a)
		if p.accept(token.COMMA) {
			continue
		}
		if p.at(token.SEMICOLON) && p.tok().Implicit && p.peek(1).Kind == token.RBRACE {
			p.next()
		}
		if p.at(token.RBRACE) {
			break
		}
		p.error("expected comma between match arms")
		for !p.at(token.COMMA) && !p.at(token.RBRACE) && !p.at(token.EOF) {
			p.next()
		}
		p.accept(token.COMMA)
		if p.pos == pos {
			p.next()
		}
	}
	p.expect(token.RBRACE)
	m.Base = p.base(start)
	return m
}

func anonymousCompositeType(x ast.Expr) bool {
	n, ok := x.(*ast.TypeExpr)
	if !ok {
		return false
	}
	switch n.Type.(type) {
	case *ast.StructType, *ast.ArrayType, *ast.SliceType, *ast.MapType:
		return true
	}
	return false
}

func hasNewErrors(ds []Diagnostic, start int) bool {
	for _, d := range ds[start:] {
		if d.Severity == Error {
			return true
		}
	}
	return false
}
func (p *parser) bindingTypeArgs() bool {
	depth := 0
	for n := 0; ; n++ {
		switch p.peek(n).Kind {
		case token.EOF:
			return false
		case token.LBRACK, token.LPAREN, token.LBRACE:
			depth++
		case token.RBRACK, token.RPAREN, token.RBRACE:
			depth--
			if depth <= 0 {
				return false
			}
		case token.ASSIGN:
			if depth == 1 {
				return true
			}
		}
	}
}
