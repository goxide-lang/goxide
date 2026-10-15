package lower

import (
	goast "go/ast"
	"go/types"
	"path/filepath"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (e *emitter) flowNativeMethod(call *syntax.CallExpr) (goast.Expr, bool) {
	fun := call.Fun
	for {
		paren, ok := fun.(*syntax.ParenExpr)
		if !ok {
			break
		}
		fun = paren.X
	}
	selector, ok := fun.(*syntax.SelectorExpr)
	if !ok || selector.Static || e.info.Types[selector.X] == nil {
		return nil, false
	}
	// The lookup package qualifies private names; source checking has already
	// validated that the selected method is visible from this file's package.
	pkg := types.NewPackage(filepath.ToSlash(filepath.Dir(e.name)), e.tree.Package.Name)
	object, _, _ := types.LookupFieldOrMethod(e.info.Types[selector.X], true, pkg, selector.Sel.Name)
	if _, method := object.(*types.Func); !method {
		return nil, false
	}
	old := e.replacements
	e.replacements = make(map[syntax.Expr]goast.Expr, len(old))
	for expr, value := range old {
		e.replacements[expr] = value
	}
	// Calls and receives in the receiver precede argument calls. The remaining
	// receiver evaluation stays at the call, preserving copies and panic timing.
	syntax.Walk(selector.X, func(node syntax.Node) bool {
		if _, closure := node.(*syntax.FuncLit); closure {
			return false
		}
		if _, typ := node.(syntax.Type); typ {
			return false
		}
		var operand syntax.Expr
		switch node := node.(type) {
		case *syntax.CallExpr:
			operand = node
		case *syntax.UnaryExpr:
			if node.Op == token.ARROW {
				operand = node
			}
		}
		if operand == nil {
			return true
		}
		e.replacements[operand] = e.spill(operand, e.expr(operand))
		return false
	})
	receiver := e.expr(selector.X)
	e.replacements = old
	args := e.exprs(call.Args)
	result := &goast.CallExpr{Fun: &goast.SelectorExpr{X: receiver, Sel: e.ident(selector.Sel)}, Args: args}
	if call.Ellipsis {
		result.Ellipsis = 1
	}
	return result, true
}
