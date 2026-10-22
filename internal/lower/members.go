package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
)

// Member objects disambiguate equal spellings on unrelated receiver types.
// The Go compiler remains responsible for reporting type errors.
type memberRenamer struct {
	sources map[types.Object]string
	prepare func(*types.Package)
	report  func(error)
}

func (r *memberRenamer) rename(packagePath string, fset *gotoken.FileSet, files []*goast.File, names map[*goast.Ident]string, importer types.Importer, sizes types.Sizes) *types.Package {
	if r.sources == nil {
		r.sources = map[types.Object]string{}
	}
	originals := make(map[*goast.Ident]string, len(names))
	for id := range names {
		originals[id] = id.Name
	}
	for _, file := range files {
		goast.Inspect(file, func(node goast.Node) bool {
			if field, ok := node.(*goast.Field); ok && len(field.Names) == 0 {
				if id := embeddedIdent(field.Type); id != nil && names[id] != "" {
					id.Name = names[id]
				}
			}
			return true
		})
	}
	for {
		info := &types.Info{
			Defs:       map[*goast.Ident]types.Object{},
			Uses:       map[*goast.Ident]types.Object{},
			Selections: map[*goast.SelectorExpr]*types.Selection{},
			Types:      map[goast.Expr]types.TypeAndValue{},
		}
		var diagnostics []error
		config := types.Config{Importer: importer, Sizes: sizes, Error: func(err error) { diagnostics = append(diagnostics, err) }}
		var pkg *types.Package
		if r.prepare == nil {
			pkg, _ = config.Check(packagePath, fset, files, info)
		} else {
			pkg = types.NewPackage(packagePath, files[0].Name.Name)
			r.prepare(pkg)
			_ = types.NewChecker(&config, fset, pkg, info).Files(files)
		}
		desired := map[types.Object]string{}
		sources := make(map[types.Object]string, len(r.sources)+len(names))
		for obj, name := range r.sources {
			sources[obj] = name
		}
		for id, name := range names {
			if obj := memberOrigin(info.Defs[id]); obj != nil {
				desired[obj] = name
				sources[obj] = originals[id]
			}
		}
		changed := false
		rename := func(id *goast.Ident, name string) {
			if name != "" && id.Name != name {
				id.Name = name
				changed = true
			}
		}
		for id, obj := range info.Uses {
			rename(id, desired[memberOrigin(obj)])
		}
		for selector, selection := range info.Selections {
			rename(selector.Sel, desired[memberOrigin(selection.Obj())])
		}
		for _, file := range files {
			goast.Inspect(file, func(node goast.Node) bool {
				switch n := node.(type) {
				case *goast.SelectorExpr:
					if info.Selections[n] != nil {
						break
					}
					if typ := info.TypeOf(n.X); typ != nil {
						for obj, source := range sources {
							if source != n.Sel.Name || obj.Name() == source {
								continue
							}
							found, _, _ := types.LookupFieldOrMethod(typ, info.Types[n.X].Addressable(), obj.Pkg(), obj.Name())
							if memberOrigin(found) == obj {
								rename(n.Sel, obj.Name())
								break
							}
						}
					}
				case *goast.CompositeLit:
					typ := info.TypeOf(n)
					if typ == nil && n.Type != nil {
						typ = info.TypeOf(n.Type)
					}
					if typ == nil {
						break
					}
					if pointer, ok := typ.Underlying().(*types.Pointer); ok {
						typ = pointer.Elem()
					}
					structure, ok := typ.Underlying().(*types.Struct)
					if !ok {
						break
					}
					for _, elt := range n.Elts {
						pair, ok := elt.(*goast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := pair.Key.(*goast.Ident)
						if !ok {
							continue
						}
						for index := 0; index < structure.NumFields(); index++ {
							field := structure.Field(index)
							if sources[memberOrigin(field)] == key.Name {
								rename(key, field.Name())
								break
							}
						}
					}
				}
				return true
			})
		}
		for id, name := range names {
			rename(id, name)
		}
		if !changed {
			if r.report != nil {
				for _, diagnostic := range diagnostics {
					r.report(diagnostic)
				}
			}
			for id, source := range originals {
				if obj := memberOrigin(info.Defs[id]); obj != nil {
					r.sources[obj] = source
				}
			}
			return pkg
		}
	}
}

func embeddedIdent(expr goast.Expr) *goast.Ident {
	switch expr := expr.(type) {
	case *goast.Ident:
		return expr
	case *goast.SelectorExpr:
		return expr.Sel
	case *goast.StarExpr:
		return embeddedIdent(expr.X)
	case *goast.IndexExpr:
		return embeddedIdent(expr.X)
	case *goast.IndexListExpr:
		return embeddedIdent(expr.X)
	default:
		return nil
	}
}

func memberOrigin(obj types.Object) types.Object {
	switch obj := obj.(type) {
	case *types.Var:
		return obj.Origin()
	case *types.Func:
		return obj.Origin()
	default:
		return obj
	}
}
