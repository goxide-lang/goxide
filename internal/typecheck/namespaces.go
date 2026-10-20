package typecheck

import (
	"fmt"
	"go/types"
	"path"
	"strings"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

// NamespaceImporter binds declared roots and toolchain namespaces to Go exports.
// In-memory callers use this when they have no source-package graph.
type NamespaceImporter struct {
	goImports  types.Importer
	roots      paths.Roots
	err        error
	goNames    *paths.GoNamespaces
	sourceUses map[*syntax.UseDecl]namespaceUse
	boundUses  map[*syntax.UseDecl]bool
}

func (i *NamespaceImporter) namespace(parts []string) (paths.Namespace, []string, bool) {
	if len(parts) < 2 || parts[0] != namespaceRootPrefix {
		return paths.Namespace{}, nil, false
	}
	parts = parts[1:]
	namespace, declared := i.roots[parts[0]]
	var ok bool
	var err error
	if declared {
		namespace, ok, err = i.goNames.Resolve(namespace, nil)
	} else {
		namespace, ok, err = i.goNames.Root(parts[0])
	}
	if err != nil {
		if i.err == nil {
			i.err = err
		}
		return paths.Namespace{}, parts[1:], true
	}
	if !ok {
		if declared && i.err == nil {
			root := i.roots[parts[0]]
			source := root.GoPrefix
			if source == "" {
				source = root.ModulePath
			}
			i.err = fmt.Errorf("dependency root %q has no packages in selected source %q", parts[0], source)
		}
		return paths.Namespace{}, parts[1:], declared
	}
	rest := parts[1:]
	for len(rest) > 0 {
		child, exists, err := i.goNames.Resolve(namespace, rest[:1])
		if err != nil {
			if i.err == nil {
				i.err = err
			}
			return paths.Namespace{}, rest, true
		}
		if !exists {
			break
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
	if err := i.goNames.Validate(namespace); err != nil {
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
	target := append(append([]string(nil), parts...), name.Name)
	if namespace, rest, mapped := i.namespace(target); mapped && len(rest) == 0 && (namespace.PackagePath != "" || namespace.GoPrefix != "") {
		return []resolve.External{{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: target}}
	}
	if len(parts) == 0 {
		return nil
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
	defer func() {
		for _, item := range file.AST.Items {
			if use, ok := item.(*syntax.UseDecl); ok {
				if original, routed := i.sourceUses[use]; routed {
					use.Tree.Path.Segments, use.Tree.Alias = original.segments, original.alias
				}
			}
		}
	}()
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
		segments := declaration.Tree.Path.Segments
		if segments[0].Name.Name == namespaceRootPrefix {
			segments = segments[1:]
		}
		root := segments[0].Name
		_, known := i.roots[root.Name]
		known = known || i.boundUses[declaration]
		if !known {
			_, known, _ = i.goNames.Root(root.Name)
		}
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
		packagePath := ""
		member := use.Resolved
		if member != nil && member.Kind != resolve.Module && member.Kind != resolve.Import {
			if use.Glob {
				switch member.Kind {
				case resolve.Variable, resolve.Constant, resolve.Function, resolve.Variant, resolve.Field, resolve.Label, resolve.Builtin, resolve.Macro:
					return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: "glob use target is not a namespace"}
				}
			}
			// Resolved member targets are canonical Go identities, not source
			// roots. Rebinding their first segment could select a shadowing alias.
			if len(use.Target) > 1 {
				packagePath = strings.Join(use.Target[:len(use.Target)-1], "/")
			}
		} else if member != nil && member.Members != nil || use.Symbol != nil && use.Symbol.AliasOf != nil {
			continue
		} else {
			namespace, rest, mapped := i.namespace(use.Target)
			if i.err != nil {
				return i.err
			}
			if !mapped || len(rest) != 0 {
				// A registered derive has no runtime Go declaration.
				if !use.Blank && !use.Glob {
					if canonical, ok := i.CanonicalPath(use.Target); ok {
						if _, registered := derives.Lookup(strings.Join(canonical[:len(canonical)-1], "/"), canonical[len(canonical)-1]); registered {
							continue
						}
					}
				}
				segments := declaration.Tree.Path.Segments
				return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: fmt.Sprintf("use target %q is not a public item or namespace", segments[len(segments)-1].Name.Name)}
			}
			packagePath = namespace.PackagePath
			if use.Blank && packagePath == "" {
				return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: "blank use requires a concrete package, not a namespace group"}
			}
			if err := i.goNames.Validate(namespace); err != nil {
				return err
			}
			if packagePath != "" {
				use.Target = strings.Split(packagePath, "/")
			}
		}
		parts := strings.Split(packagePath, "/")
		owner := path.Dir(file.Name)
		for index, part := range parts {
			if part != "internal" {
				continue
			}
			parent := strings.Join(parts[:index], "/")
			if parent == "" || owner != parent && !strings.HasPrefix(owner, parent+"/") {
				return resolve.Diagnostic{File: file.Name, Span: declaration.Span(), Message: fmt.Sprintf("internal package %s is inaccessible from %s", packagePath, owner)}
			}
		}
	}
	return i.err
}

// NewNamespaceImporter binds roots using the caller-selected Go dependency graph.
func NewNamespaceImporter(importer types.Importer, roots paths.Roots) *NamespaceImporter {
	return &NamespaceImporter{goImports: importer, roots: roots, goNames: paths.NewGoNamespaces(importer)}
}

// Err reports namespace ownership failures observed while resolving members.
func (i *NamespaceImporter) Err() error { return i.err }
