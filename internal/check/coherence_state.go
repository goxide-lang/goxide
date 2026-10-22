package check

import (
	"go/types"
	"maps"
	"slices"
)

type coherenceResult uint8

const (
	coherenceImpossible coherenceResult = iota
	coherencePossible
	coherenceUnknown
)

type coherenceAnswer struct {
	result coherenceResult
	reason string
	state  *coherenceConstraints
}

// Each alternative owns its substitutions. Conjuncts always share one store.
type coherenceConstraints struct {
	c           *checker
	variables   map[*types.TypeParam]bool
	bindings    map[*types.TypeParam]*permission
	constants   *implPatternContext
	native      []GoPredicate
	goals       []*TraitBound
	facts       []*TraitBound
	future      []coherenceFutureBody
	steps       *int
	exhausted   bool
	uncertain   bool
	uncertainty string
	closedTypes map[*types.TypeParam]types.Type
	closedCount int
	closedConst int
}

const coherenceStepLimit = 32768

func newCoherenceConstraints(c *checker) *coherenceConstraints {
	steps := 0
	return &coherenceConstraints{c: c, variables: map[*types.TypeParam]bool{}, bindings: map[*types.TypeParam]*permission{}, constants: newImplPatternContext(c.out), steps: &steps}
}

func (s *coherenceConstraints) clone() *coherenceConstraints {
	next := *s
	next.variables = maps.Clone(s.variables)
	next.bindings = maps.Clone(s.bindings)
	next.native = slices.Clone(s.native)
	next.goals = slices.Clone(s.goals)
	next.facts = slices.Clone(s.facts)
	next.future = slices.Clone(s.future)
	constants := *s.constants
	constants.parameters, constants.bindings = maps.Clone(s.constants.parameters), maps.Clone(s.constants.bindings)
	next.constants = &constants
	return &next
}

func (s *coherenceConstraints) step() bool {
	*s.steps++
	if *s.steps > coherenceStepLimit {
		s.exhausted = true
		return false
	}
	return true
}

func (s *coherenceConstraints) source(typ types.Type) *permission {
	return s.c.newPermissionChecker().fromGo(typ)
}

func (s *coherenceConstraints) variable(q *permission) *types.TypeParam {
	if q == nil || q.ref != ValueRef {
		return nil
	}
	if _, array := s.c.out.arrayShape(q.typ); array {
		return nil
	}
	parameter, _ := types.Unalias(q.typ).(*types.TypeParam)
	if s.variables[parameter] {
		return parameter
	}
	return nil
}

func (s *coherenceConstraints) resolve(q *permission) *permission {
	for {
		parameter := s.variable(q)
		if parameter == nil || s.bindings[parameter] == nil {
			return q
		}
		q = s.bindings[parameter]
	}
}

func (s *coherenceConstraints) occurs(parameter *types.TypeParam, q *permission) bool {
	seen := map[types.Type]bool{}
	var visit func(*permission) bool
	visit = func(q *permission) bool {
		if !s.step() {
			return false
		}
		q = s.resolve(q)
		if q == nil || seen[q.typ] {
			return false
		}
		if types.Unalias(q.typ) == parameter {
			return true
		}
		seen[q.typ] = true
		if array, ok := s.c.out.arrayShape(q.typ); ok {
			return visit(sourceChild(q.elem, array.Elem))
		}
		return typeChildren(q.typ, func(child types.Type) bool { return visit(s.source(child)) })
	}
	return visit(q)
}

func sourceChild(q *permission, typ types.Type) *permission {
	if q != nil {
		return q
	}
	return &permission{typ: typ}
}

func sourceAt(list []*permission, index int, typ types.Type) *permission {
	if index < len(list) {
		return sourceChild(list[index], typ)
	}
	return &permission{typ: typ}
}

