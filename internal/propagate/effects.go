package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
)

// canPlanAssignment is a non-mutating preflight. An unsupported combination
// remains on the explicit legacy assignmentFrame path; it is not a claim of
// complete guard removal. In particular we do not stage context-dependent
// potentially panicking arithmetic or invent a type for a private bool's
// unexecuted short-circuit arm.
func (l *lowering) canPlanAssignment(s *ast.AssignStmt) bool {
	if l.hasUnsafePointerConversion(s) {
		return false
	}
	if len(s.Lhs) != len(s.Rhs) {
		return false
	} // tuple/comma-ok not planned yet
	for _, x := range s.Lhs {
		if !l.canDeferredPlace(x) {
			return false
		}
	}
	for _, x := range s.Rhs {
		if !l.canEffect(x) {
			return false
		}
	}
	return true
}
func (l *lowering) canDeferredPlace(x ast.Expr) bool {
	switch n := x.(type) {
	case *ast.Ident:
		return n.Name == "_" || l.expressionFacts[x].addressable
	case *ast.ParenExpr:
		return l.canDeferredPlace(n.X)
	case *ast.StarExpr:
		return l.expressionFacts[x].addressable && l.canEffect(n.X)
	case *ast.SelectorExpr:
		if !l.expressionFacts[x].addressable {
			return false
		}
		sel := l.info.Selections[n]
		if sel == nil {
			return true
		}
		if sel.Kind() != types.FieldVal {
			return false
		}
		if _, ok := underlyingType(l.info.TypeOf(n.X)).(*types.Pointer); ok {
			return l.canEffect(n.X)
		}
		return l.canDeferredPlace(n.X)
	case *ast.IndexExpr:
		if !l.expressionFacts[x].addressable || !l.canEffect(n.Index) {
			return false
		}
		switch b := underlyingType(l.info.TypeOf(n.X)).(type) {
		case *types.Array:
			return l.canDeferredPlace(n.X)
		case *types.Pointer:
			_, ok := b.Elem().Underlying().(*types.Array)
			return ok && l.canEffect(n.X)
		}
	}
	return false
}
func (l *lowering) canEffect(x ast.Expr) bool {
	f, ok := l.expressionFacts[x]
	if !ok || f.typ == nil {
		return false
	}
	if _, tuple := f.typ.(*types.Tuple); tuple {
		return false
	}
	hasQuestion := contains(x, l.questions)
	if !hasQuestion && (f.constant || !f.untyped || isNilType(f.typ)) {
		return true
	}
	switch n := x.(type) {
	case *ast.ParenExpr:
		return l.canEffect(n.X)
	case *ast.BinaryExpr:
		if f.untyped && !f.boolean && !f.constant {
			switch n.Op {
			case token.ADD, token.SUB, token.MUL, token.AND, token.OR, token.XOR, token.AND_NOT:
			case token.SHL, token.SHR:
				// Delaying a negative shift count's panic would change evaluation.
				count := l.expressionFacts[n.Y]
				basic, _ := underlyingType(count.typ).(*types.Basic)
				if !count.constant && (basic == nil || basic.Info()&types.IsUnsigned == 0) {
					return false
				}
			default:
				return false
			}
		}
		if (n.Op == token.LAND || n.Op == token.LOR) && !f.untyped {
			if _, nameable := l.typeSpelling(f.typ); !nameable && l.expressionFacts[n.X].untyped {
				return false
			}
		}
		return l.canEffect(n.X) && l.canEffect(n.Y)
	case *ast.UnaryExpr:
		switch n.Op {
		case token.ADD, token.SUB, token.XOR, token.NOT, token.ARROW:
			return l.canEffect(n.X)
		}
	case *ast.CallExpr:
		if id, ok := n.Fun.(*ast.Ident); ok {
			if _, q := l.questions[id.Name]; q {
				return l.canEffect(n.Args[0])
			}
		}
		if b := l.builtin(n); b != nil && b.Name() != "recover" {
			return false
		}
		if !l.staticEffectCallee(n.Fun) {
			return false
		}
		for _, a := range n.Args {
			if !l.canEffect(a) {
				return false
			}
		}
		return true
	}
	return false
}
func isNilType(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.UntypedNil
}
func (l *lowering) staticEffectCallee(x ast.Expr) bool {
	tv := l.info.Types[x]
	if tv.IsType() || tv.IsBuiltin() {
		return true
	}
	switch n := x.(type) {
	case *ast.Ident:
		_, ok := l.info.Uses[n].(*types.Func)
		return ok
	case *ast.SelectorExpr:
		if l.info.Selections[n] != nil {
			return false
		}
		_, ok := l.info.Uses[n.Sel].(*types.Func)
		return ok
	case *ast.IndexExpr:
		return l.staticEffectCallee(n.X)
	case *ast.IndexListExpr:
		return l.staticEffectCallee(n.X)
	case *ast.ParenExpr:
		return l.staticEffectCallee(n.X)
	}
	return false
}

