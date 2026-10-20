package lower

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
)

func (i *moduleImporter) checkRootRequirement(requirement rootRequirement) error {
	if err := requirement.check(i.owners, i.external); err != nil {
		return err
	}
	// Resolve registers package uses in order. Check public reachability only
	// after every package has registered its reexports.
	i.rootAccesses = append(i.rootAccesses, requirement)
	return nil
}

func (i *moduleImporter) checkSourceRoots(resolved *resolve.Result) error {
	imports := map[*resolve.Symbol]*resolve.Symbol{}
	for _, file := range resolved.Files {
		for _, use := range file.Uses {
			if use.Symbol != nil && use.Resolved != nil {
				imports[use.Symbol] = use.Resolved
			}
		}
	}
	sort.SliceStable(i.rootAccesses, func(a, b int) bool {
		first, second := i.rootAccesses[a].origin, i.rootAccesses[b].origin
		if first.File != second.File {
			return first.File < second.File
		}
		if first.Span.Start != second.Span.Start {
			return first.Span.Start < second.Span.Start
		}
		return i.rootAccesses[a].namespace.PackagePath < i.rootAccesses[b].namespace.PackagePath
	})
	public := map[string]bool{}
	for _, requirement := range i.rootAccesses {
		target := requirement.namespace.PackagePath
		owner := i.owners[target]
		if owner == "" {
			owner = requirement.namespace.ModulePath
		}
		root := i.publicRoots[owner]
		if root == "" || target == "" || !i.sourcePackages[target] && !i.sourcePackages[root] {
			continue
		}
		caller, _, _ := strings.Cut(requirement.target, "/$goxide$roots/")
		if caller == owner || public[target] {
			continue
		}
		if i.publicPackage(root, target, imports) {
			public[target] = true
			continue
		}
		return &Diagnostic{File: requirement.origin.File, Span: requirement.origin.Span,
			Message: fmt.Sprintf("source package %q is not publicly reachable from library entry %q", target, root), Origin: requirement.origin}
	}
	return nil
}

func (i *moduleImporter) publicPackage(root, target string, imports map[*resolve.Symbol]*resolve.Symbol) bool {
	seenScopes := map[*resolve.Scope]bool{}
	seenPaths := map[string]bool{}
	var visitNamespace func([]string, bool) bool
	var visitScope func(*resolve.Scope, []string) bool
	var visitSymbol func(*resolve.Symbol, []string) bool
	visitSymbol = func(symbol *resolve.Symbol, owner []string) bool {
		if resolved := imports[symbol]; resolved != nil {
			symbol = resolved
		}
		for symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		if symbol.Kind != resolve.Module && symbol.Kind != resolve.Import {
			return false
		}
		if symbol.Members != nil {
			return visitScope(symbol.Members, append(append([]string(nil), owner...), symbol.Name.Name))
		}
		return len(symbol.Target) != 0 && visitNamespace(symbol.Target, true)
	}
	visitScope = func(scope *resolve.Scope, owner []string) bool {
		if scope == nil || seenScopes[scope] {
			return false
		}
		seenScopes[scope] = true
		for _, symbol := range scope.Bindings {
			if symbol.Visibility == resolve.Public && visitSymbol(symbol, owner) {
				return true
			}
		}
		for _, aliases := range scope.Aliases {
			for _, symbol := range aliases {
				if symbol.Visibility == resolve.Public && visitSymbol(symbol, owner) {
					return true
				}
			}
		}
		for _, use := range scope.Globs {
			if use.Public && (use.Resolved == nil || use.Resolved.Kind == resolve.Module) && visitNamespace(use.Target, false) {
				return true
			}
		}
		return false
	}
	visitNamespace = func(parts []string, exported bool) bool {
		name := strings.Join(parts, "/")
		if exported && name == target {
			return true
		}
		if seenPaths[name] {
			return false
		}
		seenPaths[name] = true
		for split := len(parts); split > 0; split-- {
			if requirement, ok := i.namespace(parts[:split]); ok {
				if requirement.namespace.PackagePath != "" {
					next := append(strings.Split(requirement.namespace.PackagePath, "/"), parts[split:]...)
					if visitNamespace(next, exported) {
						return true
					}
				}
				if split == len(parts) {
					for child := range requirement.namespace.Children {
						if visitNamespace(append(append([]string(nil), parts...), child), true) {
							return true
						}
					}
				}
				return false
			}
		}
		if scope := i.local[name]; scope != nil {
			return visitScope(scope, parts)
		}
		for split := len(parts) - 1; split > 0; split-- {
			scope := i.local[strings.Join(parts[:split], "/")]
			if scope == nil {
				continue
			}
			for offset, name := range parts[split:] {
				key := resolve.Key{Name: name}
				symbol := scope.Bindings[key]
				if symbol == nil && len(scope.Aliases[key]) == 1 {
					symbol = scope.Aliases[key][0]
				}
				if symbol == nil || symbol.Visibility != resolve.Public {
					return false
				}
				if resolved := imports[symbol]; resolved != nil {
					symbol = resolved
				}
				for symbol.AliasOf != nil {
					symbol = symbol.AliasOf
				}
				if symbol.Kind != resolve.Module && symbol.Kind != resolve.Import {
					return false
				}
				if len(symbol.Target) != 0 {
					next := append(append([]string(nil), symbol.Target...), parts[split+offset+1:]...)
					return visitNamespace(next, exported)
				}
				scope = symbol.Members
				if scope == nil {
					return false
				}
			}
			return visitScope(scope, parts)
		}
		return false
	}
	return root != "" && visitNamespace(strings.Split(root, "/"), true)
}
