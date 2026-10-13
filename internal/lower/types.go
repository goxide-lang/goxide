package lower

import (
	goast "go/ast"
	gotoken "go/token"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) typ(n syntax.Type) (out goast.Expr) {
	defer func() { e.macroLayout(n, out) }()
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *syntax.PathType:
		if n.Path != nil && len(n.Path.Segments) > 0 {
			id := n.Path.Segments[0].Name
			if s := e.file.References[id.ID()].Symbol; s != nil && s.Decl.File == "" && (s.Name.Name == "Option" || s.Name.Name == "Result") {
				return e.typeArgs(e.sumMember(s.Name.Name), n.Path.Segments[0].Args)
			}
		}
		return e.path(n.Path)
	case *syntax.PointerType:
		return &goast.StarExpr{Star: e.pos(n), X: e.typ(n.Elem)}
	case *syntax.SliceType:
		return &goast.ArrayType{Lbrack: e.pos(n), Elt: e.typ(n.Elem)}
	case *syntax.ArrayType:
		length := e.expr(n.Len)
		if n.Ellipsis {
			length = &goast.Ellipsis{Ellipsis: e.pos(n) + 1}
		}
		return &goast.ArrayType{Lbrack: e.pos(n), Len: length, Elt: e.typ(n.Elem)}
	case *syntax.MapType:
		return &goast.MapType{Map: e.pos(n), Key: e.typ(n.Key), Value: e.typ(n.Value)}
	case *syntax.ChanType:
		dir := goast.SEND | goast.RECV
		arrow := gotoken.NoPos
		if n.Dir == syntax.SendOnly {
			dir = goast.SEND
			arrow = e.pos(n) + 4
		} else if n.Dir == syntax.RecvOnly {
			dir = goast.RECV
			arrow = e.pos(n)
		}
		return &goast.ChanType{Begin: e.pos(n), Arrow: arrow, Dir: dir, Value: e.typ(n.Elem)}
	case *syntax.FuncType:
		ft := &goast.FuncType{Func: e.pos(n), Params: e.params(n.Params)}
		if len(n.Results) > 0 {
			ft.Results = e.params(n.Results)
		}
		return ft
	case *syntax.StructType:
		fields := make([]*goast.Field, len(n.Fields))
		for i, f := range n.Fields {
			field := &goast.Field{Type: e.typ(f.Type)}
			if len(f.Names) == 0 {
				e.embeddedType(f.Type, field.Type)
			}
			for _, name := range f.Names {
				field.Names = append(field.Names, e.ident(name))
			}
			if f.Tag != nil {
				field.Tag = &goast.BasicLit{ValuePos: e.pos(f.Tag), Kind: tokenKind(f.Tag.Kind), Value: f.Tag.Value}
			}
			fields[i] = field
		}
		return &goast.StructType{Struct: e.pos(n), Fields: &goast.FieldList{Opening: e.pos(n) + 6, List: fields, Closing: e.end(n) - 1}}
	case *syntax.InterfaceType:
		fields := make([]*goast.Field, len(n.Methods))
		for i, m := range n.Methods {
			for _, attr := range m.Attrs {
				e.fail(attr, "接口成员属性尚未降低")
			}
			field := &goast.Field{}
			if m.Name == nil {
				field.Type = e.typ(m.Type)
			} else {
				field.Names = []*goast.Ident{e.ident(m.Name)}
				ft := &goast.FuncType{Params: e.params(m.Params)}
				if len(m.Results) > 0 {
					ft.Results = e.params(m.Results)
				}
				field.Type = ft
			}
			fields[i] = field
		}
		return &goast.InterfaceType{Interface: e.pos(n), Methods: &goast.FieldList{Opening: e.pos(n) + 9, List: fields, Closing: e.end(n) - 1}}
	case *syntax.UnionType:
		var out goast.Expr
		for _, term := range n.Terms {
			if out == nil {
				out = e.typ(term)
			} else {
				out = &goast.BinaryExpr{X: out, OpPos: e.pos(term) - 1, Op: gotoken.OR, Y: e.typ(term)}
			}
		}
		return out
	case *syntax.ApproxType:
		return &goast.UnaryExpr{OpPos: e.pos(n), Op: gotoken.TILDE, X: e.typ(n.Type)}
	case *syntax.ProjectionType:
		if !n.Dot {
			e.fail(n, "关联类型投影尚未降低")
			return nil
		}
		return e.typeArgs(&goast.SelectorExpr{X: e.typ(n.X), Sel: e.ident(n.Item)}, n.Args)
	case *syntax.ParenType:
		return &goast.ParenExpr{Lparen: e.pos(n), X: e.typ(n.Type), Rparen: e.end(n) - 1}
	case *syntax.RefType:
		return &goast.StarExpr{Star: e.pos(n), X: e.typ(n.Elem)}
	case *syntax.SelfType:
		e.fail(n, "Self 类型尚未降低")
	case *syntax.MacroType:
		e.fail(n, "类型宏调用尚未降低")
	case *syntax.BadType:
		e.fail(n, "错误类型无法降低")
	default:
		e.fail(n, "类型尚未降低")
	}
	return nil
}

