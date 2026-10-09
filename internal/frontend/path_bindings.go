package frontend

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func (u *usePackage) checkedPackage(node paths.Namespace) (*types.Package, error) {
	switch node.Source {
	case "", "local", "module", "toolchain":
	default:
		return nil, fmt.Errorf("unknown namespace source %q", node.Source)
	}
	if node.PackagePath == "" {
		return nil, fmt.Errorf("namespace group has no concrete package")
	}
	if !enumOriginImportAllowed(u.metadata.Path, node.PackagePath) {
		return nil, fmt.Errorf("internal package %s is inaccessible from %s", node.PackagePath, u.metadata.Path)
	}
	if u.imp == nil {
		return nil, fmt.Errorf("static path requires a package importer")
	}
	pkg, err := u.imp.Import(node.PackagePath)
	if err != nil {
		return nil, err
	}
	if pkg == nil || pkg.Path() != node.PackagePath {
		return nil, fmt.Errorf("static path package identity mismatch: expected %s", node.PackagePath)
	}
	if node.Source == "module" || node.Source == "toolchain" || node.Source == "local" || node.ModulePath != "" {
		if node.Source == "local" && node.ModulePath == "" {
			return nil, fmt.Errorf("local mod package %s requires selected module ownership proof", node.PackagePath)
		}
		origin, ok := u.imp.(interface {
			PackageOrigin(string) (string, bool, error)
		})
		if !ok {
			return nil, fmt.Errorf("package origin verification unavailable for %s", node.PackagePath)
		}
		module, standard, err := origin.PackageOrigin(node.PackagePath)
		if err != nil {
			return nil, err
		}
		if node.Source == "toolchain" && !standard || (node.Source != "toolchain") && (standard || node.ModulePath == "" || module != node.ModulePath) {
			return nil, fmt.Errorf("static path package source mismatch for %s", node.PackagePath)
		}
	}
	return pkg, nil
}

func (u *usePackage) surface(node paths.Namespace) (map[string]useBinding, error) {
	out := map[string]useBinding{}
	if node.PackagePath != "" {
		pkg, err := u.checkedPackage(node)
		if err != nil {
			return nil, err
		}
		for _, name := range pkg.Scope().Names() {
			if ast.IsExported(name) && !isUseMarker(name) {
				if b, e := nativeUseBinding(pkg.Scope().Lookup(name), node.PackagePath, name); e == nil {
					out[name] = b
				}
			}
		}
		m, ok := u.imported[node.PackagePath]
		if !ok {
			m, err = u.load(node.PackagePath)
			if err != nil {
				return nil, err
			}
			u.imported[node.PackagePath] = m
		}
		for _, b := range m.Bindings {
			if b.Public {
				out[b.Name] = b
			} else {
				delete(out, b.Name)
			}
		}
		for _, name := range m.Unsupported {
			kind := "unsupported"
			if m.UnsupportedKinds[name] == "macro" {
				kind = "macro-unavailable"
				if _, exists := out[name]; exists {
					kind = "ambiguous"
				}
			}
			out[name] = useBinding{Name: name, Kind: kind}
		}
		if registry := u.known[node.PackagePath].macros; registry != nil {
			for _, name := range registry.PublicNames() {
				owner, original, ok := registry.PublicBinding(name)
				if !ok {
					continue
				}
				if prior, ok := out[name]; ok && prior.Kind != "unsupported" && prior.Kind != "macro-unavailable" {
					out[name] = useBinding{Name: name, Kind: "ambiguous"}
					continue
				}
				out[name] = useBinding{Name: name, Kind: "macro", OriginPath: owner, OriginName: original, RoutePath: node.PackagePath, RouteName: name, Public: true}
			}
		}
		for _, name := range []string{"Serialize", "Deserialize"} {
			if def, ok := derives.Lookup(node.PackagePath, name); ok {
				if _, exists := out[name]; exists {
					return nil, fmt.Errorf("ambiguous namespace member %s: derive and Go declaration", name)
				}
				out[name] = useBinding{Name: name, Kind: "derive", OriginPath: def.Owner, OriginName: def.Name, RoutePath: def.Owner, RouteName: def.Name, Public: true}
			}
		}
	}
	for name, child := range node.Children {
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("ambiguous namespace member %s: explicit child and package export", name)
		}
		n := child
		out[name] = useBinding{Name: name, Kind: "namespace", OriginPath: n.PackagePath, OriginName: name, RoutePath: n.PackagePath, RouteName: name, Namespace: &n, Public: true}
	}
	return out, nil
}

