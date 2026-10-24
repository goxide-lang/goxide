package lower

import (
	goast "go/ast"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
)

func (e *emitter) inferredFunctionValue(function *traitFunction, instance *check.FunctionInstance) goast.Expr {
	p := newTraitInference(e)
	bounds, evidence := e.functionCallbacks(function, instance)
	callbacks, callbackTypes := p.callbacks(bounds, evidence)
	constants, constantTypes := p.stableConstants(e.stableFunctionPlan(function), instance.Substitution, instance.ConstSubstitution)
	callbacks, callbackTypes = append(constants, callbacks...), append(constantTypes, callbackTypes...)
	enumArgs, enumTypes := p.enumChecks(e.functionEnumChecks(function, instance))
	callbacks, callbackTypes = append(callbacks, enumArgs...), append(callbackTypes, enumTypes...)
	signature := e.functionInstanceSignature(instance)
	factory := e.sourceGlobal(function.symbol.Decl.File, e.constFunctionName(function, instance))
	var prefix []goast.Expr
	for _, typ := range functionCaseArguments(instance) {
		if !e.nameableType(typ) {
			break
		}
		prefix = append(prefix, e.goType(typ))
	}
	factory = enumIndices(factory, prefix)
	fun := p.parameter(factory, signatureOf(callbackTypes, signature))
	return p.finish(&goast.CallExpr{Fun: fun, Args: callbacks}, signature)
}

func (e *emitter) inferredOperationValue(selection *check.MemberSelection) (goast.Expr, bool) {
	if !e.needsOperationInference(selection) {
		return nil, false
	}
	p := newTraitInference(e)
	return p.finish(p.operation(selection), operationSignature(selection)), true
}

func (e *emitter) inferredMemberValue(selection *check.MemberSelection, receiver goast.Expr) (goast.Expr, bool) {
	receiverType := e.memberReceiverType(selection)
	if e.nameableType(receiverType) && e.nameableType(selection.Signature) && !e.privateEvidence(check.Evidence{Impl: selection.Impl}) {
		return nil, false
	}
	p := newTraitInference(e)
	captured := p.parameter(receiver, receiverType)
	binder := p.binding(selection)
	value := &goast.CallExpr{Fun: binder, Args: []goast.Expr{captured}}
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
	bounds, evidence := viewCallbacks(impl)
	callbacks, callbackTypes := p.callbacks(bounds, evidence)
	enumArgs, enumTypes := p.enumChecks(e.viewEnumChecks(impl))
	callbacks, callbackTypes = append(callbacks, enumArgs...), append(callbackTypes, enumTypes...)
	witnesses, witnessTypes := p.viewWitnesses(impl)
	arguments := append(witnesses, callbacks...)
	parameters := append(witnessTypes, callbackTypes...)
	constructorType := signatureOf([]types.Type{captureType(impl)}, view.Type)
	factory := p.parameter(e.sourceGlobal(template.File, e.traitFactoryName(impl)), signatureOf(parameters, constructorType))
	constructor := &goast.CallExpr{Fun: factory, Args: arguments}
	result := &goast.CallExpr{Fun: constructor, Args: []goast.Expr{captured}}
	return p.finish(result, view.Type), true
}
