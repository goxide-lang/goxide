package check

import "go/types"

// TypeState owns types created while consuming a completed check result. Its
// zero value is ready to use. Each consumer must use its own state; no method
// adds array representatives to the supplied Info or its related checked views.
type TypeState struct {
	arrays        map[*types.TypeParam]*ArrayPattern
	goMemberNames map[types.Object]string
}

// ArrayOf recognizes both checked arrays and this consumer's substitutions.
func (s *TypeState) ArrayOf(info *Info, typ types.Type) (*ArrayPattern, bool) {
	if typ != nil {
		if parameter, ok := types.Unalias(typ).(*types.TypeParam); ok {
			if array := s.arrays[parameter]; array != nil {
				return array, true
			}
		}
	}
	return info.ArrayOf(typ)
}

func (s *TypeState) arrayType(info *Info, elem types.Type, length ConstValue) types.Type {
	if length.Param == nil {
		return types.NewArray(elem, length.Value)
	}
	if info != nil {
		for rep, array := range info.ArrayPatterns {
			if array.Length == length && types.Identical(array.Elem, elem) {
				return rep
			}
		}
	}
	for rep, array := range s.arrays {
		if array.Length == length && types.Identical(array.Elem, elem) {
			return rep
		}
	}
	if s.arrays == nil {
		s.arrays = map[*types.TypeParam]*ArrayPattern{}
	}
	rep := arrayRepresentative(elem, length)
	s.arrays[rep] = &ArrayPattern{Elem: elem, Length: length}
	return rep
}

// Substitute resolves source lengths and types without changing checked data.
func (s *TypeState) Substitute(info *Info, typ types.Type, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) types.Type {
	replacer := newBodyTypes(info, sub, constants)
	replacer.state = s
	return replacer.typ(typ)
}

// Fields substitutes a payload using the enum's original checked context.
func (s *TypeState) Fields(instance *Instance, variant *Variant) []Field {
	return instance.fields(variant, s)
}

// MethodSignature substitutes a bound using its original checked context.
func (s *TypeState) MethodSignature(bound *TraitBound, member *Member) *types.Signature {
	replacer := newBodyTypes(bound.info, bound.methodSubstitution(), nil)
	replacer.state = s
	signature := member.Signature
	return types.NewSignatureType(nil, nil, nil, replacer.tuple(signature.Params()), replacer.tuple(signature.Results()), signature.Variadic())
}

// Comparable follows source arrays, including privately substituted ones.
func (s *TypeState) Comparable(info *Info, typ types.Type) bool {
	if typ == nil {
		return false
	}
	if array, ok := s.ArrayOf(info, typ); ok {
		return s.Comparable(info, array.Elem)
	}
	if structure, ok := typ.Underlying().(*types.Struct); ok {
		for i := 0; i < structure.NumFields(); i++ {
			if !s.Comparable(info, structure.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return types.Comparable(typ)
}

// GoMemberName includes members copied into this consumer's substituted types.
func (s *TypeState) GoMemberName(info *Info, object types.Object) string {
	if name := s.goMemberNames[memberOrigin(object)]; name != "" {
		return name
	}
	return info.GoMemberName(object)
}

func (s *TypeState) copyGoMember(info *Info, to, from types.Object) {
	name := s.GoMemberName(info, from)
	if name != from.Name() {
		if s.goMemberNames == nil {
			s.goMemberNames = map[types.Object]string{}
		}
		s.goMemberNames[memberOrigin(to)] = name
	}
}
