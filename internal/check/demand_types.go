package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// bodyTypes is separate from the member-signature substitution helper: a body
// may contain ordinary Go generic function types whose binders must survive.
type bodyTypes struct {
	info      *Info
	constants map[*ConstParam]ConstValue
	sub       map[*types.TypeParam]types.Type
	memo      map[types.Type]types.Type
	active    map[*types.TypeParam]bool
}

func newBodyTypes(info *Info, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) *bodyTypes {
	return &bodyTypes{info: info, constants: constants, sub: sub, memo: map[types.Type]types.Type{}, active: map[*types.TypeParam]bool{}}
}

func (r *bodyTypes) typ(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	if result := r.memo[t]; result != nil {
		return result
	}
	if r.info != nil {
		if parameter, ok := types.Unalias(t).(*types.TypeParam); ok {
			if array := r.info.ArrayPatterns[parameter]; array != nil {
				result := r.info.ArrayType(r.typ(array.Elem), resolveConst(array.Length, r.constants))
				r.memo[t] = result
				return result
			}
		}
	}
	if !r.uses(t, map[types.Type]bool{}) {
		return t
	}
	var result types.Type
	switch t := t.(type) {
	case *types.TypeParam:
		result = t
		if value := r.sub[t]; value != nil && value != t && !r.active[t] {
			r.active[t] = true
			result = r.typ(value)
			delete(r.active, t)
		}
	case *types.Alias:
		result = r.typ(types.Unalias(t))
	case *types.Pointer:
		result = types.NewPointer(r.typ(t.Elem()))
	case *types.Slice:
		result = types.NewSlice(r.typ(t.Elem()))
	case *types.Array:
		result = types.NewArray(r.typ(t.Elem()), t.Len())
	case *types.Map:
		result = types.NewMap(r.typ(t.Key()), r.typ(t.Elem()))
	case *types.Chan:
		result = types.NewChan(t.Dir(), r.typ(t.Elem()))
	case *types.Named:
		args := make([]types.Type, t.TypeArgs().Len())
		for i := range args {
			args[i] = r.typ(t.TypeArgs().At(i))
		}
		if len(args) != 0 {
			result, _ = types.Instantiate(nil, t.Origin(), args, false)
		}
	case *types.Tuple:
		result = r.tuple(t)
	case *types.Signature:
		result = r.signature(t)
	case *types.Struct:
		fields, tags := make([]*types.Var, t.NumFields()), make([]string, t.NumFields())
		for i := range fields {
			field := t.Field(i)
			fields[i] = types.NewField(field.Pos(), field.Pkg(), field.Name(), r.typ(field.Type()), field.Embedded())
			tags[i] = t.Tag(i)
		}
		result = types.NewStruct(fields, tags)
	case *types.Interface:
		methods := make([]*types.Func, t.NumExplicitMethods())
		for i := range methods {
			method := t.ExplicitMethod(i)
			signature := method.Type().(*types.Signature)
			copy := types.NewSignatureType(nil, nil, nil, r.tuple(signature.Params()), r.tuple(signature.Results()), signature.Variadic())
			methods[i] = types.NewFunc(method.Pos(), method.Pkg(), method.Name(), copy)
		}
		embedded := make([]types.Type, t.NumEmbeddeds())
		for i := range embedded {
			embedded[i] = r.typ(t.EmbeddedType(i))
		}
		view := types.NewInterfaceType(methods, embedded)
		if t.IsImplicit() {
			view.MarkImplicit()
		}
		result = view.Complete()
	case *types.Union:
		terms := make([]*types.Term, t.Len())
		for i := range terms {
			term := t.Term(i)
			terms[i] = types.NewTerm(term.Tilde(), r.typ(term.Type()))
		}
		result = types.NewUnion(terms)
	}
	if result == nil {
		result = t
	}
	r.memo[t] = result
	return result
}

func (r *bodyTypes) tuple(t *types.Tuple) *types.Tuple {
	if t == nil {
		return nil
	}
	values := make([]*types.Var, t.Len())
	for i := range values {
		value := t.At(i)
		values[i] = types.NewVar(value.Pos(), value.Pkg(), value.Name(), r.typ(value.Type()))
	}
	return types.NewTuple(values...)
}

