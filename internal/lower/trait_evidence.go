package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
)

func traitBoundTarget(bound *check.TraitBound) types.Type {
	if bound.Target != nil {
		return bound.Target
	}
	return bound.Param
}

func implTemplate(impl *check.Impl) *check.Impl {
	if impl.Definition != nil {
		return impl.Definition
	}
	if len(impl.ConstArgs) != 0 {
		for _, argument := range impl.ConstArgs {
			if argument.Param != nil && impl.Template != nil {
				return impl.Template
			}
		}
		return impl
	}
	if impl.Template != nil {
		return impl.Template
	}
	return impl
}

func parameterizedImpl(impl *check.Impl) bool {
	return len(impl.TypeParams) != 0 || len(impl.ConstParams) != 0
}

func parameterizedTrait(trait *check.Trait) bool {
	return trait != nil && len(trait.TypeParams) != 0
}

func genericPairOps(info *check.Info, impl *check.Impl) bool {
	impl = implTemplate(impl)
	return parameterizedTrait(impl.Trait) && parameterizedImpl(impl) && len(impl.ConstParams) == 0 && len(impl.ConstArgs) == 0 && len(impl.Requirements) == 0 &&
		!implHasBodyCases(info, impl) && (info.Demands == nil || !info.Demands.ImplTemplates[impl])
}

func implHasBodyCases(info *check.Info, impl *check.Impl) bool {
	for _, member := range impl.Methods {
		if len(info.BodyCases[member.Decl]) != 0 {
			return true
		}
	}
	return false
}

func (e *emitter) goConstraint(constraint types.Type) goast.Expr {
	if types.Identical(constraint, types.Universe.Lookup("any").Type()) {
		return goast.NewIdent("any")
	}
	return e.goType(constraint)
}

func (e *emitter) registerEvidenceNames(bounds []*check.TraitBound) {
	if e.evidenceNames == nil {
		e.evidenceNames = map[*check.TraitBound]string{}
	}
	for _, bound := range bounds {
		if e.evidenceNames[bound] != "" {
			continue
		}
		base := "Goxide" + e.templateTargetName(traitBoundTarget(bound)) + "Ops"
		name := base
		for index := 2; e.reserved[name]; index++ {
			name = fmt.Sprintf("%s%d", base, index)
		}
		e.reserved[name] = true
		e.evidenceNames[bound] = name
	}
}

func (e *emitter) evidenceParameters(bounds []*check.TraitBound) []*goast.Field {
	e.registerEvidenceNames(bounds)
	var fields []*goast.Field
	for _, bound := range bounds {
		methods := e.evidenceMethods([]*check.TraitBound{bound})
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(e.evidenceNames[bound])}, Type: &goast.InterfaceType{Methods: &goast.FieldList{List: methods}}})
	}
	return append(fields, projectedParameters(bounds)...)
}

func (e *emitter) evidenceMethods(bounds []*check.TraitBound) []*goast.Field {
	methods := []*goast.Field{{Type: &goast.UnaryExpr{Op: gotoken.TILDE, X: &goast.StructType{Fields: &goast.FieldList{}}}}}
	seen := map[string]bool{}
	for _, bound := range bounds {
		for _, member := range bound.Trait.Methods {
			name := traitOperation(member)
			if seen[name] {
				continue
			}
			seen[name] = true
			ft, _ := e.memberSignature(e.typeState.MethodSignature(bound, member))
			if !member.Static {
				receiver := traitBoundTarget(bound)
				if member.Receiver != check.ValueRef {
					receiver = types.NewPointer(receiver)
				}
				ft.Params.List = append([]*goast.Field{{Names: []*goast.Ident{goast.NewIdent("self")}, Type: e.goType(receiver)}}, ft.Params.List...)
			}
			methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: ft})
			if !member.Static && e.traits.boundNeedsBinding(bound, member.Name) {
				selection := &check.MemberSelection{Member: member, Bound: bound, Signature: e.typeState.MethodSignature(bound, member)}
				methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(traitBinding(member))}, Type: e.goType(e.bindingSignature(selection))})
			}
		}
		for _, associated := range bound.Trait.Associated {
			name := traitWitness(bound.Trait, associated.Name)
			if seen[name] {
				continue
			}
			seen[name] = true
			methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: resultFunc(e.goType(bound.Associated[associated.Name]))})
		}
	}
	return methods
}

