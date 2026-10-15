package lower

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
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
	if local := e.localTypeAlias(t); local != nil {
		return local
	}
	if alias := e.exportedTypeAlias(t); alias != nil {
		return e.goType(alias)
	}
	if basic, ok := types.Unalias(types.Default(t)).(*types.Basic); ok {
		if basic.Kind() == types.UnsafePointer {
			return &goast.SelectorExpr{X: e.importName([]string{"unsafe"}, "unsafe", 0), Sel: goast.NewIdent("Pointer")}
		}
		return e.primitiveAlias(basic.Name())
	}
	switch t := t.(type) {
	case *types.Pointer:
		return &goast.StarExpr{X: e.goType(t.Elem())}
	case *types.Slice:
		return &goast.ArrayType{Elt: e.goType(t.Elem())}
	case *types.Array:
		return &goast.ArrayType{Len: &goast.BasicLit{Kind: gotoken.INT, Value: fmt.Sprint(t.Len())}, Elt: e.goType(t.Elem())}
	case *types.Map:
		return &goast.MapType{Key: e.goType(t.Key()), Value: e.goType(t.Elem())}
	case *types.Chan:
		dir := goast.SEND | goast.RECV
		if t.Dir() == types.SendOnly {
			dir = goast.SEND
		} else if t.Dir() == types.RecvOnly {
			dir = goast.RECV
		}
		return &goast.ChanType{Dir: dir, Value: e.goType(t.Elem())}
	case *types.Signature:
		fields := func(tuple *types.Tuple, variadic bool) *goast.FieldList {
			list := &goast.FieldList{}
			for index := 0; index < tuple.Len(); index++ {
				typ := tuple.At(index).Type()
				var field goast.Expr
				if variadic && index == tuple.Len()-1 {
					field = &goast.Ellipsis{Elt: e.goType(typ.(*types.Slice).Elem())}
				} else {
					field = e.goType(typ)
				}
				list.List = append(list.List, &goast.Field{Type: field})
			}
			return list
		}
		return &goast.FuncType{Params: fields(t.Params(), t.Variadic()), Results: fields(t.Results(), false)}
	case *types.Named:
		var name goast.Expr = goast.NewIdent(t.Obj().Name())
		if e.localTypePackage(t.Obj().Pkg()) {
			name = e.packageTypeName(t)
		}
		if pkg := t.Obj().Pkg(); pkg != nil && !e.localTypePackage(pkg) {
			path := pkg.Path()
			if mapped := e.info.PackagePaths[path]; mapped != "" {
				path = mapped
			}
			name = &goast.SelectorExpr{X: e.importName(strings.Split(path, "/"), pkg.Name(), 0), Sel: goast.NewIdent(t.Obj().Name())}
		}
		if t.TypeArgs().Len() > 0 {
			args := make([]goast.Expr, t.TypeArgs().Len())
			for i := range args {
				args[i] = e.goType(t.TypeArgs().At(i))
			}
			if len(args) == 1 {
				return &goast.IndexExpr{X: name, Index: args[0]}
			}
			return &goast.IndexListExpr{X: name, Indices: args}
		}
		return name
	}
	name := types.TypeString(types.Default(t), func(p *types.Package) string {
		if e.localTypePackage(p) {
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

func (e *emitter) expr(n syntax.Expr) (out goast.Expr) {
	if n == nil {
		return nil
	}
	if x := e.replacements[n]; x != nil {
		return x
	}
	defer func() { out = e.traitView(n, out) }()
	if e.assignmentGuard != nil {
		return e.assignmentExpr(n)
	}
	if call, ok := n.(*syntax.CallExpr); ok {
		if adapter := e.info.GoResults[call]; adapter != nil {
			return e.goResult(call, adapter)
		}
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
		for s != nil && s.AliasOf != nil {
			s = s.AliasOf
		}
		return s != nil && (s.Kind == resolve.Constant || s.Kind == resolve.Type || s.Kind == resolve.Builtin || s.Kind == resolve.Function || s.Decl.File == "" && s.Name.Name == "nil")
	case *syntax.PathExpr:
		s := e.file.References[n.Path.ID()].Symbol
		if s == nil && len(n.Path.Segments) == 1 {
			s = e.file.References[n.Path.Segments[0].Name.ID()].Symbol
		}
		for s != nil && s.AliasOf != nil {
			s = s.AliasOf
		}
		return s != nil && (s.Kind == resolve.Constant || s.Kind == resolve.Type || s.Kind == resolve.Variant || s.Kind == resolve.Builtin || s.Kind == resolve.Function)
	}
	return false
}

func (e *emitter) spill(n syntax.Expr, x goast.Expr) goast.Expr {
	if n == nil || e.constant(n) {
		return x
	}
	id := e.fresh("Value")
	t := e.info.Types[n]
	if t != nil && t != types.Typ[types.Invalid] && e.nameableType(t) {
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
			out := e.logicalTemporary(x, left)
			before, right := e.exprScoped(x.Y)
			var cond goast.Expr = out
			if x.Op == token.LOR {
				cond = &goast.UnaryExpr{Op: gotoken.NOT, X: out}
			}
			before = append(before, assign(out, right))
			e.pending = append(e.pending, &goast.IfStmt{Cond: cond, Body: &goast.BlockStmt{List: before}})
			if e.untypedBoolean(x) {
				return &goast.BinaryExpr{X: out, Op: gotoken.EQL, Y: &goast.BinaryExpr{X: integer(0), Op: gotoken.EQL, Y: integer(0)}}
			}
			return out
		}
		children = []syntax.Expr{x.X, x.Y}
	case *syntax.CallExpr:
		if result, ok := e.equalityCall(x); ok {
			return result
		}
		if result, ok := e.flowNativeMethod(x); ok {
			return result
		}
		if member := e.info.Members[x.Fun]; member != nil && member.MethodExpr {
			if result, ok := e.memberCall(x); ok {
				return result
			}
		}
		children = append([]syntax.Expr{x.Fun}, x.Args...)
		if len(x.Args) == 1 && hasFlow(x.Args[0]) {
			fun := x.Fun
			for {
				paren, ok := fun.(*syntax.ParenExpr)
				if !ok {
					break
				}
				fun = paren.X
			}
			symbol := e.file.References[fun.ID()].Symbol
			for symbol != nil && symbol.AliasOf != nil {
				symbol = symbol.AliasOf
			}
			if symbol != nil && symbol.Kind == resolve.Builtin && (symbol.Name.Name == "len" || symbol.Name.Name == "cap") {
				typ := e.info.Types[x.Args[0]]
				if typ != nil {
					typ = typ.Underlying()
					if pointer, ok := typ.(*types.Pointer); ok {
						typ = pointer.Elem().Underlying()
					}
					if _, array := typ.(*types.Array); array {
						values := e.ordered(children)
						// Removing a call while lowering ? must not make Go's
						// array len/cap rule skip the remaining operand evaluation.
						values[1] = e.spill(x.Args[0], values[1])
						return e.withExprs(n, children, values)
					}
				}
			}
		}
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
					if literal, ok := elt.(*syntax.CompositeLit); ok && literal.Type == nil && !e.nameableType(e.info.Types[elt]) && x.Type != nil {
						// The enclosing literal supplies a private element type that
						// cannot be written in a standalone temporary declaration.
						v = &goast.IndexExpr{X: &goast.CompositeLit{Type: e.typ(x.Type), Elts: []goast.Expr{v}}, Index: integer(0)}
					}
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
	case *syntax.QualifiedSelectorExpr:
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
		// Keep the original place while ? and match introduce statements.
		if e.addressable(x.X) {
			return &goast.UnaryExpr{Op: gotoken.AND, X: e.lvalue(x.X)}
		}
		return &goast.UnaryExpr{Op: gotoken.AND, X: e.expr(x.X)}
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
