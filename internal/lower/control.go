package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (e *emitter) statement(n syntax.Stmt) (output []goast.Stmt) {
	defer func() { e.anchorStatements(output, n) }()
	if n == nil {
		return nil
	}
	if out, ok := e.printStatement(n); ok {
		return out
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

func (e *emitter) withBreak(n syntax.Stmt, label *syntax.Ident) goast.Stmt {
	old := e.breakTarget
	target := &breakTarget{label: e.fresh("Break"), depth: e.matchDepth}
	e.breakTarget = target
	pending := e.pending
	e.pending = nil
	var out goast.Stmt
	switch n := n.(type) {
	case *syntax.ForStmt:
		if hasFlow(n.Init) || hasFlow(n.Cond) || hasFlow(n.Post) {
			out = e.flowFor(n)
		} else {
			out = e.stmt(n)
		}
	case *syntax.RangeStmt:
		if framed, ok := e.framedRange(n); ok {
			out = framed
			break
		}
		out = e.stmt(n)
		if hasFlow(n.X) && e.info.Types[n.X] != nil {
			if _, array := e.info.Types[n.X].Underlying().(*types.Array); array {
				// Hoisting ? must not let Go skip a formerly evaluated array
				// expression just because its length is now constant.
				rangeStmt := out.(*goast.RangeStmt)
				rangeStmt.X = e.spill(n.X, rangeStmt.X)
			}
		}
	case *syntax.SwitchStmt:
		if framed, ok := e.framedSwitch(n); ok {
			out = framed
			break
		}
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
	if len(e.pending) > 0 {
		out = &goast.BlockStmt{List: append(e.pending, out)}
	}
	e.pending = pending
	if label != nil {
		name := e.ident(label).Name
		if _, block := out.(*goast.BlockStmt); !block {
			target.label.Name = name
		}
		goast.Inspect(out, func(node goast.Node) bool {
			if _, ok := node.(*goast.FuncLit); ok {
				return false
			}
			if branch, ok := node.(*goast.BranchStmt); ok && branch.Label != nil && branch.Label.Name == name && (branch.Tok == gotoken.BREAK || branch.Tok == gotoken.CONTINUE) {
				branch.Label = target.label
				target.used = true
			}
			return true
		})
	}
	if target.used {
		if block, ok := out.(*goast.BlockStmt); ok && len(block.List) > 0 {
			last := len(block.List) - 1
			if _, labeled := block.List[last].(*goast.LabeledStmt); !labeled {
				block.List[last] = &goast.LabeledStmt{Label: target.label, Stmt: block.List[last]}
			}
		} else {
			out = &goast.LabeledStmt{Label: target.label, Stmt: out}
		}
	}
	if label != nil {
		used, jump := false, false
		symbol := e.file.Definitions[label.ID()]
		syntax.Walk(e.tree, func(node syntax.Node) bool {
			if branch, ok := node.(*syntax.BranchStmt); ok && branch.Label != nil && symbol != nil && e.file.References[branch.Label.ID()].Symbol == symbol {
				used = true
				jump = jump || branch.Tok == token.GOTO
			}
			return true
		})
		if jump || !used {
			// A goto re-enters the complete source statement, including its operands.
			out = &goast.LabeledStmt{Label: e.ident(label), Stmt: out}
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
		return e.withBreak(n, nil)
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
		return e.withBreak(n, nil)
	case *syntax.LabeledStmt:
		switch n.Stmt.(type) {
		case *syntax.ForStmt, *syntax.RangeStmt, *syntax.SwitchStmt, *syntax.TypeSwitchStmt, *syntax.SelectStmt:
			return e.withBreak(n.Stmt, n.Label)
		}

	}
	return e.stmt(n)
}

func (e *emitter) assignment(n *syntax.AssignStmt) goast.Stmt {
	if framed, ok := e.framedAssignment(n); ok {
		return framed
	}
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
		var x goast.Expr
		if t := e.info.Types[n.X]; t != nil {
			if _, array := t.Underlying().(*types.Array); array {
				// An array is storage, not an operand value. Its final bounds and
				// pointer checks belong to the assignment's write phase.
				x = e.lvalue(n.X)
			}
		}
		if x == nil {
			x = e.spill(n.X, e.expr(n.X))
		}
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
		return e.selectorPlace(n)
	}
	x := e.expr(n)
	id := e.fresh("Address")
	e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{id}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: x}}})
	return &goast.StarExpr{X: id}
}

