package check

import (
	"go/types"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (b *TraitBound) MethodSignature(m *Member) *types.Signature {
	return substituteSignature(m.Signature, b.methodSubstitution(), b.info)
}

func (b *TraitBound) methodSubstitution() map[*types.TypeParam]types.Type {
	sub := map[*types.TypeParam]types.Type{b.Trait.Self: b.Target}
	for index, parameter := range b.Trait.TypeParams {
		if index < len(b.Inputs) {
			sub[parameter] = b.Inputs[index]
		}
	}
	for _, a := range b.Trait.Associated {
		sub[a.Param] = b.Associated[a.Name]
	}
	return sub
}

func (c *checker) traitVisible(t *Trait) bool {
	if c.currentTrait == t {
		return true
	}
	bound := map[resolve.Key]bool{}
	var globs []*resolve.UseBinding
	for scope := c.file.Scope; scope != nil; scope = scope.Parent {
		if scope.Kind == resolve.PreludeScope {
			break
		}
		for name, symbol := range scope.Bindings {
			if name.Namespace == resolve.MacroNamespace {
				continue
			}
			if !bound[name] && c.traitForSymbol(symbol) == t {
				return true
			}
			bound[name] = true
		}
		for name, list := range scope.Aliases {
			if name.Namespace == resolve.MacroNamespace {
				continue
			}
			for _, symbol := range list {
				if !bound[name] && c.traitForSymbol(symbol) == t {
					return true
				}
			}
			bound[name] = true
		}
		globs = append(globs, scope.Globs...)
	}
	// Named bindings win over globs, and explicit globs win over the prelude.
	for _, implicit := range []bool{false, true} {
		members := map[resolve.Key][]*resolve.Symbol{}
		for _, use := range globs {
			if use.Implicit != implicit {
				continue
			}
			for name, symbols := range c.globMembers(use, map[*resolve.Scope]bool{}) {
				members[name] = append(members[name], symbols...)
			}
		}
		for name, symbols := range members {
			if bound[name] {
				continue
			}
			for _, symbol := range symbols {
				if c.traitForSymbol(symbol) == t {
					return true
				}
			}
			bound[name] = true
		}
	}
	return false
}

func (c *checker) globMembers(use *resolve.UseBinding, visiting map[*resolve.Scope]bool) map[resolve.Key][]*resolve.Symbol {
	var scope *resolve.Scope
	if symbol := c.localSymbol(use.Target); symbol != nil {
		scope = symbol.Members
	}
	if scope == nil {
		path := strings.Join(use.Target, "/")
		for _, pkg := range c.out.Resolve.Packages {
			if pkg.IsExternalTest() {
				continue
			}
			dir := filepath.ToSlash(pkg.Dir)
			if path == dir || path == c.out.PackagePaths[dir] {
				scope = pkg.Scope
				break
			}
		}
	}
	if scope == nil {
		path := strings.Join(use.Target, "/")
		pkg := c.imported[path]
		if pkg == nil {
			pkg, _ = c.importer.Import(path)
			c.imported[path] = pkg
		}
		members := map[resolve.Key][]*resolve.Symbol{}
		if pkg != nil {
			// Native names can shadow a prelude trait without being traits.
			for _, name := range pkg.Scope().Names() {
				if pkg.Scope().Lookup(name).Exported() {
					members[resolve.Key{Name: name}] = nil
				}
			}
		}
		return members
	}
	if visiting[scope] {
		return nil
	}
	visiting[scope] = true
	defer delete(visiting, scope)
	members := map[resolve.Key][]*resolve.Symbol{}
	for name, symbol := range scope.Bindings {
		if name.Namespace == resolve.OrdinaryNamespace && symbol.Visibility == resolve.Public {
			members[name] = []*resolve.Symbol{symbol}
		}
	}
	for name, aliases := range scope.Aliases {
		if name.Namespace == resolve.OrdinaryNamespace && scope.Bindings[name] == nil {
			for _, symbol := range aliases {
				if symbol.Visibility == resolve.Public {
					members[name] = append(members[name], symbol)
				}
			}
		}
	}
	for _, glob := range scope.Globs {
		if !glob.Public {
			continue
		}
		for name, symbols := range c.globMembers(glob, visiting) {
			if scope.Bindings[name] == nil && len(scope.Aliases[name]) == 0 {
				members[name] = append(members[name], symbols...)
			}
		}
	}
	return members
}

func memberNamed(methods []*Member, name string, static bool) *Member {
	for _, m := range methods {
		if m.Name == name && m.Static == static {
			return m
		}
	}
	return nil
}

func (c *checker) memberAccessible(m *Member) bool {
	return m.Impl == nil || m.Impl.Trait != nil || m.Impl.Owner == c.pkg || m.Symbol != nil && m.Symbol.Visibility == resolve.Public
}

func (c *checker) staticMember(n *ast.PathExpr) *MemberSelection {
	if n.Path == nil || len(n.Path.Segments) < 2 {
		return nil
	}
	parts := n.Path.Segments
	name := parts[len(parts)-1].Name.Name
	root := parts[0]
	target := c.symbolType(c.sourcePathSymbol(parts[:len(parts)-1]))
	if len(parts) == 2 && root.Name.Name == "Self" && c.currentSelf != nil {
		target = c.currentSelf
	}
	if args := parts[len(parts)-2].Args; args != nil {
		target = c.instantiate(target, args)
	}
	if len(parts) == 2 && c.currentImpl != nil && root.Name.Name == "Self" {
		if m := memberNamed(c.currentImpl.Methods, name, true); m != nil {
			return &MemberSelection{Member: m, Impl: c.currentImpl, Signature: m.Signature}
		}
	}
	var candidates []*MemberSelection
	for _, bound := range c.boundsFor(target) {
		if member := memberNamed(bound.Trait.Methods, name, true); member != nil {
			candidates = append(candidates, &MemberSelection{Member: member, Bound: bound, Signature: bound.MethodSignature(member)})
		}
	}
	private := false
	for _, template := range c.implOrder {
		if memberNamed(template.Methods, name, true) == nil {
			continue
		}
		impl := c.matchImpl(template, target)
		if impl == nil {
			continue
		}
		if impl.Trait != nil && impl.Trait.Type.Obj().Pkg() != c.pkg && (impl.Trait.Symbol == nil || impl.Trait.Symbol.Visibility != resolve.Public) {
			continue
		}
		if m := memberNamed(impl.Methods, name, true); m != nil {
			if !c.memberAccessible(m) {
				private = true
				continue
			}
			if impl.Trait == nil {
				return &MemberSelection{Member: m, Impl: impl, Signature: m.Signature}
			}
			duplicate := false
			for _, old := range candidates {
				duplicate = duplicate || old.Member.Trait == impl.Trait
			}
			if !duplicate {
				candidates = append(candidates, &MemberSelection{Member: m, Impl: impl, Signature: m.Signature})
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	if len(candidates) > 1 {
		c.diag(n, TraitInvalid, "ambiguous static member; select a trait explicitly")
	} else if private {
		c.diag(n, TraitInvalid, "private inherent member is not visible from this package")
	} else if valid(target) {
		for _, template := range c.implOrder {
			if template.Trait != nil || memberNamed(template.Methods, name, false) == nil {
				continue
			}
			impl := c.matchImpl(template, target)
			if impl == nil {
				continue
			}
			member := memberNamed(impl.Methods, name, false)
			if !c.memberAccessible(member) {
				c.diag(n, TraitInvalid, "private inherent member is not visible from this package")
				return nil
			}
			receiver := target
			if member.Receiver != ValueRef {
				receiver = types.NewPointer(receiver)
			}
			params := []*types.Var{types.NewVar(0, c.pkg, "self", receiver)}
			for i := 0; i < member.Signature.Params().Len(); i++ {
				params = append(params, member.Signature.Params().At(i))
			}
			signature := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), member.Signature.Results(), member.Signature.Variadic())
			return &MemberSelection{Member: member, Impl: impl, MethodExpr: true, Signature: signature}
		}
		if object, _, _ := types.LookupFieldOrMethod(target, false, c.pkg, name); object != nil {
			if function, ok := object.(*types.Func); ok {
				signature := function.Type().(*types.Signature)
				params := []*types.Var{types.NewVar(0, c.pkg, "", target)}
				for i := 0; i < signature.Params().Len(); i++ {
					params = append(params, types.NewVar(0, c.pkg, "", signature.Params().At(i).Type()))
				}
				method := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), signature.Results(), signature.Variadic())
				return &MemberSelection{Member: &Member{Name: name, GoName: name, Signature: signature, Native: function}, MethodExpr: true, Signature: method}
			}
		}
		if _, parameter := target.(*types.TypeParam); parameter {
			c.diag(n, TraitInvalid, "static member requires a matching trait constraint")
		}
	}
	return nil
}