func (u *usePackage) resolveNamespace(names []string) (paths.Namespace, []string, error) {
	if len(names) == 0 {
		return paths.Namespace{}, nil, fmt.Errorf("empty use path")
	}
	node, ok := u.roots[names[0]]
	if !ok {
		return node, nil, &paths.UnboundError{Name: names[0]}
	}
	remaining := names[1:]
	for len(remaining) > 0 {
		surface, err := u.surface(node)
		if err != nil {
			return node, nil, err
		}
		b, ok := surface[remaining[0]]
		if !ok || b.Kind != "namespace" || b.Namespace == nil {
			break
		}
		node = *b.Namespace
		remaining = remaining[1:]
	}
	return node, remaining, nil
}

func (u *usePackage) bindUse(file string, d useDecl, b useBinding) error {
	if b.Kind == "namespace" {
		if _, predeclared := types.Universe.Lookup(b.Name).(*types.TypeName); predeclared {
			return fmt.Errorf("%s: namespace %s conflicts with a predeclared type; choose a distinct namespace alias", d.pos, b.Name)
		}
		b.OriginName, b.RouteName = b.Name, b.Name
	}
	if _, exists := u.localDeclarations[b.Name]; exists {
		return fmt.Errorf("%s: use binding %s conflicts with a package declaration", d.pos, b.Name)
	}
	if !paths.ValidIdentifier(b.Name) || isUseMarker(b.Name) || strings.HasPrefix(b.Name, "__goxide_") || b.Name == "init" || b.Name == "main" {
		return fmt.Errorf("%s: reserved use binding %s", d.pos, b.Name)
	}
	if (b.Kind == "function" || b.Kind == "variable") && !enumOriginImportAllowed(u.metadata.Path, b.RoutePath) {
		return fmt.Errorf("%s: use route %s is inaccessible from %s", d.pos, b.RoutePath, u.metadata.Path)
	}
	if b.Kind == "derive" {
		def, ok := derives.Lookup(b.OriginPath, b.OriginName)
		if !ok {
			return fmt.Errorf("%s: unknown derive owner %s", d.pos, b.OriginPath)
		}
		if err := derives.Validate(def, u.imp); err != nil {
			return fmt.Errorf("%s: %w", d.pos, err)
		}
	}
	bindings := u.fileBindings[file]
	if prior, ok := bindings[b.Name]; ok {
		if !sameUseBinding(prior, b) {
			return fmt.Errorf("%s: ambiguous use binding %s", d.pos, b.Name)
		}
		b.Public = b.Public || prior.Public
	}
	if prior, ok := u.bindings[b.Name]; ok && !sameUseBinding(prior, b) {
		return fmt.Errorf("%s: use binding %s conflicts with public declaration", d.pos, b.Name)
	}
	bindings[b.Name] = b
	if b.Public {
		u.bindings[b.Name] = b
	}
	if _, ok := u.positions[b.Name]; !ok {
		u.positions[b.Name] = d.pos
	}
	return nil
}

func sameUseBinding(a, b useBinding) bool {
	if a.Kind == "macro" && b.Kind == "macro" {
		return a.OriginPath == b.OriginPath && a.OriginName == b.OriginName
	}
	return a.Kind == b.Kind && a.OriginPath == b.OriginPath && a.OriginName == b.OriginName && a.RoutePath == b.RoutePath && a.RouteName == b.RouteName && reflect.DeepEqual(a.Namespace, b.Namespace)
}

