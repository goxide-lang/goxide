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
	for _, evidence := range instance.Evidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, old := range d.plan.Functions {
		if old.Decl == instance.Decl && identicalConstants(old.ConstArgs, instance.ConstArgs) {
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
	definition.Evidence = nil
	for _, bound := range d.c.functionBounds[instance.Signature] {
		definition.Evidence = append(definition.Evidence, Evidence{Bound: substituteBoundConstants(bound, nil, instance.ConstSubstitution)})
	}
	d.plan.Functions = append(d.plan.Functions, &definition)
	instance.Definition = &definition
	body := &BodyInstance{Node: definition.Decl, Decl: definition.Decl, File: definition.File, Function: &definition, Substitution: definition.Substitution, ConstSubstitution: definition.ConstSubstitution}
	definition.Body = body
	d.addBody(body)
	return instance
}

func (d *demandCollector) constImpl(instance *Impl, node ast.Node) {
	if openConstants(instance.ConstArgs) {
		d.unresolved(node, "implementation array length")
		return
	}
	for _, evidence := range instance.RequirementEvidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	template := implDeclaration(instance)
	for _, old := range d.plan.GenericImpls {
		if implDeclaration(old) == template && identicalConstants(old.ConstArgs, instance.ConstArgs) {
			instance.Definition = old
			return
		}
	}
	definition := *template
	definition.Template, definition.Definition = template, nil
	definition.ConstArgs = append([]ConstValue(nil), instance.ConstArgs...)
	definition.ConstSubstitution = instance.ConstSubstitution
	definition.TypeArgs = nil
	definition.Substitution = map[*types.TypeParam]types.Type{}
	for _, parameter := range template.TypeParams {
		definition.TypeArgs = append(definition.TypeArgs, parameter)
		definition.Substitution[parameter] = parameter
	}
	r := newBodyTypes(d.c.out, definition.Substitution, definition.ConstSubstitution)
	definition.Target = r.typ(template.Target)
	definition.Requirements, definition.RequirementEvidence = nil, nil
	for _, bound := range template.Requirements {
		bound = substituteBoundConstants(bound, nil, instance.ConstSubstitution)
		definition.Requirements = append(definition.Requirements, bound)
		definition.RequirementEvidence = append(definition.RequirementEvidence, Evidence{Bound: bound})
	}
	definition.Associated = map[string]types.Type{}
	for name, typ := range template.Associated {
		definition.Associated[name] = r.typ(typ)
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

func (d *demandCollector) enterBody(body *BodyInstance) {
	d.enter(body.File)
	d.c.activeBounds = nil
	if len(body.ConstSubstitution) != 0 {
		d.c.activeBounds = body.Info.GenericBounds[body.Decl]
		if body.Impl != nil {
			d.c.activeBounds = append(append([]*TraitBound(nil), d.c.activeBounds...), body.Impl.Requirements...)
		}
	}
}
