package project

import (
	"fmt"
	"strings"
)

// Go module paths describe the native version request, independently of the
// effective source identity assigned after acquisition.
func resolveAuthorDependencies(author map[string]Dependency) (map[string]Dependency, error) {
	if author == nil {
		return nil, nil
	}
	deps := map[string]Dependency{}
	for _, alias := range keys(author) {
		d := author[alias]
		if d.Workspace {
			return nil, fmt.Errorf("dependency %s: workspace inheritance requires membership in its own workspace", alias)
		}
		count := 0
		for _, source := range []string{d.Go, d.Git, d.Path} {
			if source != "" {
				count++
			}
		}
		if count == 0 && (d.Version != "" || d.Registry != "") {
			return nil, fmt.Errorf("dependency %s: registry source is not implemented; use git, path or go", alias)
		}
		if count != 1 || d.Registry != "" || d.Version != "" && d.Go == "" {
			return nil, fmt.Errorf("dependency %s requires exactly one of registry version, git, path, go; version only accompanies go in this release", alias)
		}
		selectors := 0
		for _, ref := range []string{d.Rev, d.Tag, d.Branch} {
			if ref != "" {
				selectors++
			}
		}
		if selectors > 1 || selectors != 0 && d.Git == "" {
			return nil, fmt.Errorf("dependency %s: rev/tag/branch require git and at most one selector", alias)
		}
		if d.Go != "" {
			if d.Module != "" || !validPackagePath(d.Go) {
				return nil, fmt.Errorf("dependency %s: go requires a path without protocol; do not repeat module", alias)
			}
			if d.Version != "" {
				d.Module = d.Go
			} else {
				first, _, _ := strings.Cut(d.Go, "/")
				if strings.Contains(first, ".") {
					return nil, fmt.Errorf("dependency %s: Go module %s requires version", alias, d.Go)
				}
				d.Package = d.Go
			}
		}
		deps[alias] = d
	}
	return deps, nil
}

// TOML serialization removes only the identities derived from a Go source.
func authorDependencies(deps map[string]Dependency) map[string]Dependency {
	if deps == nil {
		return nil
	}
	author := map[string]Dependency{}
	for alias, d := range deps {
		if d.Go != "" {
			d.Module = ""
			if d.IsToolchain() {
				d.Package = ""
			}
		}
		author[alias] = d
	}
	return author
}

func (d Dependency) gitRef() string {
	if d.Tag != "" {
		return "refs/tags/" + d.Tag
	}
	if d.Branch != "" {
		return "refs/remotes/origin/" + d.Branch
	}
	if d.Rev != "" {
		return d.Rev
	}
	return "HEAD"
}

func (d Dependency) GitKey() string {
	return Digest([]string{d.Git, d.gitRef(), d.Subdir})
}
