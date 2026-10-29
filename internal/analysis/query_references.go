package analysis

import (
	"fmt"
	gotoken "go/token"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type References struct {
	Definition Location
	Locations  []Location
	Name       string
	GoName     string
	Local      bool
	NeedsGo    bool
}

type referenceIdentity struct {
	definition Location
	symbol     *resolve.Symbol
}

func sameLocation(a, b Location) bool { return a.File == b.File && a.Span == b.Span }

func (q *Queries) canonicalSymbol(symbol *resolve.Symbol) *resolve.Symbol {
	seen := map[*resolve.Symbol]bool{}
	for symbol != nil && !seen[symbol] {
		seen[symbol] = true
		if symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		} else if next := q.targets[strings.Join(symbol.Target, "/")]; next != nil && next != symbol {
			symbol = next
		} else {
			break
		}
	}
	return symbol
}

func (q *Queries) explicitAlias(symbol *resolve.Symbol) bool {
	if symbol == nil || symbol.Kind != resolve.Import {
		return false
	}
	file := q.result.Sources[symbol.Decl.File]
	node := q.nodes[symbol.Decl]
	if file == nil || file.Syntax == nil || node == nil {
		return false
	}
	span := q.location(symbol.Decl.File, node).Span
	explicit := false
	syntax.Walk(file.Syntax, func(node syntax.Node) bool {
		if tree, ok := node.(*syntax.UseTree); ok && tree.Alias != nil && tree.Alias.Span() == span {
			explicit = true
		}
		return !explicit
	})
	return explicit
}

func (q *Queries) referenceIdentity(entry queryEntry, aliases bool) referenceIdentity {
	if entry.deferred && entry.member == nil && q.canonicalSymbol(entry.symbol) == entry.symbol {
		return referenceIdentity{}
	}
	if entry.definition != nil {
		return referenceIdentity{definition: *entry.definition}
	}
	if entry.member != nil && entry.member.Member.Decl != nil {
		member := entry.member.Member
		if family := q.protocolMethod(member.Symbol); family != nil {
			member = family
		}
		return referenceIdentity{definition: q.location(member.File, member.Decl.Name), symbol: member.Symbol}
	}
	symbol := entry.symbol
	if aliases {
		if file := q.result.Resolved.Files[entry.location.File]; file != nil {
			for _, use := range file.Uses {
				if q.explicitAlias(use.Symbol) && use.Symbol.Name.Name == q.sourceText(entry.location) && q.canonicalSymbol(use.Symbol) == q.canonicalSymbol(symbol) {
					symbol = use.Symbol
					break
				}
			}
		}
	}
	if !aliases || !q.explicitAlias(symbol) {
		symbol = q.canonicalSymbol(symbol)
	}
	if symbol != nil {
		if member := q.protocolMethod(symbol); member != nil {
			return referenceIdentity{definition: q.location(member.File, member.Decl.Name), symbol: member.Symbol}
		}
		if node := q.nodes[symbol.Decl]; node != nil {
			return referenceIdentity{definition: q.location(symbol.Decl.File, node), symbol: symbol}
		}
	}
	return referenceIdentity{}
}

func (q *Queries) protocolMethod(symbol *resolve.Symbol) *check.Member {
	if symbol == nil || q.result.Checked == nil {
		return nil
	}
	for _, trait := range q.result.Checked.Traits {
		for _, member := range trait.Methods {
			if member.Symbol == symbol {
				return member
			}
		}
	}
	for _, impl := range q.result.Checked.Impls {
		if impl.Trait == nil {
			continue
		}
		for _, member := range impl.Methods {
			if member.Symbol != symbol {
				continue
			}
			for _, requirement := range impl.Trait.Methods {
				if requirement.Name == member.Name {
					return requirement
				}
			}
		}
	}
	return nil
}

func (q *Queries) sourceText(location Location) string {
	input := q.result.Sources[location.File]
	span := location.Span
	if input == nil || span.End < span.Start || int(span.End) > len(input.Content) {
		return ""
	}
	return string(input.Content[span.Start:span.End])
}

// FindReferences follows resolved identities, including source package imports.
// Native Go body references are supplied by the existing Go language backend.
func (q *Queries) FindReferences(file string, offset uint32, includeDeclaration bool) (*References, error) {
	return q.findReferences(file, offset, includeDeclaration, false)
}

