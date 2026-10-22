package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Binding an instance method is separate from calling its unbound operation:
// the saved function must enter the business body without a forwarding frame.
func (p *traitProgram) prepareBindings(info *check.Info) {
	p.boundBindings = map[*check.TraitBound]map[string]bool{}
	p.implBindings = map[*check.Impl]map[string]bool{}
	calls := map[syntax.Expr]bool{}
	for _, file := range info.Resolve.Files {
		syntax.Walk(file.AST, func(node syntax.Node) bool {
			if call, ok := node.(*syntax.CallExpr); ok {
				calls[call.Fun] = true
			}
			return true
		})
	}
	type edge struct {
		required *check.TraitBound
		supplied check.Evidence
	}
	var edges []edge
	connect := func(bounds []*check.TraitBound, evidence []check.Evidence) {
		for i, bound := range bounds {
			if i < len(evidence) {
				edges = append(edges, edge{bound, evidence[i]})
			}
		}
	}
	seenImpls := map[*check.Impl]bool{}
	var inspectImpl func(*check.Impl)
	inspectImpl = func(impl *check.Impl) {
		if impl == nil || seenImpls[impl] {
			return
		}
		seenImpls[impl] = true
		connect(implTemplate(impl).Requirements, impl.RequirementEvidence)
		for _, evidence := range impl.RequirementEvidence {
			inspectImpl(evidence.Impl)
		}
		for declaration, branch := range impl.MethodCases {
			evidence := impl.MethodCaseEvidence[declaration]
			connect(branch.Requirements, evidence)
			for _, supplied := range evidence {
				inspectImpl(supplied.Impl)
			}
		}
	}
	inspect := func(current *check.Info) {
		for node, selection := range current.Members {
			inspectImpl(selection.Impl)
			if selection.Member.Static || nativeMember(selection) {
				continue
			}
			if calls[node] {
				continue
			}
			if selection.MethodExpr {
				continue
			}
			if selection.Bound != nil {
				markBinding(p.boundBindings, selection.Bound, selection.Member.Name)
			}
			if selection.Impl != nil {
				markBinding(p.implBindings, implTemplate(selection.Impl), selection.Member.Name)
			}
		}
		for _, view := range current.TraitViews {
			inspectImpl(view.Impl)
		}
		for _, instance := range current.FunctionInstances {
			if function := p.functions[instance.Signature]; function != nil {
				if instance.Case != nil {
					connect(instance.Case.Requirements, instance.CaseEvidence)
				} else {
					connect(info.GenericBounds[function.decl], instance.Evidence)
				}
			}
			for _, evidence := range instance.Evidence {
				inspectImpl(evidence.Impl)
			}
			for _, evidence := range instance.CaseEvidence {
				inspectImpl(evidence.Impl)
			}
		}
	}
	inspect(info)
	for _, branches := range info.BodyCases {
		for _, branch := range branches {
			inspect(branch.Info)
		}
	}
	if info.Demands != nil {
		for _, bodies := range info.Demands.Bodies {
			for _, body := range bodies {
				if body.Info != nil {
					inspect(body.Info)
				}
			}
		}
	}
	// Calls such as Wrap[T] -> Save[T] must forward the binder that Save uses.
	for changed := true; changed; {
		changed = false
		for _, edge := range edges {
			for _, member := range edge.required.Trait.Methods {
				if !p.boundNeedsBinding(edge.required, member.Name) {
					continue
				}
				if edge.supplied.Bound != nil {
					changed = markBinding(p.boundBindings, edge.supplied.Bound, member.Name) || changed
				}
				if edge.supplied.Impl != nil {
					changed = markBinding(p.implBindings, implTemplate(edge.supplied.Impl), member.Name) || changed
				}
			}
		}
	}
	// Public carrier entries can be instantiated by Go without a source demand.
	// Every eligible carrier must supply the binders those entries request.
	for _, function := range p.functions {
		if function.symbol.Visibility != resolve.Public || !p.legacyFunctions[function.decl] {
			continue
		}
		for _, bound := range info.GenericBounds[function.decl] {
			for _, member := range bound.Trait.Methods {
				if !p.boundNeedsBinding(bound, member.Name) {
					continue
				}
				for _, impl := range p.impls {
					if impl.Trait == bound.Trait && carrierTarget(info, impl) {
						markBinding(p.implBindings, implTemplate(impl), member.Name)
					}
				}
			}
		}
	}
}

