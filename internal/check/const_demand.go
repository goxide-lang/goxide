package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Length specialization keeps ordinary type parameters and protocol callbacks
// on a single declaration for each demanded source length tuple.
func (d *demandCollector) constFunction(instance *FunctionInstance, node ast.Node) *FunctionInstance {
	if openConstants(instance.ConstArgs) {
		d.unresolved(node, "function array length")
		return instance
	}
	for _, evidence := range append(append([]Evidence(nil), instance.Evidence...), instance.CaseEvidence...) {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, old := range d.plan.Functions {
		if old.Decl == instance.Decl && old.Case == instance.Case && identicalConstants(old.ConstArgs, instance.ConstArgs) {
			instance.Definition = old
			return instance
		}
	}
	definition := *instance
	definition.Definition = nil
	definition.TypeArgs = identityArguments(d.c, instance.Signature)
	definition.Substitution = map[*types.TypeParam]types.Type{}
	for index := 0; index < instance.Signature.TypeParams().Len(); index++ {
		parameter := instance.Signature.TypeParams().At(index)
		definition.Substitution[parameter] = parameter
	}
	if definition.Case != nil {
		definition.Substitution = copySubstitution(definition.Case.Substitution)
		r := newBodyTypes(d.c.out, definition.Substitution, instance.ConstSubstitution)
		for index, argument := range definition.TypeArgs {
			definition.TypeArgs[index] = r.typ(argument)
		}
		for _, parameter := range definition.Case.TypeParams {
			definition.Substitution[parameter] = parameter
		}
	}
	definition.permissionSubstitution = nil
	definition.Evidence = nil
	for _, bound := range d.c.functionBounds[instance.Signature] {
		definition.Evidence = append(definition.Evidence, Evidence{Bound: d.c.substituteBoundApplication(bound, definition.Substitution, instance.ConstSubstitution, nil)})
	}
	definition.CaseEvidence = nil
	if definition.Case != nil {
		for _, bound := range definition.Case.Requirements {
			definition.CaseEvidence = append(definition.CaseEvidence, Evidence{Bound: d.c.substituteBoundApplication(bound, nil, instance.ConstSubstitution, nil)})
		}
	}
	d.plan.Functions = append(d.plan.Functions, &definition)
	instance.Definition = &definition
	body := &BodyInstance{Node: definition.Decl, Decl: definition.Decl, File: definition.File, Function: &definition, Case: definition.Case, CaseEvidence: definition.CaseEvidence, Substitution: definition.Substitution, ConstSubstitution: definition.ConstSubstitution}
	definition.Body = body
	d.addBody(body)
	return instance
}

func (d *demandCollector) constImpl(instance *Impl, node ast.Node) {
	if openConstants(instance.ConstArgs) {
		d.unresolved(node, "implementation array length")
		return
	}
	proofs := append(append([]Evidence(nil), instance.RequirementEvidence...), instance.CaseEvidence...)
	for _, member := range instance.Methods {
		proofs = append(proofs, instance.MethodCaseEvidence[member.Decl]...)
	}
	for _, evidence := range proofs {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	template := implDeclaration(instance)
	for _, old := range d.plan.GenericImpls {
		if implDeclaration(old) == template && identicalConstants(old.ConstArgs, instance.ConstArgs) && sameMethodCases(old.MethodCases, instance.MethodCases) {
			instance.Definition = old
			return
		}
	}
	definition := *template
	definition.Template, definition.Definition = template, nil
	definition.Case = d.constImplCase(instance.Case, instance.ConstSubstitution)
	definition.CaseEvidence = nil
	definition.MethodCases = instance.MethodCases
	definition.MethodCaseEvidence = map[*ast.FuncDecl][]Evidence{}
	definition.ConstArgs = append([]ConstValue(nil), instance.ConstArgs...)
	definition.ConstSubstitution = instance.ConstSubstitution
	definition.TypeArgs = nil
	definition.Substitution = map[*types.TypeParam]types.Type{}
	for _, parameter := range template.TypeParams {
		definition.TypeArgs = append(definition.TypeArgs, parameter)
		definition.Substitution[parameter] = parameter
	}
	if definition.Case != nil {
		definition.Substitution = copySubstitution(definition.Case.Substitution)
		for index, parameter := range template.TypeParams {
			definition.TypeArgs[index] = definition.Substitution[parameter]
		}
		for _, bound := range definition.Case.Requirements {
			definition.CaseEvidence = append(definition.CaseEvidence, Evidence{Bound: d.c.substituteBoundApplication(bound, nil, instance.ConstSubstitution, nil)})
		}
	}
	r := newBodyTypes(d.c.out, definition.Substitution, definition.ConstSubstitution)
	definition.Target = r.typ(template.Target)
	definition.Inputs = nil
	for _, input := range template.Inputs {
		definition.Inputs = append(definition.Inputs, r.typ(input))
	}
	definition.permissionSubstitution = nil
	definition.targetPermission = d.c.substituteSourcePermission(template.targetPermission, definition.Substitution, definition.ConstSubstitution, nil)
	definition.inputPermissions = nil
	for _, source := range template.inputPermissions {
		definition.inputPermissions = append(definition.inputPermissions, d.c.substituteSourcePermission(source, definition.Substitution, definition.ConstSubstitution, nil))
	}
	definition.Requirements, definition.RequirementEvidence = nil, nil
	for _, bound := range template.Requirements {
		bound = d.c.substituteBoundApplication(bound, definition.Substitution, instance.ConstSubstitution, nil)
		definition.Requirements = append(definition.Requirements, bound)
		definition.RequirementEvidence = append(definition.RequirementEvidence, Evidence{Bound: bound})
	}
	definition.Associated = map[string]types.Type{}
	definition.associatedPermissions = map[string]*permission{}
	for name, typ := range template.Associated {
		definition.Associated[name] = r.typ(typ)
		definition.associatedPermissions[name] = d.c.substituteSourcePermission(template.associatedPermissions[name], definition.Substitution, definition.ConstSubstitution, nil)
	}
	for node, branch := range definition.MethodCases {
		for _, bound := range branch.Requirements {
			definition.MethodCaseEvidence[node] = append(definition.MethodCaseEvidence[node], Evidence{Bound: d.c.substituteBoundApplication(bound, nil, instance.ConstSubstitution, nil)})
		}
	}
	definition.Methods = nil
	for _, member := range template.Methods {
		copy := *member
		copy.Impl = &definition
		copy.Signature = types.NewSignatureType(nil, nil, nil, r.tuple(member.Signature.Params()), r.tuple(member.Signature.Results()), member.Signature.Variadic())
		definition.Methods = append(definition.Methods, &copy)
	}
	instance.Definition = &definition
	d.genericImpl(&definition)
}

// Runtime views share one parameter environment across all methods. Keep that
// environment when fixing the length, and resolve its bodies in the same way as
// the separately emitted static method leaves.
func (d *demandCollector) constImplCase(branch *ImplCase, constants map[*ConstParam]ConstValue) *ImplCase {
	if branch == nil {
		return nil
	}
	fixed := *branch
	fixed.ConstSubstitution = cloneMap(constants)
	fixed.Info = bodyInfoPermissions(branch.Info, nil, constants, nil)
	fixed.Info.Demands = d.plan
	fixed.Requirements = nil
	for _, requirement := range branch.Requirements {
		fixed.Requirements = append(fixed.Requirements, d.c.substituteBoundApplication(requirement, nil, constants, nil))
	}
	fixed.GoConstraints = nil
	r := newBodyTypes(branch.Info, nil, constants)
	for _, predicate := range branch.GoConstraints {
		constraint, _ := r.typ(predicate.Constraint).Underlying().(*types.Interface)
		fixed.GoConstraints = append(fixed.GoConstraints, GoPredicate{Target: r.typ(predicate.Target), Constraint: constraint})
	}
	fixed.Methods = map[*ast.FuncDecl]*BodyCase{}
	for _, member := range branch.Impl.Methods {
		node := member.Decl
		method := branch.Methods[node]
		if method == nil {
			continue
		}
		copy := *method
		copy.ConstSubstitution = fixed.ConstSubstitution
		copy.GoConstraints = fixed.GoConstraints
		copy.Requirements = nil
		for _, requirement := range method.Requirements {
			copy.Requirements = append(copy.Requirements, d.c.substituteBoundApplication(requirement, nil, constants, nil))
		}
		body := &BodyInstance{Node: node, Decl: node, File: copy.File, Case: &copy, Substitution: copy.Substitution, ConstSubstitution: constants, permissionSubstitution: copy.permissionSubstitution, caseOnly: true}
		d.addBody(body)
		copy.Info = body.Info
		fixed.Methods[node] = &copy
	}
	return &fixed
}

func (d *demandCollector) enterBody(body *BodyInstance) {
	d.enter(body.File)
	d.c.activeBounds = nil
	d.c.activeGoConstraints = nil
	if body.Case != nil {
		r := newBodyTypes(d.c.out, body.Substitution, body.ConstSubstitution)
		for _, predicate := range body.Case.GoConstraints {
			constraint, _ := r.typ(predicate.Constraint).Underlying().(*types.Interface)
			d.c.activeGoConstraints = append(d.c.activeGoConstraints, GoPredicate{Target: r.typ(predicate.Target), Constraint: constraint})
		}
		for _, bound := range body.Case.Requirements {
			d.c.activeBounds = append(d.c.activeBounds, d.c.substituteBoundApplication(bound, body.Substitution, body.ConstSubstitution, body.permissionSubstitution))
		}
	}
	if len(body.ConstSubstitution) != 0 {
		d.c.activeBounds = append(d.c.activeBounds, body.Info.GenericBounds[body.Decl]...)
		if body.Impl != nil {
			d.c.activeBounds = append(append([]*TraitBound(nil), d.c.activeBounds...), body.Impl.Requirements...)
		}
	} else if body.Function != nil && openArguments(body.Function.TypeArgs) {
		// Reuse proven premises so recursive open calls retain their evidence identity.
		for _, evidence := range body.Function.Evidence {
			if evidence.Bound != nil {
				d.c.activeBounds = append(d.c.activeBounds, evidence.Bound)
			}
		}
	}
}

func sameMethodCases(left, right map[*ast.FuncDecl]*BodyCase) bool {
	if len(left) != len(right) {
		return false
	}
	for method, branch := range left {
		if other := right[method]; other == nil || other.Key != branch.Key {
			return false
		}
	}
	return true
}

func sameMethodCaseEvidence(left, right map[*ast.FuncDecl][]Evidence) bool {
	if len(left) != len(right) {
		return false
	}
	for method, evidence := range left {
		if !sameEvidence(evidence, right[method]) {
			return false
		}
	}
	return true
}
