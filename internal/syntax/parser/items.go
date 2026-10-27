package parser

import (
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
	"strings"
)

func (p *parser) visibility() ast.Visibility {
	if p.at(token.PUB) {
		t := p.next()
		return ast.Visibility{Kind: ast.VisPub, Span: ast.Span{Start: t.Start, End: t.End}}
	}
	return ast.Visibility{}
}
func (p *parser) item() ast.Item { return p.parseItem(false, false) }
func (p *parser) parseItem(member, declare bool) ast.Item {
	errors := p.errorCount()
	start := p.tok().Start
	ib := ast.ItemBase{Attrs: p.attrs()}
	ib.Vis = p.visibility()
	mods := ast.FuncMods{Unsafe: p.accept(token.UNSAFE)}
	if declare && !p.at(token.FUNC) && !p.at(token.TYPE) && !p.at(token.STRUCT) {
		p.error("declare requires func or type")
		p.sync()
		ib.Base = p.base(start)
		return &ast.BadItem{ItemBase: ib, Text: "invalid declaration"}
	}
	var n ast.Item
	switch p.tok().Kind {
	case token.FUNC:
		n = p.function(ib, mods, declare, member)
	case token.TYPE, token.STRUCT:
		n = p.typeDecl(ib, member || declare)
	case token.CONST, token.VAR:
		n = p.values(ib)
	case token.USE:
		p.next()
		tree := p.useTree()
		ast.UseLeaves(tree, func(path []*ast.PathSegment, leaf *ast.UseTree) {
			if len(path) == 0 && leaf.Glob {
				p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Error, Span: leaf.Span(), Message: "glob use requires a path"})
			}
		})
		n = &ast.UseDecl{ItemBase: ib, Tree: tree}
	case token.MOD:
		p.next()
		d := &ast.ModDecl{ItemBase: ib, Name: p.ident()}
		if p.at(token.LBRACE) {
			p.error("inline modules are not yet supported")
			p.sync()
			n = &ast.BadItem{ItemBase: ib, Text: "inline module"}
		} else {
			n = d
		}
	case token.ENUM:
		n = p.enum(ib)
	case token.TRAIT:
		n = p.trait(ib)
	case token.IMPL:
		n = p.impl(ib)
	case token.DECLARE:
		p.next()
		decl := p.parseItem(false, true)
		if d, ok := decl.(*ast.FuncDecl); ok && d.Body != nil {
			p.error("declare function must not have a body")
		}
		n = &ast.DeclareDecl{ItemBase: ib, Decl: decl}
	case token.IMPORT:
		p.error("import is not accepted in .gox; use a use declaration")
		p.next()
		p.sync()
		n = &ast.BadItem{ItemBase: ib, Text: "import"}
	default:
		if p.context("macro_rules") && p.peek(1).Kind == token.NOT {
			n = p.macroRules(ib)
		} else if p.at(token.IDENT) {
			x := p.expr()
			if m, ok := x.(*ast.MacroExpr); ok {
				n = &ast.MacroItem{ItemBase: ib, Path: m.Path, Args: m.Args}
			} else {
				p.diagnostics = append(p.diagnostics, Diagnostic{Severity: Error, Span: x.Span(), Message: "expected declaration"})
				p.sync()
				n = &ast.BadItem{ItemBase: ib, Text: "declaration"}
			}
		} else {
			p.error("expected declaration")
			p.sync()
			n = &ast.BadItem{ItemBase: ib, Text: "declaration"}
		}
	}
	if mods.Unsafe {
		if _, ok := n.(*ast.FuncDecl); !ok {
			p.error("unsafe modifier requires func")
		}
	}
	itemBase(n).Base = p.base(start)
	if p.errorCount() > errors && !hasBad(n) {
		return &ast.BadItem{ItemBase: *itemBase(n), Text: "invalid declaration"}
	}
	return n
}

func itemBase(n ast.Item) *ast.ItemBase {
	switch n := n.(type) {
	case *ast.UseDecl:
		return &n.ItemBase
	case *ast.ModDecl:
		return &n.ItemBase
	case *ast.FuncDecl:
		return &n.ItemBase
	case *ast.TypeDecl:
		return &n.ItemBase
	case *ast.EnumDecl:
		return &n.ItemBase
	case *ast.TraitDecl:
		return &n.ItemBase
	case *ast.ImplDecl:
		return &n.ItemBase
	case *ast.DeclareDecl:
		return &n.ItemBase
	case *ast.MacroRulesDecl:
		return &n.ItemBase
	case *ast.MacroItem:
		return &n.ItemBase
	case *ast.ConstDecl:
		return &n.ItemBase
	case *ast.VarDecl:
		return &n.ItemBase
	case *ast.BadItem:
		return &n.ItemBase
	}
	panic("unknown item")
}

