package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) enumValid(call *syntax.CallExpr, instance *check.Instance) goast.Expr {
	if instance.Enum.Builtin {
		// The tag accessor infers private payload types without naming them here.
		// Evaluate the argument before entering the closure, preserving recover.
		tag := e.fresh("tag")
		return &goast.CallExpr{Fun: &goast.FuncLit{
			Type: &goast.FuncType{Params: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{tag}, Type: e.sumMember("Tag")}}}, Results: e.enumValidResult()},
			Body: e.enumValidBody(instance, tag, nil),
		}, Args: []goast.Expr{&goast.CallExpr{Fun: e.sumMember("Tag" + instance.Enum.Name), Args: []goast.Expr{e.expr(call.Args[0])}}}}
	}
	if e.enumValidNeedsHelper(instance) {
		e.enumValidHelpers[instance.Enum] = true
		var function goast.Expr = goast.NewIdent(e.enumValidHelperName(instance.Enum))
		pkg := instance.Enum.Type.Obj().Pkg()
		if !e.localTypePackage(pkg) {
			path := canonicalPackage(e.info, pkg)
			function = &goast.SelectorExpr{X: e.importName(strings.Split(path, "/"), pkg.Name(), 0), Sel: function.(*goast.Ident)}
		}
		// Let Go infer every type argument, including private payload types.
		arguments := []goast.Expr{e.expr(call.Args[0])}
		if e.enumValidBoolShadowed(instance.Enum) {
			arguments = append(arguments, e.rawExpr(&syntax.BoolLit{Value: true}), e.rawExpr(&syntax.BoolLit{Value: false}))
		}
		return &goast.CallExpr{Fun: function, Args: arguments}
	}
	function := e.enumValidFunction(instance)
	return &goast.CallExpr{Fun: function, Args: []goast.Expr{e.expr(call.Args[0])}}
}

func (e *emitter) enumValidNeedsHelper(instance *check.Instance) bool {
	if instance.Enum.Builtin {
		return false
	}
	if !e.localTypePackage(instance.Enum.Type.Obj().Pkg()) {
		if !instance.Enum.Type.Obj().Exported() {
			return true
		}
		for _, variant := range instance.Enum.Variants {
			if !gotoken.IsExported(variant.GoName) {
				return true
			}
		}
	}
	for _, argument := range instance.Args {
		if !e.nameableType(argument) {
			return true
		}
	}
	return false
}

func (e *emitter) enumValidHelperName(enum *check.Enum) string {
	base := "GoxideInternalEnumValid_" + enum.GoName
	reserved := map[string]bool{}
	for _, file := range e.result.Files {
		if filepath.ToSlash(filepath.Dir(file.Name)) != enum.Type.Obj().Pkg().Path() || file.AST.Package.Name != enum.Type.Obj().Pkg().Name() {
			continue
		}
		for _, symbol := range file.Definitions {
			reserved[symbol.GoName] = true
		}
	}
	for _, other := range e.info.Enums {
		if other != enum && other.Type.Obj().Pkg() == enum.Type.Obj().Pkg() {
			reserved["GoxideInternalEnumValid_"+other.GoName] = true
		}
	}
	name := base
	for index := 1; reserved[name]; index++ {
		name = base + "_" + strconv.Itoa(index)
	}
	return name
}

// Native Go package declarations cannot be renamed to make bool accessible.
// In that case the caller supplies the two results and Go infers their type.
func (e *emitter) enumValidBoolShadowed(enum *check.Enum) bool {
	for index := 0; index < enum.Type.TypeParams().Len(); index++ {
		if enum.Type.TypeParams().At(index).Obj().Name() == "bool" {
			return true
		}
	}
	for _, pkg := range e.result.Packages {
		if filepath.ToSlash(pkg.Dir) != enum.Type.Obj().Pkg().Path() || pkg.Name != enum.Type.Obj().Pkg().Name() {
			continue
		}
		for _, symbol := range pkg.Scope.Bindings {
			if symbol.GoName == "bool" {
				return true
			}
		}
	}
	return false
}

