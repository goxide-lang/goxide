package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) decl(n syntax.Item) (out goast.Decl) {
	defer func() { e.macroLayout(n, out); e.anchor(out, n) }()
	if n == nil {
		return nil
	}
	switch n := n.(type) {
	case *syntax.UseDecl:
		return e.useDecl(n)
	case *syntax.FuncDecl:
		ordinary := false
		if e.info.Demands != nil && !e.info.Demands.FunctionTemplates[n] {
			ordinary = true
			for _, body := range e.info.Demands.Bodies[n] {
				if body.Function == nil && body.Impl == nil && body.Info != nil {
					previousInfo, previousBody := e.info, e.instanceBody
					e.info, e.instanceBody = body.Info, body
					defer func() { e.info, e.instanceBody = previousInfo, previousBody }()
					break
				}
			}
		}
		if n.Mods.Unsafe || n.Mods.Async || n.Mods.Const {
			e.fail(n, "函数修饰符尚未降低")
		}
		if n.Path != nil {
			e.fail(n.Path, "限定路径函数声明尚未降低")
			return nil
		}
		signature, _ := e.info.Symbols[e.file.Definitions[n.Name.ID()]].(*types.Signature)
		if function := e.traits.functions[signature]; function != nil && e.traits.templateFunctions[n] {
			if hasConstParameters(n.Generics) {
				e.itemExtras = append(e.itemExtras, e.constFunctionDeclarations(function)...)
			} else if e.traits.helperFunctions[n] {
				e.itemExtras = append(e.itemExtras, e.templateFunctionDeclaration(function)...)
			}
			e.itemExtras = append(e.itemExtras, e.instanceDeclarations(n)...)
		}
		if !ordinary && e.info.Demands != nil && e.info.Demands.FunctionTemplates[n] {
			if e.traits.legacyFunctions[n] {
				return e.traitFunctionDeclaration(n)
			}
			return nil
		}
		typ := &goast.FuncType{Func: e.pos(n), TypeParams: e.functionTypeParameters(n), Params: e.params(n.Params)}
		if len(n.Results) != 0 {
			typ.Results = e.params(n.Results)
		}
		decl := &goast.FuncDecl{Name: e.ident(n.Name), Type: typ, Body: e.functionBody(typ, n.Body)}
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
		if e.info.Modules[n] == nil {
			e.fail(n, "mod 声明尚未降低")
		}
	case *syntax.EnumDecl:
		e.fail(n, "enum 声明尚未降低")
	case *syntax.TraitDecl:
		return e.traitDecl(n)
	case *syntax.ImplDecl:
		if e.deriveImpl(n) || e.info.Impls[n] != nil {
			return nil
		}
		e.fail(n, "impl 声明尚未降低")
	case *syntax.DeclareDecl:
		if impl, ok := n.Decl.(*syntax.ImplDecl); ok && e.info.Impls[impl] != nil {
			return nil
		}
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
		value := &goast.ValueSpec{Type: e.typ(spec.Type)}
		for _, expr := range spec.Values {
			before, lowered := e.exprScoped(expr)
			if len(before) > 0 {
				// A package initializer has no surrounding statement block.
				before = append(before, &goast.ReturnStmt{Results: []goast.Expr{lowered}})
				lowered = &goast.CallExpr{Fun: &goast.FuncLit{Type: &goast.FuncType{Params: &goast.FieldList{}, Results: &goast.FieldList{List: []*goast.Field{{Type: e.goType(e.info.Types[expr])}}}}, Body: &goast.BlockStmt{List: before}}}
			}
			value.Values = append(value.Values, lowered)
		}
		if value.Type == nil && len(spec.Values) == 1 && len(spec.Names) == 1 {
			if instance := e.info.EnumOf(e.info.Types[spec.Values[0]]); instance != nil {
				value.Type = e.instanceType(instance)
			}
		}
		for _, name := range spec.Names {
			value.Names = append(value.Names, e.ident(name))
		}
		decl.Specs = append(decl.Specs, value)
	}
	return decl
}
