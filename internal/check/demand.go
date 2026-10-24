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
	Case                   *BodyCase
	CaseEvidence           []Evidence
	ConstSubstitution      map[*ConstParam]ConstValue
	Node                   ast.Node
	Decl                   *ast.FuncDecl
	File                   string
	Impl                   *Impl
	Function               *FunctionInstance
	Substitution           map[*types.TypeParam]types.Type
	Info                   *Info
	permissionSubstitution map[*types.TypeParam]*permission
	caseOnly               bool
}

func (c *checker) collectDemands() {
	plan := &DemandPlan{
		Bodies:            map[*ast.FuncDecl][]*BodyInstance{},
		FunctionTemplates: map[*ast.FuncDecl]bool{},
		ImplTemplates:     map[*Impl]bool{},
	}
	c.out.Demands = plan
	collector := &demandCollector{c: c, plan: plan, functions: map[*types.Signature]*demandFunction{}, declarations: map[*ast.FuncDecl]*demandFunction{}, generic: map[*Impl]bool{}}
	file, pkg, bounds, active, goConstraints := c.file, c.pkg, c.bounds, c.activeBounds, c.activeGoConstraints
	impl, trait, self := c.currentImpl, c.currentTrait, c.currentSelf
	defer func() {
		c.file, c.pkg, c.bounds, c.activeBounds = file, pkg, bounds, active
		c.activeGoConstraints = goConstraints
		c.currentImpl, c.currentTrait, c.currentSelf = impl, trait, self
	}()
	// Template assumptions are not proofs for a demanded concrete operation.
	c.bounds, c.activeBounds, c.activeGoConstraints = map[*types.TypeParam][]*TraitBound{}, nil, nil
	c.currentImpl, c.currentTrait, c.currentSelf = nil, nil, nil
	collector.index()
	collector.classify()
	plan.Info = bodyInfo(c.out, nil, nil)
	collector.caseRoots()
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
	for declaration, branches := range d.c.out.BodyCases {
		function := d.declarations[declaration]
		if function == nil || function.impl != nil {
			continue
		}
		file := d.c.out.Resolve.Files[function.file]
		symbol := file.Definitions[declaration.Name.ID()]
		for _, branch := range branches {
			if signature, ok := branch.Info.Symbols[symbol].(*types.Signature); ok {
				d.functions[signature] = function
			}
		}
	}
}

func (d *demandCollector) caseRoots() {
	check := func(branch *BodyCase) {
		body := &BodyInstance{Node: branch.Decl, Decl: branch.Decl, File: branch.File, Case: branch, Substitution: branch.Substitution, ConstSubstitution: branch.ConstSubstitution, permissionSubstitution: branch.permissionSubstitution, caseOnly: true}
		d.addBody(body)
		branch.Info = body.Info
	}
	for _, function := range d.ordered {
		for _, branch := range d.c.out.BodyCases[function.decl] {
			check(branch)
		}
	}
	for _, impl := range d.c.implOrder {
		for _, branch := range d.c.out.ImplCases[impl] {
			for _, member := range impl.Methods {
				if method := branch.Methods[member.Decl]; method != nil {
					check(method)
				}
			}
		}
	}
}

