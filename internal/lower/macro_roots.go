package lower

import (
	"fmt"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type rootRequirement struct {
	target    string
	origin    resolve.Origin
	namespace paths.Namespace
}

type moduleRootConfig struct {
	module, name string
	roots        paths.Roots
}

func (r rootRequirement) check(owners map[string]string, external types.Importer) error {
	owner, local := owners[r.namespace.PackagePath]
	standard := false
	var err error
	if !local {
		if source, ok := external.(interface {
			PackageOrigin(string) (string, bool, error)
		}); ok {
			owner, standard, err = source.PackageOrigin(r.namespace.PackagePath)
		} else {
			err = fmt.Errorf("Go importer cannot verify package ownership")
		}
	}
	if err == nil {
		if r.namespace.Source == "toolchain" {
			if !standard {
				err = fmt.Errorf("package %s is not supplied by the Go toolchain", r.namespace.PackagePath)
			}
		} else if standard || owner != r.namespace.ModulePath {
			err = fmt.Errorf("package %s belongs to module %q, want %q", r.namespace.PackagePath, owner, r.namespace.ModulePath)
		}
	}
	if err != nil {
		return &Diagnostic{File: r.origin.File, Span: r.origin.Span, Message: err.Error(), Origin: r.origin}
	}
	return nil
}

func applyModuleRoots(module, name string, file *syntax.File, roots paths.Roots) []rootRequirement {
	var requirements []rootRequirement
	for _, item := range file.Items {
		use, ok := item.(*syntax.UseDecl)
		if !ok {
			continue
		}
		if requirement, ok := applyUseRoot(moduleRootConfig{module, name, roots}, use, nil); ok {
			requirements = append(requirements, requirement)
		}
	}
	var id syntax.NodeID
	syntax.Walk(file, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
	return requirements
}

// Retained macro output can introduce uses that were absent from the source
// import pass. Roots follow the path's lexical scope, including captures and
// explicit $caller paths; diagnostics retain the use construct's origin.
func applyExpandedRoots(expanded *expand.Result, configs map[*syntax.File]moduleRootConfig) []rootRequirement {
	var requirements []rootRequirement
	for _, file := range expanded.Files {
		changed := false
		for _, item := range file.Items {
			use, ok := item.(*syntax.UseDecl)
			if !ok {
				continue
			}
			config, ok := configs[expanded.Origins[use].File]
			if !ok {
				continue
			}
			if use.Tree != nil && use.Tree.Path != nil && len(use.Tree.Path.Segments) != 0 {
				root := use.Tree.Path.Segments[0].Name
				if owner, ok := configs[expanded.Scopes[root].File]; ok {
					config.module, config.roots = owner.module, owner.roots
				}
			}
			if requirement, ok := applyUseRoot(config, use, expanded); ok {
				requirements = append(requirements, requirement)
				changed = true
			}
		}
		if changed {
			var id syntax.NodeID
			syntax.Walk(file, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
		}
	}
	return requirements
}

func applyUseRoot(config moduleRootConfig, use *syntax.UseDecl, expanded *expand.Result) (rootRequirement, bool) {
	if use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 {
		return rootRequirement{}, false
	}
	segments := use.Tree.Path.Segments
	for _, segment := range segments {
		if segment.Name.Name == "$goxide$roots" {
			return rootRequirement{}, false // Already mapped before macro expansion.
		}
	}
	namespace, ok := config.roots[segments[0].Name.Name]
	if !ok {
		return rootRequirement{}, false
	}
	copyOrigin := func(from, to syntax.Node) {
		if expanded != nil {
			expanded.Origins[to] = expanded.Origins[from]
			if id, ok := from.(*syntax.Ident); ok {
				expanded.Scopes[to.(*syntax.Ident)] = expanded.Scopes[id]
			}
		}
	}
	// A private routing prefix retains namespace children and their source
	// identities until a concrete member is selected by the resolver.
	target := config.module + "/$goxide$roots/" + segments[0].Name.Name
	origin := resolve.Origin{File: config.name, Span: use.Span(), Expn: use.Expn}
	if expanded != nil {
		origin.Span = expanded.Origins[use].Span
		origin.Stack = expanded.Expansions[use.Expn].Stack
	}
	if use.Tree.Alias == nil && !use.Tree.Glob {
		original := segments[len(segments)-1].Name
		alias := *original
		use.Tree.Alias = &alias
		copyOrigin(original, &alias)
	}
	var mapped []*syntax.PathSegment
	for _, part := range strings.Split(target, "/") {
		name := *segments[0].Name
		name.Name = part
		name.Raw = true // Routing components are module identity, not source identifiers.
		segment := *segments[0]
		segment.Name = &name
		copyOrigin(segments[0].Name, &name)
		copyOrigin(segments[0], &segment)
		mapped = append(mapped, &segment)
	}
	use.Tree.Path.Segments = append(mapped, segments[1:]...)
	return rootRequirement{target: target, origin: origin, namespace: namespace}, true
}
