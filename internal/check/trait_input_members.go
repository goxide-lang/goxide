package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (c *checker) exprSource(node ast.Expr, expected types.Type, source *permission) types.Type {
	if c.expectedSources == nil {
		c.expectedSources = map[ast.Expr]*permission{}
	}
	c.expectedSources[node] = source
	return c.expr(node, expected)
}

// Input inference sees a declaration and source type equations. It never asks
// which registered implementation could make an otherwise unknown input fit.
func (c *checker) parameterizedMember(node ast.Expr, arguments []ast.Expr, expected types.Type, call, ellipsis bool) (*MemberSelection, bool) {
	var target types.Type
	var targetSource *permission
	var receiver ast.Expr
	var name string
	var qualifier ast.Type
	static := false
	switch n := node.(type) {
	case *ast.QualifiedSelectorExpr:
		static, receiver, name, qualifier = n.Static, n.X, n.Sel.Name, n.Trait
		target = c.expr(n.X, nil)
	case *ast.SelectorExpr:
		receiver, name = n.X, n.Sel.Name
		target = c.expr(n.X, nil)
		if c.lookupGoMember(target, true, name) != nil {
			return nil, false
		}
	case *ast.PathExpr:
		if n.Path == nil || len(n.Path.Segments) < 2 {
			return nil, false
		}
		parts := n.Path.Segments
		prefix := c.sourcePathSymbol(parts[:len(parts)-1])
		if prefix == nil || prefix.Kind != resolve.Type && prefix.Kind != resolve.Enum && prefix.Kind != resolve.TypeParam {
			if len(parts) != 2 || parts[0].Name.Name != "Self" || c.currentSelf == nil {
				return nil, false
			}
		}
		path := *n.Path
		path.Segments = parts[:len(parts)-1]
		node := &ast.PathType{Base: n.Base, Path: &path}
		target = c.typ(node)
		targetSource = c.newPermissionChecker().astType(node, nil)
		static, name = true, parts[len(parts)-1].Name.Name
	default:
		return nil, false
	}
	if !valid(target) {
		return nil, false
	}
	if !static && c.traitOf(target) != nil {
		return nil, false
	}
	if targetSource == nil {
		targetSource = c.memberSource(receiver, target)
	}
	var chosen *Trait
	var args *ast.GenericArgs
	if qualifier != nil {
		if path, ok := qualifier.(*ast.PathType); ok && path.Path != nil && len(path.Path.Segments) > 0 {
			chosen = c.traitPath(path.Path.Segments)
			args = path.Path.Segments[len(path.Path.Segments)-1].Args
		}
		if chosen == nil {
			c.diag(node, TraitInvalid, "qualified call requires a trait")
			return nil, true
		}
		if !c.traitNeedsApplicationSelection(chosen) && !static {
			return nil, false
		}
	} else {
		for _, impl := range c.implOrder {
			if impl.Trait == nil && memberNamed(impl.Methods, name, static) != nil && c.matchImpl(impl, target) != nil {
				return nil, false
			}
		}
		var traits []*Trait
		parameterized := false
		for _, trait := range c.out.Traits {
			if memberNamed(trait.Methods, name, static) == nil {
				continue
			}
			visible := c.traitVisible(trait)
			if static {
				if _, parameter := types.Unalias(target).(*types.TypeParam); !parameter {
					// Concrete type paths expose public static trait members even
					// without importing the trait, just as staticMember does.
					visible = trait.Type.Obj().Pkg() == c.pkg || trait.Symbol != nil && trait.Symbol.Visibility == resolve.Public
				} else if !visible {
					// A type parameter only sees its definition's traits and
					// bounds, including qualified bounds without a bare import.
					for _, bound := range c.boundsFor(target) {
						visible = visible || bound.Trait == trait
					}
				}
			}
			if !visible {
				continue
			}
			traits = append(traits, trait)
			parameterized = parameterized || c.traitNeedsApplicationSelection(trait)
		}
		if !parameterized {
			return nil, false
		}
		if len(traits) != 1 {
			c.diag(node, TraitInvalid, "ambiguous trait member; select a trait explicitly")
			return nil, true
		}
		chosen = traits[0]
	}
	member := memberNamed(chosen.Methods, name, static)
	if member == nil {
		c.diag(node, TraitInvalid, "unknown selected trait member "+name)
		return nil, true
	}
	targets := []types.Type{target}
	sources := []*permission{targetSource}
	if !static {
		lookup, base := c.receiverTargets(receiver, target)
		if !types.Identical(lookup, target) {
			target, targetSource = lookup, sourceChild(targetSource.elem, lookup)
		}
		targets, sources = []types.Type{target}, []*permission{targetSource}
		if !types.Identical(base, target) {
			targets = append(targets, base)
			sources = append(sources, sourceChild(targetSource.elem, base))
		}
	}
	var selected *MemberSelection
	var failures []Diagnostic
	for index, candidate := range targets {
		start := len(c.out.Diagnostics)
		selection := c.parameterizedMemberTarget(node, receiver, candidate, sources[index], chosen, member, args, qualifier != nil, arguments, expected, call, ellipsis)
		if selection == nil {
			failures = append([]Diagnostic(nil), c.out.Diagnostics[start:]...)
			c.out.Diagnostics = c.out.Diagnostics[:start]
			continue
		}
		if selected != nil {
			c.diag(node, TraitInvalid, "ambiguous trait method; native pointer and value applications both apply")
			return nil, true
		}
		selected = selection
	}
	if selected == nil {
		c.out.Diagnostics = append(c.out.Diagnostics, failures...)
	}
	return selected, true
}

