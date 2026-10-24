package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"reflect"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) traitDecl(n *syntax.TraitDecl) goast.Decl {
	trait := e.info.Traits[n]
	if trait == nil {
		e.fail(n, "缺少 trait 检查结果")
		return nil
	}
	if !trait.Runtime {
		return nil
	}
	var methods []*goast.Field
	for _, m := range trait.Methods {
		if !m.Static {
			ft, _ := e.memberSignature(m.Signature)
			methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(m.GoName)}, Type: ft})
		}
	}
	var arguments []types.Type
	for _, parameter := range trait.TypeParams {
		arguments = append(arguments, parameter)
	}
	for _, associated := range trait.Associated {
		arguments = append(arguments, associated.Param)
	}
	methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(trait.Marker)}, Type: e.traitMarkerSignature(arguments)})
	spec := &goast.TypeSpec{Name: goast.NewIdent(trait.Symbol.GoName), Type: &goast.InterfaceType{Methods: &goast.FieldList{List: methods}}}
	if len(arguments) != 0 {
		spec.TypeParams = &goast.FieldList{}
		for _, parameter := range trait.TypeParams {
			spec.TypeParams.List = append(spec.TypeParams.List, &goast.Field{Names: []*goast.Ident{e.goType(parameter).(*goast.Ident)}, Type: e.goConstraint(parameter.Constraint())})
		}
		for _, a := range trait.Associated {
			spec.TypeParams.List = append(spec.TypeParams.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(a.Param.Obj().Name())}, Type: goast.NewIdent("any")})
		}
	}
	return &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{spec}}
}

func (e *emitter) traitDeclarations(declarations []goast.Decl) []goast.Decl {
	var out []goast.Decl
	for _, impl := range e.traits.impls {
		if impl.File != e.name {
			continue
		}
		if impl.Trait == nil {
			if parameterizedImpl(impl) && len(impl.Requirements) != 0 || e.info.Demands != nil && e.info.Demands.ImplTemplates[impl] {
				continue
			}
			for _, m := range impl.Methods {
				if nativeEnumMember(m) {
					out = append(out, e.enumInherentDeclarations(m)...)
					continue
				}
				out = append(out, e.inherentDeclaration(m))
				if e.traits.memberNeedsBinding(m) && !parameterizedImpl(impl) {
					out = append(out, e.bindingDeclaration(m, nil))
				}
			}
			continue
		}
		if parameterizedImpl(impl) {
			if genericPairOps(e.info, impl) {
				out = append(out, e.implOpsDeclarations(impl)...)
			}
			if impl.Trait.Runtime || e.traits.implNeedsBindings(impl) {
				out = append(out, e.templateRuntimeDeclarations(impl)...)
			}
		} else {
			out = append(out, e.implDeclarations(impl)...)
		}
	}
	var carriers []*traitCarrier
	for _, carrier := range e.traits.carriers {
		if carrier.file == e.name {
			carriers = append(carriers, carrier)
		}
	}
	sort.Slice(carriers, func(i, j int) bool { return carriers[i].name < carriers[j].name })
	for _, carrier := range carriers {
		out = append(out, e.carrierDeclarations(carrier)...)
	}
	out = append(out, e.inherentCarrierDeclarations()...)
	var facades []*traitFacade
	for _, facade := range e.traits.facades {
		if facade.file == e.name {
			facades = append(facades, facade)
		}
	}
	sort.Slice(facades, func(i, j int) bool { return targetName(facades[i].impls[0]) < targetName(facades[j].impls[0]) })
	for _, facade := range facades {
		out = append(out, e.facadeDeclarations(facade)...)
	}
	out = append(out, e.sugarDeclarations()...)
	for _, decl := range out {
		if source := e.traitBodySources[decl]; source != nil {
			e.anchor(decl, source)
			// Keep source comments next to the business body. Only generated
			// functions are inserted; existing declarations retain their order.
			position := e.pos(source)
			index := len(declarations)
			for i, existing := range declarations {
				existingPosition := existing.Pos()
				if existingSource := e.traitBodySources[existing]; existingSource != nil {
					existingPosition = e.pos(existingSource)
				}
				if existingPosition.IsValid() && existingPosition > position {
					index = i
					break
				}
			}
			declarations = append(declarations, nil)
			copy(declarations[index+1:], declarations[index:])
			declarations[index] = decl
			continue
		}
		if e.validation == nil {
			clearTraitPositions(decl)
		} else {
			e.anchor(decl, e.tree)
		}
		declarations = append(declarations, decl)
	}
	return declarations
}

