package lower

import (
	goast "go/ast"
	"go/types"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
)

func implCaseArguments(impl *check.Impl) []types.Type {
	return caseTypeArguments(&check.BodyCase{TypeParams: impl.Case.TypeParams}, impl.Substitution)
}

func (e *emitter) viewTypeArguments(impl *check.Impl) []goast.Expr {
	if impl.Case == nil {
		return e.templateImplReceiverArguments(impl)
	}
	var arguments []goast.Expr
	for _, typ := range implCaseArguments(impl) {
		arguments = append(arguments, e.goType(typ))
	}
	for _, typ := range e.stableConstTypes(e.stableImplPlan(impl), impl.Substitution, impl.ConstSubstitution) {
		arguments = append(arguments, e.goType(typ))
	}
	return arguments
}

func viewCallbacks(impl *check.Impl) ([]*check.TraitBound, []check.Evidence) {
	if impl.Case != nil {
		return impl.Case.Requirements, impl.CaseEvidence
	}
	return implTemplate(impl).Requirements, impl.RequirementEvidence
}

func (e *emitter) viewWitnessArguments(impl *check.Impl) []goast.Expr {
	if impl.Case == nil {
		return e.templateImplWitnessArguments(impl)
	}
	var witnesses []goast.Expr
	for _, typ := range append([]types.Type{impl.Target}, implCaseArguments(impl)...) {
		witnesses = append(witnesses, e.typeWitness(typ))
	}
	extra, _ := e.stableConstArguments(e.stableImplPlan(impl), impl.Substitution, impl.ConstSubstitution)
	return append(witnesses, extra...)
}

func (p *traitInference) viewWitnesses(impl *check.Impl) ([]goast.Expr, []types.Type) {
	if impl.Case == nil {
		return p.implWitnesses(impl)
	}
	var arguments []goast.Expr
	var parameters []types.Type
	for _, typ := range append([]types.Type{impl.Target}, implCaseArguments(impl)...) {
		arguments = append(arguments, p.witness(typ))
		parameters = append(parameters, signatureOf(nil, typ))
	}
	extra, extraTypes := p.stableConstants(p.e.stableImplPlan(impl), impl.Substitution, impl.ConstSubstitution)
	return append(arguments, extra...), append(parameters, extraTypes...)
}

// A wrapper is emitted once for each reachable common method partition. Its
// methods contain the checked business bodies, preserving direct recover.
func (e *emitter) caseRuntimeDeclarations(impl *check.Impl) []goast.Decl {
	source := implTemplate(impl)
	if len(source.ConstArgs) != 0 && source.Template != nil {
		source = source.Template
	}
	branches := append([]*check.ImplCase(nil), e.info.ImplCases[source]...)
	if impl.Case != nil {
		branches = []*check.ImplCase{impl.Case}
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Key < branches[j].Key })
	previousInfo, previousBody, previousView := e.info, e.instanceBody, e.caseViewImpl
	defer func() { e.info, e.instanceBody, e.caseViewImpl = previousInfo, previousBody, previousView }()
	var declarations []goast.Decl
	for _, branch := range branches {
		copy := *impl
		copy.Case = branch
		copy.Definition = implTemplate(impl)
		copy.TypeParams = branch.TypeParams
		copy.TypeArgs = nil
		copy.Substitution = branch.Substitution
		copy.Requirements, copy.RequirementEvidence = branch.Requirements, nil
		copy.CaseEvidence = nil
		copy.MethodCases = branch.Methods
		copy.MethodCaseEvidence = nil
		copy.Target = branch.Info.Substitute(impl.Target, branch.Substitution, impl.ConstSubstitution)
		copy.Inputs = make([]types.Type, len(impl.Inputs))
		for index, input := range impl.Inputs {
			copy.Inputs[index] = branch.Info.Substitute(input, branch.Substitution, impl.ConstSubstitution)
		}
		copy.Associated = map[string]types.Type{}
		for name, output := range impl.Associated {
			copy.Associated[name] = branch.Info.Substitute(output, branch.Substitution, impl.ConstSubstitution)
		}
		copy.Methods = nil
		for _, member := range impl.Methods {
			selected := *member
			selected.Impl = &copy
			selected.Signature = branch.Info.Substitute(member.Signature, branch.Substitution, impl.ConstSubstitution).(*types.Signature)
			copy.Methods = append(copy.Methods, &selected)
		}
		e.info, e.instanceBody, e.caseViewImpl = branch.Info, nil, &copy
		declarations = append(declarations, e.templateRuntimeDeclarations(&copy)...)
	}
	return declarations
}
