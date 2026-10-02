package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
)

// Keep the native for initializer: Go's implicit per-iteration variable copy
// must happen before post, including when post is moved to the next body entry.
func (l *lowering) forStatement(n *ast.ForStmt, label *ast.Ident, ft *ast.FuncType) ([]ast.Stmt, error) {
	l.guardFreeDepth++
	defer func() { l.guardFreeDepth-- }()
	var outer []ast.Stmt
	if n.Init != nil && contains(n.Init, l.questions) {
		init, e := l.statement(n.Init, ft)
		if e != nil {
			return nil, e
		}
		if a, ok := n.Init.(*ast.AssignStmt); ok && a.Tok == token.DEFINE {
			// Ordinary short declarations end in the original declaration.
			outer = append(outer, init[:len(init)-1]...)
			n.Init = init[len(init)-1]
		} else {
			outer = append(outer, init...)
			n.Init = nil
		}
	}
	var head []ast.Stmt
	if n.Post != nil && contains(n.Post, l.questions) {
		if a, ok := n.Post.(*ast.AssignStmt); ok && a.Tok == token.DEFINE {
			return nil, l.fail(n.Post, "for post cannot declare variables")
		}
		post, e := l.statement(n.Post, ft)
		if e != nil {
			return nil, e
		}
		first := l.temp()
		outer = append(outer, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(first)}, Tok: token.DEFINE, Rhs: []ast.Expr{parseExpr("0 == 0")}})
		head = append(head, &ast.IfStmt{Cond: ast.NewIdent(first), Body: &ast.BlockStmt{List: []ast.Stmt{&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(first)}, Tok: token.ASSIGN, Rhs: []ast.Expr{parseExpr("0 != 0")}}}}, Else: &ast.BlockStmt{List: post}})
		n.Post = nil
	}
	if n.Cond != nil && contains(n.Cond, l.questions) {
		pre, value, e := l.expr(n.Cond, ft)
		if e != nil {
			return nil, e
		}
		head = append(head, pre...)
		head = append(head, &ast.IfStmt{Cond: &ast.UnaryExpr{Op: token.NOT, X: value}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.BREAK}}}})
		n.Cond = nil
	} else if len(head) > 0 && n.Cond != nil {
		// A native condition would run before the moved post, which is incorrect.
		head = append(head, &ast.IfStmt{Cond: &ast.UnaryExpr{Op: token.NOT, X: n.Cond}, Body: &ast.BlockStmt{List: []ast.Stmt{&ast.BranchStmt{Tok: token.BREAK}}}})
		n.Cond = nil
	}
	loop, outerLabel := l.loopLabels(n, label, ft)
	if e := l.body(n.Body, ft); e != nil {
		return nil, e
	}
	if len(head) > 0 {
		n.Body = &ast.BlockStmt{List: append(head, n.Body)}
	}
	return wrapLoop(outer, loop, outerLabel), nil
}

func (l *lowering) rangeStatement(n *ast.RangeStmt, label *ast.Ident, ft *ast.FuncType) ([]ast.Stmt, error) {
	l.guardFreeDepth++
	defer func() { l.guardFreeDepth-- }()
	if (n.Key != nil && contains(n.Key, l.questions)) || (n.Value != nil && contains(n.Value, l.questions)) {
		return nil, l.fail(n, "? in range assignment targets is not supported yet")
	}
	var pre []ast.Stmt
	if contains(n.X, l.questions) {

		integer := false
		switch t := underlyingType(l.info.TypeOf(n.X)).(type) {
		case *types.Slice, *types.Map, *types.Chan, *types.Array:
		case *types.Pointer:
			if _, ok := t.Elem().Underlying().(*types.Array); !ok {
				return nil, l.fail(n.X, "unsupported pointer range source")
			}
		case *types.Basic:
			integer = t.Info()&types.IsInteger != 0
			if !integer && t.Info()&types.IsString == 0 {
				return nil, l.fail(n.X, "unsupported range source type")
			}
		default:
			return nil, l.fail(n.X, "? in function iterator range sources is not supported yet")
		}
		p, v, e := l.controlExpr(n.X, ft)
		if e != nil {
			return nil, e
		}
		pre = p
		if integer {
			// Keep untyped residuals in the native range context: an existing
			// iteration variable may supply a private integer type. Effects
			// have already saved operands and performed any shift checks.
			n.X = v
		} else {
			// Save the complete array value, not merely a propagated pointer:
			// range *pointerResult? must not become an unevaluated dereference.
			n.X = l.freezeValue(n.X, v, &pre)
		}

	}
	loop, outerLabel := l.loopLabels(n, label, ft)
	if e := l.body(n.Body, ft); e != nil {
		return nil, e
	}
	return wrapLoop(pre, loop, outerLabel), nil
}

// A source goto to the loop label must rerun its initializer/source prelude.
// Keep that label on the outer block, and retarget only break/continue to a
// fresh native loop label. If it had only break/continue users, omit the now
// unused outer label. An originally unused label stays unused and is rejected.
func (l *lowering) loopLabels(loop ast.Stmt, label *ast.Ident, ft *ast.FuncType) (ast.Stmt, *ast.Ident) {
	if label == nil {
		return loop, nil
	}
	internal := l.temp()
	branches := 0
	hasGoto := false
	ast.Inspect(l.functionBodies[ft], func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		b, ok := node.(*ast.BranchStmt)
		if !ok || b.Label == nil || b.Label.Name != label.Name {
			return true
		}
		if b.Tok == token.GOTO {
			hasGoto = true
		}
		return true
	})
	ast.Inspect(loop, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		b, ok := node.(*ast.BranchStmt)
		if ok && b.Label != nil && b.Label.Name == label.Name && (b.Tok == token.BREAK || b.Tok == token.CONTINUE) {
			b.Label = ast.NewIdent(internal)
			branches++
		}
		return true
	})
	if branches > 0 {
		loop = &ast.LabeledStmt{Label: ast.NewIdent(internal), Stmt: loop}
		if !hasGoto {
			label = nil
		}
	}
	return loop, label
}
func wrapLoop(pre []ast.Stmt, loop ast.Stmt, label *ast.Ident) []ast.Stmt {
	var s ast.Stmt = &ast.BlockStmt{List: append(pre, loop)}
	if label != nil {
		s = &ast.LabeledStmt{Label: label, Stmt: s}
	}
	return []ast.Stmt{s}
}

// Rewriting a used loop label must not erase an original duplicate definition.
// Nested function literals have independent label scopes and are checked by body.
func (l *lowering) validateLoopLabels(body *ast.BlockStmt) error {
	names := map[string]bool{}
	var failure error
	ast.Inspect(body, func(n ast.Node) bool {
		if failure != nil {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if branch, ok := n.(*ast.BranchStmt); ok && branch.Label != nil && branch.Label.Name == "_" {
			failure = l.fail(branch, "blank label cannot be a branch target")
			return false
		}
		if label, ok := n.(*ast.LabeledStmt); ok && label.Label.Name != "_" {
			name := label.Label.Name
			if names[name] {
				failure = l.fail(label, "duplicate label "+name)
				return false
			}
			names[name] = true
		}
		return true
	})
	return failure
}
