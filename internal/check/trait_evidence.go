package check

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type proofObligation struct{ goal *TraitBound }

func (c *checker) genericParameters(g *ast.GenericParams) []*types.TypeParam {
	c.constParameters(g)
	var params []*types.TypeParam
	if g != nil {
		for _, group := range g.Params {
			if group.Const {
				continue
			}
			for _, id := range group.Names {
				p := types.NewTypeParam(types.NewTypeName(0, c.pkg, id.Name, nil), anyType)
				c.out.Symbols[c.symbol(id)] = p
				if c.out.parameterIdentities == nil {
					c.out.parameterIdentities = map[*types.TypeParam]string{}
				}
				c.out.parameterIdentities[p] = c.genericParameterIdentity(g, len(params))
				params = append(params, p)
			}
		}
	}
	return params
}

func (c *checker) genericParameterIdentity(g *ast.GenericParams, index int) string {
	owner := ""
	ast.Walk(c.file.AST, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			if n.Generics == g {
				owner = "func:" + n.Name.Name
			}
		case *ast.TypeDecl:
			if n.Generics == g {
				owner = "type:" + n.Name.Name
			}
		case *ast.TraitDecl:
			if n.Generics == g {
				owner = "trait:" + n.Name.Name
			}
		case *ast.EnumDecl:
			if n.Generics == g {
				owner = "enum:" + n.Name.Name
			}
		}
		return true
	})
	path := c.out.CanonicalPackagePath(c.pkg)
	return fmt.Sprintf("%s::%s:%d", path, owner, index)
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
	b := &TraitBound{info: c.out, File: c.file.Name, Target: target, Trait: trait, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}}
	if _, array := c.out.ArrayOf(target); !array {
		b.Param, _ = target.(*types.TypeParam)
	}
	last := path.Path.Segments[len(path.Path.Segments)-1]
	b.Inputs, b.InputNodes, b.inputPermissions = c.readTraitInputArguments(last.Args, trait, node)
	b.targetPermission = c.newPermissionChecker().fromGo(target)
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
	b.associatedPermissions = map[string]*permission{}
	p := c.newPermissionChecker()
	for name := range b.Associated {
		b.associatedPermissions[name] = p.boundAssociated(b, name, nil)
	}
	c.inheritTraitGoRequirements(b)
	if len(b.Inputs) == len(trait.TypeParams) {
		c.checkTraitInputConstraints(b.Inputs, trait, node)
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
		if !c.sameApplication(old, bound) {
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
				sources := map[*types.TypeParam]*permission{leftParam: c.newPermissionChecker().boundAssociated(bound, a.Name, nil)}
				for _, existing := range *list {
					*existing = *c.substituteBoundApplication(existing, sub, nil, sources)
					var projected []*types.TypeParam
					for _, param := range existing.Projected {
						if param != leftParam {
							projected = append(projected, param)
						}
					}
					existing.Projected = projected
				}
				for typ, override := range c.out.TypeOverrides {
					c.out.TypeOverrides[typ] = substitute(override, sub, c.out)
				}
				old.AssociatedNodes[a.Name] = bound.AssociatedNodes[a.Name]
				old.associatedPermissions[a.Name] = sources[leftParam]
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
			if c.sameApplication(old, bound) {
				return bound
			}
		}
		c.bounds[bound.Param] = append(c.bounds[bound.Param], bound)
	}
	return bound
}

func (c *checker) freshRequirement(bound *TraitBound, sub map[*types.TypeParam]types.Type, sources map[*types.TypeParam]*permission, used map[string]bool, pkg *types.Package) *TraitBound {
	copy := c.substituteBoundApplication(bound, sub, nil, sources)
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
	copy = substituteBound(copy, fresh)
	for name, typ := range copy.Associated {
		copy.Associated[name] = substitute(typ, fresh, copy.info)
	}
	return copy
}