func (c *checker) instanceMember(node ast.Expr, receiver ast.Expr, target types.Type, name string, qualified *Trait) *MemberSelection {
	if !valid(target) {
		return nil
	}
	if trait := c.traitOf(target); trait != nil {
		if qualified != nil && qualified != trait {
			c.diag(node, TraitInvalid, "trait views do not implicitly convert to another trait")
			return nil
		}
		m := memberNamed(trait.Methods, name, false)
		if m == nil {
			c.diag(node, TraitInvalid, "unknown instance trait member "+name)
			return nil
		}
		sub := map[*types.TypeParam]types.Type{}
		n := types.Unalias(target).(*types.Named)
		for index, parameter := range trait.TypeParams {
			if index < n.TypeArgs().Len() {
				sub[parameter] = n.TypeArgs().At(index)
			}
		}
		for offset, a := range trait.Associated {
			index := len(trait.TypeParams) + offset
			if index < n.TypeArgs().Len() {
				sub[a.Param] = n.TypeArgs().At(index)
			}
		}
		return &MemberSelection{Member: m, Receiver: receiver, Signature: substituteSignature(m.Signature, sub, c.out)}
	}
	lookupTarget, base := c.receiverTargets(receiver, target)
	// Within an impl, self calls retain that implementation even if a native
	// method or another trait has the same spelling.
	if c.symbol(receiver) != nil && c.symbol(receiver) == c.currentReceiver && c.currentImpl != nil && (qualified == nil || qualified == c.currentImpl.Trait) {
		if m := memberNamed(c.currentImpl.Methods, name, false); m != nil {
			return c.selectReceiver(m, receiver, target)
		}
	}
	private := false
	for _, mode := range c.receiverModes(receiver, target) {
		var candidates []*MemberSelection
		for _, candidate := range []types.Type{lookupTarget, base} {
			for _, bound := range c.boundsFor(candidate) {
				if qualified != nil && bound.Trait != qualified {
					continue
				}
				member := memberNamed(bound.Trait.Methods, name, false)
				if member == nil || member.Receiver != mode {
					continue
				}
				duplicate := false
				for _, old := range candidates {
					duplicate = duplicate || old.Bound == bound
				}
				if !duplicate {
					candidates = append(candidates, c.selectBound(member, bound, receiver, target))
				}
			}
		}
		for _, template := range c.implOrder {
			member := memberNamed(template.Methods, name, false)
			if member == nil || member.Receiver != mode {
				continue
			}
			if qualified != nil && template.Trait != qualified {
				continue
			}
			if template.Trait != nil && qualified == nil && !c.traitVisible(template.Trait) {
				continue
			}
			impl := c.matchImpl(template, lookupTarget)
			if impl == nil && !types.Identical(lookupTarget, base) {
				impl = c.matchImpl(template, base)
			}
			if impl == nil {
				continue
			}
			m := memberNamed(impl.Methods, name, false)
			if m == nil || m.Receiver != mode {
				continue
			}
			if !c.memberAccessible(m) {
				private = true
				continue
			}
			if qualified == nil && impl.Trait == nil {
				return c.selectReceiver(m, receiver, target)
			}
			duplicate := false
			for _, old := range candidates {
				var oldTarget types.Type
				if old.Impl != nil {
					oldTarget = old.Impl.Target
				} else {
					oldTarget = old.Bound.Target
				}
				duplicate = duplicate || old.Member.Trait == impl.Trait && types.Identical(oldTarget, impl.Target)
			}
			if !duplicate {
				candidates = append(candidates, c.selectReceiver(m, receiver, target))
			}
		}
		if qualified == nil {
			if obj := c.lookupGoMember(target, true, name); obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					sig := fn.Type().(*types.Signature)
					nativeMode := ValueRef
					if sig.Recv() != nil {
						if _, ok := sig.Recv().Type().(*types.Pointer); ok {
							nativeMode = WriteRef
						}
					}
					if nativeMode == mode {
						return &MemberSelection{Member: &Member{Name: name, GoName: name, Signature: sig, Native: fn}, Receiver: receiver, Signature: sig}
					}
				} else {
					return nil
				}
			}
		}
		if len(candidates) == 1 {
			return candidates[0]
		}
		if len(candidates) > 1 {
			c.diag(node, TraitInvalid, "ambiguous trait method; use a qualified call")
			return nil
		}
	}
	if qualified != nil {
		c.diag(node, TraitInvalid, "no explicit implementation for qualified method "+name)
	} else if private {
		c.diag(node, TraitInvalid, "private inherent member is not visible from this package")
	}
	return nil
}

