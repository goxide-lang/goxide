package lower

import (
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func applyModuleRoots(file *syntax.File, roots paths.Roots) {
	for _, item := range file.Items {
		use, ok := item.(*syntax.UseDecl)
		if !ok || use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 {
			continue
		}
		segments := use.Tree.Path.Segments
		namespace, ok := roots[segments[0].Name.Name]
		if !ok {
			continue
		}
		consumed := 1
		for consumed < len(segments) {
			child, ok := namespace.Children[segments[consumed].Name.Name]
			if !ok {
				break
			}
			namespace = child
			consumed++
		}
		if namespace.PackagePath == "" {
			continue
		}
		if use.Tree.Alias == nil && !use.Tree.Glob {
			alias := *segments[len(segments)-1].Name
			use.Tree.Alias = &alias
		}
		var mapped []*syntax.PathSegment
		for _, part := range strings.Split(namespace.PackagePath, "/") {
			name := *segments[0].Name
			name.Name = part
			segment := *segments[0]
			segment.Name = &name
			mapped = append(mapped, &segment)
		}
		use.Tree.Path.Segments = append(mapped, segments[consumed:]...)
	}
	var id syntax.NodeID
	syntax.Walk(file, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
}
