package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func adjustReceiver(value goast.Expr, address, dereference bool) goast.Expr {
	if dereference {
		value = &goast.StarExpr{X: value}
	}
	if address {
		value = &goast.UnaryExpr{Op: gotoken.AND, X: value}
	}
	return value
}

func (e *emitter) traitView(n syntax.Expr, value goast.Expr) goast.Expr {
	view := e.info.TraitViews[n]
	if view == nil {
		return value
	}
	if view.Receiver != nil && view.Receiver != n {
		value = e.expr(view.Receiver)
	}
	value = adjustReceiver(value, view.Address, view.Deref)
	if inferred, ok := e.inferredTraitView(view, value); ok {
		return inferred
	}
	return &goast.CallExpr{Fun: e.implFactory(view.Impl), Args: []goast.Expr{value}}
}

func (e *emitter) operationFunction(selection *check.MemberSelection) goast.Expr {
	m := selection.Member
	if selection.Impl != nil && selection.Impl.Declare {
		return &goast.CallExpr{Fun: e.sourceGlobal(selection.Impl.File, e.declaredOperationName(m))}
	}
	if selection.Bound != nil {
		if callback := e.callbackOperation(selection.Bound, m); callback != nil {
			return callback
		}
	}
	if selection.Impl != nil && selection.Impl.Template != nil {
		if instance := e.traits.instances[instanceImplKey(e.info, selection.Impl, m.Name)]; instance != nil {
			return e.sourceGlobal(instance.body.File, instance.name)
		}
	}
	if selection.Impl != nil && parameterizedImpl(implTemplate(selection.Impl)) && (m.Trait != nil || len(implTemplate(selection.Impl).Requirements) != 0) {
		return e.templateOperation(selection)
	}
	if m.Trait == nil {
		return enumIndices(e.sourceGlobal(m.File, e.inherentName(m)), e.implTypeArgs(selection.Impl))
	}
	var target types.Type
	if selection.Bound != nil && selection.Bound.Param != nil {
		target = traitBoundTarget(selection.Bound)
	} else if selection.Impl != nil && carrierTarget(e.info, implTemplate(selection.Impl)) && e.nameableType(selection.Impl.Target) {
		target = selection.Impl.Target
	}
	if target != nil {
		carrier := &goast.CallExpr{Fun: enumIndices(e.typeinfo("Of"), []goast.Expr{e.goType(target)})}
		return &goast.SelectorExpr{X: carrier, Sel: goast.NewIdent(traitOperation(m))}
	}
	ops := e.evidenceType(check.Evidence{Bound: selection.Bound, Impl: selection.Impl})
	return &goast.SelectorExpr{X: &goast.ParenExpr{X: &goast.CompositeLit{Type: ops}}, Sel: goast.NewIdent(traitOperation(m))}
}

func nativeMember(selection *check.MemberSelection) bool {
	return selection.Impl == nil && selection.Bound == nil || selection.Member.Trait == nil && !selection.Member.Static && nativeInherentImpl(selection.Impl)
}

func (e *emitter) memberReceiverType(selection *check.MemberSelection) types.Type {
	var target types.Type
	if selection.Bound != nil {
		target = traitBoundTarget(selection.Bound)
	} else if selection.Impl != nil {
		target = selection.Impl.Target
	} else {
		target = e.info.Types[selection.Receiver]
	}
	if selection.Member.Receiver != check.ValueRef {
		target = types.NewPointer(target)
	}
	return target
}

func (e *emitter) memberExpr(selection *check.MemberSelection) goast.Expr {
	m := selection.Member
	if selection.MethodExpr && nativeMember(selection) {
		return &goast.SelectorExpr{X: e.goType(selection.Signature.Params().At(0).Type()), Sel: goast.NewIdent(m.GoName)}
	}
	if m.Static || selection.MethodExpr {
		return e.operationFunction(selection)
	}
	receiver := adjustReceiver(e.expr(selection.Receiver), selection.Address, selection.Deref)
	if nativeMember(selection) {
		return &goast.SelectorExpr{X: receiver, Sel: goast.NewIdent(m.GoName)}
	}
	if inferred, ok := e.inferredMemberValue(selection, receiver); ok {
		return inferred
	}
	return &goast.CallExpr{Fun: e.bindingFunction(selection), Args: []goast.Expr{receiver}}
}

func (e *emitter) memberCall(call *syntax.CallExpr) (goast.Expr, bool) {
	selection := e.info.Members[call.Fun]
	if selection == nil || e.replacements[call.Fun] != nil {
		return nil, false
	}
	m := selection.Member
	var fun goast.Expr
	var args []goast.Expr
	if m.Static {
		args = e.exprs(call.Args)
	} else {
		receiver := selection.Receiver
		ordinary := call.Args
		if selection.MethodExpr {
			receiver, ordinary = call.Args[0], call.Args[1:]
		}
		var x goast.Expr
		if selection.Address && hasFlow(receiver) {
			x = e.lvalue(receiver)
		} else {
			x = e.expr(receiver)
		}
		x = adjustReceiver(x, selection.Address, selection.Deref)
		if selection.MethodExpr {
			for _, arg := range ordinary {
				if hasFlow(arg) {
					captured := e.fresh("Receiver")
					e.pending = append(e.pending, varStmt(captured, e.goType(selection.Signature.Params().At(0).Type()), x))
					x = captured
					break
				}
			}
		}
		args = e.exprs(ordinary)
		if nativeMember(selection) {
			fun = &goast.SelectorExpr{X: x, Sel: goast.NewIdent(m.GoName)}
		} else {
			args = append([]goast.Expr{x}, args...)
		}
	}
	if !nativeMember(selection) {
		if selection.Impl != nil && selection.Impl.Declare || e.traits.nativeBoundCall(selection.Bound, m.Name) {
			bound := *selection
			if bound.MethodExpr {
				var parameters []*types.Var
				for i := 1; i < bound.Signature.Params().Len(); i++ {
					parameters = append(parameters, bound.Signature.Params().At(i))
				}
				bound.Signature = types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), bound.Signature.Results(), bound.Signature.Variadic())
				bound.MethodExpr = false
			}
			value, ok := e.inferredMemberValue(&bound, args[0])
			if !ok {
				value = &goast.CallExpr{Fun: e.bindingFunction(&bound), Args: args[:1]}
			}
			result := &goast.CallExpr{Fun: value, Args: args[1:]}
			if call.Ellipsis {
				result.Ellipsis = 1
			}
			return result, true
		}
		if inferred, ok := e.inferredOperationCall(selection, args, call.Ellipsis); ok {
			return inferred, true
		}
		fun = e.operationFunction(selection)
	}
	result := &goast.CallExpr{Fun: fun, Args: args}
	if call.Ellipsis {
		result.Ellipsis = 1
	}
	return result, true
}
