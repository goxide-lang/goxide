package refs

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Metadata is versioned analysis data, not a stable export ABI.
const MetadataVersion = 2
const MetadataMarker = "HgoReferenceMetadata"

type Slot struct {
	ElementAlias string `json:",omitempty"`
	Alias        string `json:",omitempty"`
	Type         string
	Permission   Permission `json:",omitempty"`
}
type Signature struct{ Params, Results []Slot }
type Metadata struct {
	Version   int
	Path      string
	Functions map[string]Signature
}

func (m Metadata) Marker() string {
	b, _ := json.Marshal(m)
	return fmt.Sprintf("v%d:%x", m.Version, sha256.Sum256(b))
}
func typeID(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Path() })
}
func signatureSlots(sig *types.Signature) Signature {
	var out Signature
	for _, pair := range []struct {
		tuple *types.Tuple
		slots *[]Slot
	}{{sig.Params(), &out.Params}, {sig.Results(), &out.Results}} {
		for i := 0; i < pair.tuple.Len(); i++ {
			*pair.slots = append(*pair.slots, Slot{Type: typeID(pair.tuple.At(i).Type())})
		}
	}
	return out
}
func scalarRefExpr(e ast.Expr) (Permission, bool) {
	id, ok := unparen(e).(*ast.Ident)
	if !ok {
		return "", false
	}
	for _, p := range []Permission{Read, Write} {
		if strings.HasPrefix(id.Name, prefix+string(p)+"_") {
			return p, true
		}
	}
	return "", false
}

