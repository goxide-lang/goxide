package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) useDecl(decl *syntax.UseDecl) goast.Decl {
	var use *resolve.UseBinding
	for _, candidate := range e.file.Uses {
		if candidate.Decl.ID == decl.ID() {
			use = candidate
			break
		}
	}
	if use == nil || !use.Public || use.Blank || use.Glob || use.Symbol == nil {
		return nil
	}
	kind, typ, goName := e.exportedUse(use)
	if kind != resolve.Type && kind != resolve.Enum && kind != resolve.Trait && kind != resolve.Constant {
		return nil
	}
	if kind == resolve.Trait {
		for _, trait := range e.info.Traits {
			if trait.Type == typ && !trait.Runtime {
				return nil
			}
		}
	}
	var target goast.Expr
	owner := strings.Join(use.Target[:len(use.Target)-1], "/")
	if use.Symbol.AliasOf != nil || owner == filepath.ToSlash(filepath.Dir(e.name)) && !e.externalTest {
		target = goast.NewIdent(goName)
	} else {
		target = e.external(decl, use.Target, goName, "")
	}
	if kind == resolve.Constant {
		return &goast.GenDecl{TokPos: e.pos(decl), Tok: gotoken.CONST, Specs: []goast.Spec{&goast.ValueSpec{Names: []*goast.Ident{goast.NewIdent(use.Symbol.GoName)}, Values: []goast.Expr{target}}}}
	}
	if typ == nil {
		e.fail(decl, "无法确定公开 use 的类型")
		return nil
	}
	spec := &goast.TypeSpec{Name: goast.NewIdent(use.Symbol.GoName), Assign: e.pos(decl) + 1, Type: target}
	var params *types.TypeParamList
	switch named := typ.(type) {
	case *types.Named:
		params = named.TypeParams()
	case *types.Alias:
		params = named.TypeParams()
	}
	if params != nil && params.Len() != 0 {
		spec.TypeParams = &goast.FieldList{}
		var args []goast.Expr
		for index := 0; index < params.Len(); index++ {
			param := params.At(index)
			name := param.Obj().Name()
			spec.TypeParams.List = append(spec.TypeParams.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: e.goType(param.Constraint())})
			args = append(args, goast.NewIdent(name))
		}
		if len(args) == 1 {
			spec.Type = &goast.IndexExpr{X: target, Index: args[0]}
		} else {
			spec.Type = &goast.IndexListExpr{X: target, Indices: args}
		}
	}
	return &goast.GenDecl{TokPos: e.pos(decl), Tok: gotoken.TYPE, Specs: []goast.Spec{spec}}
}

func (e *emitter) exportedUse(use *resolve.UseBinding) (resolve.Kind, types.Type, string) {
	if symbol := use.Symbol.AliasOf; symbol != nil {
		for symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		return symbol.Kind, e.info.Symbols[symbol], symbol.GoName
	}
	target := strings.Join(use.Target, "/")
	for _, pkg := range e.result.Packages {
		if pkg.IsExternalTest() {
			continue
		}
		prefix := filepath.ToSlash(pkg.Dir) + "/"
		if !strings.HasPrefix(target, prefix) {
			continue
		}
		scope := pkg.Scope
		parts := strings.Split(strings.TrimPrefix(target, prefix), "/")
		var symbol *resolve.Symbol
		for _, part := range parts {
			if scope == nil {
				symbol = nil
				break
			}
			symbol = scope.Bindings[resolve.Key{Name: part}]
			if symbol == nil {
				break
			}
			scope = symbol.Members
		}
		if symbol != nil {
			return symbol.Kind, e.info.Symbols[symbol], symbol.GoName
		}
	}
	if len(use.Target) < 2 || e.info.GoImporter == nil {
		return resolve.Import, nil, ""
	}
	pkg, err := e.info.GoImporter.Import(strings.Join(use.Target[:len(use.Target)-1], "/"))
	if err != nil {
		return resolve.Import, nil, ""
	}
	object := pkg.Scope().Lookup(use.Target[len(use.Target)-1])
	switch object := object.(type) {
	case *types.TypeName:
		return resolve.Type, object.Type(), object.Name()
	case *types.Const:
		return resolve.Constant, object.Type(), object.Name()
	}
	return resolve.Import, nil, ""
}
