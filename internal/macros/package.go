package macros

import (
	"fmt"
	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// Package is an opaque source-built macro registry. It contains no executable
// metadata and is never deserialized. Each expansion owns its mutable state.
type Package struct {
	topNames       map[string]bool
	inputTokens    int
	prefix         string
	path           string
	name           string
	defs           map[string]definition
	funcs          map[string]bool
	privateHelpers map[string]bool
	scopes         map[string]*scope
}
type scope struct {
	implicitImports []string
	index           int
	pkg             *Package
	imports         map[string]string
	dependencies    map[string]*Package
}

// CollectPackage collects selected source files, retaining file-local imports.
// dependencies must come from successfully compiled source packages.
func CollectPackage(path string, sources map[string][]byte, dependencies map[string]*Package) (*Package, error) {
	p := &Package{topNames: map[string]bool{}, path: path, defs: map[string]definition{}, funcs: map[string]bool{}, privateHelpers: map[string]bool{}, scopes: map[string]*scope{}}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	p.prefix = "goxideMacroGenerated"
	for {
		collision := false
		for _, source := range sources {
			if strings.Contains(string(source), p.prefix) {
				collision = true
			}
		}
		for _, dep := range dependencies {
			for _, d := range dep.defs {
				for _, r := range d.rules {
					if strings.Contains(render(r.body), p.prefix) {
						collision = true
					}
				}
			}
		}
		if !collision {
			break
		}
		p.prefix += "X"
	}
	total := 0
	for fileIndex, name := range names {
		source := sources[name]
		mapping := sourcemap.Identity(name, source)
		prepared, err := propagate.PrepareMapped(name, source, mapping)
		if err != nil {
			return nil, err
		}
		ts, tail, err := lex(name, prepared)
		if err != nil {
			return nil, err
		}
		total += len(ts)
		if total > maxTokens {
			return nil, fmt.Errorf("%s: macro package input token budget exceeded", path)
		}
		sc := &scope{index: fileIndex, pkg: p, imports: map[string]string{}, dependencies: map[string]*Package{}}
		for path, dep := range dependencies {
			sc.dependencies[path] = dep
		}
		p.scopes[name] = sc
		for i := range ts {
			ts[i].scope = sc
			ts[i].original = true
			first, last := mapping.Lookup(ts[i].pos.Offset), mapping.Lookup(ts[i].sourceEnd-1)
			if first != nil && last != nil && first.Origin != nil && last.Origin != nil {
				span := sourcemap.Position(name, source, first.Origin.Start, last.Origin.End)
				ts[i].pos = token.Position{Filename: name, Offset: span.Start, Line: span.Line, Column: span.Column}
				ts[i].sourceEnd = span.End
			} else {
				ts[i].sourceEnd = ts[i].pos.Offset
			}
		}
		e := engine{defs: p.defs}
		clean, _, err := e.readDefinitions(ts, tail)
		if err != nil {
			return nil, err
		}
		for n, d := range p.defs {
			if d.scope == nil {
				d.scope = sc
				p.defs[n] = d
			}
		}
		for n := range declaredNames(clean) {
			p.topNames[n] = true
		}
		for helper, public := range declaredDeriveHelpers(clean) {
			p.funcs[helper] = true
			if !public {
				p.privateHelpers[helper] = true
			}
		}
		for i := 0; i+2 < len(clean); i++ {
			if clean[i].text == "func" && ident(clean[i+1].text) && clean[i+2].text == "(" {
				p.funcs[clean[i+1].text] = true
			}
		}
		if f, _ := parser.ParseFile(token.NewFileSet(), name, render(clean), parser.ImportsOnly); f != nil {
			p.name = f.Name.Name
			for _, im := range f.Imports {
				path, _ := strconv.Unquote(im.Path.Value)
				parts := strings.Split(path, "/")
				alias := parts[len(parts)-1]
				if im.Name == nil {
					sc.implicitImports = append(sc.implicitImports, path)
				}
				if im.Name != nil {
					alias = im.Name.Name
				} else if dep := dependencies[path]; dep != nil {
					alias = dep.name
				}
				if alias != "_" && alias != "." {
					sc.imports[alias] = path
				}
			}
		}
	}
	p.inputTokens = total
	return p, nil
}

// ExpandPackage uses only the supplied source registry and ordinary Go importer.
func ExpandPackage(filename string, source []byte, intrinsics []string, p *Package, imp types.Importer) (Result, error) {
	return expandPackage(filename, source, intrinsics, p, imp)
}
func (e *engine) resolveMacro(call tok, name string, qualified bool) (definition, error) {
	sc := call.scope
	if sc == nil {
		sc = e.active
	}
	p := sc.pkg
	if qualified {
		path, ok := sc.imports[call.text]
		if !ok {
			return definition{}, at(call, "macro qualifier is not an explicit import: "+call.text)
		}
		p = sc.dependencies[path]
		if p == nil {
			return definition{}, at(call, "cross-package macros require a compiled source package: "+path)
		}
		if sc == e.pkg.scopes[e.file] {
			e.usedMacroImports[call.text] = true
		}
	}
	d, ok := p.defs[name]
	if !ok {
		return definition{}, at(call, "unknown macro "+name)
	}
	if qualified && !d.public {
		return definition{}, at(call, "macro "+name+" is private")
	}
	return d, nil
}
func (e *engine) runtimePath(path string) (string, error) {
	if e.pkg != nil {
		parts := strings.Split(path, "/")
		for i, part := range parts {
			if part != "internal" {
				continue
			}
			parent := strings.Join(parts[:i], "/")
			if i == 0 || (e.pkg.path != parent && !strings.HasPrefix(e.pkg.path, parent+"/")) {
				return "", fmt.Errorf("unsupported macro runtime route: Go internal package %s is inaccessible from %s", path, e.pkg.path)
			}
		}
	}

	if alias := e.callerImports[path]; alias != "" {
		return alias, nil
	}
	if alias := e.addedImports[path]; alias != "" {
		return alias, nil
	}
	alias := e.fresh()
	e.addedImports[path] = alias
	return alias, nil
}
func (e *engine) runtimeFunction(name string) (string, error) {
	if e.active.pkg == e.pkg {
		return name, nil
	}
	if !ast.IsExported(name) || e.active.pkg.privateHelpers[name] {
		return "", fmt.Errorf("private runtime helper %s cannot cross package boundary; expose a public helper or shim", name)
	}
	alias, err := e.runtimePath(e.active.pkg.path)
	if err != nil {
		return "", err
	}
	return alias + "." + name, nil
}
func (e *engine) finishImports(ts []tok) []tok {
	// A compile-time-only ordinary import remains an explicit blank dependency.
	used := map[string]bool{}
	if file, _ := parser.ParseFile(token.NewFileSet(), e.file, render(ts), parser.AllErrors); file != nil {
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && id.Obj == nil {
					used[id.Name] = true
				}
			}
			return true
		})
	}
	for key := range e.aliases {
		for alias := range e.usedMacroImports {
			if strings.HasPrefix(key, alias+".") {
				used[alias] = true
			}
		}
	}
	for i := 0; i < len(ts); i++ {
		if ts[i].text != "import" {
			continue
		}
		j := i + 1
		group := j < len(ts) && ts[j].text == "("
		if group {
			j++
		}
		for j < len(ts) {
			if group && ts[j].text == ")" {
				break
			}
			if ts[j].text == ";" {
				j++
				continue
			}
			aliasIndex := -1
			if ident(ts[j].text) || ts[j].text == "." {
				aliasIndex = j
				j++
			}
			if j >= len(ts) {
				break
			}
			path, err := strconv.Unquote(ts[j].text)
			if err != nil {
				break
			}
			parts := strings.Split(path, "/")
			alias := parts[len(parts)-1]
			if dep := e.active.dependencies[path]; dep != nil {
				alias = dep.name
			}
			if aliasIndex >= 0 {
				alias = ts[aliasIndex].text
			}
			if e.usedMacroImports[alias] && !used[alias] {
				if aliasIndex >= 0 {
					ts[aliasIndex].text = "_"
				} else {
					ts[j].text = "_ " + ts[j].text
				}
			}
			j++
			if !group {
				break
			}
		}
		i = j - 1
	}
	if len(e.addedImports) == 0 {
		return ts
	}
	paths := make([]string, 0, len(e.addedImports))
	for p := range e.addedImports {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var code strings.Builder
	for _, path := range paths {
		fmt.Fprintf(&code, "\nimport %s %q\n", e.addedImports[path], path)
	}
	// Generated import syntax has no source origin.
	for i := 0; i+1 < len(ts); i++ {
		if ts[i].text == "package" {
			insert := tok{text: code.String()}
			return append(ts[:i+2], append([]tok{insert}, ts[i+2:]...)...)
		}
	}
	return ts
}

