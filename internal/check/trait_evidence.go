package check

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type proofObligation struct {
	target types.Type
	trait  *Trait
}

func (c *checker) genericParameters(g *ast.GenericParams) []*types.TypeParam {
	var params []*types.TypeParam
	if g != nil {
		for _, group := range g.Params {
			for _, id := range group.Names {
				p := types.NewTypeParam(types.NewTypeName(0, c.pkg, id.Name, nil), anyType)
				c.out.Symbols[c.symbol(id)] = p
				params = append(params, p)
			}
		}
	}
	return params
}

func (c *checker) traitBound(target types.Type, node ast.Type, used map[string]bool) *TraitBound {
	path, ok := node.(*ast.PathType)
	if !ok || path.Path == nil || len(path.Path.Segments) == 0 {
		return nil
	}
	trait := c.traitPath(path.Path.Segments)
	if trait == nil {
		return nil
	}
	b := &TraitBound{File: c.file.Name, Target: target, Trait: trait, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}}
	b.Param, _ = target.(*types.TypeParam)
	last := path.Path.Segments[len(path.Path.Segments)-1]
	c.readAssociated(last.Args, trait, b.Associated, node)
	if last.Args != nil {
		for _, arg := range last.Args.Args {
			if arg.Name != nil {
				b.AssociatedNodes[arg.Name.Name] = arg.Type
			}
		}
	}
	for _, a := range trait.Associated {
		if b.Associated[a.Name] != nil {
			continue
		}
		stem := "Target"
		if b.Param != nil {
			stem = b.Param.Obj().Name()
		}
		base := "Goxide" + stem + a.Name
		name := base
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s%d", base, i)
		}
		used[name] = true
		p := types.NewTypeParam(types.NewTypeName(0, c.pkg, name, nil), anyType)
		b.Associated[a.Name] = p
		b.Projected = append(b.Projected, p)
	}
	return b
}

func projectedBinding(bound *TraitBound, name string) *types.TypeParam {
	for _, param := range bound.Projected {
		if bound.Associated[name] == param {
			return param
		}
	}
	return nil
}

// Repeated premises describe the same associated outputs. Missing bindings do
// not create a second output identity; explicit bindings refine that identity.
func (c *checker) addBound(list *[]*TraitBound, bound *TraitBound, node ast.Node) *TraitBound {
	for _, old := range *list {
		if old.Trait != bound.Trait || !types.Identical(old.Target, bound.Target) {
			continue
		}
		for _, a := range old.Trait.Associated {
			left, right := old.Associated[a.Name], bound.Associated[a.Name]
			leftParam, rightParam := projectedBinding(old, a.Name), projectedBinding(bound, a.Name)
			if rightParam != nil {
				continue
			}
			if leftParam != nil {
				sub := map[*types.TypeParam]types.Type{leftParam: right}
				for _, existing := range *list {
					existing.Target = substitute(existing.Target, sub)
					for name, typ := range existing.Associated {
						existing.Associated[name] = substitute(typ, sub)
					}
					var projected []*types.TypeParam
					for _, param := range existing.Projected {
						if param != leftParam {
							projected = append(projected, param)
						}
					}
					existing.Projected = projected
				}
				for typ, override := range c.out.TypeOverrides {
					c.out.TypeOverrides[typ] = substitute(override, sub)
				}
				old.AssociatedNodes[a.Name] = bound.AssociatedNodes[a.Name]
				continue
			}
			conflict := !types.Identical(left, right)
			if !conflict && old.AssociatedNodes[a.Name] != nil && bound.AssociatedNodes[a.Name] != nil {
				p := c.newPermissionChecker()
				conflict = !p.sameSourcePermissions(p.boundAssociated(old, a.Name, nil), p.boundAssociated(bound, a.Name, nil), map[[2]types.Type]bool{})
			}
			if conflict {
				c.diag(node, TraitInvalid, "conflicting associated bindings for constraint "+old.Trait.Symbol.Name.Name)
			}
		}
		return old
	}
	*list = append(*list, bound)
	if bound.Param != nil {
		for _, old := range c.bounds[bound.Param] {
			if old.Trait == bound.Trait {
				return bound
			}
		}
		c.bounds[bound.Param] = append(c.bounds[bound.Param], bound)
	}
	return bound
}

func freshRequirement(bound *TraitBound, sub map[*types.TypeParam]types.Type, used map[string]bool, pkg *types.Package) *TraitBound {
	copy := substituteBound(bound, sub)
	copy.Projected = nil
	fresh := map[*types.TypeParam]types.Type{}
	for _, param := range bound.Projected {
		if sub[param] != nil {
			continue
		}
		base, name := param.Obj().Name(), param.Obj().Name()
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s%d", base, i)
		}
		used[name] = true
		next := types.NewTypeParam(types.NewTypeName(0, pkg, name, nil), anyType)
		fresh[param] = next
		copy.Projected = append(copy.Projected, next)
	}
	copy.Target = substitute(copy.Target, fresh)
	for name, typ := range copy.Associated {
		copy.Associated[name] = substitute(typ, fresh)
	}
	return copy
}

