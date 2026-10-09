package frontend

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/hxsf/goxide/internal/protocols"
)

// validateTypeInfoSupport checks the selected backend ABI. It does not grant
// source protocol membership to a type merely because it has similarly named
// methods; that evidence belongs to independently validated protocol metadata.
func validateTypeInfoSupport(imp types.Importer) error {
	bad := func(message string) error { return fmt.Errorf("typeinfo ABI: %s", message) }
	if imp == nil {
		return bad("package importer unavailable")
	}
	pkg, err := imp.Import(protocols.TypeInfoPackage)
	if err != nil {
		return bad(err.Error())
	}
	if pkg == nil || pkg.Path() != protocols.TypeInfoPackage {
		return bad("selected package identity mismatch")
	}
	origin, ok := imp.(interface {
		PackageOrigin(string) (string, bool, error)
	})
	if !ok {
		return bad("selected module origin unavailable")
	}
	module, standard, err := origin.PackageOrigin(protocols.TypeInfoPackage)
	if err != nil {
		return bad(err.Error())
	}
	if standard || module != "github.com/goxide-lang/std" {
		return bad("selected package must belong to github.com/goxide-lang/std")
	}
	abi, ok := pkg.Scope().Lookup("GoxideTypeInfoABI").(*types.Const)
	if !ok || abi.Val().Kind() != constant.Int || abi.Val().ExactString() != "1" || !types.Identical(abi.Type(), types.Typ[types.UntypedInt]) {
		return bad("unsupported GoxideTypeInfoABI; expected 1")
	}
	obj, ok := pkg.Scope().Lookup("CarrierFor").(*types.TypeName)
	if !ok || obj.IsAlias() {
		return bad("CarrierFor must be the canonical named constraint")
	}
	carrier, ok := obj.Type().(*types.Named)
	if !ok || carrier.Obj().Pkg() != pkg || carrier.TypeParams().Len() != 2 {
		return bad("CarrierFor type parameter signature mismatch")
	}
	self, shape := carrier.TypeParams().At(0), carrier.TypeParams().At(1)
	if !types.Identical(self.Constraint(), types.NewInterfaceType(nil, nil).Complete()) || !protocolEmptyConstraint(shape.Constraint()) {
		return bad("CarrierFor requires [T any, S ~struct{}]")
	}
	methods, ok := carrier.Underlying().(*types.Interface)
	if !ok {
		return bad("CarrierFor must be an interface constraint")
	}
	methods.Complete()
	if methods.NumMethods() != 1 || methods.NumEmbeddeds() != 0 {
		return bad("CarrierFor exact-Self anchor mismatch")
	}
	anchor := methods.Method(0)
	sig, ok := anchor.Type().(*types.Signature)
	if anchor.Name() != protocols.AnchorName || anchor.Pkg() != pkg || !ok || sig.TypeParams().Len() != 0 || sig.Variadic() || sig.Params().Len() != 1 || sig.Results().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), self) || !types.Identical(sig.Results().At(0).Type(), shape) {
		return bad("CarrierFor exact-Self anchor signature mismatch")
	}
	of, ok := pkg.Scope().Lookup("Of").(*types.Func)
	if !ok || of.Pkg() != pkg {
		return bad("Of must be a canonical package function")
	}
	sig, ok = of.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 2 || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return bad("Of type parameter/function signature mismatch")
	}
	self, shape = sig.TypeParams().At(0), sig.TypeParams().At(1)
	constraint, err := types.Instantiate(nil, carrier, []types.Type{self, shape}, false)
	if err != nil || !types.Identical(self.Constraint(), constraint) || !protocolEmptyConstraint(shape.Constraint()) || !types.Identical(sig.Results().At(0).Type(), shape) {
		return bad("Of must return S under [T CarrierFor[T,S], S ~struct{}]")
	}
	return nil
}

func protocolEmptyConstraint(t types.Type) bool {
	term := types.NewTerm(true, types.NewStruct(nil, nil))
	want := types.NewInterfaceType(nil, []types.Type{types.NewUnion([]*types.Term{term})}).Complete()
	return types.Identical(t, want)
}
