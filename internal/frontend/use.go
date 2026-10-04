package frontend

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// useBinding keeps declaration identity separate from a legal Go access path.
// Functions and variables only use the original selector: no wrappers/copies.
type useBinding struct {
	Name, Kind, OriginPath, OriginName, RoutePath, RouteName string
	IdentityPath, IdentityName                               string
	Public                                                   bool
}
type useMetadata struct {
	Version       int
	Path, Package string
	Bindings      []useBinding
	Unsupported   []string
}
type useDecl struct {
	qualifier, name, alias, file string
	public                       bool
	pos                          token.Position
}
type useToken struct {
	text       string
	kind       token.Token
	start, end int
	pos        token.Position
}
type useEdit struct {
	start, end int
	text       string
}
type usePackage struct {
	generatedFile      string
	useImports         map[string]map[string]bool
	bindings           map[string]useBinding
	imports            map[string]map[string]string
	imported           map[string]useMetadata
	original, prepared map[string][]byte
	metadata           useMetadata
	imp                types.Importer
	known              map[string]PackageResult
}

func scanUses(name string, source []byte) ([]byte, []useDecl, error) {
	fs := token.NewFileSet()
	f := fs.AddFile(name, -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, source, func(token.Position, string) {}, 0)
	var ts []useToken
	for {
		p, k, v := scan.Scan()
		if k == token.EOF {
			break
		}
		if v == "" {
			v = k.String()
		}
		a := f.Offset(p)
		end := a + len(v)
		if k == token.SEMICOLON && v == "\n" {
			end = a
		}
		ts = append(ts, useToken{v, k, a, end, fs.Position(p)})
	}
	out := append([]byte(nil), source...)
	var decls []useDecl
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.kind == token.LBRACE {
			depth++
		}
		if t.kind == token.RBRACE {
			depth--
		}
		if depth != 0 || t.kind != token.IDENT || i == 0 || (ts[i-1].kind != token.SEMICOLON && ts[i-1].kind != token.RBRACE) {
			continue
		}
		j := i
		public := false
		if t.text == "pub" && j+1 < len(ts) && ts[j+1].text == "use" {
			public = true
			j++
		} else if t.text != "use" {
			continue
		}
		start := i
		j++
		d := useDecl{public: public, file: name, pos: t.pos}
		bad := func() ([]byte, []useDecl, error) {
			return nil, nil, fmt.Errorf("%s: expected [pub] use importAlias::Name [as Alias]; or importAlias::*;", t.pos)
		}
		if j >= len(ts) || ts[j].kind != token.IDENT {
			return bad()
		}
		d.qualifier = ts[j].text
		j++
		if j+1 >= len(ts) || ts[j].kind != token.COLON || ts[j+1].kind != token.COLON {
			return bad()
		}
		j += 2
		if j >= len(ts) || (ts[j].kind != token.IDENT && ts[j].kind != token.MUL) {
			return bad()
		}
		d.name = ts[j].text
		d.alias = d.name
		j++
		if j < len(ts) && ts[j].text == "as" {
			j++
			if d.name == "*" || j >= len(ts) || ts[j].kind != token.IDENT || ts[j].text == "_" {
				return bad()
			}
			d.alias = ts[j].text
			j++
		}
		if j >= len(ts) || ts[j].kind != token.SEMICOLON {
			return bad()
		}
		for k := ts[start].start; k < ts[j].end && k < len(out); k++ {
			if out[k] != '\n' && out[k] != '\r' {
				out[k] = ' '
			}
		}
		decls = append(decls, d)
		i = j
	}
	return out, decls, nil
}

