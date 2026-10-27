package main

import (
	"context"
	"go/types"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
)

func nativeSourceDependencies(root, modFile string, roots paths.Roots, cfg config.Config, automatic bool) (*compile.NativeDependencies, error) {
	return compile.NativeSourceDependencies(context.Background(), root, modFile, roots, cfg, automatic)
}

func (owner *singleFileModule) generationRoots(plan *compile.NativeDependencies, importer types.Importer) (paths.Roots, func(string, paths.Namespace) error, error) {
	return plan.GenerationRoots(owner.root, owner.config, importer, owner.recordSourceRoot)
}
func (owner *singleFileModule) recordSourceRoot(namespace paths.Namespace) {
	if owner.usedSourceRoots == nil {
		owner.usedSourceRoots = map[string]paths.Namespace{}
	}
	owner.usedSourceRoots[namespace.ModulePath] = namespace
}
