package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Template helpers keep private source names in their defining package. Their
// arguments are ordinary, precisely typed operations, never runtime dictionaries.
// Returning the business function also preserves a direct defer/recover frame.
func (e *emitter) templateCallbacks(bounds []*check.TraitBound) (*goast.FieldList, func()) {
	previous := e.boundCallbacks
	e.boundCallbacks = make(map[*check.TraitBound]map[string]goast.Expr)
	parameters := &goast.FieldList{}
	used := map[string]bool{}
	for _, bound := range bounds {
		names := map[string]goast.Expr{}
		e.boundCallbacks[bound] = names
		for _, method := range bound.Trait.Methods {
			base := "goxide" + typeTargetName(traitBoundTarget(bound)) + exportedName(method.GoName)
			name := base
			for suffix := 2; used[name] || e.reserved[name]; suffix++ {
				name = base + strconv.Itoa(suffix)
			}
			used[name] = true
			e.reserved[name] = true
			names[method.Name] = goast.NewIdent(name)
			ft, _ := e.memberSignature(bound.MethodSignature(method))
			if !method.Static {
				target := traitBoundTarget(bound)
				if method.Receiver != check.ValueRef {
					target = types.NewPointer(target)
				}
				ft.Params.List = append([]*goast.Field{{Type: e.goType(target)}}, ft.Params.List...)
			}
			// A function type may not mix named and unnamed parameters.
			for _, parameter := range ft.Params.List {
				parameter.Names = nil
			}
			parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: ft})
			if !method.Static && e.traits.boundNeedsBinding(bound, method.Name) {
				bindingName := name + "Bind"
				for e.reserved[bindingName] {
					bindingName += "_"
				}
				e.reserved[bindingName] = true
				names["bind:"+method.Name] = goast.NewIdent(bindingName)
				selection := &check.MemberSelection{Member: method, Bound: bound, Signature: bound.MethodSignature(method)}
				parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(bindingName)}, Type: e.goType(e.bindingSignature(selection))})
			}
		}
		for _, associated := range bound.Trait.Associated {
			base := "goxide" + typeTargetName(traitBoundTarget(bound)) + "Type" + exportedName(associated.Name)
			name := base
			for suffix := 2; used[name] || e.reserved[name]; suffix++ {
				name = base + strconv.Itoa(suffix)
			}
			used[name], e.reserved[name] = true, true
			names["type:"+associated.Name] = goast.NewIdent(name)
			parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: resultFunc(e.goType(bound.Associated[associated.Name]))})
		}
	}
	return parameters, func() { e.boundCallbacks = previous }
}

func (e *emitter) callbackReference(value goast.Expr) goast.Expr {
	if e.callbackReceiver != "" {
		return &goast.SelectorExpr{X: goast.NewIdent(e.callbackReceiver), Sel: value.(*goast.Ident)}
	}
	return value
}

func (e *emitter) callbackOperation(bound *check.TraitBound, member *check.Member) goast.Expr {
	if names := e.boundCallbacks[bound]; names != nil {
		return e.callbackReference(names[member.Name])
	}
	for candidate, names := range e.boundCallbacks {
		if candidate.Trait == bound.Trait && types.Identical(traitBoundTarget(candidate), traitBoundTarget(bound)) {
			return e.callbackReference(names[member.Name])
		}
	}
	return nil
}

func (e *emitter) callbackArguments(bounds []*check.TraitBound, evidence []check.Evidence) []goast.Expr {
	var args []goast.Expr
	for index, bound := range bounds {
		selected := check.Evidence{Bound: bound}
		if index < len(evidence) {
			selected = evidence[index]
		}
		for _, method := range bound.Trait.Methods {
			member := method
			if selected.Impl != nil {
				for _, actual := range selected.Impl.Methods {
					if actual.Name == method.Name {
						member = actual
						break
					}
				}
			}
			selection := &check.MemberSelection{Member: member, Impl: selected.Impl, Bound: selected.Bound, Signature: member.Signature}
			if selected.Bound != nil {
				selection.Signature = selected.Bound.MethodSignature(method)
			}
			args = append(args, e.operationFunction(selection))
			if !method.Static && e.traits.boundNeedsBinding(bound, method.Name) {
				args = append(args, e.bindingFunction(selection))
			}
		}
		for _, associated := range bound.Trait.Associated {
			args = append(args, e.associatedCallback(selected, bound, associated.Name))
		}
	}
	return args
}

