package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/rootconfig"
)

// sourceRoots selects only explicitly declared namespaces. Module versions and
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

func projectRoots(node *project.Node) (paths.Roots, error) {
	if !node.HasManifest {
		return rootconfig.Load(node.Root)
	}
	if _, err := os.Stat(filepath.Join(node.Root, rootconfig.Filename)); err == nil {
		return nil, fmt.Errorf("%s: %s is for native Go modules; declare namespace mappings in goxide.toml dependencies", node.Root, rootconfig.Filename)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	mappings := map[string]rootconfig.Mapping{}
	for alias := range node.Requests {
		dependency := node.Manifest.Dependencies[alias]
		if dependency.IsToolchain() {
			mappings[alias] = rootconfig.Mapping{Package: dependency.Package, Source: "toolchain", Namespaces: dependency.Namespaces}
			continue
		}
		mappings[alias] = rootconfig.Mapping{Module: dependency.Module, Package: dependency.Package, Source: "module", Namespaces: dependency.Namespaces}
	}
	roots, err := rootconfig.Build(mappings)
	if err != nil {
		return nil, err
	}
	if len(node.Manifest.PackageNamespaces) == 0 {
		return roots, nil
	}
	local, err := rootconfig.Build(map[string]rootconfig.Mapping{"local": {Module: node.Module, Namespaces: node.Manifest.PackageNamespaces}})
	if err != nil {
		return nil, err
	}
	for alias, namespace := range local["local"].Children {
		if _, collision := roots[alias]; collision {
			return nil, fmt.Errorf("package namespace %s conflicts with dependency root", alias)
		}
		roots[alias] = namespace
	}
	return roots, nil
}

// Host bootstrap sources receive only explicit toolchain names. Target module
// roots and current-module mappings do not authorize host dependencies.
func projectHostRoots(node *project.Node) (paths.Roots, error) {
	roots, err := projectRoots(node)
	if err != nil {
		return nil, err
	}
	host := paths.Roots{}
	for alias, namespace := range roots {
		if namespace.Source == "toolchain" {
			host[alias] = namespace
		}
	}
	return host, nil
}
