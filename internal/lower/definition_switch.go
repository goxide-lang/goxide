package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"strconv"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Unknown Go array lengths compare equal. Validate each source case separately
// so distinct rigid lengths do not become duplicate Go cases. The outer switch
// preserves default, break labels, and termination; the inner switches retain
// assertion legality, case-local variable types, and the ban on fallthrough.
// The checker rejects exact source duplicates before this validation-only AST.
// Headers are already lowered so callers can extract flow prefixes exactly once.
func (e *emitter) validationTypeSwitch(node *syntax.TypeSwitchStmt, init, guard goast.Stmt) goast.Stmt {
	var clauses []*goast.CaseClause
	symbolic := false
	for _, clause := range node.Cases {
		lowered := e.stmt(clause).(*goast.CaseClause)
		clauses = append(clauses, lowered)
		for _, typ := range lowered.List {
			goast.Inspect(typ, func(part goast.Node) bool {
				if ident, ok := part.(*goast.Ident); ok {
					for _, names := range e.validation.parameters {
						symbolic = symbolic || ident.Name == names.array
					}
				}
				return true
			})
		}
		for _, typ := range clause.Types {
			syntax.Walk(typ, func(part syntax.Node) bool {
				if ident, ok := part.(*syntax.Ident); ok {
					if array, ok := e.info.ArrayOf(e.info.Symbols[e.valueSymbol(ident)]); ok {
						symbolic = symbolic || array.Length.Param != nil
					}
				}
				return true
			})
		}
	}
	if !symbolic {
		body := &goast.BlockStmt{Rbrace: e.end(node) - 1}
		for _, clause := range clauses {
			body.List = append(body.List, clause)
		}
		return &goast.TypeSwitchStmt{Switch: e.pos(node), Init: init, Assign: guard, Body: body}
	}

	var binding *syntax.Ident
	if assignment, ok := node.Assign.(*syntax.AssignStmt); ok && len(assignment.Lhs) == 1 {
		binding, _ = assignment.Lhs[0].(*syntax.Ident)
	}
	used := false
	if binding != nil {
		for _, clause := range node.Cases {
			symbol := e.file.Definitions[clause.ID()]
			syntax.Walk(clause, func(part syntax.Node) bool {
				if ident, ok := part.(*syntax.Ident); ok && symbol != nil && e.valueSymbol(ident) == symbol {
					used = true
				}
				return true
			})
		}
	}

	outer := &goast.SwitchStmt{Switch: e.pos(node), Init: init, Tag: &goast.BasicLit{Kind: gotoken.INT, Value: "0"}, Body: &goast.BlockStmt{Rbrace: e.end(node) - 1}}
	for index, clause := range clauses {
		body := clause.Body
		if used {
			body = append([]goast.Stmt{&goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{e.ident(binding)}}}, body...)
		}
		innerClause := &goast.CaseClause{Case: clause.Case, List: clause.List, Colon: clause.Colon, Body: body}
		inner := &goast.TypeSwitchStmt{Switch: e.pos(node), Assign: guard, Body: &goast.BlockStmt{List: []goast.Stmt{innerClause}}}
		var checks []goast.Stmt
		if len(clause.List) > 1 {
			// A multi-type clause binds the interface operand, not any one
			// case type. Check assertions independently, then use default.
			for _, typ := range clause.List {
				checkGuard := guard
				if assignment, ok := checkGuard.(*goast.AssignStmt); ok && len(assignment.Rhs) == 1 {
					checkGuard = &goast.ExprStmt{X: assignment.Rhs[0]}
				}
				checks = append(checks, &goast.TypeSwitchStmt{Switch: e.pos(node), Assign: checkGuard, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.CaseClause{Case: clause.Case, List: []goast.Expr{typ}}}}})
			}
			innerClause.List = nil
		} else if clause.List != nil {
			// An unmatched validation branch must not invent a missing
			// return path for an otherwise terminating source clause.
			inner.Body.List = append(inner.Body.List, &goast.CaseClause{Body: []goast.Stmt{&goast.ForStmt{Body: &goast.BlockStmt{}}}})
		}
		checks = append(checks, inner)
		outerClause := &goast.CaseClause{Case: clause.Case, Colon: clause.Colon, Body: checks}
		if clause.List != nil {
			outerClause.List = []goast.Expr{&goast.BasicLit{Kind: gotoken.INT, Value: strconv.Itoa(index)}}
		}
		outer.Body.List = append(outer.Body.List, outerClause)
	}
	e.anchor(outer, node)
	return outer
}
