package frontend

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/typecheck"
)

// PackageGoFiles emits a complete Go package. Compiler-owned helper declarations
// get their own file per source, so each helper retains its declaring file's
// import bindings. Source results and their diagnostic maps are left unchanged.
// The emitted package is checked again before it can be used by a caller.
func PackageGoFiles(path string, result PackageResult, imp types.Importer) (map[string]Result, error) {
	fs := token.NewFileSet()
	trees := map[string]*ast.File{}
	var all []*ast.File
	for _, name := range sortedResultNames(result) {
		f, err := parser.ParseFile(fs, name, result.Files[name].Go, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		trees[name] = f
		all = append(all, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{}}
	if _, err := (&types.Config{Importer: imp}).Check(path, fs, all, info); err != nil {
		return nil, err
	}
	out := make(map[string]Result, len(result.Files))
	for _, name := range sortedResultNames(result) {
		r, f := result.Files[name], trees[name]
		if !strings.HasSuffix(name, ".gox") {
			out[name] = r
			continue
		}
		helpers := packageHelperNames(r)
		var sourceDecls, helperDecls []ast.Decl
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
				continue
			}
			if helperDeclaration(d, helpers) {
				helperDecls = append(helperDecls, d)
			} else {
				sourceDecls = append(sourceDecls, d)
			}
		}
		if len(helperDecls) == 0 {
			out[name] = r
			continue
		}
		// Prefix the original stem so GOOS/GOARCH filename suffixes survive.
		helperName := filepath.Join(filepath.Dir(name), "goxide_helpers_"+strings.TrimSuffix(filepath.Base(name), ".gox")+".go")
		if _, exists := result.Files[helperName]; exists {
			return nil, fmt.Errorf("generated helper filename conflicts: %s", helperName)
		}
		if _, exists := result.Files[strings.TrimSuffix(helperName, ".go")+".gox"]; exists {
			return nil, fmt.Errorf("generated helper filename conflicts: %s", helperName)
		}
		// Reuse the original declaration nodes as evidence across both halves.
		// Whole-file token composition cannot preserve mappings after removal.
		mapping := r.Diagnostics
		if mapping != nil && bytes.HasPrefix(r.Go, mapping.Source) {
			// Package finishing may append an unmapped metadata marker. The
			// proven prefix stays byte-identical; the new suffix gets no origin.
			snapshot := *mapping
			snapshot.Source = r.Go
			mapping = &snapshot
		}
		evidence := mapping.CaptureAST(fs, f)
		for _, part := range []struct {
			name   string
			decls  []ast.Decl
			source bool
		}{{name, sourceDecls, true}, {helperName, helperDecls, false}} {
			tree := *f
			tree.Decls = append(outputImports(f, part.decls, info, part.source), part.decls...)
			tree.Comments = ast.NewCommentMap(fs, f, f.Comments).Filter(&tree).Comments()
			// The copied ast.File is a different CommentMap key. Retain leading
			// build constraints explicitly in BOTH emitted halves.
			for _, comment := range f.Comments {
				if comment.End() < f.Package {
					tree.Comments = append(tree.Comments, comment)
				}
			}
			sort.Slice(tree.Comments, func(i, j int) bool { return tree.Comments[i].Pos() < tree.Comments[j].Pos() })
			var b bytes.Buffer
			if err := format.Node(&b, fs, &tree); err != nil {
				return nil, err
			}
			b.WriteByte('\n')
			file := r
			file.Go, file.Expanded = b.Bytes(), b.Bytes()
			if r.Diagnostics != nil {
				file.Diagnostics = evidence.Print(&tree, file.Go, "package helper emission")
			}
			refreshPackageMap(&file)
			out[part.name] = file
		}
	}
	check := make(map[string][]byte, len(out))
	for name, r := range out {
		check[name] = r.Go
	}
	if err := typecheck.CheckPackage(path, check, imp); err != nil {
		return nil, fmt.Errorf("package helper emission: %w", err)
	}
	return out, nil
}

func packageHelperNames(r Result) map[string]bool {
	names := map[string]bool{}
	for n := range r.generatedHelpers {
		names[n] = true
	}
	for n, d := range r.equalityDerives {
		if d.PartialEq && !d.ManualPartialEq {
			names["Equals"+n] = true
		}
	}
	base := func(n string) string { n, _, _ = strings.Cut(n, "["); return n }
	for _, e := range r.Enums {
		names[e.Validator], names[e.ExportValidator] = true, true
		if e.Family != "" {
			names[e.Family] = true
		}
		for _, v := range e.Variants {
			names[base(v.CaseName)], names[base(v.Constructor)] = true, true
			if e.Family != "" {
				names["Goxide"+e.Family+v.Name] = true
				names["GoxideNew"+e.Family+v.Name] = true
			}
		}
	}
	return names
}

func helperDeclaration(d ast.Decl, helpers map[string]bool) bool {
	isHelper := func(n string) bool { return strings.HasPrefix(n, "__goxide_") || helpers[n] }
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv == nil {
			return isHelper(d.Name.Name)
		}
		recv := d.Recv.List[0].Type
		if p, ok := recv.(*ast.StarExpr); ok {
			recv = p.X
		}
		if i, ok := recv.(*ast.Ident); ok {
			return isHelper(i.Name) || helpers[i.Name+"."+d.Name.Name]
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				if !isHelper(s.Name.Name) {
					return false
				}
			case *ast.ValueSpec:
				for _, n := range s.Names {
					if !isHelper(n.Name) {
						return false
					}
				}
			default:
				return false
			}
		}
		return len(d.Specs) > 0
	}
	return false
}

func outputImports(file *ast.File, decls []ast.Decl, info *types.Info, source bool) []ast.Decl {
	used := map[*types.PkgName]bool{}
	unqualified := map[string]bool{}
	selectors := map[*ast.Ident]bool{}
	for _, d := range decls {
		ast.Inspect(d, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok {
				selectors[s.Sel] = true
			}
			return true
		})
		ast.Inspect(d, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			switch obj := info.Uses[id].(type) {
			case *types.PkgName:
				used[obj] = true
			default:
				if obj != nil && obj.Pkg() != nil && !selectors[id] {
					unqualified[obj.Pkg().Path()] = true
				}
			}
			return true
		})
	}
	var imports []ast.Decl
	for _, i := range file.Imports {
		path, _ := strconv.Unquote(i.Path.Value)
		binding, _ := info.Implicits[i].(*types.PkgName)
		if i.Name != nil {
			binding, _ = info.Defs[i.Name].(*types.PkgName)
		}
		if i.Name != nil && i.Name.Name == "_" {
			if !source {
				continue
			}
		} else if i.Name != nil && i.Name.Name == "." {
			if !unqualified[path] {
				continue
			}
		} else if !used[binding] {
			continue
		}
		imports = append(imports, &ast.GenDecl{Tok: token.IMPORT, TokPos: i.Pos(), Specs: []ast.Spec{i}})
	}
	return imports
}
