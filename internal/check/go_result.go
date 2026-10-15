package check

import (
	"go/types"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

type GoResult struct {
	Call                  *ast.CallExpr
	Value, Error, Payload types.Type
	Result                types.Type
	Unit, Keep            bool
}

func (c *checker) goResult(call *ast.CallExpr, keep bool) types.Type {
	fail := func(message string) types.Type {
		c.diag(call, "go-result", "Go result macro: "+message)
		return types.Typ[types.Invalid]
	}
	if len(call.Args) < 1 || len(call.Args) > 2 {
		return fail("expected E or exactly (T, E); wrap multiple values in a struct")
	}
	argument := call.Args[len(call.Args)-1]
	for {
		paren, ok := argument.(*ast.ParenExpr)
		if !ok {
			break
		}
		argument = paren.X
	}
	native, ok := argument.(*ast.CallExpr)
	if !ok {
		return fail("operand must be a call returning E or (T, E)")
	}
	t := c.expr(native, nil)
	if !valid(t) {
		return fail("cannot resolve native call signature")
	}
	var results []types.Type
	if tuple, ok := t.(*types.Tuple); ok {
		results = tupleTypes(tuple)
	} else {
		results = []types.Type{t}
	}
	if len(results) < 1 || len(results) > 2 || len(call.Args) == 2 && len(results) != 2 {
		return fail("expected E or exactly (T, E); explicit T requires two results; wrap multiple values in a struct")
	}
	value := types.Type(types.NewStruct(nil, nil))
	if len(results) == 2 {
		value = results[0]
	}
	failure := results[len(results)-1]
	if !types.Implements(failure, types.Universe.Lookup("error").Type().Underlying().(*types.Interface)) {
		return fail("last result E must implement the predeclared error interface")
	}
	switch failure.Underlying().(type) {
	case *types.Interface, *types.Pointer, *types.Slice, *types.Map, *types.Signature, *types.Chan:
	default:
		return fail("error result E must be nilable; a non-nilable error has no implicit success value")
	}
	if len(call.Args) == 2 && (!c.isTypeExpr(call.Args[0]) || !types.Identical(c.exprType(call.Args[0]), value)) {
		return fail("explicit T must be identical to the first result type")
	}
	value, failure = c.exportedGoType(value), c.exportedGoType(failure)
	if !c.packageNameable(value) {
		return fail("T must be nameable at package scope")
	}
	if !c.packageNameable(failure) {
		return fail("E must be nameable at package scope")
	}
	if c.file.Scope.Parent.Bindings[resolve.Key{Name: "nil"}] != nil {
		return fail("package declaration shadows the predeclared nil required by adapter checking")
	}
	payload := failure
	if keep {
		payload = types.NewStruct([]*types.Var{
			types.NewField(0, nil, "Value", value, false),
			types.NewField(0, nil, "Err", failure, false),
		}, nil)
	}
	result := instanceType(&Instance{Enum: c.builtins["Result"], Args: []types.Type{value, payload}})
	c.out.GoResults[call] = &GoResult{Call: native, Value: value, Error: failure, Payload: payload, Result: result, Unit: len(results) == 1, Keep: keep}
	return result
}

// A Go function may spell a private named result even when its package exposes
// an identical public alias. Emit that alias so the adapter remains nameable.
func (c *checker) exportedGoType(t types.Type) types.Type {
	switch typ := t.(type) {
	case *types.Named:
		object := typ.Obj()
		if object.Pkg() != nil && !object.Exported() {
			var best *types.Alias
			consider := func(candidate types.Type) {
				alias, ok := candidate.(*types.Alias)
				if ok && alias.Obj().Pkg() == object.Pkg() && alias.Obj().Exported() && types.Identical(alias, t) && (best == nil || alias.Obj().Name() < best.Obj().Name()) {
					best = alias
				}
			}
			for _, name := range object.Pkg().Scope().Names() {
				if candidate, ok := object.Pkg().Scope().Lookup(name).(*types.TypeName); ok {
					consider(candidate.Type())
				}
			}
			// Native declarations in a source graph share checker types before
			// a Go package scope is materialized. Only package aliases qualify.
			for _, pkg := range c.out.Resolve.Packages {
				if pkg.Dir == object.Pkg().Path() && pkg.Name == object.Pkg().Name() {
					for _, symbol := range pkg.Scope.Bindings {
						consider(c.out.Symbols[symbol])
					}
				}
			}
			if best != nil {
				return best
			}
		}
	case *types.Pointer:
		return types.NewPointer(c.exportedGoType(typ.Elem()))
	case *types.Slice:
		return types.NewSlice(c.exportedGoType(typ.Elem()))
	case *types.Array:
		return types.NewArray(c.exportedGoType(typ.Elem()), typ.Len())
	case *types.Map:
		return types.NewMap(c.exportedGoType(typ.Key()), c.exportedGoType(typ.Elem()))
	case *types.Chan:
		return types.NewChan(typ.Dir(), c.exportedGoType(typ.Elem()))
	}
	return t
}

func (c *checker) packageNameable(t types.Type) bool {
	var object *types.TypeName
	switch t := t.(type) {
	case *types.Named:
		object = t.Obj()
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if !c.packageNameable(t.TypeArgs().At(i)) {
				return false
			}
		}
	case *types.Alias:
		object = t.Obj()
		for i := 0; i < t.TypeArgs().Len(); i++ {
			if !c.packageNameable(t.TypeArgs().At(i)) {
				return false
			}
		}
	case *types.Pointer:
		return c.packageNameable(t.Elem())
	case *types.Slice:
		return c.packageNameable(t.Elem())
	case *types.Array:
		return c.packageNameable(t.Elem())
	case *types.Map:
		return c.packageNameable(t.Key()) && c.packageNameable(t.Elem())
	case *types.Chan:
		return c.packageNameable(t.Elem())
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !c.packageNameable(t.Field(i).Type()) {
				return false
			}
		}
	case *types.Signature:
		return t.TypeParams().Len() == 0 && c.packageNameable(t.Params()) && c.packageNameable(t.Results())
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			if !c.packageNameable(t.At(i).Type()) {
				return false
			}
		}
	case *types.TypeParam:
		return false
	}
	if object == nil || object.Pkg() == nil {
		return true
	}
	if object.Pkg().Path() != c.pkg.Path() {
		return object.Exported()
	}
	for _, symbol := range c.file.Scope.Parent.Bindings {
		if typ := c.out.Symbols[symbol]; typ != nil && types.Identical(typ, t) {
			return true
		}
	}
	return false
}
