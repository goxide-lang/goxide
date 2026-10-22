package check

import "go/types"

// Owner follows the ordered source head. Only native pointers are transparent;
// a reference is a distinct source constructor and never transfers ownership.
func (c *checker) implOwnerAllowed(impl *Impl) bool {
	if impl.Trait == nil || impl.Trait.Type.Obj().Pkg() == impl.Owner {
		return true
	}
	p := c.newPermissionChecker()
	head := []*permission{impl.targetPermission}
	if head[0] == nil {
		head[0] = p.fromGo(impl.Target)
	}
	for i, input := range impl.Inputs {
		var source *permission
		if i < len(impl.inputPermissions) {
			source = impl.inputPermissions[i]
		}
		if source == nil {
			source = p.fromGo(input)
		}
		head = append(head, source)
	}
	for _, source := range head {
		if named := ownerRoot(source); named != nil && named.Obj().Pkg() == impl.Owner {
			return true
		}
		if parameter := uncoveredParameter(source); parameter != nil {
			for _, declared := range impl.TypeParams {
				if parameter == declared {
					return false
				}
			}
		}
	}
	return false
}

func ownerRoot(source *permission) *types.Named {
	for source != nil {
		if named, ok := types.Unalias(source.typ).(*types.Named); ok {
			return named.Origin()
		}
		if source.ref != ValueRef {
			return nil
		}
		switch typ := types.Unalias(source.typ).(type) {
		case *types.Pointer:
			source = source.elem
			if source == nil {
				source = &permission{typ: typ.Elem()}
			}
		default:
			return nil
		}
	}
	return nil
}

func uncoveredParameter(source *permission) *types.TypeParam {
	for source != nil && source.ref == ValueRef {
		switch typ := types.Unalias(source.typ).(type) {
		case *types.TypeParam:
			return typ
		case *types.Pointer:
			source = source.elem
			if source == nil {
				source = &permission{typ: typ.Elem()}
			}
		default:
			return nil
		}
	}
	return nil
}
