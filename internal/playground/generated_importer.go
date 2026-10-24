package playground

import (
	"fmt"
	"go/types"

	"github.com/goxide-lang/goxide/internal/typecheck"
)

// Every edit rebuilds the selected source graph. Only packages outside that
// graph use the fixed export importer, preserving its allowlist and identities.
type generatedImporter struct {
	sources  map[string]map[string][]byte
	external types.Importer
	sizes    types.Sizes
	packages map[string]*types.Package
	loading  map[string]bool
}

func (i *generatedImporter) Import(path string) (*types.Package, error) {
	if pkg := i.packages[path]; pkg != nil {
		return pkg, nil
	}
	sources := i.sources[path]
	if sources == nil {
		return i.external.Import(path)
	}
	if i.loading[path] {
		return nil, fmt.Errorf("generated package import cycle: %s", path)
	}
	i.loading[path] = true
	defer delete(i.loading, path)
	pkg, err := typecheck.CheckPackageTypes(path, sources, i, i.sizes)
	if err == nil {
		i.packages[path] = pkg
	}
	return pkg, err
}
