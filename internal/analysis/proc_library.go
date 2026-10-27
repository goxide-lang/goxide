package analysis

import (
	"fmt"
	"go/types"
	"maps"
	"path"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// ProcLibrary retains producer source and its compile-time export table. Runnable
// libraries have passed source checking; editors may retain an Unavailable
// current producer. Run belongs to the caller's host. Compilation never executes
// a producer directly or treats its Go functions as consumer exports.
type ProcLibrary struct {
	PackagePath string
	Entries     []*procmacro.Entry
	GoNames     map[string]string
	Run         func(pm.Request) ([]byte, error)
	// Unavailable retains the current producer's source errors for editor
	// consumers. Such a library has no executable program and cannot expand.
	Unavailable *Result

	sources      map[string][]byte
	entryFiles   map[*procmacro.Entry]string
	roots        paths.Roots
	config       config.Config
	importer     types.Importer
	modulePath   string
	compiledPath string
}

func NewProcLibrary(sources map[string][]byte, packagePath string, roots paths.Roots, configuration config.Config) *ProcLibrary {
	library := &ProcLibrary{PackagePath: packagePath, modulePath: packagePath, compiledPath: packagePath, GoNames: map[string]string{}, roots: maps.Clone(roots), config: configuration}
	library.config.Features.Enabled = maps.Clone(configuration.Features.Enabled)
	library.config.Features.Declared = maps.Clone(configuration.Features.Declared)
	goSugar := configuration.GoSugarEnabled()
	library.config.GoSugar = &goSugar
	if environment := configuration.Environment; environment != nil {
		copy := *environment
		copy.SupportedTargets = append([]config.Target(nil), environment.SupportedTargets...)
		library.config.Environment = &copy
	}
	library.sources = make(map[string][]byte, len(sources))
	for name, source := range sources {
		library.sources[name] = append([]byte(nil), source...)
	}
	return library
}

func (library *ProcLibrary) validate(resolved *resolve.Result, checked *check.Info, importer types.Importer) error {
	for _, entry := range library.Entries {
		var symbol *resolve.Symbol
		if file := resolved.Files[library.entryFiles[entry]]; file != nil {
			for _, item := range file.AST.Items {
				function, ok := item.(*syntax.FuncDecl)
				if ok && len(function.Recv) == 0 && function.Path == nil && function.Name.Name == entry.Function.Name.Name && function.Span() == entry.Function.Span() {
					symbol = file.Definitions[function.Name.ID()]
				}
			}
		}
		var signature *types.Signature
		if symbol != nil {
			signature, _ = checked.Symbols[symbol].(*types.Signature)
		}
		if err := procmacro.ValidateSignature(entry, signature, importer); err != nil {
			return &Diagnostic{File: library.entryFiles[entry], Span: entry.Attribute.Span(), Message: err.Error()}
		}
		library.GoNames[entry.Name] = symbol.GoName
	}
	return nil
}

type boundProc struct {
	entry   *procmacro.Entry
	library *ProcLibrary
	file    string
}

func (i *moduleImporter) procPackage(packagePath string) bool {
	for root, entries := range i.procs {
		if packagePath == root || strings.HasPrefix(packagePath, root+"/") {
			return true
		}
		for _, entry := range entries {
			original := entry.library.compiledPath
			if packagePath == original || strings.HasPrefix(packagePath, original+"/") {
				return true
			}
		}
	}
	return false
}

func (i *moduleImporter) checkProcRuntimeUses(resolved *resolve.Result) error {
	if len(i.procs) == 0 || len(resolved.Diagnostics) != 0 {
		return nil
	}
	definitionFiles := map[*syntax.File]bool{}
	for _, entries := range i.procs {
		for _, proc := range entries {
			definitionFiles[proc.entry.File] = true
		}
	}
	procExpansion := func(id syntax.ExpnID) bool {
		for _, ancestor := range resolved.Expansions[id].Stack {
			if definitionFiles[resolved.Expansions[ancestor].Definition.File] {
				return true
			}
		}
		return definitionFiles[resolved.Expansions[id].Definition.File]
	}
	checked := map[string]bool{}
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
		var failure error
		syntax.Walk(file.AST, func(node syntax.Node) bool {
			if failure != nil {
				return false
			}
			switch node.(type) {
			case *syntax.UseDecl, *syntax.Attr:
				return false
			}
			reference := file.References[node.ID()]
			symbol := reference.Symbol
			for symbol != nil && symbol.AliasOf != nil {
				symbol = symbol.AliasOf
			}
			if symbol == nil {
				return true
			}
			origin := resolved.Origins[resolve.Ref{File: name, ID: node.ID()}]
			target := strings.Join(symbol.Target, "/")
			packagePath := path.Dir(target)
			if symbol.Kind == resolve.Module || symbol.Kind == resolve.Import {
				packagePath = target
			} else if len(symbol.Target) > 1 && procExpansion(origin.Expn) {
				// A canonical item route can include a type, trait, or enum before
				// the selected member. Recover its declaration rather than treating
				// those item names as directories in the runtime import path.
				last := len(symbol.Target) - 1
				members := i.lookup(symbol.Target[:last], resolve.Key{Name: symbol.Target[last], Ctx: symbol.Name.Ctx}, map[string]bool{})
				if len(members) == 1 && members[0].symbol != nil && members[0].symbol.Decl.File != "" {
					packagePath = path.Dir(members[0].symbol.Decl.File)
				}
			}
			if requirement, ok := i.namespace(symbol.Target); ok {
				target = requirement.namespace.PackagePath
				packagePath = target
				if procExpansion(origin.Expn) && (requirement.namespace.PackagePath != "" || requirement.namespace.Unavailable != "") {
					requirement.sourceFile = name
					if err := i.checkRootRequirement(requirement); err != nil {
						failure = err
						return false
					}
				}
			}
			if i.procPackage(target) || symbol.Kind == resolve.Macro && i.procPackage(path.Dir(symbol.Decl.File)) {
				failure = &Diagnostic{File: name, Span: node.Span(), Origin: origin, Message: "procedural macro packages provide compile-time macros, not runtime values or types"}
				return false
			}
			key := path.Dir(name) + "\x00" + packagePath
			if target != "" && packagePath != "" && !checked[key] && procExpansion(origin.Expn) {
				checked[key] = true
				err := internalPackageAccess(packagePath, name)
				if err == nil && i.local[packagePath] == nil {
					_, err = i.external.Import(packagePath)
				}
				if err != nil {
					failure = &Diagnostic{File: name, Span: node.Span(), Origin: origin, Message: fmt.Sprintf("procedural macro output requires runtime package %s in the consumer: %v", packagePath, err)}
					return false
				}
			}
			return true
		})
		if failure != nil {
			return failure
		}
	}
	return nil
}