func (p *parser) function(ib ast.ItemBase, mods ast.FuncMods, declare, member bool) ast.Item {
	p.expect(token.FUNC)
	f := &ast.FuncDecl{ItemBase: ib, Mods: mods}
	if p.at(token.LPAREN) {
		f.Recv = p.params(false)
	}
	validName := p.at(token.IDENT)
	f.Name = p.ident()
	if declare && p.at(token.DCOLON) {
		s := f.Name.Span().Start
		path := &ast.Path{Segments: []*ast.PathSegment{{Base: p.base(s), Name: f.Name}}}
		for p.accept(token.DCOLON) {
			name := p.ident()
			path.Segments = append(path.Segments, &ast.PathSegment{Base: p.base(name.Span().Start), Name: name})
		}
		path.Base = p.base(s)
		f.Path = path
		f.Name = path.Segments[len(path.Segments)-1].Name
	}
	f.Generics = p.genericParams()
	f.Params, f.Results, f.ResultParen = p.signature(f.Generics != nil, member && len(f.Recv) == 0)
	if f.Generics != nil {
		f.Where = p.whereClause()
	}
	if p.at(token.LBRACE) {
		f.Body = p.block()
	}
	if !validName {
		return &ast.BadItem{ItemBase: ib, Text: "reserved function name"}
	}
	return f
}

func (p *parser) typeDecl(ib ast.ItemBase, allowEmpty bool) ast.Item {
	sugar := p.accept(token.STRUCT)
	if !sugar {
		p.expect(token.TYPE)
	}
	d := &ast.TypeDecl{ItemBase: ib}
	if !sugar && p.accept(token.LPAREN) {
		d.Grouped = true
		for !p.at(token.RPAREN) && !p.at(token.EOF) {
			p.semis()
			if p.at(token.RPAREN) {
				break
			}
			before := p.pos
			s := p.tok().Start
			x := &ast.TypeSpec{ItemBase: ast.ItemBase{Attrs: p.attrs()}}
			x.Vis = p.visibility()
			x.Name = p.ident()
			if p.typeParametersAhead() {
				x.Generics = p.genericParams()
			}
			x.Alias = p.accept(token.ASSIGN)
			x.Type = p.typ()
			if x.Generics != nil {
				x.Where = p.whereClause()
			}
			x.Base = p.base(s)
			d.Specs = append(d.Specs, x)
			if !p.at(token.RPAREN) {
				p.endStmt()
			}
			p.progress(before)
		}
		p.expect(token.RPAREN)
		return d
	}
	d.Name = p.ident()
	if sugar || p.typeParametersAhead() {
		d.Generics = p.genericParams()
	}
	if sugar {
		d.Form = ast.StructSugar
		if d.Generics != nil {
			d.Where = p.whereClause()
		}
		d.Type = p.structType()
	} else {
		d.Alias = p.accept(token.ASSIGN)
		if !allowEmpty || d.Alias || (!p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.EOF)) {
			d.Type = p.typ()
		}
		if d.Generics != nil {
			d.Where = p.whereClause()
		}
	}
	return d
}

// A bracketed Go array length wins when it also resembles type parameters.
func (p *parser) typeParametersAhead() bool {
	if !p.at(token.LBRACK) {
		return false
	}
	q := *p
	errors := q.errorCount()
	q.typeAtom()
	return q.errorCount() > errors
}

func (p *parser) values(ib ast.ItemBase) ast.Item {
	k := p.next().Kind
	group := p.accept(token.LPAREN)
	var specs []*ast.ValueSpec
	for {
		p.semis()
		if group && p.at(token.RPAREN) {
			break
		}
		before := p.pos
		s := p.tok().Start
		x := &ast.ValueSpec{ItemBase: ast.ItemBase{Attrs: p.attrs()}}
		x.Vis = p.visibility()
		x.Names = append(x.Names, p.ident())
		for p.accept(token.COMMA) {
			x.Names = append(x.Names, p.ident())
		}
		if !p.at(token.ASSIGN) && !p.at(token.SEMICOLON) && !p.at(token.RPAREN) && !p.at(token.RBRACE) && !p.at(token.EOF) {
			x.Type = p.typ()
		}
		if p.accept(token.ASSIGN) {
			x.Values = p.exprList()
		}
		if k == token.CONST && len(x.Values) == 0 && (len(specs) == 0 || x.Type != nil) {
			p.error("const declaration requires an initializer")
			x.Values = []ast.Expr{&ast.BadExpr{Base: p.base(s), Text: "missing initializer"}}
		}
		if k == token.VAR && x.Type == nil && len(x.Values) == 0 {
			p.error("var declaration requires type or value")
			x.Type = &ast.BadType{Base: p.base(s), Text: "missing type"}
		}
		x.Base = p.base(s)
		specs = append(specs, x)
		if !group {
			break
		}
		if !p.at(token.RPAREN) {
			p.endStmt()
		}
		p.progress(before)
		if p.at(token.EOF) {
			break
		}
	}
	if group {
		p.expect(token.RPAREN)
	}
	if k == token.CONST {
		return &ast.ConstDecl{ItemBase: ib, Grouped: group, Specs: specs}
	}
	return &ast.VarDecl{ItemBase: ib, Grouped: group, Specs: specs}
}

