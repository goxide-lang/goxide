package lower

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func (e *emitter) fresh(prefix string) *goast.Ident {
	for {
		e.tempIndex++
		name := fmt.Sprintf("goxide%s%d", prefix, e.tempIndex)
		if !e.reserved[name] {
			e.reserved[name] = true
			return goast.NewIdent(name)
		}
	}
}

func (e *emitter) goType(t types.Type) goast.Expr {
	if t == nil || t == types.Typ[types.Invalid] {
		e.fail(e.tree, "无法确定表达式类型")
		return goast.NewIdent("any")
	}
	name := types.TypeString(types.Default(t), func(p *types.Package) string {
		if p.Path() == filepath.ToSlash(filepath.Dir(e.name)) {
			return ""
		}
		path := p.Path()
		if mapped := e.info.PackagePaths[path]; mapped != "" {
			path = mapped
		}
		return e.importName(strings.Split(path, "/"), p.Name(), 0).Name
	})
	x, err := goparser.ParseExpr(name)
	if err != nil {
		e.fail(e.tree, "无法输出类型: "+name)
		return goast.NewIdent("any")
	}
	clearGoPositions(reflect.ValueOf(x))
	return x
}

func clearGoPositions(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	if v.Type() == reflect.TypeOf(gotoken.NoPos) && v.CanSet() {
		v.SetInt(0)
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			clearGoPositions(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			clearGoPositions(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			clearGoPositions(v.Index(i))
		}
	}
}

func hasFlow(n syntax.Node) bool {
	if n == nil {
		return false
	}
	found := false
	syntax.Walk(n, func(n syntax.Node) bool {
		if _, ok := n.(*syntax.FuncLit); ok {
			return false
		}
		switch n.(type) {
		case *syntax.MatchExpr, *syntax.TryExpr:
			found = true
			return false
		}
		return !found
	})
	return found
}

func (e *emitter) exprScoped(n syntax.Expr) ([]goast.Stmt, goast.Expr) {
	old := e.pending
	e.pending = nil
	x := e.expr(n)
	before := e.pending
	e.pending = old
	return before, x
}

func (e *emitter) expr(n syntax.Expr) goast.Expr {
	if n == nil {
		return nil
	}
	if x := e.replacements[n]; x != nil {
		return x
	}
	switch n := n.(type) {
	case *syntax.MatchExpr:
		info := e.info.Matches[n]
		if info == nil {
			e.fail(n, "缺少 match 检查结果")
			return goast.NewIdent("nil")
		}
		out := e.fresh("Match")
		e.pending = append(e.pending, varStmt(out, e.goType(info.Type), nil))
		e.pending = append(e.pending, e.matchStmts(n, out)...)
		return out
	case *syntax.TryExpr:
		return e.tryExpr(n)
	}
	if hasFlow(n) {
		return e.flowExpr(n)
	}
	return e.rawExpr(n)
}

func varStmt(id *goast.Ident, typ, value goast.Expr) goast.Stmt {
	spec := &goast.ValueSpec{Names: []*goast.Ident{id}, Type: typ}
	if value != nil {
		spec.Values = []goast.Expr{value}
	}
	return &goast.DeclStmt{Decl: &goast.GenDecl{Tok: gotoken.VAR, Specs: []goast.Spec{spec}}}
}

func assign(id goast.Expr, value goast.Expr) goast.Stmt {
	return &goast.AssignStmt{Lhs: []goast.Expr{id}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{value}}
}

func (e *emitter) constant(n syntax.Expr) bool {
	switch n := n.(type) {
	case *syntax.BasicLit, *syntax.TypeExpr:
		return true
	case *syntax.ParenExpr:
		return e.constant(n.X)
	case *syntax.BinaryExpr:
		return e.constant(n.X) && e.constant(n.Y)
	case *syntax.UnaryExpr:
		return n.Op != token.ARROW && n.Op != token.AND && e.constant(n.X)
	case *syntax.Ident:
		s := e.file.References[n.ID()].Symbol
		return n.Name == "nil" || s != nil && (s.Kind == resolve.Constant || s.Kind == resolve.Type || s.Kind == resolve.Builtin)
	case *syntax.PathExpr:
		s := e.file.References[n.Path.ID()].Symbol
		return s != nil && (s.Kind == resolve.Constant || s.Kind == resolve.Type || s.Kind == resolve.Variant || s.Kind == resolve.Builtin)
	}
	return false
}

func (e *emitter) spill(n syntax.Expr, x goast.Expr) goast.Expr {
	if n == nil || e.constant(n) {
		return x
	}
	id := e.fresh("Value")
	t := e.info.Types[n]
	if t != nil && t != types.Typ[types.Invalid] {
		e.pending = append(e.pending, varStmt(id, e.goType(t), x))
	} else {
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{id}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{x}})
	}
	return id
}

func (e *emitter) ordered(nodes []syntax.Expr) []goast.Expr {
	if len(nodes) == 0 {
		return nil
	}
	last := -1
	for i, n := range nodes {
		if hasFlow(n) {
			last = i
		}
	}
	out := make([]goast.Expr, len(nodes))
	for i, n := range nodes {
		out[i] = e.expr(n)
		if i < last && e.replacements[n] == nil {
			out[i] = e.spill(n, out[i])
		}
	}
	return out
}