func (c *checker) sourceRequirements(g *ast.GenericParams, where *ast.WhereClause, params []*types.TypeParam, used map[string]bool, node ast.Node) []*TraitBound {
	var result []*TraitBound
	add := func(target types.Type, constraint ast.Type) {
		if b := c.traitBound(target, constraint, used); b != nil {
			c.addBound(&result, b, node)
			return
		}
		constraintType := c.typ(constraint)
		if p, ok := target.(*types.TypeParam); ok {
			p.SetConstraint(types.NewInterfaceType(nil, []types.Type{p.Constraint(), constraintType}).Complete())
		} else {
			c.diag(constraint, TraitInvalid, "a concrete where target requires a declared trait constraint")
		}
	}
	index := 0
	if g != nil {
		for _, group := range g.Params {
			for range group.Names {
				if group.Constraint != nil {
					add(params[index], group.Constraint)
				}
				index++
			}
		}
	}
	if where != nil {
		for _, predicate := range where.Predicates {
			target := c.typ(predicate.Type)
			for _, constraint := range predicate.Bounds {
				add(target, constraint)
			}
		}
	}
	return result
}

func substituteBound(b *TraitBound, sub map[*types.TypeParam]types.Type) *TraitBound {
	copy := *b
	copy.substitution = map[*types.TypeParam]types.Type{}
	for param, typ := range b.substitution {
		copy.substitution[param] = substitute(typ, sub)
	}
	for param, typ := range sub {
		if copy.substitution[param] == nil {
			copy.substitution[param] = typ
		}
	}
	copy.Target = substitute(b.Target, sub)
	copy.Param, _ = copy.Target.(*types.TypeParam)
	copy.Associated = map[string]types.Type{}
	for name, t := range b.Associated {
		copy.Associated[name] = substitute(t, sub)
	}
	return &copy
}

func (c *checker) inheritedBounds(bounds []*TraitBound, node ast.Node) []*TraitBound {
	result := append([]*TraitBound(nil), bounds...)
	used := map[string]bool{}
	for _, b := range bounds {
		if b.Param != nil {
			used[b.Param.Obj().Name()] = true
		}
		for _, p := range b.Projected {
			used[p.Obj().Name()] = true
		}
	}
	var add func(*TraitBound, map[*Trait]bool)
	add = func(b *TraitBound, path map[*Trait]bool) {
		if path[b.Trait] {
			return
		}
		path[b.Trait] = true
		defer delete(path, b.Trait)
		sub := map[*types.TypeParam]types.Type{b.Trait.Self: b.Target}
		for _, a := range b.Trait.Associated {
			sub[a.Param] = b.Associated[a.Name]
		}
		for _, requirement := range b.Trait.Requirements {
			next := freshRequirement(requirement, sub, used, c.pkg)
			next = c.addBound(&result, next, node)
			add(next, path)
		}
	}
	for _, b := range bounds {
		add(b, map[*Trait]bool{})
	}
	return result
}

func (c *checker) boundFor(target types.Type, trait *Trait) *TraitBound {
	for _, b := range c.boundsFor(target) {
		if b.Trait == trait {
			return b
		}
	}
	return nil
}

func (c *checker) boundsFor(target types.Type) []*TraitBound {
	var result []*TraitBound
	if p, ok := target.(*types.TypeParam); ok {
		result = append(result, c.bounds[p]...)
	}
	add := func(bounds []*TraitBound) {
		for _, b := range bounds {
			if types.Identical(b.Target, target) {
				found := false
				for _, old := range result {
					found = found || old.Trait == b.Trait
				}
				if !found {
					result = append(result, b)
				}
			}
		}
	}
	add(c.activeBounds)
	if c.currentImpl != nil {
		add(c.currentImpl.Requirements)
	}
	return result
}

func (c *checker) prove(target types.Type, trait *Trait) Evidence {
	if len(c.proofStack) == 0 {
		c.proofFailure = ""
	}
	if !valid(target) {
		return Evidence{}
	}
	if b := c.boundFor(target, trait); b != nil {
		return Evidence{Bound: b}
	}
	for _, old := range c.proofStack {
		if old.trait == trait && types.Identical(old.target, target) {
			c.proofFailure = "cyclic trait prerequisite: " + c.proofDescription(target, trait)
			return Evidence{}
		}
	}
	if len(c.proofStack) >= 128 {
		c.proofFailure = "trait prerequisite expansion limit exceeded: " + c.proofDescription(target, trait)
		return Evidence{}
	}
	c.proofStack = append(c.proofStack, proofObligation{target, trait})
	defer func() { c.proofStack = c.proofStack[:len(c.proofStack)-1] }()
	for _, template := range c.preferredImpls() {
		if template.Trait != trait {
			continue
		}
		if instance := c.matchImpl(template, target); instance != nil {
			return Evidence{Impl: instance}
		}
	}
	return Evidence{}
}

