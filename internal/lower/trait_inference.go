package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func signatureOf(params []types.Type, result types.Type) *types.Signature {
	vars := make([]*types.Var, len(params))
	for i, typ := range params {
		vars[i] = types.NewVar(0, nil, "", typ)
	}
	var results []*types.Var
	if result != nil {
		results = append(results, types.NewVar(0, nil, "", result))
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(vars...), types.NewTuple(results...), false)
}

func (e *emitter) typeWitness(typ types.Type) goast.Expr {
	name := e.fresh("Type")
	return &goast.FuncLit{Type: resultFunc(e.goType(typ)), Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(name, e.goType(typ), nil), &goast.ReturnStmt{Results: []goast.Expr{name}}}}}
}

// Inference helpers only connect statically selected function values. Their
// type parameters keep a foreign private name out of the consuming package.
type traitInference struct {
	e              *emitter
	fields         []*goast.Field
	arguments      []goast.Expr
	parameterTypes []types.Type
	types          []types.Type
	names          []*goast.Ident
	bodyTypes      map[types.Type]*goast.Ident
}

func newTraitInference(e *emitter) *traitInference {
	return &traitInference{e: e, bodyTypes: map[types.Type]*goast.Ident{}}
}

func (p *traitInference) collect(typ types.Type) {
	if typ == nil {
		return
	}
	switch typ := typ.(type) {
	case *types.Signature:
		for i := 0; i < typ.Params().Len(); i++ {
			parameter := typ.Params().At(i).Type()
			if typ.Variadic() && i == typ.Params().Len()-1 {
				parameter = parameter.(*types.Slice).Elem()
			}
			p.collect(parameter)
		}
		p.collect(typ.Results())
		return
	case *types.Slice:
		// Ellipsis forwarding must retain the slice/element relationship.
		p.collect(typ.Elem())
		return
	case *types.Tuple:
		for i := 0; i < typ.Len(); i++ {
			p.collect(typ.At(i).Type())
		}
		return
	}
	if p.e.nameableType(typ) && !inferenceHasParameter(typ) {
		return
	}
	for _, previous := range p.types {
		if types.Identical(previous, typ) {
			return
		}
	}
	name := p.e.fresh("Inferred")
	p.types = append(p.types, typ)
	p.names = append(p.names, name)
	p.bodyTypes[typ] = name
}

func inferenceHasParameter(typ types.Type) bool {
	switch t := typ.(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if inferenceHasParameter(t.TypeArgs().At(i)) {
				return true
			}
		}
	case *types.Pointer:
		return inferenceHasParameter(t.Elem())
	case *types.Slice:
		return inferenceHasParameter(t.Elem())
	case *types.Array:
		return inferenceHasParameter(t.Elem())
	case *types.Map:
		return inferenceHasParameter(t.Key()) || inferenceHasParameter(t.Elem())
	case *types.Chan:
		return inferenceHasParameter(t.Elem())
	case *types.Signature:
		return inferenceHasParameter(t.Params()) || inferenceHasParameter(t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if inferenceHasParameter(t.At(i).Type()) {
				return true
			}
		}
	}
	return false
}

func (p *traitInference) parameter(value goast.Expr, typ types.Type) goast.Expr {
	p.collect(typ)
	name := p.e.fresh("Argument")
	// Render after the complete evidence graph has supplied all type names.
	p.fields = append(p.fields, &goast.Field{Names: []*goast.Ident{name}})
	p.arguments = append(p.arguments, value)
	p.parameterTypes = append(p.parameterTypes, typ)
	return name
}

func (p *traitInference) finish(result goast.Expr, typ types.Type) goast.Expr {
	p.collect(typ)
	previous := p.e.inferenceTypes
	p.e.inferenceTypes = p.bodyTypes
	defer func() { p.e.inferenceTypes = previous }()
	parameters := &goast.FieldList{}
	for _, name := range p.names {
		parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{name}, Type: goast.NewIdent("any")})
	}
	for i, field := range p.fields {
		field.Type = p.e.goType(p.parameterTypes[i])
	}
	name := p.e.fresh("Bind")
	decl := &goast.FuncDecl{Name: name, Type: &goast.FuncType{TypeParams: parameters, Params: &goast.FieldList{List: p.fields}, Results: &goast.FieldList{List: []*goast.Field{{Type: p.e.goType(typ)}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{result}}}}}
	p.e.flowHelpers = append(p.e.flowHelpers, decl)
	return &goast.CallExpr{Fun: name, Args: p.arguments}
}

func (p *traitInference) witness(typ types.Type) goast.Expr {
	p.collect(typ)
	previous := p.e.inferenceTypes
	p.e.inferenceTypes = p.bodyTypes
	value := p.e.typeWitness(typ)
	p.e.inferenceTypes = previous
	return value
}

