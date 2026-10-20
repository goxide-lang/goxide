package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// Enum interfaces must not turn source equality into pointer identity or hide
// known non-comparable payloads behind native interface/map comparisons.
func (c *checker) checkEqualityComparisons() {
	for _, pkg := range c.out.Resolve.Packages {
		c.pkg = c.packages[pkg]
		for _, file := range pkg.Files {
			if file.AST.NativeGo {
				continue
			}
			c.file = file
			mapTypes := map[types.Type]bool{}
			ast.Walk(file.AST, func(node ast.Node) bool {
				if expression, ok := node.(ast.Expr); ok {
					c.checkEnumMapTypes(node, c.out.Types[expression], mapTypes)
				}
				if id, ok := node.(*ast.Ident); ok {
					if symbol := file.Definitions[id.ID()]; symbol != nil {
						c.checkEnumMapTypes(node, c.out.Symbols[symbol], mapTypes)
					}
				}
				switch n := node.(type) {
				case *ast.BinaryExpr:
					if n.Op != token.EQL && n.Op != token.NEQ {
						break
					}
					left, right := c.out.Types[n.X], c.out.Types[n.Y]
					if nilComparisonType(left) || nilComparisonType(right) {
						break
					}
					if c.out.EnumOf(left) != nil && c.out.EnumOf(right) != nil {
						c.diag(n, "enum-comparison", "enum comparison requires an explicit PartialEq implementation and equals call")
					} else if c.nonComparableEnum(left, nil) || c.nonComparableEnum(right, nil) {
						c.diag(n, "enum-comparison", "comparison contains an enum with a non-comparable payload")
					}
				case *ast.SwitchStmt:
					if c.nonComparableEnum(c.out.Types[n.Tag], nil) {
						c.diag(n.Tag, "enum-comparison", "switch comparison contains an enum with a non-comparable payload")
					}
					for _, clause := range n.Cases {
						for _, value := range clause.List {
							if c.nonComparableEnum(c.out.Types[value], nil) {
								c.diag(value, "enum-comparison", "switch case comparison contains an enum with a non-comparable payload")
							}
						}
					}
				case *ast.IndexExpr:
					if mappingType(c.out.Types[n.X]) != nil {
						for _, key := range n.Indices {
							c.checkEnumMapKey(key)
						}
					}
				case *ast.CompositeLit:
					if mappingType(c.out.Types[n]) != nil {
						for _, element := range n.Elts {
							if pair, ok := element.(*ast.KeyValue); ok {
								c.checkEnumMapKey(pair.Key)
							}
						}
					}
				case *ast.CallExpr:
					if symbol := c.symbol(n.Fun); symbol != nil && symbol.Kind == resolve.Builtin && symbol.Name.Name == "delete" && len(n.Args) == 2 {
						c.checkEnumMapKey(n.Args[1])
					}
				}
				return true
			})
		}
	}
}

func nilComparisonType(t types.Type) bool {
	if t == nil {
		return false
	}
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Kind() == types.UntypedNil
}

func mappingType(t types.Type) *types.Map {
	if t == nil {
		return nil
	}
	mapping, _ := t.Underlying().(*types.Map)
	return mapping
}

func (c *checker) checkEnumMapKey(key ast.Expr) {
	if c.nonComparableEnum(c.out.Types[key], nil) {
		c.diag(key, "enum-comparison", "map key contains an enum with a non-comparable payload")
	}
}

// Inspect completed types, including map declarations without a literal. This
// does not rerun source type resolution after the enclosing scope has closed.
func (c *checker) checkEnumMapTypes(node ast.Node, t types.Type, seen map[types.Type]bool) {
	if t == nil || seen[t] {
		return
	}
	seen[t] = true
	visit := func(t types.Type) { c.checkEnumMapTypes(node, t, seen) }
	switch t := t.Underlying().(type) {
	case *types.Map:
		if c.nonComparableEnum(t.Key(), nil) {
			c.diag(node, "enum-comparison", "map key type contains an enum with a non-comparable payload")
		}
		visit(t.Key())
		visit(t.Elem())
	case *types.Pointer:
		visit(t.Elem())
	case *types.Array:
		visit(t.Elem())
	case *types.Slice:
		visit(t.Elem())
	case *types.Chan:
		visit(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			visit(t.Field(i).Type())
		}
	case *types.Signature:
		visit(t.Params())
		visit(t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			visit(t.At(i).Type())
		}
	}
}

// Structs and arrays compare their contained values. Pointers compare their
// addresses and therefore do not inherit the pointee's payload restrictions.
func (c *checker) nonComparableEnum(t types.Type, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	if seen == nil {
		seen = map[types.Type]bool{}
	}
	if instance := c.out.EnumOf(t); instance != nil {
		// Substitution can create a fresh but identical recursive instance.
		for previous := range seen {
			if types.Identical(previous, t) {
				return false
			}
		}
		seen[t] = true
		for _, variant := range instance.Enum.Variants {
			for _, field := range instance.Fields(variant) {
				if !types.Comparable(field.Type) || c.nonComparableEnum(field.Type, seen) {
					return true
				}
			}
		}
		return false
	}
	seen[t] = true
	switch t := t.Underlying().(type) {
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if c.nonComparableEnum(t.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Array:
		return c.nonComparableEnum(t.Elem(), seen)
	}
	return false
}
