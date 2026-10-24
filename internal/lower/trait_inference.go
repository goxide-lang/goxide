package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"
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
	constraints    map[types.Type][]types.Type
}

func newTraitInference(e *emitter) *traitInference {
	return &traitInference{e: e, bodyTypes: map[types.Type]*goast.Ident{}, constraints: map[types.Type][]types.Type{}}
}

func (p *traitInference) collect(typ types.Type) {
	if typ == nil {
		return
	}
	switch typ := typ.(type) {
	case *types.Named:
		// Keep a public type constructor related to its private arguments. Treating
		// Box[hidden] and hidden as unrelated type parameters loses Go inference for
		// a generic factory that accepts both func(hidden) and Box[hidden].
		if typ.TypeArgs().Len() != 0 && p.e.nameableType(typ.Origin()) {
			substitution := map[*types.TypeParam]types.Type{}
			for index := 0; index < typ.TypeArgs().Len(); index++ {
				substitution[typ.TypeParams().At(index)] = typ.TypeArgs().At(index)
			}
			for index := 0; index < typ.TypeArgs().Len(); index++ {
				argument := typ.TypeArgs().At(index)
				p.collect(argument)
				constraint := p.e.info.Substitute(typ.TypeParams().At(index).Constraint(), substitution, nil)
				if !types.Identical(constraint, types.Universe.Lookup("any").Type()) {
					p.constraints[argument] = appendAssertionType(p.constraints[argument], constraint)
				}
			}
			return
		}
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
	if typ == nil {
		return false
	}
	switch t := types.Unalias(typ).(type) {
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
	case *types.Struct:
		for index := 0; index < t.NumFields(); index++ {
			if inferenceHasParameter(t.Field(index).Type()) {
				return true
			}
		}

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

// Packing keeps the source call in its original frame while binding every
// result alongside protocol evidence, including types private to other packages.
func (p *traitInference) tupleParameter(value goast.Expr, tuple *types.Tuple) []goast.Expr {
	p.collect(tuple)
	typeParams := &goast.FieldList{}
	params := &goast.FieldList{}
	fields := &goast.FieldList{}
	var values []goast.Expr
	var fieldTypes []*types.Var
	for i := 0; i < tuple.Len(); i++ {
		typ := p.e.fresh("Element")
		name := p.e.fresh("Value")
		field := goast.NewIdent("Value" + strconv.Itoa(i))
		typeParams.List = append(typeParams.List, &goast.Field{Names: []*goast.Ident{typ}, Type: goast.NewIdent("any")})
		params.List = append(params.List, &goast.Field{Names: []*goast.Ident{name}, Type: typ})
		fields.List = append(fields.List, &goast.Field{Names: []*goast.Ident{field}, Type: typ})
		values = append(values, &goast.KeyValueExpr{Key: field, Value: name})
		fieldTypes = append(fieldTypes, types.NewField(0, nil, field.Name, tuple.At(i).Type(), false))
	}
	result := &goast.StructType{Fields: fields}
	pack := p.e.fresh("Pack")
	p.e.flowHelpers = append(p.e.flowHelpers, &goast.FuncDecl{
		Name: pack,
		Type: &goast.FuncType{TypeParams: typeParams, Params: params, Results: &goast.FieldList{List: []*goast.Field{{Type: result}}}},
		Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CompositeLit{Type: result, Elts: values}}}}},
	})
	name := p.e.fresh("Argument")
	boundFields := &goast.FieldList{}
	previous := p.e.inferenceTypes
	p.e.inferenceTypes = p.bodyTypes
	for _, field := range fieldTypes {
		boundFields.List = append(boundFields.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(field.Name())}, Type: p.e.goType(field.Type())})
	}
	p.e.inferenceTypes = previous
	p.fields = append(p.fields, &goast.Field{Names: []*goast.Ident{name}, Type: &goast.StructType{Fields: boundFields}})
	p.arguments = append(p.arguments, &goast.CallExpr{Fun: pack, Args: []goast.Expr{value}})
	p.parameterTypes = append(p.parameterTypes, types.NewStruct(fieldTypes, nil))
	var arguments []goast.Expr
	for _, field := range fieldTypes {
		arguments = append(arguments, &goast.SelectorExpr{X: name, Sel: goast.NewIdent(field.Name())})
	}
	return arguments
}

func (p *traitInference) finish(result goast.Expr, typ types.Type) goast.Expr {
	p.collect(typ)
	previous := p.e.inferenceTypes
	p.e.inferenceTypes = p.bodyTypes
	defer func() { p.e.inferenceTypes = previous }()
	parameters := &goast.FieldList{}
	for index, name := range p.names {
		var constraint goast.Expr = goast.NewIdent("any")
		if requirements := p.constraints[p.types[index]]; len(requirements) != 0 {
			methods := &goast.FieldList{}
			for _, required := range requirements {
				methods.List = append(methods.List, &goast.Field{Type: p.e.goType(required)})
			}
			constraint = &goast.InterfaceType{Methods: methods}
		}
		parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{name}, Type: constraint})
	}
	for i, field := range p.fields {
		if field.Type == nil {
			field.Type = p.e.goType(p.parameterTypes[i])
		}
	}
	if len(parameters.List) == 0 {
		parameters = nil
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
		if len(selected.Cases) != 0 {
			p.e.fail(bound.Trait.Decl, "trait 条件证据尚未实例化为操作或约束回调")
			return nil, nil
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
			args = append(args, p.operation(selection))
			parameters = append(parameters, operationSignature(selection))
			if !method.Static && p.e.traits.boundNeedsBinding(bound, method.Name) {
				args = append(args, p.binding(selection))
				parameters = append(parameters, p.e.bindingSignature(selection))
			}
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
		return p.parameter(p.e.instanceOperation(instance), operationSignature(selection))
	}
	member := selectedMember(selection)
	bounds, evidence := operationCallbacks(member)
	args, parameters := p.callbacks(bounds, evidence)
	enumArgs, enumTypes := p.enumChecks(p.e.memberEnumChecks(member))
	args, parameters = append(args, enumArgs...), append(parameters, enumTypes...)
	witnesses, witnessTypes := p.operationWitnesses(member)
	args = append(witnesses, args...)
	parameters = append(witnessTypes, parameters...)
	fun := p.parameter(p.e.sourceGlobal(impl.File, p.e.templateOperationName(member)), signatureOf(parameters, operationSignature(selection)))
	return &goast.CallExpr{Fun: fun, Args: args}
}