func (s *coherenceConstraints) unify(a, b *permission) bool {
	if !s.step() {
		return false
	}
	a, b = s.resolve(a), s.resolve(b)
	if a == nil || b == nil {
		s.uncertain, s.uncertainty = true, "unresolved source type"
		return true
	}
	// Bind a naked parameter before inspecting the reference constructor. T can
	// denote &U, and the substitution must retain that whole source type.
	if variable := s.variable(a); variable != nil {
		if variable == s.variable(b) {
			return true
		}
		if s.occurs(variable, b) || s.exhausted {
			return false
		}
		s.bindings[variable] = b
		return true
	}
	if s.variable(b) != nil {
		return s.unify(b, a)
	}
	if a.ref != b.ref {
		return false
	}
	left, right := types.Unalias(a.typ), types.Unalias(b.typ)
	if array, ok := s.c.out.arrayShape(left); ok {
		other, ok := s.c.out.arrayShape(right)
		return ok && s.constants.unify(array.Length, other.Length) && s.unify(sourceChild(a.elem, array.Elem), sourceChild(b.elem, other.Elem))
	}
	if _, ok := s.c.out.arrayShape(right); ok {
		return false
	}
	switch left := left.(type) {
	case *types.Named:
		right, ok := right.(*types.Named)
		if !ok || left.Origin() != right.Origin() || left.TypeArgs().Len() != right.TypeArgs().Len() {
			return false
		}
		for i := 0; i < left.TypeArgs().Len(); i++ {
			if !s.unify(sourceAt(a.args, i, left.TypeArgs().At(i)), sourceAt(b.args, i, right.TypeArgs().At(i))) {
				return false
			}
		}
		return true
	case *types.Pointer:
		right, ok := right.(*types.Pointer)
		return ok && s.unify(sourceChild(a.elem, left.Elem()), sourceChild(b.elem, right.Elem()))
	case *types.Slice:
		right, ok := right.(*types.Slice)
		return ok && s.unify(sourceChild(a.elem, left.Elem()), sourceChild(b.elem, right.Elem()))
	case *types.Map:
		right, ok := right.(*types.Map)
		return ok && s.unify(sourceChild(a.key, left.Key()), sourceChild(b.key, right.Key())) && s.unify(sourceChild(a.elem, left.Elem()), sourceChild(b.elem, right.Elem()))
	case *types.Chan:
		right, ok := right.(*types.Chan)
		return ok && left.Dir() == right.Dir() && s.unify(sourceChild(a.elem, left.Elem()), sourceChild(b.elem, right.Elem()))
	case *types.Struct:
		right, ok := right.(*types.Struct)
		if !ok || left.NumFields() != right.NumFields() {
			return false
		}
		for i := 0; i < left.NumFields(); i++ {
			x, y := left.Field(i), right.Field(i)
			if x.Id() != y.Id() || x.Embedded() != y.Embedded() || left.Tag(i) != right.Tag(i) || !s.unify(sourceChild(a.fields[x.Name()], x.Type()), sourceChild(b.fields[y.Name()], y.Type())) {
				return false
			}
		}
		return true
	case *types.Tuple:
		right, ok := right.(*types.Tuple)
		return ok && s.unifyTuple(left, right, a.results, b.results)
	case *types.Signature:
		right, ok := right.(*types.Signature)
		if !ok || left.Variadic() != right.Variadic() {
			return false
		}
		if left.TypeParams().Len() != 0 || right.TypeParams().Len() != 0 {
			if types.Identical(left, right) {
				return true
			}
			s.uncertain, s.uncertainty = true, "higher-rank native signature"
			return true
		}
		return s.unifyTuple(left.Params(), right.Params(), a.params, b.params) && s.unifyTuple(left.Results(), right.Results(), a.results, b.results)
	case *types.Interface:
		right, ok := right.(*types.Interface)
		if !ok {
			return false
		}
		left.Complete()
		right.Complete()
		if !left.IsMethodSet() || !right.IsMethodSet() {
			if types.Identical(left, right) {
				return true
			}
			s.uncertain, s.uncertainty = true, "symbolic interface identity"
			return true
		}
		if left.NumMethods() != right.NumMethods() {
			return false
		}
		for i := 0; i < left.NumMethods(); i++ {
			x, y := left.Method(i), right.Method(i)
			if x.Id() != y.Id() || !s.unify(sourceChild(a.fields[x.Id()], x.Type()), sourceChild(b.fields[y.Id()], y.Type())) {
				return false
			}
		}
		return true
	default:
		return types.Identical(left, right)
	}
}

func (s *coherenceConstraints) unifyTuple(a, b *types.Tuple, ap, bp []*permission) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := 0; i < a.Len(); i++ {
		if !s.unify(sourceAt(ap, i, a.At(i).Type()), sourceAt(bp, i, b.At(i).Type())) {
			return false
		}
	}
	return true
}

func (s *coherenceConstraints) types() map[*types.TypeParam]types.Type {
	if s.closedTypes != nil && s.closedCount == len(s.bindings) && s.closedConst == len(s.constants.bindings) {
		return s.closedTypes
	}
	result := make(map[*types.TypeParam]types.Type, len(s.bindings))
	var closed func(*types.TypeParam) types.Type
	closed = func(parameter *types.TypeParam) types.Type {
		if result[parameter] != nil {
			return result[parameter]
		}
		bound := s.bindings[parameter]
		if bound == nil {
			return parameter
		}
		sub := map[*types.TypeParam]types.Type{}
		seen := map[types.Type]bool{}
		var visit func(types.Type)
		visit = func(typ types.Type) {
			if seen[typ] || !s.step() {
				return
			}
			seen[typ] = true
			if array, ok := s.c.out.arrayShape(typ); ok {
				visit(array.Elem)
				return
			}
			if p, ok := types.Unalias(typ).(*types.TypeParam); ok {
				if s.bindings[p] != nil {
					sub[p] = closed(p)
				}
				return
			}
			typeChildren(typ, func(child types.Type) bool { visit(child); return false })
		}
		visit(bound.typ)
		result[parameter] = s.c.out.Substitute(bound.typ, sub, s.constants.bindings)
		return result[parameter]
	}
	for parameter := range s.bindings {
		closed(parameter)
	}
	s.closedTypes, s.closedCount, s.closedConst = result, len(s.bindings), len(s.constants.bindings)
	return result
}

func (s *coherenceConstraints) applied(q *permission) *permission {
	return s.c.substituteSourcePermission(q, s.types(), s.constants.bindings, s.bindings)
}

func (s *coherenceConstraints) addParameters(parameters []*types.TypeParam, constants []*ConstParam) {
	for _, parameter := range parameters {
		s.variables[parameter] = true
		if constraint, ok := parameter.Constraint().Underlying().(*types.Interface); ok && (!constraint.Empty() || constraint.IsComparable()) {
			s.native = append(s.native, GoPredicate{Target: parameter, Constraint: constraint})
		}
	}
	for _, parameter := range constants {
		s.constants.parameters[parameter] = true
	}
}

func (s *coherenceConstraints) failed(reason string) coherenceAnswer {
	if s.exhausted || *s.steps > coherenceStepLimit {
		return coherenceAnswer{coherenceUnknown, "coherence inference budget exhausted", s}
	}
	return coherenceAnswer{coherenceImpossible, reason, s}
}
