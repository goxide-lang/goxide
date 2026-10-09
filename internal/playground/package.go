// Package playground bounds the in-memory, single-package compiler API.
package playground

import (
	"fmt"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/frontend"
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

var sourceName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*\.gox$`)

// CompilePackage compiles one user package without reading a filesystem or
// resolving dependencies. The caller supplies the fixed build's importer.
func CompilePackage(files []File, imp types.Importer) (output []File, err error) {
	defer func() {
		if r := recover(); r != nil {
			output = nil
			err = fmt.Errorf("compiler panic: %v", r)
		}
	}()
	if len(files) == 0 || len(files) > MaxFiles {
		return nil, fmt.Errorf("expected 1 to %d source files", MaxFiles)
	}
	sources := make(map[string][]byte, len(files))
	names := make(map[string]bool, len(files))
	packageName, size := "", 0
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
		name, err := sourcePackage(file)
		if err != nil {
			return nil, err
		}
		if packageName != "" && name != packageName {
			return nil, fmt.Errorf("%s: all source files must belong to package %s (found %s)", file.Path, packageName, name)
		}
		packageName = name
		sources[file.Path] = []byte(file.Source)
	}
	if imp == nil {
		return nil, fmt.Errorf("playground requires its bundled dependency importer")
	}
	result, err := frontend.CompilePackage(PackagePath, sources, imp)
	if err != nil {
		return nil, err
	}
	emitted, err := frontend.PackageGoFiles(PackagePath, result, imp)
	if err != nil {
		return nil, err
	}
	output = make([]File, 0, len(emitted))
	size = 0
	for path, file := range emitted {
		size += len(file.Go)
		if size > MaxOutputBytes {
			return nil, fmt.Errorf("generated output exceeds 2 MiB")
		}
		if strings.HasSuffix(path, ".gox") {
			path = strings.TrimSuffix(path, ".gox") + ".go"
		}
		output = append(output, File{Path: path, Source: string(file.Go)})
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Path < output[j].Path })
	return output, nil
}

// Stop before looking beyond the package name: Go's parser scans the next token
// even in PackageClauseOnly mode, and Goxide attributes may immediately follow.
func sourcePackage(file File) (string, error) {
	fs := token.NewFileSet()
	var scan scanner.Scanner
	var failure error
	scan.Init(fs.AddFile(file.Path, -1, len(file.Source)), []byte(file.Source), func(pos token.Position, msg string) {
		if failure == nil {
			failure = fmt.Errorf("%s: %s", pos, msg)
		}
	}, 0)
	pos, tok, _ := scan.Scan()
	if failure != nil {
		return "", failure
	}
	if tok != token.PACKAGE {
		return "", fmt.Errorf("%s: expected package clause", fs.Position(pos))
	}
	pos, tok, name := scan.Scan()
	if failure != nil {
		return "", failure
	}
	if tok != token.IDENT {
		return "", fmt.Errorf("%s: expected package name", fs.Position(pos))
	}
	return name, nil
}
