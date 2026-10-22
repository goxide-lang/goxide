package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type functionFrame struct {
	typ     *goast.FuncType
	source  *syntax.Block
	result  *goast.Ident
	pointer *goast.Ident
}

func (e *emitter) functionBody(typ *goast.FuncType, source *syntax.Block) *goast.BlockStmt {
	if e.validation != nil && e.tree.NativeGo {
		// Native bodies are intentionally absent from the semantic source graph.
		// A terminating signature stub keeps generic native declarations valid in
		// the definition-only checker without pretending to validate their body.
		return &goast.BlockStmt{List: []goast.Stmt{&goast.ForStmt{Body: &goast.BlockStmt{}}}}
	}
	outer, guard := e.functionFrame, e.assignmentGuard
	frame := &functionFrame{typ: typ, source: source}
	e.functionFrame, e.assignmentGuard = frame, nil
	body := e.block(source)
	if body != nil && frame.pointer != nil {
		body.List = append([]goast.Stmt{&goast.AssignStmt{
			Lhs: []goast.Expr{frame.pointer}, Tok: gotoken.DEFINE,
			Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: frame.result}},
		}}, body.List...)
	}
	e.functionFrame, e.assignmentGuard = outer, guard
	return body
}

func (e *emitter) assignmentResult(node syntax.Node) *goast.Ident {
	frame := e.functionFrame
	if frame == nil || frame.typ.Results == nil || len(frame.typ.Results.List) != 1 || len(frame.typ.Results.List[0].Names) > 1 {
		e.fail(node, "assignment propagation requires a single Option or Result return")
		return goast.NewIdent("_")
	}
	if frame.pointer != nil {
		return frame.pointer
	}
	result := frame.typ.Results.List[0]
	if len(result.Names) == 0 {
		syntax.Walk(frame.source, func(node syntax.Node) bool {
			if _, closure := node.(*syntax.FuncLit); closure {
				return false
			}
			if statement, ok := node.(*syntax.ReturnStmt); ok && len(statement.Results) == 0 {
				e.fail(statement, "bare return requires a named source result")
			}
			return true
		})
		result.Names = []*goast.Ident{e.fresh("Result")}
	} else if result.Names[0].Name == "_" {
		result.Names[0] = e.fresh("Result")
	}
	frame.result, frame.pointer = result.Names[0], e.fresh("ResultAddress")
	return frame.pointer
}

func (e *emitter) privatePointerPlace(node syntax.Expr) bool {
	switch n := node.(type) {
	case *syntax.ParenExpr:
		return e.privatePointerPlace(n.X)
	case *syntax.IndexExpr:
		if typ := e.info.Types[n.X]; typ != nil {
			if _, array := typ.Underlying().(*types.Array); array {
				return e.privatePointerPlace(n.X)
			}
		}
	case *syntax.SelectorExpr:
		typ := e.info.Types[n.X]
		if typ == nil {
			return false
		}
		object, indices, _ := types.LookupFieldOrMethod(typ, true, nil, n.Sel.Name)
		if _, field := object.(*types.Var); !field {
			return false
		}
		hidden := false
		for _, index := range indices[:max(0, len(indices)-1)] {
			if pointer, ok := typ.Underlying().(*types.Pointer); ok {
				typ = pointer.Elem()
			}
			field := typ.Underlying().(*types.Struct).Field(index)
			hidden = hidden || !field.Exported() && field.Pkg() != nil && !e.localTypePackage(field.Pkg())
			typ = field.Type()
			if _, pointer := typ.Underlying().(*types.Pointer); pointer && hidden {
				return true
			}
		}
		if _, pointer := e.info.Types[n.X].Underlying().(*types.Pointer); !pointer {
			return e.privatePointerPlace(n.X)
		}
	}
	return false
}

func (e *emitter) framedAssignment(n *syntax.AssignStmt) (goast.Stmt, bool) {
	if n.Tok == token.DEFINE {
		return nil, false
	}
	private := false
	for _, target := range n.Lhs {
		private = private || e.privatePointerPlace(target) || e.privateContext(target)
	}
	if !private {
		return nil, false
	}
	trial := e.firstQuestion(n)
	if trial == nil || trial.Output == nil {
		return nil, false
	}
	return e.withAssignmentFrame(n, trial, func() goast.Stmt {
		lhs, rhs := make([]goast.Expr, len(n.Lhs)), make([]goast.Expr, len(n.Rhs))
		for i, target := range n.Lhs {
			lhs[i] = e.expr(target)
		}
		for i, value := range n.Rhs {
			rhs[i] = e.expr(value)
		}
		return &goast.AssignStmt{Lhs: lhs, Tok: tokenKind(n.Tok), Rhs: rhs, TokPos: e.end(n.Lhs[len(n.Lhs)-1])}
	}), true
}

