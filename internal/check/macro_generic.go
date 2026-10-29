package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Expression-shaped generic arguments are ambiguous during expansion. Once
// their parameter is known to be a type, only type captures may fill the slot.
func (c *checker) macroTypeArgument(node ast.Node) bool {
	if node == nil {
		return true
	}
	if capture, ok := c.out.Resolve.Captures[node]; ok {
		if capture.Kind == "type" {
			return true
		}
		c.diag(node, GenericConstraint, "type argument rejects captured types from "+capture.Kind+" fragments; use a type fragment")
		return false
	}
	switch n := node.(type) {
	case *ast.TypeExpr:
		return c.macroTypeArgument(n.Type)
	case *ast.ParenExpr:
		return c.macroTypeArgument(n.X)
	case *ast.StarExpr:
		return c.macroTypeArgument(n.X)
	case *ast.RefExpr:
		return c.macroTypeArgument(n.X)
	case *ast.PathExpr:
		return c.macroTypeArgument(n.Path)
	case *ast.PathType:
		return c.macroTypeArgument(n.Path)
	case *ast.ParenType:
		return c.macroTypeArgument(n.Type)
	case *ast.IndexExpr:
		return c.macroTypeArgument(n.X)
	case *ast.Path:
		for _, segment := range n.Segments {
			if !c.macroTypeArgument(segment.Name) {
				return false
			}
		}
	}
	return true
}

func (c *checker) genericExprType(argument ast.Expr) types.Type {
	if !c.macroTypeArgument(argument) {
		return types.Typ[types.Invalid]
	}
	return c.exprType(argument)
}

func (c *checker) genericArgType(argument *ast.GenericArg) types.Type {
	if !c.macroTypeArgument(argument) {
		return types.Typ[types.Invalid]
	}
	if argument.Value != nil {
		if c.macroTypeArgument(argument.Value) {
			c.diag(argument, GenericConstraint, "a type argument is required")
		}
		return types.Typ[types.Invalid]
	}
	if !c.macroTypeArgument(argument.Type) {
		return types.Typ[types.Invalid]
	}
	return c.typ(argument.Type)
}
