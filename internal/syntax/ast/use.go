package ast

// UseLeaves visits the selected paths in source order. Paths borrow the tree's
// nodes; callers creating independent declarations must copy them.
func UseLeaves(tree *UseTree, visit func([]*PathSegment, *UseTree)) {
	var walk func(*UseTree, []*PathSegment)
	walk = func(tree *UseTree, prefix []*PathSegment) {
		segments := append(append([]*PathSegment(nil), prefix...), tree.Path.Segments...)
		if tree.Children != nil {
			for _, child := range tree.Children {
				walk(child, segments)
			}
			return
		}
		if len(prefix) > 0 && len(tree.Path.Segments) == 1 {
			name := tree.Path.Segments[0].Name
			if name.Name == "self" && !name.Raw {
				segments = prefix
			}
		}
		visit(segments, tree)
	}
	walk(tree, nil)
}
