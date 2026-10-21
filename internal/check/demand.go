package check

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// DemandPlan separates implementations selected for emission from the proof
// cache. Declarations and trees retain their source identities throughout.
type DemandPlan struct {
	Info              *Info
	Roots             []*BodyInstance
	Impls             []*Impl
	GenericImpls      []*Impl
	Functions         []*FunctionInstance
	Bodies            map[*ast.FuncDecl][]*BodyInstance
	FunctionTemplates map[*ast.FuncDecl]bool
	ImplTemplates     map[*Impl]bool
}

// BodyInstance is a read-only semantic view of one emitted body. Info contains
// substituted types and selected members/callees for this instance; its AST and
// resolver tables are shared with the original Info and must not be rewritten.
// A package initializer has Node set and Decl, Impl, Function left nil.
type BodyInstance struct {
	Node         ast.Node
	Decl         *ast.FuncDecl
	File         string
	Impl         *Impl
	Function     *FunctionInstance
	Substitution map[*types.TypeParam]types.Type
	Info         *Info
}

func (c *checker) collectDemands() {
	plan := &DemandPlan{
		Bodies:            map[*ast.FuncDecl][]*BodyInstance{},
		FunctionTemplates: map[*ast.FuncDecl]bool{},
		ImplTemplates:     map[*Impl]bool{},
	}
	c.out.Demands = plan
	collector := &demandCollector{c: c, plan: plan, functions: map[*types.Signature]*demandFunction{}, declarations: map[*ast.FuncDecl]*demandFunction{}, generic: map[*Impl]bool{}}
	file, pkg, bounds, active := c.file, c.pkg, c.bounds, c.activeBounds
	impl, trait, self := c.currentImpl, c.currentTrait, c.currentSelf
	defer func() {
		c.file, c.pkg, c.bounds, c.activeBounds = file, pkg, bounds, active
		c.currentImpl, c.currentTrait, c.currentSelf = impl, trait, self
	}()
	// Template assumptions are not proofs for a demanded concrete operation.
	c.bounds, c.activeBounds = map[*types.TypeParam][]*TraitBound{}, nil
	c.currentImpl, c.currentTrait, c.currentSelf = nil, nil, nil
	collector.index()
	collector.classify()
	plan.Info = bodyInfo(c.out, nil)
	collector.roots()
}

const (
	maxDemandDepth = 128
	maxDemandCount = 4096
)

type demandFunction struct {
	decl      *ast.FuncDecl
	file      string
	signature *types.Signature
	impl      *Impl
}

type demandCollector struct {
	c            *checker
	plan         *DemandPlan
	functions    map[*types.Signature]*demandFunction
	declarations map[*ast.FuncDecl]*demandFunction
	ordered      []*demandFunction
	initializers []*BodyInstance
	generic      map[*Impl]bool
	stack        []*BodyInstance
	count        int
	failed       bool
}

func (d *demandCollector) enter(file string) {
	d.c.file = d.c.out.Resolve.Files[file]
	if d.c.file == nil {
		return
	}
	for source, pkg := range d.c.packages {
		for _, candidate := range source.Files {
			if candidate == d.c.file {
				d.c.pkg = pkg
				return
			}
		}
	}
}

