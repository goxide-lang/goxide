package analysis

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
	producer *ProcLibrary
	roots    map[*syntax.File]moduleRootConfig
}

// UpdateMacroBindings receives retained generated bindings before another
// external macro is selected. It uses the same root precedence and hygiene as
// the final resolution pass; source definitions keep their original identity.
func (m *moduleMacros) UpdateMacroBindings(expanded *expand.Result) error {
	for _, requirement := range applyExpandedRoots(expanded, m.roots, m.files) {
		if _, exists := m.imports.namespaces[requirement.target]; !exists {
			m.imports.namespaces[requirement.target] = requirement
		}
	}
	// Classify aliases from already loaded source scopes without activating
	// other dependency roots that later generated declarations may shadow.
	m.resolved = resolve.ResolveExpanded(m.files, expanded, m.imports.bindingView())
	m.imports.SetSourcePackages(m.resolved.Packages)
	return m.imports.err
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

func (m *moduleMacros) lookupMacroCandidates(path *syntax.Path, scope expand.Scope) ([]moduleMember, string, error) {
	var source *resolve.File
	for name, file := range m.files {
		if file == scope.File {
			source = m.resolved.Files[name]
			break
		}
	}
	if source == nil {
		return nil, "", fmt.Errorf("macro definition source is unavailable")
	}
	parts := make([]string, len(path.Segments))
	for i, segment := range path.Segments {
		parts[i] = segment.Name.Name
	}
	key := resolve.Key{Name: parts[0], Ctx: scope.Ctx}
	if len(parts) == 1 {
		key.Namespace = resolve.MacroNamespace
	}
	visiting := map[string]bool{}
	var roots []moduleMember
	var globs []*resolve.UseBinding
	for current := source.Scope; current != nil; current = current.Parent {
		if current.Kind == resolve.PreludeScope {
			break // Go builtins do not occupy the macro invocation namespace.
		}
		if symbol := current.Bindings[key]; symbol != nil {
			owner := strings.Split(filepath.ToSlash(filepath.Dir(symbol.Decl.File)), "/")
			roots = m.imports.symbolMembers(symbol, owner, key, visiting)
			if len(roots) != 0 {
				break
			}
		}
		if aliases := current.Aliases[key]; len(aliases) != 0 {
			for _, alias := range aliases {
				roots = append(roots, m.imports.symbolMembers(alias, nil, key, visiting)...)
			}
			if len(roots) != 0 {
				break
			}
		}
		globs = append(globs, current.Globs...)
	}
	if len(roots) == 0 {
		for _, implicit := range []bool{false, true} {
			for _, use := range globs {
				if use.Implicit == implicit {
					roots = append(roots, m.imports.lookup(use.Target, key, visiting)...)
				}
			}
			if len(roots) != 0 {
				break
			}
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
			candidates = append(candidates, m.imports.lookup(prefix, resolve.Key{Name: parts[len(parts)-1], Ctx: scope.Ctx, Namespace: resolve.MacroNamespace}, visiting)...)
		}
		if len(roots) == 0 {
			target = strings.Join(parts[:len(parts)-1], "/")
		}
	}
	return candidates, target, nil
}

func (m *moduleMacros) LookupMacro(path *syntax.Path, scope expand.Scope) (expand.Macro, error) {
	candidates, target, err := m.lookupMacroCandidates(path, scope)
	if err != nil {
		return expand.Macro{}, err
	}
	name := path.Segments[len(path.Segments)-1].Name.Name
	return m.selectMacro(candidates, target, name)
}

func (m *moduleMacros) selectMacro(candidates []moduleMember, target, name string) (expand.Macro, error) {
	var macros []moduleMember
	for _, candidate := range candidates {
		if candidate.Kind == resolve.Macro && candidate.symbol != nil {
			macros = append(macros, candidate)
		}
	}
	macros = uniqueModuleMembers(macros)
	if len(macros) > 1 {
		return expand.Macro{}, fmt.Errorf("macro %s is ambiguous", name)
	}
	if len(macros) == 1 {
		symbol := macros[0].symbol
		owner := filepath.ToSlash(filepath.Dir(symbol.Decl.File))
		if proc, ok := m.imports.procs[owner][symbol.Name.Name]; ok {
			return expand.Macro{Proc: proc.entry, File: proc.entry.File, Run: proc.library.Run}, nil
		}
		file := m.files[symbol.Decl.File]
		if file != nil {
			// Binding snapshots renumber copied AST nodes. The original file
			// and hygienic name still identify the unique source definition.
			for _, item := range file.Items {
				if decl, ok := item.(*syntax.MacroRulesDecl); ok && decl.Name.Name == symbol.Name.Name && decl.Name.Ctx == symbol.Name.Ctx {
					return expand.Macro{Rules: decl, File: file}, nil
				}
			}
		}
	}
	if requirement, ok := m.imports.namespace(strings.Split(target, "/")); ok && requirement.namespace.PackagePath != "" {
		target = requirement.namespace.PackagePath
	}
	if m.producer != nil {
		for _, entry := range m.producer.Entries {
			if target == m.producer.PackagePath && entry.Name == name {
				return expand.Macro{}, fmt.Errorf("procedural macro %s cannot be invoked by its defining crate", name)
			}
			for _, candidate := range candidates {
				if strings.Join(candidate.Target, "/") == m.producer.PackagePath+"/"+entry.Function.Name.Name {
					return expand.Macro{}, fmt.Errorf("procedural macro %s cannot be invoked by its defining crate", entry.Name)
				}
			}
		}
	}
	if packageScope := m.imports.local[target]; packageScope != nil {
		if symbol := packageScope.Bindings[resolve.Key{Name: name, Namespace: resolve.MacroNamespace}]; symbol != nil && symbol.Kind == resolve.Macro && symbol.Visibility != resolve.Public {
			return expand.Macro{}, fmt.Errorf("macro %s is private in %s", name, target)
		}
		return expand.Macro{}, fmt.Errorf("macro %s has no source definition in %s", name, target)
	}
	return expand.Macro{}, fmt.Errorf("macro %s requires source module %s", name, target)
}
