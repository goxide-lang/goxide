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

type matchPayload struct {
	values []goast.Expr
	refs   []goast.Expr
}

func (e *emitter) matchStmts(n *syntax.MatchExpr, target *goast.Ident) []goast.Stmt {
	e.matchDepth++
	defer func() { e.matchDepth-- }()
	info := e.info.Matches[n]
	if info == nil || info.Enum == nil {
		e.fail(n, "match 需要已知 enum 类型")
		return nil
	}
	var owner goast.Expr
	var prefix []goast.Stmt
	var value goast.Expr
	if e.matchBorrows(n) {
		place := n.X
		for {
			paren, ok := place.(*syntax.ParenExpr)
			if !ok {
				break
			}
			place = paren.X
		}
		if ref, ok := place.(*syntax.RefExpr); ok {
			place = ref.X
		}
		old := e.pending
		e.pending = nil
		var address goast.Expr
		if hasFlow(place) {
			// lvalue keeps arrays and selectors in their original storage.
			address = e.lvalue(place)
		} else {
			address = e.expr(place)
		}
		owner = e.fresh("Owner")
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{owner}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: address}}})
		prefix, e.pending = e.pending, old
		value = &goast.StarExpr{X: owner}
	} else {
		prefix, value = e.exprScoped(n.X)
		if info.Reference != check.ValueRef {
			value = &goast.StarExpr{X: value}
		}
	}
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
		if info.Enum.Enum.Builtin {
			clause.List = []goast.Expr{integer(variantTag(group.variant.Name))}
		} else {
			clause.List = []goast.Expr{&goast.StarExpr{X: e.variantType(info.Enum, group.variant)}}
		}
		setup, payload := e.matchPayload(info.Enum, group, tmp, bound, owner)
		clause.Body = append(setup, e.matchArms(info.Enum, group.variant, payload, group.arms, target)...)
		body.List = append(body.List, clause)
	}
	otherwise := &goast.CaseClause{}
	if len(defaults) > 0 {
		otherwise.Body = e.matchArms(info.Enum, nil, matchPayload{}, defaults, target)
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

func (e *emitter) matchBorrows(n *syntax.MatchExpr) bool {
	borrows := false
	for _, arm := range n.Arms {
		syntax.Walk(arm.Pat, func(node syntax.Node) bool {
			if binding, ok := node.(*syntax.BindPat); ok && e.info.RefBindings[binding] != check.ValueRef && binding.Name.Name != "_" {
				borrows = true
			}
			return !borrows
		})
	}
	return borrows
}

// Reference payloads come from the selected object or the original sum slot.
// Value payloads used after a guard retain their value at match entry.
func (e *emitter) matchPayload(instance *check.Instance, group *matchGroup, value, variant, owner goast.Expr) ([]goast.Stmt, matchPayload) {
	count := len(group.variant.Fields)
	payload := matchPayload{values: make([]goast.Expr, count), refs: make([]goast.Expr, count)}
	read, borrow := make([]bool, count), make([]bool, count)
	guarded := false
	for _, arm := range group.arms {
		guarded = guarded || arm.Guard != nil
		e.eachMatchField(group.variant, arm.Pat, func(index int, pat syntax.Pat) {
			if index >= count {
				return
			}
			syntax.Walk(pat, func(node syntax.Node) bool {
				switch n := node.(type) {
				case *syntax.BindPat:
					if n.Name.Name != "_" {
						if e.info.RefBindings[n] != check.ValueRef {
							borrow[index] = true
						} else {
							read[index] = true
						}
					}
				case *syntax.LitPat, *syntax.PathPat:
					read[index] = true
				}
				return true
			})
		})
	}
	var setup []goast.Stmt
	for i, field := range group.variant.Fields {
		var source goast.Expr
		if instance.Enum.Builtin {
			source = &goast.CallExpr{Fun: e.sumMember("Value" + group.variant.Name), Args: []goast.Expr{value}}
		} else {
			source = &goast.SelectorExpr{X: variant, Sel: goast.NewIdent(field.GoName)}
		}
		payload.values[i] = source
		if read[i] && guarded && len(group.arms) > 1 && !instance.Enum.Builtin {
			snapshot := e.fresh("Payload")
			setup = append(setup, &goast.AssignStmt{Lhs: []goast.Expr{snapshot}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{source}})
			payload.values[i] = snapshot
		}
		if borrow[i] {
			var projection goast.Expr = &goast.UnaryExpr{Op: gotoken.AND, X: source}
			if instance.Enum.Builtin {
				projection = &goast.CallExpr{Fun: e.sumMember("Project" + group.variant.Name), Args: []goast.Expr{owner}}
			}
			ref := e.fresh("PayloadRef")
			setup = append(setup, &goast.AssignStmt{Lhs: []goast.Expr{ref}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{projection}})
			payload.refs[i] = ref
		}
	}
	return setup, payload
}

func (e *emitter) eachMatchField(variant *check.Variant, pat syntax.Pat, visit func(int, syntax.Pat)) {
	switch pat := pat.(type) {
	case *syntax.TuplePat:
		for i, field := range pat.Elts {
			visit(i, field)
		}
	case *syntax.StructPat:
		for _, field := range pat.Fields {
			for i, decl := range variant.Fields {
				if decl.Name == field.Name.Name {
					visit(i, field.Pat)
					break
				}
			}
		}
	}
}

func (e *emitter) matchArms(instance *check.Instance, variant *check.Variant, payload matchPayload, arms []*syntax.MatchArm, target *goast.Ident) []goast.Stmt {
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

func (e *emitter) matchPattern(instance *check.Instance, variant *check.Variant, payload matchPayload, pat syntax.Pat) (goast.Expr, []goast.Expr, []goast.Expr) {
	var cond goast.Expr
	var names, values []goast.Expr
	field := func(index int) (goast.Expr, goast.Expr) {
		if index >= len(payload.values) {
			e.fail(pat, "变体模式载荷数量不匹配")
			return goast.NewIdent("_"), goast.NewIdent("_")
		}
		return payload.values[index], payload.refs[index]
	}
	var part func(syntax.Pat, goast.Expr, goast.Expr)
	part = func(p syntax.Pat, value, ref goast.Expr) {
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
				bound := value
				if e.info.RefBindings[p] != check.ValueRef {
					bound = ref
				}
				values = append(values, bound)
			}
			if p.Sub != nil {
				part(p.Sub, value, ref)
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
			value, ref := field(i)
			part(p, value, ref)
		}
	case *syntax.StructPat:
		for _, p := range pat.Fields {
			found := false
			for i, f := range variant.Fields {
				if f.Name == p.Name.Name {
					value, ref := field(i)
					part(p.Pat, value, ref)
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
