package propagate

import (
	"go/ast"
	"go/token"
)

// A direct method call is not a method-value creation. Save receiver calls and
// receives in lexical order, but leave selection, indexing and implicit receiver
// indirection on the final direct call. Capturing x.M would move its nil/bounds
// checks before argument effects (and before a failing match-arm question).
// Go does not order ordinary reads/assertions/indexing against calls; this
// lowering consistently leaves those receiver operations on the final call.
// Only calls, receives and short-circuit operations are staged beforehand.
func (l *lowering) methodReceiver(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	if x == nil {
		return nil, nil, nil
	}
	var pre []ast.Stmt
	child := func(v *ast.Expr) error {
		p, y, e := l.methodReceiver(*v, ft)
		if e != nil {
			return e
		}
		pre = append(pre, p...)
		*v = y
		return nil
	}
	switch n := x.(type) {
	case *ast.Ident, *ast.BasicLit:
	case *ast.CallExpr:
		p, y, e := l.expr(x, ft)
		if e != nil {
			return nil, nil, e
		}
		pre = append(pre, p...)
		return preWithValue(l, x, y, pre)
	case *ast.ParenExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
	case *ast.SelectorExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
	case *ast.StarExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
	case *ast.TypeAssertExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
	case *ast.IndexExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
		if e := child(&n.Index); e != nil {
			return nil, nil, e
		}
	case *ast.SliceExpr:
		for _, v := range []*ast.Expr{&n.X, &n.Low, &n.High, &n.Max} {
			if e := child(v); e != nil {
				return nil, nil, e
			}
		}
	case *ast.UnaryExpr:
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
		if n.Op == token.ARROW {
			return preWithValue(l, x, n, pre)
		}
	case *ast.BinaryExpr:
		if n.Op == token.LAND || n.Op == token.LOR {
			// The receiver result itself is evaluated here to retain short circuiting;
			// this does not perform any method selection or implicit dereference.
			p, y, e := l.expr(x, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			return preWithValue(l, x, y, pre)
		}
		if e := child(&n.X); e != nil {
			return nil, nil, e
		}
		if e := child(&n.Y); e != nil {
			return nil, nil, e
		}
	case *ast.CompositeLit:
		for i := range n.Elts {
			if e := child(&n.Elts[i]); e != nil {
				return nil, nil, e
			}
		}
	case *ast.KeyValueExpr:
		if e := child(&n.Key); e != nil {
			return nil, nil, e
		}
		if e := child(&n.Value); e != nil {
			return nil, nil, e
		}
	default:
		return nil, nil, l.fail(x, "method receiver with match arguments requires a supported Go value expression")
	}
	return pre, x, nil
}

func preWithValue(l *lowering, original, value ast.Expr, pre []ast.Stmt) ([]ast.Stmt, ast.Expr, error) {
	saved := l.freezeValue(original, value, &pre)
	return pre, saved, nil
}