// ExportMetadata runs only after complete package permission validation. It
// checks erased signatures under their actual package identity before publishing.
func ExportMetadata(path string, ir, erased map[string][]byte, imp types.Importer) (Metadata, error) {
	if imp == nil {
		imp = importer.Default()
	}
	m := Metadata{Version: MetadataVersion, Path: path, Functions: map[string]Signature{}}
	fs := token.NewFileSet()
	var files []*ast.File
	nativeDecls := map[string]*ast.FuncDecl{}
	for _, name := range sourceNames(erased) {
		f, e := parser.ParseFile(fs, name, erased[name], 0)
		if e != nil {
			return m, e
		}
		files = append(files, f)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil {
				nativeDecls[fn.Name.Name] = fn
			}
		}
	}
	pkg, e := (&types.Config{Importer: imp}).Check(path, fs, files, nil)
	if e != nil {
		return m, e
	}
	aliasDecls := map[string]string{}
	seenAliases := map[string]bool{}
	for _, name := range sourceNames(ir) {
		f, e := parser.ParseFile(token.NewFileSet(), name, ir[name], 0)
		if e != nil {
			return m, e
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || !ast.IsExported(fn.Name.Name) || fn.Recv != nil {
				continue
			}
			sig := pkg.Scope().Lookup(fn.Name.Name).Type().(*types.Signature)
			data := signatureSlots(sig)
			active := false
			for _, pair := range []struct {
				fields *ast.FieldList
				slots  []Slot
			}{{fn.Type.Params, data.Params}, {fn.Type.Results, data.Results}} {
				at := 0
				if pair.fields == nil {
					continue
				}
				for _, field := range pair.fields.List {
					count := len(field.Names)
					if count == 0 {
						count = 1
					}
					perm, ref := scalarRefExpr(field.Type)
					for i := 0; i < count; i++ {
						if ref {
							pair.slots[at].Permission = perm
							active = true
						}
						at++
					}
				}
			}
			if active {
				// Transparent slot and element aliases keep nominal Go identity
				// available even through re-exports and private/transitive types.
				native := nativeDecls[fn.Name.Name]
				for _, pair := range []struct {
					fields *ast.FieldList
					slots  []Slot
					tuple  *types.Tuple
				}{{native.Type.Params, data.Params, sig.Params()}, {native.Type.Results, data.Results, sig.Results()}} {
					at := 0
					if pair.fields == nil {
						continue
					}
					for _, field := range pair.fields.List {
						count := len(field.Names)
						if count == 0 {
							count = 1
						}
						for i := 0; i < count; i++ {
							slot := &pair.slots[at]
							typ := pair.tuple.At(at).Type()
							at++
							expr := field.Type
							if slot.Permission != "" {
								ptr := types.Unalias(typ).(*types.Pointer)
								typ = ptr.Elem()
								expr = unparen(expr).(*ast.StarExpr).X
							}
							sum := sha256.Sum256([]byte(path + "\x00" + typeID(typ)))
							alias := fmt.Sprintf("HgoReferenceType_%x", sum[:16])
							if slot.Permission != "" {
								slot.ElementAlias = alias
							} else {
								slot.Alias = alias
							}
							if seenAliases[alias] {
								continue
							}
							seenAliases[alias] = true
							if pkg.Scope().Lookup(alias) != nil {
								return m, fmt.Errorf("%s: reserved reference type alias %s", name, alias)
							}
							var text bytes.Buffer
							format.Node(&text, token.NewFileSet(), expr)
							aliasDecls[name] += fmt.Sprintf("\ntype %s = %s\n", alias, text.String())
						}
					}
				}

				if sig.Variadic() || sig.TypeParams().Len() > 0 {
					return m, fmt.Errorf("%s: reference metadata: variadic/generic exports unsupported", name)
				}
				if err := validateSignature(sig, data); err != nil {
					return m, fmt.Errorf("%s: exported reference %s: %w", name, fn.Name.Name, err)
				}
				m.Functions[fn.Name.Name] = data
			}
		}
	}
	if len(m.Functions) > 0 && pkg.Scope().Lookup(MetadataMarker) != nil {
		return m, fmt.Errorf("%s: reserved reference metadata marker %s", path, MetadataMarker)
	}
	for name, decls := range aliasDecls {
		erased[name] = append(erased[name], []byte(decls)...)
	}
	return m, nil
}
func validateSignature(sig *types.Signature, meta Signature) error {
	if sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() > 0 {
		return fmt.Errorf("unsupported reference signature")
	}
	actual := signatureSlots(sig)
	for _, pair := range []struct {
		want, got []Slot
		tuple     *types.Tuple
	}{{meta.Params, actual.Params, sig.Params()}, {meta.Results, actual.Results, sig.Results()}} {
		if len(pair.want) != len(pair.got) {
			return fmt.Errorf("reference metadata arity mismatch")
		}
		for i, w := range pair.want {
			if w.Type != pair.got[i].Type {
				return fmt.Errorf("reference metadata type identity mismatch")
			}
			if w.Permission == "" {
				continue
			}
			if w.Permission != Read && w.Permission != Write {
				return fmt.Errorf("invalid reference permission")
			}
			ptr, ok := types.Unalias(pair.tuple.At(i).Type()).(*types.Pointer)
			if !ok {
				return fmt.Errorf("reference metadata requires pointer representation")
			}
			if !supportedReferenceElement(ptr.Elem(), map[types.Type]bool{}) {
				return fmt.Errorf("reference metadata element is outside the concrete data subset")
			}

		}
	}
	return nil
}

// Matches the concrete data shapes supported by projection checking; no
// interface/channel/function or user generic reference element is inferred.
func supportedReferenceElement(t types.Type, seen map[types.Type]bool) bool {
	t = types.Unalias(t)
	if seen[t] {
		return true
	}
	seen[t] = true
	switch t := t.(type) {
	case *types.Basic:
		return scalar(t.Name()) != ""
	case *types.Named:
		if t.TypeArgs().Len() > 0 {
			return false
		}
		return supportedReferenceElement(t.Underlying(), seen)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !supportedReferenceElement(t.Field(i).Type(), seen) {
				return false
			}
		}
		return true
	case *types.Array:
		return supportedReferenceElement(t.Elem(), seen)
	case *types.Slice:
		return supportedReferenceElement(t.Elem(), seen)
	case *types.Map:
		return supportedReferenceElement(t.Key(), seen) && supportedReferenceElement(t.Elem(), seen)
	case *types.Pointer:
		return supportedReferenceElement(t.Elem(), seen)
	}
	return false
}

