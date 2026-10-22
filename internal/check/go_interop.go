package check

import "go/types"

// goInterop is a Go method-set view, not a source trait or inherent-member
// table. All local named declarations are copied so recursive fields and
// promoted methods see the same view without changing source type identities.
type goInterop struct {
	info    *Info
	local   map[*types.Package]bool
	sugar   map[*types.Named][]*SugarMember
	forward map[types.Type]types.Type
	reverse map[types.Type]types.Type
}

func newGoInterop(c *checker) *goInterop {
	r := &goInterop{info: c.out, local: map[*types.Package]bool{}, sugar: map[*types.Named][]*SugarMember{}, forward: map[types.Type]types.Type{}, reverse: map[types.Type]types.Type{}}
	for _, pkg := range c.packages {
		r.local[pkg] = true
	}
	for _, member := range c.out.Sugar {
		target := ordinaryGoReceiverTarget(implDeclaration(member.Impl))
		r.sugar[target] = append(r.sugar[target], member)
	}
	for _, implementation := range c.implOrder {
		if implementation.Trait != nil || !CanUseGoReceiver(implementation) {
			continue
		}
		for _, member := range implementation.Methods {
			if !member.Static {
				target := ordinaryGoReceiverTarget(implementation)
				r.sugar[target] = append(r.sugar[target], &SugarMember{Impl: implementation, Member: member, Name: member.GoName, Signature: member.Signature})
			}
		}
	}
	return r
}

func (c *checker) instantiateGo(t types.Type, arguments []types.Type) (types.Type, error) {
	if c.goInterop == nil || len(c.goInterop.sugar) == 0 {
		return types.Instantiate(nil, t, arguments, true)
	}
	view := make([]types.Type, len(arguments))
	for i, argument := range arguments {
		view[i] = c.goInterop.typ(argument, true)
	}
	if _, err := types.Instantiate(nil, c.goInterop.typ(t, true), view, true); err != nil {
		return nil, err
	}
	return types.Instantiate(nil, t, arguments, false)
}

func (c *checker) satisfiesGo(t types.Type, constraint *types.Interface) bool {
	t = c.refineGoType(t)
	if c.goInterop == nil || len(c.goInterop.sugar) == 0 {
		return types.Satisfies(t, constraint)
	}
	return types.Satisfies(c.goInterop.typ(t, true), c.goInterop.typ(constraint, true).(*types.Interface))
}

// Branch predicates refine fresh Go parameters; declaration parameters remain
// rigid source identities and are never mutated by a proof branch.
func (c *checker) refineGoType(t types.Type) types.Type {
	if len(c.activeGoConstraints) == 0 {
		return t
	}
	sub := map[*types.TypeParam]types.Type{}
	constraints := map[*types.TypeParam][]types.Type{}
	for _, predicate := range c.activeGoConstraints {
		if parameter, ok := types.Unalias(predicate.Target).(*types.TypeParam); ok && !c.out.isArray(parameter) {
			constraints[parameter] = append(constraints[parameter], predicate.Constraint)
			if sub[parameter] == nil {
				sub[parameter] = types.NewTypeParam(types.NewTypeName(parameter.Obj().Pos(), parameter.Obj().Pkg(), parameter.Obj().Name(), nil), anyType)
			}
		}
	}
	for original, refinements := range constraints {
		components := append([]types.Type{original.Constraint()}, refinements...)
		constraint := types.NewInterfaceType(nil, components).Complete()
		sub[original].(*types.TypeParam).SetConstraint(c.out.Substitute(constraint, sub, nil))
	}
	return c.out.Substitute(t, sub, nil)
}

// Only native Go syntax sees forwarded methods by their emitted Go name.
func (c *checker) lookupGoMember(t types.Type, addressable bool, name string) types.Object {
	if c.file.AST.NativeGo {
		object, _ := c.lookupGoInteropMember(t, addressable, name)
		return object
	}
	object, _, _ := types.LookupFieldOrMethod(t, addressable, c.pkg, name)
	return object
}