// bindMacros gives macro hygiene the same lexical bindings as ordinary source.
// Public macros are declarations of the package; every other alias belongs to
// the file containing its use declaration.
func (u *usePackage) bindMacros(registry *macros.Package, dependencies map[string]*macros.Package) error {
	files := make([]string, 0, len(u.fileBindings))
	for file := range u.fileBindings {
		files = append(files, file)
	}
	sort.Strings(files)
	registered := map[string]bool{}
	for _, file := range files {
		bindings := u.bindingsFor(file)
		for _, name := range sortedUseNames(bindings) {
			b := bindings[name]
			if b.Kind == "function" || b.Kind == "variable" || b.Kind == "type" || b.Kind == "constant" {
				if err := registry.BindUseName(file, name); err != nil {
					return u.at(name, err)
				}
			}
			switch b.Kind {
			case "macro":
				if b.Public && registered[name] {
					continue
				}
				if err := registry.BindAlias(file, name, dependencies[b.RoutePath], b.RouteName, b.Public); err != nil {
					return u.at(name, err)
				}
				registered[name] = b.Public
			case "function":
				if err := registry.BindFunctionAlias(file, name, b.RoutePath, b.RouteName); err != nil {
					return u.at(name, err)
				}
			}
		}
	}
	return nil
}

func (u *usePackage) bindingsFor(file string) map[string]useBinding {
	out := map[string]useBinding{}
	for name, b := range u.bindings {
		out[name] = b
	}
	for name, b := range u.fileBindings[file] {
		out[name] = b
	}
	return out
}

func pathBinding(b useBinding) paths.Binding {
	return paths.Binding{Kind: b.Kind, OwnerPath: b.OriginPath, Name: b.OriginName, RoutePath: b.RoutePath, RouteName: b.RouteName, Namespace: b.Namespace}
}

func (u *usePackage) resolvePath(file string, names []string) (paths.Binding, error) {
	if len(names) == 0 {
		return paths.Binding{}, fmt.Errorf("empty static path")
	}
	if len(names) == 1 {
		if b, ok := u.bindingsFor(file)[names[0]]; ok {
			return pathBinding(b), nil
		}
		if b, ok := u.localDeclarations[names[0]]; ok {
			return b, nil
		}
		return paths.Binding{}, &paths.UnboundError{Name: names[0]}
	}
	node, ok := u.namespaces[file][names[0]]
	if !ok {
		if b, found := u.bindings[names[0]]; found && b.Kind == "namespace" && b.Namespace != nil {
			node = *b.Namespace
			ok = true
		}
	}
	if !ok {
		return paths.Binding{}, &paths.UnboundError{Name: names[0]}
	}
	for i, name := range names[1:] {
		surface, err := u.surface(node)
		if err != nil {
			return paths.Binding{}, err
		}
		b, ok := surface[name]
		if !ok {
			return paths.Binding{}, fmt.Errorf("unknown, private, or unsupported static member %s", strings.Join(names, "::"))
		}
		if i == len(names)-2 {
			return pathBinding(b), nil
		}
		if b.Kind != "namespace" || b.Namespace == nil {
			return paths.Binding{}, fmt.Errorf("static item %s is not a namespace", name)
		}
		node = *b.Namespace
	}
	panic("unreachable static path")
}