func (e *emitter) traitSourceBody(declaration *goast.FuncDecl, source syntax.Node) *goast.FuncDecl {
	if e.traitBodySources == nil {
		e.traitBodySources = make(map[goast.Decl]syntax.Node)
	}
	e.traitBodySources[declaration] = source
	return declaration
}

func clearTraitPositions(node goast.Node) {
	var semantic []*gotoken.Pos
	goast.Inspect(node, func(node goast.Node) bool {
		switch n := node.(type) {
		case *goast.CallExpr:
			if n.Ellipsis != 0 {
				semantic = append(semantic, &n.Ellipsis)
			}
		case *goast.TypeSpec:
			if n.Assign != 0 {
				semantic = append(semantic, &n.Assign)
			}
		case *goast.GenDecl:
			if n.Lparen != 0 {
				semantic = append(semantic, &n.Lparen)
			}
		}
		return true
	})
	clearGoPositions(reflect.ValueOf(node))
	for _, position := range semantic {
		*position = 1
	}
}

func (e *emitter) implDeclarations(impl *check.Impl) []goast.Decl {
	var out []goast.Decl
	parameters := e.implGenerics(impl)
	instances := false
	for _, m := range impl.Methods {
		instances = instances || !m.Static
	}
	if instances || impl.Trait.Runtime {
		name := e.traitWrapper(impl)
		e.generatedName(name, impl.Decl)
		out = append(out, genericTypeDeclaration(name, parameters, &goast.StructType{Fields: receiverField("target", e.goType(captureType(impl)))}))
		for _, m := range impl.Methods {
			if !m.Static {
				out = append(out, e.wrapperMethod(m))
			}
		}
	}
	if impl.Trait.Runtime {
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(impl.Trait.Marker), Recv: receiverField("", e.implWrapperType(impl)), Type: e.traitMarkerSignature(traitViewArguments(impl)), Body: &goast.BlockStmt{}})
		out = append(out, e.factoryDeclaration(impl))
		local := impl.Base != nil && e.localTypePackage(impl.Base.Obj().Pkg())
		if local && !parameterizedTrait(impl.Trait) && nativeInherentImpl(impl) {
			e.generatedMember(impl.Base, traitAs(impl), impl)
			out = append(out, e.asDeclaration(impl, captureType(impl), nil))
		}
	}
	if !carrierTarget(e.info, impl) {
		out = append(out, e.implOpsDeclarations(impl)...)
	}
	for _, m := range impl.Methods {
		carrier := e.traits.carriers[impl.Base]
		if carrier == nil || !carrierTarget(e.info, impl) {
			// Non-parameterized protocols retain their exported free operation.
			// Parameterized protocols expose the method on their complete pair ops.
			if !parameterizedTrait(impl.Trait) && !parameterizedImpl(impl) && impl.Base != nil && impl.Base.TypeParams().Len() == 0 && impl.Base.TypeArgs().Len() == 0 {
				out = append(out, e.operationDeclaration(m, nil))
			}
		} else if carrier.file != e.name {
			out = append(out, e.operationDeclaration(m, carrier))
			if e.traits.memberNeedsBinding(m) {
				out = append(out, e.bindingDeclaration(m, carrier))
			}
		}
	}
	return out
}