func (e *emitter) firstQuestion(nodes ...syntax.Node) *check.Try {
	var trial *check.Try
	for _, node := range nodes {
		syntax.Walk(node, func(node syntax.Node) bool {
			if trial != nil {
				return false
			}
			if _, closure := node.(*syntax.FuncLit); closure {
				return false
			}
			if question, ok := node.(*syntax.TryExpr); ok {
				trial = e.info.Tries[question]
			}
			return trial == nil
		})
	}
	return trial
}

func (e *emitter) privateContext(node syntax.Expr) bool {
	if typ := e.info.Types[node]; typ != nil && !e.nameableType(typ) {
		return true
	}
	switch n := node.(type) {
	case *syntax.ParenExpr:
		return e.privateContext(n.X)
	case *syntax.IndexExpr:
		if typ := e.info.Types[n.X]; typ != nil {
			if table, ok := typ.Underlying().(*types.Map); ok && !e.nameableType(table.Key()) {
				return true
			}
		}
	}
	return false
}

func (e *emitter) withAssignmentFrame(n syntax.Node, trial *check.Try, emit func() goast.Stmt) goast.Stmt {
	pointer := e.assignmentResult(n)
	guard := e.fresh("Assignment")
	runtime := e.importName([]string{"github.com", "goxide-lang", "std", "runtime"}, "goxideRuntime", 0)
	guardType := &goast.IndexExpr{X: &goast.SelectorExpr{X: runtime, Sel: goast.NewIdent("Assignment")}, Index: e.instanceType(trial.Output)}
	previous := e.assignmentGuard
	e.assignmentGuard = guard
	statement := emit()
	e.assignmentGuard = previous
	return &goast.BlockStmt{List: []goast.Stmt{
		varStmt(guard, guardType, nil),
		&goast.DeferStmt{Call: &goast.CallExpr{Fun: &goast.SelectorExpr{X: guard, Sel: goast.NewIdent("Catch")}, Args: []goast.Expr{pointer}}},
		statement,
	}}
}

func (e *emitter) framedSwitch(n *syntax.SwitchStmt) (goast.Stmt, bool) {
	if n.Tag == nil || !e.privateContext(n.Tag) {
		return nil, false
	}
	nodes := []syntax.Node{n.Init, n.Tag}
	for _, clause := range n.Cases {
		for _, value := range clause.List {
			nodes = append(nodes, value)
		}
	}
	trial := e.firstQuestion(nodes...)
	if trial == nil || trial.Output == nil {
		return nil, false
	}
	return e.withAssignmentFrame(n, trial, func() goast.Stmt {
		statement := &goast.SwitchStmt{Switch: e.pos(n), Init: e.stmt(n.Init), Tag: e.expr(n.Tag), Body: &goast.BlockStmt{}}
		for _, source := range n.Cases {
			clause := &goast.CaseClause{Case: e.pos(source)}
			for _, value := range source.List {
				clause.List = append(clause.List, e.expr(value))
			}
			guard := e.assignmentGuard
			e.assignmentGuard = nil
			clause.Body = e.stmts(source.Body)
			e.assignmentGuard = guard
			statement.Body.List = append(statement.Body.List, clause)
		}
		return statement
	}), true
}

func (e *emitter) framedRange(n *syntax.RangeStmt) (goast.Stmt, bool) {
	if n.Tok != token.ASSIGN || !e.privateContext(n.Key) {
		return nil, false
	}
	trial := e.firstQuestion(n.X)
	if trial == nil || trial.Output == nil {
		return nil, false
	}
	return e.withAssignmentFrame(n, trial, func() goast.Stmt {
		statement := &goast.RangeStmt{For: e.pos(n), Key: e.expr(n.Key), Value: e.expr(n.Value), Tok: tokenKind(n.Tok), X: e.expr(n.X)}
		guard := e.assignmentGuard
		e.assignmentGuard = nil
		statement.Body = e.block(n.Body)
		e.assignmentGuard = guard
		return statement
	}), true
}

