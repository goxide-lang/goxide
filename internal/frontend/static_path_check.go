package frontend

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// Both normalizers retain the original byte length. Origin maps must remain
// anchored in the original .gox source, so a later typed check can distinguish
// source '.', source '::', and compiler-generated Go selectors.
func (u *usePackage) normalizeNamespacePaths(file string, source []byte, provenance *sourcemap.Map) ([]byte, error) {
	f, err := parseSourceImports(file, source)
	if err != nil {
		return nil, err
	}
	imports := defaultImportPaths(f, u.imp)
	ts := pathTokens(file, source)
	out := append([]byte(nil), source...)
	for i := 0; i+3 < len(ts); i++ {
		if ts[i].text == "#" && ts[i+1].text == "[" {
			depth := 1
			i += 2
			for i < len(ts) && depth > 0 {
				if ts[i].text == "[" {
					depth++
				}
				if ts[i].text == "]" {
					depth--
				}
				i++
			}
			i--
			continue
		}
		if ts[i].kind != token.IDENT || imports[ts[i].text] == "" || ts[i+1].kind != token.COLON || ts[i+2].kind != token.COLON {
			continue
		}
		out[ts[i+1].start], out[ts[i+2].start] = '.', ' '
	}
	if provenance != nil {
		provenance.Source = out
		provenance.Stages = append(provenance.Stages, "static namespace paths")
	}
	return out, nil
}

func (u *usePackage) normalizeTypePaths(file string, source []byte, provenance *sourcemap.Map) ([]byte, error) {
	out := append([]byte(nil), source...)
	ts := pathTokens(file, source)
	for i := 0; i+1 < len(ts); i++ {
		if ts[i].text == "#" && ts[i+1].text == "[" {
			depth := 1
			i += 2
			for i < len(ts) && depth > 0 {
				if ts[i].text == "[" {
					depth++
				}
				if ts[i].text == "]" {
					depth--
				}
				i++
			}
			i--
			continue
		}
		if ts[i].kind == token.COLON && ts[i+1].kind == token.COLON {
			out[ts[i].start], out[ts[i+1].start] = '.', ' '
			i++
		}
	}
	if provenance != nil {
		provenance.Source = out
		provenance.Stages = append(provenance.Stages, "static type paths")
	}
	return out, nil
}

// validateSelectors checks the original selector operator against actual Go
// object/type identity. Namespace spelling never authorizes value selection.
func (u *usePackage) validateSelectors(owner string, sources map[string][]byte, maps map[string]*sourcemap.Map, imp types.Importer) error {
	if u == nil {
		return nil
	}
	fs := token.NewFileSet()
	files := []*ast.File{}
	for _, name := range sortedSources(sources) {
		f, err := parser.ParseFile(fs, name, sources[name], 0)
		if err != nil {
			return err
		}
		files = append(files, f)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	config := types.Config{Importer: imp, Error: func(error) {}}
	_, _ = config.Check(owner, fs, files, info)
	var problem error
	for _, f := range files {
		file := fs.Position(f.Pos()).Filename
		mapping := maps[file]
		if mapping == nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if problem != nil {
				return false
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			seg := mapping.Lookup(fs.Position(sel.Sel.Pos()).Offset)
			// Derive/enum helper origins explain causality, not source selector
			// spelling. Only copied source or macro tokens carry that intent.
			if seg == nil || seg.Origin == nil || seg.Kind != "source" && seg.Kind != "macro" {
				return true
			}
			original, ok := u.original[seg.Origin.File]
			if !ok || !isGoxideInput(seg.Origin.File, u.single) {
				return true
			}
			tokens := pathTokens(seg.Origin.File, original)
			idx := -1
			for i, t := range tokens {
				if t.start == seg.Origin.Start && t.kind == token.IDENT {
					idx = i
					break
				}
			}
			if idx < 1 {
				return true
			}
			sourceStatic := idx >= 2 && tokens[idx-1].kind == token.COLON && tokens[idx-2].kind == token.COLON
			sourceValue := tokens[idx-1].kind == token.PERIOD
			if !sourceStatic && !sourceValue {
				return true
			}
			static := info.Types[sel.X].IsType()
			if id, ok := sel.X.(*ast.Ident); ok {
				if _, ok := info.Uses[id].(*types.PkgName); ok {
					static = true
				}
			}
			selection := info.Selections[sel]
			if selection != nil {
				static = selection.Kind() == types.MethodExpr
			}
			if sourceStatic && !static {
				problem = &sourcemap.Error{Message: "value members use '.', not '::'", Origin: seg.Origin, Stack: seg.Stack, Generated: fs.Position(sel.Sel.Pos())}
			}
			if sourceValue && static {
				problem = &sourcemap.Error{Message: "static package and type paths use '::', not '.'", Origin: seg.Origin, Stack: seg.Stack, Generated: fs.Position(sel.Sel.Pos())}
			}
			return true
		})
	}
	if problem != nil {
		return problem
	}
	return nil
}
