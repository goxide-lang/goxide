package resolve

import "github.com/goxide-lang/goxide/internal/syntax/ast"

func AttributeName(a *ast.Attr) string {
	if a == nil || a.Path == nil || len(a.Path.Segments) != 1 {
		return ""
	}
	return a.Path.Segments[0].Name.Name
}
