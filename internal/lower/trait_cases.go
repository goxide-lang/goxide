package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func caseFunctionName(name string, branch *check.BodyCase) string {
	if branch == nil {
		return name
	}
	return strings.TrimSuffix(name, "Func") + caseSuffix(branch.Key) + "Func"
}

func caseSuffix(key string) string {
	digest := sha256.Sum256([]byte(key))
	return fmt.Sprintf("Case_%x", digest[:4])
}

func (e *emitter) bodyCaseParameters(branch *check.BodyCase) *goast.FieldList {
	parameters := &goast.FieldList{}
	for _, parameter := range branch.TypeParams {
		constraint := parameter.Constraint()
		if e.instanceBody != nil {
			constraint = e.info.Substitute(constraint, e.instanceBody.Substitution, e.instanceBody.ConstSubstitution)
		}
		parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{e.goType(parameter).(*goast.Ident)}, Type: e.goConstraint(constraint)})
	}
	if len(parameters.List) == 0 {
		return nil
	}
	return parameters
}

func (e *emitter) bodyCaseBounds(branch *check.BodyCase) []*check.TraitBound {
	bounds := append([]*check.TraitBound(nil), branch.Requirements...)
	if e.instanceBody != nil && e.instanceBody.Case == branch {
		for index, evidence := range e.instanceBody.CaseEvidence {
			if index < len(bounds) && evidence.Bound != nil {
				bounds[index] = evidence.Bound
			}
		}
	}
	return bounds
}

func functionCaseArguments(instance *check.FunctionInstance) []types.Type {
	if instance.Case == nil {
		return instance.TypeArgs
	}
	return caseTypeArguments(instance.Case, instance.Substitution)
}

func caseTypeArguments(branch *check.BodyCase, substitution map[*types.TypeParam]types.Type) []types.Type {
	var arguments []types.Type
	for _, parameter := range branch.TypeParams {
		argument := substitution[parameter]
		if argument == nil {
			argument = parameter
		}
		arguments = append(arguments, argument)
	}
	return arguments
}

func (e *emitter) functionCallbacks(function *traitFunction, instance *check.FunctionInstance) ([]*check.TraitBound, []check.Evidence) {
	if instance.Case != nil {
		return instance.Case.Requirements, instance.CaseEvidence
	}
	return e.info.GenericBounds[function.decl], instance.Evidence
}

func selectedMember(selection *check.MemberSelection) *check.Member {
	if selection.Impl == nil || selection.Member.Impl == selection.Impl {
		return selection.Member
	}
	member := *selection.Member
	member.Impl = selection.Impl
	return &member
}

func memberCase(member *check.Member) *check.BodyCase {
	if member.Impl == nil {
		return nil
	}
	return member.Impl.MethodCases[member.Decl]
}

func operationCallbacks(member *check.Member) ([]*check.TraitBound, []check.Evidence) {
	if branch := memberCase(member); branch != nil {
		return branch.Requirements, member.Impl.MethodCaseEvidence[member.Decl]
	}
	return implTemplate(member.Impl).Requirements, member.Impl.RequirementEvidence
}

func (e *emitter) operationTemplateArguments(member *check.Member) []goast.Expr {
	branch := memberCase(member)
	if branch == nil {
		return e.templateImplArguments(member.Impl)
	}
	var arguments []goast.Expr
	for _, argument := range caseTypeArguments(branch, member.Impl.Substitution) {
		arguments = append(arguments, e.goType(argument))
	}
	for _, typ := range e.stableConstTypes(e.stableImplPlan(member.Impl), member.Impl.Substitution, member.Impl.ConstSubstitution) {
		arguments = append(arguments, e.goType(typ))
	}
	return arguments
}

func (e *emitter) operationWitnessArguments(member *check.Member) []goast.Expr {
	branch := memberCase(member)
	if branch == nil {
		return e.templateImplWitnessArguments(member.Impl)
	}
	var witnesses []goast.Expr
	for _, typ := range append([]types.Type{member.Impl.Target}, caseTypeArguments(branch, member.Impl.Substitution)...) {
		witnesses = append(witnesses, e.typeWitness(typ))
	}
	extra, _ := e.stableConstArguments(e.stableImplPlan(member.Impl), member.Impl.Substitution, member.Impl.ConstSubstitution)
	return append(witnesses, extra...)
}

func (p *traitInference) operationWitnesses(member *check.Member) ([]goast.Expr, []types.Type) {
	branch := memberCase(member)
	if branch == nil {
		return p.implWitnesses(member.Impl)
	}
	var arguments []goast.Expr
	var parameters []types.Type
	for _, typ := range append([]types.Type{member.Impl.Target}, caseTypeArguments(branch, member.Impl.Substitution)...) {
		arguments = append(arguments, p.witness(typ))
		parameters = append(parameters, signatureOf(nil, typ))
	}
	extra, extraTypes := p.stableConstants(p.e.stableImplPlan(member.Impl), member.Impl.Substitution, member.Impl.ConstSubstitution)
	return append(arguments, extra...), append(parameters, extraTypes...)
}