func (q *Queries) findReferences(file string, offset uint32, declaration, aliases bool) (*References, error) {
	entry, err := q.entry(file, offset)
	if err != nil || entry == nil {
		return nil, err
	}
	identity := q.referenceIdentity(*entry, aliases)
	if identity.definition.File == "" {
		return nil, &QueryUnavailable{Reason: "references require a source declaration or the Go language service", Go: entry.goRef != nil || entry.member != nil && entry.member.Member.Native != nil}
	}
	answer := &References{Definition: identity.definition, Name: q.sourceText(identity.definition)}
	if identity.symbol != nil {
		answer.GoName = identity.symbol.GoName
		answer.Local = q.explicitAlias(identity.symbol) || q.localSymbol(identity.symbol)
		answer.NeedsGo = identity.symbol.Kind == resolve.Field
	}
	seen := map[string]map[syntax.Span]bool{}
	add := func(location Location) {
		if !gotoken.IsIdentifier(strings.TrimPrefix(q.sourceText(location), "r#")) || !declaration && sameLocation(location, identity.definition) {
			return
		}
		if seen[location.File] == nil {
			seen[location.File] = map[syntax.Span]bool{}
		}
		if !seen[location.File][location.Span] {
			seen[location.File][location.Span] = true
			answer.Locations = append(answer.Locations, location)
		}
	}
	if declaration {
		add(identity.definition)
	}
	for _, entries := range q.entries {
		for _, candidate := range entries {
			if sameLocation(q.referenceIdentity(candidate, aliases).definition, identity.definition) {
				add(candidate.location)
			}
		}
	}
	// An explicit import alias has its own declaration span. The imported path
	// still refers to the original declaration even though the resolver stores
	// its binding on the alias token.
	if !aliases || !q.explicitAlias(identity.symbol) {
		for name, file := range q.result.Resolved.Files {
			for _, use := range file.Uses {
				target := q.canonicalSymbol(use.Symbol)
				if target == nil || q.nodes[target.Decl] == nil || !sameLocation(q.location(target.Decl.File, q.nodes[target.Decl]), identity.definition) {
					continue
				}
				if node, ok := q.nodes[use.Decl].(*syntax.UseDecl); ok && node.Tree != nil && node.Tree.Path != nil {
					segments := node.Tree.Path.Segments
					if len(segments) != 0 && segments[len(segments)-1].Name != nil {
						add(q.location(name, segments[len(segments)-1].Name))
					}
				}
			}
		}
	}
	sort.Slice(answer.Locations, func(i, j int) bool {
		a, b := answer.Locations[i], answer.Locations[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Span.Start < b.Span.Start
	})
	return answer, nil
}

func (q *Queries) localSymbol(symbol *resolve.Symbol) bool {
	for _, scopes := range q.scopes {
		for _, item := range scopes {
			for scope := item.scope; scope != nil; scope = scope.Parent {
				if scope.Bindings[symbol.Name] == symbol {
					return scope.Kind != resolve.PackageScope && scope.Kind != resolve.MemberScope && scope.Kind != resolve.PreludeScope
				}
			}
		}
	}
	return false
}

func ValidRename(name string) bool {
	return gotoken.IsIdentifier(name) && name != "_" && name != "self" && !token.IsHardReserved(name)
}

// GoNameAfterRename uses the declaration's written visibility, including pub.
func (q *Queries) GoNameAfterRename(file string, offset uint32, name string) (string, error) {
	entry, err := q.entry(file, offset)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", &QueryUnavailable{Reason: "rename has no source binding"}
	}
	identity := q.referenceIdentity(*entry, true)
	if identity.symbol == nil || identity.definition.Generated {
		return "", &QueryUnavailable{Reason: "rename has no authored source declaration"}
	}
	goName, _, err := q.renamedDeclaration(identity.symbol, name)
	return goName, err
}

