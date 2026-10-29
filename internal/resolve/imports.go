package resolve

import (
	"fmt"
	"slices"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (r *resolver) collectUses() {
	for _, f := range r.pkg.Files {
		r.file = f
		for _, item := range f.AST.Items {
			d, ok := item.(*ast.UseDecl)
			if !ok || d.Tree == nil || d.Tree.Children != nil {
				continue
			}
			t := d.Tree
			u := &UseBinding{Decl: r.ref(d), Public: d.Vis.Kind == ast.VisPub, Glob: t.Glob, Implicit: d.Implicit, rootFallback: slices.Clone(d.RootFallback)}
			for _, seg := range t.Path.Segments {
				u.Target = append(u.Target, seg.Name.Name)
			}
			name := t.Alias
			if name == nil && len(t.Path.Segments) > 0 && !t.Glob {
				name = t.Path.Segments[len(t.Path.Segments)-1].Name
			}
			if name != nil {
				u.Name = key(name)
				u.Blank = name.Name == "_"
				if !f.AST.NativeGo && name.Name == "crate" && name.Span().End != 0 {
					r.diag(name, "reserved-root", "crate is a reserved static path root")
				}
			}
			f.Uses = append(f.Uses, u)
			r.useNodes[u], r.useFiles[u] = t, f
			vis := Private
			if u.Public {
				vis = Public
			}
			f.Visibility[d.ID()], f.Visibility[t.ID()] = vis, vis
			scope := f.Scope
			if u.Public {
				scope = r.pkg.Scope
			}
			if u.Glob {
				scope.Globs = append(scope.Globs, u)
				continue
			}
			if name == nil {
				continue
			}
			u.Symbol = &Symbol{Name: u.Name, Kind: Import, Decl: r.ref(name), Visibility: vis, GoName: name.Name, Target: u.Target}
			if !u.Blank {
				mapped, ok := generatedName(name.Name, vis)
				if !ok {
					r.diag(name, "go-name", fmt.Sprintf("public name %q has no uppercase initial", name.Name))
				}
				u.Symbol.GoName = mapped
				f.GoNames[d.ID()], f.GoNames[name.ID()] = mapped, mapped
			}
			r.symbolUses[u.Symbol] = u
			r.useOwners[u.Symbol] = r
			f.Definitions[d.ID()], f.Definitions[name.ID()] = u.Symbol, u.Symbol
			f.Visibility[name.ID()] = vis
			if u.Blank {
				continue
			}
			scope.Aliases[u.Name] = append(scope.Aliases[u.Name], u.Symbol)
			macro := *u
			macro.Target = append([]string(nil), u.Target...)
			macro.Name.Namespace = MacroNamespace
			symbol := *u.Symbol
			symbol.Name = macro.Name
			macro.Symbol = &symbol
			u.Macro = &macro
			r.useNodes[u.Macro], r.useFiles[u.Macro] = t, f
			r.symbolUses[u.Macro.Symbol], r.useOwners[u.Macro.Symbol] = u.Macro, r
			scope.Aliases[macro.Name] = append(scope.Aliases[macro.Name], macro.Symbol)
		}
	}
}

func (r *resolver) checkUses() {
	for _, f := range r.pkg.Files {
		r.file = f
		for _, u := range f.Uses {
			if u.Macro != nil {
				r.checkUse(u.Macro)
				if u.Symbol == nil && u.Macro.Symbol != nil {
					t := r.useNodes[u]
					name := t.Alias
					if name == nil {
						name = t.Path.Segments[len(t.Path.Segments)-1].Name
					}
					f.Definitions[u.Decl.ID], f.Definitions[name.ID()] = u.Macro.Symbol, u.Macro.Symbol
				}
			}
			r.checkUse(u)
		}
	}
}

func (r *resolver) checkUse(u *UseBinding) {
	f := r.useFiles[u]
	if u.Symbol == nil {
		return
	}
	if u.Blank || u.Glob {
		return
	}
	t := r.useNodes[u]
	name := t.Alias
	if name == nil {
		name = t.Path.Segments[len(t.Path.Segments)-1].Name
	}
	if name.Name == "crate" && name.Span().End == 0 {
		return
	}
	scope := f.Scope
	if u.Public {
		scope = r.pkg.Scope
	}
	if len(scope.Aliases[u.Name]) > 1 || !u.Public && len(r.pkg.Scope.Aliases[u.Name]) > 0 {
		r.diag(name, "duplicate-use", fmt.Sprintf("duplicate use binding %q", name.Name))
	}
	if b := r.pkg.Scope.Bindings[u.Name]; b != nil {
		same := b.Kind == Module && (f.AST.NativeGo || len(u.Target) == 1 && u.Target[0] == b.Name.Name)
		if b.Kind == Module && u.Resolved != nil && u.Resolved.Kind == Module && slices.Equal(u.Resolved.Target, b.Target) {
			same = true
		}
		if !same && b.Kind == Module && r.importer != nil && len(u.Target) > 1 {
			last := len(u.Target) - 1
			for _, member := range r.importer.Lookup(u.Target[:last], Key{Name: u.Target[last]}) {
				if member.Kind == Module && slices.Equal(member.Target, b.Target) {
					same = true
				}
			}
		}
		if !same {
			r.diag(name, "use-conflict", fmt.Sprintf("use binding %q conflicts with a package declaration", name.Name))
		}
	} else if b := r.pkg.Scope.Parent.Bindings[u.Name]; b != nil && !shadowablePrelude(b) && (u.Public || isStatic(b)) {
		r.diag(name, "prelude-conflict", fmt.Sprintf("use binding %q conflicts with a preimported name", name.Name))
	}
	if u.Name.Namespace == MacroNamespace {
		return
	}
	r.checkGoName(r.pkg.Scope, name, u.Symbol)
	for _, otherFile := range r.pkg.Files {
		if otherFile != f && !u.Public {
			continue
		}
		for _, other := range otherFile.Uses {
			if other.Symbol == nil || other == u || other.Blank || other.Glob || other.Name.Ctx != u.Name.Ctx || other.Name == u.Name {
				continue
			}
			if otherFile != f && !other.Public && !u.Public {
				continue
			}
			if u.Symbol.GoName == other.Symbol.GoName || u.Symbol.GoName == other.Name.Name || other.Symbol.GoName == u.Name.Name {
				r.diag(name, "go-name-conflict", fmt.Sprintf("Go name %q conflicts with use binding %q", u.Symbol.GoName, other.Name.Name))
			}
		}
	}
}

func (r *resolver) resolveUses() {
	for _, f := range r.pkg.Files {
		r.file = f
		for _, u := range f.Uses {
			r.resolveUse(u)
			if u.Macro != nil {
				r.resolveUse(u.Macro)
			}
		}
	}
}

func (r *resolver) resolveUse(u *UseBinding) bool {
	switch r.useStates[u] {
	case 1:
		old := r.file
		r.file = r.useFiles[u]
		t := r.useNodes[u]
		r.diag(t.Path.Segments[0].Name, "alias-cycle", "use alias cycle")
		r.file = old
		return false
	case 2:
		return true
	case 3:
		return false
	}
	r.useStates[u] = 1
	t := r.useNodes[u]
	if len(t.Path.Segments) == 0 {
		r.useStates[u] = 3
		return false
	}
	f := r.useFiles[u]
	previous := r.file
	r.file = f
	defer func() { r.file = previous }()
	root := key(t.Path.Segments[0].Name)
	if len(t.Path.Segments) == 1 && !u.Glob {
		root.Namespace = u.Name.Namespace
	}
	var local *Symbol
	var aliases []*Symbol
	for scope := r.identScope(f.Scope, t.Path.Segments[0].Name); scope != nil; scope = scope.Parent {
		// Resolving an alias can remove its unavailable namespace branch.
		for _, symbol := range slices.Clone(scope.Aliases[root]) {
			owner := r.useOwners[symbol]
			if owner != nil {
				other := owner.symbolUses[symbol]
				// A self-named import denotes the external root in both spaces.
				if other.Decl == u.Decl {
					continue
				}
				if !owner.resolveUse(other) {
					r.useStates[u] = 3
					return false
				}
				if other.Symbol == nil {
					continue
				}
			}
			aliases = append(aliases, symbol)
		}
		if len(aliases) != 0 {
			break
		}
		if symbol := scope.Bindings[root]; symbol != nil && isStatic(symbol) {
			local = symbol
			break
		}
		if r.hygiene != nil {
			if context, ok := r.hygiene.boundaries[scope]; ok {
				root.Ctx = context.Definition.Ctx
			}
		}
	}
	u.BoundRoot = len(aliases) != 0 || local != nil
	if !u.BoundRoot && len(u.rootFallback) != 0 {
		u.Target = slices.Clone(u.rootFallback)
	}
	if len(aliases) > 0 {
		res := choices(aliases)
		if res.Symbol == nil {
			r.diag(t.Path.Segments[0].Name, "ambiguous", "ambiguous use target")
			r.useStates[u] = 3
			return false
		}
		if owner := r.useOwners[res.Symbol]; owner != nil && len(t.Path.Segments) == 1 {
			u.Resolved = owner.symbolUses[res.Symbol].Resolved
		}
		u.Target = append(append([]string{}, res.Symbol.Target...), u.Target[1:]...)
		local = res.Symbol.AliasOf
		if u.Symbol != nil {
			u.Symbol.Members = res.Symbol.Members
		}
	} else if b := local; b != nil {
		if b.Kind == Module && len(b.Target) != 0 {
			u.Target = append(append([]string{}, b.Target...), u.Target[1:]...)
		}
		if len(t.Path.Segments) == 1 && u.Symbol != nil {
			u.Symbol.Members = b.Members
		}
	}
	absent := false
	if local != nil {
		res := Resolution{Symbol: local}
		for index, seg := range t.Path.Segments[1:] {
			namespace := OrdinaryNamespace
			if index == len(t.Path.Segments)-2 && !u.Glob {
				namespace = u.Name.Namespace
			}
			res = r.memberIn(res, seg.Name, namespace)
		}
		u.Resolved = res.Symbol
		absent = res.Symbol == nil && !res.Deferred && len(res.Candidates) == 0
		if res.Symbol != nil && len(res.Symbol.Target) != 0 {
			u.Target = append([]string(nil), res.Symbol.Target...)
		} else if !res.Deferred && u.Symbol != nil {
			u.Symbol.AliasOf = res.Symbol
		}
	} else if u.Symbol != nil && len(u.Target) == 1 && u.Name.Namespace == OrdinaryNamespace {
		u.Symbol.Kind = Module
	}
	if u.Symbol != nil {
		u.Symbol.Target = u.Target
		if u.Resolved == nil && u.Symbol.AliasOf == nil && r.importer != nil && (len(u.Target) > 1 || u.Name.Namespace == MacroNamespace) {
			last := len(u.Target) - 1
			name := Key{Name: u.Target[last], Ctx: u.Name.Ctx, Namespace: u.Name.Namespace}
			var symbols []*Symbol
			for _, external := range r.importer.Lookup(u.Target[:last], name) {
				symbols = append(symbols, externalSymbol(u.Target[:last], external))
			}
			resolved := choices(symbols)
			if resolved.Symbol != nil {
				u.Resolved = resolved.Symbol
				u.Target = append([]string(nil), resolved.Symbol.Target...)
				u.Symbol.Target = u.Target
			} else if len(symbols) == 0 && u.Name.Namespace == MacroNamespace && !r.deferredImports() {
				absent = true
			}
		}
	}
	if absent {
		r.removeUseSymbol(u)
	}
	r.useStates[u] = 2
	if u.Macro != nil {
		r.resolveUse(u.Macro)
		// An unresolved ordinary route may still denote a package. A known
		// macro-only leaf proves that this use has no ordinary binding.
		if u.Resolved == nil && u.Symbol != nil && u.Symbol.AliasOf == nil && u.Macro.Resolved != nil {
			r.removeUseSymbol(u)
		}
	}
	return true
}

func (r *resolver) deferredImports() bool {
	if r.importer == nil {
		return true
	}
	deferred, ok := r.importer.(interface{ DeferredImports() bool })
	return ok && deferred.DeferredImports()
}

func (r *resolver) removeUseSymbol(u *UseBinding) {
	if u.Symbol == nil {
		return
	}
	file := r.useFiles[u]
	scope := file.Scope
	if u.Public {
		scope = r.pkg.Scope
	}
	scope.Aliases[u.Name] = slices.DeleteFunc(scope.Aliases[u.Name], func(symbol *Symbol) bool { return symbol == u.Symbol })
	tree := r.useNodes[u]
	name := tree.Alias
	if name == nil {
		name = tree.Path.Segments[len(tree.Path.Segments)-1].Name
	}
	for _, id := range []ast.NodeID{u.Decl.ID, name.ID()} {
		if file.Definitions[id] == u.Symbol {
			delete(file.Definitions, id)
		}
	}
	u.Symbol = nil
}

func (r *resolver) path(scope *Scope, p *ast.Path) Resolution {
	return r.resolvePath(scope, p, p != nil && len(p.Segments) > 1)
}

func (r *resolver) staticPath(scope *Scope, p *ast.Path) Resolution {
	return r.resolvePath(scope, p, true)
}

func (r *resolver) resolvePath(scope *Scope, p *ast.Path, static bool) Resolution {
	return r.pathMode(scope, p, static, false)
}

func (r *resolver) macroPath(scope *Scope, p *ast.Path) Resolution {
	return r.pathMode(scope, p, true, true)
}

func (r *resolver) pathMode(scope *Scope, p *ast.Path, static, macro bool) Resolution {
	if p == nil || len(p.Segments) == 0 {
		return Resolution{}
	}
	if p.Root == ast.SuperRoot || (len(p.Segments) > 1 && p.Segments[0].Name.Name == "super" && !p.Segments[0].Name.Raw) {
		r.diag(p.Segments[0].Name, "unsupported-root", "super:: paths are not yet supported")
		return Resolution{}
	}
	name := p.Segments[0].Name
	identity := key(name)
	if macro && len(p.Segments) == 1 {
		identity.Namespace = MacroNamespace
	}
	res := r.lookup(r.identScope(scope, name), identity, static)
	r.pathRecord(name, &res, macro)
	for i, seg := range p.Segments {
		r.genericArgs(scope, seg.Args)
		if i == 0 {
			continue
		}
		if res.Symbol == nil && !res.Deferred {
			break
		}
		namespace := OrdinaryNamespace
		if macro && i == len(p.Segments)-1 {
			namespace = MacroNamespace
		}
		res = r.memberIn(res, seg.Name, namespace)
		r.pathRecord(seg.Name, &res, macro)
	}
	r.file.References[p.ID()] = res
	return res
}

func (r *resolver) pathRecord(n ast.Node, res *Resolution, macro bool) {
	if macro && res.Symbol == nil && len(res.Candidates) == 0 {
		res.Deferred = true
	}
	r.record(n, *res)
}

func (r *resolver) member(base Resolution, name *ast.Ident) Resolution {
	return r.memberIn(base, name, OrdinaryNamespace)
}

func (r *resolver) memberIn(base Resolution, name *ast.Ident, namespace Namespace) Resolution {
	if base.Symbol != nil && base.Symbol.AliasOf != nil {
		base.Symbol = base.Symbol.AliasOf
		base.Path = nil
	}
	if base.Symbol != nil && !isStatic(base.Symbol) {
		return Resolution{}
	}
	if base.Deferred && base.Symbol != nil && (base.Symbol.Kind == Type || base.Symbol.Kind == TypeParam || base.Symbol.Kind == Enum) {
		// Keep a type's qualified associated path for checking; it is not an import.
		return Resolution{Symbol: base.Symbol, Path: append(append([]string{}, base.Path...), name.Name), Deferred: true}
	}
	if base.Symbol != nil && base.Symbol.Members != nil {
		s := base.Symbol.Members
		k := key(name)
		k.Namespace = namespace
		if s.Bindings[k] == nil && len(s.Aliases[k]) == 0 {
			k = r.memberKey(name)
			k.Namespace = namespace
		}
		if b := s.Bindings[k]; b != nil {
			if b.Kind == Field {
				return Resolution{}
			}
			return Resolution{Symbol: b}
		}
		if a := s.Aliases[k]; len(a) > 0 {
			return choices(a)
		}
		if len(base.Symbol.Target) == 0 && base.Symbol.Kind != Type && base.Symbol.Kind != TypeParam && base.Symbol.Kind != Enum {
			return Resolution{}
		}
	}
	path := base.Path
	if base.Symbol != nil && len(base.Symbol.Target) > 0 {
		path = base.Symbol.Target
	}
	if len(path) > 0 {
		if r.importer != nil {
			var choicesList []*Symbol
			identity := r.memberKey(name)
			identity.Namespace = namespace
			for _, e := range r.importer.Lookup(path, identity) {
				choicesList = append(choicesList, externalSymbol(path, e))
			}
			if len(choicesList) > 0 {
				return choices(choicesList)
			}
			if base.Symbol != nil && (base.Symbol.Kind == Type || base.Symbol.Kind == Enum) {
				return Resolution{Symbol: base.Symbol, Path: append(append([]string{}, path...), name.Name), Deferred: true}
			}
			if r.deferredImports() {
				return Resolution{Path: append(append([]string{}, path...), name.Name), Deferred: true}
			}
			return Resolution{}
		}
		return Resolution{Path: append(append([]string{}, path...), name.Name), Deferred: true}
	}
	// Source impl selection is shared by structs, enums and constrained parameters.
	if base.Symbol != nil && (base.Symbol.Kind == TypeParam || base.Symbol.Kind == Type || base.Symbol.Kind == Enum) {
		return Resolution{Symbol: base.Symbol, Path: []string{base.Symbol.Name.Name, name.Name}, Deferred: true}
	}
	return Resolution{}
}
