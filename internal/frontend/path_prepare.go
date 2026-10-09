package frontend

import (
	"fmt"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
)

type PathPreparation struct {
	Source       []byte
	Imports      []string
	Locals       paths.Roots
	declarations []useDecl
}

func pathTokens(filename string, source []byte) []useToken {
	fs := token.NewFileSet()
	f := fs.AddFile(filename, -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, source, func(token.Position, string) {}, 0)
	var out []useToken
	for {
		pos, kind, text := scan.Scan()
		if kind == token.EOF {
			break
		}
		if text == "" {
			text = kind.String()
		}
		start := f.Offset(pos)
		end := start + len(text)
		if kind == token.SEMICOLON && text == "\n" {
			end = start
		}
		out = append(out, useToken{text, kind, start, end, fs.Position(pos)})
	}
	return out
}

func scanModules(file string, source []byte) ([]byte, []useDecl, error) {
	ts := pathTokens(file, source)
	out := append([]byte(nil), source...)
	var mods []useDecl
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.kind == token.LBRACE {
			depth++
		}
		if t.kind == token.RBRACE {
			depth--
		}
		if depth != 0 || i == 0 || ts[i-1].kind != token.SEMICOLON && ts[i-1].kind != token.RBRACE {
			continue
		}
		j := i
		public := false
		if t.text == "pub" && j+1 < len(ts) && ts[j+1].text == "mod" {
			public = true
			j++
		} else if t.text != "mod" {
			continue
		}
		if j+2 >= len(ts) || ts[j+1].kind != token.IDENT || !paths.ValidIdentifier(ts[j+1].text) {
			return nil, nil, fmt.Errorf("%s: expected [pub] mod name;", t.pos)
		}
		name := ts[j+1].text
		if ts[j+2].kind != token.SEMICOLON {
			return nil, nil, fmt.Errorf("%s: local mod supports 'mod name;'; inline modules and external module initializers are unsupported", t.pos)
		}
		mods = append(mods, useDecl{alias: name, name: name, file: file, public: public, pos: t.pos})
		for k := t.start; k < ts[j+2].end; k++ {
			if out[k] != '\n' && out[k] != '\r' {
				out[k] = ' '
			}
		}
		i = j + 2
	}
	return out, mods, nil
}

// LocalDeclarations only authorizes explicitly declared, exact child packages.
// localPackages, when provided by the package loader, proves build selection.
func LocalDeclarations(owner string, sources map[string][]byte, roots paths.Roots, localPackages map[string]bool, ownerModule ...string) (paths.Roots, error) {
	merged := paths.Roots{}
	for name, node := range roots {
		merged[name] = node
	}
	seen := map[string]bool{}
	declarations := localDeclarationBindings(owner, sources)
	for _, file := range sortedSources(sources) {
		if strings.HasSuffix(file, ".go") {
			continue
		}
		_, mods, err := scanModules(file, sources[file])
		if err != nil {
			return nil, err
		}
		for _, mod := range mods {
			if _, exists := declarations[mod.alias]; exists {
				return nil, fmt.Errorf("%s: local mod %s conflicts with package declaration", mod.pos, mod.alias)
			}
			if owner == "" {
				return nil, fmt.Errorf("%s: local mod requires a package owner", mod.pos)
			}
			p := owner + "/" + mod.alias
			if seen[mod.alias] {
				return nil, fmt.Errorf("%s: duplicate local mod %s", mod.pos, mod.alias)
			}
			if prior, ok := merged[mod.alias]; ok && (prior.Source != "local" || prior.PackagePath != p) {
				return nil, fmt.Errorf("%s: local mod %s conflicts with dependency root", mod.pos, mod.alias)
			}
			if localPackages != nil && !localPackages[p] {
				return nil, fmt.Errorf("%s: local mod package %s is not selected or does not exist", mod.pos, p)
			}
			module := merged[mod.alias].ModulePath
			if len(ownerModule) > 0 {
				module = ownerModule[0]
			}
			merged[mod.alias] = paths.Namespace{PackagePath: p, Source: "local", ModulePath: module}
			seen[mod.alias] = true
		}
	}
	return merged, nil
}

func explicitNamespace(roots paths.Roots, names []string) (paths.Namespace, []string, error) {
	if len(names) == 0 {
		return paths.Namespace{}, nil, fmt.Errorf("empty static path")
	}
	node, ok := roots[names[0]]
	if !ok {
		return paths.Namespace{}, nil, &paths.UnboundError{Name: names[0]}
	}
	remaining := names[1:]
	for len(remaining) > 0 {
		child, ok := node.Children[remaining[0]]
		if !ok {
			break
		}
		node = child
		remaining = remaining[1:]
	}
	if node.PackagePath == "" && len(remaining) > 0 {
		return node, remaining, fmt.Errorf("unknown namespace member %s", remaining[0])
	}
	return node, remaining, nil
}

// PrepareSourcePaths is the dependency-discovery half of use preparation. It
// never imports a package, invents a child namespace or changes a version.
func PrepareSourcePaths(owner, file string, source []byte, roots paths.Roots, localPackages map[string]bool) (PathPreparation, error) {
	return prepareSourcePaths(owner, file, source, roots, localPackages, nil)
}

