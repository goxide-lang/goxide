package check

import (
	"go/types"
	"sort"
	"strings"
)

func (c *checker) solveApplication(goal *TraitBound) Evidence {
	if c.coherence == nil {
		c.proofFailure = "trait evidence requested before coherence certification"
		return Evidence{}
	}
	if !c.coherence.certified[goal.Trait] {
		c.proofFailure = "trait implementations have not passed coherence certification: " + goal.ApplicationKey()
		return Evidence{}
	}
	for _, old := range c.proofStack {
		if c.sameApplication(old.goal, goal) {
			c.proofFailure = "cyclic trait prerequisite: " + goal.ApplicationKey()
			return Evidence{}
		}
	}
	if len(c.proofStack) >= 128 {
		c.proofFailure = "trait prerequisite expansion limit exceeded: " + goal.ApplicationKey()
		return Evidence{}
	}
	c.proofStack = append(c.proofStack, proofObligation{goal: goal})
	defer func() { c.proofStack = c.proofStack[:len(c.proofStack)-1] }()
	if evidence := c.positiveApplication(goal); evidence.Impl != nil {
		return evidence
	}
	return c.partitionApplication(goal)
}

func (c *checker) positiveApplication(goal *TraitBound) Evidence {
	var selected *Impl
	for _, template := range c.coherence.order {
		if template.Trait != goal.Trait {
			continue
		}
		if instance := c.matchImplApplication(template, goal); instance != nil {
			if selected != nil {
				c.proofFailure = "multiple implementations satisfy certified application: " + goal.ApplicationKey()
				return Evidence{}
			}
			selected = instance
		}
	}
	return Evidence{Impl: selected}
}

// Split a rigid parameter's complete Go domain, not a list of concrete sample
// types. A ~int leaf still quantifies over every named type with that underlying
// type. Failed coverage never creates deferred runtime search.
func (c *checker) partitionApplication(goal *TraitBound) Evidence {
	if evidence := c.partitionGoApplication(goal); len(evidence.Cases) != 0 {
		return evidence
	}
	return c.partitionSourceApplication(goal)
}

func (c *checker) partitionGoApplication(goal *TraitBound) Evidence {
	s := newCoherenceConstraints(c)
	seen := map[types.Type]bool{}
	var parameters []*types.TypeParam
	var visit func(types.Type)
	visit = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if array, ok := c.out.arrayShape(typ); ok {
			visit(array.Elem)
			return
		}
		if parameter, ok := types.Unalias(typ).(*types.TypeParam); ok {
			parameters = append(parameters, parameter)
			return
		}
		typeChildren(typ, func(child types.Type) bool { visit(child); return false })
	}
	visit(goal.Target)
	for _, input := range goal.Inputs {
		visit(input)
	}
	for _, bound := range c.definitionBounds() {
		visit(bound.Target)
		for _, input := range bound.Inputs {
			visit(input)
		}
		for _, associated := range bound.Trait.Associated {
			visit(bound.Associated[associated.Name])
		}
	}
	if len(parameters) == 0 {
		return Evidence{}
	}
	type domain struct {
		parameter    *types.TypeParam
		constraint   *types.Interface
		alternatives [][]*types.Term
	}
	var domains []domain
	split := false
	for _, parameter := range parameters {
		parts := []types.Type{parameter.Constraint()}
		for _, predicate := range c.activeGoConstraints {
			if types.Identical(predicate.Target, parameter) {
				parts = append(parts, predicate.Constraint)
			}
		}
		constraint := types.NewInterfaceType(nil, parts).Complete()
		alternatives := s.nativeAlternatives(constraint)
		split = split || len(alternatives) > 1
		domains = append(domains, domain{parameter, constraint, alternatives})
	}
	if !split || s.exhausted {
		return Evidence{}
	}
	var cases []EvidenceCase
	complete := true
	var choose func(int, []GoPredicate)
	choose = func(index int, constraints []GoPredicate) {
		if !complete || !s.step() {
			complete = false
			return
		}
		if index < len(domains) {
			domain := domains[index]
			for _, alternative := range domain.alternatives {
				parts := []types.Type{domain.constraint}
				for _, term := range alternative {
					parts = append(parts, types.NewUnion([]*types.Term{term}))
				}
				predicate := GoPredicate{domain.parameter, types.NewInterfaceType(nil, parts).Complete()}
				choose(index+1, append(append([]GoPredicate(nil), constraints...), predicate))
			}
			return
		}
		possible := newCoherenceConstraints(c)
		possible.steps = s.steps
		possible.addParameters(parameters, nil)
		possible.native = append(possible.native, constraints...)
		if len(possible.nativeBranches(c.pkg)) == 0 {
			if *s.steps > coherenceStepLimit {
				complete = false
			}
			return
		}
		old := c.activeGoConstraints
		c.activeGoConstraints = append(append([]GoPredicate(nil), old...), constraints...)
		evidence := c.positiveApplication(goal)
		c.activeGoConstraints = old
		if evidence.Impl == nil {
			complete = false
			return
		}
		impl := evidence.Impl
		cases = append(cases, EvidenceCase{GoConstraints: constraints, Evidence: evidence, Substitution: impl.Substitution, ConstSubstitution: impl.ConstSubstitution, permissionSubstitution: impl.permissionSubstitution})
	}
	choose(0, nil)
	if !complete || len(cases) == 0 {
		if *s.steps > coherenceStepLimit {
			c.proofFailure = "trait partition inference budget exhausted: " + goal.ApplicationKey()
		} else {
			c.proofFailure = "trait conditions do not cover the generic declaration environment: " + goal.ApplicationKey()
		}
		return Evidence{}
	}
	sort.SliceStable(cases, func(i, j int) bool { return evidenceCaseKey(cases[i]) < evidenceCaseKey(cases[j]) })
	return Evidence{Cases: cases}
}

func evidenceCaseKey(branch EvidenceCase) string {
	var conditions []string
	for _, predicate := range branch.GoConstraints {
		conditions = append(conditions, types.TypeString(predicate.Target, nil)+":"+types.TypeString(predicate.Constraint, nil))
	}
	sort.Strings(conditions)
	return branch.Evidence.Key() + strings.Join(conditions, "&")
}

func (c *checker) evidenceConditions(requirements []*TraitBound, predicates []GoPredicate, owner *types.Package) coherenceAnswer {
	s := newCoherenceConstraints(c)
	seen := map[types.Type]bool{}
	var visit func(types.Type)
	visit = func(typ types.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		if array, ok := c.out.arrayShape(typ); ok {
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
	for _, requirement := range requirements {
		visit(requirement.Target)
		for _, input := range requirement.Inputs {
			visit(input)
		}
		for _, output := range requirement.Associated {
			visit(output)
		}
	}
	for _, predicate := range predicates {
		visit(predicate.Target)
		visit(predicate.Constraint)
	}
	s.goals = append(s.goals, requirements...)
	s.native = append(s.native, predicates...)
	return c.solveExists(s, owner, map[string]bool{})
}
