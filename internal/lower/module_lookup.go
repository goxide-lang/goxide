package lower

import (
	"fmt"
	"go/types"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/derives"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type moduleImporter struct {
	local          map[string]*resolve.Scope
	external       types.Importer
	cache          map[string]*types.Package
	namespaces     map[string]rootRequirement
	owners         map[string]string
	publicRoots    map[string]string
	sourcePackages map[string]bool
	macros         map[string]map[resolve.Key]*resolve.Symbol
	procs          map[string]map[string]boundProc
	rootAccesses   []rootRequirement
	crates         *crateGraph
	err            error
}

type moduleMember struct {
	resolve.External
	symbol *resolve.Symbol
}

func (i *moduleImporter) SetSourcePackages(packages []*resolve.Package) {
	i.local = make(map[string]*resolve.Scope, len(packages))
	first := i.macros == nil
	if first {
		i.macros = map[string]map[resolve.Key]*resolve.Symbol{}
	}
	for _, pkg := range packages {
		key := pkg.Dir + "\x00" + pkg.Name
		if first {
			for name, proc := range i.procs[filepath.ToSlash(pkg.Dir)] {
				identity := resolve.Key{Name: name}
				if pkg.Scope.Bindings[identity] != nil {
					i.err = &Diagnostic{File: proc.file, Span: proc.entry.Attribute.Span(), Message: "duplicate procedural macro name " + name}
					continue
				}
				pkg.Scope.Bindings[identity] = &resolve.Symbol{Name: identity, Kind: resolve.Macro,
					Decl: resolve.Ref{File: proc.file, ID: proc.entry.Function.Name.ID()}, Visibility: resolve.Public}
			}
			for name, symbol := range pkg.Scope.Bindings {
				if symbol.Kind == resolve.Macro {
					if i.macros[key] == nil {
						i.macros[key] = map[resolve.Key]*resolve.Symbol{}
					}
					i.macros[key][name] = symbol
				}
			}
		} else {
			// Expansion removes macro declarations from the emitted AST, but
			// uses still resolve their original identities and visibility.
			for name, symbol := range i.macros[key] {
				if pkg.Scope.Bindings[name] == nil {
					pkg.Scope.Bindings[name] = symbol
				}
			}
		}
		if !pkg.IsExternalTest() {
			i.local[filepath.ToSlash(pkg.Dir)] = pkg.Scope
		}
	}
}

func (i *moduleImporter) SourcePackage(parts []string) bool {
	return i.local[strings.Join(parts, "/")] != nil
}

