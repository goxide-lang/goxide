package lower

import (
	goast "go/ast"
	gotoken "go/token"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) equalityCall(node syntax.Expr) (goast.Expr, bool) {
	call, ok := node.(*syntax.CallExpr)
	if !ok || e.info.Equalities[call] == nil {
		return nil, false
	}
	t := e.info.Equalities[call]
	sel := call.Fun.(*syntax.SelectorExpr)
	args := e.ordered([]syntax.Expr{sel.X, call.Args[0]})
	if d := e.deriveFor(t); d != nil && deriveHas(d, "PartialEq") {
		return &goast.CallExpr{Fun: e.deriveHelper(d, "PartialEq"), Args: args}, true
	}
	left, right := e.fresh("Left"), e.fresh("Right")
	expr := &goast.BinaryExpr{X: left, Op: gotoken.EQL, Y: right}
	function := &goast.FuncLit{
		Type: &goast.FuncType{
			Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{left, right}, Type: e.goType(t)}}},
			Results: &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent("bool")}}},
		},
		Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{expr}}}},
	}
	return &goast.CallExpr{Fun: function, Args: args}, true
}
