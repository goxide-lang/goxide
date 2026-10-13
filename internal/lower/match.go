package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"strconv"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type matchGroup struct {
	variant *check.Variant
	arms    []*syntax.MatchArm
}

func (e *emitter) matchStmts(n *syntax.MatchExpr, target *goast.Ident) []goast.Stmt {
	e.matchDepth++
	defer func() { e.matchDepth-- }()
	info := e.info.Matches[n]
	if info == nil || info.Enum == nil {
		e.fail(n, "match 需要已知 enum 类型")
		return nil
	}
	prefix, value := e.exprScoped(n.X)
	tmp := e.fresh("match")
	prefix = append(prefix, &goast.DeclStmt{Decl: &goast.GenDecl{Tok: gotoken.VAR, Specs: []goast.Spec{
		&goast.ValueSpec{Names: []*goast.Ident{tmp}, Type: e.instanceType(info.Enum), Values: []goast.Expr{value}},
	}}})
	if !info.Enum.Enum.Builtin {
		prefix = append(prefix, &goast.IfStmt{Cond: &goast.BinaryExpr{X: tmp, Op: gotoken.EQL, Y: goast.NewIdent("nil")},
			Body: &goast.BlockStmt{List: []goast.Stmt{matchPanic(fmt.Sprintf("goxide: match on nil %s at %s:%d", info.Enum.Enum.Name, e.name, n.Span().Start))}},
		})
	}
	var groups []*matchGroup
	byName := make(map[string]*matchGroup)
	var defaults []*syntax.MatchArm
	for _, arm := range n.Arms {
		if _, ok := arm.Pat.(*syntax.WildPat); ok {
			defaults = append(defaults, arm)
			continue
		}
		name := e.matchVariantName(arm.Pat)
		if name == "" {
			e.fail(arm.Pat, "match 分支需要变体模式")
			return nil
		}
		variant := info.Enum.Variant(name)
		if variant == nil {
			e.fail(arm.Pat, "未知 enum 变体 "+name)
			return nil
		}
		group := byName[name]
		if group == nil {
			group = &matchGroup{variant: variant}
			byName[name] = group
			groups = append(groups, group)
		}
	}
	for _, group := range groups {
		for _, arm := range n.Arms {
			_, wild := arm.Pat.(*syntax.WildPat)
			if wild || e.matchVariantName(arm.Pat) == group.variant.Name {
				group.arms = append(group.arms, arm)
			}
		}
	}
	bound := e.fresh("variant")
	body := &goast.BlockStmt{}
	for _, group := range groups {
		clause := &goast.CaseClause{}
		var payload goast.Expr = bound
		if info.Enum.Enum.Builtin {
			clause.List = []goast.Expr{integer(variantTag(group.variant.Name))}
			if len(group.variant.Fields) > 0 {
				payload = &goast.CallExpr{Fun: e.sumMember("Value" + group.variant.Name), Args: []goast.Expr{tmp}}
			}
		} else {
			clause.List = []goast.Expr{&goast.StarExpr{X: e.variantType(info.Enum, group.variant)}}
		}
		clause.Body = e.matchArms(info.Enum, group.variant, payload, group.arms, target)
		body.List = append(body.List, clause)
	}
	otherwise := &goast.CaseClause{}
	if len(defaults) > 0 {
		otherwise.Body = e.matchArms(info.Enum, nil, tmp, defaults, target)
	} else {
		otherwise.Body = []goast.Stmt{matchPanic("goxide: invalid enum " + info.Enum.Enum.Name)}
	}
	if info.Enum.Enum.Builtin {
		tag := e.fresh("tag")
		if len(defaults) > 0 {
			var valid goast.Expr
			for _, variant := range info.Enum.Enum.Variants {
				part := &goast.BinaryExpr{X: tag, Op: gotoken.NEQ, Y: integer(variantTag(variant.Name))}
				valid = matchAnd(valid, part)
			}
			otherwise.Body = append([]goast.Stmt{&goast.IfStmt{Cond: valid, Body: &goast.BlockStmt{List: []goast.Stmt{matchPanic("goxide: invalid enum " + info.Enum.Enum.Name)}}}}, otherwise.Body...)
		}
		body.List = append(body.List, otherwise)
		return append(prefix, &goast.SwitchStmt{
			Init: &goast.AssignStmt{Lhs: []goast.Expr{tag}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.CallExpr{Fun: e.sumMember("Tag" + info.Enum.Enum.Name), Args: []goast.Expr{tmp}}}},
			Tag:  tag, Body: body,
		})
	}
	body.List = append(body.List, otherwise)
	// A unit-only match still needs a bound type switch for the same layout.
	for _, clause := range body.List {
		c := clause.(*goast.CaseClause)
		c.Body = append([]goast.Stmt{&goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{bound}}}, c.Body...)
	}
	return append(prefix, &goast.TypeSwitchStmt{Assign: &goast.AssignStmt{Lhs: []goast.Expr{bound}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.TypeAssertExpr{X: tmp}}}, Body: body})
}

