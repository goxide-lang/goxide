package check

import (
	"go/types"
	"sort"
)

type evidenceConditions struct {
	requirements []*TraitBound
	predicates   []GoPredicate
}

func (c *checker) definitionBounds() []*TraitBound {
	result := append([]*TraitBound(nil), c.activeBounds...)
	if c.currentImpl != nil {
		result = append(result, c.currentImpl.Requirements...)
	}
	seen := map[string]bool{}
	unique := result[:0]
	for _, bound := range result {
		key := bound.EvidenceKey()
		if !seen[key] {
			seen[key] = true
			unique = append(unique, bound)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return unique[i].EvidenceKey() < unique[j].EvidenceKey() })
	return unique
}

// A source premise can restrict an otherwise open Go parameter domain. Its
// bodies are a complete disjunction only after every admissible future body is
// excluded. Missing current implementations alone never establish coverage.
func (c *checker) partitionSourceApplication(goal *TraitBound) Evidence {
	var groups [][]evidenceConditions
	budget := newCoherenceConstraints(c)
	for _, bound := range c.definitionBounds() {
		if branches, complete := c.sourceBoundConditions(bound, budget); complete && len(branches) != 0 {
			groups = append(groups, branches)
		}
	}
	if len(groups) == 0 || budget.exhausted {
		return Evidence{}
	}
	oldBounds, oldPredicates, oldState := c.activeBounds, c.activeGoConstraints, c.caseState
	defer func() { c.activeBounds, c.activeGoConstraints, c.caseState = oldBounds, oldPredicates, oldState }()
	// Probing a candidate must not register a body choice until the entire
	// partition has a coverage proof and evidence for every reachable leaf.
	c.caseState = nil
	var cases []EvidenceCase
	var choose func(int, evidenceConditions) bool
	choose = func(index int, conditions evidenceConditions) bool {
		if !budget.step() {
			return false
		}
		c.activeBounds = append(append([]*TraitBound(nil), oldBounds...), conditions.requirements...)
		c.activeGoConstraints = append(append([]GoPredicate(nil), oldPredicates...), conditions.predicates...)
		if evidence := c.positiveApplication(goal); evidence.Impl != nil {
			impl := evidence.Impl
			cases = append(cases, EvidenceCase{Requirements: conditions.requirements, GoConstraints: conditions.predicates, Evidence: evidence, Substitution: impl.Substitution, ConstSubstitution: impl.ConstSubstitution, permissionSubstitution: impl.permissionSubstitution})
			return true
		}
		if index == len(groups) {
			evidence := c.partitionGoApplication(goal)
			for _, branch := range evidence.Cases {
				branch.Requirements = append(append([]*TraitBound(nil), conditions.requirements...), branch.Requirements...)
				branch.GoConstraints = append(append([]GoPredicate(nil), conditions.predicates...), branch.GoConstraints...)
				cases = append(cases, branch)
			}
			return len(evidence.Cases) != 0
		}
		for _, alternative := range groups[index] {
			next := evidenceConditions{
				requirements: append(append([]*TraitBound(nil), conditions.requirements...), alternative.requirements...),
				predicates:   append(append([]GoPredicate(nil), conditions.predicates...), alternative.predicates...),
			}
			requirements := append(append([]*TraitBound(nil), oldBounds...), next.requirements...)
			predicates := append(append([]GoPredicate(nil), oldPredicates...), next.predicates...)
			if answer := c.evidenceConditions(requirements, predicates, c.pkg); answer.result == coherenceImpossible {
				continue
			}
			if !choose(index+1, next) {
				return false
			}
		}
		return true
	}
	if !choose(0, evidenceConditions{}) || len(cases) == 0 {
		return Evidence{}
	}
	unique := cases[:0]
	seen := map[string]bool{}
	for _, branch := range cases {
		key := evidenceCaseKey(branch) + bodyConditionsKey(c.out, branch.Requirements, branch.GoConstraints)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, branch)
		}
	}
	sort.Slice(unique, func(i, j int) bool { return evidenceCaseKey(unique[i]) < evidenceCaseKey(unique[j]) })
	return Evidence{Cases: unique}
}