func (e *emitter) caseMember(member *check.Member, branch *check.BodyCase) *check.Member {
	impl := *member.Impl
	impl.Template = implTemplate(member.Impl)
	impl.Definition = nil
	if len(impl.ConstArgs) != 0 {
		impl.Definition = impl.Template
	}
	impl.TypeParams = branch.TypeParams
	impl.TypeArgs = nil
	impl.Substitution = branch.Substitution
	impl.ConstSubstitution = branch.ConstSubstitution
	if len(member.Impl.ConstSubstitution) != 0 {
		impl.ConstSubstitution = member.Impl.ConstSubstitution
	}
	impl.Target = e.info.Substitute(impl.Target, branch.Substitution, impl.ConstSubstitution)
	impl.Inputs = make([]types.Type, len(member.Impl.Inputs))
	for i, input := range member.Impl.Inputs {
		impl.Inputs[i] = e.info.Substitute(input, branch.Substitution, impl.ConstSubstitution)
	}
	impl.Associated = map[string]types.Type{}
	for name, output := range member.Impl.Associated {
		impl.Associated[name] = e.info.Substitute(output, branch.Substitution, impl.ConstSubstitution)
	}
	impl.Requirements = e.bodyCaseBounds(branch)
	impl.RequirementEvidence = nil
	impl.MethodCases = map[*syntax.FuncDecl]*check.BodyCase{member.Decl: branch}
	impl.MethodCaseEvidence = nil
	copy := *member
	copy.Impl = &impl
	copy.Signature = e.info.Substitute(member.Signature, branch.Substitution, impl.ConstSubstitution).(*types.Signature)
	return &copy
}

func (e *emitter) caseMethodDeclarations(member *check.Member) []goast.Decl {
	if len(member.Impl.ConstArgs) != 0 && memberCase(member) != nil {
		restore := e.memberBodyInfo(member)
		defer restore()
		selected := e.caseMember(member, memberCase(member))
		declarations := e.templateOperationDeclaration(selected)
		if e.traits.memberNeedsBinding(member) {
			declarations = append(declarations, e.templateBindingDeclaration(selected))
		}
		return declarations
	}
	branches := append([]*check.BodyCase(nil), e.info.BodyCases[member.Decl]...)
	sort.Slice(branches, func(i, j int) bool { return branches[i].Key < branches[j].Key })
	previousInfo, previousBody := e.info, e.instanceBody
	defer func() { e.info, e.instanceBody = previousInfo, previousBody }()
	var declarations []goast.Decl
	for _, branch := range branches {
		e.info = branch.Info
		selected := e.caseMember(member, branch)
		e.instanceBody = &check.BodyInstance{Node: branch.Decl, Decl: branch.Decl, File: branch.File, Info: branch.Info, Impl: selected.Impl, Case: branch, Substitution: branch.Substitution, ConstSubstitution: branch.ConstSubstitution}
		declarations = append(declarations, e.templateOperationDeclaration(selected)...)
		if e.traits.memberNeedsBinding(member) {
			declarations = append(declarations, e.templateBindingDeclaration(selected))
		}
	}
	return declarations
}

// Every checked leaf has a definition-package helper, including leaves not
// selected by this build. Its body remains the directly invoked closure.
func (e *emitter) caseFunctionDeclarations(function *traitFunction) []goast.Decl {
	branches := append([]*check.BodyCase(nil), e.info.BodyCases[function.decl]...)
	sort.Slice(branches, func(i, j int) bool { return branches[i].Key < branches[j].Key })
	previousInfo, previousBody := e.info, e.instanceBody
	defer func() { e.info, e.instanceBody = previousInfo, previousBody }()
	var declarations []goast.Decl
	for _, branch := range branches {
		instance := &check.FunctionInstance{Case: branch, Substitution: branch.Substitution, ConstSubstitution: branch.ConstSubstitution}
		if previousBody != nil && previousBody.Function != nil {
			instance.ConstArgs = previousBody.Function.ConstArgs
		}
		e.info = branch.Info
		e.instanceBody = &check.BodyInstance{Node: branch.Decl, Decl: branch.Decl, File: branch.File, Info: branch.Info, Function: instance, Case: branch, Substitution: branch.Substitution, ConstSubstitution: branch.ConstSubstitution}
		declarations = append(declarations, e.templateFunctionDeclaration(function)...)
	}
	return declarations
}
