package check

import (
	"go/types"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (c *checker) equalityCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "equals" || len(call.Args) != 1 || call.Ellipsis {
		return false
	}
	t := c.expr(selector.X, nil)
	if !valid(t) {
		return false
	}
	if member, _, _ := types.LookupFieldOrMethod(t, true, c.pkg, "equals"); member != nil {
		return false
	}
	t = types.Unalias(defaultType(t))
	eligible := false
	if basic, ok := t.(*types.Basic); ok {
		eligible = basic.Info()&(types.IsBoolean|types.IsInteger|types.IsFloat|types.IsComplex|types.IsString) != 0
	} else if named, ok := t.(*types.Named); ok {
		if d := c.out.Derives[named]; d != nil {
			_, manual := d.Manual["PartialEq"]
			eligible = d.Requests["PartialEq"] || manual
		}
	}
	if !eligible {
		c.diag(call, DeriveInvalid, "equals requires a primitive or an explicitly registered PartialEq type")
		return true
	}
	c.expr(call.Args[0], t)
	c.out.Equalities[call] = t
	return true
}
