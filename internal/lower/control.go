package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (e *emitter) statement(n syntax.Stmt) []goast.Stmt {
	if n == nil {
		return nil
	}
	old := e.pending
	e.pending = nil
	stmt := e.flowStmt(n)
	before := e.pending
	e.pending = old
	if stmt != nil {
		before = append(before, stmt)
	}
	if len(before) > 1 {
		// Generated temporaries must not add goto barriers to ordinary statements.
		declares := false
		switch n := n.(type) {
		case *syntax.AssignStmt:
			declares = n.Tok == token.DEFINE
		case *syntax.DeclStmt:
			declares = true
		}
		if !declares {
			return []goast.Stmt{&goast.BlockStmt{List: before}}
		}
	}
	return before
}

type breakTarget struct {
	depth int
	label *goast.Ident
	used  bool
}

func (e *emitter) withBreak(n syntax.Stmt, label string) goast.Stmt {
	old := e.breakTarget
	target := &breakTarget{label: e.fresh("Break"), depth: e.matchDepth}
	if label != "" {
		target.label = goast.NewIdent(label)
	}
	e.breakTarget = target
	var out goast.Stmt
	switch n := n.(type) {
	case *syntax.ForStmt:
		if hasFlow(n.Init) || hasFlow(n.Cond) || hasFlow(n.Post) {
			out = e.flowFor(n, label)
		} else {
			out = e.stmt(n)
		}
	case *syntax.SwitchStmt:
		flow := hasFlow(n.Init) || hasFlow(n.Tag)
		for _, c := range n.Cases {
			for _, x := range c.List {
				flow = flow || hasFlow(x)
			}
		}
		if flow {
			out = e.flowSwitch(n)
		} else {
			out = e.stmt(n)
		}
	case *syntax.TypeSwitchStmt:
		out = e.flowTypeSwitch(n)
	case *syntax.SelectStmt:
		out = e.flowSelect(n)
	default:
		out = e.stmt(n)
	}
	e.breakTarget = old
	if target.used || label != "" {
		if block, ok := out.(*goast.BlockStmt); ok && len(block.List) > 0 {
			last := len(block.List) - 1
			if _, labeled := block.List[last].(*goast.LabeledStmt); !labeled {
				block.List[last] = &goast.LabeledStmt{Label: target.label, Stmt: block.List[last]}
			}
		} else {
			out = &goast.LabeledStmt{Label: target.label, Stmt: out}
		}
	}
	return out
}

func (e *emitter) flowStmt(n syntax.Stmt) goast.Stmt {
	switch n := n.(type) {
	case *syntax.ExprStmt:
		if match, ok := n.X.(*syntax.MatchExpr); ok {
			e.pending = append(e.pending, e.matchStmts(match, nil)...)
			return nil
		}
		if hasFlow(n.X) {
			value := e.expr(n.X)
			if _, call := value.(*goast.CallExpr); call {
				return &goast.ExprStmt{X: value}
			}
			return assign(goast.NewIdent("_"), value)
		}
	case *syntax.AssignStmt:
		return e.assignment(n)
	case *syntax.DeclStmt:
		if d, ok := n.Decl.(*syntax.VarDecl); ok {
			needed := hasFlow(n)
			for _, spec := range d.Specs {
				for _, v := range spec.Values {
					needed = needed || e.info.EnumOf(e.info.Types[v]) != nil
				}
			}
			if !needed {
				return e.stmt(n)
			}
			for _, spec := range d.Specs {
				values := e.exprs(spec.Values)
				if spec.Type == nil && len(spec.Names) == len(values) {
					if len(values) > 1 {
						for i, v := range values {
							values[i] = e.spill(spec.Values[i], v)
						}
					}
					for i, name := range spec.Names {
						e.pending = append(e.pending, varStmt(e.ident(name), e.goType(e.info.Types[spec.Values[i]]), values[i]))
					}
				} else {
					vs := &goast.ValueSpec{Type: e.typ(spec.Type), Values: values}
					for _, name := range spec.Names {
						vs.Names = append(vs.Names, e.ident(name))
					}
					e.pending = append(e.pending, &goast.DeclStmt{Decl: &goast.GenDecl{Tok: gotoken.VAR, Specs: []goast.Spec{vs}}})
				}
			}
			return nil
		}
	case *syntax.IfStmt:
		if hasFlow(n.Init) || hasFlow(n.Cond) {
			init := e.statement(n.Init)
			before, cond := e.exprScoped(n.Cond)
			init = append(init, before...)
			var otherwise goast.Stmt
			if n.Else != nil {
				otherwise = &goast.BlockStmt{List: e.statement(n.Else)}
			}
			init = append(init, &goast.IfStmt{Cond: cond, Body: e.block(n.Body), Else: otherwise})
			return &goast.BlockStmt{List: init}
		}
	case *syntax.ForStmt, *syntax.RangeStmt, *syntax.SelectStmt, *syntax.TypeSwitchStmt:
		return e.withBreak(n, "")
	case *syntax.BranchStmt:
		if n.Tok == token.BREAK && n.Label == nil && e.breakTarget != nil && e.matchDepth > e.breakTarget.depth {
			e.breakTarget.used = true
			return &goast.BranchStmt{Tok: gotoken.BREAK, Label: e.breakTarget.label}
		}
	case *syntax.SendStmt:
		if hasFlow(n.Value) || hasFlow(n.Chan) {
			v := e.ordered([]syntax.Expr{n.Chan, n.Value})
			return &goast.SendStmt{Chan: v[0], Value: v[1]}
		}
	case *syntax.SwitchStmt:
		return e.withBreak(n, "")
	case *syntax.LabeledStmt:
		switch n.Stmt.(type) {
		case *syntax.ForStmt, *syntax.RangeStmt, *syntax.SwitchStmt, *syntax.TypeSwitchStmt, *syntax.SelectStmt:
			return e.withBreak(n.Stmt, e.ident(n.Label).Name)
		}

	}
	return e.stmt(n)
}

