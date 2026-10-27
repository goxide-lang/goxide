package analysis

import (
	"context"
	"fmt"
	"go/importer"
	"go/types"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type Options struct {
	// Editor preserves reliable current declarations when source syntax is incomplete.
	Editor bool
	// PreserveSyntax retains raw source trees; CLI callers can transfer their ownership.
	PreserveSyntax bool
	// Previous reuses unchanged immutable parsing results from an earlier snapshot.
	Previous            *Result
	Config              config.Config
	RejectConfiguration bool
	ProcLibraries       []*ProcLibrary
	RuntimeRoots        []RuntimeRoot
	SourcePackages      []SourcePackage
	RequireRoot         func(string, paths.Namespace) error
	OnProcDiagnostic    func(string, expand.ProcDiagnostic)
	Producer            *ProcLibrary
}

// Input is one selected source graph. Modules load staged projects; Sources
// supplies an already selected in-memory package, including native Go files.
type Input struct {
	// Files and DiskNames are the fixed module inputs returned by LoadInput.
	Files       map[string][]byte
	DiskNames   map[string]string
	Sources     map[string][]byte
	PackagePath string
	GoImporter  types.Importer
	Roots       paths.Roots
	Options     Options
	Modules     []Module
	// Overlays replace canonical source keys before any discovery or parsing.
	Overlays map[string][]byte
}

// Result retains the exact trees and semantic tables consumed by generation.
type Result struct {
	// Err prevents a partially checked result from being used for generation.
	Err         error
	Diagnostics []error
	Regions     []Region
	// Projection is a checked editor-only view. Err still describes the source.
	Projection             *Result
	ProjectionOnly         bool
	Sources                map[string]*Source
	Config                 config.Config
	Files                  map[string]*syntax.File
	Expanded               *expand.Result
	Resolved               *resolve.Result
	Checked                *check.Info
	EmitFiles              map[string]*syntax.File
	DisplayNames           map[string]string
	DiskNames, DiskOutputs map[string]string
	RemovedFiles           []string
	SourcePackages         []SourcePackage
	Modules                []Module
	state                  *graph
}
type graph struct {
	sources                *sourceSet
	files                  map[string]*syntax.File
	configs                map[*syntax.File]config.Config
	roots                  map[*syntax.File]moduleRootConfig
	display                map[string]string
	imports                *moduleImporter
	strict                 map[string]string
	external               types.Importer
	producer               *ProcLibrary
	libraries              []*ProcLibrary
	ordinary               map[string]bool
	modules                []Module
	crates                 *crateGraph
	extra                  map[string][]crateModule
	diskNames, diskOutputs map[string]string
	options                Options
}

func (g *graph) result() *Result {
	return &Result{Sources: g.sources.files, Config: g.options.Config, Files: g.files, DisplayNames: g.display, DiskNames: g.diskNames, DiskOutputs: g.diskOutputs, Modules: g.modules, state: g}
}

// Analyze runs the same source-language pipeline for CLI, editors and the web.
func Analyze(ctx context.Context, input Input) (result *Result, err error) {
	defer func() {
		if result != nil {
			result.Err = err
			if err != nil && len(result.Diagnostics) == 0 {
				result.Diagnostics = []error{err}
			}
			if input.Options.Editor {
				err = result.finishEditor(err)
			}
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sources, err := captureSources(input)
	if err != nil {
		return &Result{Sources: sources.files}, err
	}
	extra := map[string][]crateModule{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var g *graph
		var err error
		if input.Modules != nil {
			g, err = loadModules(ctx, input, extra, sources)
		} else {
			g, err = loadPackage(input, sources)
		}
		var result *Result
		if g != nil {
			result = g.result()
		}
		if err != nil {
			return result, err
		}
		result, retry, err := analyzeGraph(ctx, g)
		if !retry {
			return result, err
		}
		sources.reset()
	}
}
func loadPackage(input Input, sourceFiles *sourceSet) (g *graph, err error) {
	g = &graph{sources: sourceFiles}
	sources, packagePath, goImports, roots, options := input.Sources, input.PackagePath, input.GoImporter, input.Roots, input.Options
	options.Previous = nil
	if packagePath == "" {
		return g, fmt.Errorf("package path is required")
	}
	if goImports == nil {
		goImports = importer.Default()
	}
	if options.Producer != nil {
		options.Producer.importer = goImports
	}
	if options.Config.Reads == nil {
		options.Config.Reads = new(config.Reads)
	}
	files := map[string]*syntax.File{}
	configs := map[*syntax.File]config.Config{}
	rootConfigs := map[*syntax.File]moduleRootConfig{}
	displayNames := map[string]string{}
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	imports := &moduleImporter{external: goImports, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: map[string]string{packagePath: packagePath}}
	strict := map[string]string{}
	g.files, g.configs, g.roots, g.display, g.imports, g.strict = files, configs, rootConfigs, displayNames, imports, strict
	g.external, g.producer, g.libraries, g.options = goImports, options.Producer, options.ProcLibraries, options
	var nativeEdges []nativeCrateImport
	for _, name := range names {
		key := path.Join(packagePath, filepath.Base(name))
		if displayNames[key] != "" {
			return g, fmt.Errorf("duplicate source file %s", key)
		}
		displayNames[key] = name
		sourceFiles.add(key, sources[name])
		data := sourceFiles.files[key].Content
		switch filepath.Ext(name) {
		case ".gox":
			file, used, err := sourceFiles.working(key, options.Config)
			files[key] = file
			if err != nil {
				return g, err
			}
			if file == nil {
				delete(files, key)
				continue
			}
			if used && options.RejectConfiguration {
				return g, fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
			}
			configs[file] = options.Config
			rootConfigs[file] = moduleRootConfig{module: packagePath, name: key, roots: roots, runtimeRoots: options.RuntimeRoots, requireRoot: options.RequireRoot}
			if roots != nil {
				strict[key] = packagePath
			}
		case ".go":
			file, err := sourceFiles.native(key)
			if err != nil {
				return g, err
			}
			if file == nil {
				continue
			}
			set := sourceFiles.files[key].GoTokens
			tree, nativeErr := nativeDeclarations(key, data, file, set, map[string]string{packagePath: file.Name.Name}, goImports)
			if nativeErr != nil {
				return g, nativeErr
			}
			files[key] = tree
			configs[tree] = options.Config
			for _, imported := range file.Imports {
				target, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return g, err
				}
				nativeEdges = append(nativeEdges, nativeCrateImport{packagePath, target, key})
			}
		default:
			return g, fmt.Errorf("%s: expected .gox or .go source", name)
		}
	}
	if err := prepareSourcePackages(options.SourcePackages, files, configs, rootConfigs, imports, goImports, sourceFiles); err != nil {
		return g, err
	}
	ordinaryFiles := make(map[string]bool, len(files))
	for name := range files {
		ordinaryFiles[name] = true
	}
	if imports.crates != nil {
		imports.crates.edges = append(imports.crates.edges, nativeEdges...)
	}
	g.ordinary = ordinaryFiles
	return g, nil
}
func loadModules(ctx context.Context, input Input, extra map[string][]crateModule, sourceFiles *sourceSet) (g *graph, err error) {
	g = &graph{sources: sourceFiles}
	modules := append([]Module(nil), input.Modules...)
	for i := range modules {
		modules[i].Context = ctx
	}
	if len(modules) == 0 {
		return g, fmt.Errorf("at least one staging module is required")
	}
	reads := new(config.Reads)
	for i := range modules {
		if modules[i].Config.Environment == nil {
			return g, fmt.Errorf("staging modules require an explicit compilation environment")
		}
		if modules[i].Config.Environment.Target != modules[0].Config.Environment.Target {
			return g, fmt.Errorf("staging modules must use the same compilation target")
		}
		if modules[i].Config.Reads == nil {
			modules[i].Config.Reads = reads
		}
	}
	crates, err := selectCrates(modules, extra, sourceFiles)
	if err != nil {
		return g, err
	}
	files := map[string]*syntax.File{}
	configs := map[*syntax.File]config.Config{}
	rootConfigs := map[*syntax.File]moduleRootConfig{}
	nativePackage := modules[0].NativePackage
	var displayNames map[string]string
	if nativePackage != nil {
		displayNames = nativePackage.DisplayNames
	}
	diskNames := map[string]string{}
	diskOutputs := map[string]string{}
	owners := map[string]string{}
	strictRoots := map[string]string{}
	g.files, g.configs, g.roots, g.display, g.strict = files, configs, rootConfigs, displayNames, strictRoots
	g.modules, g.crates, g.extra, g.diskNames, g.diskOutputs = modules, crates, extra, diskNames, diskOutputs
	g.producer = modules[0].Producer
	g.options = input.Options
	g.options.Previous = nil
	g.options.Config = modules[0].Config
	g.options.OnProcDiagnostic = modules[0].OnProcDiagnostic
	if nativePackage != nil {
		g.options.RejectConfiguration = nativePackage.RejectConfiguration
	}
	for _, module := range modules {
		g.libraries = append(g.libraries, module.ProcLibraries...)
	}
	g.external = sourceModuleImporter(modules[0])
	if nativePackage != nil && nativePackage.Importer != nil {
		g.external = nativePackage.Importer
	}
	g.imports = &moduleImporter{local: map[string]*resolve.Scope{}, external: g.external, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: owners, publicRoots: map[string]string{}, sourcePackages: map[string]bool{}, crates: crates}
	for _, module := range modules {
		g.imports.publicRoots[module.Path] = module.RootPackage
	}
	for moduleIndex, module := range modules {
		if module.Path == "" || module.Dir == "" {
			return g, fmt.Errorf("module path and staging directory are required")
		}
		names := sourceFiles.moduleFiles(module, ".gox")
		roots := sourceFiles.roots[module.Path]
		for _, name := range names {
			rel, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return g, err
			}
			key := path.Join(module.Path, filepath.ToSlash(rel))
			if displayNames[key] == "" {
				matches, err := sourceFiles.matches(name, module.Config.Environment.Context(module.Tags, module.Race))
				if err != nil {
					return g, err
				}
				if !matches {
					continue
				}
			}
			if !crates.selected(module, path.Dir(key)) {
				continue
			}
			if _, exists := files[key]; exists {
				return g, fmt.Errorf("duplicate source file %s", key)
			}
			target := strings.TrimSuffix(name, ".gox") + ".go"
			if modules[0].StableGo || displayNames[key] != "" {
				target = strings.TrimSuffix(name, ".gox") + "_gox.go"
			}
			file, used, err := sourceFiles.working(key, module.Config)
			files[key] = file
			if err != nil {
				return g, err
			}
			if file == nil {
				delete(files, key)
				continue
			}
			if used && displayNames[key] != "" && nativePackage.RejectConfiguration {
				return g, fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
			}
			configs[file] = module.Config
			fileRoots, route := crates.roots(module, path.Dir(key), roots)
			rootConfigs[file] = moduleRootConfig{module: route, name: key, roots: fileRoots, runtimeRoots: module.RuntimeRoots}
			if moduleIndex == 0 && nativePackage != nil {
				configuration := rootConfigs[file]
				configuration.requireRoot = nativePackage.RequireRoot
				rootConfigs[file] = configuration
			}
			if fileRoots["crate"].PackagePath != "" {
				addCrateUse(file)
			}
			diskNames[key] = name
			diskOutputs[key] = target
			owners[path.Dir(key)] = module.Path
			if module.Roots != nil {
				strictRoots[key] = module.Path
			}
		}
	}
	if len(files) == 0 && len(crates.owners) == 0 {
		return g, nil
	}
	g.ordinary = map[string]bool{}
	for name := range diskNames {
		g.ordinary[name] = true
		g.imports.sourcePackages[path.Dir(name)] = true
	}
	return g, nil
}
func analyzeGraph(ctx context.Context, g *graph) (result *Result, retry bool, err error) {
	result = g.result()
	files, configs, rootConfigs, imports, external, producer := g.files, g.configs, g.roots, g.imports, g.external, g.producer
	checkContext := func() error { return ctx.Err() }
	rootPackage := ""
	if producer != nil {
		rootPackage = producer.PackagePath
		producer.importer = external
	}
	entries, err := procmacro.Collect(files, rootPackage, producer != nil)
	if err != nil {
		return result, false, err
	}
	if producer != nil {
		producer.Entries = entries
		producer.GoNames = map[string]string{}
		producer.entryFiles = map[*procmacro.Entry]string{}
		for _, entry := range entries {
			for name, file := range files {
				if file == entry.File {
					producer.entryFiles[entry] = name
				}
			}
		}
		procmacro.Strip(entries)
	}
	imports.procs, err = prepareProcLibraries(g.libraries, files, configs, rootConfigs, imports.owners, g.sources)
	if err != nil {
		return result, false, err
	}
	requirements := prepareSourceRoots(files, rootConfigs, external)
	preliminary := func() map[string]*syntax.File {
		if g.crates != nil {
			return g.crates.preliminaryFiles(files, g.extra)
		}
		return files
	}
	var sourcePaths resolve.Importer
	if g.crates != nil {
		sourcePaths = g.crates
	}
	packages := resolve.Resolve(preliminary(), sourcePaths)
	if len(g.modules) != 0 {
		native, err := nativeModuleFiles(g.modules, packages.Packages, external, g.crates, g.sources)
		if err != nil {
			return result, false, err
		}
		for name, file := range native {
			files[name] = file
			for _, module := range g.modules {
				if strings.HasPrefix(name, module.Path+"/") && len(module.Path) >= len(imports.owners[path.Dir(name)]) {
					imports.owners[path.Dir(name)] = module.Path
					configs[file] = module.Config
				}
			}
		}
		if len(native) > 0 {
			packages = resolve.Resolve(preliminary(), g.crates)
		}
	}
	imports.SetSourcePackages(packages.Packages)
	for _, requirement := range requirements {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	packages = resolve.Resolve(preliminary(), imports.bindingView())
	imports.SetSourcePackages(packages.Packages)
	var groups [][]*syntax.File
	for _, pkg := range packages.Packages {
		var group []*syntax.File
		for _, file := range pkg.Files {
			group = append(group, files[file.Name])
		}
		groups = append(groups, group)
	}
	if err := checkContext(); err != nil {
		return result, false, err
	}
	macros := &moduleMacros{files: files, resolved: packages, imports: imports, producer: producer, roots: rootConfigs}
	expanded, err := expand.ExpandPackages(groups, macros, configs)
	result.Expanded = expanded
	if g.crates != nil && g.crates.expandedModules(expanded, rootConfigs, g.extra) {
		return result, true, nil
	}
	reportProcDiagnostics(g.options.OnProcDiagnostic, files, g.display, expanded)
	if err := checkContext(); err != nil {
		return result, false, err
	}
	if cycle := macros.checkCycles(expanded); cycle != nil {
		return result, false, cycle
	}
	if imports.err != nil {
		return result, false, imports.err
	}
	if err != nil {
		if !g.options.Editor {
			return result, false, err
		}
		if _, sourceError := err.(*expand.Diagnostic); !sourceError {
			return result, false, err
		}
		result.Diagnostics = append(result.Diagnostics, err)
		g.recoverExpansion(expanded)
	}
	if g.options.RejectConfiguration {
		for name, file := range files {
			if g.display[name] != "" && expanded.ConfigurationFiles[file] {
				return result, false, fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
			}
		}
	}
	for _, requirement := range applyExpandedRoots(expanded, rootConfigs, files) {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	resolved := resolve.ResolveExpanded(files, expanded, imports)
	result.Resolved = resolved
	if imports.err != nil {
		return result, false, imports.err
	}
	for _, file := range resolved.Files {
		for _, use := range namespaceUses(file) {
			if use.Implicit {
				continue
			}
			if requirement, ok := imports.namespace(use.Target); ok && use.Name.Namespace == resolve.OrdinaryNamespace {
				imports.rootAccesses = append(imports.rootAccesses, requirement)
				use.Resolved = &resolve.Symbol{Kind: resolve.Module, Visibility: resolve.Public, Target: use.Target}
			}
		}
	}
	if err := imports.finishUses(resolved, g.strict); err != nil {
		return result, false, err
	}
	if len(resolved.Diagnostics) != 0 {
		for _, diagnostic := range resolved.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
		if !g.options.Editor {
			return result, false, resolved.Diagnostics[0]
		}
	}
	if err := imports.checkSourceRoots(resolved); err != nil {
		return result, false, err
	}
	if err := imports.checkCrateImports(resolved); err != nil {
		return result, false, err
	}
	expandedFiles := map[string]*syntax.File{}
	result.EmitFiles = map[string]*syntax.File{}
	for name, file := range resolved.Files {
		expandedFiles[name] = file.AST
		if g.ordinary[name] && !file.AST.NativeGo {
			result.EmitFiles[name] = file.AST
		}
	}
	configByFile := map[string]config.Config{}
	for name, file := range files {
		configByFile[name] = configs[file]
	}
	if err := checkContext(); err != nil {
		return result, false, err
	}
	info, diagnostics := check.CheckWithOptions(expandedFiles, resolved, check.Options{Importer: external, Sizes: g.options.Config.Sizes(), ConfigByFile: configByFile})
	result.Checked = info
	if err := checkContext(); err != nil {
		return result, false, err
	}
	if len(diagnostics) != 0 {
		for _, diagnostic := range diagnostics {
			result.Diagnostics = append(result.Diagnostics, diagnostic)
		}
		return result, false, diagnostics[0]
	}
	if len(result.Diagnostics) != 0 {
		return result, false, result.Diagnostics[0]
	}
	if producer != nil {
		if err := producer.validate(resolved, info, external); err != nil {
			return result, false, err
		}
	}
	if len(g.modules) != 0 && g.modules[0].OnSourcePackages != nil {
		result.SourcePackages, err = snapshotSourcePackages(g.modules[1:], g.crates, files, rootConfigs, g.sources)
		if err != nil {
			return result, false, err
		}
	}
	if g.crates != nil {
		for pkg, names := range g.crates.files {
			if g.crates.owners[pkg] == nil {
				result.RemovedFiles = append(result.RemovedFiles, names...)
			}
		}
		sort.Strings(result.RemovedFiles)
	}
	return result, false, nil
}
func reportProcDiagnostics(report func(string, expand.ProcDiagnostic), files map[string]*syntax.File, displayNames map[string]string, expanded *expand.Result) {
	if report == nil || expanded == nil || len(expanded.ProcDiagnostics) == 0 {
		return
	}
	fileNames := make(map[*syntax.File]string, len(files))
	for name, file := range files {
		if displayNames[name] != "" {
			name = displayNames[name]
		}
		fileNames[file] = name
	}
	for _, diagnostic := range expanded.ProcDiagnostics {
		report(fileNames[diagnostic.Location.File], diagnostic)
	}
}

func (r *Result) ModuleOwner(pkg string) string { return r.state.imports.owners[pkg] }