func (q *Queries) PrepareRename(file string, offset uint32) (*References, error) {
	refs, err := q.findReferences(file, offset, true, true)
	if refs == nil || err != nil {
		return refs, err
	}
	if refs.Definition.Generated || !ValidRename(refs.Name) {
		return nil, &QueryUnavailable{Reason: "this declaration does not have an editable source identifier"}
	}
	entry, _ := q.entry(file, offset)
	identity := q.referenceIdentity(*entry, true)
	if identity.symbol == nil || identity.symbol.Kind == resolve.Builtin || identity.symbol.Kind == resolve.Macro || identity.symbol.Kind == resolve.Module {
		return nil, &QueryUnavailable{Reason: "this declaration requires a structural rename"}
	}
	if q.result.Checked != nil {
		for _, trait := range q.result.Checked.Traits {
			for _, associated := range trait.Associated {
				if sameLocation(q.location(trait.File, associated.Decl.Name), refs.Definition) {
					return nil, &QueryUnavailable{Reason: "associated types require a protocol-wide structural rename"}
				}
			}
		}
		for _, impl := range q.result.Checked.Impls {
			if impl.Trait == nil {
				continue
			}
			for _, item := range impl.Decl.Items {
				if associated, ok := item.(*syntax.TypeDecl); ok && associated.Name != nil && sameLocation(q.location(impl.File, associated.Name), refs.Definition) {
					return nil, &QueryUnavailable{Reason: "associated types require a protocol-wide structural rename"}
				}
			}
		}
	}
	for _, region := range q.result.Regions {
		if !refs.Local || region.File == refs.Definition.File {
			return nil, &QueryUnavailable{Reason: "rename requires complete bindings in every affected source file"}
		}
	}
	for _, location := range refs.Locations {
		if location.Generated {
			return nil, &QueryUnavailable{Reason: "rename crosses a macro expansion"}
		}
	}
	return refs, nil
}

func (q *Queries) Rename(file string, offset uint32, newName string) ([]SourceEdit, error) {
	if !ValidRename(newName) {
		return nil, fmt.Errorf("%q is not an editable Goxide identifier", newName)
	}
	refs, err := q.PrepareRename(file, offset)
	if refs == nil || err != nil {
		return nil, err
	}
	if newName == refs.Name {
		return []SourceEdit{}, nil
	}
	entry, _ := q.entry(file, offset)
	symbol := q.referenceIdentity(*entry, true).symbol
	if err := q.validateRenameName(symbol, refs, newName); err != nil {
		return nil, err
	}
	var edits []SourceEdit
	for _, location := range refs.Locations {
		if q.sourceText(location) != refs.Name {
			continue // An explicit import alias keeps its local spelling.
		}
		if selected := q.scopeAt(location.File, location.Span.Start); selected != nil {
			for scope := selected.scope; scope != nil; scope = scope.Parent {
				key := resolve.Key{Name: newName}
				if scope.Bindings[key] != nil || len(scope.Aliases[key]) != 0 {
					return nil, fmt.Errorf("renaming %s to %s would conflict with an existing binding", refs.Name, newName)
				}
			}
		}
		edits = append(edits, SourceEdit{File: location.File, Span: location.Span, NewText: newName})
	}
	// A new local name must not capture a previously unqualified outer name.
	for name, entries := range q.entries {
		for _, candidate := range entries {
			if q.sourceText(candidate.location) != newName {
				continue
			}
			if selected := q.scopeAt(name, candidate.location.Span.Start); selected != nil {
				for scope := selected.scope; scope != nil; scope = scope.Parent {
					if scope.Bindings[symbol.Name] == symbol {
						return nil, fmt.Errorf("renaming %s to %s would capture another source reference", refs.Name, newName)
					}
				}
			}
		}
	}
	return edits, nil
}

// ValidateDeclarationRenames checks authored declarations changed by a backend
// operation, including native Go declarations whose body references only gopls
// can see. Go type checking alone does not require a main entry point or prevent
// turning an otherwise unused function into an automatically executed init.
func (q *Queries) ValidateDeclarationRenames(edits []SourceEdit) error {
	for _, edit := range edits {
		entry, err := q.entry(edit.File, edit.Span.Start)
		if err != nil || entry == nil {
			continue // Native Go body-local symbols are owned by gopls.
		}
		identity := q.referenceIdentity(*entry, true)
		if identity.symbol == nil || identity.definition.File != edit.File || identity.definition.Span != edit.Span || q.sourceText(identity.definition) == edit.NewText {
			continue
		}
		refs, err := q.findReferences(edit.File, edit.Span.Start, true, true)
		if err != nil {
			return err
		}
		if refs != nil {
			if err := q.validateRenameName(identity.symbol, refs, edit.NewText); err != nil {
				return err
			}
		}
	}
	return nil
}