func (c *checker) selectBound(member *Member, bound *TraitBound, receiver ast.Expr, actual types.Type) *MemberSelection {
	want := bound.Target
	if member.Receiver != ValueRef {
		want = types.NewPointer(want)
	}
	selection := &MemberSelection{Member: member, Bound: bound, Receiver: receiver, Signature: bound.MethodSignature(member)}
	if !types.Identical(actual, want) {
		if p, ok := want.(*types.Pointer); ok && types.Identical(actual, p.Elem()) {
			selection.Address = true
		}
		if p, ok := actual.(*types.Pointer); ok && types.Identical(p.Elem(), want) {
			selection.Deref = true
		}
	}
	return selection
}

func (c *checker) selectReceiver(m *Member, expr ast.Expr, actual types.Type) *MemberSelection {
	want := m.Impl.Target
	if m.Receiver != ValueRef {
		want = types.NewPointer(want)
	}
	selection := &MemberSelection{Member: m, Impl: m.Impl, Receiver: expr, Signature: m.Signature}
	if !types.Identical(actual, want) {
		if p, ok := want.(*types.Pointer); ok && types.Identical(actual, p.Elem()) {
			selection.Address = true
		} else if p, ok := actual.(*types.Pointer); ok && types.Identical(p.Elem(), want) {
			selection.Deref = true
		}
	}
	return selection
}

