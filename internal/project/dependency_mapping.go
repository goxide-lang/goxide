package project

import (
	"fmt"
	"strings"

	"github.com/goxide-lang/goxide/internal/paths"
)

func dependencyIdentifier(name string) bool {
	return name != "crate" && paths.ValidIdentifier(name)
}

func validateDependency(alias string, d Dependency) error {
	if d.IsToolchain() {
		if d.Module != "" || d.Version != "" || d.Path != "" || d.Git != "" || d.Rev != "" || d.Subdir != "" || d.Features != nil || d.DefaultFeatures != nil {
			return fmt.Errorf("dependency %s: Go standard library accepts only go and optional", alias)
		}
	} else if d.Module != "" && !validPackagePath(d.Module) {
		return fmt.Errorf("dependency %s: invalid module path %q", alias, d.Module)
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