func markBinding[K comparable](bindings map[K]map[string]bool, key K, method string) bool {
	if bindings[key] == nil {
		bindings[key] = map[string]bool{}
	}
	if bindings[key][method] {
		return false
	}
	bindings[key][method] = true
	return true
}

func (p *traitProgram) boundNeedsBinding(bound *check.TraitBound, member string) bool {
	for candidate, methods := range p.boundBindings {
		if candidate.ApplicationKey() == bound.ApplicationKey() && methods[member] {
			return true
		}
	}
	return false
}

func (p *traitProgram) memberNeedsBinding(member *check.Member) bool {
	return !member.Static && p.implBindings[implTemplate(member.Impl)][member.Name]
}

func (p *traitProgram) implNeedsBindings(impl *check.Impl) bool {
	for _, member := range impl.Methods {
		if p.memberNeedsBinding(member) {
			return true
		}
	}
	return false
}

func traitBinding(member *check.Member) string { return "Bind" + traitOperation(member) }
func (e *emitter) templateBindingName(member *check.Member) string {
	return strings.TrimSuffix(e.templateOperationName(member), "Func") + "Bind"
}

func (e *emitter) bindingSignature(selection *check.MemberSelection) *types.Signature {
	return signatureOf([]types.Type{e.memberReceiverType(selection)}, selection.Signature)
}

func (e *emitter) bindingFunction(selection *check.MemberSelection) goast.Expr {
	member := selectedMember(selection)
	if selection.Bound != nil {
		if callback := e.callbackOperation(selection.Bound, &check.Member{Name: "bind:" + member.Name}); callback != nil {
			return callback
		}
	}
	if selection.Impl != nil && parameterizedImpl(implTemplate(selection.Impl)) {
		impl := selection.Impl
		if !genericPairOps(e.info, impl) || e.privateEvidence(check.Evidence{Impl: impl}) {
			factory := enumIndices(e.sourceGlobal(impl.File, e.templateBindingName(member)), e.operationTemplateArguments(member))
			bounds, evidence := operationCallbacks(member)
			return &goast.CallExpr{Fun: factory, Args: append(e.operationWitnessArguments(member), e.callbackArguments(bounds, evidence)...)}
		}
	}
	if member.Trait == nil {
		return e.sourceGlobal(member.File, e.inherentName(member)+"Bind")
	}
	var target types.Type
	if selection.Bound != nil && selection.Bound.Param != nil && !parameterizedTrait(selection.Bound.Trait) {
		target = traitBoundTarget(selection.Bound)
	}
	if selection.Impl != nil && carrierTarget(e.info, implTemplate(selection.Impl)) && e.nameableType(selection.Impl.Target) {
		target = selection.Impl.Target
	}
	if target != nil {
		carrier := &goast.CallExpr{Fun: enumIndices(e.typeinfo("Of"), []goast.Expr{e.goType(target)})}
		return &goast.SelectorExpr{X: carrier, Sel: goast.NewIdent(traitBinding(member))}
	}
	ops := e.evidenceType(check.Evidence{Bound: selection.Bound, Impl: selection.Impl})
	return &goast.SelectorExpr{X: &goast.ParenExpr{X: &goast.CompositeLit{Type: ops}}, Sel: goast.NewIdent(traitBinding(member))}
}

func (p *traitInference) binding(selection *check.MemberSelection) goast.Expr {
	impl := selection.Impl
	if impl == nil || !parameterizedImpl(implTemplate(impl)) {
		return p.parameter(p.e.bindingFunction(selection), p.e.bindingSignature(selection))
	}
	member := selectedMember(selection)
	bounds, evidence := operationCallbacks(member)
	args, parameters := p.callbacks(bounds, evidence)
	witnesses, witnessTypes := p.operationWitnesses(member)
	args, parameters = append(witnesses, args...), append(witnessTypes, parameters...)
	factory := p.parameter(p.e.sourceGlobal(impl.File, p.e.templateBindingName(member)), signatureOf(parameters, p.e.bindingSignature(selection)))
	return &goast.CallExpr{Fun: factory, Args: args}
}

