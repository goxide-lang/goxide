// Package playground bounds the in-memory, single-package compiler API.
package playground

import (
	"fmt"
	"go/types"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/typecheck"
)

const (
	MaxFiles       = 16
	MaxSourceBytes = 32768
	MaxOutputBytes = 2 * 1024 * 1024
	PackagePath    = "playground"
)

// File is an editable source or a complete generated Go file, never a fragment.
type File struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type Options struct {
	Config      config.Config
	Roots       paths.Roots
	Importer    types.Importer
	PackagePath string
}

var sourceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.gox$`)

// CompilePackage compiles one user package without reading a filesystem or
// selecting dependency versions. The caller supplies the fixed build's importer
// and explicit namespace roots.
func CompilePackage(files []File, options Options) (output []File, err error) {
	diagnosticSources := make(map[string]typecheck.DiagnosticSource, len(files))
	var expanded *expand.Result
	defer func() {
		if r := recover(); r != nil {
			output = nil
			err = fmt.Errorf("compiler panic: %v", r)
		}
		err = typecheck.SourceError(err, diagnosticSources, expanded)
	}()
	if len(files) == 0 || len(files) > MaxFiles {
		return nil, fmt.Errorf("expected 1 to %d source files", MaxFiles)
	}
	sources := make(map[string][]byte, len(files))
	names := make(map[string]bool, len(files))
	size := 0
	for _, file := range files {
		if !sourceName.MatchString(file.Path) || strings.HasSuffix(file.Path, "_test.gox") {
			return nil, fmt.Errorf("%q: expected a flat .gox filename (letters, digits, _ or -; no test files)", file.Path)
		}
		if strings.HasPrefix(file.Path, "goxide_") {
			return nil, fmt.Errorf("%s: filename is reserved for generated files", file.Path)
		}
		if names[strings.ToLower(file.Path)] {
			return nil, fmt.Errorf("%s: duplicate source path", file.Path)
		}
		names[strings.ToLower(file.Path)] = true
		size += len(file.Source)
		if size > MaxSourceBytes {
			return nil, fmt.Errorf("source collection exceeds 32 KiB")
		}
		sources[file.Path] = []byte(file.Source)
	}
	packagePath := options.PackagePath
	if packagePath == "" {
		packagePath = PackagePath
	}
	ordered := make([]string, 0, len(sources))
	for name := range sources {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	trees := make(map[string]*syntax.File, len(sources))
	var inputs []*syntax.File
	packageName := ""
	for _, name := range ordered {
		logicalName := path.Join(packagePath, name)
		file, diagnostics := parser.ParseFile(logicalName, sources[name])
		diagnosticSources[logicalName] = typecheck.DiagnosticSource{Name: name, Text: sources[name], AST: file}
		for _, diagnostic := range diagnostics {
			if diagnostic.Severity == parser.Error {
				return nil, typecheck.ParserError(name, sources[name], diagnostic)
			}
		}
		if packageName != "" && file.Package.Name != packageName {
			return nil, fmt.Errorf("%s: all source files must belong to package %s (found %s)", name, packageName, file.Package.Name)
		}
		packageName = file.Package.Name
		trees[logicalName] = file
		inputs = append(inputs, file)
	}
	if options.Importer == nil {
		return nil, fmt.Errorf("playground requires its bundled dependency importer")
	}
	if options.Config.Environment == nil {
		return nil, fmt.Errorf("playground requires an explicit export-data target and environment")
	}
	configs := make(map[*syntax.File]config.Config, len(inputs))
	originalFiles := make(map[string]*syntax.File, len(trees))
	for name, file := range trees {
		originalFiles[name] = file
	}
	// Reads belong to this request even if the immutable environment is reused.
	options.Config.Reads = &config.Reads{}
	for _, file := range inputs {
		configs[file] = options.Config
	}
	expanded, err = expand.Expand(inputs, nil, configs)
	if err != nil {
		return nil, err
	}
	imports := typecheck.NewNamespaceImporter(options.Importer, options.Roots)
	resolved := resolve.ResolveExpanded(trees, expanded, imports)
	if err := imports.Err(); err != nil {
		return nil, err
	}
	if len(resolved.Diagnostics) != 0 {
		return nil, resolved.Diagnostics[0]
	}
	for _, name := range ordered {
		logicalName := path.Join(packagePath, name)
		file := resolved.Files[logicalName]
		if err := imports.CheckUses(file); err != nil {
			return nil, err
		}
		trees[logicalName] = file.AST
	}
	checked, diagnostics := check.CheckWithImporter(trees, resolved, options.Importer, options.Config.Sizes())
	if len(diagnostics) != 0 {
		return nil, diagnostics[0]
	}
	emitted, err := lower.Lower(trees, checked)
	if err != nil {
		return nil, err
	}
	if err := typecheck.CheckPackage(packagePath, emitted, options.Importer, options.Config.Sizes()); err != nil {
		return nil, err
	}
	if err := lower.ApplyTargetConstraints(emitted, originalFiles, options.Config); err != nil {
		return nil, err
	}
	output = make([]File, 0, len(emitted))
	size = 0
	for path, file := range emitted {
		size += len(file)
		if size > MaxOutputBytes {
			return nil, fmt.Errorf("generated output exceeds 2 MiB")
		}
		if strings.HasSuffix(path, ".gox") {
			path = strings.TrimSuffix(path, ".gox") + ".go"
		}
		output = append(output, File{Path: strings.TrimPrefix(path, packagePath+"/"), Source: string(file)})
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Path < output[j].Path })
	return output, nil
}