func (d *demandCollector) index() {
	var names []string
	for name := range d.c.out.Resolve.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := d.c.out.Resolve.Files[name]
		var items func([]ast.Item, *Impl)
		items = func(list []ast.Item, owner *Impl) {
			for _, item := range list {
				switch node := item.(type) {
				case *ast.FuncDecl:
					symbol := file.Definitions[node.Name.ID()]
					signature, _ := d.c.out.Symbols[symbol].(*types.Signature)
					if owner != nil {
						for _, member := range owner.Methods {
							if member.Decl == node {
								signature = member.Signature
							}
						}
					}
					function := &demandFunction{node, name, signature, owner}
					d.ordered = append(d.ordered, function)
					d.declarations[node] = function
					if signature != nil {
						d.functions[signature] = function
					}
				case *ast.ImplDecl:
					items(node.Items, d.c.out.Impls[node])
				case *ast.ModDecl:
					items(node.Items, owner)
				case *ast.DeclareDecl:
					items([]ast.Item{node.Decl}, owner)
				case *ast.VarDecl, *ast.ConstDecl:
					if owner == nil {
						d.initializers = append(d.initializers, &BodyInstance{Node: node, File: name})
					}
				}
			}
		}
		items(file.AST.Items, nil)
	}
	for _, instance := range d.c.out.FunctionInstances {
		if function := d.functions[instance.Signature]; function != nil {
			instance.Decl, instance.File = function.decl, function.file
		}
	}
}