// Each consumer gets fresh source trees. Root normalization and expansion are
// mutable, so sharing the producer's parsed trees would change later invocations.
func prepareProcLibraries(libraries []*ProcLibrary, files map[string]*syntax.File, configs map[*syntax.File]config.Config, rootConfigs map[*syntax.File]moduleRootConfig, owners map[string]string, sources *sourceSet) (map[string]map[string]boundProc, error) {
	bound := map[string]map[string]boundProc{}
	seen := map[string]*ProcLibrary{}
	for _, library := range libraries {
		if library == nil || library.PackagePath == "" || library.sources == nil {
			return nil, fmt.Errorf("procedural library must come from CompileProcLibrary")
		}
		if previous := seen[library.PackagePath]; previous != nil {
			if previous != library {
				return nil, fmt.Errorf("duplicate procedural library %s", library.PackagePath)
			}
			continue
		}
		seen[library.PackagePath] = library
		var names []string
		for name := range library.sources {
			names = append(names, name)
		}
		sort.Strings(names)
		configuration := library.config
		configuration.Reads = new(config.Reads)
		parsed := map[string]*syntax.File{}
		for _, name := range names {
			if path.Ext(name) != ".gox" {
				continue
			}
			key := path.Join(library.PackagePath, path.Base(name))
			if files[key] != nil || owners[library.PackagePath] != "" {
				return nil, fmt.Errorf("procedural library %s cannot also be a runtime source package", library.PackagePath)
			}
			sources.add(key, library.sources[name])
			file, _, err := sources.working(key, configuration)
			if err != nil {
				return nil, err
			}
			if file == nil {
				continue
			}
			parsed[key] = file
		}
		entries, err := procmacro.Collect(parsed, library.PackagePath, true)
		if err != nil {
			if library.Unavailable == nil || !sources.editor {
				return nil, err
			}
			// No reliable export table exists for this current registration. Its
			// producer diagnostics remain attached to the original source files.
			entries = nil
		}
		bound[library.PackagePath] = map[string]boundProc{}
		roots := maps.Clone(library.roots)
		if roots == nil {
			roots = paths.Roots{}
		}
		roots["crate"] = paths.Namespace{PackagePath: library.PackagePath, ModulePath: library.modulePath, Source: "module"}
		for name, file := range parsed {
			for _, entry := range entries {
				if entry.File == file {
					bound[library.PackagePath][entry.Name] = boundProc{entry, library, name}
				}
			}
			var imports []syntax.Item
			for _, item := range file.Items {
				switch declaration := item.(type) {
				case *syntax.UseDecl:
					declaration.Vis.Kind = syntax.VisNone
					imports = append(imports, declaration)
				}
			}
			file.Items = imports
			addCrateUse(file)
			files[name], configs[file] = file, configuration
			rootConfigs[file] = moduleRootConfig{module: library.PackagePath, name: name, roots: roots, sourceImporter: library.importer}
		}
		owners[library.PackagePath] = library.modulePath
	}
	return bound, nil
}
