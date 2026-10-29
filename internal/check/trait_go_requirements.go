package check

import "go/types"

// A trait application carries the Go constraints declared on both Self and its
// inputs. These are prerequisites of every implementation, just like source
// trait bounds; they are not evidence that an implementation exists.
func (c *checker) traitGoRequirements(bound *TraitBound) []GoPredicate {
	if bound == nil || bound.Trait == nil {
		return nil
	}
	trait := bound.Trait
	sub := map[*types.TypeParam]types.Type{trait.Self: bound.Target}
	parameters := []*types.TypeParam{trait.Self}
	for index, parameter := range trait.TypeParams {
		if index < len(bound.Inputs) {
			sub[parameter] = bound.Inputs[index]
			parameters = append(parameters, parameter)
		}
	}
	for _, associated := range trait.Associated {
		if actual := bound.Associated[associated.Name]; actual != nil {
			sub[associated.Param] = actual
		}
	}
	var result []GoPredicate
	for _, parameter := range parameters {
		if target := sub[parameter]; valid(target) {
			constraint, ok := c.out.Substitute(parameter.Constraint(), sub, bound.constSubstitution).Underlying().(*types.Interface)
			if ok && (!constraint.Empty() || constraint.IsComparable()) {
				result = append(result, GoPredicate{Target: target, Constraint: constraint})
			}
		}
	}
	return result
}

func (c *checker) inheritTraitGoRequirements(bound *TraitBound) {
	for _, requirement := range c.traitGoRequirements(bound) {
		parameter, ok := requirement.Target.(*types.TypeParam)
		if !ok || c.out.isArray(parameter) || types.Satisfies(parameter, requirement.Constraint) {
			continue
		}
		parameter.SetConstraint(types.NewInterfaceType(nil, []types.Type{parameter.Constraint(), requirement.Constraint}).Complete())
	}
}

func (c *checker) satisfiesTraitGoRequirements(bound *TraitBound) bool {
	for _, requirement := range c.traitGoRequirements(bound) {
		if !c.satisfiesGo(requirement.Target, requirement.Constraint) {
			c.proofFailure = "trait Go prerequisite is not satisfied: " + types.TypeString(requirement.Target, nil) + " requires " + types.TypeString(requirement.Constraint, nil)
			return false
		}
	}
	return true
}
