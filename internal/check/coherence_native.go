package check

import (
	"go/types"
)

type coherenceTypeTerm struct {
	target types.Type
	typ    types.Type
	tilde  bool
}

// nil terms means the universe; an empty slice means the empty set. Intersections
// remain conjuncts instead of prematurely choosing a representative named type.
func (s *coherenceConstraints) nativeAlternatives(typ types.Type) [][]*types.Term {
	if !s.step() {
		return nil
	}
	switch t := types.Unalias(typ).(type) {
	case *types.Named:
		if s.c.pendingUnderlying(t) {
			s.uncertain, s.uncertainty = true, "native underlying type awaits certified projection"
			return [][]*types.Term{nil}
		}
		if _, ok := t.Underlying().(*types.Interface); ok {
			return s.nativeAlternatives(t.Underlying())
		}
		return [][]*types.Term{{types.NewTerm(false, t)}}
	case *types.Interface:
		result := [][]*types.Term{nil}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			parts := s.nativeAlternatives(t.EmbeddedType(i))
			var next [][]*types.Term
			for _, left := range result {
				for _, right := range parts {
					if !s.step() {
						return nil
					}
					next = append(next, append(append([]*types.Term(nil), left...), right...))
				}
			}
			result = next
		}
		return result
	case *types.Union:
		var result [][]*types.Term
		for i := 0; i < t.Len(); i++ {
			term := t.Term(i)
			if !term.Tilde() {
				if _, ok := term.Type().Underlying().(*types.Interface); ok {
					result = append(result, s.nativeAlternatives(term.Type())...)
					continue
				}
			}
			result = append(result, []*types.Term{term})
		}
		return result
	case *types.TypeParam:
		return s.nativeAlternatives(t.Constraint())
	default:
		return [][]*types.Term{{types.NewTerm(false, t)}}
	}
}

func (s *coherenceConstraints) nativeBranches(owner *types.Package) []*coherenceConstraints {
	var results []*coherenceConstraints
	var visit func(*coherenceConstraints, int, []coherenceTypeTerm)
	visit = func(branch *coherenceConstraints, index int, terms []coherenceTypeTerm) {
		if !branch.step() {
			return
		}
		if index == len(branch.native) {
			if branch.checkNative(owner, terms) {
				results = append(results, branch)
			}
			return
		}
		predicate := branch.native[index]
		for _, alternative := range branch.nativeAlternatives(predicate.Constraint) {
			next := branch.clone()
			conjuncts := append([]coherenceTypeTerm(nil), terms...)
			for _, term := range alternative {
				conjuncts = append(conjuncts, coherenceTypeTerm{predicate.Target, term.Type(), term.Tilde()})
			}
			visit(next, index+1, conjuncts)
		}
	}
	visit(s, 0, nil)
	return results
}

func (s *coherenceConstraints) checkNative(owner *types.Package, terms []coherenceTypeTerm) bool {
	for {
		before := len(s.bindings) + len(s.constants.bindings)
		for _, term := range terms {
			if !term.tilde && !s.unify(s.source(term.target), s.source(term.typ)) {
				return false
			}
		}
		for i, term := range terms {
			if !term.tilde {
				continue
			}
			sub := s.types()
			target := s.c.out.Substitute(term.target, sub, s.constants.bindings)
			underlying := s.c.out.Substitute(term.typ, sub, s.constants.bindings)
			if s.c.pendingUnderlying(target) || s.c.pendingUnderlying(underlying) {
				s.uncertain, s.uncertainty = true, "native underlying type awaits certified projection"
				continue
			}
			if _, variable := types.Unalias(target).(*types.TypeParam); !variable {
				if !s.unify(s.source(target.Underlying()), s.source(underlying)) {
					return false
				}
			}
			for _, previous := range terms[:i] {
				other := s.c.out.Substitute(previous.target, sub, s.constants.bindings)
				if !types.Identical(target, other) {
					continue
				}
				typ := s.c.out.Substitute(previous.typ, sub, s.constants.bindings)
				if !previous.tilde {
					typ = typ.Underlying()
				}
				if !s.unify(s.source(underlying), s.source(typ)) {
					return false
				}
			}
		}
		if !s.nativeMethods(owner, terms) {
			return false
		}
		if before == len(s.bindings)+len(s.constants.bindings) {
			return true
		}
	}
}

