package lower

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// RuntimeRoot identifies one available ordinary library in the consumer's
// selected build plan. LogicalModule keeps the requested identity across replace;
// BindingID identifies the selected source content and crate within that plan.
type RuntimeRoot struct {
	LogicalModule string
	BindingID     string
	Namespace     paths.Namespace
}

// LookupRuntimeRoots is called for the file receiving the output, even when the
// invocation's tokens came from a nested expansion or a reexported definition.
func (m *moduleMacros) LookupRuntimeRoots(output *syntax.File, location expand.Origin, expn syntax.ExpnID) (map[string][]string, error) {
	configuration, ok := m.roots[output]
	if !ok {
		return nil, nil
	}
	var catalog []RuntimeRoot
	for _, roots := range m.roots {
		catalog = append(catalog, roots.runtimeRoots...)
	}
	selected := map[string]RuntimeRoot{}
	ambiguous := map[string]bool{}
	add := func(root RuntimeRoot) {
		rootPath := runtimeNamespacePath(root.Namespace)
		if root.Namespace.Unavailable != "" || ambiguous[root.LogicalModule] || m.imports.procPackage(rootPath) {
			return
		}
		if internalPackageAccess(rootPath, configuration.name) != nil {
			return
		}
		if previous, exists := selected[root.LogicalModule]; exists {
			if previous.BindingID != root.BindingID || !sameRuntimeNamespace(previous.Namespace, root.Namespace) {
				delete(selected, root.LogicalModule)
				ambiguous[root.LogicalModule] = true
			}
			return
		}
		selected[root.LogicalModule] = root
	}
	for _, root := range catalog {
		if root.LogicalModule == "" || root.BindingID == "" || root.Namespace.ModulePath == "" || root.Namespace.PackagePath == "" && root.Namespace.GoPrefix == "" {
			return nil, fmt.Errorf("runtime roots require a logical module, selected binding identity, and canonical module namespace")
		}
	}
	seen := map[*resolve.Scope]bool{}
	var visitScope func(*resolve.Scope, []string)
	var visitNamespace func(paths.Namespace, bool)
	visitNamespace = func(namespace paths.Namespace, exported bool) {
		if namespace.Unavailable != "" || m.imports.procPackage(runtimeNamespacePath(namespace)) {
			return
		}
		if exported {
			for _, root := range catalog {
				if sameRuntimeNamespace(root.Namespace, namespace) {
					add(root)
				}
			}
		}
		visitScope(m.imports.local[namespace.PackagePath], strings.Split(namespace.PackagePath, "/"))
	}
	visitScope = func(scope *resolve.Scope, owner []string) {
		if scope == nil || seen[scope] {
			return
		}
		seen[scope] = true
		visit := func(symbol *resolve.Symbol) {
			if symbol.Visibility != resolve.Public {
				return
			}
			for _, member := range m.imports.symbolMembers(symbol, owner, symbol.Name, map[string]bool{}) {
				if member.Kind != resolve.Module && member.Kind != resolve.Import {
					continue
				}
				if requirement, ok := m.imports.namespace(member.Target); ok {
					visitNamespace(requirement.namespace, true)
				} else {
					target := strings.Join(member.Target, "/")
					visitNamespace(paths.Namespace{PackagePath: target, ModulePath: m.imports.owners[target]}, true)
				}
			}
		}
		for _, symbol := range scope.Bindings {
			visit(symbol)
		}
		for _, aliases := range scope.Aliases {
			for _, symbol := range aliases {
				visit(symbol)
			}
		}
		for _, glob := range scope.Globs {
			if !glob.Public {
				continue
			}
			// A glob exposes exported bindings, not the package's entire root.
			if requirement, ok := m.imports.namespace(glob.Target); ok {
				visitNamespace(requirement.namespace, false)
			} else {
				visitScope(m.imports.local[strings.Join(glob.Target, "/")], glob.Target)
			}
		}
	}
	for _, root := range configuration.runtimeRoots {
		add(root)
		if root.Namespace.Unavailable == "" {
			visitNamespace(root.Namespace, false)
		}
	}
	names := make([]string, 0, len(selected))
	for name := range selected {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make(map[string][]string, len(names))
	display := configuration.name
	for name, file := range m.files {
		if file == location.File {
			display = name
			break
		}
	}
	for index, name := range names {
		target := configuration.module + "/$goxide$runtime/" + strconv.FormatUint(uint64(expn), 10) + "/" + strconv.Itoa(index)
		origin := resolve.Origin{File: display, Span: location.Span, Expn: expn}
		m.imports.namespaces[target] = rootRequirement{target: target, origin: origin, sourceFile: configuration.name, namespace: selected[name].Namespace}
		result[name] = strings.Split(target, "/")
	}
	return result, nil
}

func sameRuntimeNamespace(a, b paths.Namespace) bool {
	if a.ModulePath != b.ModulePath || a.GoPrefix != b.GoPrefix || (a.Source == "toolchain") != (b.Source == "toolchain") {
		return false
	}
	// Discovering a selected Go tree clears PackagePath when only child
	// packages exist. Its complete namespace is still the same GoPrefix.
	return a.GoPrefix != "" || a.PackagePath == b.PackagePath
}

func runtimeNamespacePath(namespace paths.Namespace) string {
	if namespace.GoPrefix != "" {
		return namespace.GoPrefix
	}
	return namespace.PackagePath
}

// LookupRuntimeRootMacro keeps a host-bound root when generated syntax uses a
// macro member. The path is checked by the same public source member resolver.
func (m *moduleMacros) LookupRuntimeRootMacro(target []string, path *syntax.Path) (expand.Macro, error) {
	if len(path.Segments) < 2 {
		return expand.Macro{}, fmt.Errorf("runtime root is a namespace, not a macro")
	}
	prefix := append([]string(nil), target...)
	for _, segment := range path.Segments[1 : len(path.Segments)-1] {
		prefix = append(prefix, segment.Name.Name)
	}
	name := path.Segments[len(path.Segments)-1].Name.Name
	candidates := m.imports.lookup(prefix, resolve.Key{Name: name, Namespace: resolve.MacroNamespace}, map[string]bool{})
	if m.imports.err != nil {
		return expand.Macro{}, m.imports.err
	}
	return m.selectMacro(candidates, strings.Join(prefix, "/"), name)
}