func operationSignature(selection *check.MemberSelection) *types.Signature {
	member := selection.Member
	sig := member.Signature
	if selection.Signature != nil {
		sig = selection.Signature
	}
	if selection.MethodExpr {
		return sig
	}
	if member.Static {
		return sig
	}
	var target types.Type
	if selection.Impl != nil {
		target = selection.Impl.Target
	} else {
		target = traitBoundTarget(selection.Bound)
	}
	if member.Receiver != check.ValueRef {
		target = types.NewPointer(target)
	}
	params := []*types.Var{types.NewVar(0, nil, "", target)}
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i))
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), sig.Results(), sig.Variadic())
}

func (p *traitInference) callbacks(bounds []*check.TraitBound, evidence []check.Evidence) ([]goast.Expr, []types.Type) {
	var args []goast.Expr
	var parameters []types.Type
	for i, bound := range bounds {
		selected := check.Evidence{Bound: bound}
		if i < len(evidence) {
			selected = evidence[i]
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
			selection := &check.MemberSelection{Member: member, Impl: selected.Impl, Bound: selected.Bound}
			if selected.Bound != nil {
				selection.Signature = selected.Bound.MethodSignature(method)
			}
			args = append(args, p.operation(selection))
			parameters = append(parameters, operationSignature(selection))
		}
		for _, associated := range bound.Trait.Associated {
			var typ types.Type
			if selected.Impl != nil {
				typ = selected.Impl.Associated[associated.Name]
			} else {
				typ = selected.Bound.Associated[associated.Name]
			}
			ft := signatureOf(nil, typ)
			if selected.Impl != nil && parameterizedImpl(implTemplate(selected.Impl)) {
				impl := selected.Impl
				children, childTypes := p.callbacks(implTemplate(impl).Requirements, impl.RequirementEvidence)
				witnesses, witnessTypes := p.implWitnesses(impl)
				children = append(witnesses, children...)
				childTypes = append(witnessTypes, childTypes...)
				fun := p.parameter(p.e.sourceGlobal(impl.File, p.e.templateAssociatedName(impl, associated.Name)), signatureOf(childTypes, ft))
				args = append(args, &goast.CallExpr{Fun: fun, Args: children})
			} else {
				args = append(args, p.parameter(p.e.associatedCallback(selected, bound, associated.Name), ft))
			}
			parameters = append(parameters, ft)
		}
	}
	return args, parameters
}

func (p *traitInference) implWitnesses(impl *check.Impl) ([]goast.Expr, []types.Type) {
	targets := []types.Type{impl.Target}
	if impl.Template != nil {
		targets = append(targets, impl.TypeArgs...)
	} else {
		for _, parameter := range impl.TypeParams {
			targets = append(targets, parameter)
		}
	}
	var args []goast.Expr
	var parameters []types.Type
	for _, target := range targets {
		args = append(args, p.witness(target))
		parameters = append(parameters, signatureOf(nil, target))
	}
	return args, parameters
}

func (p *traitInference) operation(selection *check.MemberSelection) goast.Expr {
	impl := selection.Impl
	if impl == nil || !parameterizedImpl(implTemplate(impl)) {
		return p.parameter(p.e.operationFunction(selection), operationSignature(selection))
	}
	if instance := p.e.traits.instances[instanceImplKey(p.e.info, impl, selection.Member.Name)]; instance != nil {
		return p.parameter(p.e.sourceGlobal(instance.body.File, instance.name), operationSignature(selection))
	}
	args, parameters := p.callbacks(implTemplate(impl).Requirements, impl.RequirementEvidence)
	witnesses, witnessTypes := p.implWitnesses(impl)
	args = append(witnesses, args...)
	parameters = append(witnessTypes, parameters...)
	fun := p.parameter(p.e.sourceGlobal(impl.File, p.e.templateOperationName(selection.Member)), signatureOf(parameters, operationSignature(selection)))
	return &goast.CallExpr{Fun: fun, Args: args}
}

func (e *emitter) functionInstanceSignature(instance *check.FunctionInstance) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, e.info.Substitute(instance.Signature.Params(), instance.Substitution, instance.ConstSubstitution).(*types.Tuple), e.info.Substitute(instance.Signature.Results(), instance.Substitution, instance.ConstSubstitution).(*types.Tuple), instance.Signature.Variadic())
}