type ImportError struct {
	Position token.Position
	Message  string
}

func (e *ImportError) Error() string { return fmt.Sprintf("%s: refs: %s", e.Position, e.Message) }

type ImportBindings struct {
	Imports   map[string]bool
	Originals map[string]string
	Stubs     map[string]bool
}

// BindImports temporarily presents imported contracts as local nominal reference
// signatures. All substitutions are resolved import-object uses, never spelling.
func BindImports(files map[string][]byte, imp types.Importer, metadata map[string]Metadata) (map[string][]byte, ImportBindings, error) {
	if imp == nil {
		imp = importer.Default()
	}
	out := map[string][]byte{}
	bindings := ImportBindings{Originals: map[string]string{}, Stubs: map[string]bool{}, Imports: map[string]bool{}}
	fs := token.NewFileSet()
	parsed := map[string]*ast.File{}
	used := map[string]bool{}
	var all []*ast.File
	for _, name := range sourceNames(files) {
		f, e := parser.ParseFile(fs, name, files[name], parser.AllErrors)
		if e != nil {
			return nil, bindings, e
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
			}
			return true
		})
		parsed[name] = f
		all = append(all, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	// Provisional errors (including local write IR) are not acceptance; Finish checks all bodies.
	deduplicateIR(all)
	_, _ = (&types.Config{Importer: imp, Error: func(error) {}}).Check("hgo.local/refs", fs, all, info)
	serial := 0
	for _, name := range sourceNames(files) {
		f := parsed[name]
		var edits []edit
		var suffix strings.Builder
		active := false
		type elementBinding struct {
			typ  types.Type
			name string
		}
		var elementBindings []elementBinding
		elements := map[string]bool{}
		aliases := map[string]string{}
		addedImports := map[string]string{}

		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			pkg, e := imp.Import(path)
			if e != nil {
				return nil, bindings, e
			}
			alias := pkg.Name()
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if aliases[path] == "" || (alias != "_" && alias != ".") {
				aliases[path] = alias
			}
			markerObject := pkg.Scope().Lookup(MetadataMarker)
			marker, marked := markerObject.(*types.Const)
			if markerObject != nil && !marked {
				return nil, bindings, &ImportError{fs.Position(spec.Pos()), "invalid reference metadata marker for " + path}
			}
			meta, known := metadata[path]
			if marked && (!known || len(meta.Functions) == 0) {
				if provider, ok := imp.(ArtifactImporter); ok {
					var e error
					meta, e = LoadMetadataArtifact(path, provider)
					if e != nil {
						return nil, bindings, &ImportError{fs.Position(spec.Pos()), e.Error()}
					}
					if metadata == nil {
						metadata = map[string]Metadata{}
					}
					metadata[path] = meta
					known = true
				}
			}
			if marked {
				if marker.Val().Kind() != constant.String || !known || meta.Version != MetadataVersion || meta.Path != path || constant.StringVal(marker.Val()) != meta.Marker() {
					return nil, bindings, &ImportError{fs.Position(spec.Pos()), "reference permission metadata missing, stale or wrong package for " + path}
				}
			} else if known && len(meta.Functions) > 0 {
				return nil, bindings, &ImportError{fs.Position(spec.Pos()), "reference metadata marker missing for " + path}
			}
		}
		parents := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		var failure error
		ast.Inspect(f, func(n ast.Node) bool {
			if failure != nil {
				return false
			}
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			fn, ok := info.Uses[id].(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Scope().Lookup(fn.Name()) != fn {
				return true
			}
			path := fn.Pkg().Path()
			meta, ok := metadata[path]
			if !ok {
				return true
			}
			contract, ok := meta.Functions[fn.Name()]
			if !ok {
				return true
			}
			sig := fn.Type().(*types.Signature)
			if e := validateSignature(sig, contract); e != nil {
				failure = fmt.Errorf("%s: %w", fs.Position(id.Pos()), e)
				return false
			}
			// Resolve the whole package selector; method values are not package functions.
			var target ast.Expr = id
			if sel, ok := parents[id].(*ast.SelectorExpr); ok && sel.Sel == id {
				target = sel
			}

			if _, ok := target.(*ast.Ident); ok {
				failure = fmt.Errorf("%s: reference dot imports are outside this metadata slice", fs.Position(id.Pos()))
				return false
			}

			var callTarget ast.Node = target
			for {
				p, ok := parents[callTarget].(*ast.ParenExpr)
				if !ok {
					break
				}
				callTarget = p
			}
			call, ok := parents[callTarget].(*ast.CallExpr)
			if !ok || call.Fun != callTarget {
				failure = &ImportError{fs.Position(target.Pos()), "reference function values/callbacks are outside this metadata slice"}
				return false
			}
			serial++
			stub := fmt.Sprintf("__hgo_permission_call_%d", serial)
			for used[stub] {
				serial++
				stub = fmt.Sprintf("__hgo_permission_call_%d", serial)
			}
			used[stub] = true
			off := func(p token.Pos) int { return fs.Position(p).Offset }
			bindings.Originals[stub] = string(files[name][off(target.Pos()):off(target.End())])
			bindings.Stubs[stub] = true
			edits = append(edits, edit{off(target.Pos()), off(target.End()), stub})
			render := func(t types.Type) string {
				return types.TypeString(t, func(p *types.Package) string {
					if alias := aliases[p.Path()]; alias != "" && alias != "_" {
						if alias == "." {
							return ""
						}
						return alias
					}
					serial++
					alias := fmt.Sprintf("__hgo_permission_import_%d", serial)
					for used[alias] {
						serial++
						alias = fmt.Sprintf("__hgo_permission_import_%d", serial)
					}
					used[alias] = true
					aliases[p.Path()] = alias
					addedImports[p.Path()] = alias
					bindings.Imports[alias] = true
					return alias
				})
			}
			var params, results []string
			for _, pair := range []struct {
				tuple  *types.Tuple
				slots  []Slot
				dst    *[]string
				params bool
			}{{sig.Params(), contract.Params, &params, true}, {sig.Results(), contract.Results, &results, false}} {
				for i, s := range pair.slots {
					spelling := ""
					if s.Permission == "" && s.Alias != "" {
						alias := fn.Pkg().Scope().Lookup(s.Alias)
						if alias == nil || !types.Identical(alias.Type(), pair.tuple.At(i).Type()) {
							failure = &ImportError{fs.Position(id.Pos()), "reference metadata ordinary slot alias identity mismatch"}
							return false
						}
						spelling = aliases[path] + "." + s.Alias
					} else {
						spelling = render(pair.tuple.At(i).Type())
					}
					if s.Permission != "" {
						elem := types.Unalias(pair.tuple.At(i).Type()).(*types.Pointer).Elem()
						if basic, ok := types.Unalias(elem).(*types.Basic); ok {
							spelling = referenceName(Ref{Element: basic.Name(), Permission: s.Permission}, name)
						} else {
							alias := fn.Pkg().Scope().Lookup(s.ElementAlias)
							if alias == nil || !types.Identical(alias.Type(), elem) {
								failure = &ImportError{fs.Position(id.Pos()), "reference metadata element alias identity mismatch"}
								return false
							}
							local := ""
							for _, b := range elementBindings {
								if types.Identical(b.typ, elem) {
									local = b.name
									break
								}
							}
							if local == "" {
								serial++
								local = fmt.Sprintf("__hgo_permission_element_%d", serial)
								for used[local] {
									serial++
									local = fmt.Sprintf("__hgo_permission_element_%d", serial)
								}
								used[local] = true
								elementBindings = append(elementBindings, elementBinding{elem, local})
								target := aliases[path] + "." + s.ElementAlias
								bindings.Stubs[local] = true
								bindings.Originals[local] = target
								fmt.Fprintf(&suffix, "\ntype %s = %s\n", local, target)
								elements[local] = true
							}
							spelling = referenceName(Ref{Element: local, Permission: s.Permission}, name)
						}
					}
					if pair.params {
						spelling = fmt.Sprintf("p%d %s", i, spelling)
					}
					*pair.dst = append(*pair.dst, spelling)
				}
			}
			result := ""
			if len(results) > 0 {
				result = "(" + strings.Join(results, ",") + ")"
			}
			fmt.Fprintf(&suffix, "\nfunc %s(%s)%s{for{}}\n", stub, strings.Join(params, ","), result)
			active = true
			return true
		})
		if failure != nil {
			return nil, bindings, failure
		}
		if active {
			names := map[string][]byte{}
			for e := range elements {
				names[e] = nil
			}
			for _, e := range sourceNames(names) {
				for _, p := range []Permission{Read, Write} {
					origin := "__hgo_ref_ro"
					if p == Write {
						origin = "__hgo_ref_rw"
					}
					fmt.Fprintf(&suffix, "\ntype %s = %s[%s]\n", referenceName(Ref{e, p}, name), origin, e)
				}
			}
			// Keep imports used during analysis even when their sole selector is replaced.
			for _, stub := range f.Imports {
				path, _ := strconv.Unquote(stub.Path.Value)
				if len(metadata[path].Functions) > 0 && aliases[path] != "_" && aliases[path] != "." {
					fmt.Fprintf(&suffix, "\nvar _ = %s.%s\n", aliases[path], MetadataMarker)
				}
			}
			if !strings.Contains(string(files[name]), "type __hgo_ref_write[") {
				suffix.WriteString(declarations())
				suffix.WriteString("\ntype __hgo_ref_write[T interface{}] *T\nfunc __hgo_ref_make_write[T interface{}](v *T)__hgo_ref_write[T]{return __hgo_ref_write[T](v)}\n")
			}
		}
		if len(addedImports) > 0 {
			var block strings.Builder
			paths := map[string][]byte{}
			for path := range addedImports {
				paths[path] = nil
			}
			for _, path := range sourceNames(paths) {
				fmt.Fprintf(&block, "\nimport %s %q\n", addedImports[path], path)
			}
			at := fs.Position(f.Name.End()).Offset
			edits = append(edits, edit{at, at, block.String()})
		}
		// Avoid duplicating IR already present in this file.
		changed := original(files[name]).apply(edits).text
		changed = append(changed, []byte(suffix.String())...)

		out[name] = changed
	}
	return out, bindings, nil
}