func (c *checker) sourceBoundConditions(bound *TraitBound, budget *coherenceConstraints) ([]evidenceConditions, bool) {
	if !c.coherence.certified[bound.Trait] {
		return nil, false
	}
	s := newCoherenceConstraints(c)
	s.steps = budget.steps
	bounds := c.definitionBounds()
	for _, premise := range bounds {
		s.addBoundParameters(premise)
		s.facts = append(s.facts, premise)
		if !c.sameApplication(premise, bound) {
			s.goals = append(s.goals, premise)
		}
	}
	s.native = append(s.native, c.activeGoConstraints...)
	for _, future := range s.futureBodies(bound, c.pkg) {
		next := s.clone()
		next.future = append(next.future, future)
		next.addTraitPrerequisites(bound)
		if answer := c.solveExists(next, c.pkg, map[string]bool{}); answer.result != coherenceImpossible {
			return nil, false
		}
	}
	var result []evidenceConditions
	for _, template := range c.coherence.order {
		if template.Trait != bound.Trait {
			continue
		}
		probe := s.clone()
		candidate := probe.freshImpl(template)
		left, right := c.implHeadPermissions(candidate), c.boundHeadPermissions(bound)
		matched := len(left) == len(right)
		for i := 0; matched && i < len(left); i++ {
			matched = probe.unify(left[i], right[i])
		}
		for _, associated := range bound.Trait.Associated {
			name := associated.Name
			if expected := bound.Associated[name]; matched && expected != nil {
				actual := candidate.Associated[name]
				matched = actual != nil && probe.unify(sourceChild(bound.associatedPermissions[name], expected), sourceChild(candidate.associatedPermissions[name], actual))
			}
		}
		if !matched {
			if probe.exhausted {
				return nil, false
			}
			continue
		}
		probe.goals = append(probe.goals, candidate.Requirements...)
		if answer := c.solveExists(probe, c.pkg, map[string]bool{}); answer.result == coherenceImpossible {
			continue
		}
		sub, constants, sources, exact := c.matchApplicationHead(template, bound)
		if !exact {
			// A symbolic head refinement needs a representable branch equality;
			// never silently drop it because rigid matching did not succeed.
			return nil, false
		}
		for _, associated := range bound.Trait.Associated {
			name := associated.Name
			if expected := bound.Associated[name]; expected != nil {
				actual := c.substituteSourcePermission(sourceChild(template.associatedPermissions[name], template.Associated[name]), sub, constants, sources)
				if !c.sameSourceType(sourceChild(bound.associatedPermissions[name], expected), actual) {
					return nil, false
				}
			}
		}
		branch := evidenceConditions{}
		for _, parameter := range template.TypeParams {
			constraint, ok := c.out.Substitute(parameter.Constraint(), sub, constants).Underlying().(*types.Interface)
			if ok && (!constraint.Empty() || constraint.IsComparable()) {
				branch.predicates = append(branch.predicates, GoPredicate{sub[parameter], constraint})
			}
		}
		for _, premise := range template.Requirements {
			branch.requirements = append(branch.requirements, c.substituteBoundApplication(premise, sub, constants, sources))
		}
		result = append(result, branch)
	}
	return result, *s.steps <= coherenceStepLimit
}

func (s *coherenceConstraints) addBoundParameters(bound *TraitBound) {
	seen := map[types.Type]bool{}
	var visit func(types.Type)
	visit = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if array, ok := s.c.out.arrayShape(typ); ok {
			if array.Length.Param != nil {
				s.constants.parameters[array.Length.Param] = true
			}
			visit(array.Elem)
			return
		}
		if parameter, ok := types.Unalias(typ).(*types.TypeParam); ok {
			s.addParameters([]*types.TypeParam{parameter}, nil)
			return
		}
		typeChildren(typ, func(child types.Type) bool { visit(child); return false })
	}
	visit(bound.Target)
	for _, input := range bound.Inputs {
		visit(input)
	}
	for _, associated := range bound.Trait.Associated {
		visit(bound.Associated[associated.Name])
	}
}
