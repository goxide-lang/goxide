package buildscript

import (
	"bytes"
	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// hostImports reads actual import declarations from selected host sources. It
// does not infer dependencies from comments, string constants, test files, or
// source files selected only for another platform. Host tools intentionally do
// not inherit the target's feature tags or race instrumentation.
func hostImports(owner string, files map[string][]byte, roots paths.Roots) ([]string, error) {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = runtime.GOOS, runtime.GOARCH, false
	ctx.BuildTags = nil
	imports := map[string]bool{}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := naming.Source(name, files[name]); err != nil {
			return nil, err
		}
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".gox" {
			continue
		}
		base := strings.TrimSuffix(filepath.Base(name), ext)
		if strings.HasSuffix(base, "_test") {
			continue
		}
		excluded := false
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(name)), "/") {
			if part != "." && (part == "testdata" || part == "vendor" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
				excluded = true
			}
		}
		if excluded {
			continue
		}
		source := files[name]
		ctx.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(source)), nil }
		selected, err := ctx.MatchFile(filepath.Dir(name), base+".go")
		if err != nil {
			return nil, err
		}
		if !selected {
			continue
		}
		if ext == ".gox" {
			packageOwner := owner
			if dir := filepath.ToSlash(filepath.Dir(name)); dir != "." {
				packageOwner += "/" + dir
			}
			plan, err := frontend.PrepareSourcePaths(packageOwner, name, source, roots, nil)
			if err != nil {
				return nil, err
			}
			for _, path := range plan.Imports {
				imports[path] = true
			}
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), name, source, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		paths := []string{}
		cgo := false
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return nil, err
			}
			if path == "C" {
				cgo = true
			}
			paths = append(paths, path)
		}
		if cgo {
			continue
		}
		for _, path := range paths {
			imports[path] = true
		}
	}
	result := make([]string, 0, len(imports))
	for path := range imports {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

// Go resolves overlapping module paths using the longest containing module.
func importedHelper(path string, modules []string) string {
	matched := ""
	for _, m := range modules {
		if (path == m || strings.HasPrefix(path, m+"/")) && len(m) > len(matched) {
			matched = m
		}
	}
	return matched
}