// effectExpr stages evaluated values in the original function. Untyped numeric
// residuals retain their enclosing Go context and only contain non-panicking
// arithmetic over already saved operands. Untyped booleans are restored as a
// comparison; typed values retain their inferred nominal identities.
func (l *lowering) effectExpr(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	f := l.expressionFacts[x]
	if !contains(x, l.questions) && (f.constant || isNilType(f.typ)) {
		return nil, x, nil
	}
	finish := func(pre []ast.Stmt, value ast.Expr) ([]ast.Stmt, ast.Expr, error) {
		if f.untyped && !f.boolean {
			return pre, value, nil
		}
		name := l.temp()
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{value}})
		var out ast.Expr = ast.NewIdent(name)
		if f.untyped && f.boolean {
			out = &ast.BinaryExpr{X: out, Op: token.EQL, Y: parseExpr("0 == 0")}
		}
		return pre, out, nil
	}
	if !contains(x, l.questions) && (!f.untyped || f.boolean) {
		return finish(nil, x)
	}
	switch n := x.(type) {
	case *ast.ParenExpr:
		p, v, e := l.effectExpr(n.X, ft)
		return p, &ast.ParenExpr{X: v}, e
	case *ast.UnaryExpr:
		p, v, e := l.effectExpr(n.X, ft)
		if e != nil {
			return nil, nil, e
		}
		return finish(p, &ast.UnaryExpr{Op: n.Op, X: v})
	case *ast.BinaryExpr:
		p, left, e := l.effectExpr(n.X, ft)
		if e != nil {
			return nil, nil, e
		}
		q, right, e := l.effectExpr(n.Y, ft)
		if e != nil {
			return nil, nil, e
		}
		if n.Op == token.LAND || n.Op == token.LOR {
			// Nameable typed bools need their actual type even when the left side is
			// untyped. Otherwise := either retains the private left type or defaults
			// a genuinely untyped logical expression to temporary builtin bool.
			var stored ast.Expr
			if !f.untyped {
				stored = l.freezeValue(x, left, &p)
			} else {
				name := l.temp()
				p = append(p, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{left}})
				stored = ast.NewIdent(name)
			}
			cond := stored
			if n.Op == token.LOR {
				cond = &ast.UnaryExpr{Op: token.NOT, X: stored}
			}
			q = append(q, &ast.AssignStmt{Lhs: []ast.Expr{stored}, Tok: token.ASSIGN, Rhs: []ast.Expr{right}})
			p = append(p, &ast.IfStmt{Cond: cond, Body: &ast.BlockStmt{List: q}})
			if f.untyped {
				return p, &ast.BinaryExpr{X: stored, Op: token.EQL, Y: parseExpr("0 == 0")}, nil
			}
			return p, stored, nil
		}
		return finish(append(p, q...), &ast.BinaryExpr{X: left, Op: n.Op, Y: right})
	case *ast.CallExpr:
		if id, ok := n.Fun.(*ast.Ident); ok {
			if d, q := l.questions[id.Name]; q {
				p, operand, e := l.effectExpr(n.Args[0], ft)
				if e != nil {
					return nil, nil, e
				}
				flow, e := l.questionFlow(n, operand, ft, d)
				if e != nil {
					return nil, nil, e
				}
				return append(p, flow.emit(returnFailure)...), flow.value, nil
			}
		}
		call := *n
		call.Args = append([]ast.Expr(nil), n.Args...)
		var pre []ast.Stmt
		for i, a := range n.Args {
			p, v, e := l.effectExpr(a, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			call.Args[i] = v
		}
		return finish(pre, &call)
	}
	return nil, nil, l.fail(x, "internal: unplanned assignment effect")
}

