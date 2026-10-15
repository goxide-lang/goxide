package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

// Inferred temporaries can retain a foreign private type without spelling it.
func (e *emitter) nameableType(t types.Type) bool {
	switch t := t.(type) {
	case *types.Named:
		if t.Obj().Pkg() != nil && !t.Obj().Exported() && !e.localTypePackage(t.Obj().Pkg()) {
			return e.exportedTypeAlias(t) != nil
		}
		for index := 0; index < t.TypeArgs().Len(); index++ {
			if !e.nameableType(t.TypeArgs().At(index)) {
				return false
			}
		}
	case *types.Alias:
		if t.Obj().Pkg() != nil && !t.Obj().Exported() && !e.localTypePackage(t.Obj().Pkg()) {
			return false
		}
		for index := 0; index < t.TypeArgs().Len(); index++ {
			if !e.nameableType(t.TypeArgs().At(index)) {
				return false
			}
		}
	case *types.Pointer:
		return e.nameableType(t.Elem())
	case *types.Array:
		return e.nameableType(t.Elem())
	case *types.Slice:
		return e.nameableType(t.Elem())
	case *types.Map:
		return e.nameableType(t.Key()) && e.nameableType(t.Elem())
	case *types.Chan:
		return e.nameableType(t.Elem())
	case *types.Signature:
		return e.nameableType(t.Params()) && e.nameableType(t.Results())
	case *types.Tuple:
		for index := 0; index < t.Len(); index++ {
			if !e.nameableType(t.At(index).Type()) {
				return false
			}
		}
	case *types.Struct:
		for index := 0; index < t.NumFields(); index++ {
			field := t.Field(index)
			if !field.Exported() && field.Pkg() != nil && !e.localTypePackage(field.Pkg()) || !e.nameableType(field.Type()) {
				return false
			}
		}
	case *types.Interface:
		for index := 0; index < t.NumMethods(); index++ {
			method := t.Method(index)
			if !method.Exported() && method.Pkg() != nil && !e.localTypePackage(method.Pkg()) || !e.nameableType(method.Type()) {
				return false
			}
		}
	}
	return true
}

func (e *emitter) exportedTypeAlias(t types.Type) types.Type {
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Exported() || e.localTypePackage(named.Obj().Pkg()) {
		return nil
	}
	var best types.Type
	name := ""
	consider := func(candidate types.Type) {
		alias, ok := candidate.(*types.Alias)
		if ok && alias.Obj().Exported() && alias.Obj().Pkg() == named.Obj().Pkg() && types.Identical(alias, t) && (name == "" || alias.Obj().Name() < name) {
			best, name = alias, alias.Obj().Name()
		}
	}
	for _, exported := range named.Obj().Pkg().Scope().Names() {
		if object, ok := named.Obj().Pkg().Scope().Lookup(exported).(*types.TypeName); ok {
			consider(object.Type())
		}
	}
	for _, typ := range e.info.Symbols {
		consider(typ)
	}
	return best
}

// A static function signature supplies a private result type without executing
// the function, reading a destination, or changing short-circuit side effects.
func (e *emitter) resultTypeWitness(expr syntax.Expr, want types.Type) (syntax.Expr, int) {
	switch expr := expr.(type) {
	case *syntax.ParenExpr:
		return e.resultTypeWitness(expr.X, want)
	case *syntax.UnaryExpr:
		if expr.Op == token.NOT {
			return e.resultTypeWitness(expr.X, want)
		}
	case *syntax.BinaryExpr:
		if expr.Op == token.LAND || expr.Op == token.LOR {
			if fun, count := e.resultTypeWitness(expr.X, want); fun != nil {
				return fun, count
			}
			return e.resultTypeWitness(expr.Y, want)
		}
	case *syntax.CallExpr:
		fun := expr.Fun
		for {
			paren, ok := fun.(*syntax.ParenExpr)
			if !ok {
				break
			}
			fun = paren.X
		}
		symbol := e.valueSymbol(fun)
		if symbol == nil || symbol.Kind != resolve.Function {
			break
		}
		sig, ok := e.info.Types[expr.Fun].(*types.Signature)
		if ok && sig.Recv() == nil && !sig.Variadic() && sig.TypeParams().Len() == 0 && sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), want) {
			return expr.Fun, sig.Params().Len()
		}
	}
	return nil, 0
}