func (e *emitter) withExprs(n syntax.Expr, children []syntax.Expr, values []goast.Expr) goast.Expr {
	old := e.replacements
	e.replacements = make(map[syntax.Expr]goast.Expr, len(old)+len(children))
	for k, v := range old {
		e.replacements[k] = v
	}
	for i, c := range children {
		if c != nil {
			e.replacements[c] = values[i]
		}
	}
	x := e.rawExpr(n)
	e.replacements = old
	return x
}

func (e *emitter) flowExpr(n syntax.Expr) goast.Expr {
	var children []syntax.Expr
	switch x := n.(type) {
	case *syntax.BinaryExpr:
		if x.Op == token.LAND || x.Op == token.LOR {
			left := e.expr(x.X)
			out := e.fresh("Bool")
			e.pending = append(e.pending, varStmt(out, e.goType(e.info.Types[n]), left))
			before, right := e.exprScoped(x.Y)
			var cond goast.Expr = out
			if x.Op == token.LOR {
				cond = &goast.UnaryExpr{Op: gotoken.NOT, X: out}
			}
			before = append(before, assign(out, right))
			e.pending = append(e.pending, &goast.IfStmt{Cond: cond, Body: &goast.BlockStmt{List: before}})
			return out
		}
		children = []syntax.Expr{x.X, x.Y}
	case *syntax.CallExpr:
		children = append([]syntax.Expr{x.Fun}, x.Args...)
	case *syntax.CompositeLit:
		// Keys of struct literals are names, rather than evaluated expressions.
		var fields []syntax.Expr
		var values []goast.Expr
		last := -1
		for i, elt := range x.Elts {
			if hasFlow(elt) {
				last = i
			}
		}
		for i, elt := range x.Elts {
			if kv, ok := elt.(*syntax.KeyValue); ok {
				isMap := false
				if t := e.info.Types[n]; t != nil {
					_, isMap = types.Unalias(t).Underlying().(*types.Map)
				}
				if isMap {
					key := e.expr(kv.Key)
					if i < last || hasFlow(kv.Value) {
						key = e.spill(kv.Key, key)
					}
					fields = append(fields, kv.Key)
					values = append(values, key)
				}
				v := e.expr(kv.Value)
				if i < last {
					v = e.spill(kv.Value, v)
				}
				fields = append(fields, kv.Value)
				values = append(values, v)
			} else {
				v := e.expr(elt)
				if i < last {
					v = e.spill(elt, v)
				}
				fields = append(fields, elt)
				values = append(values, v)
			}
		}
		return e.withExprs(n, fields, values)
	case *syntax.ParenExpr:
		children = []syntax.Expr{x.X}
	case *syntax.SelectorExpr:
		children = []syntax.Expr{x.X}
	case *syntax.IndexExpr:
		base := e.expr(x.X)
		flow := false
		for _, index := range x.Indices {
			flow = flow || hasFlow(index)
		}
		if flow {
			base = e.container(x.X, base)
		}
		children = append([]syntax.Expr{x.X}, x.Indices...)
		return e.withExprs(n, children, append([]goast.Expr{base}, e.ordered(x.Indices)...))
	case *syntax.SliceExpr:
		base := e.expr(x.X)
		if hasFlow(x.Low) || hasFlow(x.High) || hasFlow(x.Max) {
			base = e.container(x.X, base)
		}
		children = []syntax.Expr{x.X, x.Low, x.High, x.Max}
		return e.withExprs(n, children, append([]goast.Expr{base}, e.ordered(children[1:])...))
	case *syntax.TypeAssertExpr:
		children = []syntax.Expr{x.X}
	case *syntax.StarExpr:
		children = []syntax.Expr{x.X}
	case *syntax.UnaryExpr:
		children = []syntax.Expr{x.X}
	case *syntax.RefExpr:
		children = []syntax.Expr{x.X}
	default:
		return e.rawExpr(n)
	}
	return e.withExprs(n, children, e.ordered(children))
}

// Arrays keep their storage when evaluating a later index or slice bound.
func (e *emitter) container(n syntax.Expr, x goast.Expr) goast.Expr {
	if t := e.info.Types[n]; t != nil {
		if _, array := t.Underlying().(*types.Array); array && e.addressable(n) {
			id := e.fresh("Array")
			e.pending = append(e.pending, varStmt(id, &goast.StarExpr{X: e.goType(t)}, &goast.UnaryExpr{Op: gotoken.AND, X: x}))
			return id
		}
	}
	return e.spill(n, x)
}

func (e *emitter) addressable(n syntax.Expr) bool {
	switch n := n.(type) {
	case *syntax.Ident:
		s := e.file.References[n.ID()].Symbol
		return s != nil && s.Kind == resolve.Variable
	case *syntax.ParenExpr:
		return e.addressable(n.X)
	case *syntax.StarExpr:
		return true
	case *syntax.SelectorExpr:
		if t := e.info.Types[n.X]; t != nil {
			if _, pointer := t.Underlying().(*types.Pointer); pointer {
				return true
			}
		}
		return e.addressable(n.X)
	case *syntax.IndexExpr:
		if t := e.info.Types[n.X]; t != nil {
			switch t.Underlying().(type) {
			case *types.Slice, *types.Pointer:
				return true
			case *types.Array:
				return e.addressable(n.X)
			}
		}
	}
	return false
}
