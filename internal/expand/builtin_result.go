package expand

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// Preserve adapter identity separately from source names, just like printing.
func (e *engine) goResult(n ast.Node, path *ast.Path, args *ast.TokenTree, kind fragmentKind, file *ast.File, parent ast.ExpnID) (ast.Node, bool, error) {
	origin := e.r.Origins[n]
	if origin.File == nil {
		origin = Origin{file, n.Span()}
	}
	if err := e.expansionBudget(origin, origin, parent); err != nil {
		return nil, true, err
	}
	fail := func(message string) (ast.Node, bool, error) {
		return nil, true, &Diagnostic{Code: "go-result", Message: message, Call: origin, Definition: origin, Expn: parent}
	}
	if kind != exprKind && kind != stmtKind {
		return fail("Go result macro requires an expression")
	}
	caller := e.lexical[n]
	if caller.File == nil {
		caller = Scope{origin.File, path.Segments[0].Name.Ctx}
	}
	name := path.Segments[0].Name.Name
	input := e.treeInput(args, origin.File, caller.Ctx)
	parsed, err := e.parse([]syntaxToken{
		{Token: &token.Token{Kind: token.IDENT, Text: name}, Origin: origin, Scope: caller, Lexical: caller},
		{Tree: &syntaxTree{Delim: ast.Paren, Tokens: input, Origin: origin}, Origin: origin, Scope: caller, Lexical: caller},
	}, exprKind, parent, caller)
	if err != nil {
		return fail(fmt.Sprintf("Go result macro: %v", err))
	}
	call := parsed.(*ast.CallExpr)
	if len(call.Args) < 1 || len(call.Args) > 2 || call.Ellipsis {
		return fail("Go result macro expects (call) or (T, call)")
	}
	for index, arg := range call.Args {
		value, err := e.rewrite(reflect.ValueOf(arg), file, parent)
		if err != nil {
			return nil, true, err
		}
		call.Args[index] = value.Interface().(ast.Expr)
	}
	if e.r.GoResults == nil {
		e.r.GoResults = make(map[*ast.CallExpr]bool)
	}
	e.r.GoResults[call] = name == "go_result_keep"
	e.r.Origins[call] = origin
	if kind == exprKind {
		return call, true, nil
	}
	statement := &ast.ExprStmt{Base: ast.Base{Loc: n.Span(), Expn: parent}, X: call}
	block := &ast.Block{Base: ast.Base{Loc: n.Span(), Expn: parent}, List: []ast.Stmt{statement}}
	e.r.Origins[statement], e.r.Origins[block] = origin, origin
	return block, true, nil
}
