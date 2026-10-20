package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (p *permissionChecker) adjustedReceiver(node ast.Expr, q *permission, address, deref bool, mode RefMode) *permission {
	if address {
		addressable, writable := p.place(node)
		if !addressable {
			p.c.diag(node, ReferenceAddress, "trait receiver requires an addressable storage location")
		}
		if !writable && mode != ReadRef {
			p.c.diag(node, ReferencePermission, "cannot create a writable trait view through a read-only reference")
		}
		ref := mode
		if !writable {
			ref = ReadRef
		}
		result := &permission{typ: types.NewPointer(q.typ), ref: ref, elem: q}
		locations := p.locations(node)
		if len(locations) == 1 {
			result.location = locations[0]
		} else {
			result.locations = locations
		}
		return result
	}
	if deref {
		value := p.deref(q)
		if q.ref == ReadRef || q.shared {
			value = p.readValue(value)
		}
		return value
	}
	return q
}

func (p *permissionChecker) traitView(node ast.Expr, view *TraitView) *permission {
	got := p.rawExpr(node, nil)
	got = p.adjustedReceiver(node, got, view.Address, view.Deref, view.Capture)
	want := p.fromGo(view.Impl.Target)
	if view.Capture != ValueRef {
		want = &permission{typ: types.NewPointer(want.typ), ref: view.Capture, elem: want}
	}
	p.require(node, got, want)
	q := p.fromGo(view.Type)
	q.args = nil
	q.fields = map[string]*permission{"target": got}
	oldFile, oldPkg, oldImpl, oldTrait, oldSelf := p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf
	p.c.enterImpl(view.Impl)
	for _, a := range view.Impl.Trait.Associated {
		q.args = append(q.args, p.implAssociated(view.Impl, a.Name, nil))
	}
	p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf = oldFile, oldPkg, oldImpl, oldTrait, oldSelf
	return q
}

func (p *permissionChecker) traitMember(node ast.Expr, selection *MemberSelection) *permission {
	m := selection.Member
	if m.Native != nil && m.Decl == nil {
		return p.nativeMethodExpression(node, m.Native, selection.Signature)
	}
	oldFile, oldPkg, oldImpl, oldTrait, oldSelf := p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf
	defer func() {
		p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf = oldFile, oldPkg, oldImpl, oldTrait, oldSelf
	}()
	var actual *permission
	if selection.Receiver != nil {
		actual = p.expr(selection.Receiver, nil)
	}
	if selection.Impl != nil && m.Native == nil {
		p.c.enterImpl(selection.Impl)
	} else {
		p.c.currentImpl = nil
		p.c.currentTrait = m.Trait
		if m.Trait != nil {
			p.c.currentSelf = m.Trait.Self
			p.c.pkg = m.Trait.Type.Obj().Pkg()
		}
		if selection.Bound != nil {
			p.c.currentSelf = selection.Bound.Param
		}
		if file := p.c.out.Resolve.Files[m.File]; file != nil {
			p.c.file = file
		}
	}
	_, _, params := receiverParams(m.Decl)
	q := p.functionType(params, m.Decl.Results)
	q.typ = selection.Signature
	if f := p.functions[m.Symbol]; f != nil {
		q.function = f
	}
	if m.Trait != nil {
		sub := map[*types.TypeParam]*permission{}
		if selection.Impl != nil {
			sub[m.Trait.Self] = p.fromGo(selection.Impl.Target)
			for _, a := range m.Trait.Associated {
				sub[a.Param] = p.implAssociated(selection.Impl, a.Name, nil)
			}
		}
		if selection.Bound != nil {
			sub[m.Trait.Self] = p.fromGo(selection.Bound.Param)
			for _, a := range m.Trait.Associated {
				sub[a.Param] = p.boundAssociated(selection.Bound, a.Name, nil)
			}
		}
		if selection.Impl == nil && selection.Bound == nil && actual != nil {
			if named, ok := types.Unalias(actual.typ).(*types.Named); ok {
				for i, a := range m.Trait.Associated {
					if i < named.TypeArgs().Len() {
						sub[a.Param] = p.fromGo(named.TypeArgs().At(i))
						if i < len(actual.args) {
							sub[a.Param] = actual.args[i]
						}
					}
				}
			}
		}
		q = p.substitutePermission(q, sub)
	}
	if m.Static {
		return q
	}
	if selection.Impl == nil && selection.Bound == nil {
		return q
	}
	base := types.Type(nil)
	if selection.Impl != nil {
		base = selection.Impl.Target
	} else {
		base = selection.Bound.Param
	}
	q.receiver = p.fromGo(base)
	if m.Receiver != ValueRef {
		q.receiver = &permission{typ: types.NewPointer(base), ref: m.Receiver, elem: q.receiver}
	}
	if selection.MethodExpr {
		q.params = append([]*permission{q.receiver}, q.params...)
	}
	if actual != nil {
		actual = p.adjustedReceiver(selection.Receiver, actual, selection.Address, selection.Deref, m.Receiver)
		p.require(node, actual, q.receiver)
		q.receiver = p.adapt(actual, q.receiver)
	}
	return q
}

