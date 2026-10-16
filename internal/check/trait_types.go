package check

import (
	"fmt"
	"go/types"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (c *checker) traitType(node ast.Type) (types.Type, bool) {
	switch n := node.(type) {
	case *ast.SelfType:
		if c.currentSelf != nil {
			return c.currentSelf, true
		}
		if t := c.out.TypeOverrides[node]; t != nil {
			return t, true
		}
		if s := c.symbol(n); s != nil {
			for decl, impl := range c.out.Impls {
				if s.Decl.File == impl.File && s.Decl.ID == decl.ID() {
					return impl.Target, true
				}
			}
			for decl, trait := range c.out.Traits {
				if s.Decl.File == trait.File && s.Decl.ID == decl.ID() {
					return trait.Self, true
				}
			}
		}
		return types.Typ[types.Invalid], true
	case *ast.ProjectionType:
		if _, ok := n.X.(*ast.SelfType); ok {
			return c.selfAssociated(n.Item.Name, n), true
		}
	case *ast.PathType:
		if n.Path == nil || len(n.Path.Segments) == 0 {
			return nil, false
		}
		parts := n.Path.Segments
		if target, trait, qualifier := c.associatedPath(parts); trait != nil {
			c.checkProjectionBindings(target, trait, qualifier.Args, n)
			return c.projectAssociated(target, trait, parts[len(parts)-1].Name.Name, n), true
		}
		last := parts[len(parts)-1]
		if trait := c.traitPath(parts); trait != nil {
			if !trait.Runtime {
				c.diag(n, TraitInvalid, "trait cannot form a runtime view because its methods require concrete Self")
			}
			bindings := map[string]types.Type{}
			c.readAssociated(last.Args, trait, bindings, n)
			args := make([]types.Type, 0, len(trait.Associated))
			for _, a := range trait.Associated {
				v := bindings[a.Name]
				if v == nil {
					c.diag(n, TraitInvalid, "runtime trait requires associated type "+a.Name)
					v = types.Typ[types.Invalid]
				}
				args = append(args, v)
			}
			if len(args) == 0 {
				return trait.Type, true
			}
			t, err := types.Instantiate(nil, trait.Type, args, false)
			if err != nil {
				return types.Typ[types.Invalid], true
			}
			return t, true
		}
	}
	return nil, false
}

// A projection contains two independently resolved paths: the source type and
// the selected trait. Either prefix may be package-qualified.
func (c *checker) associatedPath(parts []*ast.PathSegment) (types.Type, *Trait, *ast.PathSegment) {
	for split := 1; split < len(parts)-1; split++ {
		target := c.symbolType(c.sourcePathSymbol(parts[:split]))
		if split == 1 && parts[0].Name.Name == "Self" && c.currentSelf != nil {
			target = c.currentSelf
		}
		if !valid(target) {
			continue
		}
		if trait := c.traitPath(parts[split : len(parts)-1]); trait != nil {
			if args := parts[split-1].Args; args != nil {
				target = c.instantiate(target, args)
			}
			return target, trait, parts[len(parts)-2]
		}
	}
	return nil, nil, nil
}

func (c *checker) checkProjectionBindings(target types.Type, trait *Trait, args *ast.GenericArgs, node ast.Node) {
	if args == nil {
		return
	}
	provided := map[string]types.Type{}
	c.readAssociated(args, trait, provided, node)
	var bindings map[string]types.Type
	var bound *TraitBound
	var impl *Impl
	if param, ok := target.(*types.TypeParam); ok {
		bound = c.bounds[param]
		if bound != nil && bound.Trait == trait {
			bindings = bound.Associated
		}
	} else {
		impl = c.findImpl(target, trait)
		if impl != nil {
			bindings = impl.Associated
		}
	}
	p := c.newPermissionChecker()
	for _, arg := range args.Args {
		if arg.Name == nil {
			continue
		}
		name := arg.Name.Name
		if !types.Identical(provided[name], bindings[name]) {
			c.diag(arg, TraitInvalid, "associated projection binding conflicts with implementation: "+name)
			continue
		}
		var want *permission
		if bound != nil {
			want = p.boundAssociated(bound, name, nil)
		} else if impl != nil {
			want = p.implAssociated(impl, name, nil)
		}
		if want != nil && !p.sameSourcePermissions(p.astType(arg.Type, nil), want, map[[2]types.Type]bool{}) {
			c.diag(arg, TraitInvalid, "associated projection reference permissions conflict with implementation: "+name)
		}
	}
}

func (c *checker) selfAssociated(name string, node ast.Node) types.Type {
	if c.currentImpl != nil {
		return c.resolveAssociated(c.currentImpl, name, node)
	}
	if c.currentTrait != nil {
		for _, a := range c.currentTrait.Associated {
			if a.Name == name {
				return a.Param
			}
		}
	}
	if typ, ok := node.(ast.Type); ok {
		if t := c.out.TypeOverrides[typ]; t != nil {
			return t
		}
	}
	c.diag(node, TraitInvalid, "unknown associated type "+name)
	return types.Typ[types.Invalid]
}

func (c *checker) projectAssociated(target types.Type, trait *Trait, name string, node ast.Node) types.Type {
	if param, ok := target.(*types.TypeParam); ok {
		if bound := c.bounds[param]; bound != nil && bound.Trait == trait {
			if t := bound.Associated[name]; t != nil {
				return t
			}
		}
	} else if impl := c.findImpl(target, trait); impl != nil {
		if t := impl.Associated[name]; t != nil {
			return t
		}
	}
	c.diag(node, TraitInvalid, "associated projection requires an explicit implementation of "+trait.Symbol.Name.Name)
	return types.Typ[types.Invalid]
}

func (c *checker) findImpl(target types.Type, trait *Trait) *Impl {
	if target == nil {
		return nil
	}
	for _, impl := range c.preferredImpls() {
		if impl.Trait == trait && types.Identical(types.Unalias(target), types.Unalias(impl.Target)) {
			return impl
		}
	}
	return nil
}

func (c *checker) preferredImpls() []*Impl {
	result := make([]*Impl, 0, len(c.implOrder))
	for _, impl := range c.implOrder {
		if c.sameSourcePackage(impl.File, c.file.Name) {
			result = append(result, impl)
		}
	}
	for _, impl := range c.implOrder {
		if !c.sameSourcePackage(impl.File, c.file.Name) {
			result = append(result, impl)
		}
	}
	return result
}

func (c *checker) functionParams(fn *ast.FuncDecl) []*types.TypeParam {
	if fn.Generics == nil {
		return nil
	}
	var result []*types.TypeParam
	used := map[string]bool{}
	ast.Walk(fn, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for _, p := range fn.Generics.Params {
		for _, id := range p.Names {
			param := types.NewTypeParam(types.NewTypeName(0, c.pkg, id.Name, nil), anyType)
			result = append(result, param)
			c.out.Symbols[c.symbol(id)] = param
			used[id.Name] = true
		}
	}
	index := 0
	for _, p := range fn.Generics.Params {
		for range p.Names {
			param := result[index]
			index++
			if p.Constraint == nil {
				continue
			}
			if !c.addTraitBound(fn, param, p.Constraint, used) {
				param.SetConstraint(c.typ(p.Constraint))
			}
		}
	}
	if fn.Where != nil {
		for _, pred := range fn.Where.Predicates {
			param, ok := c.typ(pred.Type).(*types.TypeParam)
			if !ok || len(pred.Bounds) != 1 {
				c.diag(pred, TraitInvalid, "where requires one constraint on a function type parameter")
				continue
			}
			if c.bounds[param] != nil {
				c.diag(pred, TraitInvalid, "a type parameter may have only one trait constraint")
				continue
			}
			if !c.addTraitBound(fn, param, pred.Bounds[0], used) {
				param.SetConstraint(c.typ(pred.Bounds[0]))
			}
		}
	}
	for _, bound := range c.out.GenericBounds[fn] {
		result = append(result, bound.Projected...)
	}
	return result
}

func (c *checker) addTraitBound(fn *ast.FuncDecl, param *types.TypeParam, node ast.Type, used map[string]bool) bool {
	path, ok := node.(*ast.PathType)
	if !ok || path.Path == nil || len(path.Path.Segments) == 0 {
		return false
	}
	last := path.Path.Segments[len(path.Path.Segments)-1]
	trait := c.traitPath(path.Path.Segments)
	if trait == nil {
		return false
	}
	bound := &TraitBound{File: c.file.Name, Param: param, Trait: trait, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}}
	c.readAssociated(last.Args, trait, bound.Associated, node)
	if last.Args != nil {
		for _, a := range last.Args.Args {
			if a.Name != nil {
				bound.AssociatedNodes[a.Name.Name] = a.Type
			}
		}
	}
	for _, a := range trait.Associated {
		if bound.Associated[a.Name] != nil {
			continue
		}
		base := "Goxide" + param.Obj().Name() + a.Name
		name := base
		for index := 2; used[name]; index++ {
			name = fmt.Sprintf("%s%d", base, index)
		}
		used[name] = true
		projected := types.NewTypeParam(types.NewTypeName(0, c.pkg, name, nil), anyType)
		bound.Associated[a.Name] = projected
		bound.Projected = append(bound.Projected, projected)
	}
	c.bounds[param] = bound
	c.out.GenericBounds[fn] = append(c.out.GenericBounds[fn], bound)
	return true
}

func (c *checker) sourceArity(sig *types.Signature) int {
	count := sig.TypeParams().Len()
	for _, bound := range c.functionBounds[sig] {
		count -= len(bound.Projected)
	}
	return count
}

func (c *checker) instantiateFunctionType(sig *types.Signature, args []types.Type, node ast.Node) types.Type {
	if len(args) != c.sourceArity(sig) {
		c.diag(node, GenericConstraint, fmt.Sprintf("got %d type arguments but want %d", len(args), c.sourceArity(sig)))
		return types.Typ[types.Invalid]
	}
	actual := append([]types.Type(nil), args...)
	for _, bound := range c.functionBounds[sig] {
		var target types.Type
		for i, arg := range args {
			if sig.TypeParams().At(i) == bound.Param {
				target = arg
				break
			}
		}
		bindings := map[string]types.Type{}
		var sourceImpl *Impl
		var sourceBound *TraitBound
		if tp, ok := target.(*types.TypeParam); ok {
			if other := c.bounds[tp]; other != nil && other.Trait == bound.Trait {
				bindings = other.Associated
				sourceBound = other
			}
		} else if impl := c.findImpl(target, bound.Trait); impl != nil {
			sourceImpl = impl
			local := false
			for _, n := range c.named {
				local = local || n == impl.Base
			}
			switch impl.Base.Underlying().(type) {
			case *types.Pointer:
				local = false
			case *types.Interface:
				local = local && c.out.EnumOf(impl.Base) != nil
			}
			if !local || !types.Identical(impl.Target, impl.Base) {
				c.diag(node, GenericConstraint, "foreign or pointer implementation has no local static carrier")
			} else {
				bindings = impl.Associated
			}
		} else {
			c.diag(node, GenericConstraint, "type does not explicitly implement "+bound.Trait.Symbol.Name.Name)
		}
		for _, a := range bound.Trait.Associated {
			got := bindings[a.Name]
			if got == nil {
				got = types.Typ[types.Invalid]
			}
			want := bound.Associated[a.Name]
			projected := false
			for _, p := range bound.Projected {
				if want == p {
					projected = true
					actual = append(actual, got)
					break
				}
			}
			if !projected && !types.Identical(got, want) {
				c.diag(node, GenericConstraint, "associated type "+a.Name+" does not satisfy constraint")
			} else if !projected && c.referenceHints != nil {
				p := c.referenceHints
				var source *permission
				if sourceImpl != nil {
					source = p.implAssociated(sourceImpl, a.Name, nil)
				} else if sourceBound != nil {
					source = p.boundAssociated(sourceBound, a.Name, nil)
				}
				if source != nil && !p.sameSourcePermissions(source, p.boundAssociated(bound, a.Name, nil), map[[2]types.Type]bool{}) {
					c.diag(node, GenericConstraint, "associated reference permissions do not satisfy constraint: "+a.Name)
				}
			}
		}
	}
	t, err := types.Instantiate(nil, sig, actual, true)
	if err != nil {
		c.diag(node, GenericConstraint, err.Error())
		return types.Typ[types.Invalid]
	}
	return t
}