func prepareUses(path string, sources map[string][]byte, known map[string]PackageResult, imp types.Importer) (*usePackage, error) {
	u := &usePackage{useImports: map[string]map[string]bool{}, bindings: map[string]useBinding{}, imports: map[string]map[string]string{}, imported: map[string]useMetadata{}, original: sources, prepared: map[string][]byte{}, imp: imp, known: known, metadata: useMetadata{Version: 1, Path: path}}
	var declarations []useDecl
	for _, file := range sortedSources(sources) {
		src := sources[file]
		if strings.HasSuffix(file, ".hgo") {
			clean, ds, e := scanUses(file, src)
			if e != nil {
				return nil, e
			}
			src = clean
			declarations = append(declarations, ds...)
		}
		u.prepared[file] = src
		f, e := parseSourceImports(file, src)
		if e != nil {
			return nil, e
		}
		u.metadata.Package = f.Name.Name
		u.imports[file] = map[string]string{}
		for _, spec := range f.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			if p == "C" {
				continue
			}
			alias := ""
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias == "" {
				if imp == nil {
					continue
				}
				pkg, e := imp.Import(p)
				if e != nil {
					return nil, e
				}
				alias = pkg.Name()
			}
			u.imports[file][alias] = p
			m, cached := u.imported[p]
			if !cached {
				m, e = u.load(p)
				if e != nil {
					return nil, e
				}
			}
			if alias == "." && m.present() && strings.HasSuffix(file, ".hgo") {
				return nil, fmt.Errorf("%s: dot imports of packages with use metadata are unsupported; use a named import", file)
			}
			u.imported[p] = m
		}
	}
	for _, d := range declarations {
		if u.useImports[d.file] == nil {
			u.useImports[d.file] = map[string]bool{}
		}
		u.useImports[d.file][d.qualifier] = true
		p, ok := u.imports[d.file][d.qualifier]
		if !ok || d.qualifier == "_" || d.qualifier == "." {
			return nil, fmt.Errorf("%s: use qualifier must be an explicit named Go import: %s", d.pos, d.qualifier)
		}
		if imp == nil {
			return nil, fmt.Errorf("%s: use requires a package importer", d.pos)
		}
		pkg, e := imp.Import(p)
		if e != nil {
			return nil, e
		}
		m := u.imported[p]
		surface := map[string]useBinding{}
		for _, name := range pkg.Scope().Names() {
			if !ast.IsExported(name) || isUseMarker(name) {
				continue
			}
			b, e := nativeUseBinding(pkg.Scope().Lookup(name), p, name)
			if e != nil {
				continue
			}
			surface[name] = b
		}
		for _, b := range m.Bindings {
			if b.Public {
				surface[b.Name] = b
			} else {
				delete(surface, b.Name)
			}
		}
		if d.name == "*" && len(m.Unsupported) > 0 {
			return nil, fmt.Errorf("%s: glob includes unsupported macro/family exports %s; use an explicit supported item list", d.pos, strings.Join(m.Unsupported, ", "))
		}
		names := []string{d.name}
		if d.name == "*" {
			names = nil
			for name := range surface {
				names = append(names, name)
			}
			sort.Strings(names)
		}
		for _, name := range names {
			b, ok := surface[name]
			if !ok {
				return nil, fmt.Errorf("%s: unknown, private, or unsupported use item %s::%s", d.pos, d.qualifier, name)
			}
			if d.name != "*" {
				b.Name = d.alias
			}
			b.Public = d.public
			if isUseMarker(b.Name) || strings.HasPrefix(b.Name, "__hgo_") || b.Name == "init" || b.Name == "main" {
				return nil, fmt.Errorf("%s: reserved use binding %s", d.pos, b.Name)
			}
			if (b.Kind == "function" || b.Kind == "variable") && !enumOriginImportAllowed(path, b.RoutePath) {
				return nil, fmt.Errorf("%s: unsupported use route for %s: Go internal package %s is inaccessible from %s; no wrapper or variable copy is generated", d.pos, b.Name, b.RoutePath, path)
			}
			if old, ok := u.bindings[b.Name]; ok {
				if old.Kind != b.Kind || old.OriginPath != b.OriginPath || old.OriginName != b.OriginName {
					return nil, fmt.Errorf("%s: ambiguous use %s: %s.%s and %s.%s; choose explicit aliases", d.pos, b.Name, old.OriginPath, old.OriginName, b.OriginPath, b.OriginName)
				}
				if b.Kind == "type" {
					oldPkg, e := imp.Import(old.RoutePath)
					if e != nil {
						return nil, e
					}
					newPkg, e := imp.Import(b.RoutePath)
					if e != nil {
						return nil, e
					}
					oldType, oldOK := oldPkg.Scope().Lookup(old.RouteName).(*types.TypeName)
					newType, newOK := newPkg.Scope().Lookup(b.RouteName).(*types.TypeName)
					if !oldOK || !newOK || !sameUseType(oldType.Type(), newType.Type()) {
						return nil, fmt.Errorf("%s: ambiguous use %s: distinct Go type identities", d.pos, b.Name)
					}
				}
				b.Public = b.Public || old.Public
			}
			u.bindings[b.Name] = b
		}
	}
	for _, file := range sortedSources(sources) {
		for alias := range u.imports[file] {
			if _, ok := u.bindings[alias]; ok {
				return nil, fmt.Errorf("%s: use binding conflicts with import alias %s", file, alias)
			}
		}
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		f, e := parser.ParseFile(token.NewFileSet(), file, sources[file], 0)
		if e != nil {
			return nil, e
		}
		for name := range f.Scope.Objects {
			if _, ok := u.bindings[name]; ok {
				return nil, fmt.Errorf("%s: use binding conflicts with native declaration %s", file, name)
			}
		}
	}
	// Alias declarations are native Go aliases, not defined types. Their import
	// routes remain the facade path where required by Go's internal restrictions.
	if len(u.bindings) > 0 {
		file := ""
		for _, n := range sortedSources(sources) {
			if strings.HasSuffix(n, ".hgo") {
				file = n
				break
			}
		}
		if file == "" {
			return nil, fmt.Errorf("use requires .hgo source")
		}
		imports := map[string]string{}
		var extra strings.Builder
		for _, name := range sortedUseNames(u.bindings) {
			b := u.bindings[name]
			if b.Kind == "type" || b.Kind == "constant" {
				alias := fmt.Sprintf("hgoUseImport%d", len(imports))
				if a := imports[b.RoutePath]; a != "" {
					alias = a
				} else {
					imports[b.RoutePath] = alias
				}
				if strings.Contains(string(sources[file]), alias) {
					return nil, fmt.Errorf("%s: generated use import conflicts with source %s", file, alias)
				}
				if b.Kind == "constant" {
					fmt.Fprintf(&extra, "\nconst %s = %s.%s\n", name, alias, b.RouteName)
				} else {
					pkg, e := imp.Import(b.RoutePath)
					if e != nil {
						return nil, e
					}
					obj, ok := pkg.Scope().Lookup(b.RouteName).(*types.TypeName)
					if !ok {
						return nil, fmt.Errorf("invalid type use route %s.%s", b.RoutePath, b.RouteName)
					}
					parameters, args, e := useTypeParameters(obj.Type(), b.RoutePath, alias)
					if e != nil {
						return nil, fmt.Errorf("use %s: %w", name, e)
					}
					fmt.Fprintf(&extra, "\ntype %s%s = %s.%s%s\n", name, parameters, alias, b.RouteName, args)
				}
			}
		}
		if extra.Len() > 0 {
			u.generatedFile = filepath.Join(filepath.Dir(file), "hgo_use_aliases.go")
			if _, exists := sources[u.generatedFile]; exists {
				return nil, fmt.Errorf("reserved generated use alias filename conflicts: %s", u.generatedFile)
			}
			source, e := addUseImports(u.generatedFile, []byte("package "+u.metadata.Package+"\n"), imports)
			if e != nil {
				return nil, e
			}
			u.prepared[u.generatedFile] = append(source, extra.String()...)
		}
	}
	for _, name := range sortedUseNames(u.bindings) {
		b := u.bindings[name]
		if b.Kind == "type" || b.Kind == "constant" {
			b.RoutePath = path
			b.RouteName = b.Name
			if b.Public && !ast.IsExported(b.Name) {
				return nil, fmt.Errorf("public type/constant use alias %s must use a Go-exported name in this version", b.Name)
			}
		}
		u.metadata.Bindings = append(u.metadata.Bindings, b)
	}
	return u, nil
}
func nativeUseBinding(obj types.Object, path, name string) (useBinding, error) {
	b := useBinding{Name: name, OriginPath: path, OriginName: name, RoutePath: path, RouteName: name, Public: true}
	switch o := obj.(type) {
	case *types.TypeName:
		b.Kind = "type"
		b.IdentityPath, b.IdentityName = path, name
		if n, ok := types.Unalias(o.Type()).(*types.Named); ok && n.Obj().Pkg() != nil {
			b.OriginPath = n.Obj().Pkg().Path()
			b.OriginName = n.Obj().Name()
		}
	case *types.Const:
		b.Kind = "constant"
	case *types.Func:
		b.Kind = "function"
	case *types.Var:
		b.Kind = "variable"
	default:
		return b, fmt.Errorf("unsupported export kind")
	}
	return b, nil
}
func sortedUseNames(m map[string]useBinding) []string {
	names := []string{}
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
func isUseMarker(n string) bool {
	return n == useMetadataMarker || n == enumMetadataMarker || n == "HgoReferenceMetadata"
}
func useTypeParameters(t types.Type, path, alias string) (string, string, error) {
	var params *types.TypeParamList
	switch t := t.(type) {
	case *types.Named:
		params = t.TypeParams()
	case *types.Alias:
		params = t.TypeParams()
	}
	if params == nil || params.Len() == 0 {
		return "", "", nil
	}
	decl, names := []string{}, []string{}
	var bad string
	for i := 0; i < params.Len(); i++ {
		p := params.At(i)
		if useConstraintHasParameter(p.Constraint(), map[types.Type]bool{}) {
			return "", "", fmt.Errorf("generic alias constraints referring to type parameters are unsupported; use an explicit native Go alias")
		}
		constraint := types.TypeString(p.Constraint(), func(pkg *types.Package) string {
			if pkg.Path() == path {
				return alias
			}
			bad = pkg.Path()
			return pkg.Name()
		})
		// Cross-package private method constraints cannot be reconstructed by spelling.
		if it, ok := p.Constraint().Underlying().(*types.Interface); ok {
			for j := 0; j < it.NumMethods(); j++ {
				if !it.Method(j).Exported() {
					return "", "", fmt.Errorf("generic alias constraint has private methods; explicit native Go alias required")
				}
			}
		}
		if bad != "" {
			return "", "", fmt.Errorf("generic alias constraint references third package %s; explicit native Go alias required", bad)
		}
		decl = append(decl, p.Obj().Name()+" "+constraint)
		names = append(names, p.Obj().Name())
	}
	return "[" + strings.Join(decl, ",") + "]", "[" + strings.Join(names, ",") + "]", nil
}
func addUseImports(file string, source []byte, imports map[string]string) ([]byte, error) {
	if len(imports) == 0 {
		return source, nil
	}
	f, e := parser.ParseFile(token.NewFileSet(), file, source, parser.PackageClauseOnly)
	if e != nil {
		return nil, e
	}
	// Insertion on the package line preserves subsequent attribute line numbers.
	at := int(f.Name.End()) - 1
	var text strings.Builder
	paths := []string{}
	for p := range imports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		fmt.Fprintf(&text, "; import %s %q", imports[p], p)
	}
	out := append([]byte(nil), source[:at]...)
	out = append(out, text.String()...)
	out = append(out, source[at:]...)
	return out, nil
}

