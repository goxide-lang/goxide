package analysis

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type actionRenderer struct {
	q       *Queries
	file    string
	offset  uint32
	imports map[string]string // selected source route -> fresh local alias
}

func newActionRenderer(q *Queries, file string, offset uint32) *actionRenderer {
	return &actionRenderer{q: q, file: file, offset: offset, imports: map[string]string{}}
}

func (q *Queries) actionSymbol(symbol *resolve.Symbol) *resolve.Symbol {
	seen := map[*resolve.Symbol]bool{}
	for symbol != nil && !seen[symbol] {
		seen[symbol] = true
		if symbol.AliasOf != nil {
			symbol = symbol.AliasOf
			continue
		}
		if target := q.targets[strings.Join(symbol.Target, "/")]; target != nil && target != symbol {
			symbol = target
			continue
		}
		return symbol
	}
	return nil
}
func (q *Queries) sameActionSymbol(a, b *resolve.Symbol) bool {
	a, b = q.actionSymbol(a), q.actionSymbol(b)
	return a != nil && b != nil && (a == b || len(a.Target) > 0 && len(b.Target) > 0 && strings.Join(a.Target, "/") == strings.Join(b.Target, "/"))
}
func (r *actionRenderer) bindings() map[string]*resolve.Symbol {
	found := map[string]*resolve.Symbol{}
	var starts []*resolve.Scope
	if selected := r.q.scopeAt(r.file, r.offset); selected != nil {
		starts = append(starts, selected.scope)
	}
	if file := r.q.result.Resolved.Files[r.file]; file != nil {
		starts = append(starts, file.Scope)
	}
	visited := map[*resolve.Scope]bool{}
	for _, start := range starts {
		for scope := start; scope != nil && !visited[scope]; scope = scope.Parent {
			visited[scope] = true
			for key, symbol := range scope.Bindings {
				if key.Ctx != 0 || key.Namespace != resolve.OrdinaryNamespace {
					continue
				}
				if declaration := r.q.nodes[symbol.Decl]; declaration != nil && symbol.Decl.File == r.file && symbol.Kind == resolve.Variable && scope.Kind != resolve.FileScope && scope.Kind != resolve.PackageScope && declaration.Span().Start > r.offset {
					continue
				}
				if _, exists := found[key.Name]; !exists {
					found[key.Name] = symbol
				}
			}
			for key, symbols := range scope.Aliases {
				if key.Ctx == 0 && key.Namespace == resolve.OrdinaryNamespace {
					if _, exists := found[key.Name]; !exists {
						if len(symbols) == 1 {
							found[key.Name] = symbols[0]
						} else {
							found[key.Name] = nil
						}
					}
				}
			}
		}
	}
	return found
}
func (r *actionRenderer) spelling(symbol *resolve.Symbol) (string, bool) {
	symbol = r.q.actionSymbol(symbol)
	if symbol == nil {
		return "", false
	}
	bindings := r.bindings()
	var names []string
	for name, binding := range bindings {
		if r.q.sameActionSymbol(binding, symbol) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > 0 {
		return names[0], true
	}
	if symbol.Decl.File != "" && sourcePackage(symbol.Decl.File) == sourcePackage(r.file) && bindings[symbol.Name.Name] == nil {
		if _, shadowed := bindings[symbol.Name.Name]; !shadowed {
			return symbol.Name.Name, true
		}
	}
	route, ok := r.route(symbol)
	if !ok {
		return "", false
	}
	if alias := r.imports[route]; alias != "" {
		return alias, true
	}
	name := symbol.Name.Name
	if len(symbol.Target) > 0 {
		name = symbol.Target[len(symbol.Target)-1]
	}
	alias := name
	for suffix := 0; ; suffix++ {
		_, bound := bindings[alias]
		used := false
		for _, value := range r.imports {
			used = used || value == alias
		}
		if !bound && !used {
			break
		}
		alias = fmt.Sprintf("%s%d", name, suffix+1)
	}
	r.imports[route] = alias
	return alias, true
}
func (r *actionRenderer) route(symbol *resolve.Symbol) (string, bool) {
	if r.q.result.state == nil {
		return "", false
	}
	owner, name := sourcePackage(symbol.Decl.File), symbol.Name.Name
	if len(symbol.Target) > 0 {
		target := symbol.Target
		owner, name = strings.Join(target[:len(target)-1], "/"), target[len(target)-1]
	}
	if owner == "." || owner == "" || internalPackageAccess(owner, r.file) != nil {
		return "", false
	}
	if symbol.Decl.File != "" && owner != sourcePackage(r.file) && symbol.Visibility != resolve.Public {
		return "", false
	}
	config := r.q.result.state.roots[r.q.result.Files[r.file]]
	var routes []string
	for alias, namespace := range config.roots {
		// Import edits are file-scoped. A local declaration that owns this root
		// name must not be bypassed by a configured dependency of the same name.
		if resolved := r.q.result.Resolved.Files[r.file]; resolved != nil {
			blocked := false
			for scope := resolved.Scope; scope != nil; scope = scope.Parent {
				binding, exists := scope.Bindings[resolve.Key{Name: alias}]
				if !exists {
					if aliases := scope.Aliases[resolve.Key{Name: alias}]; len(aliases) == 1 {
						binding, exists = aliases[0], true
					} else if len(aliases) > 1 {
						blocked = true
						break
					}
				}
				if !exists {
					continue
				}
				target := strings.Join(binding.Target, "/")
				if binding.Kind == resolve.Module {
					target = path.Join(sourcePackage(binding.Decl.File), binding.Name.Name)
				}
				blocked = target != namespace.PackagePath
				break
			}
			if blocked {
				continue
			}
		}
		if namespace.Unavailable != "" || namespace.PackagePath == "" {
			continue
		}
		if owner != namespace.PackagePath && !strings.HasPrefix(owner, namespace.PackagePath+"/") {
			continue
		}
		suffix := strings.TrimPrefix(strings.TrimPrefix(owner, namespace.PackagePath), "/")
		if suffix != "" && namespace.GoPrefix == "" {
			// Source crates expose declared, public child modules only.
			current := namespace.PackagePath
			accessible := true
			for _, part := range strings.Split(suffix, "/") {
				member := r.q.targets[current+"/"+part]
				if member == nil || member.Kind != resolve.Module || member.Visibility != resolve.Public && sourcePackage(r.file) != current {
					accessible = false
					break
				}
				current += "/" + part
			}
			if !accessible {
				continue
			}
		}
		route := alias
		if suffix != "" {
			route += "::" + strings.ReplaceAll(suffix, "/", "::")
		}
		route += "::" + name
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool {
		if len(routes[i]) != len(routes[j]) {
			return len(routes[i]) < len(routes[j])
		}
		return routes[i] < routes[j]
	})
	if len(routes) == 0 {
		return "", false
	}
	return routes[0], true
}
func (r *actionRenderer) importEdits() []SourceEdit {
	if len(r.imports) == 0 {
		return nil
	}
	source := r.q.result.Sources[r.file]
	if source == nil || source.Syntax == nil || source.Syntax.Package == nil {
		return nil
	}
	newline := "\n"
	if strings.Contains(string(source.Content), "\r\n") {
		newline = "\r\n"
	}
	var routes []string
	for route := range r.imports {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	var lines []string
	for _, route := range routes {
		line := "use " + route
		name := route[strings.LastIndex(route, "::")+2:]
		if r.imports[route] != name {
			line += " as " + r.imports[route]
		}
		lines = append(lines, line+";")
	}
	at := source.Syntax.Package.Span().End
	return []SourceEdit{{File: r.file, Span: syntax.Span{Start: at, End: at}, NewText: newline + strings.Join(lines, newline) + newline}}
}

func (q *Queries) importActions(file string, selection syntax.Span) []CodeAction {
	resolved := q.result.Resolved.Files[file]
	if resolved == nil {
		return nil
	}
	var requested string
	syntax.Walk(q.result.Sources[file].Syntax, func(node syntax.Node) bool {
		identifier, ok := node.(*syntax.Ident)
		if !ok || identifier.Missing || !actionIntersects(identifier.Span(), selection) {
			return true
		}
		for _, diagnostic := range q.result.Resolved.Diagnostics {
			if diagnostic.File == file && diagnostic.Code == "unresolved" && diagnostic.Span == identifier.Span() {
				requested = identifier.Name
				return false
			}
		}
		return true
	})
	if requested == "" {
		return nil
	}
	renderer := newActionRenderer(q, file, selection.Start)
	if _, bound := renderer.bindings()[requested]; bound {
		return nil
	}
	seen := map[string]bool{}
	var result []CodeAction
	for _, candidate := range q.targets {
		candidate = q.actionSymbol(candidate)
		if candidate == nil || candidate.Name.Name != requested || candidate.Visibility != resolve.Public || path.Dir(candidate.Decl.File) == path.Dir(file) {
			continue
		}
		route, ok := renderer.route(candidate)
		if !ok || seen[route] {
			continue
		}
		seen[route] = true
		editRenderer := newActionRenderer(q, file, selection.Start)
		editRenderer.imports[route] = requested
		result = append(result, CodeAction{Title: "Import " + route, Kind: "quickfix", Edits: editRenderer.importEdits()})
	}
	return result
}
