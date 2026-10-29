package typecheck

import (
	"context"
	"fmt"
	"go/types"
	"path"
	"sort"

	"github.com/goxide-lang/goxide/internal/analysis"
)

// GeneratedSources combines Go output with the original selected native files
// from the same analysis. Fully native packages keep their export-data identities.
func GeneratedSources(result *analysis.Result, generated map[string][]byte) map[string]map[string][]byte {
	packages := map[string]map[string][]byte{}
	externalTests := map[string]bool{}
	if result.Resolved != nil {
		for _, pkg := range result.Resolved.Packages {
			if pkg.IsExternalTest() {
				for _, file := range pkg.Files {
					externalTests[file.Name] = true
				}
			}
		}
	}
	add := func(name string, data []byte) {
		pkg := path.Dir(name)
		if externalTests[name] {
			pkg += "_test"
		}
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

// CheckGeneratedGraph checks every selected package once, including packages
// without consumers. Dependencies retain the identities from this one check.
func CheckGeneratedGraph(ctx context.Context, sources map[string]map[string][]byte, external types.Importer, sizes types.Sizes) error {
	imports := &generatedImporter{sources: sources, external: external, sizes: sizes, packages: map[string]*types.Package{}, loading: map[string]bool{}}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := imports.Import(name); err != nil {
			return err
		}
	}
	return ctx.Err()
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