func (e *emitter) templateImplParameters(impl *check.Impl) *goast.FieldList {
	var fields []*goast.Field
	for _, parameter := range impl.TypeParams {
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(parameter.Obj().Name())}, Type: e.goConstraint(parameter.Constraint())})
	}
	fields = append(fields, projectedParameters(impl.Requirements)...)
	if len(fields) == 0 {
		return nil
	}
	return &goast.FieldList{List: fields}
}

func (e *emitter) templateImplArguments(impl *check.Impl) []goast.Expr {
	var args []goast.Expr
	if impl.Template == nil {
		for _, parameter := range impl.TypeParams {
			args = append(args, e.goType(parameter))
		}
	} else {
		for _, argument := range impl.TypeArgs {
			args = append(args, e.goType(argument))
		}
	}

	return args
}

func (e *emitter) templateOperationName(member *check.Member) string {
	impl := implTemplate(member.Impl)
	return "Goxide" + e.traits.templateNames[impl] + exportedName(member.GoName) + "Func"
}

func (e *emitter) templateOperation(selection *check.MemberSelection) goast.Expr {
	impl := selection.Impl
	function := enumIndices(e.sourceGlobal(impl.File, e.templateOperationName(selection.Member)), e.templateImplArguments(impl))
	return &goast.CallExpr{Fun: function, Args: append(e.templateImplWitnessArguments(impl), e.callbackArguments(implTemplate(impl).Requirements, impl.RequirementEvidence)...)}
}

func (e *emitter) templateOperationDeclaration(member *check.Member) []goast.Decl {
	restoreInfo := e.memberBodyInfo(member)
	defer restoreInfo()
	parameters, restore := e.templateCallbacks(member.Impl.Requirements)
	defer restore()
	parameters.List = append(e.templateImplWitnessParameters(member.Impl), parameters.List...)
	name := e.templateOperationName(member)
	e.generatedName(name, member.Decl)
	ft := &goast.FuncType{Params: e.params(member.Decl.Params), Results: e.params(member.Decl.Results)}
	business := &goast.FuncLit{Type: ft, Body: e.functionBody(ft, member.Decl.Body)}
	outer := &goast.FuncType{TypeParams: e.templateImplParameters(member.Impl), Params: parameters, Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}
	declaration := &goast.FuncDecl{Name: goast.NewIdent(name), Type: outer, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{business}}}}}
	e.anchor(declaration, member.Decl)
	out := []goast.Decl{declaration}
	if e.traits.inferredCalls[member.Decl] {
		out = append(out, e.templateCallDeclaration(name, outer.TypeParams, parameters, member.Decl))
	}
	return out
}

func templateFunctionName(function *traitFunction) string {
	prefix := "Goxide"
	if function.symbol.Visibility != resolve.Public {
		prefix = "goxide"
	}
	return prefix + function.symbol.GoName + "Func"
}

func (e *emitter) templateFunctionDeclaration(function *traitFunction) []goast.Decl {
	node := function.decl
	bounds := e.info.GenericBounds[node]
	parameters, restore := e.templateCallbacks(bounds)
	defer restore()
	typeParameters := e.functionTypeParameters(node)
	if projected := projectedParameters(bounds); len(projected) != 0 {
		if typeParameters == nil {
			typeParameters = &goast.FieldList{}
		}
		typeParameters.List = append(typeParameters.List, projected...)
	}
	ft := &goast.FuncType{Params: e.params(node.Params), Results: e.params(node.Results)}
	business := &goast.FuncLit{Type: ft, Body: e.functionBody(ft, node.Body)}
	name := constFunctionName(function, e.instanceBodyFunction())
	e.generatedName(name, node)
	declaration := &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: typeParameters, Params: parameters, Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{business}}}}}
	e.anchor(declaration, node)
	callParameters := &goast.FieldList{}
	signature, _ := e.info.Symbols[function.symbol].(*types.Signature)
	count := 0
	for _, group := range node.Generics.Params {
		if group.Const {
			continue
		}
		count += len(group.Names)
	}
	for i := 0; i < count; i++ {
		callParameters.List = append(callParameters.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent("_")}, Type: resultFunc(e.goType(signature.TypeParams().At(i)))})
	}
	callParameters.List = append(callParameters.List, parameters.List...)
	out := []goast.Decl{declaration}
	if e.traits.inferredCalls[node] {
		out = append(out, e.templateCallDeclaration(name, typeParameters, callParameters, node))
	}
	return out
}

