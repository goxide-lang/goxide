package macros

import "go/ast"

// checkCaptureTypePositions checks the template before capture substitution.
// A placeholder is an opaque caller fragment: only its use in an explicitly
// typed template slot is inspected, never the tokens that it will substitute.
// This does not resolve ambiguous calls/conversions or indexing/instantiation,
// and does not replace allocationType's narrower fixed-type/binding policy.
func checkCaptureTypePositions(root ast.Node, captures map[string][]tok, nodeError func(ast.Node, string) error) error {
	if len(captures) == 0 {
		return nil
	}
	c := captureTypeChecker{captures: captures, nodeError: nodeError}
	c.expression(root)
	return c.err
}

type captureTypeChecker struct {
	captures  map[string][]tok
	nodeError func(ast.Node, string) error
	err       error
}

func (c *captureTypeChecker) captured(n ast.Node) bool {
	id, ok := n.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = c.captures[id.Name]
	return ok
}

// expression walks ordinary syntax until a parent field proves a type role.
// Each explicitly handled node visits its children here and stops ast.Inspect
// from visiting them again, including types nested in array length expressions.
func (c *captureTypeChecker) expression(root ast.Node) {
	if root == nil || c.err != nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || c.err != nil || c.captured(n) {
			return false
		}
		switch n := n.(type) {
		case *ast.ValueSpec:
			c.typeExpression(n.Type)
			for _, value := range n.Values {
				c.expression(value)
			}
			return false
		case *ast.Field:
			// Field names are binders, not part of the field's type.
			c.typeExpression(n.Type)
			return false
		case *ast.CompositeLit:
			c.typeExpression(n.Type)
			for _, value := range n.Elts {
				c.expression(value)
			}
			return false
		case *ast.TypeAssertExpr:
			c.expression(n.X)
			c.typeExpression(n.Type) // nil for the guard of a type switch
			return false
		case *ast.TypeSwitchStmt:
			c.expression(n.Init)
			c.expression(n.Assign)
			for _, statement := range n.Body.List {
				clause := statement.(*ast.CaseClause)
				for _, typ := range clause.List {
					c.typeExpression(typ)
				}
				for _, body := range clause.Body {
					c.expression(body)
				}
			}
			return false
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.FuncType, *ast.StructType, *ast.InterfaceType:
			c.typeExpression(n.(ast.Expr))
			return false
		}
		// In particular, CallExpr.Fun, StarExpr.X and ordinary IndexExpr
		// children stay expressions unless a type-owning parent proved otherwise.
		return true
	})
}

// Within a proven type role, selectors, pointers, parentheses, variadics and
// generic type arguments remain types. Array lengths are the expression-role
// exception; function field names are binders and must not be rejected here.
func (c *captureTypeChecker) typeExpression(root ast.Expr) {
	if root == nil || c.err != nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || c.err != nil {
			return false
		}
		if c.captured(n) {
			c.err = c.nodeError(n, "macro type position does not support captured syntax")
			return false
		}
		switch n := n.(type) {
		case *ast.ArrayType:
			c.expression(n.Len)
			c.typeExpression(n.Elt)
			return false
		case *ast.Field:
			c.typeExpression(n.Type)
			return false
		}
		return true
	})
}
