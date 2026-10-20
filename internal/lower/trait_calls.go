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
	return &goast.CallExpr{Fun: e.sourceGlobal(view.Impl.File, traitFactory(view.Impl)), Args: []goast.Expr{value}}
}

func (e *emitter) operationFunction(selection *check.MemberSelection) goast.Expr {
	m := selection.Member
	if m.Trait == nil {
		return e.sourceGlobal(m.File, inherentName(m))
	}
	var target types.Type
	if selection.Bound != nil {
		target = selection.Bound.Param
	} else if selection.Impl != nil && carrierTarget(e.info, selection.Impl) {
		target = selection.Impl.Target
	}
	if target == nil {
		return e.sourceGlobal(m.File, e.traitFreeOperation(m))
	}
	carrier := &goast.CallExpr{Fun: enumIndices(e.typeinfo("Of"), []goast.Expr{e.goType(target)})}
	return &goast.SelectorExpr{X: carrier, Sel: goast.NewIdent(traitOperation(m))}
}

func nativeMember(selection *check.MemberSelection) bool {
	return selection.Impl == nil && selection.Bound == nil || selection.Member.Trait == nil && !selection.Member.Static && goMethodReceiver(selection.Impl.Base)
}

func (e *emitter) memberReceiverType(selection *check.MemberSelection) types.Type {
	var target types.Type
	if selection.Bound != nil {
		target = selection.Bound.Param
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
	// Capture a method value's receiver now, including value-copy semantics.
	ft, args := e.memberSignature(selection.Signature)
	captured := e.fresh("Receiver")
	args = append([]goast.Expr{captured}, args...)
	inner := &goast.FuncLit{Type: ft, Body: forwardBody(e.operationFunction(selection), args, selection.Signature)}
	outer := &goast.FuncLit{Type: &goast.FuncType{Params: receiverField(captured.Name, e.goType(e.memberReceiverType(selection))), Results: &goast.FieldList{List: []*goast.Field{{Type: ft}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{inner}}}}}
	return &goast.CallExpr{Fun: outer, Args: []goast.Expr{receiver}}
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
		fun, args = e.operationFunction(selection), e.exprs(call.Args)
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
			fun = e.operationFunction(selection)
			args = append([]goast.Expr{x}, args...)
		}
	}
	result := &goast.CallExpr{Fun: fun, Args: args}
	if call.Ellipsis {
		result.Ellipsis = 1
	}
	return result, true
}
