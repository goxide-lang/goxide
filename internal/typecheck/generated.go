package typecheck

import (
	"fmt"
	"go/types"
	"path"

	"github.com/goxide-lang/goxide/internal/analysis"
)

// GeneratedSources combines Go output with the original selected native files
// from the same analysis. Fully native packages keep their export-data identities.
func GeneratedSources(result *analysis.Result, generated map[string][]byte) map[string]map[string][]byte {
	packages := map[string]map[string][]byte{}
	add := func(name string, data []byte) {
		pkg := path.Dir(name)
		if packages[pkg] == nil {
			packages[pkg] = map[string][]byte{}
		}
		packages[pkg][name] = data
	}
	for name, data := range generated {
		add(name, data)
	}
	for name, source := range result.Sources {
		if path.Ext(name) == ".go" && result.Files[name] != nil && result.Checked.NativePackages[path.Dir(name)] == nil {
			add(name, source.Content)
		}
	}
	return packages
}

// CheckGeneratedPackage checks the complete generated dependency graph with a
// private importer for this check; external packages retain the supplied identity.
func CheckGeneratedPackage(packagePath string, sources map[string]map[string][]byte, external types.Importer, sizes types.Sizes) error {
	imports := &generatedImporter{sources: sources, external: external, sizes: sizes, packages: map[string]*types.Package{}, loading: map[string]bool{}}
	_, err := imports.Import(packagePath)
	return err
}

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
	pkg, err := CheckPackageTypes(path, sources, i, i.sizes)
	if err == nil {
		i.packages[path] = pkg
	}
	return pkg, err
}
