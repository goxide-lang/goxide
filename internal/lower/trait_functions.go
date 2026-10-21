package lower

import (
	goast "go/ast"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type traitFunction struct {
	decl   *syntax.FuncDecl
	symbol *resolve.Symbol
}

func prepareTraitFunctions(info *check.Info) map[*types.Signature]*traitFunction {
	declarations := map[resolve.Ref]*syntax.FuncDecl{}
	for name, file := range info.Resolve.Files {
		syntax.Walk(file.AST, func(node syntax.Node) bool {
			if function, ok := node.(*syntax.FuncDecl); ok && function.Generics != nil {
				declarations[resolve.Ref{File: name, ID: function.Name.ID()}] = function
			}
			return true
		})
	}
	functions := map[*types.Signature]*traitFunction{}
	for symbol, typ := range info.Symbols {
		if symbol == nil || symbol.Kind != resolve.Function || symbol.AliasOf != nil {
			continue
		}
		declaration := declarations[symbol.Decl]
		signature, ok := typ.(*types.Signature)
		if declaration != nil && ok {
			functions[signature] = &traitFunction{decl: declaration, symbol: symbol}
		}
	}
	return functions
}

// Keep the source body in the entry itself: an extra forwarding frame changes
// the semantics of a directly deferred function that calls recover.
func (e *emitter) traitFunctionDeclaration(node *syntax.FuncDecl) goast.Decl {
	groups := e.functionEvidenceGroups(node)
	previous := e.evidenceNames
	e.evidenceNames = make(map[*check.TraitBound]string, len(previous))
	for bound, name := range previous {
		e.evidenceNames[bound] = name
	}
	for _, group := range groups {
		for _, bound := range group.bounds {
			e.evidenceNames[bound] = group.name
		}
	}
	defer func() { e.evidenceNames = previous }()
	typ := &goast.FuncType{TypeParams: e.functionGenerics(node), Params: e.params(node.Params), Results: e.params(node.Results)}
	return &goast.FuncDecl{Name: e.ident(node.Name), Type: typ, Body: e.functionBody(typ, node.Body)}
}

func (e *emitter) traitFunctionInstance(node syntax.Node) (goast.Expr, bool) {
	instance := e.info.FunctionInstances[node]
	if instance == nil {
		return nil, false
	}
	function := e.traits.functions[instance.Signature]
	if function == nil {
		return nil, false
	}
	if selected := e.traits.instances[instanceFunctionKey(e.info, function, instance)]; selected != nil {
		return e.sourceGlobal(selected.body.File, selected.name), true
	}
	if e.boundCallbacks == nil && e.instanceBody == nil && e.traits.legacyFunctions[function.decl] {
		var args []goast.Expr
		for _, typ := range instance.TypeArgs {
			args = append(args, e.goType(typ))
		}
		return enumIndices(e.sourceGlobal(function.symbol.Decl.File, function.symbol.GoName), args), true
	}
	if !e.traits.templateFunctions[function.decl] {
		return nil, false
	}
	return e.templateFunctionInstance(instance, function), true
}

func (p *traitProgram) prepareFunctionEntries(info *check.Info) {
	p.legacyFunctions = map[*syntax.FuncDecl]bool{}
	for _, function := range p.functions {
		legacy := len(info.GenericBounds[function.decl]) != 0
		for _, bound := range info.GenericBounds[function.decl] {
			legacy = legacy && bound.Param != nil
		}
		p.legacyFunctions[function.decl] = legacy
	}
	for changed := true; changed; {
		changed = false
		for _, function := range p.functions {
			if !p.legacyFunctions[function.decl] {
				continue
			}
			valid := true
			syntax.Walk(function.decl.Body, func(node syntax.Node) bool {
				if expression, ok := node.(syntax.Expr); ok {
					if member := info.Members[expression]; member != nil && member.Impl != nil && parameterizedImpl(implTemplate(member.Impl)) && len(member.Impl.Requirements) != 0 {
						valid = false
					}
				}
				if instance := info.FunctionInstances[node]; instance != nil {
					callee := p.functions[instance.Signature]
					if callee != nil && info.Demands.FunctionTemplates[callee.decl] {
						valid = valid && p.legacyFunctions[callee.decl]
						for _, argument := range instance.TypeArgs {
							if _, parameter := argument.(*types.TypeParam); parameter {
								continue
							}
							named, _ := types.Unalias(argument).(*types.Named)
							valid = valid && p.carriers[named] != nil
						}
					}
				}
				return valid
			})
			if !valid {
				p.legacyFunctions[function.decl] = false
				changed = true
			}
		}
	}
}

func (p *traitProgram) prepareTemplateHelpers(info *check.Info) {
	p.helperFunctions = map[*syntax.FuncDecl]bool{}
	p.helperImpls = map[*check.Impl]bool{}
	var inspect func(syntax.Node)
	var addFunction func(*traitFunction)
	var addImpl func(*check.Impl)
	addFunction = func(function *traitFunction) {
		if p.helperFunctions[function.decl] {
			return
		}
		p.helperFunctions[function.decl] = true
		inspect(function.decl.Body)
	}
	addImpl = func(impl *check.Impl) {
		impl = implTemplate(impl)
		if p.helperImpls[impl] {
			return
		}
		p.helperImpls[impl] = true
		for _, member := range impl.Methods {
			inspect(member.Decl.Body)
		}
	}
	inspect = func(body syntax.Node) {
		syntax.Walk(body, func(node syntax.Node) bool {
			if expression, ok := node.(syntax.Expr); ok {
				if member := info.Members[expression]; member != nil && member.Impl != nil && parameterizedImpl(implTemplate(member.Impl)) {
					addImpl(member.Impl)
				}
			}
			if instance := info.FunctionInstances[node]; instance != nil {
				if function := p.functions[instance.Signature]; function != nil && info.Demands.FunctionTemplates[function.decl] {
					addFunction(function)
				}
			}
			return true
		})
	}
	for _, function := range info.Demands.Functions {
		if declaration := p.functions[function.Signature]; declaration != nil && p.instances[instanceFunctionKey(info, declaration, function)] == nil {
			addFunction(declaration)
		}
	}
	for _, impl := range info.Demands.Impls {
		if impl.Template == nil {
			continue
		}
		for _, member := range impl.Methods {
			if p.instances[instanceImplKey(info, impl, member.Name)] == nil {
				addImpl(impl.Template)
				break
			}
		}
	}
	// Runtime views need a generic wrapper and its definition-package members.
	for _, impl := range p.impls {
		if parameterizedImpl(impl) && (!info.Demands.ImplTemplates[impl] || impl.Trait != nil && impl.Trait.Runtime) {
			addImpl(impl)
		}
	}
}