func (c *checker) checkTraitPrerequisiteCycles() {
	state := map[*Trait]uint8{}
	var stack []*Trait
	var visit func(*Trait)
	visit = func(trait *Trait) {
		if state[trait] == 2 {
			return
		}
		if state[trait] == 1 {
			var names []string
			for _, entry := range stack {
				names = append(names, entry.Symbol.Name.Name)
			}
			names = append(names, trait.Symbol.Name.Name)
			old := c.file
			c.file = c.out.Resolve.Files[trait.File]
			c.diag(trait.Decl, TraitInvalid, "cyclic trait prerequisites: "+strings.Join(names, " -> "))
			c.file = old
			return
		}
		state[trait] = 1
		stack = append(stack, trait)
		for _, requirement := range trait.Requirements {
			visit(requirement.Trait)
		}
		stack = stack[:len(stack)-1]
		state[trait] = 2
	}
	c.eachTrait(visit)
}

func (c *checker) validateConcreteImpls() {
	for _, impl := range c.implOrder {
		if len(impl.TypeParams) != 0 || len(impl.Requirements) == 0 {
			continue
		}
		c.file, c.pkg = c.out.Resolve.Files[impl.File], impl.Owner
		if c.matchImpl(impl, impl.Target) == nil {
			message := "implementation prerequisites are not satisfied"
			if c.proofFailure != "" {
				message += ": " + c.proofFailure
			}
			c.diag(impl.Decl, TraitInvalid, message)
		}
	}
}

func (c *checker) matchImpl(template *Impl, target types.Type) *Impl {
	sub, ok := matchImplTarget(template.Target, target, template.TypeParams)
	if !ok {
		return nil
	}
	for _, p := range template.TypeParams {
		constraint, ok := substitute(p.Constraint(), sub).Underlying().(*types.Interface)
		if ok && !types.Satisfies(sub[p], constraint) {
			return nil
		}
	}
	var evidence []Evidence
	for _, requirement := range template.Requirements {
		bound := substituteBound(requirement, sub)
		witness := c.prove(bound.Target, bound.Trait)
		if witness.Impl == nil && witness.Bound == nil {
			return nil
		}
		bindings := evidenceAssociated(witness)
		for _, a := range bound.Trait.Associated {
			want := bound.Associated[a.Name]
			projected := false
			for _, p := range requirement.Projected {
				if requirement.Associated[a.Name] == p {
					sub[p], projected = bindings[a.Name], true
				}
			}
			if !projected && !types.Identical(want, bindings[a.Name]) {
				return nil
			}
			if !projected {
				p := c.newPermissionChecker()
				var actual *permission
				if witness.Impl != nil {
					actual = p.implAssociated(witness.Impl, a.Name, nil)
				}
				if witness.Bound != nil {
					actual = p.boundAssociated(witness.Bound, a.Name, nil)
				}
				if actual != nil && !p.sameSourcePermissions(actual, p.boundAssociated(bound, a.Name, nil), map[[2]types.Type]bool{}) {
					return nil
				}
			}
		}
		evidence = append(evidence, witness)
	}
	if len(template.TypeParams) == 0 && len(template.Requirements) == 0 {
		return template
	}
	for _, old := range c.out.ImplInstances {
		if old.Template == template && types.Identical(old.Target, target) && sameEvidence(old.RequirementEvidence, evidence) {
			return old
		}
	}
	instance := *template
	instance.Template, instance.Target = template, target
	instance.TypeArgs = nil
	for _, p := range template.TypeParams {
		instance.TypeArgs = append(instance.TypeArgs, sub[p])
	}
	instance.RequirementEvidence = evidence
	instance.Substitution = sub
	instance.Associated = map[string]types.Type{}
	for name, t := range template.Associated {
		instance.Associated[name] = substitute(t, sub)
	}
	instance.Methods = nil
	for _, method := range template.Methods {
		copy := *method
		copy.Impl = &instance
		copy.Signature = substituteSignature(method.Signature, sub)
		instance.Methods = append(instance.Methods, &copy)
	}
	instance.Requirements = nil
	for _, requirement := range template.Requirements {
		instance.Requirements = append(instance.Requirements, substituteBound(requirement, sub))
	}
	c.out.ImplInstances = append(c.out.ImplInstances, &instance)
	return &instance
}

func evidenceAssociated(e Evidence) map[string]types.Type {
	if e.Bound != nil {
		return e.Bound.Associated
	}
	if e.Impl != nil {
		return e.Impl.Associated
	}
	return nil
}

func sameEvidence(left, right []Evidence) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (c *checker) proofDescription(target types.Type, trait *Trait) string {
	var chain []string
	for _, obligation := range c.proofStack {
		chain = append(chain, types.TypeString(obligation.target, nil)+": "+obligation.trait.Symbol.Name.Name)
	}
	chain = append(chain, types.TypeString(target, nil)+": "+trait.Symbol.Name.Name)
	return strings.Join(chain, " -> ")
}