func (c *checker) qualifiedCall(call *ast.CallExpr) bool {
	path, ok := call.Fun.(*ast.PathExpr)
	if !ok || path.Path == nil || len(path.Path.Segments) < 2 || len(call.Args) == 0 {
		return false
	}
	parts := path.Path.Segments
	trait := c.traitPath(parts[:len(parts)-1])
	if trait == nil {
		return false
	}
	name := parts[len(parts)-1].Name.Name
	if memberNamed(trait.Methods, name, false) == nil {
		return false
	}
	actual := c.expr(call.Args[0], nil)
	prefix := *path.Path
	prefix.Segments = parts[:len(parts)-1]
	qualified := &ast.QualifiedSelectorExpr{Base: path.Base, X: call.Args[0], Trait: &ast.PathType{Base: path.Base, Path: &prefix}, Sel: parts[len(parts)-1].Name}
	selection, handled := c.parameterizedMember(qualified, call.Args[1:], nil, true, call.Ellipsis)
	if !handled {
		selection = c.instanceMember(path, call.Args[0], actual, name, trait)
	}
	if selection == nil {
		return false
	}
	selection.MethodExpr = true
	var params []*types.Var
	// The call expression's first argument is adapted by lower using this selection.
	receiver := actual
	if selection.Impl != nil {
		receiver = selection.Impl.Target
		if selection.Member.Receiver != ValueRef {
			receiver = types.NewPointer(receiver)
		}
	}
	params = append(params, types.NewVar(0, c.pkg, "self", receiver))
	for i := 0; i < selection.Signature.Params().Len(); i++ {
		params = append(params, selection.Signature.Params().At(i))
	}
	selection.Signature = types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), selection.Signature.Results(), selection.Signature.Variadic())
	c.out.Members[path] = selection
	c.out.Types[path] = selection.Signature
	return true
}

