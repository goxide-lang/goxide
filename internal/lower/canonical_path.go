package lower

import (
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
)

// CanonicalPath follows namespaces and source reexports for a compiler
// registration. The final name need not be a Go object: resolve checks it
// against the fixed derive registry after the package route is established.
func (i *moduleImporter) CanonicalPath(parts []string) ([]string, bool) {
	paths := i.canonicalPaths(parts, map[string]bool{})
	var result []string
	for _, candidate := range paths {
		if result != nil && strings.Join(result, "/") != strings.Join(candidate, "/") {
			return nil, false
		}
		result = candidate
	}
	return result, len(result) > 1
}

func (i *moduleImporter) canonicalPaths(parts []string, visiting map[string]bool) [][]string {
	key := strings.Join(parts, "/")
	if len(parts) < 2 || visiting[key] {
		return nil
	}
	visiting[key] = true
	defer delete(visiting, key)
	for split := len(parts); split > 0; split-- {
		if requirement, ok := i.namespace(parts[:split]); ok {
			if requirement.namespace.PackagePath == "" {
				return nil
			}
			if err := i.checkRootRequirement(requirement); err != nil {
				if i.err == nil {
					i.err = err
				}
				return nil
			}
			target := append(strings.Split(requirement.namespace.PackagePath, "/"), parts[split:]...)
			return i.canonicalPaths(target, visiting)
		}
	}
	for split := len(parts) - 1; split > 0; split-- {
		if scope := i.local[strings.Join(parts[:split], "/")]; scope != nil {
			return i.canonicalScopePaths(scope, parts[:split], parts[split:], visiting)
		}
	}
	if i.goPackage(strings.Join(parts[:len(parts)-1], "/")) != nil {
		return [][]string{append([]string(nil), parts...)}
	}
	return nil
}

func (i *moduleImporter) canonicalScopePaths(scope *resolve.Scope, owner, parts []string, visiting map[string]bool) [][]string {
	name := resolve.Key{Name: parts[0]}
	if symbol := scope.Bindings[name]; symbol != nil {
		if symbol.Visibility != resolve.Public {
			return nil
		}
		if len(symbol.Target) != 0 {
			return i.canonicalPaths(append(append([]string(nil), symbol.Target...), parts[1:]...), visiting)
		}
		target := append(append([]string(nil), owner...), symbol.Name.Name)
		if len(parts) == 1 {
			return [][]string{target}
		}
		if symbol.Members != nil {
			return i.canonicalScopePaths(symbol.Members, target, parts[1:], visiting)
		}
		return nil
	}
	var paths [][]string
	for _, alias := range scope.Aliases[name] {
		if alias.Visibility != resolve.Public {
			continue
		}
		target := alias.Target
		if alias.AliasOf != nil {
			symbol := alias.AliasOf
			for symbol.AliasOf != nil {
				symbol = symbol.AliasOf
			}
			target = append(i.symbolOwner(symbol), symbol.Name.Name)
		}
		target = append(append([]string(nil), target...), parts[1:]...)
		paths = append(paths, i.canonicalPaths(target, visiting)...)
	}
	if len(scope.Aliases[name]) == 0 {
		for _, use := range scope.Globs {
			if use.Public {
				target := append(append([]string(nil), use.Target...), parts...)
				paths = append(paths, i.canonicalPaths(target, visiting)...)
			}
		}
	}
	return paths
}