func (e *emitter) inferredFunctionCall(call *syntax.CallExpr) (goast.Expr, bool) {
	instance := e.info.FunctionInstances[call]
	if instance == nil {
		return nil, false
	}
	function := e.traits.functions[instance.Signature]
	if function == nil || !e.traits.templateFunctions[function.decl] {
		return nil, false
	}
	if e.traits.instances[instanceFunctionKey(e.info, function, instance)] != nil {
		return nil, false
	}
	if !e.needsFunctionInference(instance) {
		return nil, false
	}
	p := newTraitInference(e)
	sig := e.functionInstanceSignature(instance)
	values := e.exprs(call.Args)
	var arguments []goast.Expr
	for i, value := range values {
		index := i
		if index >= sig.Params().Len() {
			index = sig.Params().Len() - 1
		}
		typ := sig.Params().At(index).Type()
		if sig.Variadic() && index == sig.Params().Len()-1 && !call.Ellipsis {
			typ = typ.(*types.Slice).Elem()
		}
		arguments = append(arguments, p.parameter(value, typ))
	}
	callbacks, callbackTypes := p.callbacks(e.info.GenericBounds[function.decl], instance.Evidence)
	var witnesses []goast.Expr
	var witnessTypes []types.Type
	for _, typ := range instance.TypeArgs {
		witnesses = append(witnesses, p.witness(typ))
		witnessTypes = append(witnessTypes, signatureOf(nil, typ))
	}
	zero := types.NewSignatureType(nil, nil, nil, types.NewTuple(), sig.Results(), false)
	factoryParams := append(witnessTypes, callbackTypes...)
	for i := 0; i < sig.Params().Len(); i++ {
		factoryParams = append(factoryParams, sig.Params().At(i).Type())
	}
	factorySig := signatureOf(factoryParams, zero)
	if sig.Variadic() {
		factorySig = types.NewSignatureType(nil, nil, nil, factorySig.Params(), factorySig.Results(), true)
	}
	factory := e.sourceGlobal(function.symbol.Decl.File, strings.TrimSuffix(constFunctionName(function, instance), "Func")+"Call")
	// A visible leading argument can instantiate an otherwise phantom parameter.
	var prefix []goast.Expr
	for _, typ := range instance.TypeArgs {
		if !e.nameableType(typ) {
			break
		}
		prefix = append(prefix, e.goType(typ))
	}
	factory = enumIndices(factory, prefix)
	fun := p.parameter(factory, factorySig)
	args := append(witnesses, callbacks...)
	args = append(args, arguments...)
	result := &goast.CallExpr{Fun: fun, Args: args}
	if call.Ellipsis {
		result.Ellipsis = 1
	}
	return &goast.CallExpr{Fun: p.finish(result, zero)}, true
}

func (e *emitter) privateEvidence(evidence check.Evidence) bool {
	if evidence.Impl == nil {
		return false
	}
	for _, typ := range evidence.Impl.TypeArgs {
		if !e.nameableType(typ) {
			return true
		}
	}
	for _, nested := range evidence.Impl.RequirementEvidence {
		if e.privateEvidence(nested) {
			return true
		}
	}
	return false
}

// Captured parameters are rebound inside the business closure. A source :=
// therefore updates that parameter rather than shadowing a captured outer name.
func (e *emitter) templateCallDeclaration(funcName string, typeParams, callbacks *goast.FieldList, node *syntax.FuncDecl) goast.Decl {
	name := strings.TrimSuffix(funcName, "Func") + "Call"
	params := &goast.FieldList{List: append([]*goast.Field(nil), callbacks.List...)}
	var setup []goast.Stmt
	for _, parameter := range node.Params {
		typ := e.typ(parameter.Type)
		if parameter.Variadic {
			typ = &goast.Ellipsis{Elt: typ}
		}
		if len(parameter.Names) == 0 {
			params.List = append(params.List, &goast.Field{Names: []*goast.Ident{e.fresh("Captured")}, Type: typ})
		}
		for _, source := range parameter.Names {
			captured := e.fresh("Captured")
			params.List = append(params.List, &goast.Field{Names: []*goast.Ident{captured}, Type: typ})
			used := false
			symbol := e.file.Definitions[source.ID()]
			syntax.Walk(node.Body, func(n syntax.Node) bool {
				if id, ok := n.(*syntax.Ident); ok && e.file.References[id.ID()].Symbol == symbol {
					used = true
				}
				return !used
			})
			if source.Name != "_" && used {
				setup = append(setup, &goast.AssignStmt{Lhs: []goast.Expr{e.ident(source)}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{captured}})
			}
		}
	}
	ft := &goast.FuncType{Params: &goast.FieldList{}, Results: e.params(node.Results)}
	body := e.functionBody(ft, node.Body)
	body.List = append(setup, body.List...)
	business := &goast.FuncLit{Type: ft, Body: body}
	e.generatedName(name, node)
	decl := &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: typeParams, Params: params, Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{business}}}}}
	e.anchor(decl, node)
	return decl
}