func (e *emitter) implOpsDeclarations(impl *check.Impl) []goast.Decl {
	name := e.implOpsName(impl)
	e.generatedName(name, impl.Decl)
	out := []goast.Decl{genericTypeDeclaration(name, e.implGenerics(impl), &goast.StructType{Fields: &goast.FieldList{}})}
	for _, associated := range impl.Trait.Associated {
		result := e.goType(impl.Associated[associated.Name])
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(traitWitness(impl.Trait, associated.Name)), Recv: receiverField("", e.implOpsType(impl)), Type: resultFunc(result), Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(goast.NewIdent("value"), result, nil), &goast.ReturnStmt{Results: []goast.Expr{goast.NewIdent("value")}}}}})
	}
	for _, member := range impl.Methods {
		declaration := e.operationDeclaration(member, &traitCarrier{name: name}).(*goast.FuncDecl)
		declaration.Recv = receiverField("", e.implOpsType(impl))
		out = append(out, declaration)
		if e.traits.memberNeedsBinding(member) {
			binding := e.bindingDeclaration(member, &traitCarrier{name: name}).(*goast.FuncDecl)
			binding.Recv = receiverField("", e.implOpsType(impl))
			out = append(out, binding)
		}
	}
	return out
}

func captureType(impl *check.Impl) types.Type {
	if impl.Capture != check.ValueRef {
		return types.NewPointer(impl.Target)
	}
	return impl.Target
}

func (e *emitter) traitViewType(impl *check.Impl) goast.Expr {
	var args []goast.Expr
	for _, argument := range traitViewArguments(impl) {
		args = append(args, e.goType(argument))
	}
	return enumIndices(e.sourceGlobal(impl.Trait.File, impl.Trait.Symbol.GoName), args)
}

func traitViewArguments(impl *check.Impl) []types.Type {
	args := append([]types.Type(nil), impl.Inputs...)
	for _, associated := range impl.Trait.Associated {
		args = append(args, impl.Associated[associated.Name])
	}
	return args
}

func (e *emitter) traitMarkerSignature(arguments []types.Type) *goast.FuncType {
	params := &goast.FieldList{}
	for _, argument := range arguments {
		params.List = append(params.List, &goast.Field{Type: e.goType(argument)})
	}
	return &goast.FuncType{Params: params}
}

func (e *emitter) wrapperMethod(m *check.Member) *goast.FuncDecl {
	restore := e.memberBodyInfo(m)
	defer restore()
	impl := m.Impl
	view := e.fresh("View").Name
	previousReceiver := e.callbackReceiver
	if parameterizedImpl(impl) {
		e.callbackReceiver = view
	}
	defer func() { e.callbackReceiver = previousReceiver }()
	decl := &goast.FuncDecl{Name: goast.NewIdent(m.GoName), Recv: receiverField(view, e.implWrapperType(impl))}
	var target goast.Expr = &goast.SelectorExpr{X: goast.NewIdent(view), Sel: goast.NewIdent("target")}
	if m.Receiver == check.ValueRef && impl.Capture != check.ValueRef {
		target = &goast.StarExpr{X: target}
	}
	decl.Type = &goast.FuncType{Params: e.params(m.Decl.Params[1:]), Results: e.params(m.Decl.Results)}
	decl.Body = e.functionBody(decl.Type, m.Decl.Body)
	self := e.ident(m.Decl.Params[0].Names[0])
	selfSymbol := e.file.Definitions[m.Decl.Params[0].Names[0].ID()]
	used := false
	syntax.Walk(m.Decl.Body, func(node syntax.Node) bool {
		if id, ok := node.(*syntax.Ident); ok && e.file.References[id.ID()].Symbol == selfSymbol && selfSymbol != nil {
			used = true
		}
		return true
	})
	if used {
		decl.Body.List = append([]goast.Stmt{&goast.AssignStmt{Lhs: []goast.Expr{self}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{target}}}, decl.Body.List...)
	}
	return e.traitSourceBody(decl, m.Decl)
}