func (u *usePackage) prepareNamespaceImports(file string, source []byte) ([]byte, error) {
	imports := map[string]string{}
	plan := u.plans[file]
	positions := map[string]token.Position{}
	importPositions := map[string]token.Position{}
	for _, d := range plan.declarations {
		positions[d.alias] = d.pos
	}
	_, mods, _ := scanModules(file, u.original[file])
	for _, d := range mods {
		positions[d.alias] = d.pos
	}
	aliases := u.namespaces[file]
	names := make([]string, 0, len(aliases))
	for name := range aliases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		node := aliases[name]
		if node.PackagePath != "" {
			if _, err := u.checkedPackage(node); err != nil {
				return nil, u.at(name, err)
			}
		}
	}
	fresh := func(node paths.Namespace) string {
		if alias := imports[node.PackagePath]; alias != "" {
			return alias
		}
		for i := 0; ; i++ {
			alias := fmt.Sprintf("goxideNamespace%d", i)
			_, declared := u.localDeclarations[alias]
			if !declared && !strings.Contains(string(source), alias) {
				if _, ok := aliases[alias]; !ok {
					aliases[alias] = node
					imports[node.PackagePath] = alias
					return alias
				}
			}
		}
	}
	for _, name := range names {
		if node := aliases[name]; node.PackagePath != "" {
			importPositions[fresh(node)] = positions[name]
		}
	}
	ts := pathTokens(file, source)
	for i := 0; i+1 < len(ts); i++ {
		if ts[i].kind == token.TYPE && ts[i+1].kind == token.IDENT {
			if _, visible := aliases[ts[i+1].text]; visible {
				return nil, fmt.Errorf("%s: local type %s conflicts with a visible use namespace; choose a distinct type or namespace alias", ts[i+1].pos, ts[i+1].text)
			}
		}
	}
	var edits []useEdit
	for i := 0; i+3 < len(ts); i++ {
		// Derives resolve directly through the source binding table. They do
		// not need a Go alias, and diagnostics should retain their source path.
		if ts[i].text == "#" && ts[i+1].kind == token.LBRACK {
			depth := 1
			for i += 2; i < len(ts) && depth > 0; i++ {
				if ts[i].kind == token.LBRACK {
					depth++
				}
				if ts[i].kind == token.RBRACK {
					depth--
				}
			}
			i--
			continue
		}
		if ts[i].kind != token.IDENT || ts[i+1].kind != token.COLON || ts[i+2].kind != token.COLON {
			continue
		}
		if i > 0 && ts[i-1].kind == token.PERIOD || i >= 2 && ts[i-1].kind == token.COLON && ts[i-2].kind == token.COLON {
			continue
		}
		node, ok := aliases[ts[i].text]
		if !ok {
			continue
		}
		end := i
		j := i + 3
		for j < len(ts) && ts[j].kind == token.IDENT {
			surface, err := u.surface(node)
			if err != nil {
				return nil, err
			}
			b, ok := surface[ts[j].text]
			if !ok || b.Kind != "namespace" || b.Namespace == nil {
				break
			}
			node = *b.Namespace
			end = j
			if j+2 >= len(ts) || ts[j+1].kind != token.COLON || ts[j+2].kind != token.COLON {
				break
			}
			j += 3
		}
		if node.PackagePath == "" {
			return nil, fmt.Errorf("%s: namespace group cannot be used as a value", ts[i].pos)
		}
		alias := fresh(node)
		if position := importPositions[alias]; !position.IsValid() {
			importPositions[alias] = positions[ts[i].text]
		}
		if end != i || alias != ts[i].text {
			edits = append(edits, useEdit{ts[i].start, ts[end].end, alias})
		}
		i = end
	}
	// Every explicit use remains an initialization dependency, even when its
	// runtime expression is rewritten through a legal public facade elsewhere.
	for _, p := range plan.Imports {
		if _, ok := imports[p]; !ok {
			alias := fresh(paths.Namespace{PackagePath: p})
			for _, d := range plan.declarations {
				node, _, _ := explicitNamespace(u.roots, d.segments)
				if node.PackagePath == p {
					importPositions[alias] = d.pos
					break
				}
			}
		}
	}
	for p, alias := range imports {
		u.imports[file][alias] = p
		u.useImports[file][alias] = true
	}
	mapping := u.preparedMaps[file]
	out, err := applyUseEdits(source, mapping, edits, "namespace paths", true, "namespace-path")
	if err != nil {
		return nil, err
	}
	out, err = addUseImportsMapped(file, out, imports, mapping)
	if err != nil {
		return nil, err
	}
	f, err := parseSourceImports(file, out)
	if err != nil {
		return nil, err
	}
	for _, spec := range f.Imports {
		if spec.Name == nil {
			continue
		}
		pos := importPositions[spec.Name.Name]
		if !pos.IsValid() {
			continue
		}
		origin := sourcemap.Position(pos.Filename, u.original[pos.Filename], pos.Offset, pos.Offset+len("use"))
		// Keep the cause on individual import tokens. AST printing preserves
		// exact token evidence; a span over the whole spec only proves context.
		mapping.Attach(int(spec.Name.Pos())-1, int(spec.Name.End())-1, &origin, nil, "use import")
		mapping.Attach(int(spec.Path.Pos())-1, int(spec.Path.End())-1, &origin, nil, "use import")
	}
	return out, nil
}
