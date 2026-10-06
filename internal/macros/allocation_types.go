package macros

import "go/ast"

// allocationType checks only a proven builtin make/new type operand. Captures
// remain opaque caller syntax; accepting them here would add a type fragment.
// The caller supplies definition-site primitive checks and token diagnostics.
func allocationType(x ast.Expr, captures map[string][]tok, primitiveAliases map[string]bool, primitive func(*ast.Ident) error, nodeError func(ast.Node, string) error) error {
	unsupported := func(n ast.Node) error {
		return nodeError(n, "macro allocation requires fixed predeclared type syntax; named/generic types and new(expr) are unsupported")
	}
	switch n := x.(type) {
	case *ast.Ident:
		if _, ok := captures[n.Name]; ok {
			return nodeError(n, "macro allocation type does not support captured types")
		}
		if n.Obj != nil {
			return nodeError(n, "macro allocation type cannot use a template-local binding")
		}
		// A nested expansion can already have checked and aliased a primitive
		// in another definition scope. Trust only the engine's exact alias map,
		// never a generated-looking spelling or the outer definition's binding.
		if primitiveAliases[n.Name] {
			return nil
		}
		if !primitiveName(n.Name) || n.Name == "true" || n.Name == "false" {
			return unsupported(n)
		}
		return primitive(n)
	case *ast.ParenExpr:
		return allocationType(n.X, captures, primitiveAliases, primitive, nodeError)
	case *ast.StarExpr:
		return allocationType(n.X, captures, primitiveAliases, primitive, nodeError)
	case *ast.ArrayType:
		if err := allocationLength(n.Len, captures, primitiveAliases, primitive, nodeError); err != nil {
			return err
		}
		return allocationType(n.Elt, captures, primitiveAliases, primitive, nodeError)
	case *ast.MapType:
		if err := allocationType(n.Key, captures, primitiveAliases, primitive, nodeError); err != nil {
			return err
		}
		return allocationType(n.Value, captures, primitiveAliases, primitive, nodeError)
	case *ast.ChanType:
		return allocationType(n.Value, captures, primitiveAliases, primitive, nodeError)
	default:
		return unsupported(x)
	}
}

// An array length is an expression slot, so a captured expression is legal and
// keeps its caller-owned types. Explicit type syntax in the template's length
// expression is still a type slot: len([2]$T{}) must not admit type captures.
// Constantness, size and ordinary expression validity remain Go's checks.
func allocationLength(x ast.Expr, captures map[string][]tok, primitiveAliases map[string]bool, primitive func(*ast.Ident) error, nodeError func(ast.Node, string) error) error {
	if x == nil {
		return nil // Slice types have no array length.
	}
	var problem error
	// Checking a type recursively also checks its lengths. Do not revisit that
	// same type through the expression walk: nested array literals would double
	// the work at every level. Literal elements and assertion operands still
	// receive their own expression walk.
	checkedTypes := map[ast.Node]bool{}
	checkType := func(t ast.Expr) {
		checkedTypes[t] = true
		problem = allocationType(t, captures, primitiveAliases, primitive, nodeError)
	}
	ast.Inspect(x, func(n ast.Node) bool {
		if problem != nil || checkedTypes[n] {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if _, captured := captures[id.Name]; captured {
				return false
			}
		}
		switch n := n.(type) {
		case *ast.CompositeLit:
			if n.Type != nil {
				checkType(n.Type)
			}
		case *ast.TypeAssertExpr:
			if n.Type != nil {
				checkType(n.Type)
			}
		case *ast.ArrayType, *ast.MapType, *ast.ChanType, *ast.StructType, *ast.InterfaceType, *ast.FuncType:
			checkType(n.(ast.Expr))
			return false
		case *ast.CallExpr:
			// A pointer/parenthesized composite type can be a conversion callee.
			// Named callees remain expression bindings; ordinary hygiene resolves
			// them and Go checks whether the array length is a constant.
			if pointer, ok := ast.Unparen(n.Fun).(*ast.StarExpr); ok {
				checkType(pointer)
			}
		}
		return problem == nil
	})
	return problem
}
