package lower

import (
	goast "go/ast"
	"go/types"

	"github.com/goxide-lang/goxide/internal/check"
)

func (e *emitter) enumTraitMethods(target *types.Named) []*goast.Field {
	var fields []*goast.Field
	if carrier := e.traits.carriers[target]; carrier != nil {
		e.generatedMember(target, typeCarrierAnchor, carrier.impls[0])
		fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(typeCarrierAnchor)}, Type: e.carrierAnchor(carrier, e.goType(target)).Type})
	}
	for _, impl := range e.traits.impls {
		if impl.Base == target && impl.Trait != nil && impl.Trait.Runtime && impl.Capture == check.ValueRef && types.Identical(impl.Target, target) {
			e.generatedMember(target, traitAs(impl), impl)
			fields = append(fields, &goast.Field{Names: []*goast.Ident{goast.NewIdent(traitAs(impl))}, Type: resultFunc(e.traitViewType(impl))})
		}
	}
	return fields
}

func (e *emitter) enumTraitDeclarations(target *types.Named, receiver goast.Expr) []goast.Decl {
	var out []goast.Decl
	if carrier := e.traits.carriers[target]; carrier != nil {
		out = append(out, e.carrierAnchor(carrier, receiver))
	}
	for _, impl := range e.traits.impls {
		if impl.Base == target && impl.Trait != nil && impl.Trait.Runtime && impl.Capture == check.ValueRef && types.Identical(impl.Target, target) {
			method := e.asDeclaration(impl, target, nil)
			method.Recv = receiverField("target", receiver)
			out = append(out, method)
		}
	}
	return out
}
