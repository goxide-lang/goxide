package parser

import (
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (p *parser) block() *ast.Block {
	s := p.tok().Start
	p.expect(token.LBRACE)
	old := p.noComposite
	p.noComposite = 0
	defer func() { p.noComposite = old }()
	b := &ast.Block{}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		before := p.pos
		b.List = append(b.List, p.stmt())
		if _, ok := b.List[len(b.List)-1].(*ast.EmptyStmt); !ok {
			if !p.endStmt() {
				b.List = append(b.List, &ast.BadStmt{StmtBase: ast.StmtBase{Base: p.base(p.tok().Start)}, Text: "missing semicolon"})
			}
		}
		p.progress(before)
	}
	p.expect(token.RBRACE)
	b.Base = p.base(s)
	return b
}

func stmtBase(n ast.Stmt) *ast.StmtBase {
	switch n := n.(type) {
	case *ast.AssignStmt:
		return &n.StmtBase
	case *ast.IncDecStmt:
		return &n.StmtBase
	case *ast.ExprStmt:
		return &n.StmtBase
	case *ast.SendStmt:
		return &n.StmtBase
	case *ast.GoStmt:
		return &n.StmtBase
	case *ast.DeferStmt:
		return &n.StmtBase
	case *ast.ReturnStmt:
		return &n.StmtBase
	case *ast.BranchStmt:
		return &n.StmtBase
	case *ast.Block:
		return &n.StmtBase
	case *ast.IfStmt:
		return &n.StmtBase
	case *ast.SwitchStmt:
		return &n.StmtBase
	case *ast.TypeSwitchStmt:
		return &n.StmtBase
	case *ast.SelectStmt:
		return &n.StmtBase
	case *ast.ForStmt:
		return &n.StmtBase
	case *ast.RangeStmt:
		return &n.StmtBase
	case *ast.LabeledStmt:
		return &n.StmtBase
	case *ast.DeclStmt:
		return &n.StmtBase
	case *ast.EmptyStmt:
		return &n.StmtBase
	case *ast.BadStmt:
		return &n.StmtBase
	}
	panic("unknown statement")
}

func (p *parser) stmt() ast.Stmt {
	errors := p.errorCount()
	s := p.tok().Start
	attrs := p.attrs()
	var n ast.Stmt
	switch p.tok().Kind {
	case token.SEMICOLON:
		t := p.next()
		n = &ast.EmptyStmt{Implicit: t.Implicit}
	case token.RBRACE:
		n = &ast.EmptyStmt{Implicit: true}
	case token.LBRACE:
		n = p.block()
	case token.IF:
		n = p.ifStmt()
	case token.FOR:
		n = p.forStmt()
	case token.SWITCH:
		n = p.switchStmt()
	case token.SELECT:
		n = p.selectStmt()
	case token.GO, token.DEFER:
		k := p.next().Kind
		x := p.expr()
		_, call := x.(*ast.CallExpr)
		_, macro := x.(*ast.MacroExpr)
		if !call && !macro {
			p.error("go/defer requires a function call")
			n = &ast.BadStmt{Text: "non-call go/defer"}
		} else if k == token.GO {
			n = &ast.GoStmt{Call: x}
		} else {
			n = &ast.DeferStmt{Call: x}
		}
	case token.RETURN:
		p.next()
		r := &ast.ReturnStmt{}
		if !p.at(token.SEMICOLON) && !p.at(token.RBRACE) && !p.at(token.EOF) {
			r.Results = p.exprList()
		}
		n = r
	case token.BREAK, token.CONTINUE, token.GOTO, token.FALLTHROUGH:
		k := p.next().Kind
		b := &ast.BranchStmt{Tok: k}
		if k != token.FALLTHROUGH && p.at(token.IDENT) {
			b.Label = p.ident()
		}
		if k == token.GOTO && b.Label == nil {
			p.error("goto requires a label")
		}
		n = b
	case token.CONST, token.VAR, token.TYPE, token.STRUCT:
		n = &ast.DeclStmt{Decl: p.item()}
	case token.ILLEGAL, token.DOLLAR, token.CASE, token.DEFAULT:
		p.error("expected statement")
		p.next()
		p.sync()
		n = &ast.BadStmt{Text: "statement"}
	default:
		if p.at(token.IDENT) && p.peek(1).Kind == token.COLON {
			label := p.ident()
			p.next()
			n = &ast.LabeledStmt{Label: label, Stmt: p.stmt()}
		} else {
			n = p.simpleStmt(false)
		}
	}
	stmtBase(n).Base = p.base(s)
	stmtBase(n).Attrs = attrs
	if p.errorCount() > errors && !hasBad(n) {
		return &ast.BadStmt{StmtBase: *stmtBase(n), Text: "invalid statement"}
	}
	return n
}

