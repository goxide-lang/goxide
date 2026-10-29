package typecheck

import (
	"github.com/goxide-lang/goxide/internal/expand"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

const namespaceRootPrefix = "$goxide$roots"

type namespaceUse struct {
	segments []*syntax.PathSegment
	alias    *syntax.Ident
}

// Prepare separates source roots from canonical Go package identities before
// resolution. Macro paths retain their definition/caller file's static aliases.
func (i *NamespaceImporter) Prepare(expanded *expand.Result) {
	i.sourceUses = map[*syntax.UseDecl]namespaceUse{}
	i.boundUses = map[*syntax.UseDecl]bool{}
	bindings := expand.NewStaticBindings(expanded, func(*syntax.File) string { return "" })
	for _, file := range expanded.Files {
		for _, item := range file.Items {
			use, ok := item.(*syntax.UseDecl)
			if !ok || use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 {
				continue
			}
			segments := use.Tree.Path.Segments
			first := segments[0].Name
			if first.Name == namespaceRootPrefix {
				continue
			}
			if bindings.Bound(file, use) {
				i.boundUses[use] = true
				continue
			}
			_, known := i.roots[first.Name]
			if !known {
				var err error
				_, known, err = i.goNames.Root(first.Name)
				if err != nil && i.err == nil {
					i.err = err
				}
			}
			if !known {
				continue
			}
			i.sourceUses[use] = namespaceUse{segments: segments, alias: use.Tree.Alias}
			if use.Tree.Alias == nil && !use.Tree.Glob {
				original := segments[len(segments)-1].Name
				alias := *original
				use.Tree.Alias = &alias
				expanded.Origins[&alias], expanded.Scopes[&alias] = expanded.Origins[original], expanded.Scopes[original]
				if location, ok := expanded.Locations[original]; ok {
					expanded.Locations[&alias] = location
				}
			}
			name := *first
			name.Name, name.Raw = namespaceRootPrefix, true
			prefix := *segments[0]
			prefix.Name = &name
			expanded.Origins[&name], expanded.Scopes[&name] = expanded.Origins[first], expanded.Scopes[first]
			expanded.Origins[&prefix] = expanded.Origins[segments[0]]
			if location, ok := expanded.Locations[first]; ok {
				expanded.Locations[&name] = location
			}
			if location, ok := expanded.Locations[segments[0]]; ok {
				expanded.Locations[&prefix] = location
			}
			use.Tree.Path.Segments = append([]*syntax.PathSegment{&prefix}, segments...)
		}
		var id syntax.NodeID
		syntax.Walk(file, func(node syntax.Node) bool {
			id++
			node.(interface{ SetID(syntax.NodeID) }).SetID(id)
			return true
		})
	}
}
