package expand

import "github.com/goxide-lang/goxide/internal/syntax/ast"

func (e *engine) builtin(n ast.Node, path *ast.Path, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID) (ast.Node, bool, error) {
	if path == nil || len(path.Segments) != 1 {
		return nil, false, nil
	}
	switch path.Segments[0].Name.Name {
	case "cfg":
		return e.cfg(n, args, kind, file, parent)
	case "go_result", "go_result_keep":
		return e.goResult(n, path, args, kind, file, parent)
	}
	return nil, false, nil
}
