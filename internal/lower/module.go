package lower

import (
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
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

// Module is a writable staging directory prepared by the caller. Path is its
// canonical Go module path; Roots overrides its goxide.roots.toml when non-nil.
// Config supplies the invocation's explicit environment and each module's
// features. The first module supplies the Go import context and build flags.
type Module struct {
	Path, Dir string
	// RootPackage is the canonical library entry declared by a project manifest.
	// An empty value leaves native Go package imports unrestricted.
	RootPackage string
	Crates      []Crate
	Targets     []TargetBoundary
	Roots       paths.Roots
	Config      config.Config
	Context     context.Context
	Tags        []string
	Race        bool
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
	defer func() { err = FormatSourceDiagnostic(err, files, nil, expanded) }()
	diskNames := map[string]string{}
	owners := map[string]string{}
	strictRoots := map[string]string{}
	var requirements []rootRequirement
	for _, module := range modules {
		if module.Path == "" || module.Dir == "" {
			return fmt.Errorf("module path and staging directory are required")
		}
		names, err := moduleSources(module.Dir)
		if err != nil {
			return err
		}
		roots := module.Roots
		if roots == nil {
			roots, err = rootconfig.Load(module.Dir)
			if err != nil {
				return err
			}
		}
		for _, name := range names {
			matches, err := matchesModuleSource(name, module.Config.Environment.Context(module.Tags, module.Race))
			if err != nil {
				return err
			}
			if !matches {
				continue
			}
			rel, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return err
			}
			key := path.Join(module.Path, filepath.ToSlash(rel))
			if !crates.selected(module, path.Dir(key)) {
				continue
			}
			if _, exists := files[key]; exists {
				return fmt.Errorf("duplicate source file %s", key)
			}
			target := strings.TrimSuffix(name, ".gox") + ".go"
			if _, err := os.Stat(target); err == nil {
				return fmt.Errorf("%s: generated Go would overwrite an existing file", key)
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
			if _, err := expand.Configure(file, module.Config); err != nil {
				return err
			}
			configs[file] = module.Config
			fileRoots, route := crates.roots(module, path.Dir(key), roots)
			rootConfigs[file] = moduleRootConfig{module: route, name: key, roots: fileRoots}
			if fileRoots["crate"].PackagePath != "" {
				addCrateUse(file)
			}
			diskNames[key] = name
			owners[path.Dir(key)] = module.Path
			if module.Roots != nil {
				strictRoots[key] = module.Path
			}
		}
	}
	if len(files) == 0 && len(crates.owners) == 0 {
		return crates.prune()
	}
	external := sourceModuleImporter(modules[0])
	requirements = prepareSourceRoots(files, rootConfigs, external)
	packages := resolve.Resolve(crates.preliminaryFiles(files, extra), crates)
	native, err := nativeModuleFiles(modules, packages.Packages, external, crates)
	if err != nil {
		return err
	}
	for name, file := range native {
		files[name] = file
		for _, module := range modules {
			if strings.HasPrefix(name, module.Path+"/") && len(module.Path) > len(owners[path.Dir(name)]) {
				owners[path.Dir(name)] = module.Path
			}
		}
	}
	if len(native) > 0 {
		packages = resolve.Resolve(crates.preliminaryFiles(files, extra), crates)
	}
	imports := &moduleImporter{local: map[string]*resolve.Scope{}, external: external, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: owners, publicRoots: map[string]string{}, sourcePackages: map[string]bool{}, crates: crates}
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
	macros := &moduleMacros{files: files, resolved: packages, imports: imports}
	expanded, err = expand.ExpandPackages(sourcePackages, macros, configs)
	if crates.expandedModules(expanded, rootConfigs, extra) {
		return compileModules(modules, extra)
	}
	if cycle := macros.checkCycles(expanded); cycle != nil {
		return cycle
	}
	if imports.err != nil {
		return imports.err
	}
	if err != nil {
		return err
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
	info, diagnostics := check.CheckWithImporter(expandedFiles, resolved, external, modules[0].Config.Sizes())
	if len(diagnostics) != 0 {
		return diagnostics[0]
	}
	lowerFiles := make(map[string]*syntax.File, len(diskNames))
	for name := range diskNames {
		lowerFiles[name] = expandedFiles[name]
	}
	output, err := Lower(lowerFiles, info)
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
	names := make([]string, 0, len(output))
	for name := range output {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		target := strings.TrimSuffix(diskNames[name], ".gox") + ".go"
		if err := os.WriteFile(target, output[name], 0o644); err != nil {
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