func (e *emitter) functionInstanceSignature(instance *check.FunctionInstance) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, e.info.Substitute(instance.Signature.Params(), instance.Substitution, instance.ConstSubstitution).(*types.Tuple), e.info.Substitute(instance.Signature.Results(), instance.Substitution, instance.ConstSubstitution).(*types.Tuple), instance.Signature.Variadic())
}

func (e *emitter) inferredFunctionCall(call *syntax.CallExpr) (goast.Expr, bool) {
	instance := e.functionInstance(call)
	if instance == nil {
		return nil, false
	}
	function := e.traits.functions[instance.Signature]
	if function == nil || !e.traits.templateFunctions[function.decl] {
		return nil, false
	}
	if e.carrierFunctionInstance(function, instance) {
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
	if len(call.Args) == 1 && !call.Ellipsis {
		if tuple, ok := e.info.Types[call.Args[0]].(*types.Tuple); ok && tuple.Len() > 1 {
			arguments = p.tupleParameter(values[0], tuple)
		}
	}
	if arguments == nil {
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
	}
	bounds, evidence := e.functionCallbacks(function, instance)
	callbacks, callbackTypes := p.callbacks(bounds, evidence)
	enumArgs, enumTypes := p.enumChecks(e.functionEnumChecks(function, instance))
	callbacks, callbackTypes = append(callbacks, enumArgs...), append(callbackTypes, enumTypes...)
	var witnesses []goast.Expr
	var witnessTypes []types.Type
	for _, typ := range functionCaseArguments(instance) {
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
	for _, typ := range functionCaseArguments(instance) {
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
	typesToName := append([]types.Type{evidence.Impl.Target}, evidence.Impl.TypeArgs...)
	typesToName = append(typesToName, evidence.Impl.Inputs...)
	for _, typ := range evidence.Impl.Associated {
		typesToName = append(typesToName, typ)
	}
	for _, typ := range typesToName {
		if !e.nameableType(typ) {
			return true
		}
	}
	for _, nested := range evidence.Impl.RequirementEvidence {
		if e.privateEvidence(nested) {
			return true
		}
	}
	for _, branch := range evidence.Impl.MethodCaseEvidence {
		for _, nested := range branch {
			if e.privateEvidence(nested) {
				return true
			}
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
	p.inferredOperations = map[*syntax.FuncDecl]bool{}
	inspect := func(body *check.BodyInstance) {
		source := info.Resolve.Files[body.File]
		if source == nil || body.Info == nil {
			return
		}
		e := &emitter{info: body.Info, traits: p, name: body.File, tree: source.AST}
		syntax.Walk(body.Node, func(node syntax.Node) bool {
			if expression, ok := node.(syntax.Expr); ok {
				if selection := body.Info.Members[expression]; selection != nil && e.needsOperationInference(selection) {
					p.inferredOperations[selection.Member.Decl] = true
				}
			}
			call, ok := node.(*syntax.CallExpr)
			if !ok {
				return true
			}
			if instance := e.functionInstance(call); instance != nil {
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
	for _, branches := range info.BodyCases {
		for _, branch := range branches {
			inspect(&check.BodyInstance{Node: branch.Decl, Decl: branch.Decl, File: branch.File, Info: branch.Info, Case: branch})
		}
	}
	for _, bodies := range info.Demands.Bodies {
		for _, body := range bodies {
			inspect(body)
		}
	}
}
func (e *emitter) needsFunctionInference(instance *check.FunctionInstance) bool {
	for _, typ := range functionCaseArguments(instance) {
		if !e.nameableType(typ) {
			return true
		}
	}
	for _, evidence := range append(append([]check.Evidence(nil), instance.Evidence...), instance.CaseEvidence...) {
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
	member := selectedMember(selection)
	bounds, evidence := operationCallbacks(member)
	callbacks, callbackTypes := p.callbacks(bounds, evidence)
	enumArgs, enumTypes := p.enumChecks(e.memberEnumChecks(member))
	callbacks, callbackTypes = append(callbacks, enumArgs...), append(callbackTypes, enumTypes...)
	witnesses, witnessTypes := p.operationWitnesses(member)
	zero := types.NewSignatureType(nil, nil, nil, types.NewTuple(), sig.Results(), false)
	params := append(witnessTypes, callbackTypes...)
	for i := 0; i < sig.Params().Len(); i++ {
		params = append(params, sig.Params().At(i).Type())
	}
	factorySig := signatureOf(params, zero)
	if sig.Variadic() {
		factorySig = types.NewSignatureType(nil, nil, nil, factorySig.Params(), factorySig.Results(), true)
	}
	factory := e.sourceGlobal(impl.File, strings.TrimSuffix(e.templateOperationName(member), "Func")+"Call")
	fun := p.parameter(factory, factorySig)
	args := append(witnesses, callbacks...)
	args = append(args, arguments...)
	result := &goast.CallExpr{Fun: fun, Args: args}
	if ellipsis {
		result.Ellipsis = 1
	}
	return &goast.CallExpr{Fun: p.finish(result, zero)}, true
}
