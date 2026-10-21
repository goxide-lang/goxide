package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (c *checker) enumValidBuiltin(node ast.Expr) bool {
	symbol := c.symbol(referenceScrutinee(node))
	return symbol != nil && symbol.Kind == resolve.Builtin && symbol.Name.Name == "enum_valid" && symbol.Decl.File == "" && len(symbol.Target) == 0
}

func (c *checker) enumValid(call *ast.CallExpr) (types.Type, bool) {
	if !c.enumValidBuiltin(call.Fun) {
		return nil, false
	}
	for _, argument := range call.Args {
		c.expr(argument, nil)
	}
	if len(call.Args) != 1 || call.Ellipsis {
		c.diag(call, "enum-valid-call", "enum_valid requires exactly one enum value without ...")
		return types.Typ[types.Invalid], true
	}
	instance := c.out.EnumOf(c.out.Types[call.Args[0]])
	if instance == nil {
		c.diag(call.Args[0], "enum-valid-type", "enum_valid requires a known enum value")
		return types.Typ[types.Invalid], true
	}
	if c.out.EnumValid == nil {
		c.out.EnumValid = map[*ast.CallExpr]*Instance{}
	}
	c.out.EnumValid[call] = instance
	return types.Typ[types.Bool], true
}