func (d *demandCollector) classify() {
	for _, impl := range d.c.implOrder {
		if len(impl.TypeParams) == 0 {
			continue
		}
		d.enter(impl.File)
		if d.c.matchImpl(impl, impl.Target) == nil {
			d.plan.ImplTemplates[impl] = true
		}
	}
	for _, function := range d.ordered {
		if function.impl != nil || function.signature == nil || function.signature.TypeParams().Len() == 0 {
			continue
		}
		d.enter(function.file)
		if d.functionInstance(function.signature, identityArguments(d.c, function.signature), false, function.decl) == nil {
			d.plan.FunctionTemplates[function.decl] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for _, function := range d.ordered {
			if function.impl != nil && (len(function.impl.TypeParams) == 0 || d.plan.ImplTemplates[function.impl]) || function.impl == nil && (function.signature == nil || function.signature.TypeParams().Len() == 0 || d.plan.FunctionTemplates[function.decl]) {
				continue
			}
			d.enter(function.file)
			pending := false
			ast.Walk(function.decl.Body, func(node ast.Node) bool {
				if pending || node == nil {
					return false
				}
				if expression, ok := node.(ast.Expr); ok {
					if member := d.c.out.Members[expression]; member != nil {
						pending = d.pendingSelection(member)
					}
					if view := d.c.out.TraitViews[expression]; view != nil {
						pending = pending || d.pendingImpl(view.Impl)
					}
				}
				if instance := d.c.out.FunctionInstances[node]; instance != nil {
					if callee := d.functions[instance.Signature]; callee != nil && d.plan.FunctionTemplates[callee.decl] && openArguments(instance.TypeArgs) {
						pending = true
					}
				}
				return !pending
			})
			if pending {
				if function.impl != nil {
					d.plan.ImplTemplates[function.impl] = true
				} else {
					d.plan.FunctionTemplates[function.decl] = true
				}
				changed = true
			}
		}
	}
}

func identityArguments(c *checker, signature *types.Signature) []types.Type {
	args := make([]types.Type, c.sourceArity(signature))
	for i := range args {
		args[i] = signature.TypeParams().At(i)
	}
	return args
}

func openArguments(args []types.Type) bool {
	for _, arg := range args {
		if containsTypeParameter(arg) {
			return true
		}
	}
	return false
}

func implDeclaration(impl *Impl) *Impl {
	for impl != nil && impl.AliasOf != nil {
		impl = impl.AliasOf
	}
	if impl != nil && impl.Template != nil {
		return implDeclaration(impl.Template)
	}
	return impl
}

func (d *demandCollector) pendingImpl(impl *Impl) bool {
	if impl == nil || !containsTypeParameter(impl.Target) {
		return false
	}
	template := implDeclaration(impl)
	return d.plan.ImplTemplates[template] || d.c.matchImpl(template, impl.Target) == nil
}

func (d *demandCollector) pendingSelection(selection *MemberSelection) bool {
	if selection.Bound != nil {
		witness := d.c.prove(selection.Bound.Target, selection.Bound.Trait)
		return witness.Impl == nil || d.pendingImpl(witness.Impl)
	}
	return d.pendingImpl(selection.Impl)
}

func (d *demandCollector) roots() {
	sugar := map[*Impl]bool{}
	for _, member := range d.c.out.Sugar {
		sugar[implDeclaration(member.Impl)] = true
	}
	for _, impl := range d.c.implOrder {
		if impl.AliasOf != nil {
			continue
		}
		d.enter(impl.File)
		if len(impl.TypeParams) == 0 {
			if resolved := d.c.matchImpl(impl, impl.Target); resolved != nil {
				d.impl(resolved, impl.Decl)
			}
		} else if (impl.Trait == nil || sugar[impl]) && !d.plan.ImplTemplates[impl] {
			d.genericImpl(impl)
		}
	}
	for _, function := range d.ordered {
		if function.impl != nil || d.plan.FunctionTemplates[function.decl] || function.decl.Body == nil {
			continue
		}
		d.enter(function.file)
		var sub map[*types.TypeParam]types.Type
		if function.signature != nil && function.signature.TypeParams().Len() != 0 {
			if instance := d.functionInstance(function.signature, identityArguments(d.c, function.signature), true, function.decl); instance != nil {
				sub = instance.Substitution
			}
		}
		body := &BodyInstance{Node: function.decl, Decl: function.decl, File: function.file, Substitution: sub}
		d.plan.Roots = append(d.plan.Roots, body)
		d.addBody(body)
	}
	for _, body := range d.initializers {
		d.plan.Roots = append(d.plan.Roots, body)
		d.addBody(body)
	}
}

func (d *demandCollector) addBody(body *BodyInstance) {
	if d.failed {
		return
	}
	if len(d.stack) >= maxDemandDepth || d.count >= maxDemandCount {
		d.limit(body)
		return
	}
	d.count++
	if body.Decl != nil {
		d.plan.Bodies[body.Decl] = append(d.plan.Bodies[body.Decl], body)
	}
	body.Info = bodyInfo(d.c.out, body.Substitution)
	d.stack = append(d.stack, body)
	defer func() { d.stack = d.stack[:len(d.stack)-1] }()
	d.enter(body.File)
	r := newBodyTypes(body.Substitution)
	ast.Walk(body.Node, func(node ast.Node) bool {
		if d.failed || node == nil {
			return false
		}
		// Recursive demand processing changes the checker's lexical file.
		d.enter(body.File)
		if expression, ok := node.(ast.Expr); ok {
			if selection := d.c.out.Members[expression]; selection != nil {
				copy := *selection
				copy.Signature, _ = r.typ(selection.Signature).(*types.Signature)
				var selected *Impl
				if selection.Bound != nil {
					target := r.typ(selection.Bound.Target)
					selected = d.c.prove(target, selection.Bound.Trait).Impl
				} else if selection.Impl != nil {
					selected = d.c.matchImpl(implDeclaration(selection.Impl), r.typ(selection.Impl.Target))
				}
				if selected != nil {
					copy.Impl, copy.Bound = selected, nil
					for _, member := range selected.Methods {
						if member.Decl == selection.Member.Decl || member.Name == selection.Member.Name && member.Static == selection.Member.Static {
							copy.Member = member
							break
						}
					}
					d.impl(selected, node)
				} else if selection.Impl != nil || selection.Bound != nil {
					d.unresolved(node, "member implementation")
				}
				body.Info.Members[expression] = &copy
				if body.Function == nil && body.Impl == nil {
					d.plan.Info.Members[expression] = &copy
				}
			}
			if view := d.c.out.TraitViews[expression]; view != nil {
				copy := *view
				copy.Type, copy.SourceType = r.typ(view.Type), r.typ(view.SourceType)
				if view.Impl != nil {
					copy.Impl = d.c.matchImpl(implDeclaration(view.Impl), r.typ(view.Impl.Target))
					if copy.Impl != nil {
						d.impl(copy.Impl, node)
					} else {
						d.unresolved(node, "trait view implementation")
					}
				}
				body.Info.TraitViews[expression] = &copy
				if body.Function == nil && body.Impl == nil {
					d.plan.Info.TraitViews[expression] = &copy
				}
			}
		}
		if instance := d.c.out.FunctionInstances[node]; instance != nil {
			d.enter(body.File)
			args := make([]types.Type, len(instance.TypeArgs))
			for i, argument := range instance.TypeArgs {
				args[i] = r.typ(argument)
			}
			copy := d.functionInstance(instance.Signature, args, true, node)
			if copy != nil {
				if function := d.functions[instance.Signature]; function != nil && d.plan.FunctionTemplates[function.decl] {
					copy = d.function(copy, node)
				}
				body.Info.FunctionInstances[node] = copy
				if body.Function == nil && body.Impl == nil {
					d.plan.Info.FunctionInstances[node] = copy
				}
			}
		}
		return !d.failed
	})
	if body.Function == nil && body.Impl == nil {
		d.mergeRoot(body)
	}
}

func (d *demandCollector) mergeRoot(body *BodyInstance) {
	file := d.c.out.Resolve.Files[body.File]
	ast.Walk(body.Node, func(node ast.Node) bool {
		if node == nil {
			return false
		}
		if expression, ok := node.(ast.Expr); ok {
			if typ := body.Info.Types[expression]; typ != nil {
				d.plan.Info.Types[expression] = typ
			}
			if value := body.Info.Constructors[expression]; value != nil {
				d.plan.Info.Constructors[expression] = value
			}
		}
		if typ, ok := node.(ast.Type); ok {
			if value := body.Info.TypeOverrides[typ]; value != nil {
				d.plan.Info.TypeOverrides[typ] = value
			}
		}
		if id, ok := node.(*ast.Ident); ok && file != nil {
			if symbol := file.Definitions[id.ID()]; symbol != nil {
				d.plan.Info.Symbols[symbol] = body.Info.Symbols[symbol]
			}
		}
		switch node := node.(type) {
		case *ast.MatchExpr:
			if value := body.Info.Matches[node]; value != nil {
				d.plan.Info.Matches[node] = value
			}
		case *ast.TryExpr:
			if value := body.Info.Tries[node]; value != nil {
				d.plan.Info.Tries[node] = value
			}
		case *ast.CallExpr:
			if value := body.Info.GoResults[node]; value != nil {
				d.plan.Info.GoResults[node] = value
			}
			if value := body.Info.Equalities[node]; value != nil {
				d.plan.Info.Equalities[node] = value
			}
		}
		return true
	})
}

func (d *demandCollector) impl(impl *Impl, node ast.Node) {
	template := implDeclaration(impl)
	if len(template.TypeParams) != 0 && !d.plan.ImplTemplates[template] {
		d.genericImpl(template)
		return
	}
	if containsTypeParameter(impl.Target) || openArguments(impl.TypeArgs) {
		d.unresolved(node, "conditional implementation")
		return
	}
	for _, old := range d.plan.Impls {
		if implDeclaration(old) == template && types.Identical(old.Target, impl.Target) && sameEvidence(old.RequirementEvidence, impl.RequirementEvidence) {
			return
		}
	}
	d.plan.Impls = append(d.plan.Impls, impl)
	for _, evidence := range impl.RequirementEvidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, member := range impl.Methods {
		if member.Decl == nil || member.Decl.Body == nil || impl.Declare {
			continue
		}
		body := &BodyInstance{Node: member.Decl, Decl: member.Decl, File: impl.File, Impl: impl, Substitution: impl.Substitution}
		d.addBody(body)
	}
}

func (d *demandCollector) genericImpl(impl *Impl) {
	if d.generic[impl] {
		return
	}
	d.generic[impl] = true
	d.plan.GenericImpls = append(d.plan.GenericImpls, impl)
	for _, member := range impl.Methods {
		if member.Decl != nil && member.Decl.Body != nil && !impl.Declare {
			d.addBody(&BodyInstance{Node: member.Decl, Decl: member.Decl, File: impl.File, Impl: impl})
		}
	}
}

func (d *demandCollector) functionInstance(signature *types.Signature, args []types.Type, diagnose bool, node ast.Node) *FunctionInstance {
	instance := &FunctionInstance{Signature: signature, TypeArgs: append([]types.Type(nil), args...), Substitution: map[*types.TypeParam]types.Type{}}
	if declaration := d.functions[signature]; declaration != nil {
		instance.Decl, instance.File = declaration.decl, declaration.file
	}
	for i, arg := range args {
		if i < signature.TypeParams().Len() {
			instance.Substitution[signature.TypeParams().At(i)] = arg
		}
	}
	for _, bound := range d.c.functionBounds[signature] {
		r := newBodyTypes(instance.Substitution)
		witness := d.c.prove(r.typ(bound.Target), bound.Trait)
		if witness.Impl == nil {
			if diagnose {
				d.unresolved(node, "function prerequisite "+bound.Trait.Symbol.Name.Name)
			}
			return nil
		}
		instance.Evidence = append(instance.Evidence, witness)
		for _, associated := range bound.Trait.Associated {
			actual := witness.Impl.Associated[associated.Name]
			if projected := projectedBinding(bound, associated.Name); projected != nil {
				instance.Substitution[projected] = actual
			} else if !types.Identical(r.typ(bound.Associated[associated.Name]), actual) {
				if diagnose {
					d.unresolved(node, "associated binding "+associated.Name)
				}
				return nil
			}
		}
	}
	return instance
}

func (d *demandCollector) function(instance *FunctionInstance, node ast.Node) *FunctionInstance {
	if openArguments(instance.TypeArgs) {
		d.unresolved(node, "conditional function")
		return instance
	}
	for _, old := range d.plan.Functions {
		if old.Decl == instance.Decl && identicalArguments(old.TypeArgs, instance.TypeArgs) && sameEvidence(old.Evidence, instance.Evidence) {
			return old
		}
	}
	d.plan.Functions = append(d.plan.Functions, instance)
	for _, evidence := range instance.Evidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	body := &BodyInstance{Node: instance.Decl, Decl: instance.Decl, File: instance.File, Function: instance, Substitution: instance.Substitution}
	instance.Body = body
	d.addBody(body)
	return instance
}

func identicalArguments(left, right []types.Type) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !types.Identical(left[i], right[i]) {
			return false
		}
	}
	return true
}