func (e *emitter) matchArms(instance *check.Instance, variant *check.Variant, payload goast.Expr, arms []*syntax.MatchArm, target *goast.Ident) []goast.Stmt {
	if len(arms) == 0 {
		return []goast.Stmt{matchPanic("goxide: non-exhaustive match " + instance.Enum.Name)}
	}
	arm := arms[0]
	cond, names, values := e.matchPattern(instance, variant, payload, arm.Pat)
	var bind *goast.AssignStmt
	if len(names) > 0 {
		bind = &goast.AssignStmt{Lhs: names, Tok: gotoken.DEFINE, Rhs: values}
	}
	body := e.matchArmBody(arm, target)
	if cond == nil && arm.Guard == nil {
		if bind != nil {
			body = append([]goast.Stmt{bind}, body...)
		}
		return body
	}
	next := e.matchArms(instance, variant, payload, arms[1:], target)
	guardPrefix, guard := e.exprScoped(arm.Guard)
	if len(guardPrefix) == 0 {
		condition := matchAnd(cond, guard)
		branch := &goast.IfStmt{Cond: condition, Body: &goast.BlockStmt{List: body}, Else: matchElse(next)}
		if bind != nil {
			branch.Init = bind
		}
		return []goast.Stmt{branch}
	}
	// A guard containing ? must execute in this function and only after its pattern.
	guardBranch := &goast.IfStmt{Cond: guard, Body: &goast.BlockStmt{List: body}, Else: matchElse(next)}
	inside := append(guardPrefix, guardBranch)
	if bind != nil {
		inside = append([]goast.Stmt{bind}, inside...)
	}
	if cond == nil {
		return []goast.Stmt{&goast.BlockStmt{List: inside}}
	}
	return []goast.Stmt{&goast.IfStmt{Cond: cond, Body: &goast.BlockStmt{List: inside}, Else: matchElse(next)}}
}

func (e *emitter) matchArmBody(arm *syntax.MatchArm, target *goast.Ident) []goast.Stmt {
	if arm.Block != nil {
		return e.stmts(arm.Block.List)
	}
	prefix, value := e.exprScoped(arm.Body)
	if value == nil {
		return prefix
	}
	if target != nil {
		return append(prefix, &goast.AssignStmt{Lhs: []goast.Expr{target}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{value}})
	}
	if _, call := value.(*goast.CallExpr); call {
		return append(prefix, &goast.ExprStmt{X: value})
	}
	return append(prefix, &goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{value}})
}

