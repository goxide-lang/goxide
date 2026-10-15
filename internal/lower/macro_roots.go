package lower

import (
	"fmt"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type rootRequirement struct {
	target    string
	file      string
	span      syntax.Span
	namespace paths.Namespace
}

func (r rootRequirement) check(owners map[string]string, external *moduleGoImporter) error {
	owner, local := owners[r.namespace.PackagePath]
	standard := false
	var err error
	if !local {
		owner, standard, err = external.PackageOrigin(r.namespace.PackagePath)
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
		return &Diagnostic{File: r.file, Span: r.span, Message: err.Error(), Origin: resolve.Origin{File: r.file, Span: r.span}}
	}
	return nil
}

func applyModuleRoots(module, name string, file *syntax.File, roots paths.Roots) []rootRequirement {
	var requirements []rootRequirement
	for _, item := range file.Items {
		use, ok := item.(*syntax.UseDecl)
		if !ok || use.Tree == nil || use.Tree.Path == nil || len(use.Tree.Path.Segments) == 0 {
			continue
		}
		segments := use.Tree.Path.Segments
		namespace, ok := roots[segments[0].Name.Name]
		if !ok {
			continue
		}
		// A private routing prefix retains namespace children and their source
		// identities until a concrete member is selected by the resolver.
		target := module + "/$goxide$roots/" + segments[0].Name.Name
		requirements = append(requirements, rootRequirement{target: target, file: name, span: use.Span(), namespace: namespace})
		if use.Tree.Alias == nil && !use.Tree.Glob {
			alias := *segments[len(segments)-1].Name
			use.Tree.Alias = &alias
		}
		var mapped []*syntax.PathSegment
		for _, part := range strings.Split(target, "/") {
			name := *segments[0].Name
			name.Name = part
			name.Raw = true // Routing components are module identity, not source identifiers.
			segment := *segments[0]
			segment.Name = &name
			mapped = append(mapped, &segment)
		}
		use.Tree.Path.Segments = append(mapped, segments[1:]...)
	}
	var id syntax.NodeID
	syntax.Walk(file, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
	return requirements
}