// Preserve the native expression tree; only question checks add a call frame.
func (e *emitter) assignmentExpr(node syntax.Expr) goast.Expr {
	if question, ok := node.(*syntax.TryExpr); ok {
		return e.assignmentTry(question)
	}
	if binary, ok := node.(*syntax.BinaryExpr); ok && (binary.Op == token.SHL || binary.Op == token.SHR) && !e.constant(binary.Y) {
		left, right := e.expr(binary.X), e.expr(binary.Y)
		right = &goast.CallExpr{Fun: e.checkedShiftCount(), Args: []goast.Expr{right}}
		return e.withExprs(node, []syntax.Expr{binary.X, binary.Y}, []goast.Expr{left, right})
	}
	switch node.(type) {
	case *syntax.Ident, *syntax.PathExpr, *syntax.BasicLit, *syntax.TypeExpr, *syntax.FuncLit:
		return e.rawExpr(node)
	}
	if call, ok := node.(*syntax.CallExpr); ok && e.info.GoResults[call] != nil {
		return e.rawExpr(node)
	}
	var children []syntax.Expr
	syntax.Walk(node, func(child syntax.Node) bool {
		if child == node {
			return true
		}
		if expr, ok := child.(syntax.Expr); ok {
			children = append(children, expr)
			return false
		}
		return true
	})
	values := make([]goast.Expr, len(children))
	for i, child := range children {
		values[i] = e.expr(child)
	}
	return e.withExprs(node, children, values)
}

func (e *emitter) checkedShiftCount() *goast.Ident {
	if e.shiftCount != nil {
		return e.shiftCount
	}
	hash := sha256.Sum256([]byte(e.name))
	e.shiftCount = e.fresh(fmt.Sprintf("Shift%x_", hash[:4]))
	var constraint goast.Expr
	for _, name := range []string{"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr"} {
		term := &goast.UnaryExpr{Op: gotoken.TILDE, X: e.primitiveAlias(name)}
		if constraint == nil {
			constraint = term
		} else {
			constraint = &goast.BinaryExpr{X: constraint, Op: gotoken.OR, Y: term}
		}
	}
	count := goast.NewIdent("count")
	e.flowHelpers = append(e.flowHelpers, &goast.FuncDecl{Name: e.shiftCount,
		Type: &goast.FuncType{
			TypeParams: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{goast.NewIdent("N")}, Type: constraint}}},
			Params:     &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{count}, Type: goast.NewIdent("N")}}},
			Results:    &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent("N")}}},
		},
		Body: &goast.BlockStmt{List: []goast.Stmt{
			assign(goast.NewIdent("_"), &goast.BinaryExpr{X: &goast.CallExpr{Fun: e.primitiveAlias("uint"), Args: []goast.Expr{integer(0)}}, Op: gotoken.SHL, Y: count}),
			&goast.ReturnStmt{Results: []goast.Expr{count}},
		}},
	})
	return e.shiftCount
}

func (e *emitter) assignmentTry(n *syntax.TryExpr) goast.Expr {
	info := e.info.Tries[n]
	if info == nil || info.Input == nil || info.Output == nil {
		e.fail(n, "缺少 ? 检查结果")
		return goast.NewIdent("nil")
	}
	input, output := e.fresh("Try"), e.fresh("Ok")
	body := []goast.Stmt{varStmt(output, e.goType(info.Value), nil)}
	body = append(body, e.tryDispatch(n, input, output, func(value goast.Expr) goast.Stmt {
		return &goast.ExprStmt{X: &goast.CallExpr{Fun: &goast.SelectorExpr{X: e.assignmentGuard, Sel: goast.NewIdent("Fail")}, Args: []goast.Expr{value}}}
	}))
	body = append(body, &goast.ReturnStmt{Results: []goast.Expr{output}})
	function := &goast.FuncLit{Type: &goast.FuncType{
		Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{input}, Type: e.instanceType(info.Input)}}},
		Results: &goast.FieldList{List: []*goast.Field{{Type: e.goType(info.Value)}}},
	}, Body: &goast.BlockStmt{List: body}}
	return &goast.CallExpr{Fun: function, Args: []goast.Expr{e.expr(n.X)}}
}
