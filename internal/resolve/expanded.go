package resolve

import (
	"reflect"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// ResolveExpanded 用 expanded.Files 替换 files 中对应的原始树后解析。
// files 为原始源码的命名表，须包含宏定义文件及定义包中需要参与查找的文件。
// nil expanded 等价于 Resolve；展开失败的错误仍由 Expand 的调用方处理。
func ResolveExpanded(files map[string]*ast.File, expanded *expand.Result, importer Importer) *Result {
	if expanded == nil {
		return Resolve(files, importer)
	}
	h := &hygiene{expanded: expanded, names: make(map[*ast.File]string), views: make(map[contextView]*Scope),
		boundaries: make(map[*Scope]expand.Context), callSites: make(map[ast.ExpnID]*Scope), owners: make(map[*Scope]ast.Node)}
	trees := make(map[string]*ast.File, len(files))
	for name, tree := range files {
		h.names[tree] = name
		trees[name] = tree
	}
	for _, tree := range expanded.Files {
		original := expanded.Origins[tree].File
		if name, ok := h.names[original]; ok {
			trees[name] = tree
			h.names[tree] = name
		}
	}
	result := resolveFiles(trees, h, importer)
	if len(result.Diagnostics) == 0 {
		h.validateAllocations()
	}
	return result
}

type contextView struct {
	lexical *Scope
	ctx     ast.SyntaxCtx
}

type hygiene struct {
	expanded      *expand.Result
	names         map[*ast.File]string
	result        *Result
	views         map[contextView]*Scope
	boundaries    map[*Scope]expand.Context
	callSites     map[ast.ExpnID]*Scope
	owners        map[*Scope]ast.Node
	genericOwners map[expand.Origin]expand.Origin
}

func (h *hygiene) init(result *Result) {
	h.result = result
	if len(h.expanded.Expansions) != 0 {
		result.Expansions = h.expanded.Expansions
	}
	result.Prints = h.expanded.Prints
	result.GoResults = h.expanded.GoResults
	if len(h.expanded.Contexts) == 0 {
		return
	}
	result.Contexts = make(map[ast.SyntaxCtx]*Scope)
	for ctx, c := range h.expanded.Contexts {
		scope := newScope(ContextScope, nil)
		result.Contexts[ctx] = scope
		h.boundaries[scope] = c
	}
	for ctx, c := range h.expanded.Contexts {
		scope := result.Contexts[ctx]
		if root, ok := h.expanded.RuntimeRoots[ctx]; ok {
			key := Key{Name: root.Name, Ctx: ctx}
			scope.Bindings[key] = &Symbol{Name: key, Kind: Module, Visibility: Public, Target: append([]string(nil), root.Target...)}
			continue
		}
		scope.Parent = h.target(c.Definition)
		scope.Caller = h.target(c.Caller)
	}
}

func (h *hygiene) target(target expand.Scope) *Scope {
	if scope := h.result.Contexts[target.Ctx]; scope != nil {
		return scope
	}
	if file := h.result.Files[h.names[target.File]]; file != nil {
		return file.Scope
	}
	return nil
}

// 每个词法块在其卫生链中保留自己的绑定，重复展开的块不会互相泄漏。
func (h *hygiene) view(lexical *Scope, ctx ast.SyntaxCtx) *Scope {
	root := h.result.Contexts[ctx]
	if root == nil {
		return lexical
	}
	if lexical == nil || lexical.Kind == FileScope || lexical.Kind == PackageScope || lexical.Kind == PreludeScope {
		return root
	}
	k := contextView{lexical, ctx}
	if scope := h.views[k]; scope != nil {
		return scope
	}
	scope := &Scope{Kind: lexical.Kind, Parent: h.view(lexical.Parent, ctx),
		Bindings: lexical.Bindings, Aliases: lexical.Aliases, Labels: lexical.Labels}
	h.views[k] = scope
	return scope
}

func (r *resolver) enterExpansion(scope *Scope, n ast.Node) {
	if r.hygiene == nil || n == nil {
		return
	}
	h := r.hygiene
	id := expansionID(n)
	// A nested expansion can replace the complete outer expression.
	for _, expn := range h.expanded.Expansions[id].Stack {
		if h.callSites[expn] != nil {
			continue
		}
		h.callSites[expn] = scope
		e := h.expanded.Expansions[expn]
		c := h.expanded.Contexts[e.Ctx]
		if root := h.result.Contexts[e.Ctx]; root != nil {
			root.Caller = snapshotScope(h.view(scope, c.Caller.Ctx))
			if c.Caller.Ctx == 0 && h.names[c.Caller.File] != r.file.Name {
				root.Caller = h.target(c.Caller)
			}
		}
	}
}

func (r *resolver) identScope(scope *Scope, id *ast.Ident) *Scope {
	if r.hygiene == nil {
		return scope
	}
	h := r.hygiene
	return r.syntaxScope(scope, id.Ctx, id.Expn, h.expanded.Origins[id], h.expanded.Scopes[id])
}

func (r *resolver) tokenScope(scope *Scope, tok *token.Token, expn ast.ExpnID) (*Scope, ast.SyntaxCtx) {
	if r.hygiene != nil {
		if context, ok := r.hygiene.expanded.TokenContexts[tok]; ok {
			return r.syntaxScope(scope, context.Ctx, expn, context.Origin, context.Scope), context.Ctx
		}
	}
	return scope, 0
}

func (r *resolver) syntaxScope(scope *Scope, ctx ast.SyntaxCtx, expn ast.ExpnID, origin expand.Origin, target expand.Scope) *Scope {
	h := r.hygiene
	e := h.expanded.Expansions[expn]
	if expn != 0 && e.Ctx != ctx {
		for current := e.Ctx; current != 0; {
			c, ok := h.expanded.Contexts[current]
			if !ok {
				break
			}
			if c.Caller.Ctx == ctx {
				anchor := h.result.Contexts[current].Caller
				return h.captureScope(scope, h.callSites[c.Expn], anchor, origin, expn)
			}
			current = c.Caller.Ctx
		}
	}
	if h.result.Contexts[ctx] != nil {
		return h.view(scope, ctx)
	}
	if target.File != nil && h.names[target.File] != r.file.Name {
		return h.target(target)
	}
	return scope
}

func (r *resolver) lexicalScope(kind ScopeKind, parent *Scope, owner ast.Node) *Scope {
	scope := newScope(kind, parent)
	if r.hygiene != nil {
		r.hygiene.owners[scope] = owner
	}
	return scope
}

func (r *resolver) contextAlias(scope *Scope, owner ast.Node, name string, symbol *Symbol) {
	if r.hygiene != nil {
		ctx := r.hygiene.expanded.Expansions[expansionID(owner)].Ctx
		if ctx != 0 {
			k := Key{Name: name, Ctx: ctx}
			if scope.Bindings[k] == nil {
				scope.Bindings[k] = symbol
			}
		}
	}
}

// 调用点保存可见绑定；展开语句随后在同一块声明的名字不能捕获它。
func snapshotScope(scope *Scope) *Scope {
	if scope == nil || scope.Kind == FileScope || scope.Kind == PackageScope || scope.Kind == PreludeScope || scope.Kind == ContextScope {
		return scope
	}
	copy := newScope(scope.Kind, snapshotScope(scope.Parent))
	for k, v := range scope.Bindings {
		copy.Bindings[k] = v
	}
	for k, v := range scope.Aliases {
		copy.Aliases[k] = v
	}
	copy.Globs, copy.Labels = scope.Globs, scope.Labels
	return copy
}

// 捕获片段内的函数和块仍有自己的局部作用域；模板插入的块则跳过。
func (h *hygiene) captureScope(scope, stop, anchor *Scope, origin expand.Origin, expn ast.ExpnID) *Scope {
	if scope == nil || scope == stop || scope.Kind == FileScope || scope.Kind == PackageScope {
		return anchor
	}
	parent := h.captureScope(scope.Parent, stop, anchor, origin, expn)
	// A macro can copy a generic group or rebuild one from captured parameters.
	// References from those parameters belong to the generated declaration's
	// bindings, even when the enclosing declaration or group is generated.
	if bindings := h.capturedGenericBindings(scope, origin); len(bindings) != 0 {
		return &Scope{Kind: scope.Kind, Parent: parent, Bindings: bindings}
	}
	owner, ok := h.expanded.Origins[h.owners[scope]]
	if !ok || !containsOrigin(owner, origin) {
		return parent
	}
	for _, id := range h.expanded.Expansions[expn].Stack {
		expansion := h.expanded.Expansions[id]
		if containsOrigin(expansion.Call, owner) || (expansion.Input.File != nil && containsOrigin(expansion.Input, owner)) {
			return &Scope{Kind: scope.Kind, Parent: parent, Bindings: scope.Bindings, Aliases: scope.Aliases, Labels: scope.Labels, Globs: scope.Globs}
		}
	}
	return parent
}

func (h *hygiene) capturedGenericBindings(scope *Scope, origin expand.Origin) map[Key]*Symbol {
	var params *ast.GenericParams
	switch owner := h.owners[scope].(type) {
	case *ast.FuncDecl:
		params = owner.Generics
	case *ast.TypeDecl:
		params = owner.Generics
	case *ast.TypeSpec:
		params = owner.Generics
	case *ast.EnumDecl:
		params = owner.Generics
	case *ast.TraitDecl:
		params = owner.Generics
	case *ast.ImplDecl:
		params = owner.Generics
	}
	if params == nil {
		return nil
	}
	captured := containsOrigin(h.expanded.Origins[params], origin)
	for _, param := range params.Params {
		captured = captured || containsOrigin(h.expanded.Origins[param], origin)
		for _, name := range param.Names {
			captured = captured || containsOrigin(h.expanded.Origins[name], origin) || containsOrigin(h.sourceGenericOwner(name), origin)
		}
	}
	if !captured {
		return nil
	}
	bindings := map[Key]*Symbol{}
	for name, symbol := range scope.Bindings {
		if symbol.Kind == TypeParam || symbol.Kind == ConstParam {
			bindings[name] = symbol
		}
	}
	return bindings
}

// A copied field type still refers to the generic parameters of its source
// type or enum. Preserve that relationship when a derive emits a new impl.
// Function and block origins are deliberately excluded: their locals must keep
// the lexical scopes handled by captureScope.
func (h *hygiene) sourceGenericOwner(name *ast.Ident) expand.Origin {
	origin := h.expanded.Origins[name]
	if origin.File == nil {
		return expand.Origin{}
	}
	if owner, ok := h.genericOwners[origin]; ok {
		return owner
	}
	var owner expand.Origin
	ast.Walk(origin.File, func(node ast.Node) bool {
		var params *ast.GenericParams
		switch node := node.(type) {
		case *ast.TypeDecl:
			params = node.Generics
		case *ast.TypeSpec:
			params = node.Generics
		case *ast.EnumDecl:
			params = node.Generics
		}
		if params == nil {
			return true
		}
		for _, group := range params.Params {
			for _, parameter := range group.Names {
				if containsOrigin(expand.Origin{File: origin.File, Span: parameter.Span()}, origin) {
					owner = expand.Origin{File: origin.File, Span: node.Span()}
					return false
				}
			}
		}
		return true
	})
	if h.genericOwners == nil {
		h.genericOwners = map[expand.Origin]expand.Origin{}
	}
	h.genericOwners[origin] = owner
	return owner
}

func containsOrigin(outer, inner expand.Origin) bool {
	return outer.File == inner.File && outer.Span.Start <= inner.Span.Start && outer.Span.End >= inner.Span.End
}

func (r *resolver) introduce(scope *Scope, name *ast.Ident, symbol *Symbol) {
	if r.hygiene == nil || name.Name == "_" {
		return
	}
	h := r.hygiene
	if scope.Kind == MemberScope && name.Ctx != symbol.Name.Ctx {
		scope.Bindings[key(name)] = symbol
	}
	if scope.Kind != PackageScope {
		return
	}
	if alias, ok := h.expanded.Introductions[name]; ok {
		if context := r.result.Contexts[alias.Ctx]; context != nil {
			context.Bindings[Key{Name: alias.Name, Ctx: alias.Ctx}] = symbol
		}
	}
}

func (r *resolver) declarationKey(scope *Scope, name *ast.Ident) Key {
	k := key(name)
	if r.hygiene != nil && scope.Kind == MemberScope {
		for c, ok := r.hygiene.expanded.Contexts[k.Ctx]; ok; c, ok = r.hygiene.expanded.Contexts[k.Ctx] {
			k.Ctx = c.Caller.Ctx
		}
	}
	return k
}

func (r *resolver) memberKey(name *ast.Ident) Key {
	k := key(name)
	if r.hygiene != nil {
		for c, ok := r.hygiene.expanded.Contexts[k.Ctx]; ok; c, ok = r.hygiene.expanded.Contexts[k.Ctx] {
			k.Ctx = c.Definition.Ctx
		}
	}
	return k
}

func expansionID(n ast.Node) ast.ExpnID {
	v := reflect.ValueOf(n)
	if v.IsNil() {
		return 0
	}
	return ast.ExpnID(v.Elem().FieldByName("Expn").Uint())
}

func (h *hygiene) diagnostic(n ast.Node, d *Diagnostic) {
	id := expansionID(n)
	if origin, ok := h.expanded.Origins[n]; ok {
		if name, ok := h.names[origin.File]; ok {
			d.File, d.Span = name, origin.Span
		}
		if id != 0 {
			d.Definition = origin
		}
	}
	if location, ok := h.expanded.Locations[n]; ok {
		if name, ok := h.names[location.File]; ok {
			d.File, d.Span = name, location.Span
		}
	}
	d.Origin = Origin{File: d.File, Span: d.Span, Expn: id}
	if e, ok := h.expanded.Expansions[d.Origin.Expn]; ok {
		d.Call = e.Call
		d.Origin.Stack = append([]ast.ExpnID(nil), e.Stack...)
	}
}
