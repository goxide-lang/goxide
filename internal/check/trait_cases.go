package check

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// BodyCase is a complete definition-site check of one certified evidence leaf.
// Each leaf also reaches final Go checking even if nobody calls the function.
type BodyCase struct {
	Decl                   *ast.FuncDecl
	Impl                   *Impl
	File                   string
	Info                   *Info
	Key                    string
	TypeParams             []*types.TypeParam
	Substitution           map[*types.TypeParam]types.Type
	ConstSubstitution      map[*ConstParam]ConstValue
	Requirements           []*TraitBound
	GoConstraints          []GoPredicate
	permissionSubstitution map[*types.TypeParam]*permission
	checkerState           *checker
}

type bodyChoice struct {
	goal     *TraitBound
	evidence Evidence
}
type bodyCaseState struct {
	choices            []bodyChoice
	outputs            map[string]map[string]types.Type
	checkedDiagnostics []Diagnostic
}

func (c *checker) recordBodyChoice(goal *TraitBound, evidence Evidence) Evidence {
	state := c.caseState
	key := goal.ApplicationKey()
	if state.outputs == nil {
		state.outputs = map[string]map[string]types.Type{}
	}
	outputs, known := state.outputs[key]
	if !known {
		outputs = evidenceAssociated(evidence)
		if outputs == nil {
			outputs = map[string]types.Type{}
		}
		for _, associated := range goal.Trait.Associated {
			if outputs[associated.Name] == nil {
				parameter := types.NewTypeParam(types.NewTypeName(0, c.pkg, "GoxideChoice"+goal.Trait.Symbol.Name.Name+associated.Name, nil), anyType)
				outputs[associated.Name] = parameter
				if c.out.parameterIdentities == nil {
					c.out.parameterIdentities = map[*types.TypeParam]string{}
				}
				c.out.parameterIdentities[parameter] = key + "::" + associated.Name
			}
		}
		state.outputs[key] = outputs
		state.choices = append(state.choices, bodyChoice{goal, evidence})
	}
	evidence.outputs = outputs
	return evidence
}

func (c *checker) checkFunctionCases(declaration *ast.FuncDecl) {
	if declaration.Body == nil {
		return
	}
	if c.checkingBodyCase {
		c.checkFunctionBody(declaration)
		return
	}
	if c.checkedFunctions == nil {
		c.checkedFunctions = map[*ast.FuncDecl]uint8{}
	}
	if c.checkedFunctions[declaration] != 0 {
		return
	}
	c.checkedFunctions[declaration] = 1
	defer func() { c.checkedFunctions[declaration] = 2 }()
	previous := c.caseState
	state := &bodyCaseState{}
	if signature := c.signatureCases[declaration]; signature != nil {
		state.choices = append(state.choices, signature.choices...)
		state.outputs = cloneMap(signature.outputs)
	}
	c.caseState = state
	defer func() { c.out.Diagnostics = append(c.out.Diagnostics, state.checkedDiagnostics...) }()
	start := len(c.out.Diagnostics)
	c.checkFunctionBody(declaration)
	c.caseState = previous
	if len(state.choices) == 0 {
		return
	}
	// The first pass discovers the complete partition. Only branch checks may
	// diagnose a body whose associated outputs depend on the chosen branch.
	c.out.Diagnostics = c.out.Diagnostics[:start]
	base := c.out
	if base.BodyCases == nil {
		base.BodyCases = map[*ast.FuncDecl][]*BodyCase{}
	}
	seen := map[string]bool{}
	count := 0
	var visit func([]*TraitBound, []GoPredicate, *bodyCaseState, map[*types.TypeParam]types.Type)
	visit = func(requirements []*TraitBound, predicates []GoPredicate, pending *bodyCaseState, inverse map[*types.TypeParam]types.Type) {
		if count >= 256 {
			c.diag(declaration, TraitInvalid, "body evidence partition exceeds the inference budget")
			return
		}
		if len(pending.choices) == 0 {
			return
		}
		choice := pending.choices[0]
		for _, alternative := range choice.evidence.Cases {
			nextRequirements := append([]*TraitBound(nil), requirements...)
			for _, requirement := range alternative.Requirements {
				nextRequirements = append(nextRequirements, substituteBound(requirement, inverse))
			}
			nextPredicates := append([]GoPredicate(nil), predicates...)
			for _, predicate := range alternative.GoConstraints {
				constraint, _ := base.Substitute(predicate.Constraint, inverse, nil).Underlying().(*types.Interface)
				nextPredicates = append(nextPredicates, GoPredicate{base.Substitute(predicate.Target, inverse, nil), constraint})
			}
			key := bodyConditionsKey(base, nextRequirements, nextPredicates)
			if seen[key] {
				c.diag(declaration, TraitInvalid, "body evidence partition did not select a complete leaf")
				continue
			}
			seen[key] = true
			if answer := c.evidenceConditions(nextRequirements, nextPredicates, c.pkg); answer.result == coherenceImpossible {
				continue
			}
			count++
			branch, check := c.bodyCase(declaration, key, nextRequirements, nextPredicates)
			if check.caseState == nil {
				check.caseState = &bodyCaseState{}
			}
			check.checkFunctionBody(declaration)
			if len(check.caseState.choices) != 0 {
				reverse := map[*types.TypeParam]types.Type{}
				for original, replacement := range branch.Substitution {
					if parameter, ok := replacement.(*types.TypeParam); ok {
						reverse[parameter] = original
					}
				}
				visit(nextRequirements, nextPredicates, check.caseState, reverse)
				continue
			}
			base.Diagnostics = append(base.Diagnostics, branch.Info.Diagnostics...)
			branch.Info.Diagnostics = nil
			check.referenceHints.collect([]ast.Item{declaration})
			branch.checkerState = check
			base.BodyCases[declaration] = append(base.BodyCases[declaration], branch)
		}
	}
	visit(nil, nil, state, nil)
	if len(base.BodyCases[declaration]) == 0 && len(base.Diagnostics) == start {
		c.diag(declaration, TraitInvalid, "body evidence partition has no fully checked branch")
	}
	sort.Slice(base.BodyCases[declaration], func(i, j int) bool { return base.BodyCases[declaration][i].Key < base.BodyCases[declaration][j].Key })
}

