package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) typeinfo(name string) goast.Expr {
	pkg := e.importName([]string{"github.com", "goxide-lang", "std", "typeinfo"}, "typeinfo", 0)
	return &goast.SelectorExpr{X: pkg, Sel: goast.NewIdent(name)}
}

func (e *emitter) functionGenerics(fn *syntax.FuncDecl) *goast.FieldList {
	bounds := e.info.GenericBounds[fn]
	if len(bounds) == 0 {
		return e.generics(fn.Generics)
	}
	byName := map[string]*check.TraitBound{}
	used := map[string]bool{}
	for _, p := range fn.Generics.Params {
		for _, name := range p.Names {
			used[e.ident(name).Name] = true
		}
	}
	for _, bound := range bounds {
		byName[bound.Param.Obj().Name()] = bound
		for _, p := range bound.Projected {
			used[p.Obj().Name()] = true
		}
	}
	ops := map[*check.TraitBound]string{}
	for _, bound := range bounds {
		base := "Goxide" + exportedName(bound.Param.Obj().Name()) + "Ops"
		name := base
		for index := 2; used[name] || e.reserved[name]; index++ {
			name = fmt.Sprintf("%s%d", base, index)
		}
		used[name] = true
		e.reserved[name] = true
		ops[bound] = name
	}
	list := &goast.FieldList{}
	for _, p := range fn.Generics.Params {
		for _, id := range p.Names {
			name := e.ident(id)
			var constraint goast.Expr
			if bound := byName[id.Name]; bound != nil {
				constraint = enumIndices(e.typeinfo("CarrierFor"), []goast.Expr{e.goType(bound.Param), goast.NewIdent(ops[bound])})
			} else {
				constraint = e.typ(p.Constraint)
				if constraint == nil {
					constraint = goast.NewIdent("any")
				}
			}
			list.List = append(list.List, &goast.Field{Names: []*goast.Ident{name}, Type: constraint})
		}
	}
	for _, bound := range bounds {
		methods := []*goast.Field{{Type: &goast.UnaryExpr{Op: gotoken.TILDE, X: &goast.StructType{Fields: &goast.FieldList{}}}}}
		for _, m := range bound.Trait.Methods {
			ft, _ := e.memberSignature(bound.MethodSignature(m))
			if !m.Static {
				receiver := types.Type(bound.Param)
				if m.Receiver != check.ValueRef {
					receiver = types.NewPointer(receiver)
				}
				ft.Params.List = append([]*goast.Field{{Names: []*goast.Ident{goast.NewIdent("self")}, Type: e.goType(receiver)}}, ft.Params.List...)
			}
			methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(traitOperation(m))}, Type: ft})
		}
		for _, a := range bound.Trait.Associated {
			methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(traitWitness(bound.Trait, a.Name))}, Type: resultFunc(e.goType(bound.Associated[a.Name]))})
		}
		list.List = append(list.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(ops[bound])}, Type: &goast.InterfaceType{Methods: &goast.FieldList{List: methods}}})
	}
	for _, bound := range bounds {
		for _, p := range bound.Projected {
			list.List = append(list.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(p.Obj().Name())}, Type: goast.NewIdent("any")})
		}
	}
	return list
}

func resultFunc(result goast.Expr) *goast.FuncType {
	return &goast.FuncType{Params: &goast.FieldList{}, Results: &goast.FieldList{List: []*goast.Field{{Type: result}}}}
}

// Forwarders give unnamed parameters local names but never invent result names.
func (e *emitter) memberSignature(sig *types.Signature) (*goast.FuncType, []goast.Expr) {
	ft := &goast.FuncType{Params: &goast.FieldList{}}
	var args []goast.Expr
	used := map[string]bool{"self": true}
	for index := 0; index < sig.Params().Len(); index++ {
		p := sig.Params().At(index)
		name := p.Name()
		if name == "" || name == "_" || used[name] {
			name = fmt.Sprintf("gxArg%d", index)
			for used[name] {
				name += "_"
			}
		}
		used[name] = true
		e.reserved[name] = true
		typ := e.goType(p.Type())
		if sig.Variadic() && index == sig.Params().Len()-1 {
			typ = &goast.Ellipsis{Elt: e.goType(p.Type().(*types.Slice).Elem())}
		}
		ft.Params.List = append(ft.Params.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: typ})
		args = append(args, goast.NewIdent(name))
	}
	if sig.Results().Len() != 0 {
		ft.Results = &goast.FieldList{}
		for index := 0; index < sig.Results().Len(); index++ {
			ft.Results.List = append(ft.Results.List, &goast.Field{Type: e.goType(sig.Results().At(index).Type())})
		}
	}
	return ft, args
}

func forwardBody(fun goast.Expr, args []goast.Expr, sig *types.Signature) *goast.BlockStmt {
	call := &goast.CallExpr{Fun: fun, Args: args}
	if sig.Variadic() {
		call.Ellipsis = 1
	}
	var statement goast.Stmt = &goast.ExprStmt{X: call}
	if sig.Results().Len() != 0 {
		statement = &goast.ReturnStmt{Results: []goast.Expr{call}}
	}
	return &goast.BlockStmt{List: []goast.Stmt{statement}}
}

func receiverField(name string, typ goast.Expr) *goast.FieldList {
	field := &goast.Field{Type: typ}
	if name != "" {
		field.Names = []*goast.Ident{goast.NewIdent(name)}
	}
	return &goast.FieldList{List: []*goast.Field{field}}
}

func typeDeclaration(name string, typ goast.Expr) goast.Decl {
	return &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{&goast.TypeSpec{Name: goast.NewIdent(name), Type: typ}}}
}