// PreparePackageSourcePaths shares public declarations across source files while
// retaining the definition file's private alias scope during path expansion.
func PreparePackageSourcePaths(owner string, sources map[string][]byte, roots paths.Roots, localPackages map[string]bool) (map[string]PathPreparation, error) {
	merged, err := LocalDeclarations(owner, sources, roots, localPackages)
	if err != nil {
		return nil, err
	}
	var declarations []useDecl
	for _, file := range sortedSources(sources) {
		if strings.HasSuffix(file, ".go") {
			continue
		}
		clean, _, err := scanModules(file, sources[file])
		if err != nil {
			return nil, err
		}
		_, ds, err := scanUses(file, clean)
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, ds...)
	}
	out := map[string]PathPreparation{}
	for _, file := range sortedSources(sources) {
		if strings.HasSuffix(file, ".go") {
			continue
		}
		plan, err := prepareSourcePaths(owner, file, sources[file], merged, localPackages, declarations)
		if err != nil {
			return nil, err
		}
		out[file] = plan
	}
	return out, nil
}

func prepareSourcePaths(owner, file string, source []byte, roots paths.Roots, localPackages map[string]bool, packageDeclarations []useDecl) (PathPreparation, error) {
	for _, t := range pathTokens(file, source) {
		if t.kind == token.IMPORT {
			return PathPreparation{}, fmt.Errorf("%s: .gox imports use dependency roots; replace import with use", t.pos)
		}
	}
	merged, err := LocalDeclarations(owner, map[string][]byte{strings.TrimSuffix(file, ".go") + ".gox": source}, roots, localPackages)
	if err != nil {
		return PathPreparation{}, err
	}
	clean, _, err := scanModules(file, source)
	if err != nil {
		return PathPreparation{}, err
	}
	clean, ds, err := scanUses(file, clean)
	if err != nil {
		return PathPreparation{}, err
	}
	all := packageDeclarations
	if all == nil {
		all = ds
	}
	resolved := make([]useDecl, len(ds))
	for i, d := range ds {
		index := i
		if packageDeclarations != nil {
			for j, other := range all {
				if other.file == d.file && other.pos.Offset == d.pos.Offset {
					index = j
					break
				}
			}
		}
		expanded, err := expandUseDeclaration(index, all, nil)
		if err != nil {
			return PathPreparation{}, fmt.Errorf("%s: %w", d.pos, err)
		}
		d.segments = expanded
		resolved[i] = d
	}
	ds = resolved
	plan := PathPreparation{Source: clean, Locals: paths.Roots{}, declarations: ds}
	for name, node := range merged {
		if node.Source == "local" {
			plan.Locals[name] = node
		}
	}
	imports := map[string]bool{}
	namespaces := paths.Roots{}
	for _, node := range plan.Locals {
		imports[node.PackagePath] = true
	}
	for name, node := range plan.Locals {
		namespaces[name] = node
	}
	for _, d := range ds {
		node, remaining, err := explicitNamespace(merged, d.segments)
		if err != nil {
			return plan, fmt.Errorf("%s: %w", d.pos, err)
		}
		if d.alias == "_" && (d.public || node.PackagePath == "") {
			return plan, fmt.Errorf("%s: side-effect use requires a concrete package and cannot be public", d.pos)
		}
		if node.PackagePath != "" {
			imports[node.PackagePath] = true
		}
		if len(remaining) == 0 && d.alias != "_" {
			namespaces[d.alias] = node
		}
	}
	for index, d := range packageDeclarations {
		if d.file == file || !d.public {
			continue
		}
		expanded, e := expandUseDeclaration(index, packageDeclarations, nil)
		if e != nil {
			return plan, e
		}
		node, remaining, e := explicitNamespace(merged, expanded)
		if e == nil && len(remaining) == 0 {
			namespaces[d.alias] = node
		}
	}
	ts := pathTokens(file, clean)
	for i := 0; i+3 < len(ts); i++ {
		if ts[i].kind != token.IDENT || ts[i+1].kind != token.COLON || ts[i+2].kind != token.COLON {
			continue
		}
		if i > 0 && ts[i-1].kind == token.PERIOD || i >= 2 && ts[i-1].kind == token.COLON && ts[i-2].kind == token.COLON {
			continue
		}
		if _, bound := namespaces[ts[i].text]; !bound {
			continue
		}
		selected := []string{ts[i].text}
		for j := i + 3; j < len(ts) && ts[j].kind == token.IDENT; j += 3 {
			selected = append(selected, ts[j].text)
			if j+2 >= len(ts) || ts[j+1].kind != token.COLON || ts[j+2].kind != token.COLON {
				break
			}
		}
		node, _, e := explicitNamespace(namespaces, selected)
		if e != nil {
			return plan, fmt.Errorf("%s: %w", ts[i].pos, e)
		}
		if node.PackagePath != "" {
			imports[node.PackagePath] = true
		}
	}
	for path := range imports {
		plan.Imports = append(plan.Imports, path)
	}
	sort.Strings(plan.Imports)
	return plan, nil
}

// An alias names its explicitly declared path. Resolving that declaration is
// independent of source order and never searches package directories.
func expandUseDeclaration(index int, declarations []useDecl, visiting map[int]bool) ([]string, error) {
	if visiting == nil {
		visiting = map[int]bool{}
	}
	if visiting[index] {
		return nil, fmt.Errorf("cyclic use alias %s", declarations[index].alias)
	}
	visiting[index] = true
	defer delete(visiting, index)
	d := declarations[index]
	for i, other := range declarations {
		if i == index || other.file != d.file && !other.public || other.alias == "_" || other.alias == "*" || other.alias != d.segments[0] {
			continue
		}
		// A root imported under its own name is the root declaration, rather
		// than a self-referential lexical alias.
		if len(other.segments) == 1 && other.segments[0] == other.alias {
			continue
		}
		prefix, err := expandUseDeclaration(i, declarations, visiting)
		if err != nil {
			return nil, err
		}
		return append(append([]string(nil), prefix...), d.segments[1:]...), nil
	}
	return append([]string(nil), d.segments...), nil
}
