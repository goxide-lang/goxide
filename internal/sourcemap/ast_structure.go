package sourcemap

import (
	"go/ast"
	"go/token"
	"reflect"
)

type astPair struct{ before, after ast.Node }

type astCorrespondence struct{ pairs []astPair }

// node compares complete syntax by child field roles and semantic scalar
// values. The limited normalizations below describe printer transformations,
// never a search for matching leaves or a choice between candidate owners.
func (c *astCorrespondence) node(before, after ast.Node) bool {
	if nilASTNode(before) || nilASTNode(after) {
		return nilASTNode(before) && nilASTNode(after)
	}
	var left, right []*ast.ParenExpr
	for {
		p, ok := before.(*ast.ParenExpr)
		if !ok {
			break
		}
		left = append(left, p)
		before = p.X
	}
	for {
		p, ok := after.(*ast.ParenExpr)
		if !ok {
			break
		}
		right = append(right, p)
		after = p.X
	}
	if len(left) > 0 || len(right) > 0 {
		if !c.node(before, after) {
			return false
		}
		// A changed chain cannot identify which parenthesis survived. Preserve
		// the fully paired inner tree, but none of this chain's parentheses.
		if len(left) == len(right) {
			for i := range left {
				c.pairs = append(c.pairs, astPair{left[i], right[i]})
			}
		}
		return true
	}
	if reflect.TypeOf(before) != reflect.TypeOf(after) {
		return false
	}
	switch n := before.(type) {
	case *ast.BadExpr, *ast.BadStmt, *ast.BadDecl:
		return false
	case *ast.GenDecl:
		other := after.(*ast.GenDecl)
		if (n.Lparen.IsValid() || len(n.Specs) != 1) != (other.Lparen.IsValid() || len(other.Specs) != 1) {
			return false
		}
	}
	a, b := reflect.ValueOf(before).Elem(), reflect.ValueOf(after).Elem()
	typ := a.Type()
	for i := 0; i < a.NumField(); i++ {
		field := typ.Field(i)
		if ignoredASTField(typ.Name(), field.Name) {
			continue
		}
		if field.Type == reflect.TypeOf(token.NoPos) {
			// These positions encode syntax, not just its physical location.
			if typ.Name() == "CallExpr" && field.Name == "Ellipsis" || typ.Name() == "TypeSpec" && field.Name == "Assign" {
				if (a.Field(i).Int() != 0) != (b.Field(i).Int() != 0) {
					return false
				}
			}
			continue
		}
		if typ.Name() == "FuncType" && (field.Name == "Params" || field.Name == "Results") {
			if emptyASTFields(a.Field(i)) && emptyASTFields(b.Field(i)) {
				continue
			}
		}
		if !c.value(a.Field(i), b.Field(i), typ.Name(), field.Name) {
			return false
		}
	}
	c.pairs = append(c.pairs, astPair{before, after})
	return true
}

func ignoredASTField(node, field string) bool {
	switch field {
	case "Doc", "Comment", "Comments":
		return true
	}
	if node == "Ident" && field == "Obj" {
		return true
	}
	if node == "File" {
		switch field {
		case "Scope", "Imports", "Unresolved", "GoVersion":
			return true
		}
	}
	// A final labelled empty statement may lose its explicit semicolon. Empty
	// statements have no supported token slots, so this never moves its origin.
	return node == "EmptyStmt" && field == "Implicit"
}

func emptyASTFields(v reflect.Value) bool {
	if v.IsNil() {
		return true
	}
	return len(v.Interface().(*ast.FieldList).List) == 0
}

func (c *astCorrespondence) value(a, b reflect.Value, parent, field string) bool {
	if a.Type() != b.Type() {
		return false
	}
	if a.Kind() == reflect.Interface || a.Kind() == reflect.Pointer {
		if a.IsNil() || b.IsNil() {
			return a.IsNil() && b.IsNil()
		}
		if node, ok := a.Interface().(ast.Node); ok {
			other, ok := b.Interface().(ast.Node)
			return ok && c.node(node, other)
		}
		return c.value(a.Elem(), b.Elem(), parent, field)
	}
	if a.Kind() == reflect.Slice {
		var left, right []reflect.Value
		statements := parent == "BlockStmt" && field == "List" || (parent == "CaseClause" || parent == "CommClause") && field == "Body"
		collect := func(v reflect.Value) []reflect.Value {
			var out []reflect.Value
			for i := 0; i < v.Len(); i++ {
				item := v.Index(i)
				if statements {
					if _, empty := item.Interface().(*ast.EmptyStmt); empty {
						continue
					}
				}
				out = append(out, item)
			}
			return out
		}
		left, right = collect(a), collect(b)
		if len(left) != len(right) {
			return false
		}
		for i := range left {
			if !c.value(left[i], right[i], parent, field) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a.Interface(), b.Interface())
}

func nilASTNode(n ast.Node) bool {
	return n == nil || reflect.ValueOf(n).IsNil()
}
