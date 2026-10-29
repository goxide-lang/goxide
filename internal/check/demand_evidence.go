package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (d *demandCollector) instantiateMember(info *Info, selection *MemberSelection, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) (*MemberSelection, bool) {
	result := *selection
	r := newBodyTypes(info, sub, constants)
	result.Signature, _ = r.typ(selection.Signature).(*types.Signature)
	witness := selection.Evidence
	if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
		witness = Evidence{Impl: selection.Impl, Bound: selection.Bound}
	}
	witness, resolved := d.instantiateEvidence(witness, sub, constants, sources)
	result.Impl, result.Bound, result.Evidence = witness.Impl, witness.Bound, witness
	if witness.Impl != nil {
		result.Bound = nil
		for _, member := range witness.Impl.Methods {
			if member.Decl == selection.Member.Decl || member.Name == selection.Member.Name && member.Static == selection.Member.Static {
				result.Member = member
				break
			}
		}
	}
	return &result, resolved
}

// Demand instantiation follows evidence already certified at the declaration.
// It may discharge a premise or choose a certified leaf, but never retries a
// different implementation after an implementation has been selected.
func (d *demandCollector) instantiateEvidence(evidence Evidence, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) (Evidence, bool) {
	if evidence.Bound != nil {
		goal := d.c.substituteBoundApplication(evidence.Bound, sub, constants, sources)
		witness := d.c.proveApplication(goal)
		if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
			return Evidence{}, false
		}
		if len(witness.Cases) != 0 {
			return d.instantiateEvidence(witness, nil, nil, nil)
		}
		if !d.applicationOutputs(goal, witness) {
			return Evidence{}, false
		}
		if witness.Impl != nil {
			return d.instantiateEvidence(witness, nil, nil, nil)
		}
		return witness, true
	}
	if evidence.Impl != nil {
		impl, ok := d.instantiateImpl(evidence.Impl, sub, constants, sources)
		return Evidence{Impl: impl}, ok
	}
	if len(evidence.Cases) == 0 {
		return evidence, true
	}
	var selected Evidence
	found := false
	for _, branch := range evidence.Cases {
		if !d.casePredicates(branch.GoConstraints, sub, constants) {
			continue
		}
		possible := true
		for _, requirement := range branch.Requirements {
			if _, ok := d.instantiateEvidence(Evidence{Bound: requirement}, sub, constants, sources); !ok {
				possible = false
				break
			}
		}
		if !possible {
			continue
		}
		witness, ok := d.instantiateEvidence(branch.Evidence, sub, constants, sources)
		if !ok || found && selected.Key() != witness.Key() {
			return Evidence{}, false
		}
		selected, found = witness, true
	}
	return selected, found
}

func (d *demandCollector) applicationOutputs(goal *TraitBound, witness Evidence) bool {
	actual := evidenceAssociated(witness)
	for name, want := range goal.Associated {
		if projectedBinding(goal, name) != nil {
			continue
		}
		if !types.Identical(want, actual[name]) {
			return false
		}
		if source := goal.associatedPermissions[name]; source != nil && !d.c.sameSourceType(source, d.c.evidenceAssociatedPermission(witness, name)) {
			return false
		}
	}
	return true
}

func (d *demandCollector) casePredicates(predicates []GoPredicate, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) bool {
	r := newBodyTypes(d.c.out, sub, constants)
	for _, predicate := range predicates {
		constraint, ok := r.typ(predicate.Constraint).Underlying().(*types.Interface)
		if !ok || !d.c.satisfiesGo(r.typ(predicate.Target), constraint) {
			return false
		}
	}
	return true
}

func (d *demandCollector) composePermissions(previous map[*types.TypeParam]*permission, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) map[*types.TypeParam]*permission {
	result := map[*types.TypeParam]*permission{}
	for parameter, source := range previous {
		result[parameter] = d.c.substituteSourcePermission(source, sub, constants, sources)
	}
	for parameter, source := range sources {
		if result[parameter] == nil {
			result[parameter] = source
		}
	}
	return result
}

func composeConstants(previous, next map[*ConstParam]ConstValue) map[*ConstParam]ConstValue {
	result := map[*ConstParam]ConstValue{}
	for parameter, value := range previous {
		result[parameter] = resolveConst(value, next)
	}
	for parameter, value := range next {
		if _, exists := result[parameter]; !exists {
			result[parameter] = value
		}
	}
	return result
}

