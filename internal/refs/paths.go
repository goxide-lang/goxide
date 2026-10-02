package refs

import (
	"go/ast"
	"go/types"
)

// localValuePath names an independently stored local slot. Every selection must
// traverse a value struct: implicit/explicit pointers and container indexes are
// shared storage, not field paths that can be strongly updated on this binding.
func (c *checker) localValuePath(e ast.Expr) (types.Object, []string, bool) {
	switch e := unparen(e).(type) {
	case *ast.Ident:
		obj := c.binding(e)
		_, variable := obj.(*types.Var)
		if !variable || e.Name == "_" || c.escapingSlot(obj) {
			return nil, nil, false
		}
		return obj, nil, true
	case *ast.SelectorExpr:
		obj, path, ok := c.localValuePath(e.X)
		if !ok {
			return nil, nil, false
		}
		sel := c.info.Selections[e]
		if sel == nil || sel.Kind() != types.FieldVal {
			return nil, nil, false
		}
		typ := c.typ(e.X)
		for _, index := range sel.Index() {
			st, ok := typ.Underlying().(*types.Struct)
			if !ok {
				return nil, nil, false
			}
			field := st.Field(index)
			path = append(path, field.Name())
			typ = field.Type()
		}
		return obj, path, true
	}
	return nil, nil, false
}

// withFieldPath is persistent: point snapshots and copies of the old value may
// share qualification nodes and must not change when another binding is updated.
func withFieldPath(old *qualification, path []string, value *qualification) *qualification {
	if len(path) == 0 {
		return value
	}
	if old != nil && (old.read || old.deep) {
		return old
	}
	out := &qualification{fields: map[string]*qualification{}}
	if old != nil {
		out.elem = old.elem
		for name, q := range old.fields {
			out.fields[name] = q
		}
	}
	next := withFieldPath(out.fields[path[0]], path[1:], value)
	if restricted(next) {
		out.fields[path[0]] = next
	} else {
		delete(out.fields, path[0])
	}
	if !restricted(out) {
		return nil
	}
	return out
}

func (c *checker) tracksLocalField(e ast.Expr) bool {
	obj, path, ok := c.localValuePath(e)
	return ok && len(path) > 0 && !c.conservativeObjects[obj] && !isInterface(c.typ(e))
}

// A pointer receiver selected from a value implicitly takes its address, even
// when only a method value is saved. Follow promotion to distinguish a value
// embedded field from an already-pointer receiver; no call effects are inferred.
func (c *checker) implicitReceiverAddress(e *ast.SelectorExpr) bool {
	sel := c.info.Selections[e]
	if sel == nil || sel.Kind() != types.MethodVal {
		return false
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if _, pointer := sig.Recv().Type().Underlying().(*types.Pointer); !pointer {
		return false
	}
	typ := c.typ(e.X)
	for _, index := range sel.Index()[:len(sel.Index())-1] {
		st, ok := typ.Underlying().(*types.Struct)
		if !ok {
			return false
		}
		typ = st.Field(index).Type()
	}
	_, pointer := typ.Underlying().(*types.Pointer)
	return !pointer
}
