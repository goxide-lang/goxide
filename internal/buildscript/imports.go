package buildscript

import (
	"bytes"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	goxparser "github.com/hxsf/goxide/internal/syntax/parser"
)

func hostSource(name string, source []byte) (bool, error) {
	ext := filepath.Ext(name)
	if ext != ".go" && ext != ".gox" {
		return false, nil
	}
	base := strings.TrimSuffix(filepath.Base(name), ext)
	if strings.HasSuffix(base, "_test") {
		return false, nil
	}
	for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(name)), "/") {
		if part != "." && (part == "testdata" || part == "vendor" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
			return false, nil
		}
	}
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = runtime.GOOS, runtime.GOARCH, false
	ctx.BuildTags = nil
	ctx.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(source)), nil }
	return ctx.MatchFile(filepath.Dir(name), base+".go")
}

// The compiler consumes every .gox file in its staging module. Select host
// sources before staging so target-only and test files are never lowered.
func writeHostFiles(root string, files map[string][]byte) error {
	selected := make(map[string][]byte, len(files))
	for name, source := range files {
		if filepath.Ext(name) == ".gox" {
			ok, err := hostSource(name, source)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
		}
		selected[name] = source
	}
	return writeFiles(root, selected)
}

// hostImports reads actual import declarations from selected host sources. It
// does not infer dependencies from comments, string constants, test files, or
// source files selected only for another platform. Host tools intentionally do
// not inherit the target's feature tags or race instrumentation.
func hostImports(owner string, files map[string][]byte, roots paths.Roots) ([]string, error) {
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
		source := files[name]
		selected, err := hostSource(name, source)
		if err != nil {
			return nil, err
		}
		if !selected {
			continue
		}
		if filepath.Ext(name) == ".gox" {
			packageOwner := owner
			if dir := filepath.ToSlash(filepath.Dir(name)); dir != "." {
				packageOwner += "/" + dir
			}
			if err := hostGoxideImports(packageOwner, name, source, roots, imports); err != nil {
				return nil, err
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

// Dependency discovery only follows declared namespaces. Binding diagnostics
// and member visibility are checked by CompileModules after helpers are ready.
func hostGoxideImports(owner, name string, source []byte, roots paths.Roots, imports map[string]bool) error {
	file, diagnostics := goxparser.ParseFile(name, source)
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == goxparser.Error {
			return fmt.Errorf("%s: %w", name, diagnostic)
		}
	}
	namespaces := paths.Roots{}
	for name, root := range roots {
		namespaces[name] = root
	}
	var uses []*syntax.UseTree
	for _, item := range file.Items {
		switch item := item.(type) {
		case *syntax.ModDecl:
			if !item.Inline {
				path := owner + "/" + item.Name.Name
				namespaces[item.Name.Name] = paths.Namespace{PackagePath: path, Source: "local"}
				imports[path] = true
			}
		case *syntax.UseDecl:
			uses = append(uses, item.Tree)
		}
	}
	lookup := func(path *syntax.Path) (paths.Namespace, bool) {
		if path == nil || len(path.Segments) == 0 {
			return paths.Namespace{}, false
		}
		node, ok := namespaces[path.Segments[0].Name.Name]
		if !ok {
			return paths.Namespace{}, false
		}
		for _, segment := range path.Segments[1:] {
			child, ok := node.Children[segment.Name.Name]
			if !ok {
				break
			}
			node = child
		}
		return node, true
	}
	// Aliases can refer to a later use declaration in the same source file.
	for range uses {
		for _, use := range uses {
			if use.Glob || use.Path == nil || len(use.Path.Segments) == 0 {
				continue
			}
			alias := use.Path.Segments[len(use.Path.Segments)-1].Name.Name
			if use.Alias != nil {
				alias = use.Alias.Name
			}
			if node, ok := lookup(use.Path); ok && alias != "_" {
				namespaces[alias] = node
			}
		}
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		if path, ok := node.(*syntax.Path); ok {
			if namespace, ok := lookup(path); ok && namespace.PackagePath != "" {
				imports[namespace.PackagePath] = true
			}
		}
		return true
	})
	return nil
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