func projectedParameters(bounds []*check.TraitBound) []*goast.Field {
	var fields []*goast.Field
	for _, bound := range bounds {
		for _, projected := range bound.Projected {
			fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(projected.Obj().Name())}, Type: goast.NewIdent("any")})
		}
	}
	return fields
}

func (e *emitter) implGenerics(impl *check.Impl) *goast.FieldList {
	if !parameterizedImpl(impl) {
		return nil
	}
	fields := &goast.FieldList{}
	for _, parameter := range impl.TypeParams {
		fields.List = append(fields.List, &goast.Field{Names: []*goast.Ident{e.goType(parameter).(*goast.Ident)}, Type: e.goConstraint(parameter.Constraint())})
	}
	fields.List = append(fields.List, e.evidenceParameters(impl.Requirements)...)
	return fields
}

func (e *emitter) evidenceType(evidence check.Evidence) goast.Expr {
	if evidence.Bound != nil {
		e.registerEvidenceNames([]*check.TraitBound{evidence.Bound})
		return goast.NewIdent(e.evidenceNames[evidence.Bound])
	}
	if evidence.Impl != nil {
		return e.implOpsType(evidence.Impl)
	}
	return goast.NewIdent("GoxideMissingEvidence")
}

func (e *emitter) implTypeArgs(impl *check.Impl) []goast.Expr {
	if !parameterizedImpl(implTemplate(impl)) {
		return nil
	}
	var args []goast.Expr
	if impl.Template != nil {
		for _, argument := range impl.TypeArgs {
			args = append(args, e.goType(argument))
		}
	} else {
		for _, parameter := range impl.TypeParams {
			args = append(args, e.goType(parameter))
		}
	}
	for index, bound := range impl.Requirements {
		evidence := check.Evidence{Bound: bound}
		if index < len(impl.RequirementEvidence) {
			evidence = impl.RequirementEvidence[index]
		}
		args = append(args, e.evidenceType(evidence))
	}
	for index, bound := range impl.Requirements {
		template := bound
		if impl.Template != nil {
			template = impl.Template.Requirements[index]
		}
		for _, projected := range template.Projected {
			actual := types.Type(projected)
			for _, associated := range template.Trait.Associated {
				if types.Identical(template.Associated[associated.Name], projected) {
					actual = bound.Associated[associated.Name]
					break
				}
			}
			args = append(args, e.goType(actual))
		}
	}
	return args
}

func (e *emitter) implOpsName(impl *check.Impl) string {
	impl = implTemplate(impl)
	return "Goxide" + targetName(impl) + traitApplicationName(impl) + "Ops_" + implSuffix(e.info, impl)
}

func (e *emitter) implOpsType(impl *check.Impl) goast.Expr {
	template := implTemplate(impl)
	if carrier := e.traits.carriers[template.Base]; carrier != nil && carrierTarget(e.info, template) {
		return e.sourceGlobal(carrier.file, carrierExportedName(carrier))
	}
	return enumIndices(e.sourceGlobal(template.File, e.implOpsName(template)), e.implTypeArgs(impl))
}

func (e *emitter) implWrapperType(impl *check.Impl) goast.Expr {
	template := implTemplate(impl)
	return enumIndices(e.sourceGlobal(template.File, e.traitWrapper(impl)), e.viewTypeArguments(impl))
}

func (e *emitter) implFactory(impl *check.Impl) goast.Expr {
	template := implTemplate(impl)
	if parameterizedImpl(template) {
		arguments := e.templateImplArguments(impl)
		if impl.Case != nil {
			arguments = e.viewTypeArguments(impl)
		}
		factory := enumIndices(e.sourceGlobal(template.File, e.traitFactoryName(impl)), arguments)
		bounds, evidence := viewCallbacks(impl)
		args := append(e.viewWitnessArguments(impl), e.callbackArguments(bounds, evidence)...)
		args = append(args, e.enumCheckArguments(e.viewEnumChecks(impl))...)
		return &goast.CallExpr{Fun: factory, Args: args}
	}
	return e.sourceGlobal(template.File, e.traitFactoryName(template))
}

func genericTypeDeclaration(name string, parameters *goast.FieldList, typ goast.Expr) goast.Decl {
	return &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{&goast.TypeSpec{Name: goast.NewIdent(name), TypeParams: parameters, Type: typ}}}
}