func (b ImportBindings) Restore(filename string, source []byte) ([]byte, error) {
	if len(b.Stubs) == 0 {
		return source, nil
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, filename, source, 0)
	if e != nil {
		return nil, e
	}
	var es []edit
	offset := func(p token.Pos) int { return fs.Position(p).Offset }
	for _, d := range f.Decls {
		if decl, ok := d.(*ast.GenDecl); ok && decl.Tok == token.IMPORT && len(decl.Specs) == 1 {
			spec := decl.Specs[0].(*ast.ImportSpec)
			if spec.Name != nil && b.Imports[spec.Name.Name] {
				es = append(es, edit{offset(d.Pos()), offset(d.End()), ""})
				continue
			}
		}
		if decl, ok := d.(*ast.GenDecl); ok && decl.Tok == token.TYPE && len(decl.Specs) == 1 {
			spec := decl.Specs[0].(*ast.TypeSpec)
			if b.Stubs[spec.Name.Name] {
				es = append(es, edit{offset(d.Pos()), offset(d.End()), ""})
				continue
			}
		}
		if fn, ok := d.(*ast.FuncDecl); ok && b.Stubs[fn.Name.Name] {
			es = append(es, edit{offset(d.Pos()), offset(d.End()), ""})
			continue
		}
		ast.Inspect(d, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if original := b.Originals[id.Name]; original != "" {
					es = append(es, edit{offset(id.Pos()), offset(id.End()), original})
				}
			}
			return true
		})
	}
	if len(es) == 0 {
		return source, nil
	}
	return format.Source(original(source).apply(es).text)
}
