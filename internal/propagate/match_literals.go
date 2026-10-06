package propagate

import (
	"fmt"
	"go/ast"
	"go/types"
)

// matchLiteralType follows the real enclosing literal and element role. It does
// not evaluate a witness or transplant a type spelling across lexical scopes.
func (l *lowering) matchLiteralType(slot *ast.ValueSpec, parents map[ast.Node]ast.Node) (types.Type, error) {
	var node ast.Node = slot
	key := false
	for node != nil {
		parent := parents[node]
		if fn, ok := parent.(*ast.FuncLit); ok && matchAtom(fn) == nil {
			break
		}
		if pair, ok := parent.(*ast.KeyValueExpr); ok {
			key = node == pair.Key
		}
		if literal, ok := parent.(*ast.CompositeLit); ok {
			typ := l.info.TypeOf(literal)
			if typ == nil && literal.Type != nil {
				typ = l.info.TypeOf(literal.Type)
			}
			if typ == nil || l.pendingMatchType(typ) {
				return nil, fmt.Errorf("composite match requires a resolved literal type")
			}
			underlying := typ.Underlying()
			if pointer, ok := underlying.(*types.Pointer); ok && literal.Type == nil {
				underlying = pointer.Elem().Underlying()
			}
			switch t := underlying.(type) {
			case *types.Map:
				if key {
					return t.Key(), nil
				}
				return t.Elem(), nil
			case *types.Array:
				if key {
					return nil, fmt.Errorf("match cannot be a constant array literal index")
				}
				return t.Elem(), nil
			case *types.Slice:
				if key {
					return nil, fmt.Errorf("match cannot be a constant slice literal index")
				}
				return t.Elem(), nil
			case *types.Struct:
				if key {
					return nil, fmt.Errorf("match cannot be a struct field selector")
				}
				if pair, ok := node.(*ast.KeyValueExpr); ok {
					field, ok := pair.Key.(*ast.Ident)
					if !ok {
						return nil, fmt.Errorf("struct literal requires a field name")
					}
					for i := 0; i < t.NumFields(); i++ {
						if t.Field(i).Name() == field.Name {
							return t.Field(i).Type(), nil
						}
					}
					return nil, fmt.Errorf("unknown struct literal field %s", field.Name)
				}
				for i, elt := range literal.Elts {
					if elt == node && i < t.NumFields() {
						return t.Field(i).Type(), nil
					}
				}
				return nil, fmt.Errorf("invalid positional struct literal element")
			}
			return nil, fmt.Errorf("match requires a struct, slice, array or map literal")
		}
		node = parent
	}
	return nil, fmt.Errorf("match literal element has no enclosing composite")
}