func (e *emitter) enumValidDeclarations() (decls []goast.Decl) {
	for _, item := range e.tree.Items {
		declaration, ok := item.(*syntax.EnumDecl)
		if !ok {
			continue
		}
		enum := e.info.Enums[declaration]
		if !e.enumValidHelpers[enum] {
			continue
		}
		name := e.enumValidHelperName(enum)
		if !e.generatedName(name, declaration) {
			continue
		}
		instance := &check.Instance{Enum: enum}
		for index := 0; index < enum.Type.TypeParams().Len(); index++ {
			instance.Args = append(instance.Args, enum.Type.TypeParams().At(index))
		}
		var function *goast.FuncLit
		parameters := e.enumGenerics(declaration.Generics)
		if e.enumValidBoolShadowed(enum) {
			if parameters == nil {
				parameters = &goast.FieldList{}
			}
			typ, value, yes, no := e.fresh("validType"), e.fresh("enum"), e.fresh("yes"), e.fresh("no")
			parameters.List = append(parameters.List, &goast.Field{Names: []*goast.Ident{typ}, Type: &goast.InterfaceType{Methods: &goast.FieldList{}}})
			function = &goast.FuncLit{
				Type: &goast.FuncType{
					Params: &goast.FieldList{List: []*goast.Field{
						{Names: []*goast.Ident{value}, Type: e.instanceType(instance)},
						{Names: []*goast.Ident{yes, no}, Type: typ},
					}},
					Results: &goast.FieldList{List: []*goast.Field{{Type: typ}}},
				},
				Body: e.enumValidBody(instance, value, map[bool]goast.Expr{true: yes, false: no}),
			}
		} else {
			function = e.enumValidFunction(instance)
		}
		function.Type.TypeParams = parameters
		result := &goast.FuncDecl{Name: goast.NewIdent(name), Type: function.Type, Body: function.Body}
		e.anchor(result, declaration)
		decls = append(decls, result)
	}
	return decls
}

func (e *emitter) enumValidFunction(instance *check.Instance) *goast.FuncLit {
	value := e.fresh("enum")
	return &goast.FuncLit{
		Type: &goast.FuncType{
			Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{value}, Type: e.instanceType(instance)}}},
			Results: e.enumValidResult(),
		},
		Body: e.enumValidBody(instance, value, nil),
	}
}

func (e *emitter) enumValidResult() *goast.FieldList {
	return &goast.FieldList{List: []*goast.Field{{Type: e.goType(types.Typ[types.Bool])}}}
}

func (e *emitter) enumValidBody(instance *check.Instance, value goast.Expr, outcomes map[bool]goast.Expr) *goast.BlockStmt {
	boolean := func(value bool) goast.Expr {
		if outcomes != nil {
			return outcomes[value]
		}
		return e.rawExpr(&syntax.BoolLit{Value: value})
	}
	returns := func(result goast.Expr) []goast.Stmt {
		return []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{result}}}
	}
	body := &goast.BlockStmt{}
	if instance.Enum.Builtin {
		var tags []goast.Expr
		for _, variant := range instance.Enum.Variants {
			tags = append(tags, integer(variantTag(variant.Name)))
		}
		body.List = append(body.List, &goast.SwitchStmt{
			Tag: value,
			Body: &goast.BlockStmt{List: []goast.Stmt{
				&goast.CaseClause{List: tags, Body: returns(boolean(true))},
			}},
		})
	} else if len(instance.Enum.Variants) != 0 {
		variant := e.fresh("variant")
		cases := &goast.BlockStmt{}
		for _, declaration := range instance.Enum.Variants {
			zero := e.fresh("nilVariant")
			typ := &goast.StarExpr{X: e.variantType(instance, declaration)}
			valid := &goast.BinaryExpr{X: variant, Op: gotoken.NEQ, Y: zero}
			result := returns(valid)
			if outcomes != nil {
				result = []goast.Stmt{&goast.IfStmt{Cond: valid, Body: &goast.BlockStmt{List: returns(boolean(true))}}}
				result = append(result, returns(boolean(false))...)
			}
			cases.List = append(cases.List, &goast.CaseClause{
				List: []goast.Expr{typ},
				Body: append([]goast.Stmt{&goast.DeclStmt{Decl: &goast.GenDecl{Tok: gotoken.VAR, Specs: []goast.Spec{&goast.ValueSpec{Names: []*goast.Ident{zero}, Type: typ}}}}}, result...),
			})
		}
		body.List = append(body.List, &goast.TypeSwitchStmt{
			Assign: &goast.AssignStmt{Lhs: []goast.Expr{variant}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{&goast.TypeAssertExpr{X: value}}},
			Body:   cases,
		})
	}
	body.List = append(body.List, returns(boolean(false))...)
	return body
}