func (e *emitter) templateFunctionInstance(instance *check.FunctionInstance, function *traitFunction) goast.Expr {
	var args []goast.Expr
	for _, typ := range instance.TypeArgs {
		args = append(args, e.goType(typ))
	}

	factory := enumIndices(e.sourceGlobal(function.symbol.Decl.File, constFunctionName(function, instance)), args)
	return &goast.CallExpr{Fun: factory, Args: e.callbackArguments(e.info.GenericBounds[function.decl], instance.Evidence)}
}

// Preserve the semantic ellipsis when source positions are absent in helpers.
func callWithSignature(fun goast.Expr, args []goast.Expr, signature *types.Signature) *goast.CallExpr {
	call := &goast.CallExpr{Fun: fun, Args: args}
	if signature.Variadic() {
		call.Ellipsis = gotoken.Pos(1)
	}
	return call
}

func (e *emitter) traitTemplateDeclarations(node syntax.Item) []goast.Decl {
	declaration, ok := node.(*syntax.ImplDecl)
	if !ok {
		return nil
	}
	impl := e.info.Impls[declaration]
	if impl != nil && len(impl.ConstParams) != 0 {
		var out []goast.Decl
		for _, definition := range e.traits.impls {
			if definition.Template == impl {
				out = append(out, e.templateImplDeclarations(definition)...)
			}
		}
		return out
	}
	if impl == nil || !parameterizedImpl(impl) {
		return nil
	}
	if e.info.Demands != nil && e.info.Demands.ImplTemplates[impl] && !e.traits.templateImpls[impl] {
		return nil
	}
	return e.templateImplDeclarations(impl)
}

func (e *emitter) templateImplDeclarations(impl *check.Impl) []goast.Decl {
	var out []goast.Decl
	if impl.Trait != nil {
		for _, associated := range impl.Trait.Associated {
			out = append(out, e.templateAssociatedDeclaration(impl, associated.Name))
		}
	}
	for _, member := range impl.Methods {
		if e.traits.memberNeedsBinding(member) {
			out = append(out, e.templateBindingDeclaration(member))
		}
		if (len(impl.ConstArgs) != 0 || e.traits.helperImpls[impl]) && (impl.Trait != nil || len(impl.Requirements) != 0) {
			out = append(out, e.templateOperationDeclaration(member)...)
		}
		out = append(out, e.instanceDeclarations(member.Decl)...)
	}
	return out
}

