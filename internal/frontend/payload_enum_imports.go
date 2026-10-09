package frontend

import (
	"fmt"
	"go/types"
	"sort"

	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/refs"
)

// A Go struct may expose an enum from a transitive dependency, including through
// a native companion file. Source-file import spellings are not its identity.
// known contains source-compiled or already artifact-validated packages. For a
// new transitive origin, loadEnumArtifact verifies the real importer package's
// types/signatures, origin/owner registrations and complete source/dependency
// fingerprints, exactly as it does for a direct import.
func payloadEnumResolver(owner string, known map[string]PackageResult, imp types.Importer) func(types.Type) (propagate.PayloadEnum, bool, error) {
	registry := map[string]PackageResult{}
	for path, pkg := range known {
		registry[path] = pkg
	}
	return func(t types.Type) (propagate.PayloadEnum, bool, error) {
		n, ok := types.Unalias(t).(*types.Named)
		if !ok || n.Obj().Pkg() == nil {
			return propagate.PayloadEnum{}, false, nil
		}
		origin := n.Obj().Pkg()
		st, ok := n.Underlying().(*types.Struct)
		if !ok {
			return propagate.PayloadEnum{}, false, nil
		}
		// An imported struct's export data may mention an enum without loading
		// that enum package's complete scope (including its artifact marker).
		// Use this compilation's importer, not a producer checker's package copy.
		currentOrigin, err := imp.Import(origin.Path())
		if err != nil {
			return propagate.PayloadEnum{}, false, err
		}
		origin = currentOrigin
		// Reserved private storage only triggers missing-authority rejection;
		// recognition still requires the validated producer artifact below.
		reservedMarker := st.NumFields() > 0 && st.Field(0).Name() == "__goxide_tag" && st.Field(0).Pkg() != nil && st.Field(0).Pkg().Path() == origin.Path()

		if origin.Scope().Lookup(enumMetadataMarker) != nil && !registry[origin.Path()].enumMetadata.present() {
			if owner == "" {
				if !reservedMarker {
					return propagate.PayloadEnum{}, false, nil
				}
				return propagate.PayloadEnum{}, false, fmt.Errorf("enum payload from %s requires a configured single-file owner or package compilation", origin.Path())
			}
			provider, ok := imp.(refs.ArtifactImporter)
			if !ok {
				return propagate.PayloadEnum{}, false, fmt.Errorf("enum payload metadata source importer unavailable for %s", origin.Path())
			}
			meta, err := loadEnumArtifact(origin.Path(), provider)
			if err != nil {
				return propagate.PayloadEnum{}, false, err
			}
			registry[origin.Path()] = PackageResult{enumMetadata: meta, enumTypes: origin}
		}
		paths := make([]string, 0, len(registry))
		for path := range registry {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		inaccessible := false
		for _, path := range paths {
			p := registry[path]
			for _, e := range p.enumMetadata.Enums {
				if e.DefinitionPath != n.Obj().Pkg().Path() || e.DefinitionName != n.Obj().Name() {
					continue
				}
				pkg, err := imp.Import(path)
				if err != nil {
					return propagate.PayloadEnum{}, false, err
				}
				if err := validateEnumMetadata(p.enumMetadata, pkg, imp); err != nil {
					return propagate.PayloadEnum{}, false, err
				}
				obj, ok := pkg.Scope().Lookup(e.Name).(*types.TypeName)
				if !ok || !types.Identical(t, obj.Type()) {
					continue
				}
				if !enumOriginImportAllowed(owner, e.DefinitionPath) {
					inaccessible = true
					continue
				}
				// The registry may contain unrelated facades reexporting this type.
				// Its public canonical origin is already a payload dependency;
				// importing a facade could introduce a new init side effect.
				return propagate.PayloadEnum{TypePath: e.DefinitionPath, TypeName: e.DefinitionName, ValidatorPath: e.DefinitionPath, Validator: e.Validator}, true, nil
			}
		}
		if inaccessible {
			return propagate.PayloadEnum{}, false, fmt.Errorf("enum payload validator has an inaccessible internal origin: %s", origin.Path())
		}
		// This reserved marker is only a reason to reject missing authorization;
		// it never grants the closed-enum protocol or chooses a helper by name.
		if reservedMarker {
			return propagate.PayloadEnum{}, false, fmt.Errorf("enum payload has no matching registered validation metadata: %s", n)
		}
		return propagate.PayloadEnum{}, false, nil
	}
}
