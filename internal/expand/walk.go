package expand

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

var baseType = reflect.TypeOf(ast.Base{})
var stmtSliceType = reflect.TypeOf([]ast.Stmt{})

// Copies also duplicate captures used more than once, so each output occurrence
// can receive its own NodeID without changing either input or another occurrence.
func (e *engine) copyNode(n ast.Node, expn ast.ExpnID) ast.Node {
	return e.copyValue(reflect.ValueOf(n), expn).Interface().(ast.Node)
}

func (e *engine) copyValue(v reflect.Value, expn ast.ExpnID) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(e.copyValue(v.Elem(), expn))
		return out
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(e.copyValue(v.Elem(), expn))
		if old, ok := v.Interface().(*token.Token); ok {
			if context, ok := e.r.TokenContexts[old]; ok {
				e.r.TokenContexts[out.Interface().(*token.Token)] = context
			}
		}
		if old, ok := v.Interface().(ast.Node); ok {
			n := out.Interface().(ast.Node)
			if e.originals == nil {
				e.originals = map[ast.Node]ast.Node{}
			}
			original := e.originals[old]
			if original == nil {
				original = old
			}
			e.originals[n] = original
			if e.captured[old] {
				e.captured[n] = true
			}
			if scope, ok := e.lexical[old]; ok {
				e.lexical[n] = scope
			}
			if origin, ok := e.r.Origins[old]; ok {
				e.r.Origins[n] = origin
			}
			if location, ok := e.r.Locations[old]; ok {
				e.r.Locations[n] = location
			}
			if item, ok := old.(ast.Item); ok {
				if body, ok := e.itemBodies[item]; ok {
					e.itemBodies[n.(ast.Item)] = body
				}
			}
			if capture, ok := e.r.Captures[old]; ok {
				e.r.Captures[n] = capture
			}
			if call, ok := old.(*ast.CallExpr); ok && e.r.Allocations[call] {
				e.r.Allocations[n.(*ast.CallExpr)] = true
			}
			if oldID, ok := old.(*ast.Ident); ok {
				if scope, ok := e.r.Scopes[oldID]; ok {
					e.r.Scopes[n.(*ast.Ident)] = scope
				}
			}
			if tree, ok := old.(*ast.TokenTree); ok {
				if ts, ok := e.trees[tree]; ok {
					e.trees[n.(*ast.TokenTree)] = ts
				}
			}
		}
		return out
	case reflect.Struct:
		if v.Type() == baseType {
			b := v.Interface().(ast.Base)
			if expn != 0 {
				b.Expn = expn
			}
			return reflect.ValueOf(b)
		}
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			out.Field(i).Set(e.copyValue(v.Field(i), expn))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(e.copyValue(v.Index(i), expn))
		}
		return out
	}
	return v
}

