package check

import (
	"go/types"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (p *permissionChecker) call(n *ast.CallExpr, want *permission) *permission {
	q := p.fromGo(p.c.out.Types[n])
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
	if _, print := p.c.out.Resolve.Prints[n]; print {
		for _, arg := range n.Args {
			p.expr(arg, nil)
		}
		return q
	}
	if result, handled := p.protocolCall(n, want); handled {
		return result
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
	sub := map[*types.TypeParam]*permission{}
	args := make([]*permission, len(n.Args))
	for i, arg := range n.Args {
		formal := p.callParam(fun, i, n.Ellipsis)
		args[i] = p.expr(arg, formal)
		p.inferPermission(formal, args[i], sub)
	}
	for i, arg := range n.Args {
		formal := p.substitutePermission(p.callParam(fun, i, n.Ellipsis), sub)
		p.require(arg, args[i], formal)
		if formal == nil && p.restricted(args[i], map[types.Type]bool{}) {
			p.require(arg, args[i], p.fromGo(anyType))
		}
	}
	p.checkGenericCall(fun, args, sub)
	if fun.closure != nil {
		p.checkClosure(fun.closure, fun, false)
	}
	p.applyEffects(fun)
	for _, arg := range args {
		p.applyEffects(arg)
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

func (p *permissionChecker) applyEffects(q *permission) {
	if q == nil {
		return
	}
	for s, value := range q.effects {
		p.setSymbol(s, p.merge(p.env[s], value))
	}
}

func (p *permissionChecker) checkGenericCall(fun *permission, args []*permission, sub map[*types.TypeParam]*permission) {
	f := fun.function
	if f == nil || f.decl.Body == nil || p.activeCalls[f.decl] {
		return
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
	oldEnv, oldDeclared, oldResults := p.env, p.declared, p.results
	oldObserved, oldCaptured, oldEffects := p.observed, p.captured, p.effects
	oldInstance := p.instance
	defer func() {
		p.c.file, p.c.pkg = oldFile, oldPkg
		p.env, p.declared, p.results = oldEnv, oldDeclared, oldResults
		p.observed, p.captured, p.effects = oldObserved, oldCaptured, oldEffects
		p.instance = oldInstance
	}()
	p.activeCalls[f.decl] = true
	defer delete(p.activeCalls, f.decl)
	p.c.file = f.file
	for _, pkg := range p.c.out.Resolve.Packages {
		for _, file := range pkg.Files {
			if file == f.file {
				p.c.pkg = p.c.packages[pkg]
			}
		}
	}
	p.env, p.declared, p.results = clonePermissionEnv(p.globals), permissionEnv{}, q.results
	p.observed, p.captured, p.effects = permissionEnv{}, nil, nil
	p.instance = inst
	p.bindParams(f.decl.Params, q.params)
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

func (p *permissionChecker) instantiateFunction(fun *permission, args []ast.Expr, t types.Type) *permission {
	sig, ok := fun.typ.(*types.Signature)
	if !ok || sig.TypeParams().Len() == 0 {
		return fun
	}
	sub := map[*types.TypeParam]*permission{}
	for i, arg := range args {
		if i < sig.TypeParams().Len() {
			sub[sig.TypeParams().At(i)] = p.exprType(arg)
		}
	}
	q := p.substitutePermission(fun, sub)
	q.typ = t
	p.checkGenericCall(q, q.params, sub)
	return q
}

func (p *permissionChecker) inferPermission(formal, actual *permission, sub map[*types.TypeParam]*permission) {
	if formal == nil || actual == nil {
		return
	}
	if tp, ok := formal.typ.(*types.TypeParam); ok {
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
	if q == nil {
		return nil
	}
	if tp, ok := q.typ.(*types.TypeParam); ok && sub[tp] != nil {
		return sub[tp]
	}
	r := *q
	typesub := map[*types.TypeParam]types.Type{}
	for tp, q := range sub {
		if q != nil && q.typ != nil {
			typesub[tp] = q.typ
		}
	}
	r.typ = substitute(q.typ, typesub)
	r.elem, r.key = p.substitutePermission(q.elem, sub), p.substitutePermission(q.key, sub)
	r.receiver = p.substitutePermission(q.receiver, sub)
	r.args, r.params, r.results = nil, nil, nil
	for _, v := range q.args {
		r.args = append(r.args, p.substitutePermission(v, sub))
	}
	for _, v := range q.params {
		r.params = append(r.params, p.substitutePermission(v, sub))
	}
	for _, v := range q.results {
		r.results = append(r.results, p.substitutePermission(v, sub))
	}
	r.fields = map[string]*permission{}
	for name, v := range q.fields {
		r.fields[name] = p.substitutePermission(v, sub)
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
