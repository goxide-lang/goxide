package expand

import "github.com/goxide-lang/goxide/internal/syntax/ast"

func (e *engine) splitUse(use *ast.UseDecl) []ast.Item {
	var items []ast.Item
	ast.UseLeaves(use.Tree, func(segments []*ast.PathSegment, leaf *ast.UseTree) {
		declaration, tree, path := *use, *leaf, *use.Tree.Path
		path.Segments = segments
		tree.Path, declaration.Tree = &path, &tree
		if len(items) != 0 {
			declaration.Doc = nil
		}
		for node, original := range map[ast.Node]ast.Node{&declaration: use, &tree: leaf, &path: use.Tree.Path} {
			e.r.Origins[node] = e.r.Origins[original]
			e.r.Locations[node] = e.r.Locations[original]
		}
		items = append(items, e.copyNode(&declaration, 0).(ast.Item))
	})
	return items
}
