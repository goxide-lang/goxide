package project

import (
	"fmt"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
)

func dependencyIdentifier(name string) bool {
	return paths.ValidIdentifier(name)
}

func validateDependency(alias string, d Dependency) error {
	if d.Source != "" && d.Source != "module" && !d.IsToolchain() {
		return fmt.Errorf("dependency %s: unknown source %q; expected module or toolchain", alias, d.Source)
	}
	if d.Package != "" && !validPackagePath(d.Package) {
		return fmt.Errorf("dependency %s: invalid exact package path %q", alias, d.Package)
	}
	if err := validateNamespaceMappings("dependency "+alias, d.Namespaces); err != nil {
		return err
	}
	if d.IsToolchain() {
		if d.Module != "" || d.Version != "" || d.Path != "" || d.Git != "" || d.Rev != "" || d.Subdir != "" || d.Features != nil || d.DefaultFeatures != nil {
			return fmt.Errorf("dependency %s: toolchain source accepts only package, namespaces and optional", alias)
		}
	} else if d.Module == "" {
		return fmt.Errorf("dependency %s: module source requires module", alias)
	}
	return nil
}

func validateNamespaceMappings(owner string, mappings map[string]string) error {
	for _, key := range keys(mappings) {
		for _, part := range strings.Split(key, "::") {
			if !dependencyIdentifier(part) {
				return fmt.Errorf("%s: invalid namespace path %q; expected identifiers separated by ::", owner, key)
			}
		}
		if !validPackagePath(mappings[key]) {
			return fmt.Errorf("%s namespace %s: invalid exact package path %q", owner, key, mappings[key])
		}
	}
	return nil
}

// This is syntax validation only. Ownership is checked against the selected Go
// package's module/toolchain metadata when the package is actually loaded. No
// path prefix, namespace spelling or directory layout proves that ownership.
func validPackagePath(path string) bool {
	if path == "" {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+", c) {
				continue
			}
			return false
		}
	}
	return true
}