func (p *permissionChecker) nativeMethodExpression(node ast.Expr, method *types.Func, signature *types.Signature) *permission {
	q := p.fromGo(signature)
	fn := p.sourceMethod(method)
	if fn == nil || signature.Params().Len() == 0 {
		return q
	}
	target := p.fromGo(signature.Params().At(0).Type())
	switch n := node.(type) {
	case *ast.SelectorExpr:
		target = p.exprType(n.X)
	case *ast.PathExpr:
		path := *n.Path
		path.Segments = path.Segments[:len(path.Segments)-1]
		target = p.astType(&ast.PathType{Base: n.Base, Path: &path}, nil)
	}
	receiver := target
	_, indices, _ := types.LookupFieldOrMethod(target.typ, false, p.c.pkg, method.Name())
	for _, index := range indices[:max(0, len(indices)-1)] {
		if pointerType(receiver) {
			receiver = p.deref(receiver)
		}
		structure, ok := receiver.typ.Underlying().(*types.Struct)
		if !ok || index >= structure.NumFields() {
			break
		}
		receiver = p.fields(receiver)[structure.Field(index).Name()]
	}
	if !pointerType(fn.typ.receiver) && pointerType(receiver) {
		receiver = p.deref(receiver)
	}
	sub := map[*types.TypeParam]*permission{}
	p.inferPermission(fn.typ.receiver, receiver, sub)
	q = p.substitutePermission(fn.typ, sub)
	q.typ = signature
	if types.Identical(target.typ, q.receiver.typ) {
		target = q.receiver
	}
	q.params = append([]*permission{target}, q.params...)
	p.checkGenericCall(q, q.params, sub)
	return q
}

func (p *permissionChecker) sourceMethod(method *types.Func) *permissionFunction {
	signature, ok := method.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return nil
	}
	base := signature.Recv().Type()
	if pointer, ok := base.(*types.Pointer); ok {
		base = pointer.Elem()
	}
	actual, ok := types.Unalias(base).(*types.Named)
	if !ok {
		return nil
	}
	for _, fn := range p.functions {
		if fn.decl.Name.Name != method.Name() || fn.typ.receiver == nil {
			continue
		}
		receiver := fn.typ.receiver
		if pointerType(receiver) {
			receiver = p.element(receiver)
		}
		if named, ok := types.Unalias(receiver.typ).(*types.Named); ok && actual.Origin() == named.Origin() {
			return fn
		}
	}
	return nil
}