func (e *emitter) embeddedType(source syntax.Type, lowered goast.Expr) {
	id, out := embeddedSyntaxIdent(source), embeddedIdent(lowered)
	if id == nil || out == nil {
		return
	}
	name := out.Name
	if symbol := e.file.References[id.ID()].Symbol; symbol != nil {
		if symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		if symbol.Kind == resolve.Type && symbol.GoName != "" {
			name = symbol.GoName
		}
	}
	if name != id.Name {
		out.Name = id.Name
		e.memberNames[out] = name
	}
}

func embeddedSyntaxIdent(typ syntax.Type) *syntax.Ident {
	switch typ := typ.(type) {
	case *syntax.PathType:
		if len(typ.Path.Segments) != 0 {
			return typ.Path.Segments[len(typ.Path.Segments)-1].Name
		}
	case *syntax.PointerType:
		return embeddedSyntaxIdent(typ.Elem)
	case *syntax.ProjectionType:
		return typ.Item
	case *syntax.ParenType:
		return embeddedSyntaxIdent(typ.Type)
	}
	return nil
}

func (e *emitter) params(params []*syntax.Param) *goast.FieldList {
	list := &goast.FieldList{}
	for _, p := range params {
		for _, attr := range p.Attrs {
			e.fail(attr, "参数属性尚未降低")
		}
		f := &goast.Field{Type: e.typ(p.Type)}
		if p.Variadic {
			f.Type = &goast.Ellipsis{Ellipsis: e.pos(p.Type) - 3, Elt: f.Type}
		}
		for _, name := range p.Names {
			f.Names = append(f.Names, e.ident(name))
		}
		list.List = append(list.List, f)
	}
	return list
}

func (e *emitter) generics(params *syntax.GenericParams) *goast.FieldList {
	if params == nil {
		return nil
	}
	list := &goast.FieldList{Opening: e.pos(params), Closing: e.end(params) - 1}
	for _, p := range params.Params {
		f := &goast.Field{Type: e.typ(p.Constraint)}
		for _, name := range p.Names {
			f.Names = append(f.Names, e.ident(name))
		}
		list.List = append(list.List, f)
	}
	return list
}

func (e *emitter) typeArgs(x goast.Expr, args *syntax.GenericArgs) goast.Expr {
	if args == nil {
		return x
	}
	indices := make([]goast.Expr, len(args.Args))
	for i, a := range args.Args {
		if a.Name != nil {
			e.fail(a, "关联类型绑定尚未降低")
		}
		indices[i] = e.typ(a.Type)
	}
	if len(indices) == 1 {
		return &goast.IndexExpr{X: x, Lbrack: e.pos(args), Index: indices[0], Rbrack: e.end(args) - 1}
	}
	return &goast.IndexListExpr{X: x, Lbrack: e.pos(args), Indices: indices, Rbrack: e.end(args) - 1}
}
