package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (p *permissionChecker) call(n *ast.CallExpr, want *permission, asynchronous bool) *permission {
	q := p.fromGo(p.c.out.Types[n])
	if view := p.c.out.TraitViews[n]; view != nil {
		q = p.fromGo(view.SourceType)
	}
	if con := p.c.out.Constructors[n]; con != nil {
		q = p.constructorType(n, con, want)
		r := *q
		r.fields = copyFields(q.fields)
		q = &r
		for i, arg := range n.Args {
			field := p.enumField(q, con.Variant, i)
			got := p.expr(arg, field)
			p.require(arg, got, field)
			q.fields[permissionFieldKey(con.Variant, i)] = p.adapt(got, field)
		}
		return q
	}
	if adapter := p.c.out.GoResults[n]; adapter != nil {
		actual := p.expr(adapter.Call, nil)
		value, failure := p.fromGo(adapter.Value), actual
		if !adapter.Unit && actual != nil && len(actual.results) == 2 {
			value, failure = actual.results[0], actual.results[1]
		}
		if len(n.Args) == 2 {
			p.require(n.Args[0], value, p.exprType(n.Args[0]))
		}
		payload := failure
		if adapter.Keep {
			payload = p.fromGo(adapter.Payload)
			payload.fields = map[string]*permission{"Value": value, "Err": failure}
		}
		q.args = []*permission{value, payload}
		q.fields = map[string]*permission{"Ok.0": value, "Err.0": payload}
		return q
	}
	if p.c.isTypeExpr(n.Fun) {
		target := p.exprType(n.Fun)
		for _, arg := range n.Args {
			got := p.expr(arg, target)
			p.require(arg, got, target)
		}
		return target
	}
	s := p.c.symbol(n.Fun)
	if s != nil && s.Kind == resolve.Builtin {
		return p.builtin(n)
	}
	fun := p.expr(n.Fun, nil)
	if fun == nil {
		return q
	}
	if fun.shared {
		p.require(n.Fun, fun, p.fromGo(fun.typ))
	}
	sub := map[*types.TypeParam]*permission{}
	args := make([]*permission, 0, len(n.Args))
	var argumentNodes []ast.Expr
	for i, arg := range n.Args {
		actual := p.expr(arg, p.callParam(fun, i, n.Ellipsis))
		values := []*permission{actual}
		_, call := referenceScrutinee(arg).(*ast.CallExpr)
		if tuple, ok := p.c.out.Types[arg].(*types.Tuple); ok && tuple.Len() > 1 && call && len(n.Args) == 1 && !n.Ellipsis {
			values = actual.results
		}
		for _, value := range values {
			formal := p.callParam(fun, len(args), n.Ellipsis)
			args = append(args, value)
			argumentNodes = append(argumentNodes, arg)
			p.inferPermission(formal, value, sub)
		}
	}
	p.functionEvidence(n, p.c.out.FunctionInstances[n], sub)
	for i, arg := range argumentNodes {
		formal := p.substitutePermission(p.callParam(fun, i, n.Ellipsis), sub)
		actual := args[i]
		if p.nativeScalarArgument(fun, actual, formal) {
			adapted := *actual
			adapted.ref = ValueRef
			actual = &adapted
		}
		p.require(arg, actual, formal)
		if formal == nil && p.restricted(args[i], map[types.Type]bool{}) {
			p.require(arg, args[i], p.fromGo(anyType))
		}
	}
	p.checkGenericCall(fun, args, sub)
	if fun.closure != nil {
		p.checkClosure(fun.closure, fun, false)
	}
	p.applyEffects(fun)
	if asynchronous {
		p.recordAsyncEffects(fun.effects)
	}
	for _, arg := range args {
		p.applyEffects(arg)
		if asynchronous && arg != nil {
			p.recordAsyncEffects(arg.effects)
		}
	}
	var results []*permission
	for _, result := range fun.results {
		results = append(results, p.substitutePermission(result, sub))
	}
	if len(results) == 1 {
		return results[0]
	}
	if len(results) > 1 {
		q.results = results
	}
	return q
}

func (p *permissionChecker) nativeScalarArgument(fun, actual, formal *permission) bool {
	if actual == nil || formal == nil || actual.ref != WriteRef || actual.shared || formal.ref != ValueRef || fun.instantiated {
		return false
	}
	if fun.function != nil && fun.function.decl.Generics != nil {
		return false
	}
	if signature, ok := fun.typ.(*types.Signature); ok && signature.TypeParams().Len() != 0 {
		return false
	}
	pointer, ok := formal.typ.Underlying().(*types.Pointer)
	if !ok || !types.Identical(actual.typ, formal.typ) {
		return false
	}
	_, scalar := pointer.Elem().Underlying().(*types.Basic)
	return scalar
}