func (e *emitter) factoryDeclaration(impl *check.Impl) goast.Decl {
	name := e.traitFactoryName(impl)
	e.generatedName(name, impl.Decl)
	return &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: e.implGenerics(impl), Params: receiverField("target", e.goType(captureType(impl))), Results: &goast.FieldList{List: []*goast.Field{{Type: e.traitViewType(impl)}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CompositeLit{Type: e.implWrapperType(impl), Elts: []goast.Expr{&goast.KeyValueExpr{Key: goast.NewIdent("target"), Value: goast.NewIdent("target")}}}}}}}}
}

// A nil expression means the named receiver itself is the factory argument.
func (e *emitter) asDeclaration(impl *check.Impl, receiver types.Type, argument goast.Expr) *goast.FuncDecl {
	if argument == nil {
		argument = goast.NewIdent("target")
	}
	return &goast.FuncDecl{Name: goast.NewIdent(traitAs(impl)), Recv: receiverField("target", e.goType(receiver)), Type: resultFunc(e.traitViewType(impl)), Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CallExpr{Fun: e.implFactory(impl), Args: []goast.Expr{argument}}}}}}}
}

func (e *emitter) carrierDeclarations(carrier *traitCarrier) []goast.Decl {
	owner := carrier.impls[0].Decl
	e.generatedName(carrier.name, owner)
	var out = []goast.Decl{typeDeclaration(carrier.name, &goast.StructType{Fields: &goast.FieldList{}})}
	publicName := carrierExportedName(carrier)
	e.generatedName(publicName, owner)
	out = append(out, &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{&goast.TypeSpec{Name: goast.NewIdent(publicName), Assign: 1, Type: goast.NewIdent(carrier.name)}}})
	if e.info.EnumOf(carrier.typ) == nil {
		e.generatedMember(carrier.typ, typeCarrierAnchor, carrier.impls[0])
		out = append(out, e.carrierAnchor(carrier, e.goType(carrier.typ)))
	}
	short := map[string]int{}
	for _, impl := range carrier.impls {
		for _, m := range impl.Methods {
			if m.Static {
				short[exportedName(m.GoName)]++
			}
		}
	}
	for _, impl := range carrier.impls {
		for _, a := range impl.Trait.Associated {
			result := e.goType(impl.Associated[a.Name])
			out = append(out, &goast.FuncDecl{Name: goast.NewIdent(traitWitness(impl.Trait, a.Name)), Recv: receiverField("", goast.NewIdent(carrier.name)), Type: resultFunc(result), Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(goast.NewIdent("value"), result, nil), &goast.ReturnStmt{Results: []goast.Expr{goast.NewIdent("value")}}}}})
		}
		for _, m := range impl.Methods {
			// Bodies stay in the source impl file to retain its name-resolution context.
			if impl.File == e.name {
				out = append(out, e.operationDeclaration(m, carrier))
				if e.traits.memberNeedsBinding(m) {
					out = append(out, e.bindingDeclaration(m, carrier))
				}
			}
			if m.Static && short[exportedName(m.GoName)] == 1 {
				ft, args := e.memberSignature(m.Signature)
				ops := e.fresh("Ops")
				out = append(out, &goast.FuncDecl{Name: goast.NewIdent(exportedName(m.GoName)), Recv: receiverField(ops.Name, goast.NewIdent(carrier.name)), Type: ft, Body: forwardBody(&goast.SelectorExpr{X: ops, Sel: goast.NewIdent(traitOperation(m))}, args, m.Signature)})
			}
		}
	}
	return out
}

func (e *emitter) carrierAnchor(carrier *traitCarrier, receiver goast.Expr) *goast.FuncDecl {
	return &goast.FuncDecl{Name: goast.NewIdent(typeCarrierAnchor), Recv: receiverField("", receiver), Type: &goast.FuncType{Params: &goast.FieldList{List: []*goast.Field{{Type: e.goType(carrier.typ)}}}, Results: &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent(carrier.name)}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CompositeLit{Type: goast.NewIdent(carrier.name)}}}}}}
}