func (c *checker) parameterizedMemberTarget(node ast.Expr, receiver ast.Expr, target types.Type, targetSource *permission, chosen *Trait, member *Member, args *ast.GenericArgs, qualified bool, arguments []ast.Expr, expected types.Type, call, ellipsis bool) *MemberSelection {
	goal := &TraitBound{info: c.out, File: c.file.Name, Trait: chosen, Target: target, targetPermission: targetSource, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}, associatedPermissions: map[string]*permission{}}
	if qualified {
		goal = c.applicationFromArgs(target, chosen, args, node)
		goal.targetPermission = targetSource
	} else {
		var known []*TraitBound
		for _, bound := range c.boundsFor(target) {
			if bound.Trait == chosen && c.sameSourceType(c.boundHeadPermissions(bound)[0], c.boundHeadPermissions(goal)[0]) {
				known = append(known, bound)
			}
		}
		if len(known) == 1 {
			copy := *known[0]
			goal = &copy
		} else {
			inputs, sources, ok := c.inferMemberInputs(member, goal, arguments, expected, c.expectedSources[node], call, ellipsis)
			if !ok {
				c.diag(node, TraitInvalid, "cannot infer complete trait inputs; select a full trait application or provide a target type")
				return nil
			}
			goal.Inputs, goal.inputPermissions = inputs, sources
			if !c.checkTraitInputConstraints(inputs, chosen, node) {
				return nil
			}
		}
	}
	if len(goal.Inputs) != len(chosen.TypeParams) {
		return nil
	}
	witness := c.proveApplication(goal)
	if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
		message := "no explicit implementation for selected trait application " + goal.ApplicationKey()
		if qualified && !member.Static {
			message = "no explicit implementation for qualified method " + member.Name + ": " + goal.ApplicationKey()
		}
		if c.proofFailure != "" {
			message += ": " + c.proofFailure
		}
		c.diag(node, TraitInvalid, message)
		return nil
	}
	c.checkApplicationBindings(goal, witness, node)
	var selection *MemberSelection
	if witness.Impl != nil {
		actual := memberNamed(witness.Impl.Methods, member.Name, member.Static)
		if member.Static {
			selection = &MemberSelection{Member: actual, Impl: witness.Impl, Signature: actual.Signature}
		} else {
			selection = c.selectReceiver(actual, receiver, c.out.Types[receiver])
		}
	} else {
		bound := witness.Bound
		if bound == nil {
			copy := *goal
			copy.Associated = evidenceAssociated(witness)
			bound = &copy
		}
		if member.Static {
			selection = &MemberSelection{Member: member, Bound: bound, Signature: bound.MethodSignature(member)}
		} else {
			selection = c.selectBound(member, bound, receiver, c.out.Types[receiver])
		}
	}
	selection.Evidence = witness
	return selection
}

