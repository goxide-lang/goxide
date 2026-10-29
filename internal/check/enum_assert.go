package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// The std macro requests a typed walk; it never receives the type layout.
func (c *checker) enumAssert(call *ast.CallExpr) (types.Type, bool) {
	symbol := c.symbol(referenceScrutinee(call.Fun))
	if symbol == nil || symbol.Kind != resolve.Builtin || symbol.Name.Name != "__goxide_enum_assert" || symbol.Decl.File != "" || len(symbol.Target) != 0 {
		return nil, false
	}
	for _, argument := range call.Args {
		c.expr(argument, nil)
	}
	if len(call.Args) != 1 || call.Ellipsis {
		c.diag(call, "enum-assert-call", "enum_assert requires exactly one enum value without ...")
		return types.Typ[types.Invalid], true
	}
	typ := c.out.Types[call.Args[0]]
	if c.enumOf(typ) == nil {
		c.diag(call.Args[0], "enum-assert-type", "enum_assert requires a known enum value")
		return types.Typ[types.Invalid], true
	}
	if c.out.EnumAssertions == nil {
		c.out.EnumAssertions = map[*ast.CallExpr]types.Type{}
	}
	c.out.EnumAssertions[call] = typ
	return types.NewTuple(), true
}