// rewrite resolves unbound Go identifiers after DSL syntax lowering, before
// semantic passes. Parser bindings protect locals, parameters and type params.
func (u *usePackage) rewrite(file string, source []byte) ([]byte, error) {
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, file, source, parser.ParseComments)
	if e != nil {
		return nil, e
	}
	aliases := map[string]string{}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		name := ""
		if im.Name != nil {
			name = im.Name.Name
		} else if u.imp != nil {
			pkg, e := u.imp.Import(p)
			if e != nil {
				return nil, e
			}
			name = pkg.Name()
		}
		aliases[name] = p
	}
	edits := []useEdit{}
	extra := map[string]string{}
	ignored := map[*ast.Ident]bool{}
	usedImports := map[string]bool{}
	handledImports := map[string]bool{}
	for alias := range u.useImports[file] {
		handledImports[alias] = true
	}
	route := func(b useBinding) (string, error) {
		if !enumOriginImportAllowed(u.metadata.Path, b.RoutePath) {
			return "", fmt.Errorf("%s: unsupported re-export route for %s: Go internal package %s is inaccessible", file, b.Name, b.RoutePath)
		}
		if b.RoutePath == u.metadata.Path {
			return b.RouteName, nil
		}
		a := extra[b.RoutePath]
		if a == "" {
			for i := len(extra); ; i++ {
				a = fmt.Sprintf("hgoUseRoute%d", i)
				if !bytes.Contains(source, []byte(a)) {
					used := false
					for _, prior := range extra {
						used = used || prior == a
					}
					if !used {
						break
					}
				}
			}
			extra[b.RoutePath] = a
		}
		return a + "." + b.RouteName, nil
	}
	var problem error
	ast.Inspect(f, func(n ast.Node) bool {
		if problem != nil {
			return false
		}
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ignored[n.Sel] = true
			x, ok := n.X.(*ast.Ident)
			if !ok || x.Obj != nil {
				return true
			}
			p, ok := aliases[x.Name]
			if !ok {
				return true
			}
			ignored[x] = true
			m := u.imported[p]
			found := false
			for _, b := range m.Bindings {
				if b.Name != n.Sel.Name {
					continue
				}
				found = true
				if !b.Public {
					problem = fmt.Errorf("%s: private use binding %s.%s", fs.Position(n.Pos()), x.Name, b.Name)
					return false
				}
				expr, e := route(b)
				if e != nil {
					problem = e
					return false
				}
				edits = append(edits, useEdit{int(n.Pos()) - 1, int(n.End()) - 1, expr})
				handledImports[x.Name] = true
			}
			if !found {
				usedImports[x.Name] = true
			}
		case *ast.Field:
			for _, id := range n.Names {
				ignored[id] = true
			}
		case *ast.KeyValueExpr:
			// A bare identifier key is a field spelling unless the literal's type is
			// demonstrably a map. Ambiguous alias-key cases are rejected below.
		case *ast.LabeledStmt:
			ignored[n.Label] = true
		case *ast.BranchStmt:
			if n.Label != nil {
				ignored[n.Label] = true
			}
		case *ast.ImportSpec:
			if n.Name != nil {
				ignored[n.Name] = true
			}
		}
		return true
	})
	if problem != nil {
		return nil, problem
	}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		_, mapLiteral := lit.Type.(*ast.MapType)
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			id, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			if mapLiteral {
				continue
			}
			if _, alias := u.bindings[id.Name]; alias {
				if _, isStruct := lit.Type.(*ast.StructType); !isStruct {
					problem = fmt.Errorf("%s: use name %s as a named composite-literal key is ambiguous in this version; use an explicit qualified expression for map keys", fs.Position(id.Pos()), id.Name)
				}
			}
			ignored[id] = true
		}
		return true
	})
	if problem != nil {
		return nil, problem
	}
	// Reject package declarations colliding with use bindings. Type/const aliases
	// appended by prepareUses are the sole intentional declarations of those names.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				if _, ok := u.bindings[d.Name.Name]; ok {
					return nil, fmt.Errorf("%s: use binding conflicts with function %s", fs.Position(d.Pos()), d.Name.Name)
				}
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				if typ, ok := sp.(*ast.TypeSpec); ok {
					if _, exists := u.bindings[typ.Name.Name]; exists {
						return nil, fmt.Errorf("%s: use binding conflicts with type declaration %s", fs.Position(typ.Pos()), typ.Name.Name)
					}
				}
				if v, ok := sp.(*ast.ValueSpec); ok {
					for _, id := range v.Names {
						b, exists := u.bindings[id.Name]
						if exists && b.Kind != "constant" {
							return nil, fmt.Errorf("%s: use binding conflicts with declaration %s", fs.Position(id.Pos()), id.Name)
						}
					}
				}
			}
		}
	}
	for _, id := range f.Unresolved {
		if ignored[id] {
			continue
		}
		b, ok := u.bindings[id.Name]
		if !ok || b.Kind == "type" || b.Kind == "constant" {
			continue
		}
		expr, e := route(b)
		if e != nil {
			return nil, e
		}
		edits = append(edits, useEdit{int(id.Pos()) - 1, int(id.End()) - 1, expr})
	}
	// A facade-only use is still an initialization dependency; retain blank import.
	for _, im := range f.Imports {
		a := ""
		if im.Name != nil {
			a = im.Name.Name
		} else {
			p, _ := strconv.Unquote(im.Path.Value)
			for alias, path := range aliases {
				if path == p {
					a = alias
					break
				}
			}
		}
		if handledImports[a] && !usedImports[a] {
			replacement := "_ " + im.Path.Value
			edits = append(edits, useEdit{int(im.Pos()) - 1, int(im.End()) - 1, replacement})
		}
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	out := append([]byte(nil), source...)
	last := len(source) + 1
	for _, ed := range edits {
		if ed.end > last {
			return nil, fmt.Errorf("%s: overlapping use rewrite", file)
		}
		out = append(append(append([]byte(nil), out[:ed.start]...), ed.text...), out[ed.end:]...)
		last = ed.start
	}
	return addUseImports(file, out, extra)
}

