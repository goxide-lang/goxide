package analysis

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type GoReference struct{ PackagePath, Name, Receiver string }
type Location struct {
	File                       string
	Span                       syntax.Span
	Generated                  bool
	MacroDefinition, MacroCall *Location
	Go                         *GoReference
}
type Hover struct {
	Text  string
	Range Location
}
type Completion struct{ Name, Detail, Kind string }
type DocumentSymbol struct {
	Name, Detail, Kind string
	Range, Selection   Location
	Children           []DocumentSymbol
}
type QueryUnavailable struct {
	Reason string
	Go     bool
}

func (e *QueryUnavailable) Error() string { return "query unavailable: " + e.Reason }

type queryEntry struct {
	location    Location
	symbol      *resolve.Symbol
	typ         types.Type
	member      *check.MemberSelection
	goRef       *GoReference
	unavailable string
	definition  *Location
	text        string
	deferred    bool
}
type queryScope struct {
	location Location
	scope    *resolve.Scope
}
type queryCompletion struct {
	span        syntax.Span
	items       []Completion
	unavailable string
}
type Queries struct {
	result               *Result
	entries              map[string][]queryEntry
	scopes               map[string][]queryScope
	nodes                map[resolve.Ref]syntax.Node
	names                map[*syntax.File]string
	blocked              map[string]string
	protocolsUnavailable bool
	members              map[string][]queryCompletion
	targets              map[string]*resolve.Symbol
}

// NewQueries builds source lookup indices once for one published analysis.
// Queries never parse sources, run a checker or call an external importer.
func NewQueries(result *Result) *Queries {
	q := &Queries{result: result, entries: map[string][]queryEntry{}, scopes: map[string][]queryScope{}, nodes: map[resolve.Ref]syntax.Node{}, names: map[*syntax.File]string{}, blocked: map[string]string{}, members: map[string][]queryCompletion{}, targets: map[string]*resolve.Symbol{}}
	if result == nil {
		return q
	}
	for name, file := range result.Files {
		q.names[file] = name
	}
	if result.Resolved == nil {
		return q
	}
	for _, region := range result.Regions {
		if region.Kind == UnknownDeclarations {
			q.blocked[sourcePackage(region.File)] = region.Reason
		}
		q.protocolsUnavailable = q.protocolsUnavailable || region.Kind == UnknownProtocol
	}
	for _, pkg := range result.Resolved.Packages {
		if pkg.IsExternalTest() {
			continue
		}
		q.indexScopeTargets(pkg.Dir, pkg.Scope, map[*resolve.Scope]bool{})
		if result.Checked != nil && result.Checked.PackagePaths[pkg.Dir] != "" {
			q.indexScopeTargets(result.Checked.PackagePaths[pkg.Dir], pkg.Scope, map[*resolve.Scope]bool{})
		}
	}
	for changed := true; changed; {
		changed = false
		for name, file := range result.Resolved.Files {
			if q.blocked[sourcePackage(name)] != "" {
				continue
			}
			for _, use := range file.Uses {
				for blocked := range q.blocked {
					target := strings.Join(use.Target, "/")
					if target == blocked || strings.HasPrefix(target, blocked+"/") {
						q.blocked[sourcePackage(name)] = "an imported declaration set is incomplete"
						changed = true
						break
					}
				}
			}
		}
	}
	for name, file := range result.Resolved.Files {
		q.names[file.AST] = name
		syntax.Walk(file.AST, func(node syntax.Node) bool { q.nodes[resolve.Ref{File: name, ID: node.ID()}] = node; return true })
	}
	for name, file := range result.Resolved.Files {
		fieldKeys := map[*syntax.Ident]bool{}
		syntax.Walk(file.AST, func(node syntax.Node) bool {
			if literal, ok := node.(*syntax.CompositeLit); ok {
				q.indexCompositeKeys(name, literal, fieldKeys)
			}
			if key, ok := node.(*syntax.Ident); ok && fieldKeys[key] {
				return true
			}
			location := q.location(name, node)
			if scope := file.Scopes[node.ID()]; scope != nil {
				q.scopes[location.File] = append(q.scopes[location.File], queryScope{location, scope})
			}
			symbol := file.Definitions[node.ID()]
			deferred := false
			if symbol == nil {
				reference := file.References[node.ID()]
				symbol, deferred = reference.Symbol, reference.Deferred
			}
			// Declaration tables also index the whole declaration. A cursor in
			// its body must never fall back to the function's name or signature.
			if _, identifier := node.(*syntax.Ident); !identifier {
				if _, expression := node.(syntax.Expr); !expression {
					symbol = nil
				}
			}
			var typ types.Type
			var member *check.MemberSelection
			if result.Checked != nil {
				typ = q.symbolType(symbol)
				if expression, ok := node.(syntax.Expr); ok {
					if t := result.Checked.Types[expression]; t != nil {
						typ = t
					}
					member = result.Checked.Members[expression]
				}
			}
			var goRef *GoReference
			unavailable := ""
			if member != nil {
				switch node := node.(type) {
				case *syntax.SelectorExpr:
					location = q.location(name, node.Sel)
				case *syntax.QualifiedSelectorExpr:
					location = q.location(name, node.Sel)
				}
				typ = member.Signature
				if q.protocolsUnavailable && member.Member.Native == nil {
					unavailable = "protocol selection is not reliable"
				}
			} else if selector, ok := node.(*syntax.SelectorExpr); ok {
				location = q.location(name, selector.Sel)
				symbol = nil
				if result.Checked != nil && typ != nil && typ != types.Typ[types.Invalid] {
					symbol, goRef = q.goMember(result.Checked.Types[selector.X], selector.Sel.Name, name)
					deferred = symbol == nil
				} else {
					unavailable = "member selection is not available in the current source"
				}
			}
			if symbol != nil || typ != nil || member != nil || unavailable != "" {
				q.entries[location.File] = append(q.entries[location.File], queryEntry{location: location, symbol: symbol, typ: typ, member: member, goRef: goRef, unavailable: unavailable, deferred: deferred})
			}
			return true
		})
	}
	q.indexMacroCalls()
	for name := range q.entries {
		sort.SliceStable(q.entries[name], func(i, j int) bool {
			a, b := q.entries[name][i].location.Span, q.entries[name][j].location.Span
			if a.Start != b.Start {
				return a.Start < b.Start
			}
			return a.End < b.End
		})
	}
	q.indexMemberCompletions()
	return q
}

