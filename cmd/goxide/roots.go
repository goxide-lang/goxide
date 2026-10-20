package main

import (
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/rootconfig"
)

// sourceRoots selects only explicitly declared roots. Module versions and
// package availability still belong to the input's existing Go module graph.
func sourceRoots(moduleRoot, sourceDirectory, explicit string) (paths.Roots, error) {
	if explicit != "" {
		return rootconfig.Read(explicit)
	}
	if moduleRoot != "" {
		return rootconfig.Load(moduleRoot)
	}
	return rootconfig.Discover(sourceDirectory)
}

// Host bootstrap sources receive only explicit toolchain names.
func projectHostRoots(node *project.Node) (paths.Roots, error) {
	mappings := map[string]rootconfig.Mapping{}
	for alias := range node.Requests {
		dependency := node.Manifest.Dependencies[alias]
		if dependency.IsToolchain() {
			mappings[alias] = rootconfig.Mapping{Package: dependency.Go, Source: "toolchain"}
		}
	}
	return rootconfig.Build(mappings)
}