func (u *usePackage) remap(result *PackageResult) {
	for name, r := range result.Files {
		original, ok := u.original[name]
		if !ok || r.Diagnostics == nil || bytes.Equal(original, u.prepared[name]) {
			continue
		}
		base := sourcemap.Identity(name, original).Compose(original, u.prepared[name], "use declarations")
		for i := range r.Diagnostics.Segments {
			seg := &r.Diagnostics.Segments[i]
			if seg.Origin == nil || seg.Origin.File != name {
				continue
			}
			old := seg.Origin
			first := base.Lookup(old.Start)
			if first != nil {
				seg.Origin = first.Origin
			} else {
				seg.Origin = nil
			}
		}
		refreshPackageMap(&r)
		result.Files[name] = r
	}
}

// Ask Go for complete identity, including type arguments, private fields and
// private interface methods. Metadata retains a real alias route to recover it;
// a text spelling is never used as the type-identity proof.
func sameUseType(a, b types.Type) bool {
	params := func(t types.Type) *types.TypeParamList {
		switch x := t.(type) {
		case *types.Alias:
			return x.TypeParams()
		case *types.Named:
			return x.TypeParams()
		}
		return nil
	}
	ap, bp := params(a), params(b)
	if ap.Len() != bp.Len() {
		return false
	}
	if ap.Len() == 0 {
		return types.Identical(a, b)
	}
	var args []types.Type
	for i := 0; i < ap.Len(); i++ {
		if !types.Identical(ap.At(i).Constraint(), bp.At(i).Constraint()) {
			return false
		}
		args = append(args, ap.At(i))
	}
	ai, e := types.Instantiate(nil, a, args, false)
	if e != nil {
		return false
	}
	bi, e := types.Instantiate(nil, b, args, false)
	return e == nil && types.Identical(ai, bi)
}

