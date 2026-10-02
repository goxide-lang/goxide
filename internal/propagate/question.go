package propagate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/enums"
)

// questionFlow describes a single sum check after its operand has been lowered.
// It owns validation, payload extraction and failure conversion, but not operand
// scheduling or the destination assignment. No source expression moves frames.
// A consumer selects the failure exit explicitly instead of rewriting returns
// in generated (or potentially source-authored) syntax.
type questionFlow struct {
	setup           []ast.Stmt
	value           ast.Expr
	failure         ast.Expr
	optionCondition ast.Expr
	success         []ast.Stmt
	resultSubject   ast.Stmt
	resultOK        ast.Expr
	resultErr       ast.Expr
}

type failureExit func(ast.Expr) ast.Stmt

func returnFailure(value ast.Expr) ast.Stmt {
	return &ast.ReturnStmt{Results: []ast.Expr{value}}
}

// emit builds fresh branch containers so choosing an exit never mutates the
// flow's other consumers. The failure value (including From) is evaluated before
// the selected exit; guard pending is therefore not set by a failed conversion.
func (q *questionFlow) emit(exit failureExit) []ast.Stmt {
	out := append([]ast.Stmt(nil), q.setup...)
	failure := exit(q.failure)
	if q.optionCondition != nil {
		out = append(out, &ast.IfStmt{Cond: q.optionCondition, Body: &ast.BlockStmt{List: []ast.Stmt{failure}}})
		return append(out, q.success...)
	}
	dispatch := &ast.TypeSwitchStmt{Assign: q.resultSubject, Body: &ast.BlockStmt{List: []ast.Stmt{
		&ast.CaseClause{List: []ast.Expr{q.resultOK}, Body: append([]ast.Stmt(nil), q.success...)},
		&ast.CaseClause{List: []ast.Expr{q.resultErr}, Body: []ast.Stmt{failure}},
		&ast.CaseClause{Body: parseStatements(`panic("hgo: invalid Result at ?")`)},
	}}}
	return append(out, dispatch)
}

func (l *lowering) questionFlow(n *ast.CallExpr, operand ast.Expr, ft *ast.FuncType, d enums.Decl) (*questionFlow, error) {
	target, e := l.target(ft)
	if e != nil {
		return nil, e
	}
	if target.Family != d.Family {
		return nil, l.fail(n, "Option and Result cannot propagate into each other")
	}
	tmp, val := l.temp(), l.temp()
	q := &questionFlow{
		setup: parseStatements(tmp + " := " + printNode(operand) + "; " + d.Validator + "(" + tmp + "); var " + val + " " + d.TypeArgs[0]),
		value: ast.NewIdent(val),
	}
	if d.Family == "Option" {
		q.optionCondition = parseExpr(tmp + " == " + d.Variants[1].Constructor + "()")
		q.failure = parseExpr(target.Variants[1].Constructor + "()")
		q.success = parseStatements(val + " = " + tmp + ".(" + d.Variants[0].CaseName + ").Value")
		return q, nil
	}
	errVal := "__hgo_failure.Value"
	srcType := l.info.TypeOf(n.Args[0])
	srcNamed, _ := types.Unalias(srcType).(*types.Named)
	targetType := l.info.TypeOf(ft.Results.List[0].Type)
	dstNamed, _ := types.Unalias(targetType).(*types.Named)
	if srcNamed == nil || dstNamed == nil {
		return nil, l.fail(n, "unresolved Result error types")
	}
	se, de := srcNamed.TypeArgs().At(1), dstNamed.TypeArgs().At(1)
	if !types.AssignableTo(se, de) {
		var choices []Conversion
		for _, c := range l.cfg.From {
			if types.Identical(se, l.resolveType(c.Source)) && types.Identical(de, l.resolveType(c.Target)) {
				choices = append(choices, c)
			}
		}
		if len(choices) != 1 {
			return nil, l.fail(n, "Result error requires one unique explicit From conversion; conversion chains are not searched")
		}
		helper := choices[0].Helper
		sig, ok := l.resolveType(helper).(*types.Signature)
		if !ok || sig.Variadic() || sig.Params().Len() != 1 || sig.Results().Len() != 1 || sig.TypeParams().Len() != 0 || !types.Identical(sig.Params().At(0).Type(), se) || !types.Identical(sig.Results().At(0).Type(), de) {
			return nil, l.fail(n, "registered From helper must have the exact static source and target types")
		}
		bound := l.conversions[helper]
		if bound == "" {
			l.serial++
			bound = fmt.Sprintf("__hgo_qfrom_%d", l.serial)
			if l.cfg.Namespace != "" {
				bound = fmt.Sprintf("__hgo_qfrom_%s_%d", l.cfg.Namespace, l.serial)
			}
			l.conversions[helper] = bound
			// Bind the registered package function outside every user local scope.
			l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(bound)}, Values: []ast.Expr{parseExpr(helper)}}}})
		}
		errVal = bound + "(" + errVal + ")"
	}
	q.failure = parseExpr(target.Variants[1].Constructor + "(" + errVal + ")")
	q.resultSubject = &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("__hgo_failure")}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.TypeAssertExpr{X: ast.NewIdent(tmp)}}}
	q.resultOK = parseExpr(d.Variants[0].CaseName)
	q.resultErr = parseExpr(d.Variants[1].CaseName)
	q.success = parseStatements(val + " = __hgo_failure.Value")
	return q, nil
}
