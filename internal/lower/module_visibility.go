package lower

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
)

func internalPackageAccess(packagePath, sourceFile string) error {
	parts := strings.Split(packagePath, "/")
	for n, part := range parts {
		if part == "internal" {
			parent := strings.Join(parts[:n], "/")
			caller := path.Dir(sourceFile)
			if parent == "" || caller != parent && !strings.HasPrefix(caller, parent+"/") {
				return fmt.Errorf("use of internal package %s is not allowed from %s", packagePath, caller)
			}
		}
	}
	return nil
}

func (i *moduleImporter) checkRootRequirement(requirement rootRequirement) error {
	sourceFile := requirement.sourceFile
	if sourceFile == "" {
		sourceFile = requirement.origin.File
	}
	if err := internalPackageAccess(requirement.namespace.PackagePath, sourceFile); err != nil {
		return &Diagnostic{File: requirement.origin.File, Span: requirement.origin.Span, Message: err.Error(), Origin: requirement.origin}
	}
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
		sourceFile := requirement.sourceFile
		if sourceFile == "" {
			sourceFile = requirement.origin.File
		}
		caller := i.owners[path.Dir(sourceFile)]
		if caller == "" {
			caller, _, _ = strings.Cut(requirement.target, "/$goxide$roots/")
		}
		if i.crates != nil {
			from, to := i.crates.owners[path.Dir(sourceFile)], i.crates.owners[target]
			if from != nil && to != nil && from.Root == to.Root {
				continue
			}
		}
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

func (i *moduleImporter) checkCrateImports(resolved *resolve.Result) error {
	if i.crates == nil {
		return nil
	}
	imports := map[*resolve.Symbol]*resolve.Symbol{}
	for _, file := range resolved.Files {
		for _, use := range file.Uses {
			if use.Symbol != nil && use.Resolved != nil {
				imports[use.Symbol] = use.Resolved
			}
		}
	}
	check := func(from, target, file string) error {
		if i.crates.owners[target] == nil {
			return nil
		}
		caller, owner := i.crates.owners[from], i.crates.owners[target]
		if caller != nil && caller.Root == owner.Root {
			return nil
		}
		if !owner.Library || !i.publicPackage(owner.Root, target, imports) {
			return fmt.Errorf("%s: package %q is not publicly reachable from a library entry (crate %s)", file, target, owner.Root)
		}
		return nil
	}
	for _, edge := range i.crates.edges {
		if i.crates.owners[edge.to] == nil {
			for _, module := range i.crates.modules {
				if len(module.Crates) != 0 && i.crates.moduleOwner(edge.to) == module.Path {
					return fmt.Errorf("%s: native Go import %q is outside the selected crate graph", edge.file, edge.to)
				}
			}
		}
		if err := check(edge.from, edge.to, edge.file); err != nil {
			return err
		}
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
