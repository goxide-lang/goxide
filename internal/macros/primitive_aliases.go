package macros

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

func (e *engine) primitiveAliases() map[string]string {
	aliases := map[string]string{}
	for key, alias := range e.aliases {
		if name, ok := strings.CutPrefix(key, "type "); ok && primitiveName(name) {
			aliases[alias] = name
		}
	}
	return aliases
}

// ResolvePrimitiveAliases restores a predeclared type spelling only where Go
// proves that it resolves to the same universe object. The temporary probe does
// not change user declarations or captured expressions. Unknown bindings keep
// their definition-site alias, including when another lowering stage has not
// yet supplied enough information for type checking.
//
// aliases contains only compiler-owned identities recorded by the expander.
// Removed declarations are also deleted from aliases. Identifier positions are
// retained so the frontend can continue using its original AST source evidence.
func ResolvePrimitiveAliases(packagePath string, fs *token.FileSet, f *ast.File, aliases map[string]string, imp types.Importer) {
	if len(aliases) == 0 {
		return
	}
	declarations := map[*ast.Ident]bool{}
	for _, decl := range f.Decls {
		if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.TYPE {
			for _, spec := range group.Specs {
				spec := spec.(*ast.TypeSpec)
				if aliases[spec.Name.Name] != "" {
					declarations[spec.Name] = true
				}
			}
		}
	}
	before := map[*ast.Ident]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok && !declarations[id] && aliases[id.Name] != "" {
			before[id] = id.Name
			id.Name = aliases[id.Name]
		}
		return true
	})
	if len(before) != 0 {
		if imp == nil {
			imp = importer.Default()
		}
		if packagePath == "" {
			packagePath = "macro/primitive/bindings"
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		_, _ = conf.Check(packagePath, fs, []*ast.File{f}, info)
		for id, alias := range before {
			if info.Uses[id] != types.Universe.Lookup(aliases[alias]) {
				id.Name = alias
			}
		}
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && !declarations[id] && aliases[id.Name] != "" {
			used[id.Name] = true
		}
		return true
	})
	var decls []ast.Decl
	for _, decl := range f.Decls {
		if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.TYPE {
			var specs []ast.Spec
			for _, spec := range group.Specs {
				name := spec.(*ast.TypeSpec).Name.Name
				if aliases[name] != "" && !used[name] {
					delete(aliases, name)
					continue
				}
				specs = append(specs, spec)
			}
			group.Specs = specs
			if len(specs) == 0 {
				continue
			}
		}
		decls = append(decls, decl)
	}
	f.Decls = decls
}

// Go-shaped files can finish during expansion. DSL files retain their aliases
// and use the same resolver after the frontend has lowered them to a Go AST.
func (e *engine) finishPrimitiveAliases(ts []tok, aliases map[string]string) []tok {
	if len(aliases) == 0 {
		return ts
	}
	var source strings.Builder
	source.WriteString(render(ts))
	names := make([]string, 0, len(aliases))
	for alias := range aliases {
		names = append(names, alias)
	}
	sort.Strings(names)
	for _, alias := range names {
		source.WriteString("\ntype " + alias + " = " + aliases[alias] + "\n")
	}
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, e.file, source.String(), 0)
	if err != nil {
		return ts
	}
	before := map[*ast.Ident]string{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && aliases[id.Name] != "" {
			before[id] = id.Name
		}
		return true
	})
	packagePath := ""
	if e.pkg != nil {
		packagePath = e.pkg.path
	}
	ResolvePrimitiveAliases(packagePath, fs, f, aliases, e.importer)
	edits := map[int]string{}
	for id, old := range before {
		if id.Name != old {
			edits[fs.Position(id.Pos()).Offset] = id.Name
		}
	}
	offset := 0
	for i := range ts {
		offset += len(ts[i].before)
		old := ts[i].text
		if name := edits[offset]; name != "" {
			ts[i].text = name
		}
		offset += len(old)
	}
	for key, alias := range e.aliases {
		if name, ok := strings.CutPrefix(key, "type "); ok && primitiveName(name) && aliases[alias] == "" {
			delete(e.aliases, key)
		}
	}
	return ts
}
