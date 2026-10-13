package resolve

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func key(id *ast.Ident) Key { return Key{id.Name, id.Ctx} }
func newScope(kind ScopeKind, parent *Scope) *Scope {
	return &Scope{Kind: kind, Parent: parent, Bindings: make(map[Key]*Symbol), Aliases: make(map[Key][]*Symbol), Labels: make(map[Key]*Symbol)}
}

func prelude() *Scope {
	s := newScope(PreludeScope, nil)
	for _, name := range types.Universe.Names() {
		kind := Builtin
		switch types.Universe.Lookup(name).(type) {
		case *types.TypeName:
			kind = Type
		case *types.Const:
			kind = Constant
		case *types.Nil:
			kind = Variable
		}
		k := Key{Name: name}
		s.Bindings[k] = &Symbol{Name: k, Kind: kind, GoName: name}
	}
	for _, name := range []string{"Option", "Result"} {
		k := Key{Name: name}
		s.Bindings[k] = &Symbol{Name: k, Kind: Type, GoName: name}
	}
	for name, goName := range map[string]string{
		"i8": "int8", "i16": "int16", "i32": "int32", "i64": "int64", "isize": "int",
		"u8": "uint8", "u16": "uint16", "u32": "uint32", "u64": "uint64", "usize": "uint",
		"f32": "float32", "f64": "float64",
	} {
		k := Key{Name: name}
		s.Bindings[k] = &Symbol{Name: k, Kind: Type, GoName: goName}
	}
	for _, name := range strings.Fields("Some None Ok Err") {
		k := Key{Name: name}
		s.Bindings[k] = &Symbol{Name: k, Kind: Variant, GoName: name}
	}
	for name, variants := range map[string][]string{"Option": {"Some", "None"}, "Result": {"Ok", "Err"}} {
		members := newScope(MemberScope, nil)
		for _, variant := range variants {
			k := Key{Name: variant}
			members.Bindings[k] = s.Bindings[k]
		}
		s.Bindings[Key{Name: name}].Members = members
	}
	return s
}

func isStatic(s *Symbol) bool {
	switch s.Kind {
	case Type, TypeParam, Enum, Trait, Module, Macro, Import:
		return true
	}
	return false
}

func (r *resolver) define(scope *Scope, node ast.Node, name *ast.Ident, kind Kind) *Symbol {
	if name == nil {
		return nil
	}
	s := &Symbol{Name: r.declarationKey(scope, name), Kind: kind, Decl: r.ref(name), GoName: name.Name}
	r.file.Definitions[name.ID()] = s
	r.file.Visibility[name.ID()] = Private
	r.file.GoNames[name.ID()] = name.Name
	if name.Name == "_" {
		return s
	}
	if old := scope.Bindings[s.Name]; old != nil {
		r.diag(name, "duplicate", fmt.Sprintf("duplicate declaration of %q", name.Name))
		r.introduce(scope, name, old)
		return s
	}
	if scope.Kind == PackageScope && scope.Parent.Bindings[s.Name] != nil {
		r.diag(name, "prelude-conflict", fmt.Sprintf("package declaration %q conflicts with a preimported name", name.Name))
	}
	if kind == Type || kind == Enum || kind == Trait || kind == TypeParam {
		for p := scope; p != nil; p = p.Parent {
			if len(p.Aliases[s.Name]) > 0 {
				r.diag(name, "use-conflict", fmt.Sprintf("type %q conflicts with a use binding", name.Name))
				break
			}
		}
	}
	scope.Bindings[s.Name] = s
	r.introduce(scope, name, s)
	return s
}

func (r *resolver) lookup(scope *Scope, k Key, static bool) Resolution {
	var globs []*UseBinding
	var fallback *Symbol
	for s := scope; s != nil; s = s.Parent {
		if s.Kind == PreludeScope {
			fallback = s.Bindings[k]
			break
		}
		if b := s.Bindings[k]; b != nil && (!static || isStatic(b)) {
			return Resolution{Symbol: b}
		}
		if a := s.Aliases[k]; len(a) > 0 {
			return choices(a)
		}
		globs = append(globs, s.Globs...)
		if r.hygiene != nil {
			if c, ok := r.hygiene.boundaries[s]; ok {
				k.Ctx = c.Definition.Ctx
			}
		}
	}
	// File and package globs compete only after all explicit bindings.
	var candidates []*Symbol
	var deferred []string
	seen := make(map[string]bool)
	for _, u := range globs {
		path := strings.Join(u.Target, "::")
		if seen[path] {
			continue
		}
		seen[path] = true
		if r.importer == nil {
			if deferred == nil {
				deferred = append(append([]string{}, u.Target...), k.Name)
			}
			continue
		}
		for _, e := range r.importer.Lookup(u.Target, k) {
			symbol := externalSymbol(u.Target, e)
			if !static || isStatic(symbol) {
				candidates = append(candidates, symbol)
			}
		}
	}
	if len(candidates) > 0 {
		return choices(candidates)
	}
	if fallback != nil && (!static || isStatic(fallback)) {
		return Resolution{Symbol: fallback}
	}
	if deferred != nil {
		return Resolution{Path: deferred, Deferred: true}
	}
	return Resolution{}
}

func choices(symbols []*Symbol) Resolution {
	var unique []*Symbol
	for _, s := range symbols {
		found := false
		for _, prev := range unique {
			if s == prev {
				found = true
				break
			}
		}
		if !found {
			unique = append(unique, s)
		}
	}
	if len(unique) == 1 {
		return Resolution{Symbol: unique[0], Path: unique[0].Target}
	}
	return Resolution{Candidates: unique}
}

func externalSymbol(path []string, e External) *Symbol {
	return &Symbol{Name: e.Name, Kind: e.Kind, Visibility: e.Visibility, GoName: e.GoName, Target: append(append([]string{}, path...), e.Name.Name)}
}

func (r *resolver) ident(scope *Scope, id *ast.Ident, static bool) Resolution {
	if id == nil {
		return Resolution{}
	}
	scope = r.identScope(scope, id)
	res := r.lookup(scope, key(id), static)
	r.record(id, res)
	return res
}
func (r *resolver) record(n ast.Node, res Resolution) {
	r.file.References[n.ID()] = res
	if len(res.Candidates) > 1 {
		r.diag(n, "ambiguous", "ambiguous name")
	} else if res.Symbol == nil && !res.Deferred {
		r.diag(n, "unresolved", "unresolved name")
	}
}

func (r *resolver) validateIdents() {
	selfRoots := make(map[*ast.Ident]bool)
	ast.Walk(r.file.AST, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.BadItem, *ast.BadStmt, *ast.BadExpr, *ast.BadType, *ast.BadPat:
			return false
		case *ast.Attr:
			return false // Attribute handlers own their contextual vocabulary.
		}
		if p, ok := n.(*ast.Path); ok && len(p.Segments) > 1 && p.Segments[0].Name.Name == "Self" {
			selfRoots[p.Segments[0].Name] = true
		}
		if id, ok := n.(*ast.Ident); ok && !id.Raw && !selfRoots[id] && token.IsHardReserved(id.Name) {
			r.diag(id, "reserved", fmt.Sprintf("reserved word %q requires r#", id.Name))
		}
		return true
	})
}
