package analysis

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (q *Queries) validateRenameName(symbol *resolve.Symbol, refs *References, newName string) error {
	changed := map[*resolve.Symbol]*resolve.Symbol{}
	packages := map[string]*resolve.Package{}
	var functionPackage *resolve.Package
	for _, pkg := range q.result.Resolved.Packages {
		for _, file := range pkg.Files {
			packages[file.Name] = pkg
			if symbol.Kind != resolve.Function || file.Name != symbol.Decl.File {
				continue
			}
			for _, item := range file.AST.Items {
				for {
					declaration, ok := item.(*syntax.DeclareDecl)
					if !ok {
						break
					}
					item = declaration.Decl
				}
				if function, ok := item.(*syntax.FuncDecl); ok && function.Name.ID() == symbol.Decl.ID && len(function.Recv) == 0 && function.Path == nil {
					functionPackage = pkg
				}
			}
		}
	}
	for _, file := range q.result.Resolved.Files {
		for _, candidate := range file.Definitions {
			if candidate == nil || changed[candidate] != nil || candidate.Name.Name != refs.Name {
				continue
			}
			node := q.nodes[candidate.Decl]
			if node == nil {
				continue
			}
			location := q.location(candidate.Decl.File, node)
			selected := false
			for _, occurrence := range refs.Locations {
				selected = selected || sameLocation(location, occurrence)
			}
			if !selected {
				continue
			}
			goName, visibility, err := q.renamedDeclaration(candidate, newName)
			if err != nil {
				return err
			}
			copy := *candidate
			copy.Name.Name, copy.GoName, copy.Visibility = newName, goName, visibility
			changed[candidate] = &copy
		}
	}
	if renamed := changed[symbol]; renamed != nil && functionPackage != nil {
		if symbol.Name.Name == "init" || newName == "init" || functionPackage.Name == "main" && (symbol.GoName == "main" || renamed.GoName == "main") {
			return &QueryUnavailable{Reason: "renaming init or the main entry point requires a structural change"}
		}
	}
	if renamed := changed[symbol]; renamed != nil && symbol.Visibility == resolve.Public && renamed.Visibility == resolve.Private {
		for _, location := range refs.Locations {
			if packages[location.File] != packages[symbol.Decl.File] {
				return fmt.Errorf("renaming %s to %s would make an existing cross-package reference private", refs.Name, newName)
			}
		}
	}
	seen := map[*resolve.Scope]bool{}
	var check func(*resolve.Scope) error
	check = func(scope *resolve.Scope) error {
		if scope == nil || seen[scope] {
			return nil
		}
		seen[scope] = true
		var entries []*resolve.Symbol
		for _, binding := range scope.Bindings {
			entries = append(entries, binding)
		}
		for _, aliases := range scope.Aliases {
			entries = append(entries, aliases...)
		}
		if scope.Kind == resolve.FileScope && scope.Parent != nil {
			for _, binding := range scope.Parent.Bindings {
				entries = append(entries, binding)
			}
			for _, aliases := range scope.Parent.Aliases {
				entries = append(entries, aliases...)
			}
		}
		if scope.Kind != resolve.FunctionScope && scope.Kind != resolve.BlockScope && scope.Kind != resolve.ContextScope {
			for _, original := range entries {
				renamed := changed[original]
				if renamed == nil || renamed.Kind == resolve.Macro {
					continue
				}
				for _, other := range entries {
					if other == original || other.Name.Namespace != original.Name.Namespace || other.Name.Ctx != original.Name.Ctx || other.Kind == resolve.Macro {
						continue
					}
					if next := changed[other]; next != nil {
						other = next
					}
					if renamed.GoName == other.GoName || renamed.GoName == other.Name.Name || other.GoName != "" && other.GoName == renamed.Name.Name {
						return fmt.Errorf("renaming %s to %s produces Go name %q, which conflicts with %s", refs.Name, newName, renamed.GoName, other.Name.Name)
					}
				}
			}
		}
		for _, entry := range entries {
			if err := check(entry.Members); err != nil {
				return err
			}
		}
		return check(scope.Parent)
	}
	for _, file := range q.result.Resolved.Files {
		if err := check(file.Scope); err != nil {
			return err
		}
		for _, scope := range file.Scopes {
			if err := check(scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *Queries) renamedDeclaration(symbol *resolve.Symbol, name string) (string, resolve.Visibility, error) {
	if q.localSymbol(symbol) || symbol.Kind == resolve.TypeParam || symbol.Kind == resolve.ConstParam || symbol.Kind == resolve.Label {
		return name, resolve.Private, nil
	}
	file := q.result.Resolved.Files[symbol.Decl.File]
	path := renameDeclarationPath(reflect.ValueOf(file.AST), symbol.Decl.ID, nil)
	if len(path) == 0 {
		return "", resolve.Private, fmt.Errorf("rename requires the declaration's written source form")
	}
	goForm := true
	written := syntax.Visibility{}
	var inherited *resolve.Visibility
	merge := func(inner syntax.Visibility) {
		if inner.Kind != syntax.VisNone {
			written = inner
		}
	}
	for _, node := range path {
		switch node := node.(type) {
		case *syntax.DeclareDecl:
			merge(node.Vis)
		case *syntax.ModDecl:
			written, goForm, inherited = syntax.Visibility{}, true, nil
		case *syntax.TraitDecl:
			merge(node.Vis)
			goForm = false
			if node.Name.ID() != symbol.Decl.ID {
				inherited = &symbol.Visibility
			}
		case *syntax.EnumDecl:
			merge(node.Vis)
			goForm = false
			if node.Name.ID() != symbol.Decl.ID {
				inherited = &symbol.Visibility
			}
		case *syntax.ImplDecl:
			written, goForm = syntax.Visibility{}, false
			if node.Trait != nil {
				inherited = &symbol.Visibility
			}
		case *syntax.FuncDecl:
			merge(node.Vis)
		case *syntax.TypeDecl:
			if !node.Grouped {
				goForm = goForm && node.Form == syntax.GoType
			}
			merge(node.Vis)
		case *syntax.TypeSpec:
			merge(node.Vis)
			goForm = goForm && node.Form == syntax.GoType
		case *syntax.VarDecl:
			merge(node.Vis)
		case *syntax.ConstDecl:
			merge(node.Vis)
		case *syntax.ValueSpec:
			merge(node.Vis)
			goForm = true
		case *syntax.Field:
			merge(node.Vis)
		case *syntax.UseDecl:
			written, goForm = node.Vis, false
		case *syntax.InterfaceMethod:
			written = syntax.Visibility{}
		}
	}
	goName, visibility, ok := resolve.DeclarationName(name, written, goForm, inherited)
	if !ok {
		return "", visibility, fmt.Errorf("public name %q has no uppercase initial", name)
	}
	return goName, visibility, nil
}

func renameDeclarationPath(value reflect.Value, id syntax.NodeID, path []syntax.Node) []syntax.Node {
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		if value.Kind() == reflect.Pointer {
			if node, ok := value.Interface().(syntax.Node); ok {
				path = append(path, node)
				if node.ID() == id {
					return path
				}
			}
		}
		return renameDeclarationPath(value.Elem(), id, path)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if found := renameDeclarationPath(value.Field(i), id, path); found != nil {
				return found
			}
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if found := renameDeclarationPath(value.Index(i), id, path); found != nil {
				return found
			}
		}
	}
	return nil
}
