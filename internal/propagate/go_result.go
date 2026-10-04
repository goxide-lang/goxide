package propagate

import (
	"go/ast"
	"go/types"
)

// A compiler-owned adapter literal is distinguished by reserved parameter
// identities, which source code cannot introduce. Checking the native call is
// necessary: mere assignability to error would box a nil *ConcreteError.
func (l *lowering) checkGoResults() error {
	var failure error
	ast.Inspect(l.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || failure != nil {
			return failure == nil
		}
		fun := call.Fun
		for {
			p, ok := fun.(*ast.ParenExpr)
			if !ok {
				break
			}
			fun = p.X
		}
		fn, ok := fun.(*ast.FuncLit)
		if !ok || len(fn.Type.Params.List) == 0 {
			return true
		}
		fields := fn.Type.Params.List
		last := fields[len(fields)-1]
		if len(last.Names) != 1 || last.Names[0].Name != "__hgo_go_result_error" {
			return true
		}
		bad := func(s string) bool { failure = l.fail(call, "Go result macro: "+s); return false }
		if len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return bad("expected a single native call")
		}
		arg := call.Args[0]
		for {
			p, ok := arg.(*ast.ParenExpr)
			if !ok {
				break
			}
			arg = p.X
		}
		native, ok := arg.(*ast.CallExpr)
		if !ok {
			return bad("operand must be a call returning error or (T, error)")
		}
		nativeType := l.info.TypeOf(native.Fun)
		if nativeType == nil {
			return bad("cannot resolve native call signature")
		}
		sig, ok := nativeType.Underlying().(*types.Signature)
		if !ok {
			return bad("operand must be a function call")
		}
		results := sig.Results()
		if results.Len() != len(fields) {
			return bad("expected error for (call), or exactly (T, error) for (T, call); wrap multiple values in a struct")
		}
		if !types.Identical(results.At(results.Len()-1).Type(), types.Universe.Lookup("error").Type()) {
			return bad("last result must be the predeclared error type (or its alias), not a concrete error type")
		}
		if len(fields) == 2 && !types.Identical(results.At(0).Type(), l.info.TypeOf(fields[0].Type)) {
			return bad("explicit T must be identical to the first result type")
		}
		if len(fields) == 2 && !types.Identical(results.At(0).Type(), l.resolveType(printNode(fields[0].Type))) {
			return bad("T must be nameable at package scope for the current concrete Result instance registry")
		}
		return true
	})
	return failure
}
