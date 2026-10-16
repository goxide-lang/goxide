package main

import (
	"go/types"
	"path/filepath"

	"github.com/hxsf/goxide/internal/typecheck"
)

// Check the complete selected Go package before any generated file is
// published. Native bodies are absent from the earlier declaration pass.
func checkGeneratedPackage(packagePath string, sources, generated map[string][]byte, imports types.Importer) error {
	files := make(map[string][]byte, len(sources))
	for name, source := range sources {
		if filepath.Ext(name) == ".go" {
			files[name] = source
		}
	}
	for name, source := range generated {
		files[generatedFilename(name)] = source
	}
	return typecheck.CheckPackage(packagePath, files, imports)
}