func (l *lowering) deferredPlace(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	switch n := x.(type) {
	case *ast.Ident:
		return nil, x, nil
	case *ast.ParenExpr:
		p, v, e := l.deferredPlace(n.X, ft)
		return p, &ast.ParenExpr{X: v}, e
	case *ast.StarExpr:
		p, v, e := l.effectExpr(n.X, ft)
		return p, &ast.StarExpr{X: v}, e
	case *ast.SelectorExpr:
		if l.info.Selections[n] == nil {
			return nil, x, nil
		}
		var p []ast.Stmt
		var v ast.Expr
		var e error
		if _, pointer := underlyingType(l.info.TypeOf(n.X)).(*types.Pointer); pointer {
			p, v, e = l.effectExpr(n.X, ft)
		} else {
			p, v, e = l.deferredPlace(n.X, ft)
		}
		return p, &ast.SelectorExpr{X: v, Sel: n.Sel}, e
	case *ast.IndexExpr:
		var p []ast.Stmt
		var v ast.Expr
		var e error
		if _, array := underlyingType(l.info.TypeOf(n.X)).(*types.Array); array {
			p, v, e = l.deferredPlace(n.X, ft)
		} else {
			p, v, e = l.effectExpr(n.X, ft)
		}
		if e != nil {
			return nil, nil, e
		}
		q, index, e := l.effectExpr(n.Index, ft)
		return append(p, q...), &ast.IndexExpr{X: v, Index: index}, e
	}
	return nil, nil, l.fail(x, "internal: unplanned assignment destination")
}
func (l *lowering) plannedAssignment(s *ast.AssignStmt, ft *ast.FuncType) ([]ast.Stmt, error) {
	assignment := *s
	assignment.Lhs = append([]ast.Expr(nil), s.Lhs...)
	assignment.Rhs = append([]ast.Expr(nil), s.Rhs...)
	var pre []ast.Stmt
	for i, x := range s.Lhs {
		p, v, e := l.deferredPlace(x, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		assignment.Lhs[i] = v
	}
	for i, x := range s.Rhs {
		p, v, e := l.effectExpr(x, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		assignment.Rhs[i] = v
	}
	// Keep all final destinations in one assignment. The block prevents generated
	// temporaries from obstructing a source forward goto.
	return []ast.Stmt{&ast.BlockStmt{List: append(pre, &assignment)}}, nil
}

// Pointer/uintptr conversion chains (including special syscall arguments) have
// compiler-recognized lifetime rules. Splitting them into temporaries is not an
// ordinary value-preserving transformation; retain the existing frame path.
func (l *lowering) hasUnsafePointerConversion(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !l.info.Types[call.Fun].IsType() {
			return !found
		}
		typ := l.info.TypeOf(call.Fun)
		if typ == nil {
			return !found
		}
		b, ok := typ.Underlying().(*types.Basic)
		if !ok {
			return !found
		}
		if b.Kind() == types.UnsafePointer {
			found = true
		}
		if b.Kind() == types.Uintptr && len(call.Args) == 1 {
			if t := l.info.TypeOf(call.Args[0]); t != nil {
				if a, ok := t.Underlying().(*types.Basic); ok && a.Kind() == types.UnsafePointer {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