func (e *emitter) zeroFromFunction(fun syntax.Expr, count int) goast.Expr {
	if e.zeroFunctions == nil {
		e.zeroFunctions = map[int]*goast.Ident{}
	}
	helper := e.zeroFunctions[count]
	if helper == nil {
		hash := sha256.Sum256([]byte(e.name))
		helper = e.fresh(fmt.Sprintf("Zero%x_", hash[:4]))
		e.zeroFunctions[count] = helper
		params := &goast.FieldList{}
		generics := &goast.FieldList{}
		for index := 0; index < count; index++ {
			name := fmt.Sprintf("A%d", index)
			generics.List = append(generics.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent(name)}, Type: goast.NewIdent("any")})
			params.List = append(params.List, &goast.Field{Type: goast.NewIdent(name)})
		}
		generics.List = append(generics.List, &goast.Field{Names: []*goast.Ident{goast.NewIdent("R")}, Type: goast.NewIdent("any")})
		results := func() *goast.FieldList { return &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent("R")}}} }
		fnType := &goast.FuncType{Params: params, Results: results()}
		zero := goast.NewIdent("zero")
		decl := &goast.FuncDecl{Name: helper, Type: &goast.FuncType{TypeParams: generics,
			Params: &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{goast.NewIdent("_")}, Type: fnType}}}, Results: results()},
			Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(zero, goast.NewIdent("R"), nil), &goast.ReturnStmt{Results: []goast.Expr{zero}}}}}
		e.flowHelpers = append(e.flowHelpers, decl)
	}
	return &goast.CallExpr{Fun: helper, Args: []goast.Expr{e.expr(fun)}}
}

func (e *emitter) valueSymbol(expr syntax.Expr) *resolve.Symbol {
	ref := e.file.References[expr.ID()]
	if path, ok := expr.(*syntax.PathExpr); ok {
		ref = e.file.References[path.Path.ID()]
	}
	symbol := ref.Symbol
	for symbol != nil && symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	return symbol
}

// Contextual types do not make an untyped constant or comparison a typed Go
// value. Runtime operands and genuinely typed constants retain their type in :=.
func (e *emitter) typedLogicalValue(expr syntax.Expr) bool {
	switch expr := expr.(type) {
	case *syntax.ParenExpr:
		return e.typedLogicalValue(expr.X)
	case *syntax.UnaryExpr:
		return expr.Op == token.NOT && e.typedLogicalValue(expr.X)
	case *syntax.BinaryExpr:
		return (expr.Op == token.LAND || expr.Op == token.LOR) && (e.typedLogicalValue(expr.X) || e.typedLogicalValue(expr.Y))
	case *syntax.CallExpr, *syntax.SelectorExpr, *syntax.IndexExpr, *syntax.StarExpr, *syntax.TryExpr:
		return true
	case *syntax.Ident, *syntax.PathExpr:
		symbol := e.valueSymbol(expr)
		if symbol == nil {
			return false
		}
		if symbol.Kind == resolve.Variable {
			return true
		}
		if symbol.Kind == resolve.Constant {
			typ := e.info.Symbols[symbol]
			if typ == nil && len(symbol.Target) >= 2 && e.info.GoImporter != nil {
				if pkg, err := e.info.GoImporter.Import(strings.Join(symbol.Target[:len(symbol.Target)-1], "/")); err == nil {
					if object := pkg.Scope().Lookup(symbol.Target[len(symbol.Target)-1]); object != nil {
						typ = object.Type()
					}
				}
			}
			if typ != nil {
				basic, ok := types.Unalias(typ).(*types.Basic)
				return !ok || basic.Info()&types.IsUntyped == 0
			}
		}
	}
	return false
}

