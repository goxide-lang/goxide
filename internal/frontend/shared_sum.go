package frontend

import (
	"fmt"
	"go/constant"
	"go/types"

	"github.com/hxsf/goxide/internal/enums"
)

// The compiler knows this representation contract; ordinary interface shapes
// and package aliases do not grant a family protocol.
func sharedSumFamily(family string) enumFamilyExport {
	f := enumFamilyExport{Name: family, Family: family, DefinitionPath: enums.SumPackage, DefinitionName: family, Marker: "is" + family, ABI: enums.SumABI}
	names := []string{"Some", "None"}
	if family == "Result" {
		names = []string{"Ok", "Err"}
	}
	for i, name := range names {
		c, ctor, index := name, "New"+name, i
		if name == "None" {
			c, ctor, index = "", "None", -1
		}
		f.Variants = append(f.Variants, enumFamilyVariantExport{Name: name, Case: c, Constructor: ctor, PayloadIndex: index})
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
