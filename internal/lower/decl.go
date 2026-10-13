package lower

import (
	goast "go/ast"
	gotoken "go/token"

	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

func (e *emitter) decl(n syntax.Item) goast.Decl {
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *syntax.UseDecl:
		return nil
	case *syntax.FuncDecl:
		if n.Mods.Unsafe || n.Mods.Async || n.Mods.Const {
			e.fail(n, "函数修饰符尚未降低")
		}
		if n.Where != nil {
			e.fail(n.Where, "where 约束尚未降低")
		}
		if n.Path != nil {
			e.fail(n.Path, "限定路径函数声明尚未降低")
			return nil
		}
		typ := &goast.FuncType{Func: e.pos(n), TypeParams: e.generics(n.Generics), Params: e.params(n.Params)}
		if len(n.Results) != 0 {
			typ.Results = e.params(n.Results)
		}
		decl := &goast.FuncDecl{Name: e.ident(n.Name), Type: typ, Body: e.block(n.Body)}
		if len(n.Recv) != 0 {
			decl.Recv = e.params(n.Recv)
		}
		return decl
	case *syntax.TypeDecl:
		decl := &goast.GenDecl{TokPos: e.pos(n), Tok: gotoken.TYPE}
		if n.Grouped {
			decl.Lparen, decl.Rparen = e.pos(n)+gotoken.Pos(len("type")), e.end(n)-1
			for _, spec := range n.Specs {
				decl.Specs = append(decl.Specs, e.typeSpec(spec.Name, spec.Type, spec.Generics, spec.Where, spec.Alias))
			}
		} else {
			decl.Specs = []goast.Spec{e.typeSpec(n.Name, n.Type, n.Generics, n.Where, n.Alias)}
		}
		return decl
	case *syntax.ConstDecl:
		return e.valueDecl(n, gotoken.CONST, n.Grouped, n.Specs)
	case *syntax.VarDecl:
		return e.valueDecl(n, gotoken.VAR, n.Grouped, n.Specs)
	case *syntax.ModDecl:
		e.fail(n, "mod 声明尚未降低")
	case *syntax.EnumDecl:
		e.fail(n, "enum 声明尚未降低")
	case *syntax.TraitDecl:
		e.fail(n, "trait 声明尚未降低")
	case *syntax.ImplDecl:
		e.fail(n, "impl 声明尚未降低")
	case *syntax.DeclareDecl:
		e.fail(n, "declare 声明尚未降低")
	case *syntax.MacroRulesDecl:
		e.fail(n, "宏定义尚未降低")
	case *syntax.MacroItem:
		e.fail(n, "宏调用尚未降低")
	case *syntax.BadItem:
		e.fail(n, "错误声明不能降低")
	default:
		e.fail(n, "此声明尚未降低")
	}
	return nil
}

func (e *emitter) typeSpec(name *syntax.Ident, typ syntax.Type, generics *syntax.GenericParams, where *syntax.WhereClause, alias bool) *goast.TypeSpec {
	if where != nil {
		e.fail(where, "where 约束尚未降低")
	}
	spec := &goast.TypeSpec{Name: e.ident(name), TypeParams: e.generics(generics), Type: e.typ(typ)}
	if alias {
		spec.Assign = e.end(name)
	}
	return spec
}

func (e *emitter) valueDecl(n syntax.Node, tok gotoken.Token, grouped bool, specs []*syntax.ValueSpec) *goast.GenDecl {
	decl := &goast.GenDecl{TokPos: e.pos(n), Tok: tok}
	if grouped {
		decl.Lparen, decl.Rparen = e.pos(n)+gotoken.Pos(len(tok.String())), e.end(n)-1
	}
	for _, spec := range specs {
		value := &goast.ValueSpec{Type: e.typ(spec.Type), Values: e.exprs(spec.Values)}
		for _, name := range spec.Names {
			value.Names = append(value.Names, e.ident(name))
		}
		decl.Specs = append(decl.Specs, value)
	}
	return decl
}
