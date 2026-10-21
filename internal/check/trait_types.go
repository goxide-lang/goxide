package check

import (
	"fmt"
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
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
	bound = c.boundFor(target, trait)
	if bound != nil {
		bindings = bound.Associated
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
	if bound := c.boundFor(target, trait); bound != nil {
		if t := bound.Associated[name]; t != nil {
			return t
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
	return c.prove(target, trait).Impl
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
	result := c.genericParameters(fn.Generics)
	used := map[string]bool{}
	ast.Walk(fn, func(node ast.Node) bool {
		if id, ok := node.(*ast.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	bounds := c.sourceRequirements(fn.Generics, fn.Where, result, used, fn)
	bounds = c.inheritedBounds(bounds, fn)
	if len(bounds) != 0 {
		c.out.GenericBounds[fn] = bounds
	}
	for _, bound := range bounds {
		result = append(result, bound.Projected...)
	}
	return result
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
	sub := map[*types.TypeParam]types.Type{}
	for i, arg := range args {
		sub[sig.TypeParams().At(i)] = arg
	}
	instance := &FunctionInstance{Signature: sig, TypeArgs: append([]types.Type(nil), args...)}
	for _, bound := range c.functionBounds[sig] {
		target := substitute(bound.Target, sub)
		witness := c.prove(target, bound.Trait)
		instance.Evidence = append(instance.Evidence, witness)
		bindings := evidenceAssociated(witness)
		if witness.Impl == nil && witness.Bound == nil {
			message := "type does not explicitly implement " + bound.Trait.Symbol.Name.Name
			if c.proofFailure != "" {
				message += ": " + c.proofFailure
			}
			c.diag(node, GenericConstraint, message)
		}
		for _, a := range bound.Trait.Associated {
			got := bindings[a.Name]
			if got == nil {
				got = types.Typ[types.Invalid]
			}
			want := substitute(bound.Associated[a.Name], sub)
			projected := false
			for _, p := range bound.Projected {
				if bound.Associated[a.Name] == p {
					projected = true
					actual = append(actual, got)
					sub[p] = got
					break
				}
			}
			if !projected && !types.Identical(got, want) {
				c.diag(node, GenericConstraint, "associated type "+a.Name+" does not satisfy constraint")
			} else if !projected && c.referenceHints != nil {
				p := c.referenceHints
				var source *permission
				if witness.Impl != nil {
					source = p.implAssociated(witness.Impl, a.Name, nil)
				}
				if witness.Bound != nil {
					source = p.boundAssociated(witness.Bound, a.Name, nil)
				}
				permissions := map[*types.TypeParam]*permission{}
				for param, typ := range sub {
					permissions[param] = p.fromGo(typ)
				}
				if source != nil && !p.sameSourcePermissions(source, p.boundAssociated(bound, a.Name, permissions), map[[2]types.Type]bool{}) {
					c.diag(node, GenericConstraint, "associated reference permissions do not satisfy constraint: "+a.Name)
				}
			}
		}
	}
	instance.Substitution = sub
	c.out.FunctionInstances[node] = instance
	t, err := c.instantiateGo(sig, actual)
	if err != nil {
		c.diag(node, GenericConstraint, err.Error())
		return types.Typ[types.Invalid]
	}
	return t
}