func bodyConditionsKey(info *Info, requirements []*TraitBound, predicates []GoPredicate) string {
	set := map[string]bool{}
	for _, requirement := range requirements {
		set[requirement.EvidenceKey()] = true
	}
	for _, predicate := range predicates {
		set[sourceTypeKey(info, predicate.Target, nil)+":"+sourceTypeKey(info, predicate.Constraint, nil)] = true
	}
	var parts []string
	for part := range set {
		parts = append(parts, part)
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func (c *checker) bodyCase(declaration *ast.FuncDecl, key string, requirements []*TraitBound, predicates []GoPredicate) (*BodyCase, *checker) {
	branch := &BodyCase{Decl: declaration, Impl: c.currentImpl, File: c.file.Name, Key: key, Substitution: map[*types.TypeParam]types.Type{}, permissionSubstitution: map[*types.TypeParam]*permission{}, GoConstraints: predicates}
	parameters := []*types.TypeParam{}
	if c.currentImpl != nil {
		parameters = append(parameters, c.currentImpl.TypeParams...)
	}
	signature, _ := c.out.Symbols[c.symbol(declaration.Name)].(*types.Signature)
	if signature != nil {
		for index := 0; index < signature.TypeParams().Len(); index++ {
			parameters = append(parameters, signature.TypeParams().At(index))
		}
	}
	for _, requirement := range append(append([]*TraitBound(nil), c.out.GenericBounds[declaration]...), requirements...) {
		parameters = append(parameters, requirement.Projected...)
	}
	if c.currentImpl != nil {
		for _, requirement := range c.currentImpl.Requirements {
			parameters = append(parameters, requirement.Projected...)
		}
	}
	for _, parameter := range parameters {
		if branch.Substitution[parameter] != nil {
			continue
		}
		fresh := types.NewTypeParam(types.NewTypeName(parameter.Obj().Pos(), parameter.Obj().Pkg(), parameter.Obj().Name(), nil), anyType)
		branch.Substitution[parameter] = fresh
		branch.TypeParams = append(branch.TypeParams, fresh)
		branch.permissionSubstitution[parameter] = &permission{typ: fresh}
	}
	for original, replacement := range branch.Substitution {
		constraints := []types.Type{original.Constraint()}
		for _, predicate := range predicates {
			if types.Identical(predicate.Target, original) {
				constraints = append(constraints, predicate.Constraint)
			}
		}
		constraint := types.NewInterfaceType(nil, constraints).Complete()
		replacement.(*types.TypeParam).SetConstraint(c.out.Substitute(constraint, branch.Substitution, nil))
	}
	branch.Info = bodyInfoPermissions(c.out, branch.Substitution, nil, branch.permissionSubstitution)
	branch.Info.typeNodes = cloneMap(c.out.typeNodes)
	branch.Info.Diagnostics = nil
	branch.Info.parameterIdentities = cloneMap(c.out.parameterIdentities)
	for original, replacement := range branch.Substitution {
		branch.Info.parameterIdentities[replacement.(*types.TypeParam)] = c.out.parameterIdentities[original] + ":case:" + key
	}
	check := *c
	check.out = branch.Info
	check.checkingBodyCase = true
	check.caseState = &bodyCaseState{}
	check.proofStack = nil
	check.sourceTypes = cloneMap(c.sourceTypes)
	check.sourceValues = cloneMap(c.sourceValues)
	check.expectedSources = cloneMap(c.expectedSources)
	check.referenceModes = cloneMap(c.referenceModes)
	check.memberSymbols = cloneMap(c.memberSymbols)
	check.functionBounds = cloneMap(c.functionBounds)
	check.activeGoConstraints = nil
	for _, predicate := range predicates {
		constraint, _ := branch.Info.Substitute(predicate.Constraint, branch.Substitution, nil).Underlying().(*types.Interface)
		check.activeGoConstraints = append(check.activeGoConstraints, GoPredicate{branch.Info.Substitute(predicate.Target, branch.Substitution, nil), constraint})
	}
	allRequirements := append(append([]*TraitBound(nil), c.out.GenericBounds[declaration]...), requirements...)
	if c.currentImpl != nil {
		allRequirements = append(allRequirements, c.currentImpl.Requirements...)
	}
	for _, requirement := range allRequirements {
		bound := check.substituteBoundApplication(requirement, branch.Substitution, nil, branch.permissionSubstitution)
		duplicate := false
		for _, old := range branch.Requirements {
			duplicate = duplicate || check.sameApplication(old, bound)
		}
		if !duplicate {
			branch.Requirements = append(branch.Requirements, bound)
		}
	}
	branch.Info.GenericBounds[declaration] = branch.Requirements
	check.activeBounds = branch.Requirements
	if c.currentImpl != nil {
		impl := *c.currentImpl
		impl.Target = branch.Info.Substitute(impl.Target, branch.Substitution, nil)
		impl.Inputs = nil
		for _, input := range c.currentImpl.Inputs {
			impl.Inputs = append(impl.Inputs, branch.Info.Substitute(input, branch.Substitution, nil))
		}
		impl.TypeParams = branch.TypeParams
		impl.Requirements = branch.Requirements
		impl.permissionSubstitution = branch.permissionSubstitution
		impl.targetPermission = check.substituteSourcePermission(c.currentImpl.targetPermission, branch.Substitution, nil, branch.permissionSubstitution)
		impl.inputPermissions = nil
		for _, source := range c.currentImpl.inputPermissions {
			impl.inputPermissions = append(impl.inputPermissions, check.substituteSourcePermission(source, branch.Substitution, nil, branch.permissionSubstitution))
		}
		impl.Associated = map[string]types.Type{}
		impl.associatedPermissions = map[string]*permission{}
		for name, typ := range c.currentImpl.Associated {
			impl.Associated[name] = branch.Info.Substitute(typ, branch.Substitution, nil)
			impl.associatedPermissions[name] = check.substituteSourcePermission(c.currentImpl.associatedPermissions[name], branch.Substitution, nil, branch.permissionSubstitution)
		}
		impl.Methods = nil
		for _, member := range c.currentImpl.Methods {
			copy := *member
			copy.Impl = &impl
			copy.Signature = branch.Info.Substitute(member.Signature, branch.Substitution, nil).(*types.Signature)
			impl.Methods = append(impl.Methods, &copy)
			check.memberSymbols[member.Symbol] = &copy
		}
		check.currentImpl, check.currentSelf = &impl, impl.Target
	}
	if signature != nil && c.currentImpl == nil {
		fresh := types.NewSignatureType(nil, nil, branch.TypeParams, check.tuple(declaration.Params), check.tuple(declaration.Results), signature.Variadic())
		branch.Info.Symbols[c.symbol(declaration.Name)] = fresh
		check.functionBounds[fresh] = branch.Requirements
		branch.Info.FunctionGenerics = cloneMap(c.out.FunctionGenerics)
		branch.Info.FunctionGenerics[fresh] = c.out.FunctionGenerics[signature]
		branch.Info.FunctionConstParams = cloneMap(c.out.FunctionConstParams)
		branch.Info.FunctionConstParams[fresh] = c.out.FunctionConstParams[signature]
	}
	check.referenceHints = check.newPermissionChecker()
	if c.referenceHints != nil {
		for symbol, function := range c.referenceHints.functions {
			copy := *function
			copy.typ = check.newPermissionChecker().substitutePermission(function.typ, branch.permissionSubstitution)
			copy.typ.function = &copy
			check.referenceHints.functions[symbol] = &copy
		}
	}
	ast.Walk(declaration, func(node ast.Node) bool {
		if expression, ok := node.(ast.Expr); ok {
			delete(branch.Info.Members, expression)
			delete(branch.Info.TraitViews, expression)
			delete(branch.Info.FunctionInstances, node)
		}
		return true
	})
	return branch, &check
}

func (c *checker) checkCaseReferences(globals permissionEnv) {
	for declaration, branches := range c.out.BodyCases {
		for _, branch := range branches {
			check := branch.checkerState
			if check == nil {
				continue
			}
			p := check.referenceHints
			p.activeCalls = map[*ast.FuncDecl]bool{}
			p.activeClosures = map[*ast.FuncLit]bool{}
			p.globals = globals
			p.collect([]ast.Item{declaration})
			p.checkItems([]ast.Item{declaration}, globals)
			for _, diagnostic := range branch.Info.Diagnostics {
				diagnostic.Message = fmt.Sprintf("%s (evidence branch)", diagnostic.Message)
				c.out.Diagnostics = append(c.out.Diagnostics, diagnostic)
			}
			branch.Info.Diagnostics = nil
		}
	}
}