func (r *Result) ExternalImporter() types.Importer {
	if r == nil || r.state == nil {
		return nil
	}
	return r.state.external
}

func (q *Queries) location(name string, node syntax.Node) Location {
	location := Location{File: name, Span: node.Span()}
	if q.result == nil || q.result.Expanded == nil {
		return location
	}
	if origin, ok := q.result.Expanded.Origins[node]; ok && q.names[origin.File] != "" {
		location.File, location.Span = q.names[origin.File], origin.Span
	}
	value := reflect.ValueOf(node)
	var expansion syntax.ExpnID
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		if field := value.Elem().FieldByName("Expn"); field.IsValid() {
			expansion = syntax.ExpnID(field.Uint())
		}
	}
	if frame, ok := q.result.Expanded.Expansions[expansion]; ok {
		location.Generated = true
		if name := q.names[frame.Definition.File]; name != "" {
			location.MacroDefinition = &Location{File: name, Span: frame.Definition.Span}
		}
		if name := q.names[frame.Call.File]; name != "" {
			location.MacroCall = &Location{File: name, Span: frame.Call.Span}
		}
	}
	return location
}
func (q *Queries) entry(file string, offset uint32) (*queryEntry, error) {
	if q.result == nil || q.result.Resolved == nil {
		return nil, &QueryUnavailable{Reason: "source bindings are not available"}
	}
	if reason := q.blocked[sourcePackage(file)]; reason != "" {
		return nil, &QueryUnavailable{Reason: reason}
	}
	for _, region := range q.result.Regions {
		if region.File == file && region.Kind != IncompleteBody && region.Kind != UnknownProtocol && region.Span.Start <= offset && offset < region.Span.End {
			return nil, &QueryUnavailable{Reason: region.Reason}
		}
	}
	var best *queryEntry
	for i := range q.entries[file] {
		entry := &q.entries[file][i]
		span := entry.location.Span
		if span.Start <= offset && offset < span.End && (best == nil || span.End-span.Start < best.location.Span.End-best.location.Span.Start) {
			best = entry
		}
	}
	if best != nil {
		for _, entry := range q.entries[file] {
			if entry.location.Span == best.location.Span && entry.symbol != nil && best.symbol != nil && entry.symbol != best.symbol && entry.symbol.AliasOf != best.symbol && best.symbol.AliasOf != entry.symbol {
				return nil, &QueryUnavailable{Reason: "this source range has several expanded bindings"}
			}
		}
	}
	if best != nil && best.unavailable != "" {
		return nil, &QueryUnavailable{Reason: best.unavailable}
	}
	return best, nil
}
func (q *Queries) Definition(file string, offset uint32) ([]Location, error) {
	entry, err := q.entry(file, offset)
	if entry == nil || err != nil {
		return nil, err
	}
	if entry.goRef != nil {
		return []Location{{Go: entry.goRef}}, nil
	}
	if entry.definition != nil {
		return []Location{*entry.definition}, nil
	}
	if entry.member != nil {
		member := entry.member.Member
		if member.Decl != nil && member.Decl.Name != nil {
			return []Location{q.location(member.File, member.Decl.Name)}, nil
		}
		if member.Native != nil {
			reference := &GoReference{Name: member.Native.Name()}
			if pkg := member.Native.Pkg(); pkg != nil {
				reference.PackagePath = pkg.Path()
			}
			if signature, ok := member.Native.Type().(*types.Signature); ok && signature.Recv() != nil {
				reference.Receiver = types.TypeString(signature.Recv().Type(), nil)
			}
			return []Location{{Go: reference}}, nil
		}
	}
	symbol := entry.symbol
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
		break
	}
	if symbol == nil {
		return nil, &QueryUnavailable{Reason: "no unique declaration is selected"}
	}
	if node := q.nodes[symbol.Decl]; node != nil {
		return []Location{q.location(symbol.Decl.File, node)}, nil
	}
	if len(symbol.Target) > 1 {
		return []Location{{Go: &GoReference{PackagePath: strings.Join(symbol.Target[:len(symbol.Target)-1], "/"), Name: symbol.Target[len(symbol.Target)-1]}}}, nil
	}
	return nil, nil
}