func (c *checker) traitNeedsApplicationSelection(trait *Trait) bool {
	if len(trait.TypeParams) != 0 {
		return true
	}
	for _, impl := range c.implOrder {
		if impl.Trait != trait {
			continue
		}
		if len(impl.TypeParams) != 0 || len(impl.ConstParams) != 0 || len(impl.Requirements) != 0 || len(c.out.ImplCases[impl]) != 0 {
			return true
		}
	}
	return false
}

func (c *checker) memberSource(node ast.Expr, target types.Type) *permission {
	if node != nil {
		if typeExpr, ok := node.(*ast.TypeExpr); ok {
			return c.newPermissionChecker().astType(typeExpr.Type, nil)
		}
		if c.referenceHints != nil {
			if source := c.referenceHints.sourceShape(node, map[*resolve.Symbol]bool{}); source != nil && valid(source.typ) {
				copy := *source
				copy.typ = defaultType(copy.typ)
				return &copy
			}
		}
	}
	return c.newPermissionChecker().fromGo(target)
}

func (c *checker) inferMemberInputs(member *Member, goal *TraitBound, arguments []ast.Expr, expected types.Type, expectedSource *permission, call, ellipsis bool) ([]types.Type, []*permission, bool) {
	trait := goal.Trait
	if len(trait.TypeParams) == 0 {
		return nil, nil, true
	}
	p := c.newPermissionChecker()
	file, pkg, impl, currentTrait, self := c.file, c.pkg, c.currentImpl, c.currentTrait, c.currentSelf
	c.file, c.pkg, c.currentImpl, c.currentTrait, c.currentSelf = c.out.Resolve.Files[trait.File], trait.Type.Obj().Pkg(), nil, trait, trait.Self
	_, _, params := receiverParams(member.Decl)
	formal := p.functionType(params, member.Decl.Results)
	formal.typ = member.Signature
	c.file, c.pkg, c.currentImpl, c.currentTrait, c.currentSelf = file, pkg, impl, currentTrait, self
	formal = p.substitutePermission(formal, map[*types.TypeParam]*permission{trait.Self: c.boundHeadPermissions(goal)[0]})
	if expectedSource == nil {
		expectedSource = p.fromGo(expected)
	}
	var left, right []*permission
	add := func(want, got *permission) {
		if want == nil || got == nil || !valid(want.typ) || !valid(got.typ) || got.typ == types.Typ[types.UntypedNil] {
			return
		}
		uses := false
		for _, parameter := range trait.TypeParams {
			uses = uses || c.out.containsType(want.typ, parameter)
		}
		if uses {
			left = append(left, want)
			right = append(right, got)
		}
	}
	if call {
		for index, source := range c.callArgumentSources(arguments, ellipsis, true) {
			add(p.callParam(formal, index, ellipsis), source)
		}
		if valid(expected) && len(formal.results) == 1 {
			add(formal.results[0], expectedSource)
		}
	} else if signature, ok := expected.(*types.Signature); ok {
		for index, want := range formal.params {
			if index < signature.Params().Len() {
				got := p.fromGo(signature.Params().At(index).Type())
				if expectedSource != nil && index < len(expectedSource.params) {
					got = expectedSource.params[index]
				}
				add(want, got)
			}
		}
		for index, want := range formal.results {
			if index < signature.Results().Len() {
				got := p.fromGo(signature.Results().At(index).Type())
				if expectedSource != nil && index < len(expectedSource.results) {
					got = expectedSource.results[index]
				}
				add(want, got)
			}
		}
	}
	makeSignature := func(values []*permission) *permission {
		var parameters []*types.Var
		for _, value := range values {
			parameters = append(parameters, types.NewVar(0, c.pkg, "", value.typ))
		}
		return &permission{typ: types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), nil, false), params: values}
	}
	x, y := makeSignature(left), makeSignature(right)
	sub, _, sourceSub, ok := c.matchApplicationHead(&Impl{info: c.out, Target: x.typ, targetPermission: x, TypeParams: trait.TypeParams}, &TraitBound{info: c.out, Target: y.typ, targetPermission: y})
	if !ok {
		return nil, nil, false
	}
	var inputs []types.Type
	var sources []*permission
	for _, parameter := range trait.TypeParams {
		inputs = append(inputs, sub[parameter])
		sources = append(sources, sourceSub[parameter])
	}
	return inputs, sources, true
}
