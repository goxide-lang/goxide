package parser

import (
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func typeStart(k token.Kind) bool {
	switch k {
	case token.IDENT, token.SELF, token.MUL, token.AND, token.LBRACK, token.MAP, token.CHAN, token.ARROW, token.FUNC, token.STRUCT, token.INTERFACE, token.LPAREN, token.TILDE:
		return true
	}
	return false
}
func (p *parser) typ() ast.Type {
	x := p.typeAtom()
	if p.at(token.OR) {
		u := &ast.UnionType{Terms: []ast.Type{x}}
		for p.accept(token.OR) {
			u.Terms = append(u.Terms, p.typeAtom())
		}
		u.Base = p.base(x.Span().Start)
		return u
	}
	return x
}
func (p *parser) typeAtom() ast.Type {
	start := p.tok().Start
	var t ast.Type
	switch p.tok().Kind {
	case token.IDENT:
		path := p.path()
		if p.macroAdjacent() {
			p.next()
			args := p.tokenTree()
			return &ast.MacroType{Base: p.base(start), Path: path, Args: args}
		}
		t = &ast.PathType{Base: p.base(start), Path: path}
	case token.SELF:
		p.next()
		t = &ast.SelfType{Base: p.base(start)}
	case token.MUL:
		p.next()
		e := p.typeAtom()
		return &ast.PointerType{Base: p.base(start), Elem: e}
	case token.AND:
		p.next()
		mut := p.context("mut") && typeStart(p.peek(1).Kind)
		if mut {
			p.next()
		}
		e := p.typeAtom()
		return &ast.RefType{Base: p.base(start), Elem: e, Mut: mut}
	case token.TILDE:
		p.next()
		e := p.typeAtom()
		return &ast.ApproxType{Base: p.base(start), Type: e}
	case token.LBRACK:
		p.next()
		if p.accept(token.RBRACK) {
			e := p.typeAtom()
			return &ast.SliceType{Base: p.base(start), Elem: e}
		}
		ellipsis := p.accept(token.ELLIPSIS)
		var n ast.Expr
		if !ellipsis {
			old := p.noComposite
			p.noComposite = 0
			n = p.expr()
			p.noComposite = old
		}
		p.expect(token.RBRACK)
		e := p.typeAtom()
		return &ast.ArrayType{Base: p.base(start), Len: n, Elem: e, Ellipsis: ellipsis}
	case token.MAP:
		p.next()
		p.expect(token.LBRACK)
		k := p.typ()
		p.expect(token.RBRACK)
		v := p.typeAtom()
		return &ast.MapType{Base: p.base(start), Key: k, Value: v}
	case token.CHAN, token.ARROW:
		dir := ast.SendRecv
		if p.accept(token.ARROW) {
			dir = ast.RecvOnly
			p.expect(token.CHAN)
		} else {
			p.next()
			if p.accept(token.ARROW) {
				dir = ast.SendOnly
			}
		}
		e := p.typeAtom()
		return &ast.ChanType{Base: p.base(start), Dir: dir, Elem: e}
	case token.FUNC:
		p.next()
		params, results, paren := p.signature(false, false)
		return &ast.FuncType{Base: p.base(start), Params: params, Results: results, ResultParen: paren}
	case token.STRUCT:
		return p.structType()
	case token.INTERFACE:
		return p.interfaceType()
	case token.LPAREN:
		p.next()
		e := p.typ()
		p.expect(token.RPAREN)
		return &ast.ParenType{Base: p.base(start), Type: e}
	default:
		tok := p.tok()
		p.error("expected type")
		switch tok.Kind {
		case token.EOF, token.SEMICOLON, token.RBRACE, token.RPAREN, token.RBRACK, token.COMMA, token.LBRACE, token.GTR:
		default:
			p.next()
		}
		return &ast.BadType{Base: p.base(start), Text: tok.Text}
	}
	for p.at(token.PERIOD) || p.at(token.DCOLON) {
		dot := p.next().Kind == token.PERIOD
		if _, self := t.(*ast.SelfType); dot && self {
			p.error("associated types use :: instead of .")
		}
		item := p.ident()
		var args *ast.GenericArgs
		if p.at(token.LBRACK) {
			args = p.genericArgs()
		}
		t = &ast.ProjectionType{Base: p.base(start), X: t, Item: item, Dot: dot, Args: args}
	}
	return t
}
func (p *parser) path() *ast.Path {
	start := p.tok().Start
	path := &ast.Path{}
	for {
		var name *ast.Ident
		if len(path.Segments) == 0 && p.at(token.SELF) {
			t := p.next()
			name = &ast.Ident{Base: p.base(t.Start), Name: t.Text}
		} else {
			name = p.ident()
		}
		if len(path.Segments) == 0 && !name.Raw && name.Name == "super" && p.at(token.DCOLON) {
			p.error("super:: paths are not supported yet")
		}
		seg := &ast.PathSegment{Name: name}
		if p.at(token.LBRACK) {
			seg.Args = p.genericArgs()
		}
		seg.Base = p.base(name.Span().Start)
		path.Segments = append(path.Segments, seg)
		if !p.accept(token.DCOLON) {
			break
		}
	}
	path.Base = p.base(start)
	return path
}
func (p *parser) genericArgs() *ast.GenericArgs {
	start := p.expect(token.LBRACK).Start
	args := &ast.GenericArgs{}
	for !p.at(token.RBRACK) && !p.at(token.EOF) {
		pos := p.pos
		s := p.tok().Start
		arg := &ast.GenericArg{}
		if p.at(token.IDENT) && p.peek(1).Kind == token.ASSIGN {
			arg.Name = p.ident()
			p.next()
			arg.Type = p.typ()
		} else {
			// Preserve type-only syntax while allowing expressions in const slots.
			if typeStart(p.tok().Kind) {
				q := *p
				q.diagnostics = append([]Diagnostic(nil), p.diagnostics...)
				typ := q.typ()
				if !hasNewErrors(q.diagnostics, len(p.diagnostics)) && (q.at(token.COMMA) || q.at(token.RBRACK)) {
					*p = q
					arg.Type = typ
				}
			}
			if arg.Type == nil {
				arg = p.genericArgExpr(p.expr())
			}
		}
		arg.Base = p.base(s)
		args.Args = append(args.Args, arg)
		if !p.accept(token.COMMA) || p.pos == pos {
			break
		}
	}
	p.expect(token.RBRACK)
	args.Base = p.base(start)
	return args
}
func (p *parser) signature(allowWhere, receiver bool) (params, results []*ast.Param, resultParen bool) {
	params = p.params(receiver)
	if p.at(token.LPAREN) {
		resultParen = true
		results = p.params(false)
	} else if typeStart(p.tok().Kind) && !(allowWhere && p.context("where")) {
		start := p.tok().Start
		t := p.typ()
		results = []*ast.Param{{Base: p.base(start), Type: t}}
	}
	return
}
func (p *parser) params(receiver bool) []*ast.Param {
	p.expect(token.LPAREN)
	var params []*ast.Param
	var pending []*ast.Param
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		pos := p.pos
		start := p.tok().Start
		param := &ast.Param{Attrs: p.attrs()}
		if receiver && len(params) == 0 && p.selfParam(param) {
			// Shorthand receivers share the explicit self parameter representation.
		} else if p.accept(token.ELLIPSIS) {
			param.Variadic = true
			param.Type = p.typ()
		} else if p.namedArrayField() {
			param.Names = []*ast.Ident{p.ident()}
			param.Type = p.typ()
			if len(pending) > 0 {
				names := make([]*ast.Ident, 0, len(pending)+1)
				for _, prev := range pending {
					names = append(names, bareTypeName(prev.Type))
				}
				param.Names = append(names, param.Names...)
				params = params[:len(params)-len(pending)]
				start = pending[0].Span().Start
				pending = nil
			}
		} else {
			first := p.typ()
			if id := bareTypeName(first); id != nil && (typeStart(p.tok().Kind) || p.at(token.ELLIPSIS)) {
				param.Names = []*ast.Ident{id}
				if p.accept(token.ELLIPSIS) {
					param.Variadic = true
				}
				param.Type = p.typ()
				names := make([]*ast.Ident, 0, len(pending)+1)
				for _, prev := range pending {
					names = append(names, bareTypeName(prev.Type))
				}
				param.Names = append(names, param.Names...)
				if len(pending) > 0 {
					params = params[:len(params)-len(pending)]
					start = pending[0].Span().Start
				}
				pending = nil
			} else {
				param.Type = first
			}
		}
		param.Base = p.base(start)
		params = append(params, param)
		if len(param.Names) == 0 && bareTypeName(param.Type) != nil {
			pending = append(pending, param)
		} else {
			pending = nil
		}
		if !p.accept(token.COMMA) || p.pos == pos {
			break
		}
	}
	p.expect(token.RPAREN)
	return params
}

