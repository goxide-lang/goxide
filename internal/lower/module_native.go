package lower

import (
	"fmt"
	goast "go/ast"
	"go/build"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/token"
)

// Native signatures participate in the same resolution and type checking as
// Goxide declarations, so aliases and methods retain their package identity.
// These read-only declaration trees are never lowered or written back to disk.
func nativeModuleFiles(modules []Module, packages []*resolve.Package, importer types.Importer) (map[string]*syntax.File, error) {
	type source struct {
		key, name string
		data      []byte
		imports   []string
	}
	var sources []source
	names := map[string]string{}
	selected := map[string]bool{}
	packageNames := map[string]map[string]bool{}
	for _, pkg := range packages {
		packagePath := filepath.ToSlash(pkg.Dir)
		selected[packagePath] = true
		if packageNames[packagePath] == nil {
			packageNames[packagePath] = map[string]bool{}
		}
		packageNames[packagePath][pkg.Name] = true
		if !pkg.IsExternalTest() {
			names[packagePath] = pkg.Name
		}
	}
	for _, module := range modules {
		files, err := moduleFiles(module.Dir, ".go")
		if err != nil {
			return nil, err
		}
		for _, filename := range files {
			matches, err := build.Default.MatchFile(filepath.Dir(filename), filepath.Base(filename))
			if err != nil {
				return nil, err
			}
			if !matches {
				continue
			}
			data, err := os.ReadFile(filename)
			if err != nil {
				return nil, err
			}
			file, err := goparser.ParseFile(gotoken.NewFileSet(), filename, data, goparser.ImportsOnly|goparser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			rel, err := filepath.Rel(module.Dir, filename)
			if err != nil {
				return nil, err
			}
			src := source{key: path.Join(module.Path, filepath.ToSlash(rel)), name: file.Name.Name, data: data}
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return nil, err
				}
				src.imports = append(src.imports, value)
			}
			sources = append(sources, src)
		}
	}
	// Go-only relay packages that refer to source types must join the same
	// check. Loading their export data would create a second identity for the
	// source enum and lose its protocol implementations and payload structure.
	for changed := true; changed; {
		changed = false
		for _, src := range sources {
			packagePath := path.Dir(src.key)
			if selected[packagePath] || strings.HasSuffix(src.key, "_test.go") {
				continue
			}
			for _, imported := range src.imports {
				if selected[imported] {
					selected[packagePath] = true
					packageNames[packagePath] = map[string]bool{src.name: true}
					names[packagePath] = src.name
					changed = true
					break
				}
			}
		}
	}
	result := map[string]*syntax.File{}
	for _, src := range sources {
		if !packageNames[path.Dir(src.key)][src.name] {
			continue
		}
		set := gotoken.NewFileSet()
		file, err := goparser.ParseFile(set, src.key, src.data, goparser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		tree, err := nativeDeclarations(src.key, src.data, file, set, names, importer)
		if err != nil {
			return nil, err
		}
		result[src.key] = tree
	}
	return result, nil
}

type nativeSourceEdit struct {
	start, end int
	text       string
}

func nativeDeclarations(name string, data []byte, file *goast.File, set *gotoken.FileSet, packages map[string]string, importer types.Importer) (*syntax.File, error) {
	offset := func(position gotoken.Pos) int { return set.Position(position).Offset }
	var edits []nativeSourceEdit
	blank := func(start, end gotoken.Pos) {
		a, b := offset(start), offset(end)
		text := append([]byte(nil), data[a:b]...)
		for i, c := range text {
			if c != '\n' && c != '\r' {
				text[i] = ' '
			}
		}
		edits = append(edits, nativeSourceEdit{a, b, string(text)})
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *goast.FuncDecl:
			if declaration.Body != nil {
				blank(declaration.Body.Pos(), declaration.Body.End())
				declaration.Body = nil
			}
		case *goast.GenDecl:
			if declaration.Tok == gotoken.IMPORT {
				blank(declaration.Pos(), declaration.End())
			}
		}
	}
	aliases := map[string]string{}
	var uses []*syntax.UseDecl
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, err
		}
		alias := ""
		if imported.Name != nil {
			alias = imported.Name.Name
		} else if local := packages[importPath]; local != "" {
			alias = local
		} else {
			pkg, err := importer.Import(importPath)
			if err != nil {
				return nil, fmt.Errorf("%s: native Go import %s: %w", name, importPath, err)
			}
			alias = pkg.Name()
		}
		if alias == "_" {
			continue
		}
		span := syntax.Span{Start: uint32(offset(imported.Pos())), End: uint32(offset(imported.End()))}
		base := syntax.Base{Loc: span}
		use := &syntax.UseDecl{ItemBase: syntax.ItemBase{Base: base}, Tree: &syntax.UseTree{Base: base, Path: &syntax.Path{Base: base}}}
		for _, part := range strings.Split(importPath, "/") {
			use.Tree.Path.Segments = append(use.Tree.Path.Segments, &syntax.PathSegment{Base: base, Name: &syntax.Ident{Base: base, Name: part, Raw: true}})
		}
		if alias == "." {
			use.Tree.Glob = true
		} else {
			aliases[alias] = importPath
			use.Tree.Alias = &syntax.Ident{Base: base, Name: alias, Raw: true}
		}
		uses = append(uses, use)
	}
	var visit func(goast.Node) bool
	visit = func(node goast.Node) bool {
		switch node := node.(type) {
		case *goast.ImportSpec:
			return false
		case *goast.SelectorExpr:
			if id, ok := node.X.(*goast.Ident); ok && aliases[id.Name] != "" {
				edits = append(edits, nativeSourceEdit{offset(id.End()), offset(node.Sel.Pos()), "::"})
			}
		case *goast.Ident:
			if token.IsHardReserved(node.Name) {
				edits = append(edits, nativeSourceEdit{offset(node.Pos()), offset(node.End()), "r#" + node.Name})
			}
		case *goast.FuncLit:
			// Only the function value's signature contributes to a package-level
			// variable type; its Go body remains exclusively the Go compiler's job.
			blank(node.Body.Lbrace+1, node.Body.Rbrace)
			goast.Inspect(node.Type, visit)
			return false
		}
		return true
	}
	goast.Inspect(file, visit)
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var source strings.Builder
	cursor := 0
	for _, edit := range edits {
		source.Write(data[cursor:edit.start])
		source.WriteString(edit.text)
		cursor = edit.end
	}
	source.Write(data[cursor:])
	tree, diagnostics := parser.ParseFile(name, []byte(source.String()))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			err := &Diagnostic{File: name, Span: diagnostic.Span, Message: "native Go declaration: " + diagnostic.Message, Origin: resolve.Origin{File: name, Span: diagnostic.Span}}
			return nil, FormatSourceDiagnostic(err, map[string]*syntax.File{name: tree}, nil, nil)
		}
	}
	tree.NativeGo = true
	for _, use := range uses {
		tree.Items = append(tree.Items, use)
	}
	var id syntax.NodeID
	syntax.Walk(tree, func(node syntax.Node) bool { id++; node.(interface{ SetID(syntax.NodeID) }).SetID(id); return true })
	return tree, nil
}
