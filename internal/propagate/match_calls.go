package propagate

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// Parameter contexts come only from the actual typed call. Field functions,
// bound methods, method expressions, returned functions and instantiated
// generic functions all share this path, including variadic slice expansion.
func (l *lowering) matchArgumentType(call *ast.CallExpr, index int) (types.Type, error) {
	tv := l.info.Types[call.Fun]
	if tv.IsType() || tv.IsBuiltin() || tv.Type == nil {
		return nil, fmt.Errorf("match argument requires a resolved function signature; conversions and builtins are unsupported")
	}
	sig, ok := tv.Type.Underlying().(*types.Signature)
	if !ok || sig.TypeParams().Len() != 0 {
		return nil, fmt.Errorf("match argument requires a concrete function signature")
	}
	// Compiler directives may attach escape/liveness contracts to direct call
	// operands. Keep the prior explicit boundary, resolving by object identity.
	var object types.Object
	base := ast.Unparen(call.Fun)
	switch x := base.(type) {
	case *ast.IndexExpr:
		base = ast.Unparen(x.X)
	case *ast.IndexListExpr:
		base = ast.Unparen(x.X)
	}
	switch x := base.(type) {
	case *ast.Ident:
		object = l.info.ObjectOf(x)
	case *ast.SelectorExpr:
		object = l.info.ObjectOf(x.Sel)
	}
	if _, ok := object.(*types.Func); ok {
		for _, file := range append([]*ast.File{l.file}, l.companions...) {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || l.info.Defs[fn.Name] != object || fn.Doc == nil {
					continue
				}
				for _, comment := range fn.Doc.List {
					if strings.HasPrefix(comment.Text, "//go:") {
						return nil, fmt.Errorf("match arguments on compiler-directed functions are unsupported")
					}
				}
			}
		}
	}
	count := sig.Params().Len()
	if !sig.Variadic() {
		if call.Ellipsis.IsValid() || len(call.Args) != count {
			return nil, fmt.Errorf("match argument call has invalid non-variadic arity")
		}
		return sig.Params().At(index).Type(), nil
	}
	if call.Ellipsis.IsValid() {
		if len(call.Args) != count {
			return nil, fmt.Errorf("match argument spread call must supply fixed parameters and one variadic slice")
		}
		return sig.Params().At(index).Type(), nil
	}
	if len(call.Args) < count-1 {
		return nil, fmt.Errorf("match argument call is missing fixed parameters")
	}
	if index < count-1 {
		return sig.Params().At(index).Type(), nil
	}
	return sig.Params().At(count - 1).Type().(*types.Slice).Elem(), nil
}
