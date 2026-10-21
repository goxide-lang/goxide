package lower

import (
	goast "go/ast"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
)

func (e *emitter) inferredMemberValue(selection *check.MemberSelection, receiver goast.Expr) (goast.Expr, bool) {
	receiverType := e.memberReceiverType(selection)
	if e.nameableType(receiverType) && e.nameableType(selection.Signature) && !e.privateEvidence(check.Evidence{Impl: selection.Impl}) {
		return nil, false
	}
	p := newTraitInference(e)
	captured := p.parameter(receiver, receiverType)
	operation := p.operation(selection)
	p.collect(selection.Signature)
	previous := e.inferenceTypes
	e.inferenceTypes = p.bodyTypes
	ft, arguments := e.memberSignature(selection.Signature)
	e.inferenceTypes = previous
	arguments = append([]goast.Expr{captured}, arguments...)
	value := &goast.FuncLit{Type: ft, Body: forwardBody(operation, arguments, selection.Signature)}
	return p.finish(value, selection.Signature), true
}

func (e *emitter) inferredTraitView(view *check.TraitView, value goast.Expr) (goast.Expr, bool) {
	impl := view.Impl
	template := implTemplate(impl)
	if !parameterizedImpl(template) || e.nameableType(captureType(impl)) && !e.privateEvidence(check.Evidence{Impl: impl}) {
		return nil, false
	}
	p := newTraitInference(e)
	captured := p.parameter(value, captureType(impl))
	callbacks, callbackTypes := p.callbacks(template.Requirements, impl.RequirementEvidence)
	witnesses, witnessTypes := p.implWitnesses(impl)
	arguments := append(witnesses, callbacks...)
	parameters := append(witnessTypes, callbackTypes...)
	constructorType := signatureOf([]types.Type{captureType(impl)}, view.Type)
	factory := p.parameter(e.sourceGlobal(template.File, e.traitFactoryName(template)), signatureOf(parameters, constructorType))
	constructor := &goast.CallExpr{Fun: factory, Args: arguments}
	result := &goast.CallExpr{Fun: constructor, Args: []goast.Expr{captured}}
	return p.finish(result, view.Type), true
}
