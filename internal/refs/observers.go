package refs

import (
	"go/ast"
	"go/types"
)

// EnumObserver is evidence for a generated value-only observation of a
// registered enum. Value observers identify the exact payload field so readonly
// shared projections survive the getter; this is not a general pure-call flag.
type EnumObserver struct {
	PackagePath, Function    string
	EnumTypeID, ResultTypeID string
	Kind, Field              string // Kind is validate, tag, or value.
}

type PermissionEvidence struct {
	EnumTypes map[string]bool
	Observers []EnumObserver
}

func (c *checker) enumObserver(call *ast.CallExpr) (EnumObserver, bool) {
	if len(call.Args) != 1 || call.Ellipsis.IsValid() {
		return EnumObserver{}, false
	}
	base := unparen(call.Fun)
	switch x := base.(type) {
	case *ast.IndexExpr:
		base = unparen(x.X)
	case *ast.IndexListExpr:
		base = unparen(x.X)
	}
	var id *ast.Ident
	switch x := base.(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	}
	fn, _ := c.info.Uses[id].(*types.Func)
	sig, _ := c.typ(call.Fun).(*types.Signature)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Scope().Lookup(fn.Name()) != fn || sig == nil || sig.Recv() != nil || sig.Variadic() || sig.Params().Len() != 1 {
		return EnumObserver{}, false
	}
	owner := types.Unalias(sig.Params().At(0).Type())
	named, ok := owner.(*types.Named)
	if !ok || !types.Identical(owner, c.typ(call.Args[0])) {
		return EnumObserver{}, false
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return EnumObserver{}, false
	}
	for _, proof := range c.observers {
		if proof.PackagePath != fn.Pkg().Path() || proof.Function != fn.Name() || proof.EnumTypeID != EnumTypeID(owner) {
			continue
		}
		switch proof.Kind {
		case "validate":
			if sig.Results().Len() == 0 {
				return proof, true
			}
		case "tag":
			if sig.Results().Len() == 1 && types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) && proof.ResultTypeID == "int" {
				return proof, true
			}
		case "value":
			if sig.Results().Len() != 1 || proof.ResultTypeID != EnumTypeID(sig.Results().At(0).Type()) {
				continue
			}
			for i := 0; i < st.NumFields(); i++ {
				field := st.Field(i)
				if field.Name() == proof.Field && !field.Exported() && field.Pkg() != nil && field.Pkg().Path() == named.Obj().Pkg().Path() && types.Identical(field.Type(), sig.Results().At(0).Type()) {
					return proof, true
				}
			}
		}
	}
	return EnumObserver{}, false
}