func (e *emitter) matchPattern(instance *check.Instance, variant *check.Variant, payload goast.Expr, pat syntax.Pat) (goast.Expr, []goast.Expr, []goast.Expr) {
	var cond goast.Expr
	var names, values []goast.Expr
	field := func(index int) goast.Expr {
		if instance.Enum.Builtin {
			return payload
		}
		if index >= len(variant.Fields) {
			e.fail(pat, "变体模式载荷数量不匹配")
			return goast.NewIdent("_")
		}
		return &goast.SelectorExpr{X: payload, Sel: goast.NewIdent(variant.Fields[index].GoName)}
	}
	var part func(syntax.Pat, goast.Expr)
	part = func(p syntax.Pat, value goast.Expr) {
		switch p := p.(type) {
		case *syntax.WildPat:
		case *syntax.BindPat:
			symbol := e.file.References[p.Name.ID()].Symbol
			for symbol != nil && symbol.AliasOf != nil {
				symbol = symbol.AliasOf
			}
			if symbol != nil && symbol.Kind == resolve.Constant {
				cond = matchAnd(cond, &goast.BinaryExpr{X: value, Op: gotoken.EQL, Y: e.reference(p.Name)})
				return
			}
			if p.Name.Name != "_" {
				names = append(names, e.ident(p.Name))
				values = append(values, value)
			}
			if p.Sub != nil {
				part(p.Sub, value)
			}
		case *syntax.LitPat:
			cond = matchAnd(cond, &goast.BinaryExpr{X: value, Op: gotoken.EQL, Y: e.expr(p.Value)})
		case *syntax.PathPat:
			cond = matchAnd(cond, &goast.BinaryExpr{X: value, Op: gotoken.EQL, Y: e.path(p.Path)})
		default:
			e.fail(p, "尚未支持的 enum 载荷模式")
		}
	}
	switch pat := pat.(type) {
	case *syntax.PathPat, *syntax.BindPat:
	case *syntax.TuplePat:
		for i, p := range pat.Elts {
			part(p, field(i))
		}
	case *syntax.StructPat:
		for _, p := range pat.Fields {
			found := false
			for i, f := range variant.Fields {
				if f.Name == p.Name.Name {
					part(p.Pat, field(i))
					found = true
					break
				}
			}
			if !found {
				e.fail(p, "未知的 enum 模式字段 "+p.Name.Name)
			}
		}
	}
	return cond, names, values
}

func matchPatternPath(p syntax.Pat) *syntax.Path {
	switch p := p.(type) {
	case *syntax.PathPat:
		return p.Path
	case *syntax.TuplePat:
		return p.Path
	case *syntax.StructPat:
		return p.Path
	}
	return nil
}

func (e *emitter) matchVariantName(p syntax.Pat) string {
	if path := matchPatternPath(p); path != nil && len(path.Segments) > 0 {
		if symbol := e.file.References[path.ID()].Symbol; symbol != nil {
			for symbol.AliasOf != nil {
				symbol = symbol.AliasOf
			}
			if symbol.Kind == resolve.Variant {
				return symbol.Name.Name
			}
		}
		return path.Segments[len(path.Segments)-1].Name.Name
	}
	if pat, ok := p.(*syntax.BindPat); ok {
		symbol := e.file.References[pat.Name.ID()].Symbol
		for symbol != nil && symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		if symbol != nil && (symbol.Kind == resolve.Variant || symbol.Kind == resolve.Builtin && symbol.Name.Name == "None") {
			return symbol.Name.Name
		}
	}
	return ""
}

func matchAnd(left, right goast.Expr) goast.Expr {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &goast.BinaryExpr{X: left, Op: gotoken.LAND, Y: right}
}

func matchElse(stmts []goast.Stmt) goast.Stmt {
	if len(stmts) == 1 {
		if branch, ok := stmts[0].(*goast.IfStmt); ok {
			return branch
		}
	}
	return &goast.BlockStmt{List: stmts}
}

func matchPanic(message string) goast.Stmt {
	return &goast.ExprStmt{X: &goast.CallExpr{Fun: goast.NewIdent("panic"), Args: []goast.Expr{&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote(message)}}}}
}

func integer(value int) goast.Expr {
	return &goast.BasicLit{Kind: gotoken.INT, Value: strconv.Itoa(value)}
}

func variantTag(name string) int {
	switch name {
	case "None":
		return 0
	case "Some", "Ok":
		return 1
	case "Err":
		return 2
	}
	return -1
}