func (e *emitter) templateRuntimeDeclarations(impl *check.Impl) []goast.Decl {
	parameters, restore := e.templateCallbacks(impl.Requirements)
	defer restore()
	name := e.traitWrapper(impl)
	e.generatedName(name, impl.Decl)
	fields := receiverField("target", e.goType(captureType(impl)))
	fields.List = append(fields.List, parameters.List...)
	out := []goast.Decl{genericTypeDeclaration(name, e.templateImplParameters(impl), &goast.StructType{Fields: fields})}
	for _, member := range impl.Methods {
		if !member.Static {
			out = append(out, e.wrapperMethod(member))
		}
	}
	if impl.Trait.Runtime {
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(impl.Trait.Marker), Recv: receiverField("", e.implWrapperType(impl)), Type: &goast.FuncType{Params: &goast.FieldList{}}, Body: &goast.BlockStmt{}})
	}
	var elements []goast.Expr
	elements = append(elements, &goast.KeyValueExpr{Key: goast.NewIdent("target"), Value: goast.NewIdent("target")})
	for _, parameter := range parameters.List {
		for _, parameterName := range parameter.Names {
			elements = append(elements, &goast.KeyValueExpr{Key: goast.NewIdent(parameterName.Name), Value: goast.NewIdent(parameterName.Name)})
		}
	}
	resultType := e.implWrapperType(impl)
	if impl.Trait.Runtime {
		resultType = e.traitViewType(impl)
	}
	ft := &goast.FuncType{Params: receiverField("target", e.goType(captureType(impl))), Results: &goast.FieldList{List: []*goast.Field{{Type: resultType}}}}
	factoryBody := &goast.FuncLit{Type: ft, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CompositeLit{Type: e.implWrapperType(impl), Elts: elements}}}}}}
	factoryName := e.traitFactoryName(impl)
	e.generatedName(factoryName, impl.Decl)
	out = append(out, &goast.FuncDecl{Name: goast.NewIdent(factoryName), Type: &goast.FuncType{TypeParams: e.templateImplParameters(impl), Params: &goast.FieldList{List: append(e.templateImplWitnessParameters(impl), parameters.List...)}, Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{factoryBody}}}}})
	if impl.Trait.Runtime && impl.Base != nil && e.localTypePackage(impl.Base.Obj().Pkg()) && nativeInherentImpl(impl) {
		e.generatedMember(impl.Base, traitAs(impl), impl)
		out = append(out, e.asDeclaration(impl, captureType(impl), nil))
	}
	return out
}

func (e *emitter) associatedCallback(evidence check.Evidence, bound *check.TraitBound, name string) goast.Expr {
	if evidence.Bound != nil {
		if callback := e.callbackOperation(evidence.Bound, &check.Member{Name: "type:" + name}); callback != nil {
			return callback
		}
	}
	if evidence.Impl != nil && parameterizedImpl(implTemplate(evidence.Impl)) {
		impl := evidence.Impl
		factory := enumIndices(e.sourceGlobal(impl.File, e.templateAssociatedName(impl, name)), e.templateImplArguments(impl))
		return &goast.CallExpr{Fun: factory, Args: append(e.templateImplWitnessArguments(impl), e.callbackArguments(implTemplate(impl).Requirements, impl.RequirementEvidence)...)}
	}
	return &goast.SelectorExpr{X: &goast.CompositeLit{Type: e.evidenceType(evidence)}, Sel: goast.NewIdent(traitWitness(bound.Trait, name))}
}

func (e *emitter) templateAssociatedName(impl *check.Impl, name string) string {
	return "Goxide" + e.traits.templateNames[implTemplate(impl)] + "Type" + exportedName(name) + "Func"
}

func (e *emitter) templateAssociatedDeclaration(impl *check.Impl, associated string) goast.Decl {
	parameters, restore := e.templateCallbacks(impl.Requirements)
	defer restore()
	parameters.List = append(e.templateImplWitnessParameters(impl), parameters.List...)
	typ := e.goType(impl.Associated[associated])
	ft := resultFunc(typ)
	value := e.fresh("Value")
	business := &goast.FuncLit{Type: ft, Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(value, typ, nil), &goast.ReturnStmt{Results: []goast.Expr{value}}}}}
	name := e.templateAssociatedName(impl, associated)
	e.generatedName(name, impl.Decl)
	return &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: e.templateImplParameters(impl), Params: parameters, Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{business}}}}}
}

func (e *emitter) templateImplReceiverArguments(impl *check.Impl) []goast.Expr {
	if !parameterizedImpl(implTemplate(impl)) {
		return nil
	}
	args := e.templateImplArguments(impl)
	for index, bound := range implTemplate(impl).Requirements {
		for _, projected := range bound.Projected {
			actual := types.Type(projected)
			if substituted := impl.Substitution[projected]; substituted != nil {
				actual = substituted
			} else if index < len(impl.Requirements) {
				for _, associated := range bound.Trait.Associated {
					if types.Identical(bound.Associated[associated.Name], projected) {
						actual = impl.Requirements[index].Associated[associated.Name]
					}
				}
			}
			args = append(args, e.goType(actual))
		}
	}
	return args
}