func (q *Queries) indexScopeTargets(prefix string, scope *resolve.Scope, visiting map[*resolve.Scope]bool) {
	if scope == nil || visiting[scope] {
		return
	}
	visiting[scope] = true
	defer delete(visiting, scope)
	for key, symbol := range scope.Bindings {
		if key.Ctx != 0 || key.Namespace != resolve.OrdinaryNamespace {
			continue
		}
		target := prefix + "/" + key.Name
		q.targets[target] = symbol
		q.indexScopeTargets(target, symbol.Members, visiting)
	}
}
func (q *Queries) Hover(file string, offset uint32) (*Hover, error) {
	entry, err := q.entry(file, offset)
	if entry == nil || err != nil {
		return nil, err
	}
	text := q.detail(entry.symbol, entry.typ)
	if entry.text != "" {
		text = entry.text
	}
	if entry.member != nil {
		text = "func " + entry.member.Member.Name + strings.TrimPrefix(types.TypeString(entry.member.Signature, nil), "func")
	}
	if text == "" {
		return nil, &QueryUnavailable{Reason: "current type information is not available"}
	}
	return &Hover{Text: text, Range: entry.location}, nil
}

// Imported aliases and test-package views can name a source declaration without
// owning another entry in the checker's symbol table. Follow only current
// resolved identities; never import or reconstruct a signature during a query.
func (q *Queries) symbolType(symbol *resolve.Symbol) types.Type {
	if q.result == nil || q.result.Checked == nil {
		return nil
	}
	seen := map[*resolve.Symbol]bool{}
	for symbol != nil && !seen[symbol] {
		seen[symbol] = true
		if typ := q.result.Checked.Symbols[symbol]; typ != nil {
			return typ
		}
		if symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		} else {
			symbol = q.targets[strings.Join(symbol.Target, "/")]
		}
	}
	return nil
}

