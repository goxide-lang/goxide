package procmacro

import (
	"fmt"
	"go/types"

	pm "github.com/goxide-lang/syntax/proc_macro"
)

const (
	SDKPackage   = "github.com/goxide-lang/syntax/proc_macro"
	TokenPackage = "github.com/goxide-lang/syntax/token"
)

// ValidateSignature uses the same importer that checked the function. Protocol
// content validation belongs to the build plan and precedes macro construction.
func ValidateSignature(entry *Entry, signature *types.Signature, importer types.Importer) error {
	params := 2
	switch entry.Kind {
	case pm.Function, pm.Derive:
	case pm.Attribute:
		params = 3
	default:
		return fmt.Errorf("procedural macro %s has invalid entry kind %q", entry.Name, entry.Kind)
	}
	if signature == nil || signature.Recv() != nil || signature.TypeParams().Len() != 0 || signature.RecvTypeParams().Len() != 0 || signature.Variadic() || signature.Params().Len() != params || signature.Results().Len() != 1 {
		return fmt.Errorf("procedural macro %s requires func(Context, %s) Output", entry.Name, inputTypes(params))
	}
	if importer == nil {
		return fmt.Errorf("procedural macro %s requires the selected SDK package importer", entry.Name)
	}
	context, err := sdkType(importer, SDKPackage, "Context")
	if err != nil {
		return err
	}
	stream, err := sdkType(importer, TokenPackage, "TokenStream")
	if err != nil {
		return err
	}
	output, err := sdkType(importer, SDKPackage, "Output")
	if err != nil {
		return err
	}
	for i := range params {
		want := stream
		if i == 0 {
			want = context
		}
		if !types.Identical(signature.Params().At(i).Type(), want) {
			return fmt.Errorf("procedural macro %s parameter %d must have canonical SDK type %s", entry.Name, i+1, want)
		}
	}
	if !types.Identical(signature.Results().At(0).Type(), output) {
		return fmt.Errorf("procedural macro %s result must have canonical SDK type %s", entry.Name, output)
	}
	return nil
}

func inputTypes(params int) string {
	if params == 3 {
		return "TokenStream, TokenStream"
	}
	return "TokenStream"
}

func sdkType(importer types.Importer, packagePath, name string) (types.Type, error) {
	pkg, err := importer.Import(packagePath)
	if err != nil {
		return nil, fmt.Errorf("procedural macro SDK %s: %w", packagePath, err)
	}
	if pkg == nil || pkg.Path() != packagePath {
		return nil, fmt.Errorf("procedural macro SDK package identity mismatch for %s", packagePath)
	}
	object, ok := pkg.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("procedural macro SDK %s is missing type %s", packagePath, name)
	}
	named, ok := types.Unalias(object.Type()).(*types.Named)
	if !ok || named.Obj().Pkg() != pkg || named.Obj().Name() != name || named.TypeParams().Len() != 0 {
		return nil, fmt.Errorf("procedural macro SDK type %s must be defined by %s", name, packagePath)
	}
	return object.Type(), nil
}