func (e *emitter) selectorPlace(n *syntax.SelectorExpr) goast.Expr {
	typ := e.info.Types[n.X]
	var base goast.Expr
	if typ != nil {
		if _, pointer := typ.Underlying().(*types.Pointer); pointer {
			base = e.spill(n.X, e.expr(n.X))
		}
	}
	if base == nil {
		base = e.lvalue(n.X)
	}
	if typ == nil {
		return &goast.SelectorExpr{X: base, Sel: e.ident(n.Sel)}
	}
	owner := typ
	if pointer, ok := owner.Underlying().(*types.Pointer); ok {
		owner = pointer.Elem()
	}
	var pkg *types.Package
	if named, ok := types.Unalias(owner).(*types.Named); ok {
		pkg = named.Obj().Pkg()
	} else if structure, ok := owner.Underlying().(*types.Struct); ok && structure.NumFields() > 0 {
		pkg = structure.Field(0).Pkg()
	}
	_, indices, _ := types.LookupFieldOrMethod(typ, true, pkg, n.Sel.Name)
	for _, index := range indices[:max(0, len(indices)-1)] {
		if pointer, ok := typ.Underlying().(*types.Pointer); ok {
			typ = pointer.Elem()
		}
		field := typ.Underlying().(*types.Struct).Field(index)
		base = &goast.SelectorExpr{X: base, Sel: goast.NewIdent(field.Name())}
		typ = field.Type()
		if _, pointer := typ.Underlying().(*types.Pointer); pointer {
			// Snapshot promoted pointer fields before the RHS can replace them.
			id := e.fresh("Value")
			e.pending = append(e.pending, varStmt(id, e.goType(typ), base))
			base = id
		}
	}
	return &goast.SelectorExpr{X: base, Sel: e.ident(n.Sel)}
}

func (e *emitter) flowFor(n *syntax.ForStmt) goast.Stmt {
	init := e.statement(n.Init)
	before, cond := e.exprScoped(n.Cond)
	body := e.block(n.Body)
	var loopInit goast.Stmt
	if source, ok := n.Init.(*syntax.AssignStmt); ok && source.Tok == token.DEFINE {
		if last, ok := init[len(init)-1].(*goast.AssignStmt); ok && last.Tok == gotoken.DEFINE {
			loopInit, init = last, init[:len(init)-1]
		} else {
			// Keep the variables in a native for initializer so Go creates the
			// next iteration's variables before executing its post statement.
			var names []goast.Expr
			for _, lhs := range source.Lhs {
				if id, ok := lhs.(*syntax.Ident); ok && id.Name != "_" {
					names = append(names, e.ident(id))
				}
			}
			loopInit = &goast.AssignStmt{Lhs: names, Tok: gotoken.DEFINE, Rhs: names}
		}
	}
	var post goast.Stmt
	if hasFlow(n.Post) {
		first := e.fresh("First")
		init = append(init, varStmt(first, goast.NewIdent("bool"), goast.NewIdent("true")))
		postBody := &goast.IfStmt{Cond: first, Body: &goast.BlockStmt{List: []goast.Stmt{assign(first, goast.NewIdent("false"))}}, Else: &goast.BlockStmt{List: e.statement(n.Post)}}
		before = append([]goast.Stmt{postBody}, before...)
	} else {
		post = e.stmt(n.Post)
	}
	if cond != nil {
		before = append(before, &goast.IfStmt{Cond: &goast.UnaryExpr{Op: gotoken.NOT, X: cond}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.BranchStmt{Tok: gotoken.BREAK}}}})
	}
	body.List = append(before, body.List...)
	var loop goast.Stmt = &goast.ForStmt{Init: loopInit, Post: post, Body: body}
	init = append(init, loop)
	return &goast.BlockStmt{List: init}
}

func (e *emitter) flowSwitch(n *syntax.SwitchStmt) goast.Stmt {
	list := e.statement(n.Init)
	before, tag := e.exprScoped(n.Tag)
	list = append(list, before...)
	if tag != nil {
		compared := false
		for _, clause := range n.Cases {
			compared = compared || len(clause.List) > 0
		}
		if compared {
			id := e.fresh("Switch")
			list = append(list, varStmt(id, e.goType(e.info.Types[n.Tag]), tag))
			tag = id
		} else {
			list = append(list, assign(goast.NewIdent("_"), tag))
			tag = nil
		}
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