// rewrite only visits newly copied or newly parsed trees.
func (e *engine) rewrite(v reflect.Value, file *ast.File, parent ast.ExpnID) (reflect.Value, error) {
	if !v.IsValid() {
		return v, nil
	}
	if (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) && v.IsNil() {
		return v, nil
	}
	if v.Kind() == reflect.Interface {
		x, err := e.rewrite(v.Elem(), file, parent)
		if err != nil {
			return v, err
		}
		if !x.IsValid() {
			return x, nil
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(x)
		return out, nil
	}
	if v.Kind() == reflect.Pointer {
		switch n := v.Interface().(type) {
		case *ast.MacroRulesDecl:
			return v, &Diagnostic{Code: "template", Message: "templates cannot generate macro definitions", Location: e.location(n), Definition: e.r.Origins[n], Call: e.r.Expansions[parent].Call, Expn: parent}
		}
		if item, ok := v.Interface().(ast.Item); ok {
			items, err := e.rewriteItems([]ast.Item{item}, file, parent, singleItem)
			if err != nil {
				return v, err
			}
			return reflect.ValueOf(items[0]), nil
		}
		var path *ast.Path
		var args *ast.TokenTree
		var kind fragmentKind
		switch n := v.Interface().(type) {
		case *ast.TokenTree, *ast.Attr:
			return v, nil
		case *ast.MacroExpr:
			path, args, kind = n.Path, n.Args, exprKind
		case *ast.MacroType:
			path, args, kind = n.Path, n.Args, typeKind
		case *ast.ExprStmt:
			if m, ok := n.X.(*ast.MacroExpr); ok {
				x, err := e.invoke(m, m.Path, m.Args, stmtKind, file, parent, singleStmtKind)
				if err != nil {
					return v, err
				}
				list := x.(*ast.Block).List
				if len(list) != 1 {
					return v, &Diagnostic{Code: "template", Message: "this statement slot requires exactly one statement", Location: e.location(x), Definition: e.r.Origins[x], Call: e.r.Origins[m], Expn: nodeExpn(x)}
				}
				return reflect.ValueOf(list[0]), nil
			}
		}
		if path != nil {
			n, err := e.invoke(v.Interface().(ast.Node), path, args, kind, file, parent)
			if err != nil {
				return v, err
			}
			return reflect.ValueOf(n), nil
		}
		x, err := e.rewrite(v.Elem(), file, parent)
		if err != nil {
			return v, err
		}
		v.Elem().Set(x)
		var call ast.Expr
		switch n := v.Interface().(type) {
		case *ast.GoStmt:
			call = n.Call
		case *ast.DeferStmt:
			call = n.Call
		}
		if call != nil {
			if _, ok := call.(*ast.CallExpr); !ok {
				n := v.Interface().(ast.Node)
				id := nodeExpn(n)
				return v, &Diagnostic{Code: "category", Message: "go/defer requires a call expression", Location: e.location(n), Definition: e.r.Origins[n], Call: e.r.Expansions[id].Call, Expn: id}
			}
		}
		return v, nil
	}
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == baseType {
			return v, nil
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Name == "Items" {
				switch v.Type() {
				case reflect.TypeFor[ast.ImplDecl]():
					slot := implItems
					decl := v.Interface().(ast.ImplDecl)
					if decl.Trait != nil {
						slot = traitImplItems
						if path, ok := decl.Trait.(*ast.PathType); ok && len(path.Path.Segments) == 1 && path.Path.Segments[0].Name.Name == "Default" {
							slot = defaultImplItems
						}
					}
					items, err := e.rewriteItems(v.Field(i).Interface().([]ast.Item), file, parent, slot)
					if err != nil {
						return v, err
					}
					v.Field(i).Set(reflect.ValueOf(items))
					continue
				case reflect.TypeFor[ast.TraitDecl]():
					var input []ast.Item
					for _, item := range v.Field(i).Interface().([]ast.TraitItem) {
						input = append(input, item)
					}
					items, err := e.rewriteItems(input, file, parent, traitItems)
					if err != nil {
						return v, err
					}
					var result []ast.TraitItem
					for _, item := range items {
						result = append(result, item.(ast.TraitItem))
					}
					v.Field(i).Set(reflect.ValueOf(result))
					continue
				}
			}
			x, err := e.rewrite(v.Field(i), file, parent)
			if err != nil {
				return v, err
			}
			v.Field(i).Set(x)
		}
		switch n := v.Interface().(type) {
		case ast.UseDecl:
			if n.Tree != nil && n.Tree.Path != nil && !n.Tree.Glob && len(n.Tree.Path.Segments) > 0 {
				name := n.Tree.Alias
				if name == nil {
					name = n.Tree.Path.Segments[len(n.Tree.Path.Segments)-1].Name
				}
				if name.Name == "cfg" {
					if _, err := e.lookup(n.Tree.Path, file); err == nil {
						return v, e.selection(file).fail(&n, "reserved built-in macro alias cfg")
					}
				}
			}
		case ast.ImplDecl:
			if n.Trait != nil {
				for _, item := range n.Items {
					if reflect.ValueOf(item).Elem().FieldByName("Vis").Interface().(ast.Visibility).Kind != ast.VisNone {
						return v, e.attributeError(item, file, parent, "trait members inherit visibility")
					}
				}
			}
		case ast.DeclareDecl:
			valid := false
			switch d := n.Decl.(type) {
			case *ast.FuncDecl:
				valid = d.Body == nil
			case *ast.TypeDecl:
				valid = true
			}
			if !valid {
				return v, e.attributeError(n.Decl, file, parent, "declare requires a function without a body or a type")
			}
		}
	case reflect.Slice:
		out := reflect.MakeSlice(v.Type(), 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			if v.Type() == stmtSliceType {
				if s, ok := v.Index(i).Interface().(*ast.ExprStmt); ok {
					if m, ok := s.X.(*ast.MacroExpr); ok {
						x, err := e.invoke(m, m.Path, m.Args, stmtKind, file, parent)
						if err != nil {
							return v, err
						}
						for _, s := range x.(*ast.Block).List {
							out = reflect.Append(out, reflect.ValueOf(s))
						}
						continue
					}
				}
			}
			x, err := e.rewrite(v.Index(i), file, parent)
			if err != nil {
				return v, err
			}
			if x.IsValid() {
				out = reflect.Append(out, x)
			}
		}
		return out, nil
	}
	return v, nil
}

func nodeExpn(n ast.Node) ast.ExpnID {
	return reflect.ValueOf(n).Elem().FieldByName("Base").Interface().(ast.Base).Expn
}

func (e *engine) count(n ast.Node) error {
	ast.Walk(n, func(ast.Node) bool { e.nodes++; return e.nodes <= MaxOutputNodes })
	if e.nodes > MaxOutputNodes {
		return &parseError{code: "budget", message: fmt.Sprintf("macro output budget exceeded (%d nodes)", MaxOutputNodes)}
	}
	return nil
}