func (d *demandCollector) classify() {
	for _, impl := range d.c.implOrder {
		for _, member := range impl.Methods {
			if len(d.c.out.BodyCases[member.Decl]) != 0 {
				d.plan.ImplTemplates[impl] = true
			}
		}
		if len(impl.ConstParams) != 0 {
			d.plan.ImplTemplates[impl] = true
			continue
		}
		if len(impl.TypeParams) == 0 {
			continue
		}
		d.enter(impl.File)
		if d.c.matchImplApplication(impl, d.c.implApplication(impl)) == nil {
			d.plan.ImplTemplates[impl] = true
		}
	}
	for _, function := range d.ordered {
		if function.impl == nil && len(d.c.out.BodyCases[function.decl]) != 0 {
			d.plan.FunctionTemplates[function.decl] = true
			continue
		}
		if function.impl == nil && len(d.c.out.FunctionConstParams[function.signature]) != 0 {
			d.plan.FunctionTemplates[function.decl] = true
			continue
		}
		if function.impl != nil || function.signature == nil || function.signature.TypeParams().Len() == 0 {
			continue
		}
		d.enter(function.file)
		if d.functionInstance(function.signature, identityArguments(d.c, function.signature), nil, false, function.decl) == nil {
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
					if callee := d.functions[instance.Signature]; callee != nil && d.plan.FunctionTemplates[callee.decl] && (openArguments(instance.TypeArgs) || openConstants(instance.ConstArgs)) {
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
	if impl != nil && impl.Template != nil {
		return implDeclaration(impl.Template)
	}
	return impl
}

func (d *demandCollector) pendingImpl(impl *Impl) bool {
	if impl == nil {
		return false
	}
	if openConstants(impl.ConstArgs) {
		return true
	}
	if !containsTypeParameter(impl.Target) && !openArguments(impl.Inputs) {
		return false
	}
	template := implDeclaration(impl)
	return d.plan.ImplTemplates[template] || d.c.matchImplApplication(template, d.c.implApplication(impl)) == nil
}

func (d *demandCollector) pendingSelection(selection *MemberSelection) bool {
	if len(selection.Evidence.Cases) != 0 {
		return true
	}
	if selection.Bound != nil {
		witness := d.c.proveApplication(selection.Bound)
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
		d.enter(impl.File)
		if len(impl.ConstParams) != 0 {
			continue
		}
		if len(impl.TypeParams) == 0 {
			if resolved := d.c.matchImplApplication(impl, d.c.implApplication(impl)); resolved != nil {
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
			if instance := d.functionInstance(function.signature, identityArguments(d.c, function.signature), nil, true, function.decl); instance != nil {
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
	source := d.c.out
	if body.Case != nil {
		source = body.Case.Info
	}
	body.Info = bodyInfoPermissions(source, body.Substitution, body.ConstSubstitution, body.permissionSubstitution)
	body.Info.Demands = d.plan
	d.stack = append(d.stack, body)
	defer func() { d.stack = d.stack[:len(d.stack)-1] }()
	oldBounds, oldGoConstraints := d.c.activeBounds, d.c.activeGoConstraints
	defer func() { d.c.activeBounds, d.c.activeGoConstraints = oldBounds, oldGoConstraints }()
	d.enterBody(body)
	r := newBodyTypes(source, body.Substitution, body.ConstSubstitution)
	ast.Walk(body.Node, func(node ast.Node) bool {
		if d.failed || node == nil {
			return false
		}
		// Recursive demand processing changes the checker's lexical file and premises.
		d.enterBody(body)
		if expression, ok := node.(ast.Expr); ok {
			if selection := source.Members[expression]; selection != nil {
				copy, resolved := d.instantiateMember(source, selection, body.Substitution, body.ConstSubstitution, body.permissionSubstitution)
				if copy.Impl != nil {
					if !body.caseOnly {
						d.impl(copy.Impl, node)
					}
				} else if !resolved {
					d.unresolved(node, "member implementation")
				}
				body.Info.Members[expression] = copy
				if body.Function == nil && body.Impl == nil && !body.caseOnly {
					d.plan.Info.Members[expression] = copy
				}
			}
			if attempt, ok := expression.(*ast.TryExpr); ok {
				if trial := source.Tries[attempt]; trial != nil && trial.Conversion != nil {
					selected, resolved := d.instantiateMember(source, trial.Conversion, body.Substitution, body.ConstSubstitution, body.permissionSubstitution)
					body.Info.Tries[attempt].Conversion = selected
					if !resolved {
						d.unresolved(node, "propagation conversion")
					} else if selected.Impl != nil && !body.caseOnly {
						d.impl(selected.Impl, node)
					}
					if body.Function == nil && body.Impl == nil && !body.caseOnly {
						d.plan.Info.Tries[attempt] = body.Info.Tries[attempt]
					}
				}
			}
			if view := source.TraitViews[expression]; view != nil {
				copy := *view
				copy.Type, copy.SourceType = r.typ(view.Type), r.typ(view.SourceType)
				if view.Impl != nil {
					witness, _ := d.instantiateEvidence(Evidence{Impl: view.Impl}, body.Substitution, body.ConstSubstitution, body.permissionSubstitution)
					copy.Impl = witness.Impl
					if copy.Impl != nil && !body.caseOnly {
						d.impl(copy.Impl, node)
					} else if copy.Impl == nil {
						d.unresolved(node, "trait view implementation")
					}
				}
				body.Info.TraitViews[expression] = &copy
				if body.Function == nil && body.Impl == nil && !body.caseOnly {
					d.plan.Info.TraitViews[expression] = &copy
				}
			}
		}
		if instance := source.FunctionInstances[node]; instance != nil {
			d.enter(body.File)
			args := make([]types.Type, len(instance.TypeArgs))
			for i, argument := range instance.TypeArgs {
				args[i] = r.typ(argument)
			}
			constants := make([]ConstValue, len(instance.ConstArgs))
			for i, value := range instance.ConstArgs {
				constants[i] = resolveConst(value, body.ConstSubstitution)
			}
			permissions := d.composePermissions(instance.permissionSubstitution, body.Substitution, body.ConstSubstitution, body.permissionSubstitution)
			signature := instance.Signature
			if function := d.functions[signature]; function != nil {
				signature = function.signature
			}
			copy := d.functionInstanceWithPermissions(signature, args, constants, permissions, true, node)
			if copy != nil {
				if function := d.functions[signature]; function != nil && d.plan.FunctionTemplates[function.decl] && !body.caseOnly {
					copy = d.function(copy, node)
				}
				body.Info.FunctionInstances[node] = copy
				if body.Function == nil && body.Impl == nil && !body.caseOnly {
					d.plan.Info.FunctionInstances[node] = copy
				}
			}
		}
		return !d.failed
	})
	if body.Function == nil && body.Impl == nil && !body.caseOnly {
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
			if value, ok := body.Info.ConstValues[expression]; ok {
				if d.plan.Info.ConstValues == nil {
					d.plan.Info.ConstValues = map[ast.Expr]ConstValue{}
				}
				d.plan.Info.ConstValues[expression] = value
			}
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
			if value := body.Info.EnumAssertions[node]; value != nil {
				if d.plan.Info.EnumAssertions == nil {
					d.plan.Info.EnumAssertions = map[*ast.CallExpr]types.Type{}
				}
				d.plan.Info.EnumAssertions[node] = value
			}
			if value := body.Info.EnumValid[node]; value != nil {
				d.plan.Info.EnumValid[node] = value
			}
			if value := body.Info.GoResults[node]; value != nil {
				d.plan.Info.GoResults[node] = value
			}
		}
		return true
	})
}

func (d *demandCollector) impl(impl *Impl, node ast.Node) {
	template := implDeclaration(impl)
	if len(template.ConstParams) != 0 {
		d.constImpl(impl, node)
		return
	}
	if len(template.TypeParams) != 0 && !d.plan.ImplTemplates[template] {
		d.genericImpl(template)
		return
	}
	if (containsTypeParameter(impl.Target) || openArguments(impl.Inputs) || openArguments(impl.TypeArgs)) && len(impl.MethodCases) == 0 {
		d.unresolved(node, "conditional implementation")
		return
	}
	for _, old := range d.plan.Impls {
		if implDeclaration(old) == template && old.ApplicationKey() == impl.ApplicationKey() && sameEvidence(old.RequirementEvidence, impl.RequirementEvidence) && sameMethodCases(old.MethodCases, impl.MethodCases) && sameMethodCaseEvidence(old.MethodCaseEvidence, impl.MethodCaseEvidence) {
			return
		}
	}
	d.plan.Impls = append(d.plan.Impls, impl)
	for _, evidence := range impl.RequirementEvidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, evidence := range impl.CaseEvidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, proofs := range impl.MethodCaseEvidence {
		for _, evidence := range proofs {
			if evidence.Impl != nil {
				d.impl(evidence.Impl, node)
			}
		}
	}
	for _, member := range impl.Methods {
		if member.Decl == nil || member.Decl.Body == nil {
			continue
		}
		body := &BodyInstance{Node: member.Decl, Decl: member.Decl, File: impl.File, Impl: impl, Case: impl.MethodCases[member.Decl], CaseEvidence: impl.MethodCaseEvidence[member.Decl], Substitution: impl.Substitution, ConstSubstitution: impl.ConstSubstitution, permissionSubstitution: impl.permissionSubstitution}
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
		if member.Decl != nil && member.Decl.Body != nil {
			sub := impl.Substitution
			branch := impl.MethodCases[member.Decl]
			if branch != nil {
				sub = copySubstitution(sub)
				for parameter, typ := range branch.Substitution {
					sub[parameter] = typ
				}
			}
			d.addBody(&BodyInstance{Node: member.Decl, Decl: member.Decl, File: impl.File, Impl: impl, Case: branch, CaseEvidence: impl.MethodCaseEvidence[member.Decl], Substitution: sub, ConstSubstitution: impl.ConstSubstitution, permissionSubstitution: impl.permissionSubstitution})
		}
	}
}

func (d *demandCollector) functionInstance(signature *types.Signature, args []types.Type, constants []ConstValue, diagnose bool, node ast.Node) *FunctionInstance {
	return d.functionInstanceWithPermissions(signature, args, constants, nil, diagnose, node)
}

func (d *demandCollector) functionInstanceWithPermissions(signature *types.Signature, args []types.Type, constants []ConstValue, permissions map[*types.TypeParam]*permission, diagnose bool, node ast.Node) *FunctionInstance {
	instance := &FunctionInstance{Signature: signature, TypeArgs: append([]types.Type(nil), args...), ConstArgs: append([]ConstValue(nil), constants...), Substitution: map[*types.TypeParam]types.Type{}, ConstSubstitution: map[*ConstParam]ConstValue{}, permissionSubstitution: permissions}
	if len(constants) != len(d.c.out.FunctionConstParams[signature]) {
		if diagnose {
			d.unresolved(node, "function array length")
		}
		return nil
	}
	for index, parameter := range d.c.out.FunctionConstParams[signature] {
		instance.ConstSubstitution[parameter] = constants[index]
	}
	if declaration := d.functions[signature]; declaration != nil {
		instance.Decl, instance.File = declaration.decl, declaration.file
	}
	for i, arg := range args {
		if i < signature.TypeParams().Len() {
			instance.Substitution[signature.TypeParams().At(i)] = arg
		}
	}
	for _, bound := range d.c.functionBounds[signature] {
		r := newBodyTypes(d.c.out, instance.Substitution, instance.ConstSubstitution)
		goal := d.c.substituteBoundApplication(bound, instance.Substitution, instance.ConstSubstitution, instance.permissionSubstitution)
		witness := d.c.proveApplication(goal)
		if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
			if diagnose {
				d.unresolved(node, "function prerequisite "+bound.Trait.Symbol.Name.Name)
			}
			return nil
		}
		if diagnose {
			var ok bool
			witness, ok = d.instantiateEvidence(witness, nil, nil, nil)
			if !ok {
				d.unresolved(node, "function prerequisite branch "+bound.Trait.Symbol.Name.Name)
				return nil
			}
		}
		instance.Evidence = append(instance.Evidence, witness)
		for _, associated := range bound.Trait.Associated {
			actual := evidenceAssociated(witness)[associated.Name]
			if projected := projectedBinding(bound, associated.Name); projected != nil {
				instance.Substitution[projected] = actual
				if instance.permissionSubstitution == nil {
					instance.permissionSubstitution = map[*types.TypeParam]*permission{}
				}
				instance.permissionSubstitution[projected] = d.c.evidenceAssociatedPermission(witness, associated.Name)
			} else if !types.Identical(r.typ(bound.Associated[associated.Name]), actual) {
				if diagnose {
					d.unresolved(node, "associated binding "+associated.Name)
				}
				return nil
			}
		}
	}
	if len(d.c.out.BodyCases[instance.Decl]) != 0 && diagnose {
		branch, sub, evidence, ok := d.selectBodyCase(instance.Decl, instance.Substitution, instance.ConstSubstitution, instance.permissionSubstitution)
		if !ok {
			d.unresolved(node, "function body branch")
			return nil
		}
		instance.Case, instance.Substitution, instance.CaseEvidence = branch, sub, evidence
		instance.permissionSubstitution = d.casePermissions(branch, sub, instance.ConstSubstitution, instance.permissionSubstitution)
	}
	return instance
}

func (d *demandCollector) function(instance *FunctionInstance, node ast.Node) *FunctionInstance {
	if len(instance.ConstArgs) != 0 {
		return d.constFunction(instance, node)
	}
	for _, old := range d.plan.Functions {
		if old.Decl == instance.Decl && old.Case == instance.Case && identicalArguments(old.TypeArgs, instance.TypeArgs) && sameEvidence(old.Evidence, instance.Evidence) && sameEvidence(old.CaseEvidence, instance.CaseEvidence) {
			return old
		}
	}
	d.plan.Functions = append(d.plan.Functions, instance)
	for _, evidence := range instance.Evidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	for _, evidence := range instance.CaseEvidence {
		if evidence.Impl != nil {
			d.impl(evidence.Impl, node)
		}
	}
	body := &BodyInstance{Node: instance.Decl, Decl: instance.Decl, File: instance.File, Function: instance, Case: instance.Case, CaseEvidence: instance.CaseEvidence, Substitution: instance.Substitution, ConstSubstitution: instance.ConstSubstitution, permissionSubstitution: instance.permissionSubstitution}
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
