package check

import (
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
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
				core := coreType(typ, nil)
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

// A shared underlying type preserves operation results for Go type parameters.
// Native Go validates the complete type set and operation constraints.
func coreType(t types.Type, visiting map[types.Type]bool) types.Type {
	return commonType(t, visiting, nil)
}

// Indexing can share an element type even when its container types differ.
func commonType(t types.Type, visiting map[types.Type]bool, project func(types.Type) types.Type) types.Type {
	if t == nil || visiting[t] {
		return nil
	}
	if visiting == nil {
		visiting = map[types.Type]bool{}
	}
	visiting[t] = true
	defer delete(visiting, t)
	merge := func(first, next types.Type) types.Type {
		if first == nil || types.Identical(first, next) {
			return next
		}
		if project == nil {
			a, aok := first.(*types.Chan)
			b, bok := next.(*types.Chan)
			if aok && bok && types.Identical(a.Elem(), b.Elem()) {
				if a.Dir() == types.SendRecv {
					return b
				}
				if b.Dir() == types.SendRecv {
					return a
				}
			}
		}
		return nil
	}
	switch underlying := t.Underlying().(type) {
	case *types.Interface:
		var core types.Type
		for i := 0; i < underlying.NumEmbeddeds(); i++ {
			bound := commonType(underlying.EmbeddedType(i), visiting, project)
			if bound == nil {
				continue
			}
			core = merge(core, bound)
			if core == nil {
				return nil
			}
		}
		return core
	case *types.Union:
		var core types.Type
		for i := 0; i < underlying.Len(); i++ {
			term := commonType(underlying.Term(i).Type(), visiting, project)
			if term == nil {
				return nil
			}
			core = merge(core, term)
			if core == nil {
				return nil
			}
		}
		return core
	default:
		if project != nil {
			return project(underlying)
		}
		return underlying
	}
}
