package frontend

import (
	"fmt"
	"go/types"
)

// selectedModuleImporter completes a selected local package when a validated
// namespace re-export first reveals its exact path. Namespace groups are not
// traversed eagerly, and packages outside the selected build view are untouched.
type selectedModuleImporter struct {
	base    types.Importer
	prepare func(string) error
}

func (i selectedModuleImporter) Import(path string) (*types.Package, error) {
	if err := i.prepare(path); err != nil {
		return nil, err
	}
	return i.base.Import(path)
}
func (i selectedModuleImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if err := i.prepare(path); err != nil {
		return nil, err
	}
	if from, ok := i.base.(types.ImporterFrom); ok {
		return from.ImportFrom(path, dir, mode)
	}
	return i.base.Import(path)
}
func (i selectedModuleImporter) PackageOrigin(path string) (string, bool, error) {
	if err := i.prepare(path); err != nil {
		return "", false, err
	}
	if origin, ok := i.base.(interface {
		PackageOrigin(string) (string, bool, error)
	}); ok {
		return origin.PackageOrigin(path)
	}
	return "", false, fmt.Errorf("package origin verification unavailable for %s", path)
}
func (i selectedModuleImporter) PackageFiles(path string) (string, []string, error) {
	if err := i.prepare(path); err != nil {
		return "", nil, err
	}
	if provider, ok := i.base.(interface {
		PackageFiles(string) (string, []string, error)
	}); ok {
		return provider.PackageFiles(path)
	}
	return "", nil, fmt.Errorf("source files unavailable for %s", path)
}
func (i selectedModuleImporter) PackageFingerprint(path string) (string, error) {
	if err := i.prepare(path); err != nil {
		return "", err
	}
	if provider, ok := i.base.(interface{ PackageFingerprint(string) (string, error) }); ok {
		return provider.PackageFingerprint(path)
	}
	return "", fmt.Errorf("package fingerprint unavailable for %s", path)
}