// Permission replay also needs this view after a Go type parameter becomes a
// concrete source type. Otherwise a forwarded pointer method could disappear
// from lookup and its receiver permission check would be skipped.
func (c *checker) lookupGoInteropMember(t types.Type, addressable bool, name string) (types.Object, []int) {
	if c.goInterop != nil && len(c.goInterop.sugar) != 0 {
		object, indices, _ := types.LookupFieldOrMethod(c.goInterop.typ(t, true), addressable, c.pkg, name)
		if function, ok := object.(*types.Func); ok {
			return types.NewFunc(function.Pos(), function.Pkg(), function.Name(), c.goInterop.typ(function.Type(), false).(*types.Signature)), indices
		}
		if field, ok := object.(*types.Var); ok {
			return types.NewField(field.Pos(), field.Pkg(), field.Name(), c.goInterop.typ(field.Type(), false), field.Embedded()), indices
		}
		return object, indices
	}
	object, indices, _ := types.LookupFieldOrMethod(t, addressable, c.pkg, name)
	return object, indices
}

func (c *checker) assignableGo(actual, expected types.Type) bool {
	if c.goInterop != nil && len(c.goInterop.sugar) != 0 {
		return types.AssignableTo(c.goInterop.typ(actual, true), c.goInterop.typ(expected, true))
	}
	return types.AssignableTo(actual, expected)
}

func (r *goInterop) typ(t types.Type, forward bool) types.Type {
	if t == nil {
		return nil
	}
	known := r.forward
	if !forward {
		known = r.reverse
	}
	if result := known[t]; result != nil {
		return result
	}
	visit := func(t types.Type) types.Type { return r.typ(t, forward) }
	var result types.Type
	switch t := t.(type) {
	case *types.Alias:
		result = visit(types.Unalias(t))
	case *types.TypeParam:
		if array, ok := r.info.ArrayOf(t); ok {
			result = r.info.ArrayType(visit(array.Elem), array.Length)
			break
		}
		if !forward {
			return t
		}
		parameter := types.NewTypeParam(types.NewTypeName(t.Obj().Pos(), t.Obj().Pkg(), t.Obj().Name(), nil), anyType)
		r.forward[t], r.reverse[parameter] = parameter, t
		parameter.SetConstraint(visit(t.Constraint()))
		return parameter
	case *types.Named:
		if t.TypeArgs().Len() != 0 {
			arguments := make([]types.Type, t.TypeArgs().Len())
			for i := range arguments {
				arguments[i] = visit(t.TypeArgs().At(i))
			}
			result, _ = types.Instantiate(nil, visit(t.Origin()), arguments, false)
		} else if !forward || !r.local[t.Obj().Pkg()] {
			return t
		} else {
			copy := types.NewNamed(types.NewTypeName(t.Obj().Pos(), t.Obj().Pkg(), t.Obj().Name(), nil), nil, nil)
			r.forward[t], r.reverse[copy] = copy, t
			copy.SetTypeParams(r.parameters(t.TypeParams(), forward))
			copy.SetUnderlying(visit(t.Underlying()))
			for i := 0; i < t.NumMethods(); i++ {
				method := t.Method(i)
				copy.AddMethod(types.NewFunc(method.Pos(), method.Pkg(), method.Name(), visit(method.Type()).(*types.Signature)))
			}
			for _, member := range r.sugar[t] {
				copy.AddMethod(r.method(copy, member))
			}
			return copy
		}
	case *types.Pointer:
		result = types.NewPointer(visit(t.Elem()))
	case *types.Slice:
		result = types.NewSlice(visit(t.Elem()))
	case *types.Array:
		result = types.NewArray(visit(t.Elem()), t.Len())
	case *types.Map:
		result = types.NewMap(visit(t.Key()), visit(t.Elem()))
	case *types.Chan:
		result = types.NewChan(t.Dir(), visit(t.Elem()))
	case *types.Tuple:
		values := make([]*types.Var, t.Len())
		for i := range values {
			value := t.At(i)
			values[i] = types.NewVar(value.Pos(), value.Pkg(), value.Name(), visit(value.Type()))
		}
		result = types.NewTuple(values...)
	case *types.Signature:
		var receiver *types.Var
		if value := t.Recv(); value != nil {
			receiver = types.NewVar(value.Pos(), value.Pkg(), value.Name(), visit(value.Type()))
		}
		var recvParams []*types.TypeParam
		if forward {
			recvParams = r.parameters(t.RecvTypeParams(), true)
		}
		result = types.NewSignatureType(receiver, recvParams, r.parameters(t.TypeParams(), forward), visit(t.Params()).(*types.Tuple), visit(t.Results()).(*types.Tuple), t.Variadic())
	case *types.Struct:
		fields, tags := make([]*types.Var, t.NumFields()), make([]string, t.NumFields())
		for i := range fields {
			field := t.Field(i)
			fields[i] = types.NewField(field.Pos(), field.Pkg(), field.Name(), visit(field.Type()), field.Embedded())
			tags[i] = t.Tag(i)
		}
		result = types.NewStruct(fields, tags)
	case *types.Interface:
		methods := make([]*types.Func, t.NumExplicitMethods())
		for i := range methods {
			method := t.ExplicitMethod(i)
			signature := method.Type().(*types.Signature)
			// Completed interface methods point their receiver back to the
			// interface. The receiver is implicit in this declaration.
			copy := types.NewSignatureType(nil, nil, nil, visit(signature.Params()).(*types.Tuple), visit(signature.Results()).(*types.Tuple), signature.Variadic())
			methods[i] = types.NewFunc(method.Pos(), method.Pkg(), method.Name(), copy)
		}
		embedded := make([]types.Type, t.NumEmbeddeds())
		for i := range embedded {
			embedded[i] = visit(t.EmbeddedType(i))
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
			terms[i] = types.NewTerm(term.Tilde(), visit(term.Type()))
		}
		result = types.NewUnion(terms)
	default:
		return t
	}
	if result == nil {
		return t
	}
	known[t] = result
	if forward {
		r.reverse[result] = t
	}
	return result
}