func (c *checker) adaptTrait(node ast.Expr, actual, expected types.Type) types.Type {
	trait := c.traitOf(expected)
	if trait == nil || !valid(actual) {
		return actual
	}
	if !trait.Runtime {
		c.diag(node, TraitInvalid, "trait cannot form a runtime view because its methods require concrete Self")
		return actual
	}
	if c.traitOf(actual) != nil {
		if !types.Identical(actual, expected) {
			c.diag(node, TraitInvalid, "trait view has a different trait or associated binding")
		}
		return actual
	}
	var impl *Impl
	lookupTarget, base := c.receiverTargets(node, actual)
	n := types.Unalias(expected).(*types.Named)
	goal := &TraitBound{info: c.out, Trait: trait, Target: lookupTarget}
	if known := c.out.traitApplications[n]; known != nil {
		copy := *known
		goal = &copy
		goal.Target = lookupTarget
	} else {
		for index := range trait.TypeParams {
			if index < n.TypeArgs().Len() {
				goal.Inputs = append(goal.Inputs, n.TypeArgs().At(index))
			}
		}
	}
	goal.targetPermission = c.memberSource(node, actual)
	if !types.Identical(actual, lookupTarget) && goal.targetPermission != nil {
		goal.targetPermission = goal.targetPermission.elem
	}
	impl = c.proveApplication(goal).Impl
	if impl == nil {
		goal.Target = base
		if goal.targetPermission != nil && !types.Identical(goal.targetPermission.typ, base) {
			goal.targetPermission = goal.targetPermission.elem
		}
		impl = c.proveApplication(goal).Impl
	}
	if impl == nil {
		c.diag(node, TraitInvalid, "runtime trait conversion requires an explicit implementation")
		return actual
	}
	for offset, a := range trait.Associated {
		i := len(trait.TypeParams) + offset
		if i >= n.TypeArgs().Len() || !types.Identical(n.TypeArgs().At(i), impl.Associated[a.Name]) {
			c.diag(node, TraitInvalid, "associated binding does not match implementation: "+a.Name)
		}
	}
	want := impl.Target
	if impl.Capture != ValueRef {
		want = types.NewPointer(want)
	}
	view := &TraitView{Impl: impl, Type: expected, SourceType: actual, Receiver: node, Capture: impl.Capture}
	if !types.Identical(actual, want) {
		if p, ok := want.(*types.Pointer); ok && types.Identical(actual, p.Elem()) {
			view.Address = true
		} else if p, ok := actual.(*types.Pointer); ok && types.Identical(p.Elem(), want) {
			view.Deref = true
		} else {
			c.diag(node, TraitInvalid, "trait receiver does not match implementation target")
		}
	}
	c.out.TraitViews[node] = view
	return expected
}
