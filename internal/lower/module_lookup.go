package lower

import (
	"fmt"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type moduleImporter struct {
	local      map[string]*resolve.Scope
	external   *moduleGoImporter
	cache      map[string]*types.Package
	namespaces map[string]rootRequirement
	owners     map[string]string
	err        error
}

type moduleMember struct {
	resolve.External
	symbol *resolve.Symbol
}

func (i *moduleImporter) SetSourcePackages(packages []*resolve.Package) {
	i.local = make(map[string]*resolve.Scope, len(packages))
	for _, pkg := range packages {
		if !pkg.IsExternalTest() {
			i.local[filepath.ToSlash(pkg.Dir)] = pkg.Scope
		}
	}
}

func (i *moduleImporter) SourcePackage(parts []string) bool {
	return i.local[strings.Join(parts, "/")] != nil
}

func (i *moduleImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	members := i.lookup(parts, name, map[string]bool{})
	var out []resolve.External
	for _, member := range members {
		out = append(out, member.External)
	}
	return out
}

func (i *moduleImporter) lookup(parts []string, name resolve.Key, visiting map[string]bool) []moduleMember {
	packagePath := strings.Join(parts, "/")
	key := fmt.Sprintf("%s::%s@%d", packagePath, name.Name, name.Ctx)
	if visiting[key] {
		return nil
	}
	visiting[key] = true
	defer delete(visiting, key)
	if requirement, ok := i.namespace(parts); ok {
		if _, child := requirement.namespace.Children[name.Name]; child && name.Ctx == 0 {
			target := append(append([]string(nil), parts...), name.Name)
			return []moduleMember{{External: resolve.External{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: target}}}
		}
		if requirement.namespace.PackagePath == "" {
			return nil
		}
		if err := requirement.check(i.owners, i.external); err != nil {
			if i.err == nil {
				i.err = err
			}
			return nil
		}
		return i.lookup(strings.Split(requirement.namespace.PackagePath, "/"), name, visiting)
	}
source:
	for split := len(parts); split > 0; split-- {
		scope := i.local[strings.Join(parts[:split], "/")]
		if scope == nil {
			continue
		}
		target := parts[:split]
		for offset, member := range parts[split:] {
			found := i.scopeMembers(scope, target, resolve.Key{Name: member}, visiting)
			if len(found) != 1 {
				if len(found) != 0 {
					return found
				}
				break source
			}
			entry := found[0]
			if entry.symbol != nil && entry.symbol.Members != nil {
				scope, target = entry.symbol.Members, entry.Target
				continue
			}
			if entry.Kind == resolve.Module || entry.Kind == resolve.Import {
				target := append(append([]string(nil), entry.Target...), parts[split+offset+1:]...)
				return i.lookup(target, name, visiting)
			}
			break source
		}
		return i.scopeMembers(scope, target, name, visiting)
	}
	pkg := i.goPackage(packagePath)
	if pkg == nil || name.Ctx != 0 {
		return nil
	}
	object := pkg.Scope().Lookup(name.Name)
	if object == nil || !object.Exported() {
		return nil
	}
	var kind resolve.Kind
	switch object.(type) {
	case *types.Const:
		kind = resolve.Constant
	case *types.Var:
		kind = resolve.Variable
	case *types.Func:
		kind = resolve.Function
	case *types.Builtin:
		kind = resolve.Builtin
	case *types.TypeName:
		kind = resolve.Type
	default:
		return nil
	}
	target := append(append([]string(nil), parts...), object.Name())
	return []moduleMember{{External: resolve.External{Name: name, Kind: kind, GoName: object.Name(), Visibility: resolve.Public, Target: target}}}
}

func (i *moduleImporter) scopeMembers(scope *resolve.Scope, target []string, name resolve.Key, visiting map[string]bool) []moduleMember {
	if symbol := scope.Bindings[name]; symbol != nil {
		if symbol.Visibility != resolve.Public {
			return nil
		}
		return i.symbolMembers(symbol, target, name, visiting)
	}
	var out []moduleMember
	for _, alias := range scope.Aliases[name] {
		if alias.Visibility == resolve.Public {
			out = append(out, i.symbolMembers(alias, target, name, visiting)...)
		}
	}
	if len(scope.Aliases[name]) == 0 {
		for _, use := range scope.Globs {
			if use.Public {
				out = append(out, i.lookup(use.Target, name, visiting)...)
			}
		}
	}
	return uniqueModuleMembers(out)
}

func (i *moduleImporter) symbolMembers(symbol *resolve.Symbol, owner []string, name resolve.Key, visiting map[string]bool) []moduleMember {
	for symbol.AliasOf != nil {
		symbol = symbol.AliasOf
		owner = i.symbolOwner(symbol)
	}
	if len(symbol.Target) > 0 {
		target := symbol.Target
		_, namespace := i.namespace(target)
		if namespace || i.SourcePackage(target) {
			return []moduleMember{{External: resolve.External{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: target}, symbol: symbol}}
		}
		if len(target) > 1 {
			found := i.lookup(target[:len(target)-1], resolve.Key{Name: target[len(target)-1], Ctx: symbol.Name.Ctx}, visiting)
			if len(found) > 0 {
				for index := range found {
					found[index].Name = name
				}
				return found
			}
		}
		if i.goPackage(strings.Join(target, "/")) != nil {
			return []moduleMember{{External: resolve.External{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: target}, symbol: symbol}}
		}
		return nil
	}
	target := append(append([]string(nil), owner...), symbol.Name.Name)
	return []moduleMember{{External: resolve.External{Name: name, Kind: symbol.Kind, GoName: symbol.GoName, Visibility: resolve.Public, Target: target}, symbol: symbol}}
}

func (i *moduleImporter) symbolOwner(symbol *resolve.Symbol) []string {
	owner := strings.Split(filepath.ToSlash(filepath.Dir(symbol.Decl.File)), "/")
	var find func(*resolve.Scope, []string) []string
	find = func(scope *resolve.Scope, target []string) []string {
		if scope == nil {
			return nil
		}
		for _, candidate := range scope.Bindings {
			if candidate == symbol {
				return target
			}
			if found := find(candidate.Members, append(append([]string(nil), target...), candidate.Name.Name)); found != nil {
				return found
			}
		}
		return nil
	}
	if found := find(i.local[strings.Join(owner, "/")], owner); found != nil {
		return found
	}
	return owner
}

func (i *moduleImporter) goPackage(path string) *types.Package {
	pkg, loaded := i.cache[path]
	if !loaded {
		pkg, _ = i.external.Import(path)
		i.cache[path] = pkg
	}
	return pkg
}

func (i *moduleImporter) namespace(parts []string) (rootRequirement, bool) {
	for split := len(parts); split > 0; split-- {
		requirement, ok := i.namespaces[strings.Join(parts[:split], "/")]
		if !ok {
			continue
		}
		for _, name := range parts[split:] {
			child, ok := requirement.namespace.Children[name]
			if !ok {
				return rootRequirement{}, false
			}
			if child.ModulePath == "" && child.Source == "" {
				child.ModulePath, child.Source = requirement.namespace.ModulePath, requirement.namespace.Source
			}
			requirement.namespace = child
		}
		return requirement, true
	}
	return rootRequirement{}, false
}

func uniqueModuleMembers(members []moduleMember) []moduleMember {
	var out []moduleMember
	seen := map[string]bool{}
	for _, member := range members {
		key := strings.Join(member.Target, "/")
		if !seen[key] {
			seen[key] = true
			out = append(out, member)
		}
	}
	return out
}

func (i *moduleImporter) finishUses(resolved *resolve.Result, strict map[string]string) error {
	var names []string
	for name := range resolved.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file := resolved.Files[name]
		for _, use := range file.Uses {
			var decl *syntax.UseDecl
			for _, item := range file.AST.Items {
				if candidate, ok := item.(*syntax.UseDecl); ok && candidate.ID() == use.Decl.ID {
					decl = candidate
					break
				}
			}
			if decl == nil {
				continue
			}
			fail := func(message string) error {
				return &Diagnostic{File: name, Span: decl.Span(), Message: message, Origin: resolved.Origins[use.Decl]}
			}
			if module := strict[name]; module != "" && !i.boundUse(file, use, decl, module) {
				return fail(fmt.Sprintf("unknown dependency root %q", decl.Tree.Path.Segments[0].Name.Name))
			}
			if use.Blank {
				if requirement, ok := i.namespace(use.Target); ok {
					if requirement.namespace.PackagePath == "" {
						return fail("blank use requires a concrete package, not a namespace group")
					}
					if err := requirement.check(i.owners, i.external); err != nil {
						return err
					}
					use.Target = strings.Split(requirement.namespace.PackagePath, "/")
				}
				if !i.SourcePackage(use.Target) {
					if _, _, err := i.external.PackageOrigin(strings.Join(use.Target, "/")); err != nil {
						return fail(err.Error())
					}
				}
			}
			// Emission associates import documentation with this canonical path.
			// Resolved symbols retain their namespace routing targets.
			if requirement, ok := i.namespace(use.Target); ok && requirement.namespace.PackagePath != "" {
				use.Target = strings.Split(requirement.namespace.PackagePath, "/")
			}
		}
	}
	return nil
}

func (i *moduleImporter) boundUse(file *resolve.File, use *resolve.UseBinding, decl *syntax.UseDecl, module string) bool {
	for _, segment := range decl.Tree.Path.Segments {
		if segment.Name.Name == "$goxide$roots" {
			return true
		}
	}
	root := decl.Tree.Path.Segments[0].Name
	key := resolve.Key{Name: root.Name, Ctx: root.Ctx}
	for scope := file.Scope; scope != nil && scope.Kind != resolve.PreludeScope; scope = scope.Parent {
		if symbol := scope.Bindings[key]; symbol != nil {
			return true
		}
		for _, alias := range scope.Aliases[key] {
			if alias != use.Symbol {
				return true
			}
		}
	}
	for split := len(use.Target); split > 0; split-- {
		if i.owners[strings.Join(use.Target[:split], "/")] == module {
			return true
		}
	}
	return false
}
