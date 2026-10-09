package propagate

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// questionFlow describes a single sum check after its operand has been lowered.
// It owns validation, payload extraction and failure conversion, but not operand
// scheduling or the destination assignment. No source expression moves frames.
// A consumer selects the failure exit explicitly instead of rewriting returns
// in generated (or potentially source-authored) syntax.
type questionFlow struct {
	evidence        *sourcemap.ASTEvidence
	context         sourcemap.ASTContext
	setup           []ast.Stmt
	value           ast.Expr
	failure         ast.Expr
	optionCondition ast.Expr
	success         []ast.Stmt
	resultSubject   ast.Expr
	resultOK        ast.Expr
	resultErr       ast.Expr
}

// A failure exit creates a closed, fresh template and identifies its one value
// slot. Evidence is registered before the existing failure expression is spliced
// into that slot; no generated parent can relabel a reused child.
type failureExit func() (ast.Stmt, *ast.Expr)

func returnFailure(value ast.Expr) ast.Stmt {
	return &ast.ReturnStmt{Results: []ast.Expr{value}}
}
func returnFailureExit() (ast.Stmt, *ast.Expr) {
	r := &ast.ReturnStmt{Results: []ast.Expr{ast.NewIdent("_")}}
	return r, &r.Results[0]
}

func (q *questionFlow) generated(nodes ...ast.Node) {
	if q.evidence != nil {
		for _, n := range nodes {
			q.evidence.Generated(n, q.context)
		}
	}
}
func (q *questionFlow) generatedTree(n ast.Node) {
	if q.evidence != nil {
		q.evidence.GeneratedTree(n, q.context)
	}
}
func (q *questionFlow) expression(source string) ast.Expr {
	x := parseExpr(source)
	q.generatedTree(x)
	return x
}
func (q *questionFlow) statements(source string) []ast.Stmt {
	statements := parseStatements(source)
	for _, statement := range statements {
		q.generatedTree(statement)
	}
	return statements
}

// emit builds fresh branch containers so choosing an exit never mutates the
// flow's other consumers. Each container owns only its own tokens; reused
// conditions, success statements and the failure value retain their identities.
func (q *questionFlow) emit(exit failureExit) []ast.Stmt {
	out := append([]ast.Stmt(nil), q.setup...)
	failure, valueSlot := exit()
	q.generatedTree(failure)
	*valueSlot = q.failure
	if q.optionCondition != nil {
		body := &ast.BlockStmt{List: []ast.Stmt{failure}}
		branch := &ast.IfStmt{Cond: q.optionCondition, Body: body}
		q.generated(body, branch)
		out = append(out, branch)
		return append(out, q.success...)
	}
	success := &ast.CaseClause{List: []ast.Expr{q.resultOK}, Body: append([]ast.Stmt(nil), q.success...)}
	failed := &ast.CaseClause{List: []ast.Expr{q.resultErr}, Body: []ast.Stmt{failure}}
	invalid := &ast.CaseClause{Body: q.statements(`panic("goxide: invalid Result at ?")`)}
	body := &ast.BlockStmt{List: []ast.Stmt{success, failed, invalid}}
	dispatch := &ast.SwitchStmt{Tag: q.resultSubject, Body: body}
	q.generated(success, failed, invalid, body, dispatch)
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
	q := &questionFlow{evidence: l.evidence, value: ast.NewIdent(val)}
	if l.evidence != nil {
		q.context = l.evidence.Context(n.Fun, sourcemap.IdentToken)
	}
	// The marker owns this lowering operation. Its operand may have a different
	// macro context and must remain the exact already-lowered AST child.
	save := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(tmp)}, Tok: token.DEFINE, Rhs: []ast.Expr{ast.NewIdent("_")}}
	q.generatedTree(save)
	save.Rhs[0] = operand
	q.generated(q.value)
	q.setup = append([]ast.Stmt{save}, q.statements(d.Validator+"("+tmp+"); var "+val+" "+d.TypeArgs[0])...)
	if d.Family == "Option" {
		q.optionCondition = q.expression(fmt.Sprintf("%s(%s) == %d", d.Tag, tmp, d.Variants[1].Tag))
		q.failure = q.expression(target.Variants[1].Constructor + "()")
		q.success = q.statements(val + " = " + d.Variants[0].Value + "(" + tmp + ")")
		return q, nil
	}
	errVal := d.Variants[1].Value + "(" + tmp + ")"
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
			bound = fmt.Sprintf("__goxide_qfrom_%d", l.serial)
			if l.cfg.Namespace != "" {
				bound = fmt.Sprintf("__goxide_qfrom_%s_%d", l.cfg.Namespace, l.serial)
			}
			l.conversions[helper] = bound
			// Bind the registered package function outside every user local scope.
			l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(bound)}, Values: []ast.Expr{parseExpr(helper)}}}})
		}
		errVal = bound + "(" + errVal + ")"
	}
	q.failure = q.expression(target.Variants[1].Constructor + "(" + errVal + ")")
	q.resultSubject = q.expression(d.Tag + "(" + tmp + ")")
	q.resultOK = q.expression(fmt.Sprint(d.Variants[0].Tag))
	q.resultErr = q.expression(fmt.Sprint(d.Variants[1].Tag))
	q.success = q.statements(val + " = " + d.Variants[0].Value + "(" + tmp + ")")
	return q, nil
}
