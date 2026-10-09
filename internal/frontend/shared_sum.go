package frontend

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/hxsf/goxide/internal/enums"
)

// Only the canonical shared package and its exact inline representation grant
// the sum protocol; source aliases do not change the definition or helper owner.
func sharedSumFamily(family string) enumFamilyExport {
	f := enumFamilyExport{Name: family, Family: family, DefinitionPath: enums.SumPackage, DefinitionName: family, TagField: "tag", Tag: "Tag" + family, Validator: "Validate" + family, ABI: enums.SumABI}
	if family == "Option" {
		f.Variants = []enumFamilyVariantExport{{Name: "Some", Tag: 1, Field: "some", Value: "ValueSome", Project: "ProjectSome", Constructor: "NewSome", PayloadIndex: 0}, {Name: "None", Tag: 0, Constructor: "None", PayloadIndex: -1}}
	} else {
		f.Variants = []enumFamilyVariantExport{{Name: "Ok", Tag: 1, Field: "ok", Value: "ValueOk", Project: "ProjectOk", Constructor: "NewOk", PayloadIndex: 0}, {Name: "Err", Tag: 2, Field: "err", Value: "ValueErr", Project: "ProjectErr", Constructor: "NewErr", PayloadIndex: 1}}
	}
	return f
}
func sharedSumMetadata(pkg *types.Package, imp types.Importer) (enumMetadata, error) {
	if pkg == nil || pkg.Path() != enums.SumPackage {
		return enumMetadata{}, fmt.Errorf("shared sum package identity mismatch")
	}
	abi, ok := pkg.Scope().Lookup("GoxideSumABI").(*types.Const)
	if !ok || abi.Val().Kind() != constant.Int {
		return enumMetadata{}, fmt.Errorf("shared sum ABI marker missing")
	}
	n, exact := constant.Int64Val(abi.Val())
	if !exact || n != enums.SumABI {
		return enumMetadata{}, fmt.Errorf("unsupported shared sum ABI; regenerate with matching std")
	}
	m := enumMetadata{Version: enumMetadataVersion, Path: pkg.Path(), Package: pkg.Name(), Families: []enumFamilyExport{sharedSumFamily("Option"), sharedSumFamily("Result")}}
	return m, validateEnumFamilies(m, pkg, imp)
}
func familyContract(f enumFamilyExport) enumFamilyExport {
	f.Name = f.DefinitionName
	f.Instances = nil
	return f
}