func (p *parser) useTree() *ast.UseTree {
	s := p.tok().Start
	path := &ast.Path{}
	group := p.at(token.LBRACE)
	glob := p.at(token.MUL)
	for !group && !glob {
		n := p.ident()
		path.Segments = append(path.Segments, &ast.PathSegment{Base: p.base(n.Span().Start), Name: n})
		if !p.accept(token.DCOLON) {
			break
		}
		if len(path.Segments) == 1 && n.Name == "super" && !n.Raw {
			p.error("super:: paths are not yet supported")
		}
		group = p.at(token.LBRACE)
		glob = p.at(token.MUL)
	}
	path.Base = p.base(s)
	u := &ast.UseTree{Path: path, Glob: glob}
	if glob {
		p.next()
	}
	if group {
		p.expect(token.LBRACE)
		u.Children = []*ast.UseTree{}
		for !p.at(token.RBRACE) && !p.at(token.EOF) {
			before := p.pos
			u.Children = append(u.Children, p.useTree())
			if !p.accept(token.COMMA) || p.pos == before {
				break
			}
		}
		p.expect(token.RBRACE)
	}
	if p.context("as") {
		if u.Glob || u.Children != nil {
			p.error("use aliases require a single path")
		}
		p.next()
		u.Alias = p.ident()
	}
	u.Base = p.base(s)
	return u
}

func (p *parser) enum(ib ast.ItemBase) ast.Item {
	p.next()
	d := &ast.EnumDecl{ItemBase: ib, Name: p.ident()}
	d.Generics = p.genericParams()
	if d.Generics != nil {
		d.Where = p.whereClause()
	}
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		before := p.pos
		s := p.tok().Start
		v := &ast.Variant{Attrs: p.attrs(), Name: p.ident()}
		if p.accept(token.LPAREN) {
			v.Kind = ast.Tuple
			for !p.at(token.RPAREN) && !p.at(token.EOF) {
				fs := p.tok().Start
				f := &ast.Field{Attrs: p.attrs(), Type: p.typ()}
				f.Base = p.base(fs)
				v.Fields = append(v.Fields, f)
				if !p.accept(token.COMMA) {
					break
				}
			}
			p.expect(token.RPAREN)
		} else if p.at(token.LBRACE) {
			v.Kind = ast.Struct
			v.Fields = p.enumFields()
		}
		v.Base = p.base(s)
		d.Variants = append(d.Variants, v)
		if p.accept(token.COMMA) {
			continue
		}
		if p.at(token.RBRACE) {
			break
		}
		p.error("expected comma between enum variants")
		p.sync()
		p.semis()
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return d
}

func (p *parser) enumFields() []*ast.Field {
	p.expect(token.LBRACE)
	var fields []*ast.Field
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before, start := p.pos, p.tok().Start
		field := &ast.Field{Attrs: p.attrs()}
		field.Names = []*ast.Ident{p.ident()}
		for p.accept(token.COMMA) {
			field.Names = append(field.Names, p.ident())
		}
		field.Type = p.typ()
		field.Base = p.base(start)
		fields = append(fields, field)
		if !p.accept(token.COMMA) && !p.at(token.SEMICOLON) && !p.at(token.RBRACE) {
			p.error("expected separator after enum field")
			p.sync()
		}
		p.semis()
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return fields
}

