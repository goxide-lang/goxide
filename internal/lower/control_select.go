package lower

import (
	goast "go/ast"
	gotoken "go/token"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (e *emitter) flowTypeSwitch(n *syntax.TypeSwitchStmt) goast.Stmt {
	if !hasFlow(n.Init) && !hasFlow(n.Assign) {
		return e.stmt(n)
	}
	list := e.statement(n.Init)
	old := e.pending
	e.pending = nil
	assign := e.stmt(n.Assign)
	list = append(list, e.pending...)
	e.pending = old
	if e.validation != nil {
		list = append(list, e.validationTypeSwitch(n, nil, assign))
	} else {
		list = append(list, &goast.TypeSwitchStmt{Assign: assign, Body: e.cases(n, n.Cases)})
	}
	return &goast.BlockStmt{List: list}
}

func (e *emitter) flowSelect(n *syntax.SelectStmt) goast.Stmt {
	flow := false
	for _, clause := range n.Cases {
		flow = flow || hasFlow(clause.Comm)
	}
	if !flow {
		return e.stmt(n)
	}
	var before []goast.Stmt
	body := &goast.BlockStmt{}
	for _, clause := range n.Cases {
		lowered := &goast.CommClause{}
		var selected []goast.Stmt
		switch comm := clause.Comm.(type) {
		case nil:
		case *syntax.SendStmt:
			prefix, channel := e.selectOperand(comm.Chan)
			before = append(before, prefix...)
			prefix, value := e.selectOperand(comm.Value)
			before = append(before, prefix...)
			lowered.Comm = &goast.SendStmt{Chan: channel, Value: value}
		case *syntax.ExprStmt:
			receive := selectReceive(comm.X)
			if receive == nil {
				e.fail(comm, "select 分支需要发送或接收")
				return nil
			}
			prefix, channel := e.selectOperand(receive.X)
			before = append(before, prefix...)
			lowered.Comm = &goast.ExprStmt{X: &goast.UnaryExpr{Op: gotoken.ARROW, X: channel}}
		case *syntax.AssignStmt:
			if len(comm.Rhs) != 1 || selectReceive(comm.Rhs[0]) == nil {
				e.fail(comm, "select 赋值需要接收表达式")
				return nil
			}
			receive := selectReceive(comm.Rhs[0])
			prefix, channel := e.selectOperand(receive.X)
			before = append(before, prefix...)
			x := &goast.UnaryExpr{Op: gotoken.ARROW, X: channel}
			if comm.Tok == token.DEFINE {
				lowered.Comm = &goast.AssignStmt{Lhs: e.exprs(comm.Lhs), Tok: gotoken.DEFINE, Rhs: []goast.Expr{x}}
			} else {
				// Receive first; Go evaluates assignment destinations only for the chosen case.
				values := make([]goast.Expr, len(comm.Lhs))
				for i := range values {
					values[i] = e.fresh("Receive")
				}
				lowered.Comm = &goast.AssignStmt{Lhs: values, Tok: gotoken.DEFINE, Rhs: []goast.Expr{x}}
				old := e.pending
				e.pending = nil
				var lhs []goast.Expr
				for _, target := range comm.Lhs {
					lhs = append(lhs, e.lvalue(target))
				}
				selected = append(selected, e.pending...)
				e.pending = old
				selected = append(selected, &goast.AssignStmt{Lhs: lhs, Tok: gotoken.ASSIGN, Rhs: values})
			}
		default:
			e.fail(comm, "select 分支需要发送或接收")
			return nil
		}
		lowered.Body = append(selected, e.stmts(clause.Body)...)
		body.List = append(body.List, lowered)
	}
	before = append(before, &goast.SelectStmt{Body: body})
	return &goast.BlockStmt{List: before}
}

func (e *emitter) selectOperand(n syntax.Expr) ([]goast.Stmt, goast.Expr) {
	before, value := e.exprScoped(n)
	old := e.pending
	e.pending = nil
	value = e.spill(n, value)
	before = append(before, e.pending...)
	e.pending = old
	return before, value
}

func selectReceive(n syntax.Expr) *syntax.UnaryExpr {
	if paren, ok := n.(*syntax.ParenExpr); ok {
		return selectReceive(paren.X)
	}
	if unary, ok := n.(*syntax.UnaryExpr); ok && unary.Op == token.ARROW {
		return unary
	}
	return nil
}
