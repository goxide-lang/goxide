package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

// Check only goxide files. Native Go callers keep Go's comparison/map semantics.
// Compiler-created Option None tests have a reserved RHS temporary, so they do
// not grant an exception to a source enum-to-enum comparison.
func checkEnumEquality(path string, sources map[string][]byte, imp types.Importer, decls []enums.Decl, single ...bool) error {
	if len(decls) == 0 {
		return nil
	}
	fs := token.NewFileSet()
	files := []*ast.File{}
	byName := map[string]*ast.File{}
	for _, name := range sortedSources(sources) {
		f, e := parser.ParseFile(fs, name, sources[name], 0)
		if e != nil {
			return e
		}
		files = append(files, f)
		byName[name] = f
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: imp, Error: func(error) {}}
	pkg, _ := conf.Check(path, fs, files, info)
	if pkg == nil {
		return nil
	}
	resolved := map[*types.Named]enums.Decl{}
	for _, d := range decls {
		for _, f := range files {
			tv, e := types.Eval(fs, pkg, f.Pos(), d.Name)
			if e != nil {
				continue
			}
			n, ok := types.Unalias(tv.Type).(*types.Named)
			if !ok {
				continue
			}
			if n.Obj().Pkg() == nil {
				continue
			}
			if d.PackagePath != "" && n.Obj().Pkg().Path() != d.PackagePath {
				continue
			}
			resolved[n.Origin()] = d
			break
		}
	}
	enumOf := func(t types.Type) (*types.Named, enums.Decl, bool) {
		if t == nil {
			return nil, enums.Decl{}, false
		}
		n, ok := types.Unalias(t).(*types.Named)
		if !ok {
			return nil, enums.Decl{}, false
		}
		d, ok := resolved[n.Origin()]
		return n, d, ok
	}
	var risk func(types.Type, map[types.Type]bool) (bool, bool)
	risk = func(t types.Type, active map[types.Type]bool) (bool, bool) {
		if t == nil || active[t] {
			return false, false
		}
		active[t] = true
		defer delete(active, t)
		if n, d, ok := enumOf(t); ok {
			if d.Family != "" {
				for i := 0; i < n.TypeArgs().Len(); i++ {
					bad, _ := risk(n.TypeArgs().At(i), active)
					if bad {
						return true, true
					}
				}
				return false, true
			}
			for _, v := range d.Variants {
				for _, f := range files {
					tv, e := types.Eval(fs, pkg, f.Pos(), v.CaseName)
					if e != nil {
						continue
					}
					bad, _ := risk(tv.Type, active)
					if bad {
						return true, true
					}
					break
				}
			}
			return false, true
		}
		switch x := types.Unalias(t).Underlying().(type) {
		case *types.Struct:
			bad, contains := false, false
			for i := 0; i < x.NumFields(); i++ {
				b, c := risk(x.Field(i).Type(), active)
				bad = bad || b
				contains = contains || c
			}
			return bad, contains
		case *types.Array:
			return risk(x.Elem(), active)
		case *types.Slice, *types.Map, *types.Signature, *types.Interface:
			return true, false
		}
		return false, false
	}
	isNil := func(x ast.Expr) bool {
		t := info.TypeOf(x)
		b, ok := t.(*types.Basic)
		return ok && b.Kind() == types.UntypedNil
	}
	for _, name := range sortedSources(sources) {
		if strings.HasSuffix(name, ".go") && !(len(single) > 0 && single[0]) {
			continue
		}
		var failure error
		ast.Inspect(byName[name], func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			fail := func(pos token.Pos, message string) {
				if failure == nil {
					failure = fmt.Errorf("%s: %s [generated/unmapped]", fs.Position(pos), message)
				}
			}
			if b, ok := node.(*ast.BinaryExpr); ok && (b.Op == token.EQL || b.Op == token.NEQ) {
				if id, ok := b.Y.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__goxide_enum_none_") {
					return true
				}
				_, _, left := enumOf(info.TypeOf(b.X))
				_, _, right := enumOf(info.TypeOf(b.Y))
				if left && right && !isNil(b.X) && !isNil(b.Y) {
					fail(b.OpPos, "enum equality requires explicit PartialEq and .equals(...); direct ==/!= is unsupported")
				}
				if !isNil(b.X) && !isNil(b.Y) {
					lb, le := risk(info.TypeOf(b.X), map[types.Type]bool{})
					rb, re := risk(info.TypeOf(b.Y), map[types.Type]bool{})
					if lb && le || rb && re {
						fail(b.OpPos, "comparison contains an enum with potentially non-comparable payload; use explicit PartialEq and .equals(...)")
					}
				}
			}
			if sw, ok := node.(*ast.SwitchStmt); ok && sw.Tag != nil {
				bad, contains := risk(info.TypeOf(sw.Tag), map[types.Type]bool{})
				if bad && contains {
					fail(sw.Tag.Pos(), "switch comparison contains an enum with potentially non-comparable payload; use match or explicit equals")
				}
				for _, stmt := range sw.Body.List {
					if clause, ok := stmt.(*ast.CaseClause); ok {
						for _, expr := range clause.List {
							bad, contains := risk(info.TypeOf(expr), map[types.Type]bool{})
							if bad && contains {
								fail(expr.Pos(), "switch case comparison contains an enum with potentially non-comparable payload; use match or explicit equals")
							}
						}
					}
				}
			}
			keyRisk := func(expr ast.Expr) {
				bad, contains := risk(info.TypeOf(expr), map[types.Type]bool{})
				if bad && contains {
					fail(expr.Pos(), "native map key value contains an enum with potentially non-comparable payload")
				}
			}
			switch n := node.(type) {
			case *ast.CallExpr:
				fn := n.Fun
				for {
					p, ok := fn.(*ast.ParenExpr)
					if !ok {
						break
					}
					fn = p.X
				}
				if id, ok := fn.(*ast.Ident); ok && len(n.Args) == 2 {
					if b, ok := info.Uses[id].(*types.Builtin); ok && b.Name() == "delete" {
						keyRisk(n.Args[1])
					}
				}
			case *ast.IndexExpr:
				if typ := info.TypeOf(n.X); typ != nil {
					if _, ok := typ.Underlying().(*types.Map); ok {
						keyRisk(n.Index)
					}
				}
			case *ast.CompositeLit:
				if typ := info.TypeOf(n); typ != nil {
					if _, ok := typ.Underlying().(*types.Map); ok {
						for _, elt := range n.Elts {
							if kv, ok := elt.(*ast.KeyValueExpr); ok {
								keyRisk(kv.Key)
							}
						}
					}
				}
			}
			if expr, ok := node.(ast.Expr); ok {
				typ := info.TypeOf(expr)
				if typ != nil {
					if m, ok := typ.Underlying().(*types.Map); ok {
						bad, contains := risk(m.Key(), map[types.Type]bool{})
						if bad && contains {
							fail(expr.Pos(), "native map key contains an enum with potentially non-comparable payload; Eq does not grant Go map key eligibility")
						}
					}
				}
			}
			return true
		})
		if failure != nil {
			return failure
		}
	}
	return nil
}