func (p *parser) trait(ib ast.ItemBase) ast.Item {
	p.next()
	d := &ast.TraitDecl{ItemBase: ib, Name: p.ident()}
	d.Generics = p.genericParams()
	d.Where = p.whereClause()
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before := p.pos
		x := p.member(true, true)
		if ti, ok := x.(ast.TraitItem); ok {
			d.Items = append(d.Items, ti)
		}
		p.endStmt()
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return d
}
func (p *parser) impl(ib ast.ItemBase) ast.Item {
	p.next()
	d := &ast.ImplDecl{ItemBase: ib}
	d.Generics = p.genericParams()
	first := p.typ()
	if p.accept(token.FOR) {
		d.Trait = first
		d.Type = p.typ()
	} else {
		d.Type = first
	}
	d.Where = p.whereClause()
	d.Items = []ast.Item{}
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before := p.pos
		d.Items = append(d.Items, p.member(d.Trait != nil, false))
		p.endStmt()
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return d
}
func (p *parser) member(inherited, inTrait bool) ast.Item {
	errors := p.errorCount()
	s := p.tok().Start
	attrs := p.attrs()
	vis := p.visibility()
	mods := ast.FuncMods{Unsafe: p.accept(token.UNSAFE)}
	ib := ast.ItemBase{Attrs: attrs, Vis: vis}
	if inherited && vis.Kind != ast.VisNone {
		p.error("trait members inherit visibility")
	}
	var n ast.Item
	switch p.tok().Kind {
	case token.FUNC:
		n = p.function(ib, mods, false, true)
	case token.TYPE:
		n = p.typeDecl(ib, true)
		if d, ok := n.(*ast.TypeDecl); ok && !inTrait && (d.Type == nil || !d.Alias) {
			p.error("impl associated type requires a type binding with =")
		}
	default:
		if p.at(token.IDENT) {
			x := p.expr()
			if macro, ok := x.(*ast.MacroExpr); ok {
				n = &ast.MacroItem{ItemBase: ib, Path: macro.Path, Args: macro.Args}
			}
		}
		if n == nil {
			p.error("trait/impl methods must start with func or a macro invocation")
			p.sync()
			n = &ast.BadItem{ItemBase: ib, Text: "invalid member"}
		}
	}
	itemBase(n).Base = p.base(s)
	if mods.Unsafe {
		if _, ok := n.(*ast.FuncDecl); !ok {
			p.error("unsafe modifier requires func")
		}
	}
	if p.errorCount() > errors && !hasBad(n) {
		return &ast.BadItem{ItemBase: *itemBase(n), Text: "invalid member"}
	}
	return n
}

func (p *parser) macroRules(ib ast.ItemBase) ast.Item {
	p.next()
	p.expect(token.NOT)
	d := &ast.MacroRulesDecl{ItemBase: ib, Name: p.ident()}
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before := p.pos
		s := p.tok().Start
		m := p.tokenTree()
		p.expect(token.FATARROW)
		t := p.tokenTree()
		d.Rules = append(d.Rules, &ast.MacroRule{Base: p.base(s), Matcher: m, Template: t})
		if !p.accept(token.SEMICOLON) && !p.at(token.RBRACE) {
			p.error("expected semicolon between macro rules")
		}
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return d
}

func (p *parser) addComment(t token.Token, src []byte) {
	p.nextID++
	b := ast.Base{NodeID: p.nextID, Loc: ast.Span{Start: t.Start, End: t.End}}
	c := &ast.Comment{Base: b, Text: t.Text}
	var g *ast.CommentGroup
	if len(p.comments) > 0 {
		last := p.comments[len(p.comments)-1]
		if adjacent(src, last.Span().End, t.Start) && strings.TrimSpace(string(src[last.Span().End:t.Start])) == "" {
			g = last
		}
	}
	if g == nil {
		p.nextID++
		g = &ast.CommentGroup{Base: ast.Base{NodeID: p.nextID, Loc: ast.Span{Start: t.Start, End: t.End}}}
		p.comments = append(p.comments, g)
	}
	g.List = append(g.List, c)
	g.Loc.End = t.End
	if strings.HasPrefix(t.Text, "//go:") || strings.HasPrefix(t.Text, "//goxide:") {
		name := strings.Fields(strings.TrimPrefix(t.Text, "//"))[0]
		p.nextID++
		p.directives = append(p.directives, &ast.Directive{Base: ast.Base{NodeID: p.nextID, Loc: b.Loc}, Name: name, Text: t.Text})
	}
}
func (p *parser) attachDocs(f *ast.File, src []byte) {
	ast.Walk(f, func(n ast.Node) bool {
		var doc **ast.CommentGroup
		var dirs *[]*ast.Directive
		switch v := n.(type) {
		case ast.Item:
			ib := itemBase(v)
			doc = &ib.Doc
			dirs = &ib.Directives
		case *ast.Field:
			doc = &v.Doc
		case *ast.TypeSpec:
			doc = &v.Doc
			dirs = &v.Directives
		case *ast.ValueSpec:
			doc = &v.Doc
			dirs = &v.Directives
		}
		if doc == nil {
			return true
		}
		for _, g := range p.comments {
			if adjacent(src, g.Span().End, n.Span().Start) && strings.TrimSpace(string(src[g.Span().End:n.Span().Start])) == "" {
				*doc = g
				if dirs != nil {
					for _, d := range p.directives {
						if d.Span().Start >= g.Span().Start && d.Span().End <= g.Span().End {
							*dirs = append(*dirs, d)
						}
					}
				}
			}
		}
		return true
	})
}
