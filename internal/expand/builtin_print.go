package expand

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// Print calls keep their identity in a side table: no source spelling can
// impersonate the compiler's formatter or capture its generated call.
func (e *engine) builtin(n ast.Node, path *ast.Path, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID) (ast.Node, bool, error) {
	if path == nil || len(path.Segments) != 1 {
		return nil, false, nil
	}
	name := path.Segments[0].Name.Name
	if name == "cfg" {
		return e.cfg(n, args, kind, file, parent)
	}
	if name == "go_result" || name == "go_result_keep" {
		return e.goResult(n, path, args, kind, file, parent)
	}
	if name != "print" && name != "println" {
		return nil, false, nil
	}
	origin := e.r.Origins[n]
	if origin.File == nil {
		origin = Origin{file, n.Span()}
	}
	if err := e.expansionBudget(origin, origin, parent); err != nil {
		return nil, true, err
	}
	fail := func(err error) (ast.Node, bool, error) {
		return nil, true, &Diagnostic{Code: "format", Message: err.Error(), Call: origin, Definition: origin, Expn: parent}
	}
	if kind != stmtKind {
		return fail(fmt.Errorf("%s! requires a direct statement", name))
	}
	caller := e.lexical[n]
	if caller.File == nil {
		caller = Scope{origin.File, path.Segments[0].Name.Ctx}
	}
	input := e.treeInput(args, origin.File, caller.Ctx)
	ts := []syntaxToken{
		{Token: &token.Token{Kind: token.IDENT, Text: name}, Origin: origin, Scope: caller, Lexical: caller},
		{Tree: &syntaxTree{Delim: ast.Paren, Tokens: input, Origin: origin}, Origin: origin, Scope: caller, Lexical: caller},
	}
	parsed, err := e.parse(ts, exprKind, parent, caller)
	if err != nil {
		return fail(err)
	}
	call := parsed.(*ast.CallExpr)
	for i, arg := range call.Args {
		value, err := e.rewrite(reflect.ValueOf(arg), file, parent)
		if err != nil {
			return nil, true, err
		}
		call.Args[i] = value.Interface().(ast.Expr)
	}
	if e.r.Prints == nil {
		e.r.Prints = make(map[*ast.CallExpr]bool)
	}
	e.r.Prints[call] = name == "println"
	statement := &ast.ExprStmt{Base: ast.Base{Loc: n.Span(), Expn: parent}, X: call}
	block := &ast.Block{Base: ast.Base{Loc: n.Span(), Expn: parent}, List: []ast.Stmt{statement}}
	e.r.Origins[call], e.r.Origins[statement], e.r.Origins[block] = origin, origin, origin
	return block, true, nil
}
