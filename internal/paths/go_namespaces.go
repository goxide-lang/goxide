package paths

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// GoNamespaces shares package-tree discovery between module and in-memory
// compilation. Its importer owns the selected Go environment and package index.
type GoNamespaces struct{ importer types.Importer }

func NewGoNamespaces(importer types.Importer) *GoNamespaces {
	return &GoNamespaces{importer: importer}
}

// Root finds a toolchain namespace by its first path segment. A grouping prefix
// such as archive is a namespace even though it is not itself a Go package.
func (g *GoNamespaces) Root(name string) (Namespace, bool, error) {
	if !goNamespaceSegment(name) {
		return Namespace{}, false, nil
	}
	return g.Resolve(Namespace{Source: "toolchain", GoPrefix: name}, nil)
}

// Resolve walks the selected Go package tree. Authored source namespaces do not
// carry a GoPrefix; their public item edges are resolved by the source importer.
// bool distinguishes a missing child from a namespace without a root package.
func (g *GoNamespaces) Resolve(root Namespace, segments []string) (Namespace, bool, error) {
	if root.Unavailable != "" {
		return Namespace{}, false, fmt.Errorf("%s", root.Unavailable)
	}
	if root.Source == "toolchain" && root.GoPrefix == "" {
		root.GoPrefix = root.PackagePath
	}
	current, ok, err := g.node(root)
	if err != nil || !ok {
		return current, ok, err
	}
	for _, name := range segments {
		if current.GoPrefix == "" || !goNamespaceSegment(name) {
			return Namespace{}, false, nil
		}
		child := Namespace{Source: current.Source, ModulePath: current.ModulePath, GoPrefix: current.GoPrefix + "/" + name}
		child, ok, err = g.node(child)
		if err != nil || !ok {
			return child, ok, err
		}
		if current.PackagePath != "" && token.IsExported(name) {
			if err := g.Validate(current); err != nil {
				return Namespace{}, false, err
			}
			pkg, err := g.importer.Import(current.PackagePath)
			if err != nil {
				return Namespace{}, false, err
			}
			if object := pkg.Scope().Lookup(name); object != nil && object.Exported() {
				return Namespace{}, false, fmt.Errorf("ambiguous static path %s::%s: both an exported Go member and a child package", current.GoPrefix, name)
			}
		}
		current = child
	}
	return current, true, nil
}

// Source parsing already requires raw identifiers for reserved words. Package
// paths may still contain those words, for example r#go::ast.
func goNamespaceSegment(name string) bool {
	return name != "_" && (token.IsIdentifier(name) || token.Lookup(name).IsKeyword())
}

func (g *GoNamespaces) node(namespace Namespace) (Namespace, bool, error) {
	if namespace.Unavailable != "" {
		return Namespace{}, false, fmt.Errorf("%s", namespace.Unavailable)
	}
	if namespace.GoPrefix == "" {
		return namespace, true, nil
	}
	index, ok := g.importer.(interface {
		PackageIndex(source, module string) ([]string, error)
	})
	if !ok {
		return Namespace{}, false, fmt.Errorf("Go importer cannot enumerate the selected package tree")
	}
	packages, err := index.PackageIndex(namespace.Source, namespace.ModulePath)
	if err != nil {
		return Namespace{}, false, err
	}
	namespace.PackagePath = ""
	found := false
	for _, candidate := range packages {
		if candidate == namespace.GoPrefix {
			namespace.PackagePath, found = candidate, true
		} else if strings.HasPrefix(candidate, namespace.GoPrefix+"/") {
			found = true
		}
	}
	return namespace, found, nil
}

// Validate proves exact ownership; import path prefixes alone are insufficient.
func (g *GoNamespaces) Validate(namespace Namespace) error {
	if namespace.Unavailable != "" {
		return fmt.Errorf("%s", namespace.Unavailable)
	}
	if namespace.PackagePath == "" {
		return nil
	}
	origin, ok := g.importer.(interface {
		PackageOrigin(string) (string, bool, error)
	})
	if !ok {
		return fmt.Errorf("Go importer cannot verify package ownership")
	}
	owner, standard, err := origin.PackageOrigin(namespace.PackagePath)
	if err != nil {
		return err
	}
	if namespace.Source == "toolchain" {
		if !standard {
			return fmt.Errorf("package %s is not supplied by the Go toolchain", namespace.PackagePath)
		}
	} else if standard || owner != namespace.ModulePath {
		return fmt.Errorf("package %s belongs to module %q, want %q", namespace.PackagePath, owner, namespace.ModulePath)
	}
	return nil
}