// Taking the address of a resolved variable supplies its type without reading
// it. Strip dereferences only in the witness: no pointer value is loaded early.
func (e *emitter) variableTypeWitness(expr syntax.Expr, want types.Type) (syntax.Expr, int) {
	switch n := expr.(type) {
	case *syntax.ParenExpr:
		return e.variableTypeWitness(n.X, want)
	case *syntax.UnaryExpr:
		if n.Op == token.NOT {
			return e.variableTypeWitness(n.X, want)
		}
	case *syntax.BinaryExpr:
		if n.Op == token.LAND || n.Op == token.LOR {
			if variable, count := e.variableTypeWitness(n.X, want); variable != nil {
				return variable, count
			}
			return e.variableTypeWitness(n.Y, want)
		}
	}
	if !types.Identical(e.info.Types[expr], want) {
		return nil, 0
	}
	count := 0
	for {
		switch n := expr.(type) {
		case *syntax.ParenExpr:
			expr = n.X
		case *syntax.StarExpr:
			count++
			expr = n.X
		case *syntax.Ident, *syntax.PathExpr:
			if symbol := e.valueSymbol(expr); symbol != nil && symbol.Kind == resolve.Variable {
				return expr, count
			}
			return nil, 0
		default:
			return nil, 0
		}
	}
}

func (e *emitter) zeroFromVariable(variable syntax.Expr, count int) goast.Expr {
	if e.zeroPointers == nil {
		e.zeroPointers = map[int]*goast.Ident{}
	}
	helper := e.zeroPointers[count]
	if helper == nil {
		hash := sha256.Sum256([]byte(e.name))
		helper = e.fresh(fmt.Sprintf("ZeroPointer%x_", hash[:4]))
		e.zeroPointers[count] = helper
		result := goast.NewIdent("R")
		generics := &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{result}, Type: goast.NewIdent("any")}}}
		var element goast.Expr = result
		for index := 0; index < count; index++ {
			name := goast.NewIdent(fmt.Sprintf("P%d", index))
			generics.List = append(generics.List, &goast.Field{Names: []*goast.Ident{name}, Type: &goast.UnaryExpr{Op: gotoken.TILDE, X: &goast.StarExpr{X: element}}})
			element = name
		}
		zero := goast.NewIdent("zero")
		decl := &goast.FuncDecl{Name: helper, Type: &goast.FuncType{TypeParams: generics,
			Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{goast.NewIdent("_")}, Type: &goast.StarExpr{X: element}}}},
			Results: &goast.FieldList{List: []*goast.Field{{Type: result}}}},
			Body: &goast.BlockStmt{List: []goast.Stmt{varStmt(zero, result, nil), &goast.ReturnStmt{Results: []goast.Expr{zero}}}}}
		e.flowHelpers = append(e.flowHelpers, decl)
	}
	return &goast.CallExpr{Fun: helper, Args: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: e.expr(variable)}}}
}

func (e *emitter) logicalTemporary(expr *syntax.BinaryExpr, left goast.Expr) *goast.Ident {
	out := e.fresh("Bool")
	typ := e.info.Types[expr]
	if e.untypedBoolean(expr) {
		typ = types.Typ[types.Bool]
	}
	if e.nameableType(typ) {
		e.pending = append(e.pending, varStmt(out, e.goType(typ), left))
		return out
	}
	if types.Identical(e.info.Types[expr.X], typ) && e.typedLogicalValue(expr.X) {
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{out}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{left}})
		return out
	}
	if fun, count := e.resultTypeWitness(expr, typ); fun != nil {
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{out}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{e.zeroFromFunction(fun, count)}}, assign(out, left))
		return out
	}
	if variable, count := e.variableTypeWitness(expr, typ); variable != nil {
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{out}, Tok: gotoken.DEFINE, Rhs: []goast.Expr{e.zeroFromVariable(variable, count)}}, assign(out, left))
		return out
	}
	e.fail(expr, "cannot preserve the private logical result type")
	return out
}

func (e *emitter) untypedBoolean(expr syntax.Expr) bool {
	switch expr := expr.(type) {
	case *syntax.ParenExpr:
		return e.untypedBoolean(expr.X)
	case *syntax.UnaryExpr:
		return expr.Op == token.NOT && e.untypedBoolean(expr.X)
	case *syntax.BinaryExpr:
		switch expr.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return true
		case token.LAND, token.LOR:
			return e.untypedBoolean(expr.X) && e.untypedBoolean(expr.Y)
		}
	case *syntax.Ident, *syntax.PathExpr:
		if symbol := e.valueSymbol(expr); symbol != nil && symbol.Kind == resolve.Constant && !e.typedLogicalValue(expr) {
			if typ := e.info.Types[expr]; typ != nil {
				basic, ok := typ.Underlying().(*types.Basic)
				return ok && basic.Info()&types.IsBoolean != 0
			}
		}
	}
	return false
}
