package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/check"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) enumDecl(n *syntax.EnumDecl) []goast.Decl {
	info := e.info.Enums[n]
	if info == nil {
		e.fail(n, "缺少 enum 类型检查结果")
		return nil
	}
	marker := "is" + n.Name.Name
	var args []goast.Expr
	if n.Generics != nil {
		for _, p := range n.Generics.Params {
			for _, name := range p.Names {
				args = append(args, e.ident(name))
			}
		}
	}
	markerParams := func() *goast.FieldList {
		params := &goast.FieldList{}
		for _, arg := range args {
			params.List = append(params.List, &goast.Field{Type: arg})
		}
		return params
	}
	method := &goast.Field{Names: []*goast.Ident{goast.NewIdent(marker)}, Type: &goast.FuncType{Params: markerParams()}}
	methods := []*goast.Field{method}
	if d := e.deriveFor(info.Type); d != nil && d.Requests["Error"] {
		methods = append(methods, errorInterfaceMethods()...)
	}
	methods = append(methods, e.enumTraitMethods(info.Type)...)
	position := gotoken.NoPos
	if len(e.tree.Comments) > 0 {
		position = e.pos(n)
	}
	decls := []goast.Decl{&goast.GenDecl{TokPos: position, Tok: gotoken.TYPE, Specs: []goast.Spec{&goast.TypeSpec{
		Name: goast.NewIdent(info.GoName), TypeParams: e.enumGenerics(n.Generics),
		Type: &goast.InterfaceType{Methods: &goast.FieldList{List: methods}},
	}}}}
	for _, v := range info.Variants {
		var fields []*goast.Field
		for _, f := range v.Fields {
			fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(f.GoName)}, Type: e.goType(f.Type)})
		}
		decls = append(decls, &goast.GenDecl{TokPos: position, Tok: gotoken.TYPE, Specs: []goast.Spec{&goast.TypeSpec{
			Name: goast.NewIdent(v.GoName), TypeParams: e.enumGenerics(n.Generics),
			Type: &goast.StructType{Fields: &goast.FieldList{List: fields}},
		}}})
		recv := &goast.StarExpr{X: enumIndices(goast.NewIdent(v.GoName), args)}
		decls = append(decls, &goast.FuncDecl{Name: goast.NewIdent(marker),
			Recv: &goast.FieldList{List: []*goast.Field{{Type: recv}}},
			Type: &goast.FuncType{Func: position, Params: markerParams()}, Body: &goast.BlockStmt{},
		})
		decls = append(decls, e.enumTraitDeclarations(info.Type, recv)...)
	}
	return decls
}

func (e *emitter) enumGenerics(params *syntax.GenericParams) *goast.FieldList {
	list := e.generics(params)
	if list != nil {
		for _, f := range list.List {
			if f.Type == nil {
				f.Type = goast.NewIdent("any")
			}
		}
	}
	return list
}

func (e *emitter) enumConstruct(n syntax.Expr) (goast.Expr, bool) {
	if e.info == nil {
		return nil, false
	}
	c := e.info.Constructors[n]
	if c == nil {
		return nil, false
	}
	var values []goast.Expr
	var source []syntax.Expr
	switch n := n.(type) {
	case *syntax.CallExpr:
		source = n.Args
	case *syntax.CompositeLit:
		source = n.Elts
	}
	if c.Enum.Enum.Builtin {
		name := "New" + c.Variant.Name
		if c.Variant.Name == "None" {
			name = "None"
		}
		return &goast.CallExpr{Fun: enumIndices(e.sumMember(name), e.enumArgs(c.Enum)), Args: e.exprs(source)}, true
	}
	for i, expr := range source {
		name := ""
		value := expr
		if kv, ok := expr.(*syntax.KeyValue); ok {
			value = kv.Value
			if id, ok := kv.Key.(*syntax.Ident); ok {
				for _, f := range c.Variant.Fields {
					if f.Name == id.Name {
						name = f.GoName
						break
					}
				}
			}
		} else if i < len(c.Variant.Fields) {
			name = c.Variant.Fields[i].GoName
		}
		if name == "" {
			e.fail(expr, "未知的 enum 载荷字段")
			return nil, true
		}
		values = append(values, &goast.KeyValueExpr{Key: goast.NewIdent(name), Value: e.expr(value)})
	}
	return &goast.UnaryExpr{Op: gotoken.AND, X: &goast.CompositeLit{Type: e.variantType(c.Enum, c.Variant), Elts: values}}, true
}

func (e *emitter) enumArgs(instance *check.Instance) []goast.Expr {
	args := make([]goast.Expr, len(instance.Args))
	for i, arg := range instance.Args {
		args[i] = e.goType(arg)
	}
	return args
}

func enumIndices(base goast.Expr, args []goast.Expr) goast.Expr {
	if len(args) == 1 {
		return &goast.IndexExpr{X: base, Index: args[0]}
	}
	if len(args) > 1 {
		return &goast.IndexListExpr{X: base, Indices: args}
	}
	return base
}

func (e *emitter) variantType(instance *check.Instance, variant *check.Variant) goast.Expr {
	// Reuse the enum's package qualifier; variants live beside its interface.
	base := e.instanceType(instance)
	for {
		switch x := base.(type) {
		case *goast.IndexExpr:
			base = x.X
		case *goast.IndexListExpr:
			base = x.X
		default:
			goto named
		}
	}
named:
	switch base := base.(type) {
	case *goast.SelectorExpr:
		return enumIndices(&goast.SelectorExpr{X: base.X, Sel: goast.NewIdent(variant.GoName)}, e.enumArgs(instance))
	default:
		return enumIndices(goast.NewIdent(variant.GoName), e.enumArgs(instance))
	}
}

func (e *emitter) instanceType(instance *check.Instance) goast.Expr {
	typ := types.Type(instance.Enum.Type)
	if len(instance.Args) > 0 {
		inst, err := types.Instantiate(nil, instance.Enum.Type, instance.Args, false)
		if err != nil {
			e.fail(e.tree, fmt.Sprintf("enum 实例化失败: %v", err))
			return nil
		}
		typ = inst
	}
	return e.goType(typ)
}

func (e *emitter) sumMember(name string) goast.Expr {
	pkg := e.importName([]string{"github.com", "goxide-lang", "std", "sum"}, "sum", e.pos(e.tree))
	return &goast.SelectorExpr{X: pkg, Sel: goast.NewIdent(name)}
}
