package refs

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Projection is frontend evidence for one concrete registered enum projection.
// Type IDs use types.TypeString with full package paths. Alias spellings, when
// supplied, belong to the helper's package and are checked against its actual
// signature; they are not authorization by spelling.
type Projection struct {
	PackagePath, Function     string
	EnumTypeID, PayloadTypeID string
	EnumAlias, PayloadAlias   string
}

const (
	ProjectRead  = "__goxide_enum_project_read"
	ProjectWrite = "__goxide_enum_project_write"
)

// BindProjectionsWithPath replaces compiler projection requests with checked
// reference signatures. The returned bindings must be restored only after
// Finish succeeds. No stub or additional function frame survives in output.
func BindProjectionsWithPath(path string, files map[string][]byte, imp types.Importer, registry []Projection) (map[string][]byte, ImportBindings, error) {
	if imp == nil {
		imp = importer.Default()
	}
	path = packagePath(path)
	bindings := ImportBindings{Originals: map[string]string{}, Stubs: map[string]bool{}, Imports: map[string]bool{}, ReferenceElements: map[string]bool{}, Copied: map[string][]int{}, Stage: "refs.BindProjections"}
	fs := token.NewFileSet()
	parsed := map[string]*ast.File{}
	var all []*ast.File
	used := map[string]bool{}
	for _, name := range sourceNames(files) {
		f, err := parser.ParseFile(fs, name, files[name], parser.AllErrors)
		if err != nil {
			return nil, bindings, err
		}
		parsed[name] = f
		all = append(all, f)
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Instances: map[*ast.Ident]types.Instance{}}
	deduplicateIR(all)
	_, _ = (&types.Config{Importer: imp, Error: func(error) {}}).Check(path, fs, all, info)
	serial := 0
	fresh := func(kind string) string {
		for {
			serial++
			name := fmt.Sprintf("__goxide_permission_%s_%d", kind, serial)
			if !used[name] {
				used[name] = true
				return name
			}
		}
	}
	type expected struct {
		enum, payload types.Type
		position      token.Position
	}
	expect := map[string]expected{}
	out := map[string][]byte{}
	for _, name := range sourceNames(files) {
		f := parsed[name]
		source := files[name]
		offset := func(p token.Pos) int { return fs.PositionFor(p, false).Offset }
		aliases := map[string]string{}
		var routes []*types.Package
		for _, spec := range f.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			pkg, err := imp.Import(p)
			if err != nil {
				return nil, bindings, err
			}
			routes = append(routes, pkg)
			alias := pkg.Name()
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "_" && alias != "." {
				aliases[p] = alias
			}
		}
		added := map[string]string{}
		qualifier := func(pkg *types.Package) string {
			if pkg.Path() == path {
				return ""
			}
			if alias := aliases[pkg.Path()]; alias != "" {
				return alias
			}
			alias := fresh("projection_import")
			aliases[pkg.Path()] = alias
			added[pkg.Path()] = alias
			bindings.Imports[alias] = true
			return alias
		}
		var edits []edit
		var suffix strings.Builder
		var failure error
		ast.Inspect(f, func(n ast.Node) bool {
			if failure != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			marker, ok := unparen(call.Fun).(*ast.Ident)
			if !ok || marker.Name != ProjectRead && marker.Name != ProjectWrite {
				return true
			}
			fail := func(message string) bool {
				failure = &ImportError{fs.PositionFor(call.Pos(), false), message}
				return false
			}
			if len(call.Args) != 2 || call.Ellipsis.IsValid() {
				return fail("invalid enum projection request")
			}
			base := unparen(call.Args[0])
			switch indexed := base.(type) {
			case *ast.IndexExpr:
				base = unparen(indexed.X)
			case *ast.IndexListExpr:
				base = unparen(indexed.X)
			}
			var id *ast.Ident
			switch base := base.(type) {
			case *ast.Ident:
				id = base
			case *ast.SelectorExpr:
				id = base.Sel
			}
			fn, _ := info.Uses[id].(*types.Func)
			sig, _ := info.TypeOf(call.Args[0]).(*types.Signature)
			if fn == nil || fn.Pkg() == nil || fn.Pkg().Scope().Lookup(fn.Name()) != fn || sig == nil || sig.Recv() != nil || sig.Variadic() || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
				return fail("enum projection must resolve to a registered package function")
			}
			owner, ownerOK := types.Unalias(sig.Params().At(0).Type()).(*types.Pointer)
			payload, payloadOK := types.Unalias(sig.Results().At(0).Type()).(*types.Pointer)
			if !ownerOK || !payloadOK {
				return fail("enum projection signature must be (*Enum) *Payload")
			}
			var proof *Projection
			for i := range registry {
				p := &registry[i]
				if p.PackagePath == fn.Pkg().Path() && p.Function == fn.Name() && p.EnumTypeID == EnumTypeID(owner.Elem()) && p.PayloadTypeID == EnumTypeID(payload.Elem()) {
					proof = p
					break
				}
			}
			if proof == nil {
				return fail(fmt.Sprintf("enum projection owner or concrete type identity is not registered: %s.%s (%s -> %s)", fn.Pkg().Path(), fn.Name(), EnumTypeID(owner.Elem()), EnumTypeID(payload.Elem())))
			}
			bindings.ReferenceElements[EnumTypeID(owner.Elem())] = true
			bindings.ReferenceElements[EnumTypeID(payload.Elem())] = true
			if _, ok := types.Unalias(owner.Elem()).(*types.Named); !ok {
				return fail("enum projection requires a nominal enum owner")
			}
			render := func(t types.Type, alias string) string {
				if alias == "" {
					spelling, err := projectionTypeSpelling(t, path, routes, qualifier)
					if err != nil {
						failure = &ImportError{fs.PositionFor(call.Pos(), false), err.Error()}
					}
					return spelling
				}
				q := qualifier(fn.Pkg())
				if q != "" {
					return q + "." + alias
				}
				return alias
			}
			ownerAlias, payloadAlias := fresh("element"), fresh("element")
			for _, element := range []struct{ local, actual string }{
				{ownerAlias, render(owner.Elem(), proof.EnumAlias)},
				{payloadAlias, render(payload.Elem(), proof.PayloadAlias)},
			} {
				if failure != nil {
					return false
				}
				bindings.Stubs[element.local] = true
				bindings.Originals[element.local] = element.actual
				fmt.Fprintf(&suffix, "\ntype %s = %s\n", element.local, element.actual)
				for _, permission := range []Permission{Read, Write} {
					origin := "__goxide_ref_ro"
					if permission == Write {
						origin = "__goxide_ref_rw"
					}
					fmt.Fprintf(&suffix, "type %s = %s[%s]\n", referenceName(Ref{element.local, permission}, name), origin, element.local)
				}
			}
			permission := Read
			if marker.Name == ProjectWrite {
				permission = Write
			}
			stub := fresh("projection")
			bindings.Stubs[stub] = true
			bindings.Originals[stub] = string(source[offset(call.Args[0].Pos()):offset(call.Args[0].End())])
			expect[stub] = expected{owner.Elem(), payload.Elem(), fs.PositionFor(call.Pos(), false)}
			fmt.Fprintf(&suffix, "\nfunc %s(v %s) %s {for{}}\n", stub, referenceName(Ref{ownerAlias, permission}, name), referenceName(Ref{payloadAlias, permission}, name))
			edits = append(edits, edit{offset(call.Pos()), offset(call.Args[1].Pos()), stub + "("})
			return true
		})
		if failure != nil {
			return nil, bindings, failure
		}
		if suffix.Len() > 0 && !strings.Contains(string(source), "type __goxide_ref_write[") {
			suffix.WriteString(declarations())
			suffix.WriteString("\ntype __goxide_ref_write[T interface{}] *T\nfunc __goxide_ref_make_write[T interface{}](v *T)__goxide_ref_write[T]{return __goxide_ref_write[T](v)}\n")
		}
		if len(added) > 0 {
			var block strings.Builder
			paths := map[string][]byte{}
			for p := range added {
				paths[p] = nil
			}
			for _, p := range sourceNames(paths) {
				fmt.Fprintf(&block, "\nimport %s %q\n", added[p], p)
			}
			at := offset(f.Name.End())
			if at < len(source) && source[at] == ';' {
				at++
			}
			edits = append(edits, edit{at, at, block.String()})
		}
		changed := original(source).apply(edits)
		bindings.Copied[name] = changed.copied
		out[name] = append(changed.text, []byte(suffix.String())...)
	}
	// Alias spellings are conveniences only. Recheck their actual identities
	// after parsing the generated signatures, including private-type routes.
	var checked []*ast.File
	checkFS := token.NewFileSet()
	for _, name := range sourceNames(out) {
		f, err := parser.ParseFile(checkFS, name, out[name], parser.AllErrors)
		if err != nil {
			return nil, bindings, err
		}
		checked = append(checked, f)
	}
	deduplicateIR(checked)
	pkg, _ := (&types.Config{Importer: imp, Error: func(error) {}}).Check(path, checkFS, checked, nil)
	for stub, want := range expect {
		if pkg == nil {
			return nil, bindings, fmt.Errorf("unresolved enum projection signature")
		}
		fn, _ := pkg.Scope().Lookup(stub).(*types.Func)
		if fn == nil {
			return nil, bindings, fmt.Errorf("unresolved enum projection signature %s", stub)
		}
		sig := fn.Type().(*types.Signature)
		owner := referenceTarget(sig.Params().At(0).Type())
		payload := referenceTarget(sig.Results().At(0).Type())
		// Local named objects belong to a fresh type-check package here; their
		// canonical IDs must match the independently checked original helper.
		if owner == nil || payload == nil || EnumTypeID(owner) != EnumTypeID(want.enum) || EnumTypeID(payload) != EnumTypeID(want.payload) {
			return nil, bindings, &ImportError{want.position, "enum projection alias identity mismatch for " + stub}
		}
	}
	return out, bindings, nil
}
