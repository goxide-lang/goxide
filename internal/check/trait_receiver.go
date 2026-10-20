package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type sourceExpression struct {
	file *resolve.File
	node ast.Expr
}

// Member lookup needs the source reference mode before Go erases & and &mut.
// This reads declaration shapes only; the flow-sensitive permission pass still
// checks every selected call, storage location and captured function value.
func (c *checker) receiverModes(node ast.Expr, target types.Type) []RefMode {
	if _, pointer := types.Unalias(target).(*types.Pointer); !pointer {
		return []RefMode{ValueRef, ReadRef, WriteRef}
	}
	if c.referenceHints != nil {
		q := c.referenceHints.sourceShape(node, map[*resolve.Symbol]bool{})
		if q != nil && (q.ref == ReadRef || q.shared) {
			return []RefMode{ReadRef, ValueRef, WriteRef}
		}
	}
	return []RefMode{WriteRef, ReadRef, ValueRef}
}

func (p *permissionChecker) sourceShape(node ast.Expr, seen map[*resolve.Symbol]bool) *permission {
	if node == nil {
		return nil
	}
	q := p.fromGo(p.c.out.Types[node])
	if selection := p.c.out.Members[node]; selection != nil && selection.Member.Decl != nil {
		copy := *selection
		copy.Receiver = nil
		return p.traitMember(node, &copy)
	}
	switch n := node.(type) {
	case *ast.Ident, *ast.PathExpr:
		s := p.c.symbol(node)
		if s == nil || seen[s] {
			return q
		}
		seen[s] = true
		defer delete(seen, s)
		if mode, ok := p.c.referenceModes[s]; ok {
			q.ref = mode
			return q
		}
		if d := p.c.sourceTypes[s]; d.node != nil {
			old := p.c.file
			p.c.file = d.file
			q = p.astType(d.node, nil)
			p.c.file = old
			return q
		}
		if d := p.c.sourceValues[s]; d.node != nil {
			old := p.c.file
			p.c.file = d.file
			q = p.sourceShape(d.node, seen)
			p.c.file = old
			return q
		}
		if f := p.functions[s]; f != nil {
			return f.typ
		}
	case *ast.ParenExpr:
		return p.sourceShape(n.X, seen)
	case *ast.RefExpr:
		q.ref = WriteRef
		q.elem = p.sourceShape(n.X, seen)
	case *ast.StarExpr:
		return p.element(p.sourceShape(n.X, seen))
	case *ast.SelectorExpr:
		base := p.sourceShape(n.X, seen)
		if pointerType(base) {
			base = p.element(base)
		}
		if field := p.fields(base)[n.Sel.Name]; field != nil {
			return field
		}
	case *ast.IndexExpr:
		base := p.sourceShape(n.X, seen)
		if signatureType(base) {
			return base
		}
		return p.element(base)
	case *ast.CallExpr:
		fun := p.sourceShape(n.Fun, seen)
		if fun != nil && len(fun.results) == 1 {
			return fun.results[0]
		}
	case *ast.TypeAssertExpr:
		return p.astType(n.Type, nil)
	case *ast.FuncLit:
		q = p.functionType(n.Params, n.Results)
		q.typ = p.c.out.Types[node]
	}
	return q
}