func (d *demandCollector) unresolved(node ast.Node, what string) {
	if d.failed {
		return
	}
	d.failed = true
	message := "cannot resolve demanded " + what
	if d.c.proofFailure != "" {
		message += ": " + d.c.proofFailure
	}
	d.c.diag(node, GenericConstraint, message)
}

func (d *demandCollector) limit(body *BodyInstance) {
	if d.failed {
		return
	}
	d.failed = true
	d.enter(body.File)
	var chain []string
	for _, entry := range append(append([]*BodyInstance(nil), d.stack...), body) {
		name := "initializer"
		if entry.Function != nil {
			var args []string
			for _, typ := range entry.Function.TypeArgs {
				args = append(args, types.TypeString(typ, nil))
			}
			name = entry.Decl.Name.Name + "[" + strings.Join(args, ", ") + "]"
		} else if entry.Impl != nil {
			name = types.TypeString(entry.Impl.Target, nil) + "." + entry.Decl.Name.Name
		} else if entry.Decl != nil {
			name = entry.Decl.Name.Name
		}
		chain = append(chain, name)
	}
	d.c.diag(body.Node, GenericConstraint, fmt.Sprintf("instance expansion limit exceeded (depth %d, instances %d): %s", maxDemandDepth, maxDemandCount, strings.Join(chain, " -> ")))
}
