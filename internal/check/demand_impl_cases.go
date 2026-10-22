package check

import (
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// ImplCase is a reachable conjunction of already checked method leaves. All
// methods share one set of fresh parameters, so correlated predicates cannot
// accidentally become independent choices in a runtime view.
type ImplCase struct {
	Impl                   *Impl
	Info                   *Info
	Key                    string
	TypeParams             []*types.TypeParam
	Substitution           map[*types.TypeParam]types.Type
	ConstSubstitution      map[*ConstParam]ConstValue
	Requirements           []*TraitBound
	GoConstraints          []GoPredicate
	Methods                map[*ast.FuncDecl]*BodyCase
	permissionSubstitution map[*types.TypeParam]*permission
}

func (c *checker) prepareImplCases() {
	if c.out.ImplCases == nil {
		c.out.ImplCases = map[*Impl][]*ImplCase{}
	}
	for _, impl := range c.implOrder {
		c.prepareImplCase(impl)
	}
}

func (c *checker) prepareImplCase(impl *Impl) {
	if len(c.out.ImplCases[impl]) != 0 {
		return
	}
	var methods []*Member
	for _, member := range impl.Methods {
		if len(c.out.BodyCases[member.Decl]) != 0 {
			methods = append(methods, member)
		}
	}
	if len(methods) == 0 {
		return
	}
	file, pkg := c.file, c.pkg
	c.file, c.pkg = c.out.Resolve.Files[impl.File], impl.Owner
	defer func() { c.file, c.pkg = file, pkg }()
	selected := map[*ast.FuncDecl]*BodyCase{}
	var visit func(int, []*TraitBound, []GoPredicate)
	visit = func(index int, requirements []*TraitBound, predicates []GoPredicate) {
		if len(c.out.ImplCases[impl]) >= 256 {
			c.diag(impl.Decl, TraitInvalid, "implementation evidence partition exceeds the inference budget")
			return
		}
		if index == len(methods) {
			c.out.ImplCases[impl] = append(c.out.ImplCases[impl], c.sharedImplCase(impl, selected, requirements, predicates))
			return
		}
		method := methods[index]
		for _, branch := range c.out.BodyCases[method.Decl] {
			inverse := map[*types.TypeParam]types.Type{}
			for original, replacement := range branch.Substitution {
				if parameter, ok := replacement.(*types.TypeParam); ok {
					inverse[parameter] = original
				}
			}
			nextRequirements := append([]*TraitBound(nil), requirements...)
			for _, requirement := range branch.Requirements {
				next := c.substituteBoundApplication(requirement, inverse, nil, nil)
				duplicate := false
				for _, previous := range nextRequirements {
					duplicate = duplicate || previous.EvidenceKey() == next.EvidenceKey()
				}
				if !duplicate {
					nextRequirements = append(nextRequirements, next)
				}
			}
			nextPredicates := append(append([]GoPredicate(nil), predicates...), branch.GoConstraints...)
			if answer := c.evidenceConditions(nextRequirements, nextPredicates, impl.Owner); answer.result == coherenceImpossible {
				continue
			}
			selected[method.Decl] = branch
			visit(index+1, nextRequirements, nextPredicates)
			delete(selected, method.Decl)
		}
	}
	visit(0, append([]*TraitBound(nil), impl.Requirements...), nil)
	sort.Slice(c.out.ImplCases[impl], func(i, j int) bool { return c.out.ImplCases[impl][i].Key < c.out.ImplCases[impl][j].Key })
}

func (c *checker) sharedImplCase(impl *Impl, methods map[*ast.FuncDecl]*BodyCase, requirements []*TraitBound, predicates []GoPredicate) *ImplCase {
	branch := &ImplCase{Impl: impl, Substitution: map[*types.TypeParam]types.Type{}, Methods: map[*ast.FuncDecl]*BodyCase{}, GoConstraints: predicates, permissionSubstitution: map[*types.TypeParam]*permission{}}
	var keys []string
	for _, member := range impl.Methods {
		if method := methods[member.Decl]; method != nil {
			keys = append(keys, member.Name+":"+method.Key)
		}
	}
	sort.Strings(keys)
	branch.Key = impl.DeclID + ":" + strings.Join(keys, "|")
	parameters := append([]*types.TypeParam(nil), impl.TypeParams...)
	for _, requirement := range requirements {
		parameters = append(parameters, requirement.Projected...)
	}
	for _, parameter := range parameters {
		if branch.Substitution[parameter] != nil {
			continue
		}
		fresh := types.NewTypeParam(types.NewTypeName(parameter.Obj().Pos(), parameter.Obj().Pkg(), parameter.Obj().Name(), nil), anyType)
		branch.Substitution[parameter] = fresh
		branch.permissionSubstitution[parameter] = &permission{typ: fresh}
		branch.TypeParams = append(branch.TypeParams, fresh)
	}
	for original, replacement := range branch.Substitution {
		components := []types.Type{original.Constraint()}
		for _, predicate := range predicates {
			if types.Identical(predicate.Target, original) {
				components = append(components, predicate.Constraint)
			}
		}
		replacement.(*types.TypeParam).SetConstraint(c.out.Substitute(types.NewInterfaceType(nil, components).Complete(), branch.Substitution, nil))
	}
	branch.Info = bodyInfoPermissions(c.out, branch.Substitution, nil, branch.permissionSubstitution)
	branch.Info.parameterIdentities = cloneMap(c.out.parameterIdentities)
	for original, replacement := range branch.Substitution {
		branch.Info.parameterIdentities[replacement.(*types.TypeParam)] = sourceTypeKey(c.out, original, nil) + ":implcase:" + branch.Key
	}
	for _, requirement := range requirements {
		branch.Requirements = append(branch.Requirements, c.substituteBoundApplication(requirement, branch.Substitution, nil, branch.permissionSubstitution))
	}
	for node, method := range methods {
		rebound := *method
		rebound.TypeParams = branch.TypeParams
		rebound.GoConstraints = predicates
		rebound.Substitution = copySubstitution(branch.Substitution)
		for original, previous := range method.Substitution {
			if parameter, ok := previous.(*types.TypeParam); ok {
				if common := branch.Substitution[original]; common != nil {
					rebound.Substitution[parameter] = common
				}
			}
		}
		rebound.Requirements = nil
		for _, requirement := range method.Requirements {
			rebound.Requirements = append(rebound.Requirements, c.substituteBoundApplication(requirement, rebound.Substitution, nil, branch.permissionSubstitution))
		}
		rebound.permissionSubstitution = branch.permissionSubstitution
		rebound.Info = bodyInfoPermissions(method.Info, rebound.Substitution, nil, branch.permissionSubstitution)
		rebound.Info.parameterIdentities = branch.Info.parameterIdentities
		branch.Methods[node] = &rebound
	}
	return branch
}

func (d *demandCollector) selectImplCase(impl *Impl) bool {
	branches := d.c.out.ImplCases[implDeclaration(impl)]
	if len(branches) == 0 {
		return true
	}
	var selected *ImplCase
	var substitution map[*types.TypeParam]types.Type
	var permissions map[*types.TypeParam]*permission
	var evidence []Evidence
	for _, branch := range branches {
		if !d.casePredicates(branch.GoConstraints, impl.Substitution, impl.ConstSubstitution) {
			continue
		}
		next := copySubstitution(impl.Substitution)
		nextSources := d.composePermissions(branch.permissionSubstitution, next, impl.ConstSubstitution, impl.permissionSubstitution)
		r := newBodyTypes(d.c.out, next, impl.ConstSubstitution)
		for original, typ := range branch.Substitution {
			if parameter, ok := typ.(*types.TypeParam); ok {
				next[parameter] = r.typ(original)
				source := impl.permissionSubstitution[original]
				if source == nil {
					source = d.c.newPermissionChecker().fromGo(original)
				}
				nextSources[parameter] = d.c.substituteSourcePermission(source, next, impl.ConstSubstitution, impl.permissionSubstitution)
			}
		}
		var proofs []Evidence
		possible := true
		for _, requirement := range branch.Requirements {
			proof, ok := d.instantiateEvidence(Evidence{Bound: requirement}, next, impl.ConstSubstitution, nextSources)
			if !ok {
				possible = false
				break
			}
			proofs = append(proofs, proof)
		}
		if !possible {
			continue
		}
		if selected != nil && selected.Key != branch.Key {
			return false
		}
		selected, substitution, permissions, evidence = branch, next, nextSources, proofs
	}
	if selected == nil {
		return false
	}
	impl.Case, impl.CaseEvidence = selected, evidence
	impl.Substitution, impl.permissionSubstitution = substitution, permissions
	return true
}
