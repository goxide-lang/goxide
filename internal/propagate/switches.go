package propagate

import (
	"go/ast"
	"go/token"
	"strconv"
)

// Case dispatch erases the original case lists. Retain their original Go
// diagnostics (duplicates, comparability, contextual types) before doing so.
func (l *lowering) switchHeaderError(n *ast.SwitchStmt) error {
	in := func(pos token.Pos, node ast.Node) bool { return node != nil && pos >= node.Pos() && pos < node.End() }
	for _, e := range l.checkErrors {
		if (n.Init != nil && in(e.Pos, n.Init)) || (n.Tag != nil && in(e.Pos, n.Tag)) {
			return l.fail(n, e.Msg)
		}
		for _, s := range n.Body.List {
			c := s.(*ast.CaseClause)
			if e.Pos >= c.Case && e.Pos <= c.Colon {
				return l.fail(c, e.Msg)
			}
		}
	}
	return nil
}
func (l *lowering) switchStatement(n *ast.SwitchStmt, label *ast.Ident, ft *ast.FuncType) ([]ast.Stmt, error) {
	l.guardFreeDepth++
	defer func() { l.guardFreeDepth-- }()
	if e := l.switchHeaderError(n); e != nil {
		return nil, e
	}
	var pre []ast.Stmt
	if n.Init != nil {
		p, e := l.statement(n.Init, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		n.Init = nil
	}
	var tag ast.Expr
	if n.Tag != nil {
		p, v, e := l.controlExpr(n.Tag, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		tag = l.freezeValue(n.Tag, v, &pre)
		n.Tag = tag
	}
	caseQuestions := false
	for _, s := range n.Body.List {
		for _, x := range s.(*ast.CaseClause).List {
			caseQuestions = caseQuestions || contains(x, l.questions)
		}
	}
	if caseQuestions {
		selected := l.temp()
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(selected)}, Tok: token.DEFINE, Rhs: []ast.Expr{parseExpr("-1")}})
		for i, s := range n.Body.List {
			c := s.(*ast.CaseClause)
			for _, x := range c.List {
				p, v, e := l.controlExpr(x, ft)
				if e != nil {
					return nil, e
				}
				choose := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(selected)}, Tok: token.ASSIGN, Rhs: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(i)}}}
				test := &ast.SwitchStmt{Tag: tag, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.CaseClause{List: []ast.Expr{v}, Body: []ast.Stmt{choose}}}}}
				p = append(p, test)
				pre = append(pre, &ast.IfStmt{Cond: &ast.BinaryExpr{X: ast.NewIdent(selected), Op: token.LSS, Y: parseExpr("0")}, Body: &ast.BlockStmt{List: p}})
			}
			if c.List != nil {
				c.List = []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(i)}}
			}
		}
		n.Tag = ast.NewIdent(selected)
	}
	control, outerLabel := l.loopLabels(n, label, ft)
	for _, s := range n.Body.List {
		c := s.(*ast.CaseClause)
		body := &ast.BlockStmt{List: c.Body}
		if e := l.body(body, ft); e != nil {
			return nil, e
		}
		c.Body = body.List
	}
	return wrapLoop(pre, control, outerLabel), nil
}

// Reuse the proven contextual residual planner when available. In particular a
// private numeric case must not default a partial shift to int. Unsafe chains
// containing propagation need their own plan, not split pointer temporaries.
func (l *lowering) controlExpr(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	if l.hasUnsafePointerConversion(x) {
		if contains(x, l.questions) {
			return nil, nil, l.fail(x, "unsafe conversion chain with ? needs an unimplemented control-flow expression plan")
		}
	} else if l.canEffect(x) {
		return l.effectExpr(x, ft)
	}
	return l.expr(x, ft)
}

// Retain the native type-switch guard and case lists: Go assigns a different
// implicit variable type in each single-type case, and the interface type in
// multi-type/default/nil cases. Only its subject's effects move before it.
func (l *lowering) typeSwitchStatement(n *ast.TypeSwitchStmt, label *ast.Ident, ft *ast.FuncType) ([]ast.Stmt, error) {
	l.guardFreeDepth++
	defer func() { l.guardFreeDepth-- }()
	var pre []ast.Stmt
	if n.Init != nil {
		p, e := l.statement(n.Init, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		n.Init = nil
	}
	var guard ast.Expr
	switch a := n.Assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			guard = a.Rhs[0]
		}
	case *ast.ExprStmt:
		guard = a.X
	}
	assertion, ok := guard.(*ast.TypeAssertExpr)
	if !ok || assertion.Type != nil {
		return nil, l.fail(n, "invalid type switch guard")
	}
	p, v, e := l.controlExpr(assertion.X, ft)
	if e != nil {
		return nil, e
	}
	pre = append(pre, p...)
	assertion.X = l.freezeValue(assertion.X, v, &pre)
	control, outerLabel := l.loopLabels(n, label, ft)
	for _, s := range n.Body.List {
		c := s.(*ast.CaseClause)
		body := &ast.BlockStmt{List: c.Body}
		if e := l.body(body, ft); e != nil {
			return nil, e
		}
		c.Body = body.List
	}
	return wrapLoop(pre, control, outerLabel), nil
}
