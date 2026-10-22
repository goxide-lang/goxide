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

// Existing Go-expressible protocol functions retain carrier inference. Each
// target uses one carrier proving all of its declared source bounds.
func (e *emitter) functionGenerics(fn *syntax.FuncDecl) *goast.FieldList {
	ordinary := e.functionTypeParameters(fn)
	bounds := e.info.GenericBounds[fn]
	if len(bounds) == 0 {
		return ordinary
	}
	if ordinary == nil {
		ordinary = &goast.FieldList{}
	}
	groups := e.functionEvidenceGroups(fn)
	signature, _ := e.info.Symbols[e.file.Definitions[fn.Name.ID()]].(*types.Signature)
	projected := projectedParameters(bounds)
	parameters := append(append([]*goast.Field(nil), ordinary.List...), projected...)
	for index, parameter := range parameters {
		var target *types.TypeParam
		if signature != nil && index < signature.TypeParams().Len() {
			target = signature.TypeParams().At(index)
		}
		for _, group := range groups {
			if group.param == nil || group.param != target {
				continue
			}
			carrier := enumIndices(e.typeinfo("CarrierFor"), []goast.Expr{e.goType(target), goast.NewIdent(group.name)})
			if id, ok := parameter.Type.(*goast.Ident); ok && id.Name == "any" {
				parameter.Type = carrier
			} else {
				parameter.Type = &goast.InterfaceType{Methods: &goast.FieldList{List: []*goast.Field{{Type: parameter.Type}, {Type: carrier}}}}
			}
		}
	}
	for _, group := range groups {
		ordinary.List = append(ordinary.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(group.name)}, Type: &goast.InterfaceType{Methods: &goast.FieldList{List: e.evidenceMethods(group.bounds)}}})
	}
	ordinary.List = append(ordinary.List, projected...)
	return ordinary
}

type functionEvidenceGroup struct {
	param  *types.TypeParam
	bounds []*check.TraitBound
	name   string
}

func (e *emitter) functionEvidenceGroups(fn *syntax.FuncDecl) []functionEvidenceGroup {
	bounds := e.info.GenericBounds[fn]
	e.registerEvidenceNames(bounds)
	var groups []functionEvidenceGroup
	byParam := map[*types.TypeParam]int{}
	for _, bound := range bounds {
		param, _ := traitBoundTarget(bound).(*types.TypeParam)
		if index, found := byParam[param]; param != nil && found && !parameterizedTrait(bound.Trait) {
			groups[index].bounds = append(groups[index].bounds, bound)
			continue
		}
		if param != nil && !parameterizedTrait(bound.Trait) {
			byParam[param] = len(groups)
		}
		groups = append(groups, functionEvidenceGroup{param: param, bounds: []*check.TraitBound{bound}, name: e.evidenceNames[bound]})
	}
	return groups
}

func (e *emitter) functionTypeParameters(fn *syntax.FuncDecl) *goast.FieldList {
	if fn.Generics == nil {
		return nil
	}
	if fn.Where == nil && len(e.info.GenericBounds[fn]) == 0 {
		return e.generics(fn.Generics)
	}
	symbol := e.file.Definitions[fn.Name.ID()]
	signature, _ := e.info.Symbols[symbol].(*types.Signature)
	list := &goast.FieldList{}
	index := 0
	for _, parameter := range fn.Generics.Params {
		if parameter.Const {
			continue
		}
		for _, name := range parameter.Names {
			var constraint goast.Expr
			if signature != nil && index < signature.TypeParams().Len() {
				constraint = e.goConstraint(signature.TypeParams().At(index).Constraint())
			} else {
				constraint = e.typ(parameter.Constraint)
			}
			if constraint == nil {
				constraint = goast.NewIdent("any")
			}
			list.List = append(list.List, &goast.Field{Names: []*goast.Ident{e.ident(name)}, Type: constraint})
			index++
		}
	}
	if len(list.List) == 0 {
		return nil
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
