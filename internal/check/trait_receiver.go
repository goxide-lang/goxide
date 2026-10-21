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

// A source borrow selects members of its borrowed value. Its Go pointer
// representation is not a second native-pointer implementation candidate.
func (c *checker) receiverTargets(node ast.Expr, target types.Type) (types.Type, types.Type) {
	base := types.Unalias(target)
	if pointer, ok := base.(*types.Pointer); ok {
		base = types.Unalias(pointer.Elem())
		if c.referenceHints != nil {
			source := c.referenceHints.sourceShape(node, map[*resolve.Symbol]bool{})
			if source != nil && source.ref != ValueRef {
				return base, base
			}
		}
	}
	return target, base
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
		q.ref = ValueRef
		if n.Mut {
			q.ref = WriteRef
		} else if p.sourceReadOnlyPlace(n.X, seen) {
			q.ref = ReadRef
		} else if operand, ok := referenceScrutinee(n.X).(*ast.StarExpr); ok {
			if source := p.sourceShape(operand.X, seen); source != nil && source.ref != ValueRef {
				q.ref = ReadRef
			}
		}
		q.elem = p.sourceShape(n.X, seen)
	case *ast.StarExpr:
		return p.sourceDereference(p.sourceShape(n.X, seen))
	case *ast.SelectorExpr:
		base := p.sourceShape(n.X, seen)
		if base != nil && base.typ != nil {
			object, indices, _ := types.LookupFieldOrMethod(base.typ, true, p.c.pkg, n.Sel.Name)
			if _, field := object.(*types.Var); !field {
				return q
			}
			for _, index := range indices {
				base = p.sourceDereference(base)
				structure, ok := base.typ.Underlying().(*types.Struct)
				if !ok || index >= structure.NumFields() {
					break
				}
				base = p.fields(base)[structure.Field(index).Name()]
			}
			if len(indices) != 0 {
				return base
			}
		}
	case *ast.IndexExpr:
		base := p.sourceShape(n.X, seen)
		if signatureType(base) {
			return base
		}
		base = p.sourceDereference(base)
		value := p.element(base)
		if base != nil && base.shared {
			value = p.readValue(value)
		}
		return value
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

// These hints use source declarations before the permission pass has a flow
// environment. They only preserve known read-only projections for lookup;
// place still performs the final addressability and permission checks.
func (p *permissionChecker) sourceReadOnlyPlace(node ast.Expr, seen map[*resolve.Symbol]bool) bool {
	switch n := node.(type) {
	case *ast.ParenExpr:
		return p.sourceReadOnlyPlace(n.X, seen)
	case *ast.StarExpr:
		q := p.sourceShape(n.X, seen)
		return q != nil && (q.ref == ReadRef || q.shared)
	case *ast.SelectorExpr:
		q := p.sourceShape(n.X, seen)
		readOnly := p.sourceReadOnlyPlace(n.X, seen)
		if q == nil || q.typ == nil {
			return readOnly
		}
		object, indices, _ := types.LookupFieldOrMethod(q.typ, true, p.c.pkg, n.Sel.Name)
		if _, field := object.(*types.Var); !field {
			return readOnly
		}
		for _, index := range indices {
			if pointerType(q) {
				readOnly = readOnly || q.ref == ReadRef || q.shared
				q = p.element(q)
			}
			structure, ok := q.typ.Underlying().(*types.Struct)
			if !ok || index >= structure.NumFields() {
				break
			}
			q = p.fields(q)[structure.Field(index).Name()]
		}
		return readOnly
	case *ast.IndexExpr:
		q := p.sourceShape(n.X, seen)
		if q == nil || q.typ == nil {
			return false
		}
		if pointerType(q) {
			return q.ref == ReadRef || q.shared
		}
		if p.c.out.isArray(q.typ) {
			return p.sourceReadOnlyPlace(n.X, seen)
		}
		return q.shared
	}
	return false
}

func (p *permissionChecker) sourceDereference(q *permission) *permission {
	if !pointerType(q) {
		return q
	}
	value := p.element(q)
	if q.ref == ReadRef || q.shared {
		return p.readValue(value)
	}
	return value
}
