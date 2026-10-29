package check

import (
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type caseFunction struct {
	declaration *ast.FuncDecl
	file        *resolve.File
	pkg         *types.Package
}

// Checking a callee before propagating its cases makes source declaration order
// irrelevant. A recursive call keeps the existing definition-site assumptions.
func (c *checker) ensureFunctionCases(signature *types.Signature) {
	function, known := c.caseFunctions[signature]
	if !known || c.checkedFunctions[function.declaration] != 0 || c.referenceHints == nil {
		return
	}
	file, pkg, impl, trait, self, receiver := c.file, c.pkg, c.currentImpl, c.currentTrait, c.currentSelf, c.currentReceiver
	bounds, predicates := c.activeBounds, c.activeGoConstraints
	c.file, c.pkg = function.file, function.pkg
	c.currentImpl, c.currentTrait, c.currentSelf, c.currentReceiver = nil, nil, nil, nil
	c.activeBounds, c.activeGoConstraints = nil, nil
	state, start := c.caseState, len(c.out.Diagnostics)
	c.checkFunctionCases(function.declaration)
	if state != nil {
		// A caller's discovery pass may discard provisional diagnostics once it
		// finds a partition. The separately checked callee remains authoritative.
		state.checkedDiagnostics = append(state.checkedDiagnostics, c.out.Diagnostics[start:]...)
		c.out.Diagnostics = c.out.Diagnostics[:start]
	}
	c.file, c.pkg, c.currentImpl, c.currentTrait, c.currentSelf, c.currentReceiver = file, pkg, impl, trait, self, receiver
	c.activeBounds, c.activeGoConstraints = bounds, predicates
}

func (c *checker) appliedCaseConditions(requirements []*TraitBound, predicates []GoPredicate, fresh map[*types.TypeParam]types.Type, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) ([]*TraitBound, []GoPredicate) {
	rebound := copySubstitution(sub)
	reboundSources := cloneMap(sources)
	for original, replacement := range fresh {
		if parameter, ok := replacement.(*types.TypeParam); ok {
			rebound[parameter] = c.out.Substitute(original, sub, constants)
			reboundSources[parameter] = c.substituteSourcePermission(c.newPermissionChecker().fromGo(original), sub, constants, sources)
		}
	}
	var result []*TraitBound
	for _, requirement := range requirements {
		result = append(result, c.substituteBoundApplication(requirement, rebound, constants, reboundSources))
	}
	var conditions []GoPredicate
	for _, predicate := range predicates {
		constraint, _ := c.out.Substitute(predicate.Constraint, rebound, constants).Underlying().(*types.Interface)
		conditions = append(conditions, GoPredicate{Target: c.out.Substitute(predicate.Target, rebound, constants), Constraint: constraint})
	}
	return result, conditions
}

func (c *checker) caseConditionsSatisfied(requirements []*TraitBound, predicates []GoPredicate) ([]Evidence, bool) {
	for _, predicate := range predicates {
		if predicate.Constraint == nil || !c.satisfiesGo(predicate.Target, predicate.Constraint) {
			return nil, false
		}
	}
	var evidence []Evidence
	for _, requirement := range requirements {
		witness := c.proveApplication(requirement)
		if witness.Impl == nil && witness.Bound == nil {
			return nil, false
		}
		if !c.applicationOutputsMatch(requirement, witness) {
			return nil, false
		}
		evidence = append(evidence, witness)
	}
	return evidence, true
}

func (c *checker) applicationOutputsMatch(goal *TraitBound, evidence Evidence) bool {
	outputs := evidenceAssociated(evidence)
	for name, wanted := range goal.Associated {
		projected := false
		for _, parameter := range goal.Projected {
			projected = projected || wanted == parameter
		}
		if !projected && !types.Identical(wanted, outputs[name]) {
			return false
		}
		if !projected && goal.associatedPermissions[name] != nil && !c.sameSourceType(goal.associatedPermissions[name], c.evidenceAssociatedPermission(evidence, name)) {
			return false
		}
	}
	return true
}

func (c *checker) implementationBodyEvidence(implementation *Impl) Evidence {
	definition := implementation
	if implementation.Template != nil {
		definition = implementation.Template
	}
	if definition.Definition != nil {
		definition = definition.Definition
	}
	branches := c.out.ImplCases[definition]
	if len(branches) == 0 || implementation.Case != nil {
		return Evidence{Impl: implementation}
	}
	choice := Evidence{}
	for _, branch := range branches {
		requirements, predicates := c.appliedCaseConditions(branch.Requirements, branch.GoConstraints, branch.Substitution, implementation.Substitution, implementation.ConstSubstitution, implementation.permissionSubstitution)
		if proofs, satisfied := c.caseConditionsSatisfied(requirements, predicates); satisfied {
			selected := *implementation
			selected.Case, selected.CaseEvidence = branch, proofs
			selected.MethodCases = map[*ast.FuncDecl]*BodyCase{}
			for declaration, combined := range branch.Methods {
				for _, original := range c.out.BodyCases[declaration] {
					if original.Key == combined.Key {
						selected.MethodCases[declaration] = original
						break
					}
				}
			}
			return Evidence{Impl: &selected}
		}
		choice.Cases = append(choice.Cases, EvidenceCase{Requirements: requirements, GoConstraints: predicates, Evidence: Evidence{Impl: implementation}})
	}
	return choice
}

func (c *checker) functionBodyEvidence(instance *FunctionInstance) {
	function, known := c.caseFunctions[instance.Signature]
	if !known {
		return
	}
	branches := c.out.BodyCases[function.declaration]
	if len(branches) == 0 {
		return
	}
	var alternatives []EvidenceCase
	for _, branch := range branches {
		requirements, predicates := c.appliedCaseConditions(branch.Requirements, branch.GoConstraints, branch.Substitution, instance.Substitution, instance.ConstSubstitution, instance.permissionSubstitution)
		if proofs, satisfied := c.caseConditionsSatisfied(requirements, predicates); satisfied {
			instance.Case, instance.CaseEvidence = branch, proofs
			for original, replacement := range branch.Substitution {
				if parameter, ok := replacement.(*types.TypeParam); ok {
					instance.Substitution[parameter] = c.out.Substitute(original, instance.Substitution, instance.ConstSubstitution)
					source := instance.permissionSubstitution[original]
					if source == nil {
						source = c.newPermissionChecker().fromGo(original)
					}
					instance.permissionSubstitution[parameter] = c.substituteSourcePermission(source, instance.Substitution, instance.ConstSubstitution, instance.permissionSubstitution)
				}
			}
			return
		}
		alternatives = append(alternatives, EvidenceCase{Requirements: requirements, GoConstraints: predicates})
	}
	if c.caseState == nil {
		return
	}
	key := "function:" + c.out.CanonicalPackagePath(function.pkg) + "::" + function.declaration.Name.Name
	var inputs []string
	for index, argument := range instance.TypeArgs {
		var source *permission
		if index < instance.Signature.TypeParams().Len() {
			source = instance.permissionSubstitution[instance.Signature.TypeParams().At(index)]
		}
		inputs = append(inputs, sourceTypeKey(c.out, argument, source))
	}
	for _, constant := range instance.ConstArgs {
		inputs = append(inputs, sourceTypeKey(c.out, c.out.ArrayType(types.Typ[types.Bool], constant), nil))
	}
	key += "[" + strings.Join(inputs, ",") + "]"
	if c.caseState.outputs == nil {
		c.caseState.outputs = map[string]map[string]types.Type{}
	}
	if _, seen := c.caseState.outputs[key]; !seen {
		c.caseState.outputs[key] = nil
		c.caseState.choices = append(c.caseState.choices, bodyChoice{evidence: Evidence{Cases: alternatives}})
	}
}
