package lower

import (
	"bytes"
	"context"
	"fmt"
	"go/types"
	"io/fs"
	"os"
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
	"github.com/goxide-lang/goxide/internal/rootconfig"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

// Module is a writable staging directory prepared by the caller. Path is its
// canonical Go module path; Roots overrides its goxide.roots.toml when non-nil.
// Config supplies the invocation's explicit environment and each module's
// features. The first module supplies the Go import context and build flags.
type Module struct {
	Path, Dir string
	// StableGo emits definition-owned entries usable by separately compiled Go consumers.
	// Only the first module's value is used for the complete source graph.
	StableGo bool
	// RootPackage is the canonical library entry declared by a project manifest.
	// An empty value leaves native Go package imports unrestricted.
	RootPackage   string
	Crates        []Crate
	Targets       []TargetBoundary
	Roots         paths.Roots
	Config        config.Config
	Context       context.Context
	Tags          []string
	Race          bool
	ProcLibraries []*ProcLibrary
	RuntimeRoots  []RuntimeRoot
	// Only the first module's callback is used. It receives the complete source
	// graph's procedural diagnostics in expansion order, including errors, after
	// module discovery finishes; intermediate discovery passes are not reported.
	// Filename is the canonical source key, including the owning package path.
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
	// OnSourcePackages receives the final selected ordinary dependency sources,
	// excluding the first module, before they are replaced by generated Go.
	OnSourcePackages func([]SourcePackage) error
	NativePackage    *NativePackage
	procLibrary      *ProcLibrary
}

// NativePackage retains one native invocation's importer, source diagnostics,
// and output selection while it joins the complete dependency source graph.
type NativePackage struct {
	Importer                            types.Importer
	DisplayNames                        map[string]string
	Generated, Expanded                 map[string][]byte
	LineDirectives, RejectConfiguration bool
	StableGo                            bool
	RequireRoot                         func(alias string, namespace paths.Namespace) error
}

// Crate describes an authored target without changing its Go package identity.
// SourceDir retains the canonical source package path before file projection.
type Crate struct {
	Name, Root, SourceDir string
	Library               bool
	Unavailable           string
}

// TargetBoundary records authored entries, including targets not being built.
type TargetBoundary struct {
	Path string
	File bool
}

// CompileModules compiles the complete source graph in one pass. The first
// module's go.mod supplies the selected Go dependency graph. On success each
// .gox file is replaced by generated Go; native Go files and resources remain
// unchanged. Callers discard the staging directories when compilation fails.
func CompileModules(modules []Module) (err error) {
	return compileModules(modules, map[string][]crateModule{})
}

func compileModules(modules []Module, extra map[string][]crateModule) (err error) {
	if len(modules) == 0 {
		return fmt.Errorf("at least one staging module is required")
	}
	reads := new(config.Reads)
	for i := range modules {
		if modules[i].Config.Environment == nil {
			return fmt.Errorf("staging modules require an explicit compilation environment")
		}
		if modules[i].Config.Environment.Target != modules[0].Config.Environment.Target {
			return fmt.Errorf("staging modules must use the same compilation target")
		}
		if modules[i].Config.Reads == nil {
			modules[i].Config.Reads = reads
		}
	}
	crates, err := selectCrates(modules, extra)
	if err != nil {
		return err
	}
	files := map[string]*syntax.File{}
	configs := map[*syntax.File]config.Config{}
	rootConfigs := map[*syntax.File]moduleRootConfig{}
	var expanded *expand.Result
	nativePackage := modules[0].NativePackage
	stableGo := modules[0].StableGo || nativePackage != nil && nativePackage.StableGo
	var displayNames map[string]string
	if nativePackage != nil {
		displayNames = nativePackage.DisplayNames
	}
	defer func() { err = FormatSourceDiagnostic(err, files, displayNames, expanded) }()
	diskNames := map[string]string{}
	diskOutputs := map[string]string{}
	owners := map[string]string{}
	strictRoots := map[string]string{}
	var requirements []rootRequirement
	for moduleIndex, module := range modules {
		if module.Path == "" || module.Dir == "" {
			return fmt.Errorf("module path and staging directory are required")
		}
		names, err := moduleSources(module.Dir)
		if err != nil {
			return err
		}
		if module.NativePackage != nil {
			selected := make(map[string]bool, len(names))
			for _, name := range names {
				selected[name] = true
			}
			// Explicit native inputs have already been selected by the caller,
			// including names and directories omitted by package discovery.
			for key := range module.NativePackage.DisplayNames {
				relative, ok := strings.CutPrefix(key, module.Path+"/")
				if !ok || !filepath.IsLocal(filepath.FromSlash(relative)) {
					return fmt.Errorf("native source %s is outside module %s", key, module.Path)
				}
				name := filepath.Join(module.Dir, filepath.FromSlash(relative))
				if !selected[name] {
					names = append(names, name)
					selected[name] = true
				}
			}
			sort.Strings(names)
		}
		roots := module.Roots
		if roots == nil {
			roots, err = rootconfig.Load(module.Dir)
			if err != nil {
				return err
			}
		}
		for _, name := range names {
			rel, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return err
			}
			key := path.Join(module.Path, filepath.ToSlash(rel))
			if displayNames[key] == "" {
				matches, err := matchesModuleSource(name, module.Config.Environment.Context(module.Tags, module.Race))
				if err != nil {
					return err
				}
				if !matches {
					continue
				}
			}
			if !crates.selected(module, path.Dir(key)) {
				continue
			}
			if _, exists := files[key]; exists {
				return fmt.Errorf("duplicate source file %s", key)
			}
			target := strings.TrimSuffix(name, ".gox") + ".go"
			if modules[0].StableGo || displayNames[key] != "" {
				target = strings.TrimSuffix(name, ".gox") + "_gox.go"
			}
			if _, err := os.Stat(target); err == nil {
				owned, readErr := ownedGeneratedModuleFile(target)
				if readErr != nil {
					return readErr
				}
				if !owned {
					return fmt.Errorf("%s: generated Go would overwrite an existing file", key)
				}
			} else if !os.IsNotExist(err) {
				return err
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			file, diagnostics := parser.ParseFile(key, data)
			files[key] = file // Retain line starts even when parsing fails.
			for _, d := range diagnostics {
				if d.Severity == parser.Error {
					return &Diagnostic{File: key, Span: d.Span, Message: d.Message, Origin: resolve.Origin{File: key, Span: d.Span}}
				}
			}
			used, err := expand.Configure(file, module.Config)
			if err != nil {
				return err
			}
			if used && displayNames[key] != "" && nativePackage.RejectConfiguration {
				return fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
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
		return crates.prune()
	}
	producer := modules[0].procLibrary
	rootPackage := ""
	if producer != nil {
		rootPackage = producer.PackagePath
	}
	entries, err := procmacro.Collect(files, rootPackage, producer != nil)
	if err != nil {
		return err
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
	var libraries []*ProcLibrary
	for _, module := range modules {
		libraries = append(libraries, module.ProcLibraries...)
	}
	procs, err := prepareProcLibraries(libraries, files, configs, rootConfigs, owners)
	if err != nil {
		return err
	}
	var external types.Importer = sourceModuleImporter(modules[0])
	if nativePackage != nil && nativePackage.Importer != nil {
		external = nativePackage.Importer
	}
	if producer != nil {
		producer.importer = external
	}
	requirements = prepareSourceRoots(files, rootConfigs, external)
	packages := resolve.Resolve(crates.preliminaryFiles(files, extra), crates)
	native, err := nativeModuleFiles(modules, packages.Packages, external, crates)
	if err != nil {
		return err
	}
	for name, file := range native {
		files[name] = file
		for _, module := range modules {
			if strings.HasPrefix(name, module.Path+"/") && len(module.Path) >= len(owners[path.Dir(name)]) {
				owners[path.Dir(name)] = module.Path
				configs[file] = module.Config
			}
		}
	}
	if len(native) > 0 {
		packages = resolve.Resolve(crates.preliminaryFiles(files, extra), crates)
	}
	imports := &moduleImporter{local: map[string]*resolve.Scope{}, external: external, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: owners, publicRoots: map[string]string{}, sourcePackages: map[string]bool{}, crates: crates}
	imports.procs = procs
	for _, module := range modules {
		imports.publicRoots[module.Path] = module.RootPackage
	}
	for name := range diskNames {
		imports.sourcePackages[path.Dir(name)] = true
	}
	imports.SetSourcePackages(packages.Packages)
	for _, requirement := range requirements {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	var sourcePackages [][]*syntax.File
	for _, pkg := range packages.Packages {
		var source []*syntax.File
		for _, file := range pkg.Files {
			source = append(source, files[file.Name])
		}
		sourcePackages = append(sourcePackages, source)
	}
	macros := &moduleMacros{files: files, resolved: packages, imports: imports, producer: producer, roots: rootConfigs}
	expanded, err = expand.ExpandPackages(sourcePackages, macros, configs)
	if crates.expandedModules(expanded, rootConfigs, extra) {
		return compileModules(modules, extra)
	}
	reportProcDiagnostics(modules[0].OnProcDiagnostic, files, displayNames, expanded)
	if cycle := macros.checkCycles(expanded); cycle != nil {
		return cycle
	}
	if imports.err != nil {
		return imports.err
	}
	if err != nil {
		return err
	}
	if nativePackage != nil && nativePackage.RejectConfiguration {
		for name, file := range files {
			if displayNames[name] != "" && expanded.ConfigurationFiles[file] {
				return fmt.Errorf("conditional compilation requires whole-package generation; use gen with a directory input")
			}
		}
	}
	for _, requirement := range applyExpandedRoots(expanded, rootConfigs, files) {
		if _, exists := imports.namespaces[requirement.target]; !exists {
			imports.namespaces[requirement.target] = requirement
		}
	}
	resolved := resolve.ResolveExpanded(files, expanded, imports)
	if imports.err != nil {
		return imports.err
	}
	for _, file := range resolved.Files {
		for _, use := range file.Uses {
			if requirement, ok := imports.namespace(use.Target); ok {
				imports.rootAccesses = append(imports.rootAccesses, requirement)
				use.Resolved = &resolve.Symbol{Kind: resolve.Module, Visibility: resolve.Public, Target: use.Target}
			}
		}
	}
	if err := imports.finishUses(resolved, strictRoots); err != nil {
		return err
	}
	if len(resolved.Diagnostics) != 0 {
		return resolved.Diagnostics[0]
	}
	if err := imports.checkSourceRoots(resolved); err != nil {
		return err
	}
	if err := imports.checkCrateImports(resolved); err != nil {
		return err
	}
	expandedFiles := make(map[string]*syntax.File, len(resolved.Files))
	for name, file := range resolved.Files {
		expandedFiles[name] = file.AST
	}
	configByFile := make(map[string]config.Config, len(files))
	for name, original := range files {
		configByFile[name] = configs[original]
	}
	info, diagnostics := check.CheckWithOptions(expandedFiles, resolved, check.Options{Importer: external, Sizes: modules[0].Config.Sizes(), ConfigByFile: configByFile})
	if len(diagnostics) != 0 {
		return diagnostics[0]
	}
	if producer != nil {
		if err := producer.validate(resolved, info, external); err != nil {
			return err
		}
	}
	if modules[0].OnSourcePackages != nil {
		sources, err := snapshotSourcePackages(modules[1:], crates, files, rootConfigs)
		if err != nil {
			return err
		}
		if err := modules[0].OnSourcePackages(sources); err != nil {
			return err
		}
	}
	lowerFiles := make(map[string]*syntax.File, len(diskNames))
	for name := range diskNames {
		lowerFiles[name] = expandedFiles[name]
	}
	lineDirectives := nativePackage != nil && nativePackage.LineDirectives
	output, err := lower(lowerFiles, info, emissionOptions{lineDirectives: lineDirectives, originals: files, stableGo: stableGo})
	if err != nil {
		return err
	}
	for _, module := range modules {
		moduleFiles := map[string]*syntax.File{}
		for name, file := range files {
			if owners[path.Dir(name)] == module.Path {
				moduleFiles[name] = file
			}
		}
		if err := ApplyTargetConstraints(output, moduleFiles, module.Config); err != nil {
			return err
		}
	}
	if err := checkModuleCycles(modules, output, resolved, imports); err != nil {
		return err
	}
	if err := crates.prune(); err != nil {
		return err
	}
	if nativePackage != nil {
		for name, display := range displayNames {
			if nativePackage.Generated != nil {
				nativePackage.Generated[display] = output[name]
			}
			if nativePackage.Expanded != nil && resolved.Files[name] != nil {
				nativePackage.Expanded[display] = []byte(printer.Source(resolved.Files[name].AST))
			}
		}
	}
	names := make([]string, 0, len(output))
	for name := range output {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := diskOutputs[name]
		data := output[name]
		if modules[0].StableGo {
			data = append([]byte(stableGeneratedHeader), data...)
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	for _, name := range names {
		if err := os.Remove(diskNames[name]); err != nil {
			return err
		}
	}
	return nil
}

func moduleSources(root string) ([]string, error) {
	return moduleFiles(root, ".gox")
}

func moduleFiles(root, extension string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name == root {
				return nil
			}
			base := entry.Name()
			if base == "testdata" || base == "vendor" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return filepath.SkipDir
			}
			for _, marker := range []string{"go.mod", "goxide.toml"} {
				if _, err := os.Stat(filepath.Join(name, marker)); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		if filepath.Ext(name) == extension && !strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(entry.Name(), "_") {
			if extension == ".go" {
				owned, err := ownedGeneratedModuleFile(name)
				if err != nil {
					return err
				}
				if owned {
					return nil
				}
			}
			names = append(names, name)
		}
		return nil
	})
	return names, err
}

// PrepareModule lowers all .gox files beneath sourceDirs into a temporary Go
// module. Their paths are relative to the directories' common ancestor. The
// caller owns the returned directory and must remove it after use.
func PrepareModule(sourceDirs []string, modulePath string, cfg config.Config) (string, error) {
	if len(sourceDirs) == 0 || modulePath == "" {
		return "", fmt.Errorf("source directories and module path are required")
	}
	var root string
	names := map[string]bool{}
	for _, source := range sourceDirs {
		dir, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s: source directory required", dir)
		}
		if root == "" {
			root = dir
		} else {
			root = commonDirectory(root, dir)
		}
		files, err := moduleSources(dir)
		if err != nil {
			return "", err
		}
		for _, name := range files {
			names[name] = true
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no .gox source files found")
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	dir, err := os.MkdirTemp("", "goxide-lower-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(dir)
		}
	}()
	module := []byte("module " + strconv.Quote(modulePath) + "\n\ngo 1.27\n")
	if source, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		lines := strings.Split(string(source), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "module ") {
				lines[i] = "module " + strconv.Quote(modulePath)
				break
			}
		}
		module = []byte(strings.Join(lines, "\n"))
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if roots, err := os.ReadFile(filepath.Join(root, rootconfig.Filename)); err == nil {
		if err := os.WriteFile(filepath.Join(dir, rootconfig.Filename), roots, 0o644); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), module, 0o644); err != nil {
		return "", err
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, name := range ordered {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := CompileModules([]Module{{Path: modulePath, Dir: dir, Config: cfg}}); err != nil {
		return "", err
	}
	complete = true
	return dir, nil
}

const stableGeneratedHeader = "// Code generated by goxide. DO NOT EDIT.\n\n"

// Only our marked output with a corresponding source is replaced or ignored.
// A user-authored file with the same suffix remains an ordinary Go input.
func ownedGeneratedModuleFile(name string) (bool, error) {
	if !strings.HasSuffix(name, "_gox.go") {
		return false, nil
	}
	source := strings.TrimSuffix(name, "_gox.go") + ".gox"
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return false, err
	}
	return bytes.HasPrefix(data, []byte(stableGeneratedHeader)), nil
}
