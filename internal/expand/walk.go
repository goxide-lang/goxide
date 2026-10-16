package expand

import (
	"fmt"
	"reflect"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
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
		var path *ast.Path
		var args *ast.TokenTree
		var kind fragmentKind
		switch n := v.Interface().(type) {
		case *ast.TokenTree, *ast.Attr:
			return v, nil
		case *ast.UseDecl:
			if n.Tree != nil && n.Tree.Path != nil && !n.Tree.Glob && len(n.Tree.Path.Segments) > 0 {
				name := n.Tree.Alias
				if name == nil {
					name = n.Tree.Path.Segments[len(n.Tree.Path.Segments)-1].Name
				}
				if name.Name == "cfg" {
					if _, err := e.lookup(n.Tree.Path, file); err == nil {
						return v, e.selection(file).fail(n, "reserved built-in macro alias cfg")
					}
				}
			}
		case *ast.MacroRulesDecl:
			return v, &Diagnostic{Code: "template", Message: "templates cannot generate macro definitions", Definition: e.r.Origins[n], Call: e.r.Expansions[parent].Call, Expn: parent}
		case *ast.MacroExpr:
			path, args, kind = n.Path, n.Args, exprKind
		case *ast.MacroType:
			path, args, kind = n.Path, n.Args, typeKind
		case *ast.MacroItem:
			path, args, kind = n.Path, n.Args, itemKind
		case *ast.ExprStmt:
			if m, ok := n.X.(*ast.MacroExpr); ok {
				x, err := e.invoke(m, m.Path, m.Args, stmtKind, file, parent)
				if err != nil {
					return v, err
				}
				list := x.(*ast.Block).List
				if len(list) != 1 {
					return v, &Diagnostic{Code: "template", Message: "this statement slot requires exactly one statement", Definition: e.r.Origins[x], Call: e.r.Origins[m], Expn: nodeExpn(x)}
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
				return v, &Diagnostic{Code: "category", Message: "go/defer requires a call expression", Definition: e.r.Origins[n], Call: e.r.Expansions[id].Call, Expn: id}
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
			x, err := e.rewrite(v.Field(i), file, parent)
			if err != nil {
				return v, err
			}
			v.Field(i).Set(x)
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
