package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"strconv"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) tryExpr(n *syntax.TryExpr) goast.Expr {
	info := e.info.Tries[n]
	if info == nil || info.Input == nil || info.Output == nil {
		e.fail(n, "缺少 ? 检查结果")
		return goast.NewIdent("nil")
	}
	value := e.expr(n.X)
	input, output := e.fresh("Try"), e.fresh("Ok")
	e.pending = append(e.pending, varStmt(input, e.instanceType(info.Input), value), varStmt(output, e.goType(info.Value), nil))
	e.pending = append(e.pending, e.tryDispatch(n, input, output, func(value goast.Expr) goast.Stmt {
		return &goast.ReturnStmt{Results: []goast.Expr{value}}
	}))
	return output
}

func (e *emitter) tryDispatch(n *syntax.TryExpr, input, output *goast.Ident, failed func(goast.Expr) goast.Stmt) goast.Stmt {
	info := e.info.Tries[n]
	for _, typ := range info.Input.Args {
		if !e.nameableType(typ) {
			e.fail(n, "? payload type is not nameable: "+typ.String())
			return &goast.EmptyStmt{}
		}
	}
	family := info.Input.Enum.Name
	success, failure := "Some", "None"
	successTag, failureTag := "1", "0"
	if family == "Result" {
		success, failure, failureTag = "Ok", "Err", "2"
	}
	read := &goast.CallExpr{Fun: e.sumMember("Value" + success), Args: []goast.Expr{input}}
	var args []goast.Expr
	constructor := "None"
	if failure == "Err" {
		constructor = "NewErr"
		var errValue goast.Expr = &goast.CallExpr{Fun: e.sumMember("ValueErr"), Args: []goast.Expr{input}}
		if conversion := info.Conversion; conversion != nil {
			var helper goast.Expr = goast.NewIdent(conversion.GoName)
			if len(conversion.Target) > 0 {
				helper = e.external(n, conversion.Target, conversion.GoName, "")
			}
			errValue = &goast.CallExpr{Fun: helper, Args: []goast.Expr{errValue}}
		}
		args = []goast.Expr{errValue}
	}
	result := &goast.CallExpr{Fun: enumIndices(e.sumMember(constructor), e.enumArgs(info.Output)), Args: args}
	return &goast.SwitchStmt{
		Tag: &goast.CallExpr{Fun: e.sumMember("Tag" + family), Args: []goast.Expr{input}},
		Body: &goast.BlockStmt{List: []goast.Stmt{
			&goast.CaseClause{List: []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: successTag}}, Body: []goast.Stmt{assign(output, read)}},
			&goast.CaseClause{List: []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: failureTag}}, Body: []goast.Stmt{failed(result)}},
			&goast.CaseClause{Body: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote("goxide: invalid " + family + " at ?")}}}}}},
		}},
	}
}
