package lower

import (
	"fmt"
	"go/types"
	"path"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type rootRequirement struct {
	target         string
	origin         resolve.Origin
	sourceFile     string // Actual source/output owner; diagnostic spans cannot change access.
	namespace      paths.Namespace
	sourceImporter types.Importer
}

type moduleRootConfig struct {
	module, name   string
	roots          paths.Roots
	goRoots        *paths.GoNamespaces
	bindings       map[string]bool
	uses           map[string][]*syntax.UseDecl
	sourceImporter types.Importer
	runtimeRoots   []RuntimeRoot
}

func (r rootRequirement) check(owners map[string]string, external types.Importer) error {
	if r.sourceImporter != nil {
		external = r.sourceImporter
	}
	if r.namespace.Unavailable != "" {
		return &Diagnostic{File: r.origin.File, Span: r.origin.Span, Message: r.namespace.Unavailable, Origin: r.origin}
	}
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

func addCrateUse(file *syntax.File) {
	id := &syntax.Ident{Name: "crate"}
	file.Items = append(file.Items, &syntax.UseDecl{Tree: &syntax.UseTree{
		Path: &syntax.Path{Segments: []*syntax.PathSegment{{Name: id}}},
	}})
}

func prepareSourceRoots(files map[string]*syntax.File, configs map[*syntax.File]moduleRootConfig, external types.Importer) []rootRequirement {
	modules := map[string]map[string]bool{}
	var names []string
	for name, file := range files {
		names = append(names, name)
		pkg := path.Dir(name)
		if modules[pkg] == nil {
			modules[pkg] = map[string]bool{}
		}
		for _, item := range file.Items {
			collectRootDeclarations(item, modules[pkg])
		}
	}
	sort.Strings(names)
	var requirements []rootRequirement
	for _, name := range names {
		file := files[name]
		config, ok := configs[file]
		if !ok {
			continue
		}
		sourceImports := external
		if config.sourceImporter != nil {
			sourceImports = config.sourceImporter
		}
		if _, ok := sourceImports.(interface {
			PackageIndex(string, string) ([]string, error)
		}); ok {
			config.goRoots = paths.NewGoNamespaces(sourceImports)
		}
		roots := make(paths.Roots, len(config.roots))
		for name, root := range config.roots {
			if modules[root.PackagePath] != nil {
				root.GoPrefix = ""
			}
			roots[name] = root
		}
		config.roots = roots
		config.bindings = map[string]bool{}
		config.uses = map[string][]*syntax.UseDecl{}
		for name := range modules[path.Dir(name)] {
			config.bindings[name] = true
		}
		for _, item := range file.Items {
			if use, ok := item.(*syntax.UseDecl); ok && use.Tree != nil && use.Tree.Path != nil && len(use.Tree.Path.Segments) != 0 && !use.Tree.Glob {
				segments := use.Tree.Path.Segments
				name := segments[len(segments)-1].Name.Name
				if use.Tree.Alias != nil {
					name = use.Tree.Alias.Name
				}
				if name != "_" {
					config.uses[name] = append(config.uses[name], use)
				}
			}
		}
		configs[file] = config
		requirements = append(requirements, applyModuleRoots(config, file)...)
	}
	return requirements
}

func collectRootDeclarations(item syntax.Item, names map[string]bool) {
	add := func(name *syntax.Ident) {
		if name != nil {
			names[name.Name] = true
		}
	}
	switch item := item.(type) {
	case *syntax.TypeDecl:
		add(item.Name)
		for _, spec := range item.Specs {
			add(spec.Name)
		}
	case *syntax.EnumDecl:
		add(item.Name)
	case *syntax.TraitDecl:
		add(item.Name)
	case *syntax.ModDecl:
		add(item.Name)
	case *syntax.MacroRulesDecl:
		add(item.Name)
	case *syntax.DeclareDecl:
		collectRootDeclarations(item.Decl, names)
	}
}

func applyModuleRoots(config moduleRootConfig, file *syntax.File) []rootRequirement {
	var requirements []rootRequirement
	for _, item := range file.Items {
		use, ok := item.(*syntax.UseDecl)
		if !ok {
			continue
		}
		if requirement, ok := applyUseRoot(config, use, nil); ok {
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
func applyExpandedRoots(expanded *expand.Result, configs map[*syntax.File]moduleRootConfig, files map[string]*syntax.File) []rootRequirement {
	// The first pass only makes source macros available. Expansion can add
	// declarations and aliases that take precedence over those tentative roots.
	for _, file := range expanded.Files {
		for _, item := range file.Items {
			use, ok := item.(*syntax.UseDecl)
			if !ok || use.Tree == nil || use.Tree.Path == nil {
				continue
			}
			for index, segment := range use.Tree.Path.Segments {
				if segment.Name.Name == "$goxide$roots" {
					use.Tree.Path.Segments = use.Tree.Path.Segments[index+1:]
					break
				}
			}
		}
	}
	packages := make(map[*syntax.File]string, len(files))
	fileNames := make(map[*syntax.File]string, len(files))
	for name, file := range files {
		packages[file] = path.Dir(name) + "\x00" + file.Package.Name
		fileNames[file] = name
	}
	bindings := expand.NewStaticBindings(expanded, func(file *syntax.File) string { return packages[file] })
	var requirements []rootRequirement
	for _, file := range expanded.Files {
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
				if root.Name != "crate" && bindings.Bound(file, use) {
					continue
				}
				if owner, ok := configs[expanded.Scopes[root].File]; ok {
					config.module, config.roots, config.goRoots, config.bindings, config.uses = owner.module, owner.roots, owner.goRoots, owner.bindings, owner.uses
					config.sourceImporter = owner.sourceImporter
				}
			}
			config.bindings, config.uses = nil, nil // Recomputed from the expanded scopes above.
			if requirement, ok := applyUseRoot(config, use, expanded); ok {
				requirement.sourceFile = fileNames[expanded.Origins[file].File]
				if location, ok := expanded.Locations[use]; ok {
					if name, ok := fileNames[location.File]; ok {
						requirement.origin.File, requirement.origin.Span = name, location.Span
					}
				}
				requirements = append(requirements, requirement)
			}
		}
		var id syntax.NodeID
		syntax.Walk(file, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
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
	if segments[0].Name.Name != "crate" && config.bindings[segments[0].Name.Name] {
		return rootRequirement{}, false
	}
	if segments[0].Name.Name != "crate" {
		for _, binding := range config.uses[segments[0].Name.Name] {
			same := binding == use
			if expanded != nil && use.Expn == 0 {
				same = expanded.Origins[use].Span == binding.Span()
			}
			if !same {
				return rootRequirement{}, false
			}
		}
	}
	namespace, ok := config.roots[segments[0].Name.Name]
	if !ok && config.goRoots != nil {
		var err error
		namespace, ok, err = config.goRoots.Root(segments[0].Name.Name)
		if err != nil {
			namespace, ok = paths.Namespace{Unavailable: err.Error()}, true
		}
	}
	if !ok {
		return rootRequirement{}, false
	}
	copyOrigin := func(from, to syntax.Node) {
		if expanded != nil {
			expanded.Origins[to] = expanded.Origins[from]
			if location, ok := expanded.Locations[from]; ok {
				expanded.Locations[to] = location
			}
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
	return rootRequirement{target: target, origin: origin, sourceFile: config.name, namespace: namespace, sourceImporter: config.sourceImporter}, true
}
