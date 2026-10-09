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

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// useBinding keeps declaration identity separate from a legal Go access path.
// Functions and variables only use the original selector: no wrappers/copies.
type useBinding struct {
	Name, Kind, OriginPath, OriginName, RoutePath, RouteName string
	IdentityPath, IdentityName                               string
	Public                                                   bool
	Namespace                                                *paths.Namespace `json:",omitempty"`
}
type useMetadata struct {
	Version          int
	Path, Package    string
	Bindings         []useBinding
	Unsupported      []string
	UnsupportedKinds map[string]string `json:",omitempty"`
}
type useDecl struct {
	qualifier, name, alias, file string
	public                       bool
	pos                          token.Position
	segments                     []string
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
	localDeclarations  map[string]paths.Binding
	fileBindings       map[string]map[string]useBinding
	namespaces         map[string]map[string]paths.Namespace
	roots              paths.Roots
	single             bool
	generatedFile      string
	generatedMap       *sourcemap.Map
	positions          map[string]token.Position
	useImports         map[string]map[string]bool
	bindings           map[string]useBinding
	imports            map[string]map[string]string
	imported           map[string]useMetadata
	original, prepared map[string][]byte
	preparedMaps       map[string]*sourcemap.Map
	plans              map[string]PathPreparation
	metadata           useMetadata
	imp                types.Importer
	known              map[string]PackageResult
	protocols          map[string]protocols.Metadata
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
			return nil, nil, fmt.Errorf("%s: expected [pub] use root[::Name...] [as Alias];", t.pos)
		}
		if j >= len(ts) || ts[j].kind != token.IDENT {
			return bad()
		}
		d.segments = append(d.segments, ts[j].text)
		j++
		for j+1 < len(ts) && ts[j].kind == token.COLON && ts[j+1].kind == token.COLON {
			j += 2
			if j >= len(ts) || (ts[j].kind != token.IDENT && ts[j].kind != token.MUL) {
				return bad()
			}
			d.segments = append(d.segments, ts[j].text)
			j++
		}
		d.qualifier = d.segments[0]
		d.name = d.segments[len(d.segments)-1]
		d.alias = d.name
		if j < len(ts) && ts[j].text == "as" {
			j++
			if d.name == "*" || j >= len(ts) || ts[j].kind != token.IDENT {
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

func prepareUses(path string, sources map[string][]byte, known map[string]PackageResult, imp types.Importer, roots paths.Roots, single bool) (*usePackage, error) {
	u := &usePackage{positions: map[string]token.Position{}, useImports: map[string]map[string]bool{}, bindings: map[string]useBinding{}, fileBindings: map[string]map[string]useBinding{}, namespaces: map[string]map[string]paths.Namespace{}, imports: map[string]map[string]string{}, imported: map[string]useMetadata{}, original: sources, prepared: map[string][]byte{}, imp: imp, known: known, roots: roots, single: single, metadata: useMetadata{Version: useMetadataVersion, Path: path}}
	u.preparedMaps = map[string]*sourcemap.Map{}
	if single {
		for file, source := range sources {
			_, mods, err := scanModules(file, source)
			if err != nil {
				return nil, err
			}
			if len(mods) > 0 {
				return nil, fmt.Errorf("%s: local mod requires directory package compilation to select its child package", mods[0].pos)
			}
		}
	}
	merged, err := LocalDeclarations(path, sources, roots, nil)
	if err != nil {
		return nil, err
	}
	u.roots = merged
	u.localDeclarations = localDeclarationBindings(path, sources)
	if !single {
		u.plans, err = PreparePackageSourcePaths(path, sources, merged, nil)
		if err != nil {
			return nil, err
		}
	} else {
		u.plans = map[string]PathPreparation{}
	}
	for _, file := range sortedSources(sources) {
		source := sources[file]
		if !isGoxideInput(file, single) {
			u.prepared[file] = source
			continue
		}
		plan := u.plans[file]
		if single {
			plan, err = PrepareSourcePaths(path, file, source, merged, nil)
			if err != nil {
				return nil, err
			}
			u.plans[file] = plan
		}
		u.prepared[file] = plan.Source
		// Stripping use/mod only blanks their exact byte ranges. Every surviving
		// token therefore retains its original offset, even repeated spellings.
		u.preparedMaps[file] = sourceMapAfterBlanks(file, source, plan.Source)
		f, err := parseSourceImports(file, plan.Source)
		if err != nil {
			return nil, err
		}
		u.metadata.Package = f.Name.Name
		u.fileBindings[file] = map[string]useBinding{}
		u.namespaces[file] = map[string]paths.Namespace{}
		u.imports[file] = map[string]string{}
		u.useImports[file] = map[string]bool{}
		for n, ns := range plan.Locals {
			u.namespaces[file][n] = ns
		}
		for _, d := range plan.declarations {
			if single && d.public {
				return nil, fmt.Errorf("%s: pub use/mod requires directory package compilation", d.pos)
			}
			node, remaining, err := u.resolveNamespace(d.segments)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", d.pos, err)
			}
			if len(remaining) == 0 {
				if d.alias == "_" {
					if d.public || node.PackagePath == "" {
						return nil, fmt.Errorf("%s: side-effect use requires a concrete package and cannot be public", d.pos)
					}
					if _, err := u.checkedPackage(node); err != nil {
						return nil, fmt.Errorf("%s: %w", d.pos, err)
					}
					// The pre-scan may only know a facade package. Preserve the
					// resolved namespace's own initialization dependency too.
					selected := u.plans[file]
					found := false
					for _, p := range selected.Imports {
						found = found || p == node.PackagePath
					}
					if !found {
						selected.Imports = append(selected.Imports, node.PackagePath)
						sort.Strings(selected.Imports)
						u.plans[file] = selected
					}
					continue
				}
				b := useBinding{Name: d.alias, Kind: "namespace", OriginPath: node.PackagePath, OriginName: d.alias, RoutePath: node.PackagePath, RouteName: d.alias, Namespace: &node, Public: d.public}
				if err := u.bindUse(file, d, b); err != nil {
					return nil, err
				}
				u.namespaces[file][d.alias] = node
				continue
			}
			if len(remaining) != 1 {
				return nil, fmt.Errorf("%s: unsupported use path after item %s", d.pos, strings.Join(remaining, "::"))
			}
			surface, err := u.surface(node)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", d.pos, err)
			}
			names := []string{remaining[0]}
			if remaining[0] == "*" {
				names = sortedUseNames(surface)
			}
			if d.alias == "_" {
				return nil, fmt.Errorf("%s: side-effect use requires a concrete package, not an item", d.pos)
			}
			for _, name := range names {
				b, ok := surface[name]
				if !ok {
					return nil, fmt.Errorf("%s: unknown, private, or unsupported use item %s", d.pos, strings.Join(d.segments, "::"))
				}
				if b.Kind == "unsupported" {
					return nil, fmt.Errorf("%s: unsupported use item %s", d.pos, name)
				}
				if b.Kind == "macro-unavailable" {
					return nil, fmt.Errorf("%s: macro use %s requires a compiled source registry", d.pos, name)
				}
				if b.Kind == "ambiguous" {
					return nil, fmt.Errorf("%s: ambiguous use item %s: macro and Go declaration", d.pos, name)
				}
				if remaining[0] != "*" {
					b.Name = d.alias
				}
				b.Public = d.public
				if err := u.bindUse(file, d, b); err != nil {
					return nil, err
				}
				if b.Kind == "namespace" && b.Namespace != nil {
					u.namespaces[file][b.Name] = *b.Namespace
				}
			}
		}
		// Namespace declarations are visible as declarations throughout this package.
		_, mods, err := scanModules(file, source)
		if err != nil {
			return nil, err
		}
		for _, mod := range mods {
			node := merged[mod.alias]
			u.namespaces[file][mod.alias] = node
			if mod.public {
				if single {
					return nil, fmt.Errorf("%s: pub mod requires directory package compilation", mod.pos)
				}
				b := useBinding{Name: mod.alias, Kind: "namespace", OriginPath: node.PackagePath, OriginName: mod.alias, RoutePath: node.PackagePath, RouteName: mod.alias, Namespace: &node, Public: true}
				if err := u.bindUse(file, mod, b); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, file := range sortedSources(sources) {
		if !isGoxideInput(file, single) {
			continue
		}
		for name, public := range u.bindings {
			if private, ok := u.fileBindings[file][name]; ok && !sameUseBinding(public, private) {
				return nil, fmt.Errorf("%s: use binding %s conflicts with public declaration", file, name)
			}
			if public.Kind == "namespace" && public.Namespace != nil {
				u.namespaces[file][name] = *public.Namespace
			}
		}
		// Expand only explicit namespace tree prefixes, retaining the final ::
		// for macro/derive/type-aware lowering.
		out, err := u.prepareNamespaceImports(file, u.prepared[file])
		if err != nil {
			return nil, err
		}
		u.prepared[file] = out
	}
	// Alias declarations are native Go aliases, not defined types. Their import
	// routes remain the facade path where required by Go's internal restrictions.
	if len(u.bindings) > 0 {
		file := ""
		for _, n := range sortedSources(sources) {
			if isGoxideInput(n, single) {
				file = n
				break
			}
		}
		if file == "" {
			return nil, fmt.Errorf("use requires .gox source")
		}
		imports := map[string]string{}
		reserved := map[string]bool{}
		// Imports are file scoped, while every alias parameter list has its own
		// scope. Reserve all parameter names before allocating any import.
		for _, source := range sources {
			fs := token.NewFileSet()
			f := fs.AddFile("", -1, len(source))
			var sc scanner.Scanner
			sc.Init(f, source, func(token.Position, string) {}, 0)
			for {
				_, kind, text := sc.Scan()
				if kind == token.EOF {
					break
				}
				if kind == token.IDENT {
					reserved[text] = true
				}
			}
		}
		aliasTypes := map[string]types.Type{}
		for _, name := range sortedUseNames(u.bindings) {
			reserved[name] = true
			b := u.bindings[name]
			if b.Kind != "type" {
				continue
			}
			pkg, err := imp.Import(b.RoutePath)
			if err != nil {
				return nil, u.at(name, err)
			}
			obj, ok := pkg.Scope().Lookup(b.RouteName).(*types.TypeName)
			if !ok {
				return nil, u.at(name, fmt.Errorf("invalid type use route %s.%s", b.RoutePath, b.RouteName))
			}
			aliasTypes[name] = obj.Type()
			if params := useTypeParameterList(obj.Type()); params != nil {
				for i := 0; i < params.Len(); i++ {
					reserved[params.At(i).Obj().Name()] = true
				}
			}
		}
		nextImport := 0
		var extra strings.Builder
		protocolAliases := map[string]bool{}
		for _, name := range sortedUseNames(u.bindings) {
			b := u.bindings[name]
			if b.Kind == "type" || b.Kind == "constant" || b.Kind == "protocol" {
				alias := imports[b.RoutePath]
				if alias == "" {
					for {
						alias = fmt.Sprintf("goxideUseImport%d", nextImport)
						nextImport++
						if !reserved[alias] {
							break
						}
					}
					reserved[alias] = true
					imports[b.RoutePath] = alias
				}
				if b.Kind == "protocol" {
					def, e := u.protocolDefinition(b.OriginPath, b.OriginName)
					if e != nil {
						return nil, u.at(name, e)
					}
					if !protocolAliases[def.OpsName] {
						fmt.Fprintf(&extra, "\ntype %s[Self any] = %s.%s[Self]\n", def.OpsName, alias, b.RouteName)
						protocolAliases[def.OpsName] = true
					}
				} else if b.Kind == "constant" {
					fmt.Fprintf(&extra, "\nconst %s = %s.%s\n", name, alias, b.RouteName)
				} else {
					parameters, args, e := useTypeParameters(aliasTypes[name], b.RoutePath, alias)
					if e != nil {
						return nil, u.at(name, fmt.Errorf("use %s: %w", name, e))
					}
					fmt.Fprintf(&extra, "\ntype %s%s = %s.%s%s\n", name, parameters, alias, b.RouteName, args)
				}
			}
		}
		if extra.Len() > 0 {
			protocolSymbols := make([]string, 0, len(protocolAliases))
			for name := range protocolAliases {
				protocolSymbols = append(protocolSymbols, name)
			}
			if err := checkProtocolGeneratedNames(sources, protocolSymbols, nil); err != nil {
				return nil, err
			}
			u.generatedFile = filepath.Join(filepath.Dir(file), "goxide_use_aliases.go")
			if _, exists := sources[u.generatedFile]; exists {
				return nil, fmt.Errorf("reserved generated use alias filename conflicts: %s", u.generatedFile)
			}
			source, e := addUseImports(u.generatedFile, []byte("package "+u.metadata.Package+"\n"), imports)
			if e != nil {
				return nil, e
			}
			u.prepared[u.generatedFile] = append(source, extra.String()...)
			u.generatedMap = sourcemap.Identity(u.generatedFile, u.prepared[u.generatedFile])
			generated, err := parser.ParseFile(token.NewFileSet(), u.generatedFile, u.prepared[u.generatedFile], 0)
			if err != nil {
				return nil, err
			}
			for _, decl := range generated.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok == token.IMPORT {
					continue
				}
				for _, spec := range gd.Specs {
					name := ""
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						name = sp.Name.Name
					case *ast.ValueSpec:
						name = sp.Names[0].Name
					}
					pos := u.positions[name]
					if !pos.IsValid() {
						continue
					}
					origin := sourcemap.Position(pos.Filename, u.original[pos.Filename], pos.Offset, pos.Offset+len("use"))
					for i := range u.generatedMap.Segments {
						seg := &u.generatedMap.Segments[i]
						if seg.Start >= int(gd.Pos())-1 && seg.End <= int(gd.End())-1 {
							seg.Origin = &origin
							seg.Kind = "use alias declaration"
						}
					}
				}
			}
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
		if b.Kind == "protocol" {
			def, err := u.protocolDefinition(b.OriginPath, b.OriginName)
			if err != nil {
				return nil, u.at(name, err)
			}
			b.RoutePath, b.RouteName = path, def.OpsName
		}
		if b.Kind != "macro" {
			u.metadata.Bindings = append(u.metadata.Bindings, b)
		}
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
	return n == useMetadataMarker || n == enumMetadataMarker || n == "GoxideReferenceMetadata" || n == protocolMetadataMarker
}
func useTypeParameterList(t types.Type) *types.TypeParamList {
	var params *types.TypeParamList
	switch t := t.(type) {
	case *types.Named:
		params = t.TypeParams()
	case *types.Alias:
		params = t.TypeParams()
	}
	return params
}
func (u *usePackage) at(name string, err error) error {
	return fmt.Errorf("%s: %w", u.positions[name], err)
}
func useTypeParameters(t types.Type, path, alias string) (string, string, error) {
	params := useTypeParameterList(t)
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
	f, e := parseSourceImports(file, source)
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
func (u *usePackage) rewrite(file string, source []byte, provenance ...*sourcemap.Map) ([]byte, error) {
	bindings := u.bindingsFor(file)
	var mapping *sourcemap.Map
	if len(provenance) > 0 {
		mapping = provenance[0]
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, file, source, parser.ParseComments)
	if e != nil {
		return nil, e
	}
	diagnostic := func(pos token.Pos, message string) error {
		position := fs.Position(pos)
		mapped := &sourcemap.Error{Message: message, Generated: position}
		if mapping != nil {
			if segment := mapping.Lookup(position.Offset); segment != nil {
				mapped.Origin, mapped.Stack = segment.Origin, segment.Stack
			}
		}
		return mapped
	}
	aliases := map[string]string{}
	importNames := map[*ast.ImportSpec]string{}
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
		importNames[im] = name
	}
	edits := []useEdit{}
	boundEdits := map[int]bool{}
	extra := map[string]string{}
	ignored := map[*ast.Ident]bool{}
	usedImports := map[string]bool{}
	handledImports := map[string]bool{}
	for alias := range u.useImports[file] {
		handledImports[alias] = true
	}
	// Contextual enum arms are compiler-owned syntax suspended in a string
	// argument. Keep imports until those nodes have been expanded and rewritten
	// in their complete lexical scope; ordinary string literals never count.
	deferredMatch := false
	shadowedAliases := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Obj != nil {
			shadowedAliases[id.Name] = true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || id.Name != "__goxide_enum_context_match" {
			return true
		}
		literal, ok := call.Args[1].(*ast.BasicLit)
		deferredMatch = deferredMatch || ok && literal.Kind == token.STRING
		return true
	})
	// Deferred arms can expose another use of an already materialized route.
	// Reuse its import rather than creating a duplicate package import on every
	// expansion. A locally shadowed alias is not a usable lexical route.
	routes := map[string]string{}
	blankImports := map[string]*ast.ImportSpec{}
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if im.Name != nil && im.Name.Name == "_" {
			blankImports[path] = im
			continue
		}
		alias := importNames[im]
		if alias != "" && alias != "." && alias != "_" && !shadowedAliases[alias] && routes[path] == "" {
			routes[path] = alias
		}
	}
	route := func(b useBinding) (string, error) {
		if !enumOriginImportAllowed(u.metadata.Path, b.RoutePath) {
			return "", fmt.Errorf("%s: unsupported re-export route for %s: Go internal package %s is inaccessible", file, b.Name, b.RoutePath)
		}
		if b.RoutePath == u.metadata.Path {
			return b.RouteName, nil
		}
		a := routes[b.RoutePath]
		if a != "" {
			usedImports[a] = true
			return a + "." + b.RouteName, nil
		}
		a = extra[b.RoutePath]
		if a == "" {
			for i := len(extra); ; i++ {
				a = fmt.Sprintf("goxideUseRoute%d", i)
				_, declared := u.localDeclarations[a]
				if !declared && !bytes.Contains(source, []byte(a)) {
					used := false
					for _, prior := range routes {
						used = used || prior == a
					}
					if !used {
						break
					}
				}
			}
			if blank := blankImports[b.RoutePath]; blank != nil {
				edits = append(edits, useEdit{int(blank.Pos()) - 1, int(blank.End()) - 1, a + " " + blank.Path.Value})
				delete(blankImports, b.RoutePath)
			} else {
				extra[b.RoutePath] = a
			}
			routes[b.RoutePath] = a
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
			if _, namespace := u.namespaces[file][x.Name]; namespace && u.imports[file][x.Name] == "" {
				problem = diagnostic(n.Sel.Pos(), "static package and type paths use '::', not '.'")
				return false
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
					problem = diagnostic(n.Sel.Pos(), fmt.Sprintf("private use binding %s.%s", x.Name, b.Name))
					return false
				}
				if b.Kind == "protocol" {
					continue
				}
				expr, e := route(b)
				if e != nil {
					problem = e
					return false
				}
				edits = append(edits, useEdit{int(n.Pos()) - 1, int(n.End()) - 1, expr})
				boundEdits[int(n.Pos())-1] = true
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
			if binding, alias := bindings[id.Name]; alias && binding.Kind != "namespace" && binding.Kind != "derive" && binding.Kind != "macro" && binding.Kind != "protocol" {
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
				if _, ok := bindings[d.Name.Name]; ok {
					return nil, fmt.Errorf("%s: use binding conflicts with function %s", fs.Position(d.Pos()), d.Name.Name)
				}
			}
		case *ast.GenDecl:
			for _, sp := range d.Specs {
				if typ, ok := sp.(*ast.TypeSpec); ok {
					if _, exists := bindings[typ.Name.Name]; exists {
						return nil, fmt.Errorf("%s: use binding conflicts with type declaration %s", fs.Position(typ.Pos()), typ.Name.Name)
					}
				}
				if v, ok := sp.(*ast.ValueSpec); ok {
					for _, id := range v.Names {
						b, exists := bindings[id.Name]
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
		b, ok := bindings[id.Name]
		if !ok || b.Kind == "macro" || b.Kind == "derive" || b.Kind == "protocol" || b.Kind == "namespace" || b.Public && (b.Kind == "type" || b.Kind == "constant") {
			continue
		}
		expr, e := route(b)
		if e != nil {
			return nil, e
		}
		edits = append(edits, useEdit{int(id.Pos()) - 1, int(id.End()) - 1, expr})
		boundEdits[int(id.Pos())-1] = true
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
		if !deferredMatch && handledImports[a] && !usedImports[a] {
			replacement := "_ " + im.Path.Value
			edits = append(edits, useEdit{int(im.Pos()) - 1, int(im.End()) - 1, replacement})
		}
	}
	out, err := applyBoundUseEdits(source, mapping, edits, boundEdits)
	if err != nil {
		return nil, err
	}
	return addUseImportsMapped(file, out, extra, mapping)
}

func (u *usePackage) rewriteExpanded(file string, source []byte, mapping *sourcemap.Map) ([]byte, error) {
	if u == nil {
		return source, nil
	}
	return u.rewrite(file, source, mapping)
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
