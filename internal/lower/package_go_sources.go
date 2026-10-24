package lower

import (
	"go/types"
	"path"
)

func collectPackageGoSources(packages map[string]map[string][]byte, packagePath string, sources map[string][]byte, dependencies []SourcePackage, generated map[string][]byte, native map[string]*types.Package) {
	add := func(pkg, name string, source []byte) {
		if packages[pkg] == nil {
			packages[pkg] = map[string][]byte{}
		}
		packages[pkg][name] = source
	}
	for name, source := range generated {
		add(path.Dir(name), name, source)
	}
	for name, source := range sources {
		if path.Ext(name) == ".go" {
			add(packagePath, path.Join(packagePath, path.Base(name)), source)
		}
	}
	for _, dependency := range dependencies {
		if native[dependency.PackagePath] != nil {
			continue
		}
		for name, source := range dependency.Sources {
			if path.Ext(name) == ".go" {
				add(dependency.PackagePath, path.Join(dependency.PackagePath, name), source)
			}
		}
	}
}
