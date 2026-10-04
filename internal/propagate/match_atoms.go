package propagate

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// matchAtom recognizes compiler-owned syntax, never an arbitrary user closure.
// The source frontend reserves these slot names before constructing the atom.
func matchAtom(n ast.Node) *ast.FuncLit {
	var fn *ast.FuncLit
	switch x := n.(type) {
	case *ast.FuncLit:
		fn = x
	case *ast.CallExpr:
		if len(x.Args) == 0 {
			fn, _ = x.Fun.(*ast.FuncLit)
		}
	}
	if fn == nil || len(fn.Body.List) < 2 {
		return nil
	}
	decl, ok := fn.Body.List[0].(*ast.DeclStmt)
	if !ok {
		return nil
	}
	gen, ok := decl.Decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
		return nil
	}
	slot, ok := gen.Specs[0].(*ast.ValueSpec)
	if !ok || len(slot.Names) != 1 || !strings.HasPrefix(slot.Names[0].Name, "__goxide_match_expr_atom_") {
		return nil
	}
	ret, ok := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	id, ok := ret.Results[0].(*ast.Ident)
	if !ok || id.Name != slot.Names[0].Name {
		return nil
	}
	return fn
}
func (l *lowering) matchAtomExpr(fn *ast.FuncLit, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	var pre []ast.Stmt
	for _, s := range fn.Body.List[:len(fn.Body.List)-1] {
		p, e := l.statement(s, ft)
		if e != nil {
			return nil, nil, e
		}
		pre = append(pre, p...)
	}
	return pre, fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt).Results[0], nil
}

func hasMatchAtom(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if matchAtom(n) != nil {
			found = true
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		return !found
	})
	return found
}

// matchPeerType follows the actual operator tree, rather than evaluating or
// copying a sibling as a type witness. Numeric/logical chains propagate their
// operand identity; comparisons and shift counts end that propagation.
func (l *lowering) matchPeerType(slot *ast.ValueSpec, parents map[ast.Node]ast.Node) types.Type {
	var atom ast.Node = slot
	for atom != nil && matchAtom(atom) == nil {
		atom = parents[atom]
	}
	if atom == nil {
		return nil
	}
	call, ok := parents[atom].(*ast.CallExpr)
	if !ok {
		return nil
	}
	var node ast.Node = call
	category := ""
	for {
		parent := parents[node]
		if _, ok := parent.(*ast.ParenExpr); ok {
			node = parent
			continue
		}
		binary, ok := parent.(*ast.BinaryExpr)
		if !ok {
			return nil
		}
		next := "numeric"
		comparison := false
		switch binary.Op {
		case token.LAND, token.LOR:
			next = "boolean"
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			comparison = true
		}
		if category != "" && next != category && !comparison {
			return nil
		}
		if binary.Op == token.SHL || binary.Op == token.SHR {
			if node == binary.Y {
				return nil
			}
			category = next
			node = binary
			continue
		}
		peer := binary.Y
		if node == binary.Y {
			peer = binary.X
		}
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if types.CheckExpr(l.fs, l.pkg, peer.Pos(), peer, info) == nil {
			t := info.Types[peer].Type
			if t != nil && !pendingMatchType(t) {
				if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsUntyped == 0 {
					if (comparison && b.Info()&(types.IsNumeric|types.IsString|types.IsBoolean) != 0) || (next == "numeric" && b.Info()&(types.IsNumeric|types.IsString) != 0) || (next == "boolean" && b.Info()&types.IsBoolean != 0) {
						return t
					}
				}
			}
		}
		if comparison {
			return nil
		}
		category = next
		node = binary
	}
}
