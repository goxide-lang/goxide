package parser

import "github.com/goxide-lang/goxide/internal/syntax/ast"

// FlattenUses normalizes source imports after their attributes are consumed.
// Attribute macros must first receive the complete, original grouped item.
func FlattenUses(items []ast.Item) []ast.Item {
	var result []ast.Item
	for _, item := range items {
		use, ok := item.(*ast.UseDecl)
		if !ok || use.Tree == nil || use.Tree.Children == nil || len(use.Attrs) != 0 {
			result = append(result, item)
			continue
		}
		first := true
		ast.UseLeaves(use.Tree, func(segments []*ast.PathSegment, leaf *ast.UseTree) {
			declaration, tree, path := *use, *leaf, *use.Tree.Path
			path.Segments = nil
			for _, segment := range segments {
				copy, name := *segment, *segment.Name
				copy.Name = &name
				path.Segments = append(path.Segments, &copy)
			}
			if leaf.Alias != nil {
				alias := *leaf.Alias
				tree.Alias = &alias
			}
			tree.Path, declaration.Tree = &path, &tree
			if !first {
				declaration.Doc = nil
			}
			first = false
			result = append(result, &declaration)
		})
	}
	return result
}