func (r *bodyTypes) signature(signature *types.Signature) *types.Signature {
	local := newBodyTypes(r.info, copySubstitution(r.sub), r.constants)
	parameters := func(list *types.TypeParamList) []*types.TypeParam {
		var result []*types.TypeParam
		for i := 0; i < list.Len(); i++ {
			old := list.At(i)
			if replacement := r.sub[old]; replacement != nil && replacement != old {
				continue
			}
			parameter := types.NewTypeParam(types.NewTypeName(old.Obj().Pos(), old.Obj().Pkg(), old.Obj().Name(), nil), anyType)
			local.sub[old] = parameter
			result = append(result, parameter)
		}
		return result
	}
	receiverParams, params := parameters(signature.RecvTypeParams()), parameters(signature.TypeParams())
	for _, list := range []*types.TypeParamList{signature.RecvTypeParams(), signature.TypeParams()} {
		for i := 0; i < list.Len(); i++ {
			old := list.At(i)
			if next, ok := local.sub[old].(*types.TypeParam); ok && next != old && r.sub[old] == nil {
				next.SetConstraint(local.typ(old.Constraint()))
			}
		}
	}
	var receiver *types.Var
	if old := signature.Recv(); old != nil {
		receiver = types.NewVar(old.Pos(), old.Pkg(), old.Name(), local.typ(old.Type()))
	}
	return types.NewSignatureType(receiver, receiverParams, params, local.tuple(signature.Params()), local.tuple(signature.Results()), signature.Variadic())
}

func (r *bodyTypes) uses(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if parameter, ok := t.(*types.TypeParam); ok {
		if r.info != nil {
			if array := r.info.ArrayPatterns[parameter]; array != nil {
				return resolveConst(array.Length, r.constants) != array.Length || r.uses(array.Elem, seen)
			}
		}
		return r.sub[parameter] != nil && r.sub[parameter] != parameter
	}
	return typeChildren(t, func(child types.Type) bool { return r.uses(child, seen) })
}

func copySubstitution(sub map[*types.TypeParam]types.Type) map[*types.TypeParam]types.Type {
	copy := make(map[*types.TypeParam]types.Type, len(sub))
	for parameter, value := range sub {
		copy[parameter] = value
	}
	return copy
}

func usesSubstitution(t types.Type, sub map[*types.TypeParam]types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	if parameter, ok := t.(*types.TypeParam); ok {
		return sub[parameter] != nil && sub[parameter] != parameter
	}
	return typeChildren(t, func(child types.Type) bool { return usesSubstitution(child, sub, seen) })
}

func containsTypeParameter(t types.Type) bool {
	seen := map[types.Type]bool{}
	var visit func(types.Type) bool
	visit = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if _, ok := t.(*types.TypeParam); ok {
			return true
		}
		return typeChildren(t, visit)
	}
	return visit(t)
}

