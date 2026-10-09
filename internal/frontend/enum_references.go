package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/typecheck"
)

// enumReferenceEvidence derives permissions from compiler-owned declarations or
// verified producer metadata. All concrete IDs and projection types come from
// this compilation's actual Go type graph, never source or helper spellings.
func enumReferenceEvidence(path string, files map[string][]byte, imp types.Importer, declarations map[string][]enums.Decl, metadata map[string]enumMetadata) ([]refs.Projection, map[string]bool, []refs.EnumObserver, error) {
	if path == "" {
		path = "goxide/generated"
	}
	names := sortedSources(files)
	if imp == nil && len(names) > 0 {
		imp = typecheck.NewImporter(names[0])
	}
	fs := token.NewFileSet()
	parsed := map[string]*ast.File{}
	all := []*ast.File{}
	for _, name := range names {
		f, err := parser.ParseFile(fs, name, files[name], parser.AllErrors)
		if err != nil {
			return nil, nil, nil, err
		}
		parsed[name] = f
		all = append(all, f)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Instances: map[*ast.Ident]types.Instance{}}
	pkg, _ := (&types.Config{Importer: imp, Error: func(error) {}}).Check(path, fs, all, info)
	ids := map[string]bool{}
	if pkg == nil {
		return nil, ids, nil, nil
	}
	type projectionAuthority struct {
		origin                    *types.Named
		field, payloadAlias, kind string
	}
	authorities := map[string][]projectionAuthority{}
	origins := map[*types.Named]bool{}
	key := func(fn *types.Func) string { return fn.Pkg().Path() + "." + fn.Name() }
	allow := func(n *types.Named) { origins[n.Origin()] = true }
	add := func(fn *types.Func, n *types.Named, field, alias string) {
		if fn != nil && fn.Pkg() != nil {
			authorities[key(fn)] = append(authorities[key(fn)], projectionAuthority{origin: n.Origin(), field: field, payloadAlias: alias, kind: "project"})
		}
	}
	addObserver := func(fn *types.Func, n *types.Named, field, kind string) {
		if fn != nil && fn.Pkg() != nil {
			authorities[key(fn)] = append(authorities[key(fn)], projectionAuthority{origin: n.Origin(), field: field, kind: kind})
		}
	}
	observePackage := func(owner *types.Package, n *types.Named, name, field, kind string) {
		fn, _ := owner.Scope().Lookup(name).(*types.Func)
		addObserver(fn, n, field, kind)
	}
	resolveFunction := func(f *ast.File, spelling string) *types.Func {
		expr, err := parser.ParseExpr(strings.Split(spelling, "[")[0])
		if err != nil {
			return nil
		}
		switch x := expr.(type) {
		case *ast.Ident:
			fn, _ := pkg.Scope().Lookup(x.Name).(*types.Func)
			return fn
		case *ast.SelectorExpr:
			q, ok := x.X.(*ast.Ident)
			if !ok {
				return nil
			}
			for _, im := range f.Imports {
				if im.Name != nil && im.Name.Name == q.Name {
					if pn, ok := info.Defs[im.Name].(*types.PkgName); ok {
						fn, _ := pn.Imported().Scope().Lookup(x.Sel.Name).(*types.Func)
						return fn
					}
				}
			}
		}
		return nil
	}
	// Metadata was validated when imported. Validate again against the same
	// importer before granting a generic or externally defined reference target.
	paths := make([]string, 0, len(metadata))
	for p := range metadata {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		m := metadata[p]
		if !m.present() {
			continue
		}
		owner, err := imp.Import(p)
		if err != nil {
			return nil, nil, nil, err
		}
		if err = validateEnumMetadata(m, owner, imp); err != nil {
			return nil, nil, nil, err
		}
		for _, e := range m.Enums {
			origin := owner
			if e.DefinitionPath != p {
				origin, err = imp.Import(e.DefinitionPath)
				if err != nil {
					return nil, nil, nil, err
				}
			}
			n := types.Unalias(origin.Scope().Lookup(e.DefinitionName).Type()).(*types.Named)
			allow(n)
			observePackage(origin, n, e.Tag, "", "tag")
			observePackage(origin, n, e.Validator, "", "validate")
			for _, v := range e.Variants {
				if v.HasPayload {
					fn, _ := origin.Scope().Lookup(v.Project).(*types.Func)
					add(fn, n, v.Field, v.PayloadAlias)
					observePackage(origin, n, v.Value, v.Field, "value")
				}
			}
		}
		for _, f := range m.Families {
			origin := owner
			if f.DefinitionPath != p {
				origin, err = imp.Import(f.DefinitionPath)
				if err != nil {
					return nil, nil, nil, err
				}
			}
			n := types.Unalias(origin.Scope().Lookup(f.DefinitionName).Type()).(*types.Named)
			allow(n)
			observePackage(origin, n, f.Tag, "", "tag")
			observePackage(origin, n, f.Validator, "", "validate")
			for _, instance := range f.Instances {
				owner, err := imp.Import(instance.ValidatorPath)
				if err != nil {
					return nil, nil, nil, err
				}
				observePackage(owner, n, instance.Validator, "", "validate")
			}
			for _, v := range f.Variants {
				if v.PayloadIndex >= 0 {
					fn, _ := origin.Scope().Lookup(v.Project).(*types.Func)
					add(fn, n, v.Field, "")
					observePackage(origin, n, v.Value, v.Field, "value")
				}
			}
		}
	}
	for _, name := range names {
		f := parsed[name]
		for _, d := range declarations[name] {
			value, err := types.Eval(fs, pkg, f.Pos(), d.Name)
			if err != nil {
				continue
			}
			n, ok := types.Unalias(value.Type).(*types.Named)
			if !ok || n.Obj().Pkg() == nil {
				continue
			}
			owner := d.PackagePath
			if owner == "" {
				owner = path
			}
			if d.Family != "" {
				owner = enums.SumPackage
			}
			if n.Obj().Pkg().Path() != owner || n.Obj().Name() != d.Identity {
				continue
			}
			// Imported declarations need the registered canonical origin above. Local
			// families refer to the compiler's fixed shared ABI, verified here too.
			if owner != path && !origins[n.Origin()] {
				if owner != enums.SumPackage {
					continue
				}
				sum, err := imp.Import(enums.SumPackage)
				if err != nil {
					return nil, nil, nil, err
				}
				if _, err = sharedSumMetadata(sum, imp); err != nil {
					return nil, nil, nil, err
				}
			}
			if _, ok := n.Underlying().(*types.Struct); !ok {
				continue
			}
			allow(n)
			for _, observer := range []struct{ name, kind string }{{d.Tag, "tag"}, {d.Validator, "validate"}} {
				if fn := resolveFunction(f, observer.name); fn != nil && fn.Pkg() == pkg {
					addObserver(fn, n, "", observer.kind)
				}
			}
			for _, v := range d.Variants {
				if v.Payload != "" {
					if fn := resolveFunction(f, v.Value); fn != nil && fn.Pkg() == pkg {
						addObserver(fn, n, v.Field, "value")
					}
				}

				if v.Payload == "" {
					continue
				}
				base := strings.Split(v.Project, "[")[0]
				expr, err := parser.ParseExpr(base)
				if err != nil {
					continue
				}
				var fn *types.Func
				switch x := expr.(type) {
				case *ast.Ident:
					fn, _ = pkg.Scope().Lookup(x.Name).(*types.Func)
				case *ast.SelectorExpr:
					q, ok := x.X.(*ast.Ident)
					if !ok {
						continue
					}
					for _, im := range f.Imports {
						alias := im.Name
						if alias != nil && alias.Name == q.Name {
							obj := info.Defs[alias]
							if pn, ok := obj.(*types.PkgName); ok {
								fn, _ = pn.Imported().Scope().Lookup(x.Sel.Name).(*types.Func)
							}
						}
					}
				}
				// Only this compilation's generated functions can extend local authority.
				// Imported canonical projectors were already recorded from metadata.
				if fn != nil && fn.Pkg() == pkg {
					alias := v.PayloadAlias
					if !d.Public {
						alias = ""
					}
					add(fn, n, v.Field, alias)
				}
			}
		}
	}
	typeID := refs.EnumTypeID
	visited := map[types.Type]bool{}
	var visit func(types.Type)
	visit = func(t types.Type) {
		if t == nil || visited[t] {
			return
		}
		visited[t] = true
		t = types.Unalias(t)
		switch t := t.(type) {
		case *types.Named:
			if origins[t.Origin()] {
				ids[typeID(t)] = true
				if st, ok := t.Underlying().(*types.Struct); ok {
					for i := 1; i < st.NumFields(); i++ {
						if types.Identical(st.Field(i).Type(), types.Universe.Lookup("error").Type()) {
							ids[typeID(st.Field(i).Type())] = true
						}
					}
				}
			}
			for i := 0; i < t.TypeArgs().Len(); i++ {
				visit(t.TypeArgs().At(i))
			}
			visit(t.Underlying())
		case *types.Pointer:
			visit(t.Elem())
		case *types.Slice:
			visit(t.Elem())
		case *types.Array:
			visit(t.Elem())
		case *types.Map:
			visit(t.Key())
			visit(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				visit(t.Field(i).Type())
			}
		case *types.Signature:
			visit(t.Params())
			visit(t.Results())
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				visit(t.At(i).Type())
			}
		}
	}
	for _, tv := range info.Types {
		visit(tv.Type)
	}
	for _, obj := range info.Defs {
		if obj != nil {
			visit(obj.Type())
		}
	}
	projections := []refs.Projection{}
	observers := []refs.EnumObserver{}
	seen := map[string]bool{}
	var failure error
	for _, name := range names {
		ast.Inspect(parsed[name], func(node ast.Node) bool {
			if failure != nil {
				return false
			}
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			marker, isMarker := call.Fun.(*ast.Ident)
			if !isMarker || (marker.Name != refs.ProjectRead && marker.Name != refs.ProjectWrite) || len(call.Args) != 2 {
				base := call.Fun
				switch x := base.(type) {
				case *ast.IndexExpr:
					base = x.X
				case *ast.IndexListExpr:
					base = x.X
				}
				var id *ast.Ident
				switch x := base.(type) {
				case *ast.Ident:
					id = x
				case *ast.SelectorExpr:
					id = x.Sel
				}
				fn, _ := info.Uses[id].(*types.Func)
				sig, _ := info.TypeOf(call.Fun).(*types.Signature)
				if fn == nil || fn.Pkg() == nil || sig == nil || sig.Params().Len() != 1 || sig.Variadic() {
					return true
				}
				n, ok := types.Unalias(sig.Params().At(0).Type()).(*types.Named)
				if !ok {
					return true
				}
				for _, proof := range authorities[key(fn)] {
					if proof.kind == "project" || !types.Identical(n.Origin(), proof.origin) {
						continue
					}
					resultID := ""
					switch proof.kind {
					case "validate":
						if sig.Results().Len() != 0 {
							continue
						}
					case "tag":
						if sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Int]) {
							continue
						}
						resultID = typeID(sig.Results().At(0).Type())
					case "value":
						if sig.Results().Len() != 1 {
							continue
						}
						st, ok := n.Underlying().(*types.Struct)
						if !ok {
							continue
						}
						valid := false
						for i := 0; i < st.NumFields(); i++ {
							f := st.Field(i)
							if f.Name() == proof.field && f.Pkg() == n.Obj().Pkg() && !f.Exported() && !f.Embedded() && types.Identical(f.Type(), sig.Results().At(0).Type()) {
								valid = true
								break
							}
						}
						if !valid {
							continue
						}
						resultID = typeID(sig.Results().At(0).Type())
					}
					enumID := typeID(n)
					unique := key(fn) + ":" + proof.kind + ":" + enumID + ":" + resultID
					if !seen[unique] {
						observers = append(observers, refs.EnumObserver{PackagePath: fn.Pkg().Path(), Function: fn.Name(), EnumTypeID: enumID, ResultTypeID: resultID, Kind: proof.kind, Field: proof.field})
						seen[unique] = true
					}
					break
				}
				return true
			}
			base := call.Args[0]
			switch x := base.(type) {
			case *ast.IndexExpr:
				base = x.X
			case *ast.IndexListExpr:
				base = x.X
			}
			var id *ast.Ident
			switch x := base.(type) {
			case *ast.Ident:
				id = x
			case *ast.SelectorExpr:
				id = x.Sel
			}
			fn, _ := info.Uses[id].(*types.Func)
			sig, _ := info.TypeOf(call.Args[0]).(*types.Signature)
			if fn == nil || fn.Pkg() == nil || sig == nil || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
				return true
			}
			e, eok := types.Unalias(sig.Params().At(0).Type()).(*types.Pointer)
			v, vok := types.Unalias(sig.Results().At(0).Type()).(*types.Pointer)
			if !eok || !vok {
				return true
			}
			n, ok := types.Unalias(e.Elem()).(*types.Named)
			if !ok {
				return true
			}
			for _, proof := range authorities[key(fn)] {
				if proof.kind != "project" || !types.Identical(n.Origin(), proof.origin) {
					continue
				}
				st, ok := n.Underlying().(*types.Struct)
				if !ok {
					continue
				}
				valid := false
				for i := 0; i < st.NumFields(); i++ {
					field := st.Field(i)
					if field.Name() == proof.field && field.Pkg() != nil && field.Pkg().Path() == n.Obj().Pkg().Path() && !field.Exported() && !field.Embedded() && types.Identical(field.Type(), v.Elem()) {
						valid = true
						break
					}
				}
				if !valid {
					failure = fmt.Errorf("enum projector does not address its registered typed payload: %s", key(fn))
					return false
				}
				eid, pid := typeID(e.Elem()), typeID(v.Elem())
				unique := key(fn) + ":" + eid + ":" + pid
				if !seen[unique] {
					projections = append(projections, refs.Projection{PackagePath: fn.Pkg().Path(), Function: fn.Name(), EnumTypeID: eid, PayloadTypeID: pid, PayloadAlias: proof.payloadAlias})
					seen[unique] = true
				}
				ids[eid] = true
				break
			}
			return true
		})
	}
	return projections, ids, observers, failure
}
