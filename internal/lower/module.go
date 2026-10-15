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

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/rootconfig"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

// Module is a writable staging directory prepared by the caller. Path is its
// canonical Go module path; Roots overrides its goxide.roots.toml when non-nil.
// Context and Environment control Go imports; nil values use the background
// context and inherited environment. The first module supplies these options.
type Module struct {
	Path, Dir, GoTool string
	Roots             paths.Roots
	Context           context.Context
	Environment       []string
}

// CompileModules compiles the complete source graph in one pass. The first
// module's go.mod supplies the selected Go dependency graph. On success each
// .gox file is replaced by generated Go; native Go files and resources remain
// unchanged. Callers discard the staging directories when compilation fails.
func CompileModules(modules []Module) (err error) {
	if len(modules) == 0 {
		return fmt.Errorf("at least one staging module is required")
	}
	files := map[string]*syntax.File{}
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
			rel, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return err
			}
			key := path.Join(module.Path, filepath.ToSlash(rel))
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
			requirements = append(requirements, applyModuleRoots(module.Path, key, file, roots)...)
			diskNames[key] = name
			owners[path.Dir(key)] = module.Path
			if module.Roots != nil {
				strictRoots[key] = module.Path
			}
		}
	}
	if len(files) == 0 {
		return nil
	}
	packages := resolve.Resolve(files, nil)
	external := sourceModuleImporter(modules[0].Dir, modules[0].GoTool, modules[0].Context, modules[0].Environment)
	native, err := nativeModuleFiles(modules, packages.Packages, external)
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
		packages = resolve.Resolve(files, nil)
	}
	imports := &moduleImporter{local: map[string]*resolve.Scope{}, external: external, cache: map[string]*types.Package{}, namespaces: map[string]rootRequirement{}, owners: owners}
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
			source = append(source, file.AST)
		}
		sourcePackages = append(sourcePackages, source)
	}
	macros := &moduleMacros{files: files, resolved: packages, imports: imports}
	if err := macros.checkCycles(); err != nil {
		return err
	}
	expanded, err = expand.ExpandPackages(sourcePackages, macros)
	if imports.err != nil {
		return imports.err
	}
	if err != nil {

		return err
	}
	resolved := resolve.ResolveExpanded(files, expanded, imports)
	if imports.err != nil {
		return imports.err
	}
	if err := imports.finishUses(resolved, strictRoots); err != nil {
		return err
	}
	if len(resolved.Diagnostics) != 0 {
		return resolved.Diagnostics[0]
	}
	expandedFiles := make(map[string]*syntax.File, len(resolved.Files))
	for name, file := range resolved.Files {
		expandedFiles[name] = file.AST
	}
	info, diagnostics := check.CheckWithImporter(expandedFiles, resolved, external)
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
	if err := checkModuleCycles(modules, output, resolved, imports); err != nil {
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
func PrepareModule(sourceDirs []string, modulePath string) (string, error) {
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
	if err := CompileModules([]Module{{Path: modulePath, Dir: dir}}); err != nil {
		return "", err
	}
	complete = true
	return dir, nil
}