func (r *goInterop) parameters(list *types.TypeParamList, forward bool) []*types.TypeParam {
	var parameters []*types.TypeParam
	for i := 0; i < list.Len(); i++ {
		parameters = append(parameters, r.typ(list.At(i), forward).(*types.TypeParam))
	}
	return parameters
}

func (r *goInterop) method(target *types.Named, member *SugarMember) *types.Func {
	var parameters []*types.TypeParam
	sub := map[*types.TypeParam]types.Type{}
	source := types.Unalias(member.Impl.Target).(*types.Named)
	for i := 0; i < source.TypeArgs().Len(); i++ {
		original := source.TypeArgs().At(i).(*types.TypeParam)
		parameter := types.NewTypeParam(types.NewTypeName(0, original.Obj().Pkg(), original.Obj().Name(), nil), anyType)
		parameters = append(parameters, parameter)
		sub[original] = parameter
		// These parameters already belong to the Go view.
		r.forward[parameter], r.reverse[parameter] = parameter, original
	}
	body := newBodyTypes(r.info, sub, nil)
	for i, parameter := range parameters {
		original := source.TypeArgs().At(i).(*types.TypeParam)
		parameter.SetConstraint(r.typ(body.typ(original.Constraint()), true))
	}
	var receiver types.Type = target
	if len(parameters) != 0 {
		arguments := make([]types.Type, len(parameters))
		for i := range arguments {
			arguments[i] = parameters[i]
		}
		receiver, _ = types.Instantiate(nil, target, arguments, false)
	}
	if member.Member.Receiver != ValueRef {
		receiver = types.NewPointer(receiver)
	}
	signature := r.typ(body.typ(member.Signature), true).(*types.Signature)
	signature = types.NewSignatureType(types.NewVar(0, member.Impl.Owner, "", receiver), parameters, nil, signature.Params(), signature.Results(), signature.Variadic())
	return types.NewFunc(0, member.Impl.Owner, member.Name, signature)
}