func (p *permissionChecker) applyEffects(q *permission) {
	if q == nil {
		return
	}
	for s, value := range q.effects {
		p.setSymbol(s, p.merge(p.env[s], value))
	}
	p.recordAsyncEffects(q.asyncEffects)
}

func (p *permissionChecker) recordAsyncEffects(effects permissionEnv) {
	if len(effects) == 0 {
		return
	}
	if p.asyncEffects == nil {
		p.asyncEffects = permissionEnv{}
	}
	for s, value := range effects {
		p.asyncEffects[s] = p.merge(p.asyncEffects[s], value)
	}
}

func (p *permissionChecker) checkGenericCall(fun *permission, args []*permission, sub map[*types.TypeParam]*permission) {
	f := fun.function
	if f == nil || f.decl.Body == nil || p.activeCalls[f.decl] {
		return
	}
	if f.typ.receiver != nil && len(fun.params) == len(f.typ.params)+1 {
		method := *fun
		method.params = fun.params[1:]
		method.receiver = p.substitutePermission(fun.receiver, sub)
		fun = &method
		if len(args) > 0 {
			args = args[1:]
		}
	}
	generic := f.decl.Generics != nil
	if receiver := f.typ.receiver; receiver != nil {
		if pointerType(receiver) {
			receiver = p.element(receiver)
		}
		if named, ok := types.Unalias(receiver.typ).(*types.Named); ok {
			generic = generic || named.Origin().TypeParams().Len() != 0
		}
	}
	if !generic {
		return
	}
	needed := p.restricted(fun.receiver, map[types.Type]bool{})
	for _, arg := range args {
		needed = needed || p.restricted(arg, map[types.Type]bool{})
	}
	if !needed {
		return
	}
	inst := map[*types.TypeParam]*permission{}
	for tp, q := range sub {
		inst[tp] = q
	}
	p.inferPermission(f.typ.receiver, fun.receiver, inst)
	for i, arg := range args {
		p.inferPermission(p.callParam(f.typ, i, false), arg, inst)
	}
	q := p.substitutePermission(f.typ, inst)
	oldFile, oldPkg := p.c.file, p.c.pkg
	oldImpl, oldTrait, oldSelf, oldBounds := p.c.currentImpl, p.c.currentTrait, p.c.currentSelf, p.c.activeBounds
	oldEnv, oldDeclared, oldResults := p.env, p.declared, p.results
	oldObserved, oldCaptured, oldEffects := p.observed, p.captured, p.effects
	oldInstance := p.instance
	oldAsyncEffects := p.asyncEffects
	defer func() {
		p.c.file, p.c.pkg = oldFile, oldPkg
		p.c.currentImpl, p.c.currentTrait, p.c.currentSelf, p.c.activeBounds = oldImpl, oldTrait, oldSelf, oldBounds
		p.env, p.declared, p.results = oldEnv, oldDeclared, oldResults
		p.observed, p.captured, p.effects = oldObserved, oldCaptured, oldEffects
		p.instance = oldInstance
		p.asyncEffects = oldAsyncEffects
	}()
	p.activeCalls[f.decl] = true
	defer delete(p.activeCalls, f.decl)
	p.c.file = f.file
	p.c.currentImpl, p.c.currentTrait, p.c.currentSelf = nil, nil, nil
	if member := p.c.memberSymbols[p.c.symbol(f.decl.Name)]; member != nil && member.Impl != nil {
		p.c.enterImpl(member.Impl)
	}
	p.c.activeBounds = p.c.out.GenericBounds[f.decl]
	for _, pkg := range p.c.out.Resolve.Packages {
		for _, file := range pkg.Files {
			if file == f.file {
				p.c.pkg = p.c.packages[pkg]
			}
		}
	}
	p.env, p.declared, p.results = clonePermissionEnv(p.globals), permissionEnv{}, q.results
	p.observed, p.captured, p.effects = permissionEnv{}, nil, nil
	p.asyncEffects = permissionEnv{}
	p.instance = inst
	params := q.params
	if p.c.memberSymbols[p.c.symbol(f.decl.Name)] != nil && q.receiver != nil {
		params = append([]*permission{q.receiver}, params...)
	}
	p.bindParams(f.decl.Params, params)
	p.bindParams(f.decl.Results, q.results)
	if q.receiver != nil {
		p.bindParams(f.decl.Recv, []*permission{q.receiver})
	}
	p.functionBlock(f.decl.Body)
}