func (p *parser) exprList() []ast.Expr {
	list := []ast.Expr{p.expr()}
	for p.accept(token.COMMA) {
		list = append(list, p.expr())
	}
	return list
}
func assignment(k token.Kind) bool {
	switch k {
	case token.ASSIGN, token.DEFINE, token.ADD_ASSIGN, token.SUB_ASSIGN, token.MUL_ASSIGN, token.QUO_ASSIGN, token.REM_ASSIGN, token.AND_ASSIGN, token.OR_ASSIGN, token.XOR_ASSIGN, token.SHL_ASSIGN, token.SHR_ASSIGN, token.AND_NOT_ASSIGN:
		return true
	}
	return false
}
func (p *parser) simpleStmt(allowRange bool) ast.Stmt {
	s := p.tok().Start
	if allowRange && p.accept(token.RANGE) {
		x := p.expr()
		return &ast.RangeStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, X: x}
	}
	lhs := p.exprList()
	if assignment(p.tok().Kind) {
		k := p.next().Kind
		if allowRange && p.accept(token.RANGE) {
			r := &ast.RangeStmt{Tok: k, X: p.expr()}
			if len(lhs) > 0 {
				r.Key = lhs[0]
			}
			if len(lhs) > 1 {
				r.Value = lhs[1]
			}
			if len(lhs) > 2 {
				p.error("range accepts at most two iteration variables")
			}
			if k != token.ASSIGN && k != token.DEFINE {
				p.error("range requires = or :=")
			}
			r.Base = p.base(s)
			return r
		}
		return &ast.AssignStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, Lhs: lhs, Tok: k, Rhs: p.exprList()}
	}
	if len(lhs) != 1 {
		p.error("expression list requires assignment")
		return &ast.BadStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, Text: "expression list"}
	}
	if p.at(token.INC) || p.at(token.DEC) {
		k := p.next().Kind
		return &ast.IncDecStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, X: lhs[0], Tok: k}
	}
	if p.accept(token.ARROW) {
		v := p.expr()
		return &ast.SendStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, Chan: lhs[0], Value: v}
	}
	return &ast.ExprStmt{StmtBase: ast.StmtBase{Base: p.base(s)}, X: lhs[0]}
}
func (p *parser) condition(n ast.Stmt) ast.Expr {
	if e, ok := n.(*ast.ExprStmt); ok {
		return e.X
	}
	p.error("expected condition expression")
	return &ast.BadExpr{Base: p.base(n.Span().Start), Text: "condition"}
}
func (p *parser) headStmt(allowRange bool) ast.Stmt {
	p.noComposite++
	defer func() { p.noComposite-- }()
	return p.simpleStmt(allowRange)
}
func (p *parser) ifStmt() ast.Stmt {
	p.next()
	n := &ast.IfStmt{}
	var first ast.Stmt
	if !p.at(token.SEMICOLON) {
		first = p.headStmt(false)
	}
	if p.accept(token.SEMICOLON) {
		n.Init = first
		n.Cond = p.exprHead()
	} else {
		n.Cond = p.condition(first)
	}
	n.Body = p.block()
	if p.accept(token.ELSE) {
		if p.at(token.IF) {
			s := p.tok().Start
			n.Else = p.ifStmt()
			stmtBase(n.Else).Base = p.base(s)
		} else {
			n.Else = p.block()
		}
	}
	return n
}
func (p *parser) forStmt() ast.Stmt {
	p.next()
	n := &ast.ForStmt{}
	if p.at(token.LBRACE) {
		n.Body = p.block()
		return n
	}
	var first ast.Stmt
	if !p.at(token.SEMICOLON) {
		first = p.headStmt(true)
	}
	if r, ok := first.(*ast.RangeStmt); ok {
		r.Body = p.block()
		return r
	}
	if p.accept(token.SEMICOLON) {
		n.Init = first
		if !p.at(token.SEMICOLON) {
			n.Cond = p.exprHead()
		}
		p.expect(token.SEMICOLON)
		if !p.at(token.LBRACE) {
			n.Post = p.headStmt(false)
			if a, ok := n.Post.(*ast.AssignStmt); ok && a.Tok == token.DEFINE {
				p.error("for post statement cannot declare variables")
			}
		}
	} else if first != nil {
		n.Cond = p.condition(first)
	}
	n.Body = p.block()
	return n
}
func typeSwitchGuard(n ast.Stmt) bool {
	var x ast.Expr
	switch n := n.(type) {
	case *ast.ExprStmt:
		x = n.X
	case *ast.AssignStmt:
		if n.Tok == token.DEFINE && len(n.Lhs) == 1 && len(n.Rhs) == 1 {
			x = n.Rhs[0]
		}
	}
	a, ok := x.(*ast.TypeAssertExpr)
	return ok && a.Type == nil
}
func (p *parser) switchStmt() ast.Stmt {
	p.next()
	var init, guard ast.Stmt
	if !p.at(token.LBRACE) && !p.at(token.SEMICOLON) {
		guard = p.headStmt(false)
	}
	if p.accept(token.SEMICOLON) {
		init = guard
		guard = nil
		if !p.at(token.LBRACE) {
			guard = p.headStmt(false)
		}
	}
	isType := typeSwitchGuard(guard)
	p.expect(token.LBRACE)
	var cases []*ast.CaseClause
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before := p.pos
		s := p.tok().Start
		c := &ast.CaseClause{}
		if p.accept(token.DEFAULT) {
			c.Default = true
		} else {
			p.expect(token.CASE)
			if isType {
				c.Types = append(c.Types, p.typ())
				for p.accept(token.COMMA) {
					c.Types = append(c.Types, p.typ())
				}
			} else {
				c.List = p.exprList()
			}
		}
		p.expect(token.COLON)
		c.Body = p.caseBody()
		c.Base = p.base(s)
		cases = append(cases, c)
		p.progress(before)
	}
	p.expect(token.RBRACE)
	if isType {
		return &ast.TypeSwitchStmt{Init: init, Assign: guard, Cases: cases}
	}
	n := &ast.SwitchStmt{Init: init, Cases: cases}
	if guard != nil {
		n.Tag = p.condition(guard)
	}
	return n
}
func (p *parser) caseBody() []ast.Stmt {
	var body []ast.Stmt
	for !p.at(token.CASE) && !p.at(token.DEFAULT) && !p.at(token.RBRACE) && !p.at(token.EOF) {
		before := p.pos
		n := p.stmt()
		body = append(body, n)
		if _, ok := n.(*ast.EmptyStmt); !ok {
			p.endStmt()
		}
		p.progress(before)
	}
	return body
}
func (p *parser) selectStmt() ast.Stmt {
	p.next()
	p.expect(token.LBRACE)
	n := &ast.SelectStmt{}
	for !p.at(token.RBRACE) && !p.at(token.EOF) {
		p.semis()
		if p.at(token.RBRACE) {
			break
		}
		before := p.pos
		s := p.tok().Start
		c := &ast.CommClause{}
		if p.accept(token.DEFAULT) {
			c.Default = true
		} else {
			p.expect(token.CASE)
			c.Comm = p.simpleStmt(false)
		}
		p.expect(token.COLON)
		c.Body = p.caseBody()
		c.Base = p.base(s)
		n.Cases = append(n.Cases, c)
		p.progress(before)
	}
	p.expect(token.RBRACE)
	return n
}
