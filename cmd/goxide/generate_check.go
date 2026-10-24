package main

import (
	"go/types"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/typecheck"
)

// Check the complete selected Go package before any generated file is
// published. Native bodies are absent from the earlier declaration pass.
func checkGeneratedPackage(packagePath string, sources, generated map[string][]byte, imports types.Importer, sizes types.Sizes, nativeDeclarationsOnly bool) error {
	files := make(map[string][]byte, len(sources))
	declarations := map[string]bool{}
	for name, source := range sources {
		if filepath.Ext(name) == ".go" {
			files[name] = source
			if nativeDeclarationsOnly {
				declarations[name] = true
			}
		}
	}
	for name, source := range generated {
		files[generatedFilename(name)] = source
	}
	if nativeDeclarationsOnly {
		return typecheck.CheckPackageWithDeclarations(packagePath, files, declarations, imports, sizes)
	}
	return typecheck.CheckPackage(packagePath, files, imports, sizes)
}