func (p *permissionChecker) callParam(fun *permission, index int, ellipsis bool) *permission {
	var formal *permission
	if index < len(fun.params) {
		formal = fun.params[index]
	}
	variadic := false
	if fun.typ != nil {
		if sig, ok := fun.typ.Underlying().(*types.Signature); ok {
			variadic = sig.Variadic()
		}
	}
	if variadic && len(fun.params) != 0 && index >= len(fun.params)-1 {
		formal = fun.params[len(fun.params)-1]
		if !ellipsis {
			formal = p.element(formal)
		}
	}
	return formal
}

func (p *permissionChecker) exprType(node ast.Expr) *permission {
	switch n := node.(type) {
	case *ast.Ident:
		return p.astType(&ast.PathType{Base: n.Base, Path: &ast.Path{Base: n.Base, Segments: []*ast.PathSegment{{Name: n}}}}, nil)
	case *ast.TypeExpr:
		return p.astType(n.Type, nil)
	case *ast.ParenExpr:
		return p.exprType(n.X)
	case *ast.PathExpr:
		return p.astType(&ast.PathType{Base: n.Base, Path: n.Path}, nil)
	case *ast.IndexExpr:
		q := p.exprType(n.X)
		r := *q
		r.typ = p.c.out.Types[n]
		r.args = nil
		for _, index := range n.Indices {
			r.args = append(r.args, p.exprType(index))
		}
		return &r
	}
	return p.fromGo(p.c.out.Types[node])
}

func (p *permissionChecker) instantiateFunction(fun *permission, args []ast.Expr, t types.Type, node ast.Node) *permission {
	sig, ok := fun.typ.(*types.Signature)
	if !ok || !p.c.functionGeneric(sig) {
		return fun
	}
	sub := map[*types.TypeParam]*permission{}
	for index, position := range p.c.typeArgumentPositions(sig, len(args)) {
		if index < sig.TypeParams().Len() {
			sub[sig.TypeParams().At(index)] = p.exprType(args[position])
		}
	}
	return p.instantiatedFunction(fun, t, node, sub)
}

func (p *permissionChecker) instantiatedFunction(fun *permission, t types.Type, node ast.Node, sub map[*types.TypeParam]*permission) *permission {
	p.functionEvidence(node, p.c.out.FunctionInstances[node], sub)
	q := p.substitutePermission(fun, sub)
	q.typ = t
	q.instantiated = true
	p.checkGenericCall(q, q.params, sub)
	return q
}

func (p *permissionChecker) functionEvidence(node ast.Node, instance *FunctionInstance, sub map[*types.TypeParam]*permission) {
	if instance == nil {
		return
	}
	for i, bound := range p.c.functionBounds[instance.Signature] {
		if i >= len(instance.Evidence) {
			continue
		}
		witness := instance.Evidence[i]
		target := p.substitutePermission(p.fromGo(bound.Target), sub)
		for _, associated := range bound.Trait.Associated {
			actual := p.evidenceAssociated(witness, associated.Name, target, sub)
			if actual == nil {
				continue
			}
			if projected := projectedBinding(bound, associated.Name); projected != nil {
				sub[projected] = actual
			} else if !p.sameSourcePermissions(actual, p.boundAssociated(bound, associated.Name, sub), map[[2]types.Type]bool{}) {
				p.c.diag(node, GenericConstraint, "associated reference permissions do not satisfy constraint: "+associated.Name)
			}
		}
	}
}

func (p *permissionChecker) evidenceAssociated(witness Evidence, name string, target *permission, input map[*types.TypeParam]*permission) *permission {
	if witness.Impl != nil {
		sub := map[*types.TypeParam]*permission{}
		for param, value := range input {
			sub[param] = value
		}
		if witness.Impl.Template != nil && target != nil {
			p.inferPermission(p.fromGo(witness.Impl.Template.Target), target, sub)
		}
		return p.implAssociated(witness.Impl, name, sub)
	}
	if witness.Bound != nil {
		return p.boundAssociated(witness.Bound, name, input)
	}
	return nil
}

