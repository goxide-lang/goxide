package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) rawExpr(n syntax.Expr) (out goast.Expr) {
	defer func() { e.macroLayout(n, out) }()
	if n == nil {
		return nil
	}
	if call, ok := n.(*syntax.CallExpr); ok {
		if adapter := e.info.GoResults[call]; adapter != nil {
			return e.goResult(call, adapter)
		}
		if result, handled := e.memberCall(call); handled {
			return result
		}
	}
	if member := e.info.Members[n]; member != nil {
		return e.memberExpr(member)
	}
	if result, ok := e.enumConstruct(n); ok {
		return result
	}
	if result, ok := e.equalityCall(n); ok {
		return result
	}
	switch n := n.(type) {
	case *syntax.Ident:
		return e.reference(n)
	case *syntax.BasicLit:
		return &goast.BasicLit{ValuePos: e.pos(n), Kind: tokenKind(n.Kind), Value: n.Value}
	case *syntax.BoolLit:
		name := "false"
		if n.Value {
			name = "true"
		}
		if e.booleanShadowed(name) {
			op := gotoken.NEQ
			if n.Value {
				op = gotoken.EQL
			}
			return &goast.BinaryExpr{X: &goast.BasicLit{ValuePos: e.pos(n), Kind: gotoken.INT, Value: "0"}, Op: op, Y: &goast.BasicLit{Kind: gotoken.INT, Value: "0"}}
		}
		return &goast.Ident{NamePos: e.pos(n), Name: name}
	case *syntax.CompositeLit:
		brace := e.pos(n)
		if n.Type != nil {
			brace = e.end(n.Type)
		}
		elts := make([]goast.Expr, len(n.Elts))
		for i, elt := range n.Elts {
			elts[i] = e.expr(elt)
			if typ := e.info.Types[n]; typ != nil {
				if _, record := typ.Underlying().(*types.Struct); record {
					if field, ok := elt.(*syntax.KeyValue); ok {
						if id, ok := field.Key.(*syntax.Ident); ok {
							elts[i].(*goast.KeyValueExpr).Key = &goast.Ident{NamePos: e.pos(id), Name: id.Name}
						}
					}
				}
			}
		}
		typ := e.typ(n.Type)
		pointer := false
		if typ == nil && e.nameableType(e.info.Types[n]) {
			checked := e.info.Types[n]
			if p, ok := checked.(*types.Pointer); ok {
				checked, pointer = p.Elem(), true
			}
			typ = e.goType(checked)
		}
		literal := &goast.CompositeLit{Type: typ, Lbrace: brace, Elts: elts, Rbrace: e.end(n) - 1}
		if pointer {
			return &goast.UnaryExpr{Op: gotoken.AND, X: literal}
		}
		return literal
	case *syntax.KeyValue:
		return &goast.KeyValueExpr{Key: e.expr(n.Key), Colon: e.end(n.Key), Value: e.expr(n.Value)}
	case *syntax.ParenExpr:
		return &goast.ParenExpr{Lparen: e.pos(n), X: e.expr(n.X), Rparen: e.end(n) - 1}
	case *syntax.SelectorExpr:
		return &goast.SelectorExpr{X: e.expr(n.X), Sel: e.ident(n.Sel)}
	case *syntax.IndexExpr:
		if len(n.Indices) == 1 {
			return &goast.IndexExpr{X: e.expr(n.X), Lbrack: e.end(n.X), Index: e.expr(n.Indices[0]), Rbrack: e.end(n) - 1}
		}
		return &goast.IndexListExpr{X: e.expr(n.X), Lbrack: e.end(n.X), Indices: e.exprs(n.Indices), Rbrack: e.end(n) - 1}
	case *syntax.SliceExpr:
		return &goast.SliceExpr{X: e.expr(n.X), Lbrack: e.end(n.X), Low: e.expr(n.Low), High: e.expr(n.High), Max: e.expr(n.Max), Slice3: n.Slice3, Rbrack: e.end(n) - 1}
	case *syntax.TypeAssertExpr:
		return &goast.TypeAssertExpr{X: e.expr(n.X), Lparen: e.end(n.X) + 1, Type: e.typ(n.Type), Rparen: e.end(n) - 1}
	case *syntax.CallExpr:
		call := &goast.CallExpr{Fun: e.expr(n.Fun), Lparen: e.end(n.Fun), Args: e.exprs(n.Args), Rparen: e.end(n) - 1}
		if n.Ellipsis {
			call.Ellipsis = e.end(n) - 4
		}
		return call
	case *syntax.StarExpr:
		return &goast.StarExpr{Star: e.pos(n), X: e.expr(n.X)}
	case *syntax.UnaryExpr:
		return &goast.UnaryExpr{OpPos: e.pos(n), Op: tokenKind(n.Op), X: e.expr(n.X)}
	case *syntax.BinaryExpr:
		return &goast.BinaryExpr{X: e.expr(n.X), OpPos: e.end(n.X), Op: tokenKind(n.Op), Y: e.expr(n.Y)}
	case *syntax.FuncLit:
		outerBreak := e.breakTarget
		e.breakTarget = nil
		defer func() { e.breakTarget = outerBreak }()
		if n.Form != syntax.GoFunc || n.Mods.Unsafe || n.Mods.Async || n.Mods.Const || n.Expr != nil {
			e.fail(n, "扩展闭包尚未降低")
			return nil
		}
		ft := &goast.FuncType{Func: e.pos(n), Params: e.params(n.Params)}
		if len(n.Results) > 0 {
			ft.Results = e.params(n.Results)
		} else if n.Result != nil {
			ft.Results = &goast.FieldList{List: []*goast.Field{{Type: e.typ(n.Result)}}}
		}
		return &goast.FuncLit{Type: ft, Body: e.functionBody(ft, n.Body)}
	case *syntax.PathExpr:
		return e.path(n.Path)
	case *syntax.RefExpr:
		return &goast.UnaryExpr{OpPos: e.pos(n), Op: gotoken.AND, X: e.expr(n.X)}
	case *syntax.TypeExpr:
		return e.typ(n.Type)
	case *syntax.MatchExpr:
		e.fail(n, "match 表达式尚未降低")
	case *syntax.TryExpr:
		e.fail(n, "? 错误传播尚未降低")
	case *syntax.UnsafeExpr:
		e.fail(n, "unsafe 表达式尚未降低")
	case *syntax.MacroExpr:
		e.fail(n, "宏调用尚未降低")
	case *syntax.QualifiedSelectorExpr:
		e.fail(n, "trait 限定分派尚未降低")
	case *syntax.AwaitExpr:
		e.fail(n, "await 表达式尚未降低")
	case *syntax.BadExpr:
		e.fail(n, "错误表达式无法降低")
	default:
		e.fail(n, "表达式尚未降低")
	}
	return nil
}

func (e *emitter) exprs(nodes []syntax.Expr) []goast.Expr {
	return e.ordered(nodes)
}
