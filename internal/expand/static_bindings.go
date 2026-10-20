package expand

import ast "github.com/hxsf/goxide/internal/syntax/ast"

// StaticBindings identifies explicit source bindings before dependency roots are
// routed. packageOf receives original files, including macro definition files.
type StaticBindings struct {
	expanded     *Result
	packageOf    func(*ast.File) string
	aliases      map[*ast.File]map[Key][]*ast.UseDecl
	public       map[string]map[Key][]*ast.UseDecl
	declarations map[string]map[Key]bool
}

func NewStaticBindings(expanded *Result, packageOf func(*ast.File) string) *StaticBindings {
	bindings := &StaticBindings{
		expanded: expanded, packageOf: packageOf,
		aliases:      map[*ast.File]map[Key][]*ast.UseDecl{},
		public:       map[string]map[Key][]*ast.UseDecl{},
		declarations: map[string]map[Key]bool{},
	}
	for _, file := range expanded.Files {
		pkg := packageOf(bindings.owner(file, nil))
		if bindings.declarations[pkg] == nil {
			bindings.declarations[pkg] = map[Key]bool{}
		}
		for _, item := range file.Items {
			collectStaticDeclarations(item, bindings.declarations[pkg])
			use, ok := item.(*ast.UseDecl)
			if !ok || use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 || use.Tree.Glob {
				continue
			}
			segments := use.Tree.Path.Segments
			name := use.Tree.Alias
			if name == nil {
				name = segments[len(segments)-1].Name
			}
			if name.Name == "_" {
				continue
			}
			owner := bindings.owner(file, name)
			if bindings.aliases[owner] == nil {
				bindings.aliases[owner] = map[Key][]*ast.UseDecl{}
			}
			key := Key{Name: name.Name, Ctx: name.Ctx}
			bindings.aliases[owner][key] = append(bindings.aliases[owner][key], use)
			if use.Vis.Kind == ast.VisPub {
				pkg := packageOf(owner)
				if bindings.public[pkg] == nil {
					bindings.public[pkg] = map[Key][]*ast.UseDecl{}
				}
				bindings.public[pkg][key] = append(bindings.public[pkg][key], use)
			}
		}
	}
	return bindings
}

// Bound reports whether a use's first segment names an explicit static binding
// in its lexical scope. A self-named use still selects an external source root.
func (b *StaticBindings) Bound(file *ast.File, use *ast.UseDecl) bool {
	if use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 {
		return false
	}
	first := use.Tree.Path.Segments[0].Name
	scope := Scope{File: b.owner(file, first), Ctx: first.Ctx}
	for {
		key := Key{Name: first.Name, Ctx: scope.Ctx}
		pkg := b.packageOf(scope.File)
		if b.declarations[pkg][key] {
			return true
		}
		for _, candidates := range [][]*ast.UseDecl{b.aliases[scope.File][key], b.public[pkg][key]} {
			for _, other := range candidates {
				if other != use {
					return true
				}
			}
		}
		context, ok := b.expanded.Contexts[scope.Ctx]
		if !ok {
			return false
		}
		scope = context.Definition
	}
}

func (b *StaticBindings) owner(file *ast.File, name *ast.Ident) *ast.File {
	if name != nil {
		if scope := b.expanded.Scopes[name]; scope.File != nil {
			return scope.File
		}
	}
	if origin := b.expanded.Origins[file]; origin.File != nil {
		return origin.File
	}
	return file
}

func collectStaticDeclarations(item ast.Item, names map[Key]bool) {
	add := func(name *ast.Ident) {
		if name != nil {
			names[Key{Name: name.Name, Ctx: name.Ctx}] = true
		}
	}
	switch item := item.(type) {
	case *ast.TypeDecl:
		add(item.Name)
		for _, spec := range item.Specs {
			add(spec.Name)
		}
	case *ast.EnumDecl:
		add(item.Name)
	case *ast.TraitDecl:
		add(item.Name)
	case *ast.ModDecl:
		add(item.Name)
	case *ast.MacroRulesDecl:
		add(item.Name)
	case *ast.DeclareDecl:
		collectStaticDeclarations(item.Decl, names)
	}
}