// A named declaration is not unfolded. Its type arguments carry all free
// parameters, and unfolding recursive underlying types would never terminate.
func typeChildren(t types.Type, visit func(types.Type) bool) bool {
	switch t := t.(type) {
	case *types.Alias:
		return visit(types.Unalias(t))
	case *types.Pointer:
		return visit(t.Elem())
	case *types.Slice:
		return visit(t.Elem())
	case *types.Array:
		return visit(t.Elem())
	case *types.Map:
		return visit(t.Key()) || visit(t.Elem())
	case *types.Chan:
		return visit(t.Elem())
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if visit(t.TypeArgs().At(i)) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if visit(t.At(i).Type()) {
				return true
			}
		}
	case *types.Signature:
		if t.Recv() != nil && visit(t.Recv().Type()) || visit(t.Params()) || visit(t.Results()) {
			return true
		}
		for _, list := range []*types.TypeParamList{t.TypeParams(), t.RecvTypeParams()} {
			for i := 0; i < list.Len(); i++ {
				if visit(list.At(i)) || visit(list.At(i).Constraint()) {
					return true
				}
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if visit(t.Field(i).Type()) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if visit(t.ExplicitMethod(i).Type()) {
				return true
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if visit(t.EmbeddedType(i)) {
				return true
			}
		}
	case *types.Union:
		for i := 0; i < t.Len(); i++ {
			if visit(t.Term(i).Type()) {
				return true
			}
		}
	}
	return false
}

func bodyInfo(source *Info, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) *Info {
	return bodyInfoPermissions(source, sub, constants, nil)
}

func bodyInfoPermissions(source *Info, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) *Info {
	result := *source
	r := newBodyTypes(source, sub, constants)
	result.ConstValues = make(map[ast.Expr]ConstValue, len(source.ConstValues))
	for node, value := range source.ConstValues {
		result.ConstValues[node] = resolveConst(value, constants)
	}
	result.GenericBounds = make(map[*ast.FuncDecl][]*TraitBound, len(source.GenericBounds))
	for node, bounds := range source.GenericBounds {
		for _, bound := range bounds {
			c := &checker{out: source}
			result.GenericBounds[node] = append(result.GenericBounds[node], c.substituteBoundApplication(bound, sub, constants, sources))
		}
	}
	result.EnumValid = make(map[*ast.CallExpr]*Instance, len(source.EnumValid))
	for node, instance := range source.EnumValid {
		result.EnumValid[node] = r.enum(instance)
	}
	result.EnumAssertions = make(map[*ast.CallExpr]types.Type, len(source.EnumAssertions))
	for node, typ := range source.EnumAssertions {
		result.EnumAssertions[node] = r.typ(typ)
	}
	result.Types = make(map[ast.Expr]types.Type, len(source.Types))
	for node, typ := range source.Types {
		result.Types[node] = r.typ(typ)
	}
	result.Symbols = make(map[*resolve.Symbol]types.Type, len(source.Symbols))
	for symbol, typ := range source.Symbols {
		result.Symbols[symbol] = r.typ(typ)
	}
	result.TypeOverrides = make(map[ast.Type]types.Type, len(source.TypeOverrides)+len(source.typeNodes))
	for node, typ := range source.TypeOverrides {
		result.TypeOverrides[node] = r.typ(typ)
	}
	for node, typ := range source.typeNodes {
		if r.uses(typ, map[types.Type]bool{}) {
			result.TypeOverrides[node] = r.typ(typ)
		}
	}
	result.Members = cloneMap(source.Members)
	result.TraitViews = cloneMap(source.TraitViews)
	result.FunctionInstances = cloneMap(source.FunctionInstances)
	result.Matches = make(map[*ast.MatchExpr]*Match, len(source.Matches))
	for node, match := range source.Matches {
		copy := *match
		copy.Type, copy.Enum = r.typ(copy.Type), r.enum(copy.Enum)
		result.Matches[node] = &copy
	}
	result.Constructors = make(map[ast.Expr]*Constructor, len(source.Constructors))
	for node, constructor := range source.Constructors {
		copy := *constructor
		copy.Enum = r.enum(copy.Enum)
		result.Constructors[node] = &copy
	}
	result.Tries = make(map[*ast.TryExpr]*Try, len(source.Tries))
	for node, attempt := range source.Tries {
		copy := *attempt
		copy.Input, copy.Output = r.enum(copy.Input), r.enum(copy.Output)
		copy.Value, copy.Error = r.typ(copy.Value), r.typ(copy.Error)
		if attempt.Conversion != nil {
			selection := *attempt.Conversion
			selection.Signature, _ = r.typ(selection.Signature).(*types.Signature)
			copy.Conversion = &selection
		}
		result.Tries[node] = &copy
	}
	result.GoResults = make(map[*ast.CallExpr]*GoResult, len(source.GoResults))
	for node, conversion := range source.GoResults {
		copy := *conversion
		copy.Value, copy.Error, copy.Payload, copy.Result = r.typ(copy.Value), r.typ(copy.Error), r.typ(copy.Payload), r.typ(copy.Result)
		result.GoResults[node] = &copy
	}
	return &result
}

func cloneMap[K comparable, V any](source map[K]V) map[K]V {
	copy := make(map[K]V, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

func (r *bodyTypes) enum(instance *Instance) *Instance {
	if instance == nil {
		return nil
	}
	copy := *instance
	copy.Args = make([]types.Type, len(instance.Args))
	for i, argument := range instance.Args {
		copy.Args[i] = r.typ(argument)
	}
	return &copy
}
