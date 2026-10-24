package main

import (
	"fmt"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/typecheck"
)

// Generation leaves only Go source in the author's module. Its final check
// therefore uses the authored graph, not temporary source-library replacements.
func (owner *singleFileModule) generationImporter(tags []string) (types.Importer, error) {
	modFile := filepath.Join(owner.temporary, "generation.mod")
	if err := os.WriteFile(modFile, owner.authoredMod, 0600); err != nil {
		return nil, err
	}
	// Source discovery may have downloaded checksums into its private copy.
	// Reuse those sums without changing any authored require or replace directive.
	sums, err := os.ReadFile(strings.TrimSuffix(owner.modFile, ".mod") + ".sum")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil {
		if err := os.WriteFile(strings.TrimSuffix(modFile, ".mod")+".sum", sums, 0600); err != nil {
			return nil, err
		}
	}
	return typecheck.NewImporter(owner.input, typecheck.ImporterOptions{Config: owner.config, Tags: tags, ModFile: modFile}), nil
}

func generationPublicationError(err error, owner *singleFileModule) error {
	if owner != nil && len(owner.usedSourceRoots) != 0 {
		err = fmt.Errorf("generated Go does not type-check against the authored Go module graph; source dependencies must publish the stable Go API required by generated code: %w", err)
	}
	return generationDependencyError(err, owner)
}
