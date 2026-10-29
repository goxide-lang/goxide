package check

import (
	"fmt"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (c *checker) traitType(node ast.Type) (types.Type, bool) {
	if projection, ok := node.(*ast.ProjectionType); ok {
		if parts := projectionTypePath(projection); len(parts) >= 3 {
			return c.traitType(&ast.PathType{Base: projection.Base, Path: &ast.Path{Base: projection.Base, Segments: parts}})
		}
	}
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
			goal := c.applicationFromArgs(target, trait, qualifier.Args, n)
			goal.targetPermission = c.projectionTargetPermission(parts, trait)
			witness := c.projectionEvidence(goal, parts[len(parts)-1].Name.Name, n)
			c.checkApplicationBindings(goal, witness, n)
			if output := evidenceAssociated(witness)[parts[len(parts)-1].Name.Name]; output != nil {
				return output, true
			}
			c.diag(n, TraitInvalid, "associated projection requires an explicit implementation of "+trait.Symbol.Name.Name)
			return types.Typ[types.Invalid], true
		}
		last := parts[len(parts)-1]
		if trait := c.traitPath(parts); trait != nil {
			if !trait.Runtime {
				c.diag(n, TraitInvalid, "trait cannot form a runtime view because its methods require concrete Self")
			}
			bindings := map[string]types.Type{}
			c.readAssociated(last.Args, trait, bindings, n)
			inputs, nodes, sources := c.readTraitInputs(last.Args, trait, n)
			args := append([]types.Type(nil), inputs...)
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
			if named, ok := t.(*types.Named); ok {
				if c.out.traitApplications == nil {
					c.out.traitApplications = map[*types.Named]*TraitBound{}
				}
				c.out.traitApplications[named] = &TraitBound{info: c.out, File: c.file.Name, Trait: trait, Inputs: inputs, InputNodes: nodes, inputPermissions: sources, Associated: bindings}
			}
			return t, true
		}
	}
	return nil, false
}

// Self paths retain projection nodes in the parser, including nested generic
// arguments. Expose the same ordered segments used by ordinary qualified paths.
func projectionTypePath(node ast.Type) []*ast.PathSegment {
	switch typ := node.(type) {
	case *ast.PathType:
		if typ.Path != nil {
			return typ.Path.Segments
		}
	case *ast.SelfType:
		return []*ast.PathSegment{{Base: typ.Base, Name: &ast.Ident{Base: typ.Base, Name: "Self"}}}
	case *ast.ProjectionType:
		if typ.Dot {
			return nil
		}
		if prefix := projectionTypePath(typ.X); prefix != nil {
			return append(append([]*ast.PathSegment(nil), prefix...), &ast.PathSegment{Base: typ.Base, Name: typ.Item, Args: typ.Args})
		}
	}
	return nil
}

// Fully qualified Self projections name the declaration's own associated
// bindings, just like Self::Item. They do not select a global implementation.
func (c *checker) projectionEvidence(goal *TraitBound, name string, node ast.Node) Evidence {
	for _, bound := range c.boundsFor(goal.Target) {
		if c.sameApplication(bound, goal) {
			return Evidence{Bound: bound}
		}
	}
	if c.currentImpl != nil && c.sameApplication(c.implApplication(c.currentImpl), goal) {
		c.resolveAssociated(c.currentImpl, name, node)
		return Evidence{Impl: c.currentImpl}
	}
	if trait := c.currentTrait; c.currentImpl == nil && trait != nil && goal.Trait == trait {
		bound := &TraitBound{info: c.out, File: c.file.Name, Trait: trait, Target: trait.Self, Associated: map[string]types.Type{}}
		for _, parameter := range trait.TypeParams {
			bound.Inputs = append(bound.Inputs, parameter)
		}
		if c.sameApplication(bound, goal) {
			for _, associated := range trait.Associated {
				bound.Associated[associated.Name] = associated.Param
			}
			return Evidence{Bound: bound}
		}
	}
	return c.proveApplication(goal)
}

func (c *checker) applicationFromArgs(target types.Type, trait *Trait, args *ast.GenericArgs, node ast.Node) *TraitBound {
	goal := &TraitBound{info: c.out, File: c.file.Name, Trait: trait, Target: target, Associated: map[string]types.Type{}, AssociatedNodes: map[string]ast.Type{}, associatedPermissions: map[string]*permission{}}
	goal.Inputs, goal.InputNodes, goal.inputPermissions = c.readTraitInputs(args, trait, node)
	c.readAssociated(args, trait, goal.Associated, node)
	if args != nil {
		for _, arg := range args.Args {
			if arg.Name != nil {
				goal.AssociatedNodes[arg.Name.Name] = arg.Type
				goal.associatedPermissions[arg.Name.Name] = c.newPermissionChecker().astType(arg.Type, nil)
			}
		}
	}
	return goal
}

