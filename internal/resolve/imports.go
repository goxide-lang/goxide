package resolve

import (
	"fmt"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (r *resolver) collectUses() {
	for _, f := range r.pkg.Files {
		r.file = f
		for _, item := range f.AST.Items {
			d, ok := item.(*ast.UseDecl)
			if !ok || d.Tree == nil {
				continue
			}
			t := d.Tree
			u := &UseBinding{Decl: r.ref(d), Public: d.Vis.Kind == ast.VisPub, Glob: t.Glob}
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
			f.Definitions[d.ID()], f.Definitions[name.ID()] = u.Symbol, u.Symbol
			f.Visibility[name.ID()] = vis
			if u.Blank {
				continue
			}
			if len(scope.Aliases[u.Name]) > 0 {
				r.diag(name, "duplicate-use", fmt.Sprintf("duplicate use binding %q", name.Name))
			}
			scope.Aliases[u.Name] = append(scope.Aliases[u.Name], u.Symbol)
		}
	}
	for _, f := range r.pkg.Files {
		r.file = f
		for _, u := range f.Uses {
			if u.Blank || u.Glob {
				continue
			}
			t := r.useNodes[u]
			name := t.Alias
			if name == nil {
				name = t.Path.Segments[len(t.Path.Segments)-1].Name
			}
			if !u.Public && len(r.pkg.Scope.Aliases[u.Name]) > 0 {
				r.diag(name, "duplicate-use", fmt.Sprintf("duplicate use binding %q", name.Name))
			}
			if b := r.pkg.Scope.Bindings[u.Name]; b != nil {
				r.diag(name, "use-conflict", fmt.Sprintf("use binding %q conflicts with a package declaration", name.Name))
			} else if b := r.pkg.Scope.Parent.Bindings[u.Name]; b != nil && (u.Public || isStatic(b)) {
				r.diag(name, "prelude-conflict", fmt.Sprintf("use binding %q conflicts with a preimported name", name.Name))
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
	}
	for _, f := range r.pkg.Files {
		for _, u := range f.Uses {
			r.resolveUse(u)
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
	root := key(t.Path.Segments[0].Name)
	var local *Symbol
	var aliases []*Symbol
	for s := f.Scope; s != nil; s = s.Parent {
		if len(s.Aliases[root]) > 0 {
			aliases = s.Aliases[root]
			break
		}
	}
	// A self-named import is the external root, not an alias edge.
	if len(aliases) == 1 && aliases[0] == u.Symbol {
		aliases = nil
	}
	if len(aliases) > 0 {
		res := choices(aliases)
		if res.Symbol == nil {
			old := r.file
			r.file = f
			r.diag(t.Path.Segments[0].Name, "ambiguous", "ambiguous use target")
			r.file = old
			r.useStates[u] = 3
			return false
		}
		other := r.symbolUses[res.Symbol]
		if other != nil && !r.resolveUse(other) {
			r.useStates[u] = 3
			return false
		}
		u.Target = append(append([]string{}, res.Symbol.Target...), u.Target[1:]...)
		local = res.Symbol.AliasOf
		if u.Symbol != nil {
			u.Symbol.Members = res.Symbol.Members
		}
	} else if b := r.pkg.Scope.Bindings[root]; b != nil && isStatic(b) {
		local = b
		if len(u.Target) == 1 && u.Symbol != nil {
			u.Symbol.Members = b.Members
		}
	}
	if u.Symbol != nil {
		u.Symbol.Target = u.Target
		if local != nil {
			res := Resolution{Symbol: local}
			for _, seg := range t.Path.Segments[1:] {
				res = r.member(res, seg.Name)
			}
			if !res.Deferred {
				u.Symbol.AliasOf = res.Symbol
			}
		} else if len(u.Target) == 1 {
			u.Symbol.Kind = Module
		}
	}
	r.useStates[u] = 2
	return true
}

func (r *resolver) path(scope *Scope, p *ast.Path) Resolution {
	return r.resolvePath(scope, p, p != nil && len(p.Segments) > 1)
}

func (r *resolver) staticPath(scope *Scope, p *ast.Path) Resolution {
	return r.resolvePath(scope, p, true)
}

func (r *resolver) resolvePath(scope *Scope, p *ast.Path, static bool) Resolution {
	if p == nil || len(p.Segments) == 0 {
		return Resolution{}
	}
	if p.Root == ast.SuperRoot || (len(p.Segments) > 1 && p.Segments[0].Name.Name == "super" && !p.Segments[0].Name.Raw) {
		r.diag(p.Segments[0].Name, "unsupported-root", "super:: paths are not yet supported")
		return Resolution{}
	}
	res := r.ident(scope, p.Segments[0].Name, static)
	for i, seg := range p.Segments {
		if seg.Args != nil {
			for _, arg := range seg.Args.Args {
				r.typ(scope, arg.Type)
			}
		}
		if i == 0 {
			continue
		}
		if res.Symbol == nil && !res.Deferred {
			break
		}
		res = r.member(res, seg.Name)
		r.record(seg.Name, res)
	}
	r.file.References[p.ID()] = res
	return res
}

func (r *resolver) member(base Resolution, name *ast.Ident) Resolution {
	if base.Symbol != nil && base.Symbol.AliasOf != nil {
		base.Symbol = base.Symbol.AliasOf
		base.Path = nil
	}
	if base.Symbol != nil && base.Symbol.Members != nil {
		s := base.Symbol.Members
		if b := s.Bindings[key(name)]; b != nil {
			if b.Kind == Field {
				return Resolution{}
			}
			return Resolution{Symbol: b}
		}
		if a := s.Aliases[key(name)]; len(a) > 0 {
			return choices(a)
		}
		if len(base.Symbol.Target) == 0 && base.Symbol.Kind != Type && base.Symbol.Kind != TypeParam {
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
			for _, e := range r.importer.Lookup(path, key(name)) {
				choicesList = append(choicesList, externalSymbol(path, e))
			}
			if len(choicesList) > 0 {
				return choices(choicesList)
			}
			return Resolution{}
		}
		return Resolution{Path: append(append([]string{}, path...), name.Name), Deferred: true}
	}
	// Associated items of type parameters and built-in types need type checking.
	if base.Symbol != nil && (base.Symbol.Kind == TypeParam || base.Symbol.Kind == Type) {
		return Resolution{Symbol: base.Symbol, Path: []string{base.Symbol.Name.Name, name.Name}, Deferred: true}
	}
	return Resolution{}
}