func (p *permissionChecker) inferPermission(formal, actual *permission, sub map[*types.TypeParam]*permission) {
	if formal == nil || actual == nil {
		return
	}
	if tp, ok := formal.typ.(*types.TypeParam); ok && !p.c.out.isArray(formal.typ) {
		// A free parameter in an impl or a constrained method is already fixed.
		// Borrowing a value of that same parameter does not redefine the type's
		// capabilities (and must not taint a distinct value returned as Self).
		if types.Identical(tp, actual.typ) {
			return
		}
		sub[tp] = p.merge(sub[tp], actual)
		return
	}
	p.inferPermission(formal.elem, actual.elem, sub)
	p.inferPermission(formal.key, actual.key, sub)
	for i, q := range formal.args {
		if i < len(actual.args) {
			p.inferPermission(q, actual.args[i], sub)
		}
	}
	for i, q := range formal.params {
		if i < len(actual.params) {
			p.inferPermission(q, actual.params[i], sub)
		}
	}
	for i, q := range formal.results {
		if i < len(actual.results) {
			p.inferPermission(q, actual.results[i], sub)
		}
	}
}

func (p *permissionChecker) substitutePermission(q *permission, sub map[*types.TypeParam]*permission) *permission {
	return p.substitutePermissionActive(q, sub, map[*types.TypeParam]bool{})
}

func (p *permissionChecker) substitutePermissionActive(q *permission, sub map[*types.TypeParam]*permission, active map[*types.TypeParam]bool) *permission {
	if q == nil {
		return nil
	}
	if tp, ok := q.typ.(*types.TypeParam); ok && !p.c.out.isArray(q.typ) && q.ref == ValueRef && sub[tp] != nil && sub[tp] != q && !active[tp] {
		active[tp] = true
		result := p.substitutePermissionActive(sub[tp], sub, active)
		delete(active, tp)
		return result
	}
	r := *q
	typesub := map[*types.TypeParam]types.Type{}
	for tp, q := range sub {
		if q != nil && q.typ != nil {
			typesub[tp] = q.typ
		}
	}
	r.typ = substitute(q.typ, typesub, p.c.out)
	r.elem, r.key = p.substitutePermissionActive(q.elem, sub, active), p.substitutePermissionActive(q.key, sub, active)
	r.receiver = p.substitutePermissionActive(q.receiver, sub, active)
	r.args, r.params, r.results = nil, nil, nil
	for _, v := range q.args {
		r.args = append(r.args, p.substitutePermissionActive(v, sub, active))
	}
	for _, v := range q.params {
		r.params = append(r.params, p.substitutePermissionActive(v, sub, active))
	}
	for _, v := range q.results {
		r.results = append(r.results, p.substitutePermissionActive(v, sub, active))
	}
	r.fields = map[string]*permission{}
	for name, v := range q.fields {
		r.fields[name] = p.substitutePermissionActive(v, sub, active)
	}
	return &r
}

func (p *permissionChecker) builtin(n *ast.CallExpr) *permission {
	q := p.fromGo(p.c.out.Types[n])
	name := calleeName(n.Fun)
	args := make([]*permission, len(n.Args))
	for i, arg := range n.Args {
		if i == 0 && (name == "make" || name == "new") {
			args[i] = p.exprType(arg)
		} else {
			args[i] = p.expr(arg, nil)
		}
	}
	mutates := func(i int) {
		if i < len(args) && args[i] != nil && (args[i].shared || args[i].ref == ReadRef) {
			p.reportWrite(n.Args[i])
		}
	}
	switch name {
	case "new":
		if len(args) > 0 {
			q.elem = args[0]
		}
	case "make":
		if len(args) > 0 {
			q = args[0]
		}
	case "append":
		mutates(0)
		if len(args) > 0 {
			q = args[0]
			for i := 1; i < len(args); i++ {
				got := args[i]
				if n.Ellipsis {
					got = p.element(got)
					if args[i] != nil && args[i].shared {
						got = p.readValue(got)
					}
				}
				p.require(n.Args[i], got, p.element(q))
			}
		}
	case "copy":
		mutates(0)
		if len(args) > 1 {
			element := p.element(args[1])
			if args[1] != nil && args[1].shared {
				element = p.readValue(element)
			}
			p.require(n.Args[1], element, p.element(args[0]))
		}
	case "clear", "delete", "close":
		mutates(0)
	case "panic":
		for i, arg := range args {
			p.require(n.Args[i], arg, p.fromGo(anyType))
		}
	}
	return q
}