func (e *emitter) assignment(n *syntax.AssignStmt) goast.Stmt {
	flow := false
	for _, x := range n.Lhs {
		flow = flow || hasFlow(x)
	}
	for _, x := range n.Rhs {
		flow = flow || hasFlow(x)
	}
	var lhs []goast.Expr
	for _, x := range n.Lhs {
		if flow && n.Tok != token.DEFINE {
			lhs = append(lhs, e.lvalue(x))
		} else {
			lhs = append(lhs, e.expr(x))
		}
	}
	rhs := e.exprs(n.Rhs)
	if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) {
		typed := false
		for _, x := range n.Rhs {
			typed = typed || e.info.EnumOf(e.info.Types[x]) != nil
		}
		if typed {
			// Typed slots keep inferred enum locals at their source interface type.
			if len(rhs) > 1 {
				for i, x := range rhs {
					rhs[i] = e.spill(n.Rhs[i], x)
				}
			}
			for i, l := range n.Lhs {
				id, ok := l.(*syntax.Ident)
				if ok && e.file.Definitions[id.ID()] != nil && id.Name != "_" {
					e.pending = append(e.pending, varStmt(e.ident(id), e.goType(e.info.Types[n.Rhs[i]]), rhs[i]))
				} else {
					e.pending = append(e.pending, assign(lhs[i], rhs[i]))
				}
			}
			return nil
		}
	}
	return &goast.AssignStmt{Lhs: lhs, Tok: tokenKind(n.Tok), Rhs: rhs, TokPos: e.end(n.Lhs[len(n.Lhs)-1])}
}

func (e *emitter) lvalue(n syntax.Expr) goast.Expr {
	switch n := n.(type) {
	case *syntax.Ident:
		return e.expr(n)
	case *syntax.ParenExpr:
		return &goast.ParenExpr{X: e.lvalue(n.X)}
	case *syntax.IndexExpr:
		x := e.container(n.X, e.expr(n.X))
		indices := e.ordered(n.Indices)
		for i, id := range n.Indices {
			indices[i] = e.spill(id, indices[i])
		}
		if len(indices) == 1 {
			return &goast.IndexExpr{X: x, Index: indices[0]}
		}
	case *syntax.StarExpr:
		return &goast.StarExpr{X: e.spill(n.X, e.expr(n.X))}
	case *syntax.SelectorExpr:
		base := e.expr(n.X)
		if t := e.info.Types[n.X]; t != nil {
			if _, pointer := t.Underlying().(*types.Pointer); pointer {
				return &goast.SelectorExpr{X: e.spill(n.X, base), Sel: e.ident(n.Sel)}
			}
		}
		id := e.fresh("Address")
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{id}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: base}}})
		return &goast.SelectorExpr{X: id, Sel: e.ident(n.Sel)}
	}
	x := e.expr(n)
	id := e.fresh("Address")
	e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{id}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: x}}})
	return &goast.StarExpr{X: id}
}

