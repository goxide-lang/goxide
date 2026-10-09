package propagate

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
)

// Keep the real projector function object for the reference pass's owner and
// signature checks. A function-value alias would erase that evidence. Fresh
// imports and already-bound type arguments prevent caller locals from capturing
// the canonical function without introducing an unregistered forwarding helper.
func (l *lowering) bindProjectionFunction(value string, args []string) (string, error) {
	return l.bindCanonicalEnumExpression(value, args, l.file, false)
}

// The source file owns its import spellings. Companion declarations must never
// resolve a same-spelled import from the consuming file instead.
func (l *lowering) bindCanonicalEnumExpression(value string, args []string, source *ast.File, typeOnly bool) (string, error) {
	expression := parseExpr(value)
	base := expression
	indices := make([]ast.Expr, len(args))
	for i, arg := range args {
		indices[i] = parseExpr(arg)
	}
	switch indexed := expression.(type) {
	case *ast.IndexExpr:
		base = indexed.X
		if len(indices) != 1 {
			return "", fmt.Errorf("projection type argument arity mismatch")
		}
		indexed.Index = indices[0]
	case *ast.IndexListExpr:
		base = indexed.X
		if len(indices) != len(indexed.Indices) {
			return "", fmt.Errorf("projection type argument arity mismatch")
		}
		indexed.Indices = indices
	}
	selector, ok := base.(*ast.SelectorExpr)
	if !ok {
		return printNode(expression), nil
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("unsupported projection function qualifier")
	}
	path := ""
	for _, spec := range source.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := ""
		if spec.Name != nil {
			alias = spec.Name.Name
		} else {
			for _, imported := range l.pkg.Imports() {
				if imported.Path() == p {
					alias = imported.Name()
					break
				}
			}
		}
		if alias == qualifier.Name {
			path = p
			break
		}
	}
	if path == "" {
		return "", fmt.Errorf("projection import is not bound: %s", qualifier.Name)
	}
	if l.projectionImports == nil {
		l.projectionImports = map[string]string{}
	}
	alias := l.projectionImports[path]
	if alias == "" {
		l.serial++
		alias = fmt.Sprintf("__goxide_qprojection_%s_%d", l.cfg.Namespace, l.serial)
		l.projectionImports[path] = alias
		spec := &ast.ImportSpec{Name: ast.NewIdent(alias), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}}
		l.file.Imports = append(l.file.Imports, spec)
		l.file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, l.file.Decls...)
	}
	selector.X = ast.NewIdent(alias)
	// Family registration also runs for value-only uses. Keep this legitimate
	// import used even when no subsequent arm requests a payload projection.
	anchor := &ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("_")}}
	if typeOnly {
		anchor.Type = &ast.StarExpr{X: expression}
	} else {
		anchor.Values = []ast.Expr{expression}
	}
	l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{anchor}})
	return printNode(expression), nil
}
