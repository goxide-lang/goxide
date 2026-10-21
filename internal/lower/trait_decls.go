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
	methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(trait.Marker)}, Type: &goast.FuncType{Params: &goast.FieldList{}}})
	spec := &goast.TypeSpec{Name: goast.NewIdent(trait.Symbol.GoName), Type: &goast.InterfaceType{Methods: &goast.FieldList{List: methods}}}
	if len(trait.Associated) != 0 {
		spec.TypeParams = &goast.FieldList{}
		for _, a := range trait.Associated {
			spec.TypeParams.List = append(spec.TypeParams.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(a.Param.Obj().Name())}, Type: goast.NewIdent("any")})
		}
	}
	return &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{spec}}
}

func (e *emitter) traitDeclarations() []goast.Decl {
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
				out = append(out, e.inherentDeclaration(m))
			}
			continue
		}
		if parameterizedImpl(impl) {
			if impl.Trait.Runtime {
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
		clearTraitPositions(decl)
	}
	return out
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
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(impl.Trait.Marker), Recv: receiverField("", e.implWrapperType(impl)), Type: &goast.FuncType{Params: &goast.FieldList{}}, Body: &goast.BlockStmt{}})
		out = append(out, e.factoryDeclaration(impl))
		local := impl.Base != nil && e.localTypePackage(impl.Base.Obj().Pkg())
		if local && nativeInherentImpl(impl) {
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
			// Existing non-carrier impls retain their exported free operation.
			if !parameterizedImpl(impl) && impl.Base != nil && impl.Base.TypeParams().Len() == 0 && impl.Base.TypeArgs().Len() == 0 {
				out = append(out, e.operationDeclaration(m, nil))
			}
		} else if carrier.file != e.name {
			out = append(out, e.operationDeclaration(m, carrier))
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
	legacyFree := !parameterizedImpl(impl) && impl.Base != nil && impl.Base.TypeParams().Len() == 0 && impl.Base.TypeArgs().Len() == 0
	for _, member := range impl.Methods {
		var declaration *goast.FuncDecl
		if legacyFree {
			ft, args := e.memberSignature(member.Signature)
			if !member.Static {
				receiver := impl.Target
				if member.Receiver != check.ValueRef {
					receiver = types.NewPointer(receiver)
				}
				ft.Params.List = append([]*goast.Field{{Names: []*goast.Ident{goast.NewIdent("self")}, Type: e.goType(receiver)}}, ft.Params.List...)
				args = append([]goast.Expr{goast.NewIdent("self")}, args...)
			}
			declaration = &goast.FuncDecl{Name: goast.NewIdent(traitOperation(member)), Type: ft, Body: forwardBody(e.sourceGlobal(impl.File, e.traitFreeOperation(member)), args, member.Signature)}
		} else {
			declaration = e.operationDeclaration(member, &traitCarrier{name: name}).(*goast.FuncDecl)
		}
		declaration.Recv = receiverField("", e.implOpsType(impl))
		out = append(out, declaration)
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
	for _, a := range impl.Trait.Associated {
		args = append(args, e.goType(impl.Associated[a.Name]))
	}
	return enumIndices(e.sourceGlobal(impl.Trait.File, impl.Trait.Symbol.GoName), args)
}

func (e *emitter) wrapperMethod(m *check.Member) *goast.FuncDecl {
	restore := e.memberBodyInfo(m)
	defer restore()
	impl := m.Impl
	var nativeType *goast.FuncType
	var nativeArgs []goast.Expr
	if impl.Declare {
		nativeType, nativeArgs = e.memberSignature(m.Signature)
	}
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
	if impl.Declare {
		decl.Type = nativeType
		decl.Body = forwardBody(&goast.SelectorExpr{X: target, Sel: goast.NewIdent(m.Native.Name())}, nativeArgs, m.Signature)
		return decl
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
	return decl
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
	if m.Static {
		decl.Type = &goast.FuncType{Params: e.params(m.Decl.Params), Results: e.params(m.Decl.Results)}
		decl.Body = e.functionBody(decl.Type, m.Decl.Body)
		return decl
	}
	ft, args := e.memberSignature(m.Signature)
	receiver := m.Impl.Target
	if m.Receiver != check.ValueRef {
		receiver = types.NewPointer(receiver)
	}
	ft.Params.List = append([]*goast.Field{{Names: []*goast.Ident{goast.NewIdent("self")}, Type: e.goType(receiver)}}, ft.Params.List...)
	var target goast.Expr = goast.NewIdent("self")
	if m.Receiver == check.ValueRef && m.Impl.Capture != check.ValueRef {
		target = &goast.UnaryExpr{Op: gotoken.AND, X: target}
	}
	var wrapper goast.Expr = &goast.CompositeLit{Type: e.implWrapperType(m.Impl), Elts: []goast.Expr{&goast.KeyValueExpr{Key: goast.NewIdent("target"), Value: target}}}
	if m.Impl.Trait.Runtime {
		wrapper = &goast.CallExpr{Fun: e.implFactory(m.Impl), Args: []goast.Expr{target}}
	}
	decl.Type = ft
	decl.Body = forwardBody(&goast.SelectorExpr{X: wrapper, Sel: goast.NewIdent(m.GoName)}, args, m.Signature)
	return decl
}

func (e *emitter) inherentDeclaration(m *check.Member) goast.Decl {
	restore := e.memberBodyInfo(m)
	defer restore()
	parameters := e.implGenerics(m.Impl)
	decl := &goast.FuncDecl{Name: goast.NewIdent(e.inherentName(m)), Type: &goast.FuncType{TypeParams: parameters, Params: e.params(m.Decl.Params), Results: e.params(m.Decl.Results)}}
	decl.Body = e.functionBody(decl.Type, m.Decl.Body)
	if !m.Static && nativeInherentImpl(m.Impl) {
		decl.Name = goast.NewIdent(m.GoName)
		decl.Recv = e.params(m.Decl.Params[:1])
		decl.Type.Params = e.params(m.Decl.Params[1:])
		decl.Type.TypeParams = nil
	} else {
		e.generatedName(decl.Name.Name, m.Decl)
	}
	return decl
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
			// The full Self of `declare impl ... for *T` remains *T.
			arg = &goast.UnaryExpr{Op: gotoken.AND, X: arg}
		}
		out = append(out, &goast.FuncDecl{Name: goast.NewIdent(traitAs(impl)), Recv: receiverField("view", goast.NewIdent(name)), Type: resultFunc(e.traitViewType(impl)), Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{&goast.CallExpr{Fun: e.implFactory(impl), Args: []goast.Expr{arg}}}}}}})
	}
	return out
}
