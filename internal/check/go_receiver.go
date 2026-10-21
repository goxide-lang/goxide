package check

import "go/types"

// Go receiver declarations cover either a nongeneric named type or every
// instantiation of a generic named type. Partial and repeated arguments cannot
// be expressed as a receiver declaration.
func ordinaryGoReceiverTarget(impl *Impl) *types.Named {
	if impl == nil || impl.AliasOf != nil {
		return nil
	}
	named, ok := types.Unalias(impl.Target).(*types.Named)
	if !ok || named.Obj().Pkg() != impl.Owner {
		return nil
	}
	switch named.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return nil
	}
	origin := named.Origin()
	if origin.TypeParams().Len() == 0 {
		return origin
	}
	if named.TypeArgs().Len() != origin.TypeParams().Len() || len(impl.TypeParams) != origin.TypeParams().Len() {
		return nil
	}
	seen := map[*types.TypeParam]bool{}
	for i := 0; i < named.TypeArgs().Len(); i++ {
		parameter, ok := named.TypeArgs().At(i).(*types.TypeParam)
		if !ok || seen[parameter] {
			return nil
		}
		found := false
		for _, declared := range impl.TypeParams {
			found = found || declared == parameter
		}
		if !found {
			return nil
		}
		seen[parameter] = true
	}
	return origin
}

// CanUseGoReceiver reports whether this impl can declare ordinary Go methods
// for every instantiation admitted by the named type's own constraints.
// Rechecking against those parameters avoids treating an impl's stronger
// constraints as facts about every receiver instantiation.
func CanUseGoReceiver(impl *Impl) bool {
	if ordinaryGoReceiverTarget(impl) == nil || len(impl.Requirements) != 0 {
		return false
	}
	target := types.Unalias(impl.Target).(*types.Named)
	sub := map[*types.TypeParam]types.Type{}
	for i, parameter := range impl.TypeParams {
		if target.TypeArgs().At(i) != parameter {
			return false
		}
		sub[parameter] = target.Origin().TypeParams().At(i)
	}
	for _, parameter := range impl.TypeParams {
		constraint, ok := substitute(parameter.Constraint(), sub, impl.info).Underlying().(*types.Interface)
		if !ok || !types.Satisfies(sub[parameter], constraint) {
			return false
		}
	}
	return true
}

func universalGoTarget(impl *Impl) types.Type {
	origin := ordinaryGoReceiverTarget(impl)
	if origin == nil {
		return nil
	}
	if origin.TypeParams().Len() == 0 {
		return origin
	}
	arguments := make([]types.Type, origin.TypeParams().Len())
	for i := range arguments {
		arguments[i] = origin.TypeParams().At(i)
	}
	target, _ := types.Instantiate(nil, origin, arguments, false)
	return target
}
