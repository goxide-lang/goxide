// Package playground bounds the in-memory, single-package compiler API.
package playground

import (
	"context"
	"fmt"
	"go/types"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/typecheck"
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
	Config           config.Config
	Roots            paths.Roots
	Importer         types.Importer
	PackagePath      string
	ProcLibraries    []*analysis.ProcLibrary
	RuntimeRoots     []analysis.RuntimeRoot
	SourcePackages   []analysis.SourcePackage
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
}

var sourceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.gox$`)

// CompilePackage compiles one user package without reading a filesystem or
// selecting dependency versions. The caller supplies the fixed build's importer
// and declared roots plus the selected toolchain's package trees.
func CompilePackage(files []File, options Options) (output []File, err error) {
	var result *analysis.Result
	defer func() {
		if r := recover(); r != nil {
			output = nil
			err = fmt.Errorf("compiler panic: %v", r)
		}
		if result != nil {
			err = analysis.FormatSourceDiagnostic(err, result.Files, result.DisplayNames, result.Expanded)
		}
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
	// All frontend consumers use the same selected source graph. This request
	// owns its configuration reads; prepared libraries retain independent reads.
	options.Config.Reads = &config.Reads{}
	var analyzeErr error
	result, analyzeErr = analysis.Analyze(context.Background(), analysis.Input{Sources: sources, PackagePath: packagePath, GoImporter: options.Importer, Roots: options.Roots, Options: analysis.Options{Config: options.Config, ProcLibraries: options.ProcLibraries, RuntimeRoots: options.RuntimeRoots, SourcePackages: options.SourcePackages, OnProcDiagnostic: options.OnProcDiagnostic}})
	packageName := ""
	for _, name := range ordered {
		key := path.Join(packagePath, name)
		if result == nil {
			break
		}
		source := result.Sources[key]
		if source != nil {
			for _, diagnostic := range source.Diagnostics {
				if diagnostic.Severity == parser.Error {
					return nil, typecheck.ParserError(name, source.Content, diagnostic)
				}
			}
		}
		file := result.Files[key]
		if file == nil {
			continue
		}
		if packageName != "" && file.Package.Name != packageName {
			return nil, fmt.Errorf("%s: all source files must belong to package %s (found %s)", name, packageName, file.Package.Name)
		}
		packageName = file.Package.Name
	}
	if options.Importer == nil {
		return nil, fmt.Errorf("playground requires its bundled dependency importer")
	}
	if options.Config.Environment == nil {
		return nil, fmt.Errorf("playground requires an explicit export-data target and environment")
	}
	if analyzeErr != nil {
		return nil, analyzeErr
	}
	emitted, err := lower.Generate(context.Background(), result, lower.GenerationOptions{})
	if err != nil {
		return nil, err
	}
	if err := typecheck.CheckGeneratedPackage(packagePath, typecheck.GeneratedSources(result, emitted), options.Importer, options.Config.Sizes()); err != nil {
		return nil, err
	}
	output = make([]File, 0, len(emitted))
	size = 0
	for path, file := range emitted {
		if result.DisplayNames[path] == "" {
			continue
		}
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