func (p *parser) selfParam(param *ast.Param) bool {
	start, offset := p.tok().Start, 0
	borrowed := p.at(token.AND)
	if borrowed {
		offset++
	}
	mutable := borrowed && p.peek(offset).Kind == token.IDENT && !p.peek(offset).Raw && p.peek(offset).Text == "mut"
	if mutable {
		offset++
	}
	name, next := p.peek(offset), p.peek(offset+1)
	if name.Kind != token.IDENT || name.Raw || name.Text != "self" || (next.Kind != token.COMMA && next.Kind != token.RPAREN) {
		return false
	}
	for range offset {
		p.next()
	}
	ident := p.ident()
	param.Names = []*ast.Ident{ident}
	base := p.base(ident.Span().Start)
	// The implicit type must not claim a macro's captured self identifier.
	base.Loc.End = base.Loc.Start
	param.Type = &ast.SelfType{Base: base}
	if borrowed {
		param.Type = &ast.RefType{Base: p.base(start), Elem: param.Type, Mut: mutable}
	}
	return true
}

func bareTypeName(t ast.Type) *ast.Ident {
	n, ok := t.(*ast.PathType)
	if ok && len(n.Path.Segments) == 1 && n.Path.Segments[0].Args == nil {
		return n.Path.Segments[0].Name
	}
	return nil
}
func (p *parser) genericParams() *ast.GenericParams {
	if !p.at(token.LBRACK) {
		return nil
	}
	start := p.next().Start
	g := &ast.GenericParams{}
	var pending []*ast.Ident
	var pendingStart uint32
	pendingConst := false
	flushPending := func() {
		for i, name := range pending {
			start := name.Span().Start
			if i == 0 && pendingConst {
				start = pendingStart
			}
			g.Params = append(g.Params, &ast.GenericParam{Base: p.base(start), Names: []*ast.Ident{name}, Const: pendingConst})
		}
		pending, pendingConst = nil, false
	}
	for !p.at(token.RBRACK) && !p.at(token.EOF) {
		pos := p.pos
		s := p.tok().Start
		if p.accept(token.CONST) {
			flushPending()
			pendingStart, pendingConst = s, true
		} else if len(pending) == 0 {
			pendingStart = s
		}
		name := p.ident()
		pending = append(pending, name)
		if typeStart(p.tok().Kind) {
			c := p.typ()
			g.Params = append(g.Params, &ast.GenericParam{Base: p.base(pendingStart), Names: pending, Constraint: c, Const: pendingConst})
			pending, pendingConst = nil, false
		}
		if !p.accept(token.COMMA) || p.pos == pos {
			break
		}
	}
	flushPending()
	p.expect(token.RBRACK)
	g.Base = p.base(start)
	return g
}
func (p *parser) whereClause() *ast.WhereClause {
	if !p.context("where") {
		return nil
	}
	start := p.next().Start
	w := &ast.WhereClause{}
	for !p.at(token.LBRACE) && !p.at(token.SEMICOLON) && !p.at(token.EOF) {
		pos := p.pos
		s := p.tok().Start
		pred := &ast.WherePredicate{Type: p.typeAtom()}
		p.expect(token.COLON)
		pred.Bounds = append(pred.Bounds, p.typeAtom())
		for p.accept(token.ADD) {
			pred.Bounds = append(pred.Bounds, p.typeAtom())
		}
		pred.Base = p.base(s)
		w.Predicates = append(w.Predicates, pred)
		if !p.accept(token.COMMA) || p.pos == pos {
			break
		}
	}
	if len(w.Predicates) == 0 {
		p.error("expected predicate after where")
	}
	w.Base = p.base(start)
	return w
}
func (p *parser) structType() *ast.StructType {
	start := p.tok().Start
	p.accept(token.STRUCT)
	p.expect(token.LBRACE)
	s := &ast.StructType{}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		pos := p.pos
		st := p.tok().Start
		f := &ast.Field{Attrs: p.attrs()}
		if p.at(token.PUB) {
			v := p.next()
			f.Vis = ast.Visibility{Kind: ast.VisPub, Span: ast.Span{Start: v.Start, End: v.End}}
		}
		var first ast.Type
		if p.namedArrayField() {
			f.Names = []*ast.Ident{p.ident()}
			f.Type = p.typ()
		} else {
			first = p.typ()
			if id := bareTypeName(first); id != nil && (p.at(token.COMMA) || typeStart(p.tok().Kind)) {
				f.Names = []*ast.Ident{id}
				for p.accept(token.COMMA) {
					f.Names = append(f.Names, p.ident())
				}
				f.Type = p.typ()
			} else {
				f.Type = first
			}
		}
		if p.at(token.STRING) {
			t := p.next()
			f.Tag = &ast.BasicLit{Base: p.base(t.Start), Kind: t.Kind, Value: t.Text}
		}
		f.Base = p.base(st)
		s.Fields = append(s.Fields, f)
		if p.pos == pos {
			p.next()
		}
		if !p.at(token.SEMICOLON) && !p.at(token.RBRACE) {
			p.error("expected semicolon after struct field")
			for !p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.EOF) {
				p.next()
			}
		}
		p.semis()
	}
	p.expect(token.RBRACE)
	s.Base = p.base(start)
	return s
}
func (p *parser) interfaceType() *ast.InterfaceType {
	start := p.expect(token.INTERFACE).Start
	p.expect(token.LBRACE)
	it := &ast.InterfaceType{}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		pos := p.pos
		s := p.tok().Start
		m := &ast.InterfaceMethod{Attrs: p.attrs()}
		if p.at(token.IDENT) && p.peek(1).Kind == token.LPAREN {
			m.Name = p.ident()
			m.Params, m.Results, m.ResultParen = p.signature(false, false)
		} else {
			m.Type = p.typ()
		}
		m.Base = p.base(s)
		it.Methods = append(it.Methods, m)
		if p.pos == pos {
			p.next()
		}
		if !p.at(token.SEMICOLON) && !p.at(token.RBRACE) {
			p.error("expected semicolon after interface member")
			for !p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.EOF) {
				p.next()
			}
		}
		p.semis()
	}
	p.expect(token.RBRACE)
	it.Base = p.base(start)
	return it
}

func (p *parser) namedArrayField() bool {
	if !p.at(token.IDENT) || p.peek(1).Kind != token.LBRACK {
		return false
	}
	depth := 0
	for n := 1; ; n++ {
		switch p.peek(n).Kind {
		case token.EOF:
			return false
		case token.LBRACK:
			depth++
		case token.RBRACK:
			depth--
			if depth == 0 {
				return n == 2 || typeStart(p.peek(n+1).Kind)
			}
		}
	}
}
