package lower

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type moduleMacros struct {
	files    map[string]*syntax.File
	resolved *resolve.Result
	imports  *moduleImporter
}

// Actual expansions retain source dependencies even when no Go import remains.
// Calls inside items removed by cfg never enter this graph.
func (m *moduleMacros) checkCycles(expanded *expand.Result) error {
	var names []string
	fileNames := make(map[*syntax.File]string, len(m.files))
	for name, file := range m.files {
		names = append(names, name)
		fileNames[file] = name
	}
	sort.Strings(names)
	graph := map[string][]moduleImport{}
	ids := make([]syntax.ExpnID, 0, len(expanded.Expansions))
	for id := range expanded.Expansions {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		frame := expanded.Expansions[id]
		name, target := fileNames[frame.Call.File], fileNames[frame.Definition.File]
		owner := filepath.ToSlash(filepath.Dir(name))
		dependency := filepath.ToSlash(filepath.Dir(target))
		if owner != dependency {
			origin := resolve.Origin{File: name, Span: frame.Call.Span, Expn: id, Stack: frame.Stack}
			graph[owner] = append(graph[owner], moduleImport{target: dependency, file: name, span: frame.Call.Span, origin: origin})
		}
	}
	states := map[string]uint8{}
	var stack []string
	var visit func(string) error
	visit = func(owner string) error {
		states[owner] = 1
		stack = append(stack, owner)
		for _, edge := range graph[owner] {
			if states[edge.target] == 1 {
				start := 0
				for stack[start] != edge.target {
					start++
				}
				cycle := append(append([]string(nil), stack[start:]...), edge.target)
				return &Diagnostic{File: edge.file, Span: edge.span, Origin: edge.origin, Message: "macro source dependency cycle: " + strings.Join(cycle, " -> ")}
			}
			if states[edge.target] == 0 {
				if err := visit(edge.target); err != nil {
					return err
				}
			}
		}
		states[owner] = 2
		stack = stack[:len(stack)-1]
		return nil
	}
	for _, name := range names {
		owner := filepath.ToSlash(filepath.Dir(name))
		if states[owner] == 0 {
			if err := visit(owner); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *moduleMacros) LookupMacro(path *syntax.Path, scope expand.Scope) (*syntax.MacroRulesDecl, *syntax.File, error) {
	var source *resolve.File
	for name, file := range m.files {
		if file == scope.File {
			source = m.resolved.Files[name]
			break
		}
	}
	if source == nil {
		return nil, nil, fmt.Errorf("macro definition source is unavailable")
	}
	parts := make([]string, len(path.Segments))
	for i, segment := range path.Segments {
		parts[i] = segment.Name.Name
	}
	key := resolve.Key{Name: parts[0], Ctx: scope.Ctx}
	visiting := map[string]bool{}
	var roots []moduleMember
	var globs []*resolve.UseBinding
	for current := source.Scope; current != nil; current = current.Parent {
		if symbol := current.Bindings[key]; symbol != nil {
			owner := strings.Split(filepath.ToSlash(filepath.Dir(symbol.Decl.File)), "/")
			roots = m.imports.symbolMembers(symbol, owner, key, visiting)
			break
		}
		if aliases := current.Aliases[key]; len(aliases) != 0 {
			for _, alias := range aliases {
				roots = append(roots, m.imports.symbolMembers(alias, nil, key, visiting)...)
			}
			break
		}
		globs = append(globs, current.Globs...)
	}
	if len(roots) == 0 {
		for _, use := range globs {
			roots = append(roots, m.imports.lookup(use.Target, key, visiting)...)
		}
	}
	var candidates []moduleMember
	target := filepath.ToSlash(filepath.Dir(source.Name))
	if len(parts) == 1 {
		candidates = roots
	} else {
		for _, root := range roots {
			if root.Kind != resolve.Module && root.Kind != resolve.Import {
				continue
			}
			prefix := append(append([]string(nil), root.Target...), parts[1:len(parts)-1]...)
			target = strings.Join(prefix, "/")
			candidates = append(candidates, m.imports.lookup(prefix, resolve.Key{Name: parts[len(parts)-1], Ctx: scope.Ctx}, visiting)...)
		}
		if len(roots) == 0 {
			target = strings.Join(parts[:len(parts)-1], "/")
		}
	}
	var macros []moduleMember
	for _, candidate := range candidates {
		if candidate.Kind == resolve.Macro && candidate.symbol != nil {
			macros = append(macros, candidate)
		}
	}
	macros = uniqueModuleMembers(macros)
	if len(macros) > 1 {
		return nil, nil, fmt.Errorf("macro %s is ambiguous", parts[len(parts)-1])
	}
	if len(macros) == 1 {
		symbol := macros[0].symbol
		file := m.files[symbol.Decl.File]
		if file != nil {
			for _, item := range file.Items {
				if decl, ok := item.(*syntax.MacroRulesDecl); ok && decl.Name.ID() == symbol.Decl.ID {
					return decl, file, nil
				}
			}
		}
	}
	if requirement, ok := m.imports.namespace(strings.Split(target, "/")); ok && requirement.namespace.PackagePath != "" {
		target = requirement.namespace.PackagePath
	}
	name := parts[len(parts)-1]
	if packageScope := m.imports.local[target]; packageScope != nil {
		if symbol := packageScope.Bindings[resolve.Key{Name: name}]; symbol != nil && symbol.Kind == resolve.Macro && symbol.Visibility != resolve.Public {
			return nil, nil, fmt.Errorf("macro %s is private in %s", name, target)
		}
		return nil, nil, fmt.Errorf("macro %s has no source definition in %s", name, target)
	}
	return nil, nil, fmt.Errorf("macro %s requires source module %s", name, target)
}
