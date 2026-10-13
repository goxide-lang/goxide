package lower

import (
	goast "go/ast"
	gotoken "go/token"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) stmt(n syntax.Stmt) goast.Stmt {
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *syntax.AssignStmt:
		pos := e.pos(n)
		if len(n.Lhs) != 0 {
			pos = e.end(n.Lhs[len(n.Lhs)-1])
		}
		return &goast.AssignStmt{Lhs: e.exprs(n.Lhs), TokPos: pos, Tok: tokenKind(n.Tok), Rhs: e.exprs(n.Rhs)}
	case *syntax.IncDecStmt:
		return &goast.IncDecStmt{X: e.expr(n.X), TokPos: e.end(n.X), Tok: tokenKind(n.Tok)}
	case *syntax.ExprStmt:
		return &goast.ExprStmt{X: e.expr(n.X)}
	case *syntax.SendStmt:
		return &goast.SendStmt{Chan: e.expr(n.Chan), Arrow: e.end(n.Chan), Value: e.expr(n.Value)}
	case *syntax.GoStmt:
		call, ok := e.expr(n.Call).(*goast.CallExpr)
		if !ok {
			e.fail(n, "go 语句需要函数调用")
			return nil
		}
		return &goast.GoStmt{Go: e.pos(n), Call: call}
	case *syntax.DeferStmt:
		call, ok := e.expr(n.Call).(*goast.CallExpr)
		if !ok {
			e.fail(n, "defer 语句需要函数调用")
			return nil
		}
		return &goast.DeferStmt{Defer: e.pos(n), Call: call}
	case *syntax.ReturnStmt:
		return &goast.ReturnStmt{Return: e.pos(n), Results: e.exprs(n.Results)}
	case *syntax.BranchStmt:
		return &goast.BranchStmt{TokPos: e.pos(n), Tok: tokenKind(n.Tok), Label: e.ident(n.Label)}
	case *syntax.Block:
		return e.block(n)
	case *syntax.IfStmt:
		return &goast.IfStmt{If: e.pos(n), Init: e.stmt(n.Init), Cond: e.expr(n.Cond), Body: e.block(n.Body), Else: e.stmt(n.Else)}
	case *syntax.SwitchStmt:
		return &goast.SwitchStmt{Switch: e.pos(n), Init: e.stmt(n.Init), Tag: e.expr(n.Tag), Body: e.cases(n, n.Cases)}
	case *syntax.TypeSwitchStmt:
		return &goast.TypeSwitchStmt{Switch: e.pos(n), Init: e.stmt(n.Init), Assign: e.stmt(n.Assign), Body: e.cases(n, n.Cases)}
	case *syntax.CaseClause:
		list := e.exprs(n.List)
		for _, typ := range n.Types {
			list = append(list, e.typ(typ))
		}
		colon := e.pos(n)
		if len(n.List) > 0 {
			colon = e.end(n.List[len(n.List)-1])
		} else if len(n.Types) > 0 {
			colon = e.end(n.Types[len(n.Types)-1])
		}
		return &goast.CaseClause{Case: e.pos(n), List: list, Colon: colon, Body: e.stmts(n.Body)}
	case *syntax.SelectStmt:
		body := &goast.BlockStmt{Lbrace: e.pos(n) + gotoken.Pos(len("select")), Rbrace: e.end(n) - 1}
		for _, clause := range n.Cases {
			body.List = append(body.List, e.stmt(clause))
		}
		return &goast.SelectStmt{Select: e.pos(n), Body: body}
	case *syntax.CommClause:
		comm := e.stmt(n.Comm)
		colon := e.pos(n)
		if n.Comm != nil {
			colon = e.end(n.Comm)
		}
		return &goast.CommClause{Case: e.pos(n), Comm: comm, Colon: colon, Body: e.stmts(n.Body)}
	case *syntax.ForStmt:
		return &goast.ForStmt{For: e.pos(n), Init: e.stmt(n.Init), Cond: e.expr(n.Cond), Post: e.stmt(n.Post), Body: e.block(n.Body)}
	case *syntax.RangeStmt:
		tokPos := gotoken.NoPos
		if n.Key != nil {
			tokPos = e.end(n.Key)
			if n.Value != nil {
				tokPos = e.end(n.Value)
			}
		}
		return &goast.RangeStmt{For: e.pos(n), Key: e.expr(n.Key), Value: e.expr(n.Value), TokPos: tokPos, Tok: tokenKind(n.Tok), X: e.expr(n.X), Body: e.block(n.Body)}
	case *syntax.LabeledStmt:
		return &goast.LabeledStmt{Label: e.ident(n.Label), Colon: e.end(n.Label), Stmt: e.stmt(n.Stmt)}
	case *syntax.DeclStmt:
		decl := e.decl(n.Decl)
		if decl == nil {
			return nil
		}
		return &goast.DeclStmt{Decl: decl}
	case *syntax.EmptyStmt:
		return &goast.EmptyStmt{Semicolon: e.pos(n), Implicit: n.Implicit}
	case *syntax.LetStmt:
		e.fail(n, "let 语句尚未降低")
	case *syntax.LoopStmt:
		e.fail(n, "loop 语句尚未降低")
	case *syntax.WhileStmt:
		e.fail(n, "while 语句尚未降低")
	case *syntax.BadStmt:
		e.fail(n, "错误语句不能降低")
	default:
		e.fail(n, "此语句尚未降低")
	}
	return nil
}

func (e *emitter) stmts(nodes []syntax.Stmt) []goast.Stmt {
	var list []goast.Stmt
	for _, node := range nodes {
		if stmt := e.stmt(node); stmt != nil {
			list = append(list, stmt)
		}
	}
	return list
}

func (e *emitter) block(n *syntax.Block) *goast.BlockStmt {
	if n == nil {
		return nil
	}
	return &goast.BlockStmt{Lbrace: e.pos(n), List: e.stmts(n.List), Rbrace: e.end(n) - 1}
}

func (e *emitter) cases(n syntax.Node, clauses []*syntax.CaseClause) *goast.BlockStmt {
	pos := e.pos(n) + gotoken.Pos(len("switch"))
	if len(clauses) > 0 {
		pos = e.pos(clauses[0]) - 1
	}
	body := &goast.BlockStmt{Lbrace: pos, Rbrace: e.end(n) - 1}
	for _, clause := range clauses {
		body.List = append(body.List, e.stmt(clause))
	}
	return body
}
