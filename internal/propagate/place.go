package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
)

// place stages only operands of a destination. Pure struct/array paths remain
// lvalues: taking their address now would move a nil/bounds panic from the
// write phase into the operand phase. Crossing a pointer/slice/map boundary,
// however, reads and saves that container now, before subsequent operands.
func (l *lowering) place(original ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, func() ast.Expr, error) {
	var pre []ast.Stmt
	var zero func() ast.Expr
	stage := func(x ast.Expr) (ast.Expr, error) {
		p, value, err := l.expr(x, ft)
		pre = append(pre, p...)
		if err != nil {
			return nil, err
		}
		return l.freezeValue(x, value, &pre), nil
	}
	child := func(x ast.Expr) (ast.Expr, error) {
		p, target, witness, err := l.place(x, ft)
		pre = append(pre, p...)
		zero = witness
		return target, err
	}
	switch x := original.(type) {
	case *ast.Ident:
		// The address of a variable is stable and does not read its contents.
		if x.Name != "_" {
			zero = func() ast.Expr {
				return l.witnessCall("pointer", &ast.UnaryExpr{Op: token.AND, X: ast.NewIdent(x.Name)})
			}
		}
		return nil, x, zero, nil
	case *ast.ParenExpr:
		v, err := child(x.X)
		return pre, &ast.ParenExpr{X: v}, zero, err
	case *ast.StarExpr:
		v, err := stage(x.X)
		zero = func() ast.Expr { return l.witnessCall("pointer", v) }
		return pre, &ast.StarExpr{X: v}, zero, err
	case *ast.IndexExpr:
		var base ast.Expr
		var err error
		kind := underlyingType(l.info.TypeOf(x.X))
		if _, array := kind.(*types.Array); array {
			if !l.info.Types[x.X].Addressable() {
				return nil, nil, nil, l.fail(x, "cannot assign to an unaddressable array element")
			}
			base, err = child(x.X)
		} else {
			base, err = stage(x.X)
			zero = func() ast.Expr { return base }
		}
		if err != nil {
			return nil, nil, nil, err
		}
		parent := zero
		switch t := kind.(type) {
		case *types.Array:
			if parent != nil {
				zero = func() ast.Expr { return l.witnessCall("array-"+strconv.FormatInt(t.Len(), 10), parent()) }
			}
		case *types.Pointer:
			a, ok := t.Elem().Underlying().(*types.Array)
			if !ok {
				return nil, nil, nil, l.fail(x, "assignment index requires an array, slice, or map")
			}
			zero = func() ast.Expr {
				return l.witnessCall("array-"+strconv.FormatInt(a.Len(), 10), l.witnessCall("pointer", base))
			}
		case *types.Slice:
			zero = func() ast.Expr { return l.witnessCall("slice", base) }
		case *types.Map:
			if types.Identical(l.info.TypeOf(x.Index), t.Key()) {
				l.registerWitness(x.Index, func() ast.Expr { return l.witnessCall("map-key", base) })
			}
			zero = func() ast.Expr { return l.witnessCall("map-value", base) }
		default:
			return nil, nil, nil, l.fail(x, "assignment index requires an array, slice, or map")
		}
		index, err := stage(x.Index)
		return pre, &ast.IndexExpr{X: base, Index: index}, zero, err
	case *ast.SelectorExpr:
		if l.placeFields[x] != nil {
			return l.fieldPlace(x.X, x.Sel, ft)
		}
		selection := l.info.Selections[x]
		if selection == nil {
			if !l.info.Types[x].Addressable() {
				return nil, nil, nil, l.fail(x, "assignment requires an addressable field or variable")
			}
			zero = func() ast.Expr { return l.witnessCall("pointer", &ast.UnaryExpr{Op: token.AND, X: x}) }
			return nil, x, zero, nil
		}
		if selection.Kind() != types.FieldVal || !l.info.Types[x].Addressable() {
			return nil, nil, nil, l.fail(x, "assignment requires an addressable field")
		}
		// Explicitly expose embedded pointer boundaries to the same recursive
		// logic as spelled-out selectors. Synthetic nodes retain original types.
		base := x.X
		path := selection.Index()
		lastPointer := -1
		t := underlyingType(l.info.TypeOf(base))
		for i, index := range path[:len(path)-1] {
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem().Underlying()
			}
			field := t.(*types.Struct).Field(index)
			t = field.Type().Underlying()
			if _, pointer := t.(*types.Pointer); pointer {
				lastPointer = i
			}
		}
		// A suffix containing only structs needs no expansion. This also
		// preserves legal promotion through imported private struct fields.
		for _, index := range path[:lastPointer+1] {
			t := underlyingType(l.info.TypeOf(base))
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem().Underlying()
			}
			field := t.(*types.Struct).Field(index)
			if !field.Exported() && field.Pkg() != l.pkg {
				return nil, nil, nil, l.fail(x, "? assignment through a private imported embedded pointer path is not yet lowered")
			}
			next := &ast.SelectorExpr{X: base, Sel: ast.NewIdent(field.Name())}
			tv := l.info.Types[base]
			tv.Type = field.Type()
			l.info.Types[next] = tv
			if l.placeFields == nil {
				l.placeFields = map[*ast.SelectorExpr]*types.Var{}
			}
			l.placeFields[next] = field
			base = next
		}
		return l.fieldPlace(base, x.Sel, ft)
	default:
		return nil, nil, nil, l.fail(original, "assignment requires an addressable destination or map index")
	}
}

func (l *lowering) fieldPlace(base ast.Expr, field *ast.Ident, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, func() ast.Expr, error) {
	var pre []ast.Stmt
	var value ast.Expr
	var zero func() ast.Expr
	var err error
	if _, pointer := underlyingType(l.info.TypeOf(base)).(*types.Pointer); pointer {
		pre, value, err = l.expr(base, ft)
		if err == nil {
			value = l.freezeValue(base, value, &pre)
		}
		zero = func() ast.Expr { return l.witnessCall("pointer", value) }
	} else {
		pre, value, zero, err = l.place(base, ft)
	}
	parent := zero
	if parent != nil {
		zero = func() ast.Expr { return &ast.SelectorExpr{X: parent(), Sel: ast.NewIdent(field.Name)} }
	}
	return pre, &ast.SelectorExpr{X: value, Sel: field}, zero, err
}

func (l *lowering) registerWitness(original ast.Expr, context func() ast.Expr) {
	if context == nil {
		return
	}
	expected := l.info.TypeOf(original)
	if l.witnesses == nil {
		l.witnesses = map[ast.Expr]func() ast.Expr{}
	}
	ast.Inspect(original, func(node ast.Node) bool {
		if _, closure := node.(*ast.FuncLit); closure {
			return false
		}
		if expr, ok := node.(ast.Expr); ok && expected != nil && types.Identical(l.info.TypeOf(expr), expected) {
			l.witnesses[expr] = context
		}
		return true
	})
}
