package testutil

import (
	"fmt"
	"go/build"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/rootconfig"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/printer"
	"github.com/hxsf/goxide/internal/typecheck"
)

type SourceResult struct {
	Go, Expanded []byte
	Info         *check.Info
}

func Source(filename string, source []byte, roots paths.Roots) (SourceResult, error) {
	generated, info, err := Package("fixture", map[string][]byte{filename: source}, typecheck.NewImporter(filename), roots)
	if err != nil {
		return SourceResult{}, err
	}
	return SourceResult{Go: generated[filename], Expanded: []byte(printer.Source(info.Resolve.Files[filename].AST)), Info: info}, nil
}

func Package(packagePath string, sources map[string][]byte, importer types.Importer, roots paths.Roots) (map[string][]byte, *check.Info, error) {
	if importer == nil {
		importer = typecheck.NewImporter("fixture.gox")
	}
	files := map[string]*syntax.File{}
	origins := map[string]typecheck.DiagnosticSource{}
	var input []*syntax.File
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := sources[name]
		file, diagnostics := parser.ParseFile(name, source)
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == parser.Error {
				return nil, nil, typecheck.ParserError(name, source, diagnostic)
			}
		}
		files[name] = file
		origins[name] = typecheck.DiagnosticSource{Name: name, Text: source, AST: file}
		input = append(input, file)
	}
	expanded, err := expand.Expand(input, nil)
	if err != nil {
		return nil, nil, typecheck.SourceError(err, origins, expanded)
	}
	namespaces := typecheck.NewNamespaceImporter(importer, roots)
	resolved := resolve.ResolveExpanded(files, expanded, namespaces)
	if namespaces.Err() != nil {
		return nil, nil, namespaces.Err()
	}
	if len(resolved.Diagnostics) != 0 {
		return nil, nil, typecheck.SourceError(resolved.Diagnostics[0], origins, expanded)
	}
	for name, file := range resolved.Files {
		if err := namespaces.CheckUses(file); err != nil {
			return nil, nil, typecheck.SourceError(err, origins, expanded)
		}
		files[name] = file.AST
	}
	info, diagnostics := check.CheckWithImporter(files, resolved, importer)
	if len(diagnostics) != 0 {
		return nil, nil, typecheck.SourceError(diagnostics[0], origins, expanded)
	}
	generated, err := lower.Lower(files, info)
	if err == nil {
		err = typecheck.CheckPackage(packagePath, generated, importer)
	}
	return generated, info, typecheck.SourceError(err, origins, expanded)
}

type ModuleInput struct {
	Source, Destination string
	Roots               paths.Roots
}

func Module(source, destination string, roots paths.Roots) error {
	return ModuleGraph(ModuleInput{Source: source, Destination: destination, Roots: roots})
}

// ModuleGraph compiles fixture modules together from their source declarations.
// Destinations are independent writable staging directories, just as in P5.
func ModuleGraph(inputs ...ModuleInput) error {
	modules := make([]lower.Module, 0, len(inputs))
	for _, input := range inputs {
		if err := os.CopyFS(input.Destination, os.DirFS(input.Source)); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(input.Destination, "go.mod"))
		if err != nil {
			return err
		}
		module := ""
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "module" {
				module = fields[1]
				if unquoted, err := strconv.Unquote(module); err == nil {
					module = unquoted
				}
				break
			}
		}
		if module == "" {
			return fmt.Errorf("fixture go.mod has no module path")
		}
		roots := input.Roots
		if roots == nil {
			roots, err = rootconfig.Load(input.Source)
			if err != nil {
				return err
			}
		}

		modules = append(modules, lower.Module{Path: module, Dir: input.Destination, Roots: roots})
	}
	if err := lower.CompileModules(modules); err != nil {
		return err
	}
	for _, module := range modules {
		if err := checkGeneratedModule(module.Path, module.Dir); err != nil {
			return err
		}
	}
	return nil
}

func checkGeneratedModule(module, destination string) error {
	packages := map[string]map[string][]byte{}
	err := filepath.WalkDir(destination, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != destination && (strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			if path != destination {
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.Dir(path)
		matches, err := build.Default.MatchFile(dir, entry.Name())
		if err != nil || !matches {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if packages[dir] == nil {
			packages[dir] = map[string][]byte{}
		}
		packages[dir][path] = data
		return nil
	})
	if err != nil {
		return err
	}
	dirs := make([]string, 0, len(packages))
	for dir := range packages {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	importer := typecheck.NewImporter(filepath.Join(destination, "fixture.go"))
	for _, dir := range dirs {
		relative, err := filepath.Rel(destination, dir)
		if err != nil {
			return err
		}
		packagePath := module
		if relative != "." {
			packagePath += "/" + filepath.ToSlash(relative)
		}
		if err := typecheck.CheckPackage(packagePath, packages[dir], importer); err != nil {
			return err
		}
	}
	return nil
}