func (c *checker) sourceRequirements(g *ast.GenericParams, where *ast.WhereClause, params []*types.TypeParam, used map[string]bool, node ast.Node) []*TraitBound {
	var result []*TraitBound
	add := func(target types.Type, targetNode ast.Type, constraint ast.Type) {
		if b := c.traitBound(target, constraint, used); b != nil {
			b.TargetNode = targetNode
			if targetNode != nil {
				b.targetPermission = c.newPermissionChecker().astType(targetNode, nil)
			}
			c.addBound(&result, b, node)
			return
		}
		if c.constraintUsesConst(constraint) {
			c.diag(constraint, GenericConstraint, "const array patterns in Go type constraints are not supported")
			return
		}
		constraintType := c.typ(constraint)
		if p, ok := target.(*types.TypeParam); ok && c.out.ArrayPatterns[p] == nil {
			p.SetConstraint(types.NewInterfaceType(nil, []types.Type{p.Constraint(), constraintType}).Complete())
		} else {
			c.diag(constraint, TraitInvalid, "a concrete where target requires a declared trait constraint")
		}
	}
	index := 0
	if g != nil {
		for _, group := range g.Params {
			if group.Const {
				continue
			}
			for range group.Names {
				if group.Constraint != nil {
					add(params[index], nil, group.Constraint)
				}
				index++
			}
		}
	}
	if where != nil {
		for _, predicate := range where.Predicates {
			target := c.typ(predicate.Type)
			for _, constraint := range predicate.Bounds {
				add(target, predicate.Type, constraint)
			}
		}
	}
	return result
}

func substituteBound(b *TraitBound, sub map[*types.TypeParam]types.Type) *TraitBound {
	return substituteBoundConstants(b, sub, nil)
}

func substituteBoundConstants(b *TraitBound, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) *TraitBound {
	copy := *b
	copy.constSubstitution = make(map[*ConstParam]ConstValue, len(b.constSubstitution)+len(constants))
	for parameter, value := range b.constSubstitution {
		copy.constSubstitution[parameter] = resolveConst(value, constants)
	}
	for parameter, value := range constants {
		if _, exists := copy.constSubstitution[parameter]; !exists {
			copy.constSubstitution[parameter] = value
		}
	}
	copy.substitution = map[*types.TypeParam]types.Type{}
	for param, typ := range b.substitution {
		copy.substitution[param] = b.info.Substitute(typ, sub, constants)
	}
	for param, typ := range sub {
		if copy.substitution[param] == nil {
			copy.substitution[param] = typ
		}
	}
	copy.Target = b.info.Substitute(b.Target, sub, constants)
	copy.Inputs = nil
	for _, input := range b.Inputs {
		copy.Inputs = append(copy.Inputs, b.info.Substitute(input, sub, constants))
	}
	copy.Param = nil
	if _, array := b.info.ArrayOf(copy.Target); !array {
		copy.Param, _ = copy.Target.(*types.TypeParam)
	}
	copy.Associated = map[string]types.Type{}
	for name, t := range b.Associated {
		copy.Associated[name] = b.info.Substitute(t, sub, constants)
	}
	copy.Projected = nil
	for _, parameter := range b.Projected {
		if projected, ok := b.info.Substitute(parameter, sub, constants).(*types.TypeParam); ok && !b.info.isArray(projected) {
			copy.Projected = append(copy.Projected, projected)
		}
	}
	c := &checker{out: b.info}
	copy.targetPermission = c.substituteSourcePermission(b.targetPermission, sub, constants, b.permissionSubstitution)
	copy.inputPermissions = nil
	for _, source := range b.inputPermissions {
		copy.inputPermissions = append(copy.inputPermissions, c.substituteSourcePermission(source, sub, constants, b.permissionSubstitution))
	}
	copy.associatedPermissions = map[string]*permission{}
	for name, source := range b.associatedPermissions {
		copy.associatedPermissions[name] = c.substituteSourcePermission(source, sub, constants, b.permissionSubstitution)
	}
	return &copy
}

func (c *checker) substituteBoundApplication(bound *TraitBound, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue, sources map[*types.TypeParam]*permission) *TraitBound {
	copy := *bound
	copy.permissionSubstitution = map[*types.TypeParam]*permission{}
	for parameter, source := range bound.permissionSubstitution {
		copy.permissionSubstitution[parameter] = source
	}
	for parameter, source := range sources {
		copy.permissionSubstitution[parameter] = source
	}
	return substituteBoundConstants(&copy, sub, constants)
}