func (q *Queries) detail(symbol *resolve.Symbol, typ types.Type) string {
	if typ != nil && !reliableType(typ, map[types.Type]bool{}) {
		return ""
	}
	if typ == nil {
		if symbol != nil {
			return kindName(symbol.Kind) + " " + symbol.Name.Name
		}
		return ""
	}
	text := types.TypeString(typ, func(pkg *types.Package) string { return pkg.Name() })
	if symbol == nil {
		return text
	}
	if _, function := typ.(*types.Signature); function {
		return "func " + symbol.Name.Name + strings.TrimPrefix(text, "func")
	}
	return kindName(symbol.Kind) + " " + symbol.Name.Name + " " + text
}

func reliableType(typ types.Type, seen map[types.Type]bool) bool {
	if typ == nil || typ == types.Typ[types.Invalid] {
		return false
	}
	if seen[typ] {
		return true
	}
	seen[typ] = true
	switch typ := typ.(type) {
	case *types.Alias:
		return reliableType(types.Unalias(typ), seen)
	case *types.Named:
		for i := 0; i < typ.TypeArgs().Len(); i++ {
			if !reliableType(typ.TypeArgs().At(i), seen) {
				return false
			}
		}
		return reliableType(typ.Underlying(), seen)
	case *types.Pointer:
		return reliableType(typ.Elem(), seen)
	case *types.Slice:
		return reliableType(typ.Elem(), seen)
	case *types.Array:
		return reliableType(typ.Elem(), seen)
	case *types.Map:
		return reliableType(typ.Key(), seen) && reliableType(typ.Elem(), seen)
	case *types.Chan:
		return reliableType(typ.Elem(), seen)
	case *types.Signature:
		for i := 0; i < typ.TypeParams().Len(); i++ {
			if !reliableType(typ.TypeParams().At(i), seen) {
				return false
			}
		}
		return reliableType(typ.Params(), seen) && reliableType(typ.Results(), seen)
	case *types.TypeParam:
		return reliableType(typ.Constraint(), seen)
	case *types.Interface:
		for i := 0; i < typ.NumEmbeddeds(); i++ {
			if !reliableType(typ.EmbeddedType(i), seen) {
				return false
			}
		}
		for i := 0; i < typ.NumExplicitMethods(); i++ {
			if !reliableType(typ.ExplicitMethod(i).Type(), seen) {
				return false
			}
		}
	case *types.Union:
		for i := 0; i < typ.Len(); i++ {
			if !reliableType(typ.Term(i).Type(), seen) {
				return false
			}
		}
	case *types.Tuple:
		for i := 0; i < typ.Len(); i++ {
			if !reliableType(typ.At(i).Type(), seen) {
				return false
			}
		}
	case *types.Struct:
		for i := 0; i < typ.NumFields(); i++ {
			if !reliableType(typ.Field(i).Type(), seen) {
				return false
			}
		}
	}
	return true
}