// The Go method value captures this wrapper once. wrapperMethod contains the
// original body and establishes a fresh self parameter on every invocation.
func (e *emitter) boundWrapperMethod(member *check.Member, receiver goast.Expr) goast.Expr {
	if member.Trait == nil || memberCase(member) != nil {
		return e.boundInherentBody(member, receiver)
	}
	target := receiver
	if member.Receiver == check.ValueRef && member.Impl.Capture != check.ValueRef {
		target = &goast.UnaryExpr{Op: gotoken.AND, X: target}
	}
	if parameterizedImpl(implTemplate(member.Impl)) {
		value := &goast.CallExpr{Fun: e.implFactory(member.Impl), Args: []goast.Expr{target}}
		return &goast.SelectorExpr{X: value, Sel: goast.NewIdent(member.GoName)}
	}
	elements := []goast.Expr{&goast.KeyValueExpr{Key: goast.NewIdent("target"), Value: target}}
	wrapper := &goast.CompositeLit{Type: e.implWrapperType(member.Impl), Elts: elements}
	return &goast.SelectorExpr{X: &goast.ParenExpr{X: wrapper}, Sel: goast.NewIdent(member.GoName)}
}

func (e *emitter) bindingDeclaration(member *check.Member, carrier *traitCarrier) goast.Decl {
	restore := e.memberBodyInfo(member)
	defer restore()
	selection := &check.MemberSelection{Member: member, Impl: member.Impl, Signature: member.Signature}
	receiver := e.fresh("Receiver")
	typ := e.goType(e.bindingSignature(selection)).(*goast.FuncType)
	typ.Params.List[0].Names = []*goast.Ident{receiver}
	body := &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{e.boundWrapperMethod(member, receiver)}}}}
	declaration := &goast.FuncDecl{Type: typ, Body: body}
	if member.Trait == nil {
		name := e.inherentName(member) + "Bind"
		e.generatedName(name, member.Decl)
		declaration.Name = goast.NewIdent(name)
		e.traitSourceBody(declaration, member.Decl)
	} else {
		declaration.Name = goast.NewIdent(traitBinding(member))
		declaration.Recv = receiverField("", goast.NewIdent(carrier.name))
	}
	return declaration
}

func (e *emitter) templateBindingDeclaration(member *check.Member) goast.Decl {
	restoreInfo := e.memberBodyInfo(member)
	defer restoreInfo()
	callbacks, restore := e.templateCallbacks(member.Impl.Requirements)
	defer restore()
	parameters := &goast.FieldList{List: append(e.templateImplWitnessParameters(member.Impl), callbacks.List...)}
	selection := &check.MemberSelection{Member: member, Impl: member.Impl, Signature: member.Signature}
	receiver := e.fresh("Receiver")
	binderType := e.goType(e.bindingSignature(selection)).(*goast.FuncType)
	binderType.Params.List[0].Names = []*goast.Ident{receiver}
	binder := &goast.FuncLit{Type: binderType, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{e.boundWrapperMethod(member, receiver)}}}}}
	name := e.templateBindingName(member)
	e.generatedName(name, member.Decl)
	typeParameters := e.templateImplParameters(member.Impl)
	if branch := memberCase(member); branch != nil {
		typeParameters = e.bodyCaseParameters(branch)
	}
	declaration := &goast.FuncDecl{Name: goast.NewIdent(name), Type: &goast.FuncType{TypeParams: typeParameters, Params: parameters, Results: &goast.FieldList{List: []*goast.Field{{Type: binderType}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{binder}}}}}
	if member.Trait == nil {
		e.anchor(declaration, member.Decl)
	}
	return declaration
}

func (e *emitter) boundInherentBody(member *check.Member, captured goast.Expr) goast.Expr {
	typ := &goast.FuncType{Params: e.params(member.Decl.Params[1:]), Results: e.params(member.Decl.Results)}
	body := e.functionBody(typ, member.Decl.Body)
	source := member.Decl.Params[0].Names[0]
	symbol := e.file.Definitions[source.ID()]
	used := false
	syntax.Walk(member.Decl.Body, func(node syntax.Node) bool {
		if id, ok := node.(*syntax.Ident); ok && e.file.References[id.ID()].Symbol == symbol && symbol != nil {
			used = true
		}
		return !used
	})
	if source.Name != "_" && used {
		body.List = append([]goast.Stmt{&goast.AssignStmt{Lhs: []goast.Expr{e.ident(source)}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{captured}}}, body.List...)
	}
	return &goast.FuncLit{Type: typ, Body: body}
}
