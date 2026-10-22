package check

import (
	"fmt"
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (c *checker) validateSymbolicInterface(node *ast.InterfaceType, methods []*types.Func, embedded []types.Type) {
	seen := map[string]*types.Func{}
	check := func(method *types.Func) {
		if previous := seen[method.Id()]; previous != nil && c.symbolicLengthConflict(method.Type(), previous.Type(), map[[2]types.Type]bool{}) {
			c.diag(node, GenericConstraint, "symbolic array lengths must agree in interface method "+method.Name())
		}
		seen[method.Id()] = method
	}
	for _, method := range methods {
		check(method)
	}
	for _, typ := range embedded {
		if !valid(typ) {
			continue
		}
		if iface, ok := typ.Underlying().(*types.Interface); ok {
			for index := 0; index < iface.NumMethods(); index++ {
				check(iface.Method(index))
			}
		}
	}
}

// The Go definition validator represents an unresolved array with an unknown
// length. Go deliberately treats that length as a wildcard, so source length
// identity must be checked before entering that representation. These checks
// retain the declaration's rigid N/M identities, including nested arrays.
func (c *checker) validateConstBody(body ast.Node, returns []types.Type) {
	if body == nil || len(c.out.ArrayPatterns) == 0 {
		return
	}
	check := func(node ast.Node, got, want types.Type) {
		if c.symbolicLengthConflict(got, want, map[[2]types.Type]bool{}) {
			c.diag(node, GenericConstraint, fmt.Sprintf("symbolic array lengths must agree: cannot use %s as %s", got, want))
		}
	}
	values := func(expressions []ast.Expr) []types.Type {
		var result []types.Type
		for _, expression := range expressions {
			typ := c.out.Types[expression]
			if tuple, ok := typ.(*types.Tuple); ok {
				result = append(result, tupleTypes(tuple)...)
			} else {
				result = append(result, typ)
			}
		}
		return result
	}
	ast.Walk(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncLit:
			if signature, ok := c.out.Types[n].(*types.Signature); ok {
				c.validateConstBody(n.Body, tupleTypes(signature.Results()))
				if n.Expr != nil && signature.Results().Len() == 1 {
					check(n.Expr, c.out.Types[n.Expr], signature.Results().At(0).Type())
					c.validateConstBody(n.Expr, nil)
				}
			}
			return false
		case *ast.ValueSpec:
			for index, got := range values(n.Values) {
				if index < len(n.Names) {
					check(n, got, c.out.Symbols[c.symbol(n.Names[index])])
				}
			}
		case *ast.LetStmt:
			if n.Type != nil {
				check(n, c.out.Types[n.Value], c.typ(n.Type))
			}
		case *ast.AssignStmt:
			for index, got := range values(n.Rhs) {
				if index < len(n.Lhs) {
					check(n, got, c.out.Types[n.Lhs[index]])
				}
			}
		case *ast.ReturnStmt:
			for index, got := range values(n.Results) {
				if index < len(returns) {
					check(n, got, returns[index])
				}
			}
		case *ast.SendStmt:
			if typ := c.out.Types[n.Chan]; valid(typ) {
				if channel, ok := typ.Underlying().(*types.Chan); ok {
					check(n, c.out.Types[n.Value], channel.Elem())
				}
			}
		case *ast.BinaryExpr:
			check(n, c.out.Types[n.X], c.out.Types[n.Y])
		case *ast.SwitchStmt:
			for _, clause := range n.Cases {
				for _, expression := range clause.List {
					check(expression, c.out.Types[expression], c.out.Types[n.Tag])
				}
			}
		case *ast.TypeAssertExpr:
			if n.Type != nil {
				check(n, c.typ(n.Type), c.out.Types[n.X])
			}
		case *ast.TypeSwitchStmt:
			var assertion *ast.TypeAssertExpr
			switch assignment := n.Assign.(type) {
			case *ast.AssignStmt:
				if len(assignment.Rhs) == 1 {
					assertion, _ = assignment.Rhs[0].(*ast.TypeAssertExpr)
				}
			case *ast.ExprStmt:
				assertion, _ = assignment.X.(*ast.TypeAssertExpr)
			}
			if assertion != nil {
				var previous []types.Type
				for _, clause := range n.Cases {
					for _, target := range clause.Types {
						typ := c.typ(target)
						check(target, typ, c.out.Types[assertion.X])
						for _, earlier := range previous {
							if types.Identical(earlier, typ) {
								c.diag(target, GenericConstraint, "duplicate case in type switch")
								break
							}
						}
						previous = append(previous, typ)
					}
				}
			}
		case *ast.IndexExpr:
			if typ := c.out.Types[n.X]; valid(typ) {
				if mapping, ok := typ.Underlying().(*types.Map); ok && len(n.Indices) == 1 {
					check(n, c.out.Types[n.Indices[0]], mapping.Key())
				}
			}
		case *ast.CompositeLit:
			typ := c.out.Types[n]
			for index, expression := range n.Elts {
				want, key := c.literalType(typ, index, "")
				constructor := c.out.Constructors[n]
				if constructor != nil && index < len(constructor.Enum.Fields(constructor.Variant)) {
					want = constructor.Enum.Fields(constructor.Variant)[index].Type
				}
				if pair, ok := expression.(*ast.KeyValue); ok {
					if key != nil {
						check(pair.Key, c.out.Types[pair.Key], key)
					} else if name, ok := pair.Key.(*ast.Ident); ok {
						want, _ = c.literalType(typ, index, name.Name)
						if constructor != nil {
							for _, field := range constructor.Enum.Fields(constructor.Variant) {
								if field.Name == name.Name {
									want = field.Type
								}
							}
						}
					}
					expression = pair.Value
				}
				check(expression, c.out.Types[expression], want)
			}
		case *ast.CallExpr:
			if constructor := c.out.Constructors[n]; constructor != nil {
				fields := constructor.Enum.Fields(constructor.Variant)
				for index, argument := range n.Args {
					if index < len(fields) {
						check(argument, c.out.Types[argument], fields[index].Type)
					}
				}
				break
			}
			if c.isTypeExpr(n.Fun) {
				if len(n.Args) == 1 {
					check(n, c.out.Types[n.Args[0]], c.out.Types[n])
				}
				break
			}
			signature, _ := c.out.Types[n.Fun].(*types.Signature)
			if instance := c.out.FunctionInstances[n]; instance != nil {
				signature, _ = c.out.Substitute(instance.Signature, instance.Substitution, instance.ConstSubstitution).(*types.Signature)
			}
			if signature != nil {
				for index, got := range values(n.Args) {
					parameter := index
					if signature.Variadic() && parameter >= signature.Params().Len()-1 {
						parameter = signature.Params().Len() - 1
					}
					if parameter < 0 || parameter >= signature.Params().Len() {
						continue
					}
					want := signature.Params().At(parameter).Type()
					if signature.Variadic() && parameter == signature.Params().Len()-1 && !n.Ellipsis {
						want = want.(*types.Slice).Elem()
					}
					check(n, got, want)
				}
			} else if len(n.Args) >= 2 {
				left := c.out.Types[n.Args[0]]
				if valid(left) {
					if mapping, ok := left.Underlying().(*types.Map); ok && calleeName(n.Fun) == "delete" {
						check(n.Args[1], c.out.Types[n.Args[1]], mapping.Key())
					}
					if slice, ok := left.Underlying().(*types.Slice); ok {
						switch calleeName(n.Fun) {
						case "append":
							for _, argument := range n.Args[1:] {
								want := types.Type(slice.Elem())
								if n.Ellipsis {
									want = slice
								}
								check(argument, c.out.Types[argument], want)
							}
						case "copy":
							check(n, c.out.Types[n.Args[1]], slice)
						}
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok != token.ASSIGN {
				break
			}
			typ := c.out.Types[n.X]
			if !valid(typ) {
				break
			}
			var key, value types.Type
			if array, ok := c.out.ArrayOf(typ); ok {
				value = array.Elem
			}
			switch source := typ.Underlying().(type) {
			case *types.Slice:
				value = source.Elem()
			case *types.Map:
				key, value = source.Key(), source.Elem()
			case *types.Chan:
				key = source.Elem()
			}
			check(n, key, c.out.Types[n.Key])
			check(n, value, c.out.Types[n.Value])
		}
		return true
	})
}

func (c *checker) symbolicLengthConflict(left, right types.Type, seen map[[2]types.Type]bool) bool {
	if !valid(left) || !valid(right) || left == right {
		return false
	}
	left, right = types.Unalias(left), types.Unalias(right)
	pair := [2]types.Type{left, right}
	if seen[pair] {
		return false
	}
	seen[pair] = true
	recur := func(a, b types.Type) bool { return c.symbolicLengthConflict(a, b, seen) }
	if a, ok := c.out.ArrayOf(left); ok {
		if b, ok := c.out.ArrayOf(right); ok {
			return (a.Length.Param != nil || b.Length.Param != nil) && a.Length != b.Length || recur(a.Elem, b.Elem)
		}
	}
	if a, ok := left.(*types.Named); ok {
		if b, ok := right.(*types.Named); ok && a.Origin() == b.Origin() {
			for index := 0; index < a.TypeArgs().Len() && index < b.TypeArgs().Len(); index++ {
				if recur(a.TypeArgs().At(index), b.TypeArgs().At(index)) {
					return true
				}
			}
			return false
		}
	}
	if target, ok := right.Underlying().(*types.Interface); ok && !c.out.isArray(right) {
		for index := 0; index < target.NumMethods(); index++ {
			method := target.Method(index)
			if actual, _ := c.lookupGoInteropMember(left, false, method.Name()); actual != nil && recur(actual.Type(), method.Type()) {
				return true
			}
		}
		return false
	}
	switch a := left.Underlying().(type) {
	case *types.Pointer:
		if b, ok := right.Underlying().(*types.Pointer); ok {
			return recur(a.Elem(), b.Elem())
		}
	case *types.Slice:
		if b, ok := right.Underlying().(*types.Slice); ok {
			return recur(a.Elem(), b.Elem())
		}
	case *types.Map:
		if b, ok := right.Underlying().(*types.Map); ok {
			return recur(a.Key(), b.Key()) || recur(a.Elem(), b.Elem())
		}
	case *types.Chan:
		if b, ok := right.Underlying().(*types.Chan); ok {
			return recur(a.Elem(), b.Elem())
		}
	case *types.Signature:
		if b, ok := right.Underlying().(*types.Signature); ok {
			return recur(a.Params(), b.Params()) || recur(a.Results(), b.Results())
		}
	case *types.Tuple:
		if b, ok := right.(*types.Tuple); ok && a.Len() == b.Len() {
			for index := 0; index < a.Len(); index++ {
				if recur(a.At(index).Type(), b.At(index).Type()) {
					return true
				}
			}
		}
	case *types.Struct:
		if b, ok := right.Underlying().(*types.Struct); ok && a.NumFields() == b.NumFields() {
			for index := 0; index < a.NumFields(); index++ {
				if recur(a.Field(index).Type(), b.Field(index).Type()) {
					return true
				}
			}
		}
	}
	return false
}