func (e *emitter) flowFor(n *syntax.ForStmt, label string) goast.Stmt {
	init := e.statement(n.Init)
	before, cond := e.exprScoped(n.Cond)
	body := e.block(n.Body)
	var post goast.Stmt
	if hasFlow(n.Post) {
		postLabel := e.fresh("Post")
		used := rewriteContinue(body, postLabel, label, 0)
		var postBody goast.Stmt = &goast.BlockStmt{List: e.statement(n.Post)}
		if used {
			postBody = &goast.LabeledStmt{Label: postLabel, Stmt: postBody}
		}
		body = &goast.BlockStmt{List: []goast.Stmt{body, postBody}}
	} else {
		post = e.stmt(n.Post)
	}
	if cond != nil {
		before = append(before, &goast.IfStmt{Cond: &goast.UnaryExpr{Op: gotoken.NOT, X: cond}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.BranchStmt{Tok: gotoken.BREAK}}}})
	}
	body.List = append(before, body.List...)
	var loop goast.Stmt = &goast.ForStmt{Post: post, Body: body}
	if label != "" {
		loop = &goast.LabeledStmt{Label: goast.NewIdent(label), Stmt: loop}
	}
	init = append(init, loop)
	return &goast.BlockStmt{List: init}
}

func rewriteContinue(n goast.Node, target *goast.Ident, label string, depth int) bool {
	used := false
	goast.Inspect(n, func(node goast.Node) bool {
		if node == nil {
			return false
		}
		if _, ok := node.(*goast.FuncLit); ok {
			return false
		}
		if node != n {
			switch node.(type) {
			case *goast.ForStmt, *goast.RangeStmt:
				used = rewriteContinue(node, target, label, depth+1) || used
				return false
			}
		}
		if b, ok := node.(*goast.BranchStmt); ok && b.Tok == gotoken.CONTINUE && (b.Label == nil && depth == 0 || b.Label != nil && b.Label.Name == label) {
			b.Tok = gotoken.GOTO
			b.Label = target
			used = true
		}
		return true
	})
	return used
}

func (e *emitter) flowSwitch(n *syntax.SwitchStmt) goast.Stmt {
	list := e.statement(n.Init)
	before, tag := e.exprScoped(n.Tag)
	list = append(list, before...)
	if tag != nil {
		id := e.fresh("Switch")
		list = append(list, varStmt(id, e.goType(e.info.Types[n.Tag]), tag))
		tag = id
	}
	chosen := e.fresh("Case")
	list = append(list, varStmt(chosen, goast.NewIdent("int"), &goast.UnaryExpr{Op: gotoken.SUB, X: &goast.BasicLit{Kind: gotoken.INT, Value: "1"}}))
	var cases []goast.Stmt
	for i, c := range n.Cases {
		index := &goast.BasicLit{Kind: gotoken.INT, Value: fmtInt(i)}
		if c.Default {
			// The default remains a default, preserving terminating switches.
		} else {
			for _, x := range c.List {
				before, value := e.exprScoped(x)
				if tag != nil {
					value = &goast.BinaryExpr{X: tag, Op: gotoken.EQL, Y: value}
				}
				before = append(before, &goast.IfStmt{Cond: value, Body: &goast.BlockStmt{List: []goast.Stmt{assign(chosen, index)}}})
				list = append(list, &goast.IfStmt{Cond: &goast.BinaryExpr{X: chosen, Op: gotoken.LSS, Y: &goast.BasicLit{Kind: gotoken.INT, Value: "0"}}, Body: &goast.BlockStmt{List: before}})
			}
		}
		clause := &goast.CaseClause{Body: e.stmts(c.Body)}
		if !c.Default {
			clause.List = []goast.Expr{index}
		}
		cases = append(cases, clause)
	}

	list = append(list, &goast.SwitchStmt{Tag: chosen, Body: &goast.BlockStmt{List: cases}})
	return &goast.BlockStmt{List: list}
}

func fmtInt(n int) string { return strconv.Itoa(n) }
