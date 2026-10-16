package check

import (
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (c *checker) unsafeCall(call *ast.CallExpr) (types.Type, bool) {
	fun := call.Fun
	for {
		paren, ok := fun.(*ast.ParenExpr)
		if !ok {
			break
		}
		fun = paren.X
	}
	symbol := c.symbol(fun)
	for symbol != nil && symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	if symbol == nil || symbol.Kind != resolve.Builtin || len(symbol.Target) != 2 || symbol.Target[0] != "unsafe" {
		return nil, false
	}
	name := symbol.Target[1]
	for _, arg := range call.Args {
		if name == "Sizeof" || name == "Alignof" || name == "Offsetof" {
			ast.Walk(arg, func(node ast.Node) bool {
				if _, closure := node.(*ast.FuncLit); closure {
					return false
				}
				switch node.(type) {
				case *ast.TryExpr, *ast.MatchExpr:
					c.diag(node, "unsafe-operand", "unsafe compile-time operands cannot contain ? or match")
					return false
				}
				return true
			})
		}
		c.expr(arg, nil)
	}
	switch name {
	case "Sizeof", "Alignof", "Offsetof":
		return types.Typ[types.Uintptr], true
	case "Add":
		return types.Typ[types.UnsafePointer], true
	case "String":
		return types.Typ[types.String], true
	case "StringData":
		return types.NewPointer(types.Typ[types.Byte]), true
	case "Slice", "SliceData":
		if len(call.Args) > 0 {
			typ := c.out.Types[call.Args[0]]
			if valid(typ) {
				core := unsafeCoreType(typ, nil)
				if pointer, ok := core.(*types.Pointer); ok && name == "Slice" {
					return types.NewSlice(pointer.Elem()), true
				}
				if slice, ok := core.(*types.Slice); ok && name == "SliceData" {
					return types.NewPointer(slice.Elem()), true
				}
			}
		}
	}
	c.diag(call, "unsafe-call", "invalid call to "+strings.Join(symbol.Target, "."))
	return types.Typ[types.Invalid], true
}

// A parameter with a common pointer/slice underlying type keeps its element
// type in unsafe.Slice/SliceData. Native Go still validates the full type set.
func unsafeCoreType(t types.Type, visiting map[types.Type]bool) types.Type {
	if t == nil || visiting[t] {
		return nil
	}
	if visiting == nil {
		visiting = map[types.Type]bool{}
	}
	visiting[t] = true
	defer delete(visiting, t)
	switch underlying := t.Underlying().(type) {
	case *types.Interface:
		var core types.Type
		for i := 0; i < underlying.NumEmbeddeds(); i++ {
			bound := unsafeCoreType(underlying.EmbeddedType(i), visiting)
			if bound == nil {
				continue
			}
			if core != nil && !types.Identical(core, bound) {
				return nil
			}
			core = bound
		}
		return core
	case *types.Union:
		var core types.Type
		for i := 0; i < underlying.Len(); i++ {
			term := unsafeCoreType(underlying.Term(i).Type(), visiting)
			if term == nil || core != nil && !types.Identical(core, term) {
				return nil
			}
			core = term
		}
		return core
	default:
		return underlying
	}
}