// Budget belongs to a compilation pass, never to the shared Package registry.
type Budget struct{ expansions, tokens, input int }

func (b *Budget) Add(r Result) error {
	b.expansions += r.ExpansionCount
	b.tokens += r.EmittedTokens
	if b.expansions > maxExpansions {
		return fmt.Errorf("macro package expansion count budget exceeded")
	}
	if b.tokens > maxTokens {
		return fmt.Errorf("macro package output token budget exceeded")
	}
	return nil
}

func (b *Budget) AddPackage(p *Package) error {
	b.input += p.inputTokens
	if b.input > maxTokens {
		return fmt.Errorf("macro build input token budget exceeded")
	}
	return nil
}

// declaredDeriveHelpers registers compiler-owned names only from explicit
// derive attributes on named declarations. Full attribute/type validation stays
// in frontend; malformed programs never become a compiled package dependency.
func declaredDeriveHelpers(ts []tok) map[string]bool {
	out := map[string]bool{}
	// Explicit manual Default is protocol evidence too. The frontend validates
	// its signature and target before a dependency's metadata can be consumed.
	for i := 0; i+4 < len(ts); i++ {
		if ts[i].text == "impl" && ts[i+1].text == "Default" && ts[i+2].text == "for" && ident(ts[i+3].text) && ts[i+4].text == "{" {
			name := ts[i+3].text
			out["Default"+name] = ast.IsExported(name)
		}
	}
	for i := 0; i+5 < len(ts); i++ {
		if ts[i].text != "#" || ts[i+1].text != "[" || ts[i+2].text != "derive" || ts[i+3].text != "(" {
			continue
		}
		end, e := closeGroup(ts, i+1)
		if e != nil {
			continue
		}
		kinds := []string{}
		for j := i + 4; j < end; j++ {
			switch ts[j].text {
			case "Default", "Clone", "Debug", "Serialize", "Deserialize":
				kinds = append(kinds, ts[j].text)
			}
		}
		j := end + 1
		for j < len(ts) {
			if ts[j].text == ";" {
				j++
				continue
			}
			if j+1 < len(ts) && ts[j].text == "#" && ts[j+1].text == "[" {
				k, e := closeGroup(ts, j+1)
				if e != nil {
					break
				}
				j = k + 1
				continue
			}
			break
		}
		public := false
		if j < len(ts) && ts[j].text == "pub" {
			public = true
			j++
		}
		if j+1 >= len(ts) {
			continue
		}
		kind := ts[j].text
		name := ts[j+1].text
		if kind != "type" && kind != "struct" && kind != "enum" {
			// A TypeSpec inside type (...) has no repeated type keyword.
			if ident(kind) && name == "struct" {
				name, kind = kind, "type"
			} else {
				continue
			}
		}
		if !ident(name) {
			continue
		}
		if kind != "enum" {
			public = ast.IsExported(name)
		}
		for _, derive := range kinds {
			out[derive+name] = public
		}
	}
	return out
}