func (d *demandCollector) instantiateImpl(impl *Impl, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) (*Impl, bool) {
	if impl.Template == nil && len(impl.TypeParams) == 0 && len(impl.ConstParams) == 0 && len(impl.Requirements) == 0 {
		partitioned := false
		for _, member := range impl.Methods {
			partitioned = partitioned || len(d.c.out.BodyCases[member.Decl]) != 0
		}
		if !partitioned {
			return impl, true
		}
	}
	result := *impl
	r := newBodyTypes(d.c.out, sub, constants)
	result.Substitution = copySubstitution(sub)
	for parameter, value := range impl.Substitution {
		result.Substitution[parameter] = r.typ(value)
	}
	result.ConstSubstitution = composeConstants(impl.ConstSubstitution, constants)
	result.permissionSubstitution = d.composePermissions(impl.permissionSubstitution, sub, constants, sources)
	result.Target = r.typ(impl.Target)
	result.Inputs = nil
	for _, input := range impl.Inputs {
		result.Inputs = append(result.Inputs, r.typ(input))
	}
	result.TypeArgs = nil
	for _, argument := range impl.TypeArgs {
		result.TypeArgs = append(result.TypeArgs, r.typ(argument))
	}
	if impl.Template == nil && (len(impl.TypeParams) != 0 || len(impl.ConstParams) != 0) {
		template := d.c.out.Impls[impl.Decl]
		if template == nil {
			template = impl
		}
		result.Template = template
		for index, parameter := range template.TypeParams {
			argument := types.Type(parameter)
			if index < len(impl.TypeParams) {
				argument = r.typ(impl.TypeParams[index])
			}
			result.TypeArgs = append(result.TypeArgs, argument)
			result.Substitution[parameter] = argument
		}
	}
	result.ConstArgs = nil
	for _, argument := range impl.ConstArgs {
		result.ConstArgs = append(result.ConstArgs, resolveConst(argument, constants))
	}
	if impl.Template == nil {
		for _, parameter := range impl.ConstParams {
			result.ConstArgs = append(result.ConstArgs, resolveConst(ConstValue{Param: parameter}, constants))
		}
	}
	result.targetPermission = d.c.substituteSourcePermission(impl.targetPermission, sub, constants, sources)
	result.inputPermissions = nil
	for _, source := range impl.inputPermissions {
		result.inputPermissions = append(result.inputPermissions, d.c.substituteSourcePermission(source, sub, constants, sources))
	}
	result.Associated = map[string]types.Type{}
	result.associatedPermissions = map[string]*permission{}
	for name, typ := range impl.Associated {
		result.Associated[name] = r.typ(typ)
		result.associatedPermissions[name] = d.c.substituteSourcePermission(impl.associatedPermissions[name], sub, constants, sources)
	}
	result.Requirements, result.RequirementEvidence = nil, nil
	for index, requirement := range impl.Requirements {
		result.Requirements = append(result.Requirements, d.c.substituteBoundApplication(requirement, sub, constants, sources))
		witness := Evidence{Bound: requirement}
		if index < len(impl.RequirementEvidence) {
			witness = impl.RequirementEvidence[index]
		}
		actual, ok := d.instantiateEvidence(witness, sub, constants, sources)
		if !ok {
			return nil, false
		}
		result.RequirementEvidence = append(result.RequirementEvidence, actual)
	}
	result.Methods = nil
	for _, member := range impl.Methods {
		copy := *member
		copy.Impl = &result
		copy.Signature, _ = r.typ(member.Signature).(*types.Signature)
		result.Methods = append(result.Methods, &copy)
	}
	result.MethodCases = map[*ast.FuncDecl]*BodyCase{}
	result.MethodCaseEvidence = map[*ast.FuncDecl][]Evidence{}
	if !d.selectImplCase(&result) {
		return nil, false
	}
	for _, member := range result.Methods {
		if len(d.c.out.BodyCases[member.Decl]) == 0 {
			continue
		}
		branch, applied, evidence, ok := d.selectBodyCase(member.Decl, result.Substitution, result.ConstSubstitution, result.permissionSubstitution)
		if !ok {
			return nil, false
		}
		if result.Case != nil {
			if expected := result.Case.Methods[member.Decl]; expected == nil || expected.Key != branch.Key {
				return nil, false
			}
		}
		result.MethodCases[member.Decl], result.MethodCaseEvidence[member.Decl] = branch, evidence
		for parameter, typ := range applied {
			result.Substitution[parameter] = typ
		}
		result.permissionSubstitution = d.casePermissions(branch, applied, result.ConstSubstitution, result.permissionSubstitution)
	}
	return &result, true
}

func (d *demandCollector) selectBodyCase(decl *ast.FuncDecl, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) (*BodyCase, map[*types.TypeParam]types.Type, []Evidence, bool) {
	var selected *BodyCase
	var applied map[*types.TypeParam]types.Type
	var evidence []Evidence
	for _, branch := range d.c.out.BodyCases[decl] {
		if !d.casePredicates(branch.GoConstraints, sub, constants) {
			continue
		}
		next := copySubstitution(sub)
		r := newBodyTypes(d.c.out, sub, constants)
		for original, typ := range branch.Substitution {
			if parameter, ok := typ.(*types.TypeParam); ok {
				next[parameter] = r.typ(original)
			}
		}
		nextSources := d.casePermissions(branch, next, constants, sources)
		var proofs []Evidence
		possible := true
		for _, requirement := range branch.Requirements {
			witness, ok := d.instantiateEvidence(Evidence{Bound: requirement}, next, constants, nextSources)
			if !ok {
				possible = false
				break
			}
			proofs = append(proofs, witness)
		}
		if !possible {
			continue
		}
		if selected != nil && selected.Key != branch.Key {
			return nil, nil, nil, false
		}
		selected, applied, evidence = branch, next, proofs
	}
	return selected, applied, evidence, selected != nil
}

func (d *demandCollector) casePermissions(branch *BodyCase, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) map[*types.TypeParam]*permission {
	result := d.composePermissions(branch.permissionSubstitution, sub, constants, sources)
	for original, typ := range branch.Substitution {
		if parameter, ok := typ.(*types.TypeParam); ok {
			source := sources[original]
			if source == nil {
				source = d.c.newPermissionChecker().fromGo(original)
			}
			result[parameter] = d.c.substituteSourcePermission(source, sub, constants, sources)
		}
	}
	return result
}