func (i *moduleImporter) ModulePath(pkg, child string) []string {
	if i.crates != nil {
		return i.crates.ModulePath(pkg, child)
	}
	return strings.Split(pkg+"/"+child, "/")
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
	if name.Ctx == 0 {
		target := append(append([]string(nil), parts...), name.Name)
		if _, ok := i.namespace(target); ok {
			// A mapped root is itself a namespace, just like an explicit child.
			return []moduleMember{{External: resolve.External{Name: name, Kind: resolve.Module, Visibility: resolve.Public, Target: target}}}
		}
	}
	if requirement, ok := i.namespace(parts); ok {
		if requirement.namespace.Unavailable != "" {
			i.err = &Diagnostic{File: requirement.origin.File, Span: requirement.origin.Span, Message: requirement.namespace.Unavailable, Origin: requirement.origin}
			return nil
		}
		if requirement.namespace.PackagePath == "" {
			return nil
		}
		if err := i.checkRootRequirement(requirement); err != nil {
			if i.err == nil {
				i.err = err
			}
			return nil
		}
		if path.Dir(requirement.sourceFile) == requirement.namespace.PackagePath {
			if scope := i.local[requirement.namespace.PackagePath]; scope != nil {
				if symbol := scope.Bindings[name]; symbol != nil {
					return i.symbolMembers(symbol, strings.Split(requirement.namespace.PackagePath, "/"), name, visiting)
				}
			}
		}
		return i.packageMembers(strings.Split(requirement.namespace.PackagePath, "/"), name, visiting)
	}
	// A source root follows declared module/item edges one segment at a time.
	// Appending physical directories here would expose private or undeclared
	// packages, and would lose aliases in multi-segment macro paths.
	for split := len(parts) - 1; split > 0; split-- {
		if _, ok := i.namespace(parts[:split]); !ok {
			continue
		}
		target := parts[:split]
		for _, segment := range parts[split:] {
			members := i.lookup(target, resolve.Key{Name: segment}, visiting)
			if len(members) != 1 {
				return nil
			}
			target = members[0].Target
		}
		return i.lookup(target, name, visiting)
	}
	if members, found := i.sourceItemMembers(parts, name, visiting); found {
		return members
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
	return i.packageMembers(parts, name, visiting)
}

// Canonical item routes can share their spelling with physical packages.
// Keep the item's scope; explicit namespaces enter through packageMembers.
func (i *moduleImporter) sourceItemMembers(parts []string, name resolve.Key, visiting map[string]bool) ([]moduleMember, bool) {
	for split := 1; split < len(parts); split++ {
		scope := i.local[strings.Join(parts[:split], "/")]
		if scope == nil {
			continue
		}
		key := resolve.Key{Name: parts[split]}
		if symbol := scope.Bindings[key]; symbol != nil && symbol.Kind != resolve.Module && symbol.Visibility != resolve.Public {
			return nil, true
		}
		found := i.scopeMembers(scope, parts[:split], key, visiting)
		if len(found) > 1 {
			return found, true
		}
		if len(found) == 0 || found[0].Kind == resolve.Module || found[0].Kind == resolve.Import {
			continue
		}
		entry := found[0]
		for _, part := range parts[split+1:] {
			if entry.symbol == nil || entry.symbol.Members == nil {
				return nil, true
			}
			found = i.scopeMembers(entry.symbol.Members, entry.Target, resolve.Key{Name: part}, visiting)
			if len(found) != 1 {
				return found, true
			}
			entry = found[0]
		}
		if entry.symbol == nil || entry.symbol.Members == nil {
			return nil, true
		}
		return i.scopeMembers(entry.symbol.Members, entry.Target, name, visiting), true
	}
	return nil, false
}

func (i *moduleImporter) packageMembers(parts []string, name resolve.Key, visiting map[string]bool) []moduleMember {
	packagePath := strings.Join(parts, "/")
	if scope := i.local[packagePath]; scope != nil {
		return i.scopeMembers(scope, parts, name, visiting)
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
		if namespace {
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
		if i.SourcePackage(target) || i.goPackage(strings.Join(target, "/")) != nil {
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
	if i.procPackage(path) {
		return nil
	}
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
		sourceImports := i.external
		if requirement.sourceImporter != nil {
			sourceImports = requirement.sourceImporter
		}
		namespace, found, err := paths.NewGoNamespaces(sourceImports).Resolve(requirement.namespace, parts[split:])
		if err == nil && !found && split == len(parts) && requirement.namespace.PackagePath != "" {
			err = requirement.check(i.owners, i.external)
		}
		if err != nil {
			if i.err == nil {
				i.err = &Diagnostic{File: requirement.origin.File, Span: requirement.origin.Span, Message: err.Error(), Origin: requirement.origin}
			}
			return rootRequirement{}, false
		}
		requirement.namespace = namespace
		return requirement, found
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
		if i.procPackage(path.Dir(name)) {
			continue
		}
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
			for split := len(use.Target); split > 0; split-- {
				requirement, ok := i.namespace(use.Target[:split])
				if !ok || requirement.namespace.PackagePath == "" {
					continue
				}
				requirement.sourceFile = name
				if err := internalPackageAccess(requirement.namespace.PackagePath, name); err != nil {
					return fail(err.Error())
				}
				if requirement.namespace.PackagePath != "" || requirement.namespace.Unavailable != "" {
					if err := i.checkRootRequirement(requirement); err != nil {
						return err
					}
				}
				break
			}
			if i.err != nil {
				return i.err
			}
			if module := strict[name]; module != "" && !i.boundUse(use, decl, module) {
				return fail(fmt.Sprintf("unknown dependency root %q", decl.Tree.Path.Segments[0].Name.Name))
			}
			if len(resolved.Diagnostics) == 0 && !use.Blank && !i.finishUseTarget(use) {
				if i.err != nil {
					return i.err
				}
				segments := decl.Tree.Path.Segments
				return fail(fmt.Sprintf("use target %q is not a public item or namespace", segments[len(segments)-1].Name.Name))
			}
			if use.Resolved != nil {
				target := use.Resolved.Target
				if len(target) == 0 {
					target = use.Target
				}
				if use.Resolved.Kind != resolve.Module && use.Resolved.Kind != resolve.Import && len(target) > 1 {
					target = target[:len(target)-1]
				}
				if err := internalPackageAccess(strings.Join(target, "/"), name); err != nil {
					return fail(err.Error())
				}
			}
			if use.Glob && use.Resolved != nil {
				switch use.Resolved.Kind {
				case resolve.Variable, resolve.Constant, resolve.Function, resolve.Variant, resolve.Field, resolve.Label, resolve.Builtin, resolve.Macro:
					segments := decl.Tree.Path.Segments
					return fail(fmt.Sprintf("glob use target %q is not a namespace", segments[len(segments)-1].Name.Name))
				}
			}
			if use.Blank {
				if requirement, ok := i.namespace(use.Target); ok {
					requirement.sourceFile = name
					if requirement.namespace.PackagePath == "" {
						return fail("blank use requires a concrete package, not a namespace group")
					}
					if err := i.checkRootRequirement(requirement); err != nil {
						return err
					}
					use.Target = strings.Split(requirement.namespace.PackagePath, "/")
				}
				if i.procPackage(strings.Join(use.Target, "/")) {
					return fail("procedural macro packages cannot be imported for runtime side effects")
				}
				if err := internalPackageAccess(strings.Join(use.Target, "/"), name); err != nil {
					return fail(err.Error())
				}
				if !i.SourcePackage(use.Target) {
					if _, err := i.external.Import(strings.Join(use.Target, "/")); err != nil {
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
	return i.checkProcRuntimeUses(resolved)
}

func (i *moduleImporter) finishUseTarget(use *resolve.UseBinding) bool {
	if use.Resolved != nil || use.Symbol != nil && use.Symbol.AliasOf != nil {
		return true
	}
	if _, ok := i.namespace(use.Target); ok {
		return true
	}
	if len(use.Target) > 1 {
		last := len(use.Target) - 1
		members := i.Lookup(use.Target[:last], resolve.Key{Name: use.Target[last], Ctx: use.Name.Ctx})
		if len(members) == 1 {
			member := members[0]
			use.Resolved = &resolve.Symbol{Name: member.Name, Kind: member.Kind, Visibility: member.Visibility, GoName: member.GoName, Target: member.Target}
			use.Target = append([]string(nil), member.Target...)
			if use.Symbol != nil {
				use.Symbol.Target = use.Target
			}
			return true
		}
	}
	if i.SourcePackage(use.Target) || i.goPackage(strings.Join(use.Target, "/")) != nil {
		return true
	}
	// A derive registration is a compile-time use target without a Go object.
	if canonical, ok := i.CanonicalPath(use.Target); ok {
		_, registered := derives.Lookup(strings.Join(canonical[:len(canonical)-1], "/"), canonical[len(canonical)-1])
		return registered
	}
	return false
}

func (i *moduleImporter) boundUse(use *resolve.UseBinding, decl *syntax.UseDecl, module string) bool {
	if use.BoundRoot {
		return true
	}
	for _, segment := range decl.Tree.Path.Segments {
		if segment.Name.Name == "$goxide$roots" {
			return true
		}
	}
	for split := len(use.Target); split > 0; split-- {
		if i.owners[strings.Join(use.Target[:split], "/")] == module {
			return true
		}
	}
	return false
}