func (p *permissionChecker) implAssociated(impl *Impl, name string, sub map[*types.TypeParam]*permission) *permission {
	if !valid(impl.Associated[name]) {
		return p.fromGo(impl.Associated[name])
	}
	if node := impl.AssociatedNodes[name]; node != nil {
		oldFile, oldPkg, oldImpl, oldTrait, oldSelf := p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf
		p.c.enterImpl(impl)
		q := p.astType(node, sub)
		p.c.file, p.c.pkg, p.c.currentImpl, p.c.currentTrait, p.c.currentSelf = oldFile, oldPkg, oldImpl, oldTrait, oldSelf
		return q
	}
	return p.fromGo(impl.Associated[name])
}

func (p *permissionChecker) boundAssociated(bound *TraitBound, name string, sub map[*types.TypeParam]*permission) *permission {
	if node := bound.AssociatedNodes[name]; node != nil {
		old := p.c.file
		p.c.file = p.c.out.Resolve.Files[bound.File]
		q := p.astType(node, sub)
		p.c.file = old
		return q
	}
	q := p.fromGo(bound.Associated[name])
	return p.substitutePermission(q, sub)
}

func (p *permissionChecker) checkImplSignatures() {
	for _, impl := range p.c.implOrder {
		if impl.AliasOf != nil {
			p.c.enterImpl(impl)
			for _, a := range impl.Trait.Associated {
				if !p.sameSourcePermissions(p.implAssociated(impl, a.Name, nil), p.implAssociated(impl.AliasOf, a.Name, nil), map[[2]types.Type]bool{}) {
					p.c.diag(impl.Decl, TraitInvalid, "conflicting associated reference permissions: "+a.Name)
				}
			}
			p.c.leaveImpl()
		}
		if impl.Trait == nil || impl.Declare {
			continue
		}
		p.c.enterImpl(impl)
		for _, member := range impl.Methods {
			decl := memberNamed(impl.Trait.Methods, member.Name, member.Static)
			if decl == nil {
				continue
			}
			_, _, params := receiverParams(member.Decl)
			got := p.functionType(params, member.Decl.Results)
			p.c.file = p.c.out.Resolve.Files[impl.Trait.File]
			p.c.currentImpl, p.c.currentTrait, p.c.currentSelf = nil, impl.Trait, impl.Trait.Self
			_, _, params = receiverParams(decl.Decl)
			want := p.functionType(params, decl.Decl.Results)
			sub := map[*types.TypeParam]*permission{impl.Trait.Self: p.fromGo(impl.Target)}
			for _, a := range impl.Trait.Associated {
				sub[a.Param] = p.implAssociated(impl, a.Name, nil)
			}
			want = p.substitutePermission(want, sub)
			p.c.enterImpl(impl)
			if !p.sameSourcePermissions(got, want, map[[2]types.Type]bool{}) {
				p.c.diag(member.Decl, TraitInvalid, "reference permissions do not match trait member "+member.Name)
			}
		}
		p.c.leaveImpl()
	}
}

func (p *permissionChecker) sameSourcePermissions(a, b *permission, seen map[[2]types.Type]bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ref != b.ref || a.shared != b.shared {
		return false
	}
	if !p.sameSourcePermissions(a.elem, b.elem, seen) || !p.sameSourcePermissions(a.key, b.key, seen) {
		return false
	}
	for _, pair := range [][2][]*permission{{a.args, b.args}, {a.params, b.params}, {a.results, b.results}} {
		if len(pair[0]) != len(pair[1]) {
			return false
		}
		for i, q := range pair[0] {
			if !p.sameSourcePermissions(q, pair[1][i], seen) {
				return false
			}
		}
	}
	pair := [2]types.Type{a.typ, b.typ}
	if permissionPairSeen(seen, pair) {
		return true
	}
	seen[pair] = true
	defer delete(seen, pair)
	af, bf := p.fields(a), p.fields(b)
	if len(af) != len(bf) {
		return false
	}
	for name, q := range af {
		if !p.sameSourcePermissions(q, bf[name], seen) {
			return false
		}
	}
	return true
}