// PublicNames enumerates public source bindings for use resolution and artifact
// diagnostics. It never serializes macro definitions.
func (p *Package) PublicNames() []string {
	var names []string
	for name, definition := range p.defs {
		if definition.public {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// PublicBinding reports the canonical source definition, not a facade spelling.
func (p *Package) PublicBinding(name string) (path, original string, ok bool) {
	d, ok := p.defs[name]
	if !ok || !d.public || d.scope == nil {
		return "", "", false
	}
	return d.scope.pkg.path, d.name, true
}

// BindPublicAlias retains the original definition scope and token provenance.
// Only source-built registries participate; no macro payload is deserialized.
func (p *Package) BindPublicAlias(alias string, source *Package, name string, public bool) error {
	if alias == "print" || alias == "println" || goResultName(alias) {
		return fmt.Errorf("macro use alias %s is a reserved builtin macro name", alias)
	}

	if source == nil {
		return fmt.Errorf("macro use requires a compiled source package")
	}
	d, ok := source.defs[name]
	if !ok || !d.public {
		return fmt.Errorf("unknown or private macro use %s", name)
	}
	if _, exists := p.defs[alias]; exists {
		return fmt.Errorf("macro use conflicts with declaration %s", alias)
	}
	if p.topNames[alias] {
		return fmt.Errorf("macro use conflicts with package declaration %s", alias)
	}
	d.public = public
	p.defs[alias] = d
	return nil
}