func (e *emitter) templateImplWitnessParameters(impl *check.Impl) []*goast.Field {
	typesToWitness := []types.Type{impl.Target}
	for _, parameter := range impl.TypeParams {
		typesToWitness = append(typesToWitness, parameter)
	}
	var fields []*goast.Field
	for _, typ := range typesToWitness {
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent("_")}, Type: resultFunc(e.goType(typ))})
	}
	return fields
}
func (e *emitter) templateImplWitnessArguments(impl *check.Impl) []goast.Expr {
	typesToWitness := []types.Type{impl.Target}
	if impl.Template != nil {
		typesToWitness = append(typesToWitness, impl.TypeArgs...)
	} else {
		for _, parameter := range impl.TypeParams {
			typesToWitness = append(typesToWitness, parameter)
		}
	}
	var args []goast.Expr
	for _, typ := range typesToWitness {
		args = append(args, e.typeWitness(typ))
	}
	return args
}

func (p *traitProgram) prepareInferenceCalls(info *check.Info) {
	p.inferredCalls = map[*syntax.FuncDecl]bool{}
	inspect := func(body *check.BodyInstance) {
		source := info.Resolve.Files[body.File]
		if source == nil || body.Info == nil {
			return
		}
		e := &emitter{info: body.Info, traits: p, name: body.File, tree: source.AST}
		syntax.Walk(body.Node, func(node syntax.Node) bool {
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			if instance := body.Info.FunctionInstances[call]; instance != nil {
				if function := p.functions[instance.Signature]; function != nil && p.instances[instanceFunctionKey(info, function, instance)] == nil && e.needsFunctionInference(instance) {
					p.inferredCalls[function.decl] = true
				}
			}
			if selection := body.Info.Members[call.Fun]; selection != nil && e.needsOperationInference(selection) {
				p.inferredCalls[selection.Member.Decl] = true
			}
			return true
		})
	}
	for _, body := range info.Demands.Roots {
		inspect(body)
	}
	for _, bodies := range info.Demands.Bodies {
		for _, body := range bodies {
			inspect(body)
		}
	}
}
func (e *emitter) needsFunctionInference(instance *check.FunctionInstance) bool {
	for _, typ := range instance.TypeArgs {
		if !e.nameableType(typ) {
			return true
		}
	}
	for _, evidence := range instance.Evidence {
		if e.privateEvidence(evidence) {
			return true
		}
	}
	return false
}
func (e *emitter) needsOperationInference(selection *check.MemberSelection) bool {
	impl := selection.Impl
	return impl != nil && parameterizedImpl(implTemplate(impl)) && e.traits.instances[instanceImplKey(e.info, impl, selection.Member.Name)] == nil && (!e.nameableType(operationSignature(selection)) || e.privateEvidence(check.Evidence{Impl: impl}))
}
func (e *emitter) inferredOperationCall(selection *check.MemberSelection, values []goast.Expr, ellipsis bool) (goast.Expr, bool) {
	if !e.needsOperationInference(selection) {
		return nil, false
	}
	p := newTraitInference(e)
	impl := selection.Impl
	sig := operationSignature(selection)
	var arguments []goast.Expr
	for i, value := range values {
		index := i
		if index >= sig.Params().Len() {
			index = sig.Params().Len() - 1
		}
		typ := sig.Params().At(index).Type()
		if sig.Variadic() && index == sig.Params().Len()-1 && !ellipsis {
			typ = typ.(*types.Slice).Elem()
		}
		arguments = append(arguments, p.parameter(value, typ))
	}
	callbacks, callbackTypes := p.callbacks(implTemplate(impl).Requirements, impl.RequirementEvidence)
	witnesses, witnessTypes := p.implWitnesses(impl)
	zero := types.NewSignatureType(nil, nil, nil, types.NewTuple(), sig.Results(), false)
	params := append(witnessTypes, callbackTypes...)
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i).Type())
	}
	factorySig := signatureOf(params, zero)
	if sig.Variadic() {
		factorySig = types.NewSignatureType(nil, nil, nil, factorySig.Params(), factorySig.Results(), true)
	}
	factory := e.sourceGlobal(impl.File, strings.TrimSuffix(e.templateOperationName(selection.Member), "Func")+"Call")
	fun := p.parameter(factory, factorySig)
	args := append(witnesses, callbacks...)
	args = append(args, arguments...)
	result := &goast.CallExpr{Fun: fun, Args: args}
	if ellipsis {
		result.Ellipsis = 1
	}
	return &goast.CallExpr{Fun: p.finish(result, zero)}, true
}