func useConstraintHasParameter(t types.Type, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch x := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := 0; i < x.TypeArgs().Len(); i++ {
			if useConstraintHasParameter(x.TypeArgs().At(i), seen) {
				return true
			}
		}
	case *types.Pointer:
		return useConstraintHasParameter(x.Elem(), seen)
	case *types.Array:
		return useConstraintHasParameter(x.Elem(), seen)
	case *types.Slice:
		return useConstraintHasParameter(x.Elem(), seen)
	case *types.Chan:
		return useConstraintHasParameter(x.Elem(), seen)
	case *types.Map:
		return useConstraintHasParameter(x.Key(), seen) || useConstraintHasParameter(x.Elem(), seen)
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if useConstraintHasParameter(x.Field(i).Type(), seen) {
				return true
			}
		}
	case *types.Tuple:
		for i := 0; i < x.Len(); i++ {
			if useConstraintHasParameter(x.At(i).Type(), seen) {
				return true
			}
		}
	case *types.Signature:
		return useConstraintHasParameter(x.Params(), seen) || useConstraintHasParameter(x.Results(), seen)
	case *types.Interface:
		for i := 0; i < x.NumEmbeddeds(); i++ {
			if useConstraintHasParameter(x.EmbeddedType(i), seen) {
				return true
			}
		}
		for i := 0; i < x.NumMethods(); i++ {
			if useConstraintHasParameter(x.Method(i).Type(), seen) {
				return true
			}
		}
	case *types.Union:
		for i := 0; i < x.Len(); i++ {
			if useConstraintHasParameter(x.Term(i).Type(), seen) {
				return true
			}
		}
	}
	return false
}
