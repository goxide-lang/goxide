package typecheck

import (
	"fmt"
	"go/types"
	"path"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

// NamespaceImporter binds explicit source namespaces to Go export data.
// In-memory callers use this when they have no source-package graph.
type NamespaceImporter struct {
	goImports types.Importer
	roots     paths.Roots
	err       error
}

func (i *NamespaceImporter) namespace(parts []string) (paths.Namespace, []string, bool) {
	if len(parts) == 0 {
		return paths.Namespace{}, nil, false
	}
	namespace, ok := i.roots[parts[0]]
	if !ok {
		return paths.Namespace{}, nil, false
	}
	rest := parts[1:]
	for len(rest) > 0 {
		child, exists := namespace.Children[rest[0]]
		if !exists {
			break
		}
		if child.ModulePath == "" && child.Source == "" {
			child.ModulePath, child.Source = namespace.ModulePath, namespace.Source
		}
		namespace, rest = child, rest[1:]
	}
	return namespace, rest, true
}

func (i *NamespaceImporter) canonical(parts []string) ([]string, bool) {
	namespace, rest, mapped := i.namespace(parts)
	if !mapped {
		return parts, true
	}
	if namespace.PackagePath == "" || len(rest) > 1 {
		return nil, false
	}
	origin, ok := i.goImports.(interface {
		PackageOrigin(string) (string, bool, error)
	})
	if !ok {
		i.err = fmt.Errorf("single-file importer cannot verify package ownership")
		return nil, false
	}
	owner, standard, err := origin.PackageOrigin(namespace.PackagePath)
	if err == nil {
		if namespace.Source == "toolchain" {
			if !standard {
				err = fmt.Errorf("package %s is not supplied by the Go toolchain", namespace.PackagePath)
			}
		} else if standard || owner != namespace.ModulePath {
			err = fmt.Errorf("package %s belongs to module %q, want %q", namespace.PackagePath, owner, namespace.ModulePath)
		}
	}
	if err != nil {
		if i.err == nil {
			i.err = err
		}
		return nil, false
	}
	return append(strings.Split(namespace.PackagePath, "/"), rest...), true
}

func (i *NamespaceImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	if name.Ctx != 0 {
		return nil
	}
	if namespace, rest, mapped := i.namespace(parts); mapped {
		if len(rest) != 0 {
			return nil
		}
		if _, exists := namespace.Children[name.Name]; exists {
			return []resolve.External{{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: append(append([]string(nil), parts...), name.Name)}}
		}
	}
	canonical, ok := i.canonical(parts)
	if !ok {
		return nil
	}
	pkg, err := i.goImports.Import(strings.Join(canonical, "/"))
	if err != nil {
		return nil
	}
	object := pkg.Scope().Lookup(name.Name)
	if object == nil || !object.Exported() {
		return nil
	}
	var kind resolve.Kind
	switch object.(type) {
	case *types.TypeName:
		kind = resolve.Type
	case *types.Func:
		kind = resolve.Function
	case *types.Builtin:
		kind = resolve.Builtin
	case *types.Var:
		kind = resolve.Variable
	case *types.Const:
		kind = resolve.Constant
	default:
		return nil
	}
	return []resolve.External{{Name: name, Kind: kind, GoName: object.Name(), Visibility: resolve.Public, Target: append(canonical, object.Name())}}
}

func (i *NamespaceImporter) CanonicalPath(parts []string) ([]string, bool) {
	if len(parts) < 2 {
		return nil, false
	}
	canonical, ok := i.canonical(parts)
	if !ok {
		return nil, false
	}
	_, err := i.goImports.Import(strings.Join(canonical[:len(canonical)-1], "/"))
	return canonical, err == nil
}

func (i *NamespaceImporter) CheckUses(file *resolve.File) error {
	for _, use := range file.Uses {
		var declaration *syntax.UseDecl
		for _, item := range file.AST.Items {
			if candidate, ok := item.(*syntax.UseDecl); ok && candidate.ID() == use.Decl.ID {
				declaration = candidate
				break
			}
		}
		if declaration == nil || len(declaration.Tree.Path.Segments) == 0 {
			continue
		}
		root := declaration.Tree.Path.Segments[0].Name
		_, known := i.roots[root.Name]
		key := resolve.Key{Name: root.Name, Ctx: root.Ctx}
		for scope := file.Scope; !known && scope != nil && scope.Kind != resolve.PreludeScope; scope = scope.Parent {
			known = scope.Bindings[key] != nil
			for _, alias := range scope.Aliases[key] {
				known = known || alias != use.Symbol
			}
		}
		if !known {
			return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: fmt.Sprintf("unknown dependency root %q", root.Name)}
		}
		if namespace, _, mapped := i.namespace(use.Target); mapped && namespace.PackagePath != "" {
			parts := strings.Split(namespace.PackagePath, "/")
			owner := path.Dir(file.Name)
			for index, part := range parts {
				if part != "internal" {
					continue
				}
				parent := strings.Join(parts[:index], "/")
				if parent == "" || owner != parent && !strings.HasPrefix(owner, parent+"/") {
					return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: fmt.Sprintf("internal package %s is inaccessible from %s", namespace.PackagePath, owner)}
				}
			}
		}
		if use.Blank {
			if namespace, rest, mapped := i.namespace(use.Target); mapped && (len(rest) != 0 || namespace.PackagePath == "") {
				return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: "blank use requires an explicitly declared concrete package"}
			}
			target, ok := i.canonical(use.Target)
			if !ok {
				if i.err != nil {
					return i.err
				}
				return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: "blank use requires a concrete package, not a namespace group"}
			}
			if _, err := i.goImports.Import(strings.Join(target, "/")); err != nil {
				return err
			}
			use.Target = target
		} else if namespace, rest, mapped := i.namespace(use.Target); mapped && len(rest) == 0 && namespace.PackagePath != "" {
			use.Target = strings.Split(namespace.PackagePath, "/")
		}
	}
	return i.err
}

// NewNamespaceImporter binds roots using the caller-selected Go dependency graph.
func NewNamespaceImporter(importer types.Importer, roots paths.Roots) *NamespaceImporter {
	return &NamespaceImporter{goImports: importer, roots: roots}
}

// Err reports namespace ownership failures observed while resolving members.
func (i *NamespaceImporter) Err() error { return i.err }