func (s *coherenceConstraints) nativeMethods(owner *types.Package, terms []coherenceTypeTerm) bool {
	view := newGoInterop(s.c)
	type requirement struct {
		target types.Type
		method *types.Func
	}
	var required []requirement
	for _, predicate := range s.native {
		if !s.step() {
			return false
		}
		sub := s.types()
		target := s.c.out.Substitute(predicate.Target, sub, s.constants.bindings)
		constraint := s.c.out.Substitute(predicate.Constraint, sub, s.constants.bindings).Underlying().(*types.Interface)
		if constraint.IsComparable() {
			if !s.possiblyComparable(target, terms, map[types.Type]bool{}) {
				return false
			}
		}
		for i := 0; i < constraint.NumMethods(); i++ {
			method := constraint.Method(i)
			for _, old := range required {
				if types.Identical(target, old.target) && method.Id() == old.method.Id() && !s.unify(s.source(method.Type()), s.source(old.method.Type())) {
					return false
				}
			}
			required = append(required, requirement{target, method})
			if _, variable := types.Unalias(target).(*types.TypeParam); variable {
				continue
			}
			if s.c.pendingNativeMethod(target, method.Name()) {
				s.uncertain, s.uncertainty = true, "native method signature awaits certified projection: "+method.Name()
				continue
			}
			set := types.NewMethodSet(view.typ(target, true))
			selection := set.Lookup(method.Pkg(), method.Name())
			if selection != nil {
				actual := view.typ(selection.Obj().Type(), false)
				test := s.clone()
				if !test.unify(test.source(actual), test.source(method.Type())) {
					// A directly declared method cannot be repaired by adding
					// another method of the same name. Promoted paths can change.
					if len(selection.Index()) == 1 || !s.externalPromotion(target, owner, map[types.Type]bool{}) {
						return false
					}
					s.uncertain, s.uncertainty = true, "external promoted method may change"
				} else {
					*s = *test
				}
				continue
			}
			root := ownerRoot(s.source(target))
			if root == nil {
				if s.externalPromotion(target, owner, map[types.Type]bool{}) {
					s.uncertain, s.uncertainty = true, "external embedded type may gain "+method.Name()
					continue
				}
				// An unnamed fixed type cannot acquire methods in another package.
				return false
			}
			if root.Obj().Pkg() == owner && !s.externalPromotion(target, owner, map[types.Type]bool{}) && !s.potentialSugar(root, method.Name()) {
				return false
			}
			s.uncertain, s.uncertainty = true, "native method set may gain "+method.Name()
		}
	}
	return true
}

func (s *coherenceConstraints) possiblyComparable(typ types.Type, terms []coherenceTypeTerm, seen map[types.Type]bool) bool {
	if seen[typ] {
		return true
	}
	seen[typ] = true
	if s.c.pendingUnderlying(typ) {
		s.uncertain, s.uncertainty = true, "native comparability awaits certified projection"
		return true
	}
	if array, ok := s.c.out.arrayShape(typ); ok {
		return s.possiblyComparable(array.Elem, terms, seen)
	}
	switch t := types.Unalias(typ).(type) {
	case *types.TypeParam:
		for _, term := range terms {
			sub := s.types()
			if types.Identical(typ, s.c.out.Substitute(term.target, sub, s.constants.bindings)) && !s.possiblyComparable(s.c.out.Substitute(term.typ, sub, s.constants.bindings), terms, seen) {
				return false
			}
		}
		return true
	case *types.Named:
		return s.possiblyComparable(t.Underlying(), terms, seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !s.possiblyComparable(t.Field(i).Type(), terms, seen) {
				return false
			}
		}
		return true
	default:
		return types.Comparable(typ)
	}
}

func (s *coherenceConstraints) externalPromotion(typ types.Type, owner *types.Package, seen map[types.Type]bool) bool {
	if seen[typ] {
		return false
	}
	seen[typ] = true
	if pointer, ok := types.Unalias(typ).(*types.Pointer); ok {
		return s.externalPromotion(pointer.Elem(), owner, seen)
	}
	if named, ok := types.Unalias(typ).(*types.Named); ok && named.Obj().Pkg() != owner {
		return true
	}
	if structure, ok := typ.Underlying().(*types.Struct); ok {
		for i := 0; i < structure.NumFields(); i++ {
			field := structure.Field(i)
			if field.Embedded() && s.externalPromotion(field.Type(), owner, seen) {
				return true
			}
		}
	}
	return false
}

func (s *coherenceConstraints) potentialSugar(root *types.Named, name string) bool {
	for _, impl := range s.c.implOrder {
		if impl.Trait == nil || len(impl.Trait.TypeParams) != 0 || ordinaryGoReceiverTarget(impl) != root || !s.c.out.ConfigByFile[impl.File].GoSugarEnabled() {
			continue
		}
		for _, method := range impl.Methods {
			if !method.Static && sugarExportedName(method.GoName) == name {
				return true
			}
		}
	}
	return false
}
