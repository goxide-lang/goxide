package check

import (
	"crypto/sha256"
	"fmt"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type coherencePair struct{ left, right *Impl }
type coherenceCertificate struct{ owner *types.Package }

type coherenceState struct {
	certified    map[*Trait]bool
	certificates map[coherencePair]coherenceCertificate
	dependencies map[*types.Package]map[*types.Package]bool
	order        []*Impl
	fresh        int
}

func (c *checker) certifyImpls() bool {
	r := &coherenceState{certified: map[*Trait]bool{}, certificates: map[coherencePair]coherenceCertificate{}, dependencies: c.coherenceDependencies()}
	c.coherence = r
	var pairs []coherencePair
	refresh := func() {
		r.order = nil
		r.certified = map[*Trait]bool{}
		r.certificates = map[coherencePair]coherenceCertificate{}
		r.dependencies = c.coherenceDependencies()
		for _, impl := range c.implOrder {
			impl.DeclID = c.implDeclID(impl)
			if impl.Trait != nil {
				r.order = append(r.order, impl)
			}
		}
		sort.SliceStable(r.order, func(i, j int) bool {
			a, b := r.order[i], r.order[j]
			if a.DeclID != b.DeclID {
				return a.DeclID < b.DeclID
			}
			if a.File != b.File {
				return a.File < b.File
			}
			return a.Decl.Span().Start < b.Decl.Span().Start
		})
		pairs = nil
		for index, right := range r.order {
			for _, left := range r.order[:index] {
				if left.Trait == right.Trait {
					pairs = append(pairs, coherencePair{left, right})
				}
			}
		}
	}
	refresh()
	var traits []*Trait
	c.eachTrait(func(trait *Trait) { traits = append(traits, trait) })
	answers := map[coherencePair]coherenceAnswer{}
	for {
		progress := false
		for _, pair := range pairs {
			if _, done := r.certificates[pair]; done {
				continue
			}
			answer, owner := c.intersectImpls(pair.left, pair.right)
			answers[pair] = answer
			if answer.result == coherenceImpossible {
				r.certificates[pair] = coherenceCertificate{owner}
				progress = true
			}
		}
		for _, trait := range traits {
			if r.certified[trait] {
				continue
			}
			complete := !c.pendingTrait(trait)
			for _, pair := range pairs {
				if pair.left.Trait == trait {
					_, done := r.certificates[pair]
					complete = complete && done
				}
			}
			if complete {
				r.certified[trait], progress = true, true
			}
		}
		if c.completeCertifiedSignatures() {
			refresh()
			progress = true
		}
		if !progress {
			break
		}
	}
	file, pkg := c.file, c.pkg
	defer func() { c.file, c.pkg = file, pkg }()
	complete := c.diagnosePendingDeclarations()
	for _, pair := range pairs {
		if _, done := r.certificates[pair]; done {
			continue
		}
		answer := answers[pair]
		complete = false
		message := "duplicate trait implementation: overlapping applications"
		if answer.result == coherenceUnknown {
			message = "cannot prove trait implementations disjoint: " + answer.reason
		} else if answer.reason != "" {
			message += ": " + answer.reason
		}
		message += fmt.Sprintf(" (other declaration: %s:%d)", pair.left.File, pair.left.Decl.Span().Start)
		c.file, c.pkg = c.out.Resolve.Files[pair.right.File], pair.right.Owner
		c.diag(pair.right.Decl, TraitInvalid, message)
	}
	return complete
}

func (c *checker) implDeclID(impl *Impl) string {
	sub := map[*types.TypeParam]types.Type{}
	for index, parameter := range impl.TypeParams {
		sub[parameter] = types.NewTypeParam(types.NewTypeName(0, nil, "$p"+strconv.Itoa(index), nil), anyType)
	}
	constants := map[*ConstParam]ConstValue{}
	for index, parameter := range impl.ConstParams {
		copy := *parameter
		copy.Name = &ast.Ident{Name: "$c" + strconv.Itoa(index)}
		constants[parameter] = ConstValue{Param: &copy}
	}
	for _, requirement := range impl.Requirements {
		application := c.substituteBoundApplication(requirement, sub, constants, nil).ApplicationKey()
		for _, associated := range requirement.Trait.Associated {
			if parameter := projectedBinding(requirement, associated.Name); parameter != nil {
				sub[parameter] = types.NewTypeParam(types.NewTypeName(0, nil, "$projection("+application+")."+associated.Name, nil), anyType)
			}
		}
	}
	head := c.substituteBoundApplication(c.implApplication(impl), sub, constants, nil).ApplicationKey()
	var predicates []string
	for _, parameter := range impl.TypeParams {
		predicates = append(predicates, sourceTypeKey(c.out, sub[parameter], nil)+":"+sourceTypeKey(c.out, c.out.Substitute(parameter.Constraint(), sub, constants), nil))
	}
	for _, requirement := range impl.Requirements {
		predicates = append(predicates, c.substituteBoundApplication(requirement, sub, constants, nil).EvidenceKey())
	}
	sort.Strings(predicates)
	owner := c.out.CanonicalPackagePath(impl.Owner)
	digest := sha256.Sum256([]byte(owner + "|" + head + "|" + strings.Join(predicates, "&")))
	return fmt.Sprintf("%x", digest[:12])
}

func (c *checker) intersectImpls(left, right *Impl) (coherenceAnswer, *types.Package) {
	var result coherenceAnswer
	for index, owner := range []*types.Package{left.Owner, right.Owner} {
		if index != 0 && owner == left.Owner {
			continue
		}
		s := newCoherenceConstraints(c)
		a, b := s.freshImpl(left), s.freshImpl(right)
		x, y := c.implHeadPermissions(a), c.implHeadPermissions(b)
		matched := len(x) == len(y)
		for i := 0; matched && i < len(x); i++ {
			matched = s.unify(x[i], y[i])
		}
		if !matched {
			answer := s.failed("different implementation heads")
			if answer.result == coherenceImpossible {
				return answer, owner
			}
			result = answer
			continue
		}
		s.goals = append(s.goals, a.Requirements...)
		s.goals = append(s.goals, b.Requirements...)
		answer := c.solveExists(s, owner, map[string]bool{})
		if answer.result == coherenceImpossible {
			return answer, owner
		}
		if index == 0 || answer.result == coherencePossible {
			result = answer
		}
	}
	return result, nil
}

func (s *coherenceConstraints) freshImpl(impl *Impl) *Impl {
	s.c.coherence.fresh++
	serial := s.c.coherence.fresh
	sub := map[*types.TypeParam]types.Type{}
	var parameters []*types.TypeParam
	add := func(parameter *types.TypeParam) {
		if sub[parameter] != nil {
			return
		}
		fresh := types.NewTypeParam(types.NewTypeName(0, parameter.Obj().Pkg(), fmt.Sprintf("%s$%d", parameter.Obj().Name(), serial), nil), anyType)
		sub[parameter] = fresh
		parameters = append(parameters, fresh)
	}
	for _, parameter := range impl.TypeParams {
		add(parameter)
	}
	for _, requirement := range impl.Requirements {
		for _, parameter := range requirement.Projected {
			add(parameter)
		}
	}
	constants := map[*ConstParam]ConstValue{}
	var constParams []*ConstParam
	for _, parameter := range impl.ConstParams {
		fresh := *parameter
		name := *parameter.Name
		name.Name += "$" + strconv.Itoa(serial)
		fresh.Name = &name
		constants[parameter] = ConstValue{Param: &fresh}
		constParams = append(constParams, &fresh)
	}
	for original, fresh := range sub {
		fresh.(*types.TypeParam).SetConstraint(s.c.out.Substitute(original.Constraint(), sub, constants))
	}
	s.addParameters(parameters, constParams)
	copy := *impl
	copy.Target = s.c.out.Substitute(impl.Target, sub, constants)
	copy.targetPermission = s.c.substituteSourcePermission(s.c.implHeadPermissions(impl)[0], sub, constants, nil)
	copy.Inputs, copy.inputPermissions = nil, nil
	for i, input := range impl.Inputs {
		copy.Inputs = append(copy.Inputs, s.c.out.Substitute(input, sub, constants))
		copy.inputPermissions = append(copy.inputPermissions, s.c.substituteSourcePermission(s.c.implHeadPermissions(impl)[i+1], sub, constants, nil))
	}
	copy.TypeParams, copy.ConstParams = parameters, constParams
	copy.Requirements = nil
	for _, requirement := range impl.Requirements {
		copy.Requirements = append(copy.Requirements, s.c.substituteBoundApplication(requirement, sub, constants, nil))
	}
	copy.Associated, copy.associatedPermissions = map[string]types.Type{}, map[string]*permission{}
	for name, output := range impl.Associated {
		copy.Associated[name] = s.c.out.Substitute(output, sub, constants)
		copy.associatedPermissions[name] = s.c.substituteSourcePermission(sourceChild(impl.associatedPermissions[name], output), sub, constants, nil)
	}
	return &copy
}

func (s *coherenceConstraints) bound(bound *TraitBound) *TraitBound {
	return s.c.substituteBoundApplication(bound, s.types(), s.constants.bindings, s.bindings)
}

func (s *coherenceConstraints) functionalDependencies() bool {
	all := append(append([]*TraitBound(nil), s.facts...), s.goals...)
	for i, left := range all {
		for _, right := range all[:i] {
			if !s.step() {
				return false
			}
			a, b := s.bound(left), s.bound(right)
			if !s.c.sameApplication(a, b) {
				continue
			}
			for _, associated := range a.Trait.Associated {
				name := associated.Name
				x, y := a.Associated[name], b.Associated[name]
				if x == nil || y == nil {
					continue
				}
				if s.c.coherence.certified[left.Trait] {
					if !s.unify(sourceChild(a.associatedPermissions[name], x), sourceChild(b.associatedPermissions[name], y)) {
						return false
					}
				} else if probe := s.clone(); !probe.unify(sourceChild(a.associatedPermissions[name], x), sourceChild(b.associatedPermissions[name], y)) {
					s.uncertain, s.uncertainty = true, "CircularCoherenceDependency: associated output uniqueness is not independently certified"
				}
			}
		}
	}
	return true
}

func (c *checker) solveExists(s *coherenceConstraints, owner *types.Package, active map[string]bool) coherenceAnswer {
	if !s.step() || !s.functionalDependencies() || !s.futureValid() {
		return s.failed("inconsistent shared constraints")
	}
	branches := s.nativeBranches(owner)
	if len(branches) == 0 {
		return s.failed("incompatible native type constraints")
	}
	result := coherenceAnswer{coherenceImpossible, "no admissible implementation", s}
	for _, branch := range branches {
		if !branch.functionalDependencies() || !branch.futureValid() {
			continue
		}
		answer := c.solveGoals(branch, owner, active)
		if answer.result == coherencePossible {
			return answer
		}
		if answer.result == coherenceUnknown {
			result = answer
		}
	}
	if *s.steps > coherenceStepLimit {
		return coherenceAnswer{coherenceUnknown, "coherence inference budget exhausted", s}
	}
	return result
}

func (c *checker) solveGoals(s *coherenceConstraints, owner *types.Package, active map[string]bool) coherenceAnswer {
	if len(s.goals) == 0 {
		if s.uncertain {
			return coherenceAnswer{coherenceUnknown, s.uncertainty, s}
		}
		return coherenceAnswer{coherencePossible, "consistent shared type and prerequisite constraints", s}
	}
	if c.pendingTrait(s.goals[0].Trait) {
		return coherenceAnswer{coherenceUnknown, "trait declaration evidence is pending", s}
	}
	key := s.queryKey()
	if active[key] {
		if s.uncertain {
			return coherenceAnswer{coherenceUnknown, s.uncertainty, s}
		}
		return coherenceAnswer{coherenceImpossible, "closed inductive prerequisite cycle has no base", s}
	}
	active[key] = true
	defer delete(active, key)
	goal := s.bound(s.goals[0])
	remaining := s.goals[1:]
	result := coherenceAnswer{coherenceImpossible, "no implementation for " + goal.ApplicationKey(), s}
	candidates := append([]*Impl(nil), c.coherence.order...)
	sort.SliceStable(candidates, func(i, j int) bool { return len(candidates[i].Requirements) < len(candidates[j].Requirements) })
	for _, impl := range candidates {
		if impl.Trait != goal.Trait {
			continue
		}
		next := s.clone()
		candidate := next.freshImpl(impl)
		x, y := c.implHeadPermissions(candidate), c.boundHeadPermissions(goal)
		matched := len(x) == len(y)
		for i := 0; matched && i < len(x); i++ {
			matched = next.unify(x[i], y[i])
		}
		for _, associated := range goal.Trait.Associated {
			name := associated.Name
			if expected := goal.Associated[name]; matched && expected != nil {
				actual := candidate.Associated[name]
				matched = actual != nil && next.unify(sourceChild(goal.associatedPermissions[name], expected), sourceChild(candidate.associatedPermissions[name], actual))
			}
		}
		if !matched {
			continue
		}
		next.goals = append(append([]*TraitBound(nil), remaining...), candidate.Requirements...)
		next.facts = append(next.facts, goal)
		answer := c.solveExists(next, owner, active)
		if answer.result == coherencePossible {
			return answer
		}
		if answer.result == coherenceUnknown {
			result = answer
		}
	}
	for _, future := range s.futureBodies(goal, owner) {
		next := s.clone()
		next.goals = append([]*TraitBound(nil), remaining...)
		next.future = append(next.future, future)
		next.facts = append(next.facts, goal)
		next.addTraitPrerequisites(goal)
		answer := c.solveExists(next, owner, active)
		if answer.result == coherencePossible {
			return answer
		}
		if answer.result == coherenceUnknown {
			result = answer
		}
	}
	if *s.steps > coherenceStepLimit {
		return coherenceAnswer{coherenceUnknown, "coherence inference budget exhausted at " + goal.ApplicationKey(), s}
	}
	return result
}

func (s *coherenceConstraints) addTraitPrerequisites(goal *TraitBound) {
	sub := map[*types.TypeParam]types.Type{goal.Trait.Self: goal.Target}
	permissions := map[*types.TypeParam]*permission{goal.Trait.Self: s.c.boundHeadPermissions(goal)[0]}
	for index, parameter := range goal.Trait.TypeParams {
		if index >= len(goal.Inputs) {
			continue
		}
		sub[parameter] = goal.Inputs[index]
		permissions[parameter] = s.c.boundHeadPermissions(goal)[index+1]
	}
	for parameter, target := range sub {
		if constraint, ok := s.c.out.Substitute(parameter.Constraint(), sub, nil).Underlying().(*types.Interface); ok && (!constraint.Empty() || constraint.IsComparable()) {
			s.native = append(s.native, GoPredicate{target, constraint})
		}
	}
	for _, requirement := range goal.Trait.Requirements {
		s.goals = append(s.goals, s.c.substituteBoundApplication(requirement, sub, nil, permissions))
	}
}

// Fresh logical variables are numbered by first structural occurrence. Query
// identity includes the shared residual store, not just the selected trait.
func (s *coherenceConstraints) queryKey() string {
	sub := map[*types.TypeParam]types.Type{}
	constants := map[*ConstParam]ConstValue{}
	var visit func(types.Type)
	seen := map[types.Type]bool{}
	visit = func(typ types.Type) {
		if typ == nil || seen[typ] || !s.step() {
			return
		}
		seen[typ] = true
		if array, ok := s.c.out.arrayShape(typ); ok {
			if p := resolveConst(array.Length, s.constants.bindings).Param; p != nil {
				if _, found := constants[p]; !found {
					fresh := *p
					fresh.Name = &ast.Ident{Name: "$c" + strconv.Itoa(len(constants))}
					constants[p] = ConstValue{Param: &fresh}
				}
			}
			visit(array.Elem)
			return
		}
		if parameter, ok := types.Unalias(typ).(*types.TypeParam); ok && s.variables[parameter] {
			if sub[parameter] == nil {
				sub[parameter] = types.NewTypeParam(types.NewTypeName(0, nil, "$v"+strconv.Itoa(len(sub)), nil), anyType)
			}
			return
		}
		typeChildren(typ, func(child types.Type) bool { visit(child); return false })
	}
	var goals []*TraitBound
	for _, goal := range append(append([]*TraitBound(nil), s.goals...), s.relevantFacts()...) {
		goal = s.bound(goal)
		goals = append(goals, goal)
		visit(goal.Target)
		for _, input := range goal.Inputs {
			visit(input)
		}
		for _, associated := range goal.Trait.Associated {
			visit(goal.Associated[associated.Name])
		}
	}
	var native []GoPredicate
	for _, predicate := range s.native {
		actual := GoPredicate{Target: s.c.out.Substitute(predicate.Target, s.types(), s.constants.bindings), Constraint: s.c.out.Substitute(predicate.Constraint, s.types(), s.constants.bindings).Underlying().(*types.Interface)}
		native = append(native, actual)
		visit(actual.Target)
		visit(actual.Constraint)
	}
	parts := map[string]bool{}
	for _, goal := range goals {
		parts[s.c.substituteBoundApplication(goal, sub, constants, nil).EvidenceKey()] = true
	}
	for _, predicate := range native {
		parts[sourceTypeKey(s.c.out, s.c.out.Substitute(predicate.Target, sub, constants), nil)+":"+sourceTypeKey(s.c.out, s.c.out.Substitute(predicate.Constraint, sub, constants), nil)] = true
	}
	for _, future := range s.future {
		owner := "$fresh"
		if future.owner != nil {
			owner = future.owner.Path()
		}
		parts["future:"+owner+":"+strconv.Itoa(future.newRoot)+":"+s.c.substituteBoundApplication(s.bound(future.goal), sub, constants, nil).EvidenceKey()] = true
	}
	var keys []string
	for key := range parts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, " & ")
}

func (s *coherenceConstraints) relevantFacts() []*TraitBound {
	var result []*TraitBound
	for _, fact := range s.facts {
		for _, goal := range s.goals {
			if fact.Trait != goal.Trait {
				continue
			}
			probe := s.clone()
			left, right := s.c.boundHeadPermissions(fact), s.c.boundHeadPermissions(goal)
			same := len(left) == len(right)
			for i := 0; same && i < len(left); i++ {
				same = probe.unify(left[i], right[i])
			}
			if same {
				result = append(result, fact)
				break
			}
		}
	}
	return result
}