func (e *emitter) operationDeclaration(m *check.Member, carrier *traitCarrier) goast.Decl {
	restore := e.memberBodyInfo(m)
	defer restore()
	name := traitOperation(m)
	decl := &goast.FuncDecl{Name: goast.NewIdent(name)}
	if carrier == nil {
		name = e.traitFreeOperation(m)
		e.generatedName(name, m.Impl.Decl)
		decl.Name = goast.NewIdent(name)
	} else {
		decl.Recv = receiverField("", goast.NewIdent(carrier.name))
	}
	decl.Type = &goast.FuncType{Params: e.params(m.Decl.Params), Results: e.params(m.Decl.Results)}
	decl.Body = e.functionBody(decl.Type, m.Decl.Body)
	return e.traitSourceBody(decl, m.Decl)
}

func (e *emitter) inherentDeclaration(m *check.Member) goast.Decl {
	restore := e.memberBodyInfo(m)
	defer restore()
	parameters := e.implGenerics(m.Impl)
	decl := &goast.FuncDecl{Name: goast.NewIdent(e.inherentName(m)), Type: &goast.FuncType{TypeParams: parameters, Params: e.params(m.Decl.Params), Results: e.params(m.Decl.Results)}}
	decl.Body = e.functionBody(decl.Type, m.Decl.Body)
	if m.Static {
		decl.Name = goast.NewIdent(m.GoName)
		decl.Recv = receiverField("", e.inherentCarrierType(m.Impl))
		decl.Type.TypeParams = nil
	} else if nativeInherentImpl(m.Impl) {
		decl.Name = goast.NewIdent(m.GoName)
		decl.Recv = e.params(m.Decl.Params[:1])
		decl.Type.Params = e.params(m.Decl.Params[1:])
		decl.Type.TypeParams = nil
	} else {
		e.generatedName(decl.Name.Name, m.Decl)
		e.guardEnumInherentBody(m, decl.Body)
	}
	return e.traitSourceBody(decl, m.Decl)
}

func (e *emitter) facadeDeclarations(facade *traitFacade) []goast.Decl {
	first := facade.impls[0]
	name, factory := "goxideFor"+targetName(first), "For"+targetName(first)
	e.generatedName(name, first.Decl)
	e.generatedName(factory, first.Decl)
	ptr := &goast.StarExpr{X: e.goType(facade.typ)}
	out := []goast.Decl{typeDeclaration(name, &goast.StructType{Fields: receiverField("target", ptr)}), &goast.FuncDecl{Name: goast.NewIdent(factory), Type: &goast.FuncType{Params: receiverField("target", ptr), Results: &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent(name)}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CompositeLit{Type: goast.NewIdent(name), Elts: []goast.Expr{&goast.KeyValueExpr{Key: goast.NewIdent("target"), Value: goast.NewIdent("target")}}}}}}}}}
	seen := map[string]bool{}
	for _, impl := range facade.impls {
		if seen[traitAs(impl)] {
			e.fail(impl.Decl, "生成的 Go 方法 "+traitAs(impl)+" 与另一入口冲突")
			continue
		}
		seen[traitAs(impl)] = true
		var arg goast.Expr = &goast.SelectorExpr{X: goast.NewIdent("view"), Sel: goast.NewIdent("target")}
		if types.Identical(captureType(impl), facade.typ) {
			arg = &goast.StarExpr{X: arg}
		} else if !types.Identical(captureType(impl), types.NewPointer(facade.typ)) {
			// A pointer Self retains its additional receiver indirection.
			arg = &goast.UnaryExpr{Op: gotoken.AND, X: arg}
		}
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(traitAs(impl)), Recv: receiverField("view", goast.NewIdent(name)), Type: resultFunc(e.traitViewType(impl)), Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CallExpr{Fun: e.implFactory(impl), Args: []goast.Expr{arg}}}}}}})
	}
	return out
}