func (q *Queries) goMember(receiver types.Type, name, file string) (*resolve.Symbol, *GoReference) {
	if receiver == nil {
		return nil, nil
	}
	base := types.Unalias(receiver)
	if pointer, ok := base.(*types.Pointer); ok {
		base = types.Unalias(pointer.Elem())
	}
	var pkg *types.Package
	if named, ok := base.(*types.Named); ok && named.Obj().Pkg() != nil && (named.Obj().Pkg().Path() == sourcePackage(file) || named.Obj().Pkg().Path() == q.result.Checked.PackagePaths[sourcePackage(file)]) {
		pkg = named.Obj().Pkg()
	}
	object, _, _ := types.LookupFieldOrMethod(receiver, true, pkg, name)
	if object == nil {
		return nil, nil
	}
	for symbol, typ := range q.result.Checked.Symbols {
		if typ != nil && symbol.Members != nil && types.Identical(types.Unalias(typ), base) {
			if member := symbol.Members.Bindings[resolve.Key{Name: name}]; member != nil {
				return member, nil
			}
		}
	}
	reference := &GoReference{Name: name}
	if object.Pkg() != nil {
		reference.PackagePath = object.Pkg().Path()
	}
	if signature, ok := object.Type().(*types.Signature); ok && signature.Recv() != nil {
		reference.Receiver = types.TypeString(signature.Recv().Type(), nil)
	}
	return nil, reference
}
func (q *Queries) Complete(file string, offset uint32) ([]Completion, error) {
	if q.result == nil || q.result.Resolved == nil {
		return nil, &QueryUnavailable{Reason: "source scopes are not available"}
	}
	if reason := q.blocked[sourcePackage(file)]; reason != "" {
		return nil, &QueryUnavailable{Reason: reason}
	}
	for _, member := range q.members[file] {
		if member.span.Start <= offset && offset <= member.span.End {
			if member.unavailable != "" {
				return nil, &QueryUnavailable{Reason: member.unavailable}
			}
			return append([]Completion(nil), member.items...), nil
		}
	}
	selected := q.scopeAt(file, offset)
	if selected == nil {
		return nil, nil
	}
	return q.lexicalCompletions(file, offset, selected), nil
}

func (q *Queries) scopeAt(file string, offset uint32) *queryScope {
	var selected *queryScope
	for i := range q.scopes[file] {
		scope := &q.scopes[file][i]
		span := scope.location.Span
		if span.Start <= offset && offset <= span.End && (selected == nil || span.End-span.Start < selected.location.Span.End-selected.location.Span.Start) {
			selected = scope
		}
	}
	return selected
}

func (q *Queries) lexicalCompletions(file string, offset uint32, selected *queryScope) []Completion {
	seen := map[string]bool{}
	var result []Completion
	add := func(symbol *resolve.Symbol, scope *resolve.Scope) {
		if symbol == nil || symbol.Name.Ctx != 0 || symbol.Name.Name == "_" || seen[symbol.Name.Name] {
			return
		}
		if node := q.nodes[symbol.Decl]; scope.Kind != resolve.PackageScope && scope.Kind != resolve.FileScope && node != nil && symbol.Decl.File == file && (symbol.Kind == resolve.Variable || symbol.Kind == resolve.Label) && node.Span().Start > offset {
			return
		}
		seen[symbol.Name.Name] = true
		var typ types.Type
		if q.result.Checked != nil {
			typ = q.symbolType(symbol)
		}
		detail := q.detail(symbol, typ)
		if detail == "" {
			return
		}
		result = append(result, Completion{Name: symbol.Name.Name, Detail: detail, Kind: kindName(symbol.Kind)})
	}
	visited := map[*resolve.Scope]bool{}
	for scope := selected.scope; scope != nil && !visited[scope]; scope = scope.Parent {
		visited[scope] = true
		for _, symbol := range scope.Bindings {
			add(symbol, scope)
		}
		for _, aliases := range scope.Aliases {
			if len(aliases) == 1 {
				add(aliases[0], scope)
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
func (q *Queries) Symbols(file string) ([]DocumentSymbol, error) {
	if q.result == nil {
		return nil, &QueryUnavailable{Reason: "source syntax is not available"}
	}
	var tree *syntax.File
	if source := q.result.Sources[file]; source != nil {
		tree = source.Syntax
	}
	if tree == nil {
		tree = q.result.Files[file]
	}
	if tree == nil {
		return nil, &QueryUnavailable{Reason: "source syntax is not available"}
	}
	return documentSymbols(file, tree.Items), nil
}

func kindName(kind resolve.Kind) string {
	names := []string{"variable", "constant", "function", "type", "type-parameter", "constant-parameter", "enum", "trait", "variant", "module", "macro", "import", "field", "label", "builtin"}
	if int(kind) < len(names) {
		return names[kind]
	}
	return fmt.Sprint(kind)
}
