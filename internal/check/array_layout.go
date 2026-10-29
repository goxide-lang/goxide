package check

import (
	"go/constant"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// Only canonical built-ins enter ordinary array constant evaluation. A local
// function named len or Sizeof remains a runtime call.
func (c *checker) arrayBuiltinConstant(call *ast.CallExpr) constant.Value {
	unknown := constant.MakeUnknown()
	if len(call.Args) != 1 || call.Ellipsis || c.isTypeExpr(call.Args[0]) {
		return unknown
	}
	symbol := c.symbol(referenceScrutinee(call.Fun))
	if symbol == nil || symbol.Kind != resolve.Builtin {
		return unknown
	}
	if len(symbol.Target) == 2 && symbol.Target[0] == "unsafe" {
		return c.arrayLayoutConstant(call, symbol.Target[1])
	}
	if len(symbol.Target) != 0 || symbol.Name.Name != "len" && symbol.Name.Name != "cap" {
		return unknown
	}
	if symbol.Name.Name == "len" {
		if value := c.arrayStringConstant(call.Args[0]); value.Kind() == constant.String {
			return constant.MakeInt64(int64(len(constant.StringVal(value))))
		}
	}
	typ := c.expr(call.Args[0], nil)
	if !valid(typ) {
		return unknown
	}
	typ = typ.Underlying()
	if pointer, ok := typ.(*types.Pointer); ok {
		typ = pointer.Elem().Underlying()
	}
	array, ok := typ.(*types.Array)
	if !ok || c.arrayOperandHasCallOrReceive(call.Args[0]) {
		return unknown
	}
	return constant.MakeInt64(array.Len())
}

// A length of a fixed array is constant only if its operand has no runtime
// calls or channel receives. Conversion syntax does not itself count as a call.
func (c *checker) arrayOperandHasCallOrReceive(expression ast.Expr) bool {
	found := false
	ast.Walk(expression, func(node ast.Node) bool {
		if found {
			return false
		}
		switch node := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			found = node.Op == token.ARROW
		case *ast.CallExpr:
			if !c.isTypeExpr(node.Fun) && c.arrayBuiltinConstant(node).Kind() == constant.Unknown {
				found = true
			}
		}
		return !found
	})
	return found
}

func (c *checker) arrayStringConstant(node ast.Expr) constant.Value {
	if value := c.constant(node); value.Kind() == constant.String {
		return value
	}
	if object := c.importedConstantObject(node); object != nil && object.Val().Kind() == constant.String {
		return object.Val()
	}
	if symbol := c.symbol(node); symbol != nil && symbol.Kind == resolve.Constant {
		if source, ok := c.sourceValues[symbol]; ok {
			if c.constantStack == nil {
				c.constantStack = map[*resolve.Symbol]bool{}
			}
			if c.constantStack[symbol] {
				return constant.MakeUnknown()
			}
			c.constantStack[symbol] = true
			previous := c.file
			c.file = source.file
			defer func() { c.file = previous; delete(c.constantStack, symbol) }()
			return c.arrayStringConstant(source.node)
		}
	}
	switch node := node.(type) {
	case *ast.ParenExpr:
		return c.arrayStringConstant(node.X)
	case *ast.BinaryExpr:
		if node.Op == token.ADD {
			left, right := c.arrayStringConstant(node.X), c.arrayStringConstant(node.Y)
			if left.Kind() == constant.String && right.Kind() == constant.String {
				return constant.BinaryOp(left, gotoken.ADD, right)
			}
		}
	}
	return constant.MakeUnknown()
}

// Sizeof and Alignof are ordinary Go constants when their operand has a fixed
// layout. The operand is checked for its type; neither it nor its callees run.
func (c *checker) arrayLayoutConstant(call *ast.CallExpr, name string) constant.Value {
	unknown := constant.MakeUnknown()
	if name != "Sizeof" && name != "Alignof" {
		return unknown
	}
	c.unsafeCall(call)
	typ := defaultType(c.out.Types[call.Args[0]])
	if !FixedLayout(typ) {
		return unknown
	}
	var size int64
	if name == "Sizeof" {
		size = c.out.GoSizes.Sizeof(typ)
	} else {
		size = c.out.GoSizes.Alignof(typ)
	}
	if size < 0 {
		return unknown
	}
	return constant.MakeInt64(size)
}

// FixedLayout reports whether a type has a compile-time Go size and alignment.
// Pointer, slice, map, channel, function and interface headers have a fixed
// layout even when they refer to type parameters. Arrays and structs do not.
func FixedLayout(typ types.Type) bool {
	if !valid(typ) {
		return false
	}
	typ = types.Unalias(typ)
	if _, parameter := typ.(*types.TypeParam); parameter {
		return false
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Array:
		return FixedLayout(underlying.Elem())
	case *types.Struct:
		for index := 0; index < underlying.NumFields(); index++ {
			if !FixedLayout(underlying.Field(index).Type()) {
				return false
			}
		}
	case *types.Basic:
		return underlying.Kind() != types.UntypedNil
	}
	return true
}
