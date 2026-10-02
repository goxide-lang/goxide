package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// resultWitness finds a pure, static function value whose signature supplies the
// required private result type. It never evaluates a receiver, invokes the
// function, or reads the destination. Generic/variadic functions and fields are
// deliberately left to the explicit frame path until separately supported.
func (l *lowering) resultWitness(x ast.Expr, want types.Type) (ast.Expr, int, bool) {
	switch n := x.(type) {
	case *ast.ParenExpr:
		return l.resultWitness(n.X, want)
	case *ast.UnaryExpr:
		if n.Op == token.NOT {
			return l.resultWitness(n.X, want)
		}
	case *ast.BinaryExpr:
		if n.Op == token.LAND || n.Op == token.LOR {
			if fn, arity, ok := l.resultWitness(n.X, want); ok {
				return fn, arity, true
			}
			return l.resultWitness(n.Y, want)
		}
	case *ast.CallExpr:
		var object types.Object
		switch fn := ast.Unparen(n.Fun).(type) {
		case *ast.Ident:
			object = l.info.Uses[fn]
		case *ast.SelectorExpr:
			if l.info.Selections[fn] == nil {
				object = l.info.Uses[fn.Sel]
			}
		}
		if fn, ok := object.(*types.Func); ok {
			sig, ok := fn.Type().(*types.Signature)
			if ok && sig.Recv() == nil && !sig.Variadic() && sig.TypeParams().Len() == 0 && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), want) {
				return n.Fun, sig.Params().Len(), true
			}
		}
	}
	return nil, 0, false
}

func (l *lowering) logicalValue(original ast.Expr, left ast.Expr, pre *[]ast.Stmt) ast.Expr {
	fact := l.expressionFacts[original]
	n := original.(*ast.BinaryExpr)
	if _, nameable := l.typeSpelling(fact.typ); !nameable && l.expressionFacts[n.X].untyped {
		fn, arity, ok := l.resultWitness(original, fact.typ)
		if !ok {
			panic("internal: missing preflighted logical type witness")
		}
		name := l.temp()
		zero := l.witnessCall("function-result-"+strconv.Itoa(arity), fn)
		*pre = append(*pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{zero}}, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{left}})
		return ast.NewIdent(name)
	}
	return l.freezeValue(original, left, pre)
}

func (l *lowering) signedResidualShift(n *ast.BinaryExpr) bool {
	f := l.expressionFacts[n]
	count := l.expressionFacts[n.Y]
	if !f.untyped || f.boolean || f.constant || count.constant || (n.Op != token.SHL && n.Op != token.SHR) {
		return false
	}
	b, ok := underlyingType(count.typ).(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
}

// len/cap must use this planner's context-preserving operands, not the legacy
// expr pass: a private bool argument or contextual mask shift must not default
// to bool/int while being extracted. Literals currently cover explicit arrays
// and slices with constant element keys, not implicit nested literals/maps.
func (l *lowering) canMeasureOperand(x ast.Expr) bool {
	if n, ok := ast.Unparen(x).(*ast.CompositeLit); ok && contains(n, l.questions) {
		if n.Type == nil {
			return false
		}
		switch underlyingType(l.info.TypeOf(n)).(type) {
		case *types.Array, *types.Slice:
		default:
			return false
		}
		for _, elt := range n.Elts {
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if l.info.Types[kv.Key].Value == nil {
					return false
				}
				value = kv.Value
			}
			if lit, ok := ast.Unparen(value).(*ast.CompositeLit); ok && lit.Type == nil {
				return false
			}
			if !l.canEffect(value) {
				return false
			}
		}
		return true
	}
	return l.canEffect(x)
}
func (l *lowering) measureExpr(n *ast.CallExpr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	var pre []ast.Stmt
	var operand ast.Expr
	if literal, ok := ast.Unparen(n.Args[0]).(*ast.CompositeLit); ok && contains(literal, l.questions) {
		copy := *literal
		copy.Elts = append([]ast.Expr(nil), literal.Elts...)
		for i, elt := range literal.Elts {
			value := elt
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				value = kv.Value
			}
			p, v, err := l.effectExpr(value, ft)
			if err != nil {
				return nil, nil, err
			}
			pre = append(pre, p...)
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				copy.Elts[i] = &ast.KeyValueExpr{Key: kv.Key, Value: v}
			} else {
				copy.Elts[i] = v
			}
		}
		// Evaluate the complete literal even after its calls become saved values.
		name := l.temp()
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{&copy}})
		operand = ast.NewIdent(name)
	} else {
		var err error
		pre, operand, err = l.effectExpr(n.Args[0], ft)
		if err != nil {
			return nil, nil, err
		}
	}
	call := *n
	call.Args = []ast.Expr{operand}
	name := l.temp()
	pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{&call}})
	return pre, ast.NewIdent(name), nil
}
