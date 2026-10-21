package check

import "go/types"

// matchImplTarget binds only the impl's parameters; target parameters stay rigid.
func matchImplTarget(pattern, target types.Type, parameters []*types.TypeParam) (map[*types.TypeParam]types.Type, bool) {
	variables := make(map[*types.TypeParam]bool, len(parameters))
	for _, parameter := range parameters {
		variables[parameter] = true
	}
	bindings := make(map[*types.TypeParam]types.Type, len(parameters))
	var match func(types.Type, types.Type) bool
	match = func(pattern, target types.Type) bool {
		pattern, target = types.Unalias(pattern), types.Unalias(target)
		if parameter, ok := pattern.(*types.TypeParam); ok && variables[parameter] {
			if previous, bound := bindings[parameter]; bound {
				return types.Identical(previous, target)
			}
			bindings[parameter] = target
			return true
		}
		return sameImplTargetShape(pattern, target, match)
	}
	if !match(pattern, target) || len(bindings) != len(variables) {
		return nil, false
	}
	return bindings, true
}

// Impl overlap depends on target shapes, never on their where predicates.
func implTargetsOverlap(left, right types.Type, leftParams, rightParams []*types.TypeParam) bool {
	variables := make(map[*types.TypeParam]bool, len(leftParams)+len(rightParams))
	for _, parameters := range [][]*types.TypeParam{leftParams, rightParams} {
		for _, parameter := range parameters {
			variables[parameter] = true
		}
	}
	bindings := make(map[*types.TypeParam]types.Type)
	resolve := func(typ types.Type) types.Type {
		for {
			typ = types.Unalias(typ)
			parameter, ok := typ.(*types.TypeParam)
			if !ok || bindings[parameter] == nil {
				return typ
			}
			typ = bindings[parameter]
		}
	}
	var occurs func(*types.TypeParam, types.Type) bool
	occurs = func(parameter *types.TypeParam, typ types.Type) bool {
		typ = resolve(typ)
		if typ == parameter {
			return true
		}
		switch typ := typ.(type) {
		case *types.Named:
			for i := 0; i < typ.TypeArgs().Len(); i++ {
				if occurs(parameter, typ.TypeArgs().At(i)) {
					return true
				}
			}
		case *types.Pointer:
			return occurs(parameter, typ.Elem())
		case *types.Slice:
			return occurs(parameter, typ.Elem())
		case *types.Array:
			return occurs(parameter, typ.Elem())
		case *types.Map:
			return occurs(parameter, typ.Key()) || occurs(parameter, typ.Elem())
		case *types.Chan:
			return occurs(parameter, typ.Elem())
		case *types.Struct:
			for i := 0; i < typ.NumFields(); i++ {
				if occurs(parameter, typ.Field(i).Type()) {
					return true
				}
			}
		case *types.Tuple:
			for i := 0; i < typ.Len(); i++ {
				if occurs(parameter, typ.At(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			return occurs(parameter, typ.Params()) || occurs(parameter, typ.Results())
		case *types.Interface:
			for i := 0; i < typ.NumExplicitMethods(); i++ {
				if occurs(parameter, typ.ExplicitMethod(i).Type()) {
					return true
				}
			}
			for i := 0; i < typ.NumEmbeddeds(); i++ {
				if occurs(parameter, typ.EmbeddedType(i)) {
					return true
				}
			}
		case *types.Union:
			for i := 0; i < typ.Len(); i++ {
				if occurs(parameter, typ.Term(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	var unify func(types.Type, types.Type) bool
	unify = func(left, right types.Type) bool {
		left, right = resolve(left), resolve(right)
		if types.Identical(left, right) {
			return true
		}
		if parameter, ok := left.(*types.TypeParam); ok && variables[parameter] {
			if occurs(parameter, right) {
				return false
			}
			bindings[parameter] = right
			return true
		}
		if parameter, ok := right.(*types.TypeParam); ok && variables[parameter] {
			return unify(right, left)
		}
		return sameImplTargetShape(left, right, unify)
	}
	return unify(left, right)
}

func sameImplTargetShape(left, right types.Type, compare func(types.Type, types.Type) bool) bool {
	switch left := left.(type) {
	case *types.Named:
		right, ok := right.(*types.Named)
		if !ok || left.Origin() != right.Origin() || left.TypeArgs().Len() != right.TypeArgs().Len() {
			return false
		}
		for i := 0; i < left.TypeArgs().Len(); i++ {
			if !compare(left.TypeArgs().At(i), right.TypeArgs().At(i)) {
				return false
			}
		}
		return true
	case *types.Pointer:
		right, ok := right.(*types.Pointer)
		return ok && compare(left.Elem(), right.Elem())
	case *types.Slice:
		right, ok := right.(*types.Slice)
		return ok && compare(left.Elem(), right.Elem())
	case *types.Array:
		right, ok := right.(*types.Array)
		return ok && left.Len() == right.Len() && compare(left.Elem(), right.Elem())
	case *types.Map:
		right, ok := right.(*types.Map)
		return ok && compare(left.Key(), right.Key()) && compare(left.Elem(), right.Elem())
	case *types.Chan:
		right, ok := right.(*types.Chan)
		return ok && left.Dir() == right.Dir() && compare(left.Elem(), right.Elem())
	case *types.Struct:
		right, ok := right.(*types.Struct)
		if !ok || left.NumFields() != right.NumFields() {
			return false
		}
		for i := 0; i < left.NumFields(); i++ {
			a, b := left.Field(i), right.Field(i)
			if a.Id() != b.Id() || a.Embedded() != b.Embedded() || left.Tag(i) != right.Tag(i) || !compare(a.Type(), b.Type()) {
				return false
			}
		}
		return true
	case *types.Tuple:
		right, ok := right.(*types.Tuple)
		if !ok || left.Len() != right.Len() {
			return false
		}
		for i := 0; i < left.Len(); i++ {
			if !compare(left.At(i).Type(), right.At(i).Type()) {
				return false
			}
		}
		return true
	case *types.Signature:
		right, ok := right.(*types.Signature)
		if !ok || left.Variadic() != right.Variadic() {
			return false
		}
		if left.TypeParams().Len() != 0 || right.TypeParams().Len() != 0 {
			return types.Identical(left, right)
		}
		return compare(left.Params(), right.Params()) && compare(left.Results(), right.Results())
	case *types.Interface:
		right, ok := right.(*types.Interface)
		if !ok {
			return false
		}
		if !left.IsMethodSet() || !right.IsMethodSet() {
			return types.Identical(left, right)
		}
		if left.NumMethods() != right.NumMethods() {
			return false
		}
		for i := 0; i < left.NumMethods(); i++ {
			a, b := left.Method(i), right.Method(i)
			if a.Id() != b.Id() || !compare(a.Type(), b.Type()) {
				return false
			}
		}
		return true
	}
	return types.Identical(left, right)
}