func (c *checker) projectionTargetPermission(parts []*ast.PathSegment, trait *Trait) *permission {
	for split := 1; split < len(parts)-1; split++ {
		if c.traitPath(parts[split:len(parts)-1]) == trait {
			if split == 1 && parts[0].Name.Name == "Self" && c.currentSelf != nil {
				if c.currentImpl != nil {
					return c.implHeadPermissions(c.currentImpl)[0]
				}
				return c.newPermissionChecker().fromGo(c.currentSelf)
			}
			path := &ast.PathType{Base: parts[0].Base, Path: &ast.Path{Base: parts[0].Base, Segments: parts[:split]}}
			return c.newPermissionChecker().astType(path, nil)
		}
	}
	return nil
}

func (c *checker) checkApplicationBindings(goal *TraitBound, witness Evidence, node ast.Node) {
	bindings := evidenceAssociated(witness)
	for name, want := range goal.Associated {
		if !types.Identical(want, bindings[name]) {
			c.diag(node, TraitInvalid, "associated projection binding conflicts with implementation: "+name)
			continue
		}
		if source := goal.associatedPermissions[name]; source != nil && !c.sameSourceType(source, c.evidenceAssociatedPermission(witness, name)) {
			c.diag(node, TraitInvalid, "associated projection reference permissions conflict with implementation: "+name)
		}
	}
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

func (c *checker) findImpl(target types.Type, trait *Trait) *Impl {
	return c.prove(target, trait).Impl
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

// A call may supply a leading type-argument prefix and infer the remaining
// arguments from its ordinary parameters. Keep the original signature until
// inference finishes so its trait premises are instantiated only once.
func (c *checker) partialFunctionApplication(call *ast.CallExpr) (*types.Signature, map[*types.TypeParam]types.Type) {
	var signature *types.Signature
	var arguments []types.Type
	switch expression := call.Fun.(type) {
	case *ast.IndexExpr:
		signature, _ = c.expr(expression.X, nil).(*types.Signature)
		if signature == nil || len(c.out.FunctionConstParams[signature]) != 0 || len(expression.Indices) >= c.sourceArity(signature) {
			return nil, nil
		}
		for _, argument := range expression.Indices {
			arguments = append(arguments, c.genericExprType(argument))
		}
	case *ast.PathExpr:
		if expression.Path == nil || len(expression.Path.Segments) == 0 {
			return nil, nil
		}
		last := expression.Path.Segments[len(expression.Path.Segments)-1]
		if last.Args == nil {
			return nil, nil
		}
		signature, _ = c.symbolType(c.sourcePathSymbol(expression.Path.Segments)).(*types.Signature)
		if signature == nil {
			signature, _ = c.importType(expression.Path).(*types.Signature)
		}
		if signature == nil || len(c.out.FunctionConstParams[signature]) != 0 || len(last.Args.Args) >= c.sourceArity(signature) {
			return nil, nil
		}
		for _, argument := range last.Args.Args {
			arguments = append(arguments, c.genericArgType(argument))
		}
	default:
		return nil, nil
	}
	if len(arguments) == 0 {
		return nil, nil
	}
	substitution := map[*types.TypeParam]types.Type{}
	for index, argument := range arguments {
		substitution[signature.TypeParams().At(index)] = argument
	}
	return signature, substitution
}

func (c *checker) instantiateFunctionType(sig *types.Signature, args []types.Type, constants []ConstValue, node ast.Node) types.Type {
	c.ensureFunctionCases(sig)
	constSub, validConsts := c.bindConstArguments(sig, constants, node)
	if !validConsts {
		return types.Typ[types.Invalid]
	}
	if len(args) != c.sourceArity(sig) {
		c.diag(node, GenericConstraint, fmt.Sprintf("got %d type arguments but want %d", len(args), c.sourceArity(sig)))
		return types.Typ[types.Invalid]
	}
	actual := append([]types.Type(nil), args...)
	sub := map[*types.TypeParam]types.Type{}
	for i, arg := range args {
		sub[sig.TypeParams().At(i)] = arg
	}
	sources := c.functionArgumentPermissions(sig, node, sub)
	instance := &FunctionInstance{Signature: sig, TypeArgs: append([]types.Type(nil), args...), ConstArgs: append([]ConstValue(nil), constants...), ConstSubstitution: constSub, permissionSubstitution: sources}
	for _, bound := range c.functionBounds[sig] {
		goal := c.substituteBoundApplication(bound, sub, constSub, sources)
		witness := c.proveApplication(goal)
		instance.Evidence = append(instance.Evidence, witness)
		bindings := evidenceAssociated(witness)
		if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
			message := "type does not explicitly implement " + bound.Trait.Symbol.Name.Name + ": " + goal.ApplicationKey()
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
			want := c.out.Substitute(bound.Associated[a.Name], sub, constSub)
			projected := false
			for _, p := range bound.Projected {
				if bound.Associated[a.Name] == p {
					projected = true
					actual = append(actual, got)
					sub[p] = got
					sources[p] = c.evidenceAssociatedPermission(witness, a.Name)
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
				for param, source := range sources {
					permissions[param] = source
				}
				if source != nil && !p.sameSourcePermissions(source, p.boundAssociated(bound, a.Name, permissions), map[[2]types.Type]bool{}) {
					c.diag(node, GenericConstraint, "associated reference permissions do not satisfy constraint: "+a.Name)
				}
			}
		}
	}
	instance.Substitution = sub
	c.functionBodyEvidence(instance)
	c.out.FunctionInstances[node] = instance
	var t types.Type = sig
	var err error
	if sig.TypeParams().Len() != 0 {
		t, err = c.instantiateGo(sig, actual)
	}
	if err != nil {
		c.diag(node, GenericConstraint, err.Error())
		return types.Typ[types.Invalid]
	}
	if instance.Case != nil {
		function := c.caseFunctions[sig]
		symbol := function.file.Definitions[function.declaration.Name.ID()]
		if selected, ok := instance.Case.Info.Symbols[symbol].(*types.Signature); ok {
			r := newBodyTypes(c.out, instance.Substitution, constSub)
			return types.NewSignatureType(nil, nil, nil, r.tuple(selected.Params()), r.tuple(selected.Results()), selected.Variadic())
		}
	}
	if len(constants) != 0 {
		r := newBodyTypes(c.out, sub, constSub)
		return types.NewSignatureType(nil, nil, nil, r.tuple(sig.Params()), r.tuple(sig.Results()), sig.Variadic())
	}
	return t
}

func (c *checker) functionArgumentPermissions(signature *types.Signature, node ast.Node, sub map[*types.TypeParam]types.Type) map[*types.TypeParam]*permission {
	p := c.referenceHints
	if p == nil {
		p = c.newPermissionChecker()
	}
	result := map[*types.TypeParam]*permission{}
	for parameter, typ := range sub {
		result[parameter] = p.fromGo(typ)
	}
	switch n := node.(type) {
	case *ast.IndexExpr:
		for index, position := range c.typeArgumentPositions(signature, len(n.Indices)) {
			if index < signature.TypeParams().Len() {
				result[signature.TypeParams().At(index)] = p.exprType(n.Indices[position])
			}
		}
	case *ast.GenericArgs:
		index := 0
		for _, arg := range n.Args {
			if arg.Type != nil && index < signature.TypeParams().Len() {
				result[signature.TypeParams().At(index)] = p.astType(arg.Type, nil)
				index++
			}
		}
	case *ast.CallExpr:
		formal := p.sourceShape(n.Fun, map[*resolve.Symbol]bool{})
		inferred := map[*types.TypeParam]*permission{}
		for index, value := range c.callArgumentSources(n.Args, n.Ellipsis, false) {
			if formal != nil {
				p.inferPermission(p.callParam(formal, index, n.Ellipsis), value, inferred)
			}
		}
		for parameter, source := range inferred {
			if sub[parameter] != nil {
				result[parameter] = source
			}
		}
		// Explicit arguments remain authoritative, including source references
		// erased to Go pointers in the ordinary type substitution.
		var explicit ast.Node
		switch function := n.Fun.(type) {
		case *ast.IndexExpr:
			explicit = function
		case *ast.PathExpr:
			if function.Path != nil && len(function.Path.Segments) != 0 {
				if arguments := function.Path.Segments[len(function.Path.Segments)-1].Args; arguments != nil {
					explicit = arguments
				}
			}
		}
		if explicit != nil {
			for parameter, source := range c.functionArgumentPermissions(signature, explicit, nil) {
				result[parameter] = source
			}
		}
	}
	return result
}

// Calls expand a sole multi-result argument before matching formal parameters.
// The same source shapes drive ordinary generic and trait-input inference.
func (c *checker) callArgumentSources(arguments []ast.Expr, ellipsis, evaluate bool) []*permission {
	var values []*permission
	for _, argument := range arguments {
		typ := c.out.Types[argument]
		if evaluate || !valid(typ) {
			typ = c.expr(argument, nil)
		}
		source := c.memberSource(argument, defaultType(typ))
		_, call := referenceScrutinee(argument).(*ast.CallExpr)
		if tuple, ok := typ.(*types.Tuple); ok && tuple.Len() > 1 && call && len(arguments) == 1 && !ellipsis && source != nil {
			values = append(values, source.results...)
		} else {
			values = append(values, source)
		}
	}
	return values
}
