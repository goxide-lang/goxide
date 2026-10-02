package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Multi-values are planned only in the single-RHS assignment context already
// checked by Go. They must never be expanded into ordinary expression lists.
func (l *lowering) canMultiEffect(x ast.Expr, arity int) bool {
	if n, ok := x.(*ast.ParenExpr); ok {
		return l.canMultiEffect(n.X, arity)
	}
	switch n := x.(type) {
	case *ast.CallExpr:
		tuple, ok := l.info.TypeOf(n).(*types.Tuple)
		if !ok || tuple.Len() != arity || !l.staticEffectCallee(n.Fun) {
			return false
		}
		for _, a := range n.Args {
			if !l.canEffect(a) {
				return false
			}
		}
		return true
	case *ast.IndexExpr:
		_, ok := underlyingType(l.info.TypeOf(n.X)).(*types.Map)
		return arity == 2 && ok && l.info.Types[n].HasOk() && l.canEffect(n.X) && l.canEffect(n.Index)
	case *ast.TypeAssertExpr:
		return arity == 2 && n.Type != nil && l.info.Types[n].HasOk() && l.canEffect(n.X)
	case *ast.UnaryExpr:
		return arity == 2 && n.Op == token.ARROW && l.info.Types[n].HasOk() && l.canEffect(n.X)
	}
	return false
}

func (l *lowering) multiEffect(x ast.Expr, arity int, ft *ast.FuncType) ([]ast.Stmt, []ast.Expr, error) {
	if n, ok := x.(*ast.ParenExpr); ok {
		return l.multiEffect(n.X, arity, ft)
	}
	var pre []ast.Stmt
	stage := func(x ast.Expr) (ast.Expr, error) {
		p, v, e := l.effectExpr(x, ft)
		pre = append(pre, p...)
		return v, e
	}
	var value ast.Expr
	commaOK := false
	switch n := x.(type) {
	case *ast.CallExpr:
		c := *n
		c.Args = append([]ast.Expr(nil), n.Args...)
		for i, a := range n.Args {
			v, e := stage(a)
			if e != nil {
				return nil, nil, e
			}
			c.Args[i] = v
		}
		value = &c
	case *ast.IndexExpr:
		a, e := stage(n.X)
		if e != nil {
			return nil, nil, e
		}
		b, e := stage(n.Index)
		if e != nil {
			return nil, nil, e
		}
		value = &ast.IndexExpr{X: a, Index: b}
		commaOK = true
	case *ast.TypeAssertExpr:
		a, e := stage(n.X)
		if e != nil {
			return nil, nil, e
		}
		value = &ast.TypeAssertExpr{X: a, Type: n.Type}
		commaOK = true
	case *ast.UnaryExpr:
		a, e := stage(n.X)
		if e != nil {
			return nil, nil, e
		}
		value = &ast.UnaryExpr{Op: token.ARROW, X: a}
		commaOK = true
	default:
		return nil, nil, l.fail(x, "internal: unplanned multi-value expression")
	}
	temps := make([]ast.Expr, arity)
	values := make([]ast.Expr, arity)
	for i := range temps {
		name := l.temp()
		temps[i] = ast.NewIdent(name)
		values[i] = ast.NewIdent(name)
	}
	pre = append(pre, &ast.AssignStmt{Lhs: temps, Tok: token.DEFINE, Rhs: []ast.Expr{value}})
	// Comma-ok's second result is untyped bool, unlike a function's bool result.
	// Restore its assignment context without spelling a possibly private type.
	if commaOK {
		values[1] = &ast.BinaryExpr{X: values[1], Op: token.EQL, Y: parseExpr("0 == 0")}
	}
	return pre, values, nil
}