func (c *checker) evidenceAssociatedPermission(e Evidence, name string) *permission {
	p := c.newPermissionChecker()
	if e.Impl != nil {
		return p.implAssociated(e.Impl, name, nil)
	}
	if e.Bound != nil {
		return p.boundAssociated(e.Bound, name, nil)
	}
	var result *permission
	for _, branch := range e.Cases {
		source := c.evidenceAssociatedPermission(branch.Evidence, name)
		if result != nil && !c.sameSourceType(result, source) {
			return nil
		}
		result = source
	}
	return result
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
		c.inheritTraitGoRequirements(b)
		path[b.Trait] = true
		defer delete(path, b.Trait)
		sub := map[*types.TypeParam]types.Type{b.Trait.Self: b.Target}
		head := c.boundHeadPermissions(b)
		sources := map[*types.TypeParam]*permission{b.Trait.Self: head[0]}
		for index, parameter := range b.Trait.TypeParams {
			if index < len(b.Inputs) {
				sub[parameter] = b.Inputs[index]
				sources[parameter] = head[index+1]
			}
		}
		for _, a := range b.Trait.Associated {
			sub[a.Param] = b.Associated[a.Name]
			sources[a.Param] = c.newPermissionChecker().boundAssociated(b, a.Name, nil)
		}
		for _, requirement := range b.Trait.Requirements {
			next := c.freshRequirement(requirement, sub, sources, used, c.pkg)
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
		if b.Trait == trait && len(b.Inputs) == 0 {
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
					found = found || c.sameApplication(old, b)
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
	return c.proveApplication(&TraitBound{info: c.out, Target: target, Trait: trait})
}

func (c *checker) proveApplication(goal *TraitBound) Evidence {
	if len(c.proofStack) == 0 {
		c.proofFailure = ""
	}
	if goal == nil || goal.Trait == nil || !valid(goal.Target) {
		return Evidence{}
	}
	for _, bound := range c.boundsFor(goal.Target) {
		if c.sameApplication(bound, goal) {
			return Evidence{Bound: bound}
		}
	}
	evidence := c.solveApplication(goal)
	if evidence.Impl != nil {
		evidence = c.implementationBodyEvidence(evidence.Impl)
	}
	if len(evidence.Cases) != 0 && c.caseState != nil {
		evidence = c.recordBodyChoice(goal, evidence)
	}
	return evidence
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
		if len(impl.TypeParams) != 0 || len(impl.ConstParams) != 0 || impl.Trait == nil && len(impl.Requirements) == 0 {
			continue
		}
		c.file, c.pkg = c.out.Resolve.Files[impl.File], impl.Owner
		c.proofFailure = ""
		if c.matchImplApplication(impl, c.implApplication(impl)) == nil {
			message := "implementation prerequisites are not satisfied"
			if c.proofFailure != "" {
				message += ": " + c.proofFailure
			}
			c.diag(impl.Decl, TraitInvalid, message)
		}
	}
}

func (c *checker) matchImpl(template *Impl, target types.Type) *Impl {
	goal := c.implApplication(template)
	goal.Target = target
	if !types.Identical(template.Target, target) {
		goal.targetPermission = c.newPermissionChecker().fromGo(target)
	}
	return c.matchImplApplication(template, goal)
}

func (c *checker) matchImplApplication(template *Impl, goal *TraitBound) *Impl {
	sub, constSub, sourceSub, ok := c.matchApplicationHead(template, goal)
	if !ok {
		return nil
	}
	if !c.satisfiesTraitGoRequirements(goal) {
		return nil
	}
	for _, p := range template.TypeParams {
		constraint, ok := c.out.Substitute(p.Constraint(), sub, constSub).Underlying().(*types.Interface)
		if ok && !c.satisfiesGo(sub[p], constraint) {
			return nil
		}
	}
	var evidence []Evidence
	for _, requirement := range template.Requirements {
		bound := c.substituteBoundApplication(requirement, sub, constSub, sourceSub)
		witness := c.proveApplication(bound)
		if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
			if c.proofFailure == "" {
				c.proofFailure = "missing trait implementation: " + bound.ApplicationKey()
			}
			return nil
		}
		bindings := evidenceAssociated(witness)
		for _, a := range bound.Trait.Associated {
			want := bound.Associated[a.Name]
			projected := false
			for _, p := range requirement.Projected {
				if requirement.Associated[a.Name] == p {
					sub[p], projected = bindings[a.Name], true
					sourceSub[p] = c.evidenceAssociatedPermission(witness, a.Name)
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
	if len(template.TypeParams) == 0 && len(template.ConstParams) == 0 && len(template.Requirements) == 0 {
		return template
	}
	for _, old := range c.out.ImplInstances {
		if old.Template == template && c.sameApplication(c.implApplication(old), goal) && sameEvidence(old.RequirementEvidence, evidence) {
			return old
		}
	}
	instance := *template
	instance.Template = template
	// Instances are shared by semantic application identity. Retain the
	// declaration's type paths so an alias at the first call site cannot make
	// the defining package import one of its consumers.
	instance.Target = c.out.Substitute(template.Target, sub, constSub)
	instance.Inputs = nil
	for _, input := range template.Inputs {
		instance.Inputs = append(instance.Inputs, c.out.Substitute(input, sub, constSub))
	}
	head := c.boundHeadPermissions(goal)
	instance.targetPermission, instance.inputPermissions = head[0], head[1:]
	instance.permissionSubstitution = sourceSub
	instance.TypeArgs = nil
	for _, p := range template.TypeParams {
		instance.TypeArgs = append(instance.TypeArgs, sub[p])
	}
	instance.RequirementEvidence = evidence
	instance.Substitution = sub
	instance.ConstSubstitution = constSub
	instance.ConstArgs = nil
	for _, parameter := range template.ConstParams {
		instance.ConstArgs = append(instance.ConstArgs, constSub[parameter])
	}
	instance.Associated = map[string]types.Type{}
	instance.associatedPermissions = map[string]*permission{}
	for name, source := range template.associatedPermissions {
		instance.associatedPermissions[name] = c.substituteSourcePermission(source, sub, constSub, sourceSub)
	}
	for name, t := range template.Associated {
		instance.Associated[name] = c.out.Substitute(t, sub, constSub)
	}
	instance.Methods = nil
	for _, method := range template.Methods {
		copy := *method
		copy.Impl = &instance
		r := newBodyTypes(c.out, sub, constSub)
		copy.Signature = types.NewSignatureType(nil, nil, nil, r.tuple(method.Signature.Params()), r.tuple(method.Signature.Results()), method.Signature.Variadic())
		instance.Methods = append(instance.Methods, &copy)
	}
	instance.Requirements = nil
	for _, requirement := range template.Requirements {
		instance.Requirements = append(instance.Requirements, c.substituteBoundApplication(requirement, sub, constSub, sourceSub))
	}
	c.out.ImplInstances = append(c.out.ImplInstances, &instance)
	return &instance
}

func evidenceAssociated(e Evidence) map[string]types.Type {
	if e.outputs != nil {
		return e.outputs
	}
	if e.Bound != nil {
		return e.Bound.Associated
	}
	if e.Impl != nil {
		return e.Impl.Associated
	}
	if len(e.Cases) != 0 {
		result := map[string]types.Type{}
		for name, typ := range evidenceAssociated(e.Cases[0].Evidence) {
			result[name] = typ
		}
		for _, branch := range e.Cases[1:] {
			for name, typ := range result {
				if !types.Identical(typ, evidenceAssociated(branch.Evidence)[name]) {
					delete(result, name)
				}
			}
		}
		return result
	}
	return nil
}

func sameEvidence(left, right []Evidence) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Key() != right[i].Key() {
			return false
		}
	}
	return true
}

func (c *checker) proofDescription(target types.Type, trait *Trait) string {
	var chain []string
	for _, obligation := range c.proofStack {
		chain = append(chain, obligation.goal.ApplicationKey())
	}
	chain = append(chain, types.TypeString(target, nil)+": "+trait.Symbol.Name.Name)
	return strings.Join(chain, " -> ")
}
