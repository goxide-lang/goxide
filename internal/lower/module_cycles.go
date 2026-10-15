package lower

import (
	"bytes"
	"fmt"
	"go/build"
	goparser "go/parser"
	gotoken "go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
)

type moduleImport struct {
	target string
	file   string
	span   syntax.Span
	origin resolve.Origin
}

// Check the emitted imports: a source-only macro or an unused use does not
// create a Go dependency. Native Go files contribute edges without being edited.
func checkModuleCycles(modules []Module, output map[string][]byte, resolved *resolve.Result, imports *moduleImporter) error {
	graph := map[string][]moduleImport{}
	add := func(name string, data []byte) error {
		if strings.HasSuffix(name, "_test.gox") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		context := build.Default
		context.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
		base := strings.TrimSuffix(path.Base(name), ".gox")
		if !strings.HasSuffix(base, ".go") {
			base += ".go"
		}
		matches, err := context.MatchFile("", base)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if !matches {
			return nil
		}
		fset := gotoken.NewFileSet()
		file, err := goparser.ParseFile(fset, name, data, goparser.ImportsOnly)
		if err != nil {
			return err
		}
		owner := path.Dir(name)
		if graph[owner] == nil {
			graph[owner] = []moduleImport{}
		}
		for _, spec := range file.Imports {
			target, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			span := syntax.Span{Start: uint32(fset.Position(spec.Pos()).Offset), End: uint32(fset.Position(spec.End()).Offset)}
			edge := moduleImport{target: target, file: name, span: span, origin: resolve.Origin{File: name, Span: span}}
			if source := resolved.Files[name]; source != nil && !source.AST.NativeGo {
				edge.span = source.AST.Package.Span()
				edge.origin = resolve.Origin{File: name, Span: edge.span}
				for _, use := range source.Uses {
					useTarget := strings.Join(use.Target, "/")
					if requirement, ok := imports.namespace(use.Target); ok {
						useTarget = requirement.namespace.PackagePath
					}
					if useTarget != target && !strings.HasPrefix(useTarget, target+"/") {
						continue
					}
					for _, item := range source.AST.Items {
						if item.ID() == use.Decl.ID {
							edge.span = item.Span()
							edge.origin = resolved.Origins[use.Decl]
						}
					}
					break
				}
			}
			graph[owner] = append(graph[owner], edge)
		}
		return nil
	}
	var names []string
	for name := range output {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := add(name, output[name]); err != nil {
			return err
		}
	}
	for _, module := range modules {
		names, err := moduleFiles(module.Dir, ".go")
		if err != nil {
			return err
		}
		for _, name := range names {
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return err
			}
			if err := add(path.Join(module.Path, filepath.ToSlash(rel)), data); err != nil {
				return err
			}
		}
	}
	states := map[string]uint8{}
	var stack []string
	var visit func(string) error
	visit = func(owner string) error {
		states[owner] = 1
		stack = append(stack, owner)
		for _, edge := range graph[owner] {
			if _, local := graph[edge.target]; !local {
				continue
			}
			if states[edge.target] == 1 {
				start := 0
				for stack[start] != edge.target {
					start++
				}
				cycle := append(append([]string(nil), stack[start:]...), edge.target)
				return &Diagnostic{File: edge.file, Span: edge.span, Origin: edge.origin, Message: "Go import cycle: " + strings.Join(cycle, " -> ")}
			}
			if states[edge.target] == 0 {
				if err := visit(edge.target); err != nil {
					return err
				}
			}
		}
		states[owner] = 2
		stack = stack[:len(stack)-1]
		return nil
	}
	names = names[:0]
	for name := range graph {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if states[name] == 0 {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}
