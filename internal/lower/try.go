package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"strconv"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) tryExpr(n *syntax.TryExpr, discard bool) goast.Expr {
	info := e.info.Tries[n]
	if info == nil || info.Input == nil || info.Output == nil {
		e.fail(n, "缺少 ? 检查结果")
		return goast.NewIdent("nil")
	}
	value := e.expr(n.X)
	input := e.fresh("Try")
	e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{input}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{value}})
	e.pending = append(e.pending, e.tryDispatch(n, input, nil, func(value goast.Expr) goast.Stmt {
		return &goast.ReturnStmt{Results: []goast.Expr{value}}
	}))
	if discard {
		return nil
	}
	// Infer the payload type, including foreign private types, after failure returns.
	success := "Some"
	if info.Input.Enum.Name == "Result" {
		success = "Ok"
	}
	output := e.fresh("Ok")
	read := &goast.CallExpr{Fun: e.sumMember("Value" + success), Args: []goast.Expr{input}}
	e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{output}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{read}})
	return output
}

func (e *emitter) tryDispatch(n *syntax.TryExpr, input, output *goast.Ident, failed func(goast.Expr) goast.Stmt) goast.Stmt {
	info := e.info.Tries[n]
	family := info.Input.Enum.Name
	success, failure := "Some", "None"
	successTag, failureTag := "1", "0"
	if family == "Result" {
		success, failure, failureTag = "Ok", "Err", "2"
	}
	read := &goast.CallExpr{Fun: e.sumMember("Value" + success), Args: []goast.Expr{input}}
	var successBody []goast.Stmt
	if output != nil {
		successBody = []goast.Stmt{assign(output, read)}
	}
	var args []goast.Expr
	constructor := "None"
	if failure == "Err" {
		constructor = "NewErr"
		var errValue goast.Expr = &goast.CallExpr{Fun: e.sumMember("ValueErr"), Args: []goast.Expr{input}}
		if conversion := info.Conversion; conversion != nil {
			if len(conversion.Evidence.Cases) != 0 {
				e.fail(n, "? conversion evidence has not selected a complete definition branch")
				return &goast.EmptyStmt{}
			}
			if inferred, ok := e.inferredOperationCall(conversion, []goast.Expr{errValue}, false); ok {
				errValue = inferred
			} else {
				errValue = &goast.CallExpr{Fun: e.operationFunction(conversion), Args: []goast.Expr{errValue}}
			}
		}
		args = []goast.Expr{errValue}
	}
	result := &goast.CallExpr{Fun: enumIndices(e.sumMember(constructor), e.enumArgs(info.Output)), Args: args}
	return &goast.SwitchStmt{
		Tag: &goast.CallExpr{Fun: e.sumMember("Tag" + family), Args: []goast.Expr{input}},
		Body: &goast.BlockStmt{List: []goast.Stmt{
			&goast.CaseClause{List: []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: successTag}}, Body: successBody},
			&goast.CaseClause{List: []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: failureTag}}, Body: []goast.Stmt{failed(result)}},
			&goast.CaseClause{Body: []goast.Stmt{&goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote("goxide: invalid " + family + " at ?")}}}}}},
		}},
	}
}
