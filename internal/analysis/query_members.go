package analysis

import (
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// The completion index contains only members certified in this analysis. It
// does not instantiate impls or resolve new protocol obligations on a request.
func (q *Queries) indexMemberCompletions() {
	if q.result.Checked == nil {
		return
	}
	for name, source := range q.result.Sources {
		if source.Syntax == nil {
			continue
		}
		syntax.Walk(source.Syntax, func(node syntax.Node) bool {
			if path, ok := node.(*syntax.Path); ok && len(path.Segments) >= 2 {
				last := path.Segments[len(path.Segments)-1]
				if last.Name == nil {
					return true
				}
				completion := queryCompletion{span: syntax.Span{Start: last.Name.Span().Start, End: last.Span().End}}
				completion.items = q.staticCompletions(name, path)
				if completion.items == nil {
					completion.unavailable = "namespace members are not available in the current source"
				}
				q.members[name] = append(q.members[name], completion)
			}
			selector, ok := node.(*syntax.SelectorExpr)
			if !ok || selector.Sel == nil {
				return true
			}
			completion := queryCompletion{span: syntax.Span{Start: selector.X.Span().End + 1, End: selector.Span().End}}
			var receiver types.Type
			for _, entry := range q.entries[name] {
				if entry.location.Span == selector.X.Span() && entry.typ != nil {
					receiver = entry.typ
					break
				}
			}
			if receiver == nil || !reliableType(receiver, map[types.Type]bool{}) {
				completion.unavailable = "receiver type is not available in the current source"
			} else {
				completion.items = q.memberCompletions(name, receiver)
			}
			q.members[name] = append(q.members[name], completion)
			return true
		})
	}
}

func (q *Queries) staticCompletions(file string, path *syntax.Path) []Completion {
	selected := q.scopeAt(file, path.Span().Start)
	if selected == nil {
		return nil
	}
	var symbol *resolve.Symbol
	for scope := selected.scope; scope != nil; scope = scope.Parent {
		key := resolve.Key{Name: path.Segments[0].Name.Name}
		if symbol = scope.Bindings[key]; symbol != nil {
			break
		}
		if aliases := scope.Aliases[key]; len(aliases) == 1 {
			symbol = aliases[0]
			break
		} else if len(aliases) > 1 {
			return nil
		}
	}
	var members *resolve.Scope
	for _, segment := range path.Segments[1:] {
		if symbol == nil {
			return nil
		}
		if symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		members = symbol.Members
		if members == nil && len(symbol.Target) > 0 {
			target := strings.Join(symbol.Target, "/")
			if local := q.targets[target]; local != nil {
				members = local.Members
			}
			if members == nil {
				for _, pkg := range q.result.Resolved.Packages {
					if pkg.Dir == target || q.result.Checked.PackagePaths[pkg.Dir] == target {
						members = pkg.Scope
						break
					}
				}
			}
		}
		if segment == path.Segments[len(path.Segments)-1] {
			break
		}
		if members == nil {
			return nil
		}
		symbol = members.Bindings[resolve.Key{Name: segment.Name.Name}]
	}
	if members == nil {
		return nil
	}
	result := []Completion{}
	for _, member := range members.Bindings {
		if member.Name.Ctx != 0 || member.Visibility != resolve.Public && sourcePackage(member.Decl.File) != sourcePackage(file) {
			continue
		}
		if reason := q.blocked[sourcePackage(member.Decl.File)]; reason != "" {
			continue
		}
		typ := q.result.Checked.Symbols[member]
		detail := q.detail(member, typ)
		if detail != "" {
			result = append(result, Completion{Name: member.Name.Name, Kind: kindName(member.Kind), Detail: detail})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (q *Queries) memberCompletions(file string, receiver types.Type) []Completion {
	base := types.Unalias(receiver)
	if pointer, ok := base.(*types.Pointer); ok {
		base = types.Unalias(pointer.Elem())
	}
	var pkg *types.Package
	var sourceType *resolve.Symbol
	for symbol, typ := range q.result.Checked.Symbols {
		if typ != nil && symbol.Members != nil && types.Identical(types.Unalias(typ), base) {
			sourceType = symbol
			break
		}
	}
	if named, ok := base.(*types.Named); ok && named.Obj().Pkg() != nil && (named.Obj().Pkg().Path() == sourcePackage(file) || named.Obj().Pkg().Path() == q.result.Checked.PackagePaths[sourcePackage(file)]) {
		pkg = named.Obj().Pkg()
	}
	candidates := map[string]Completion{}
	addObject := func(object types.Object, kind string) {
		if object == nil {
			return
		}
		if sourceType != nil {
			if member := sourceType.Members.Bindings[resolve.Key{Name: object.Name()}]; member != nil && member.Visibility != resolve.Public && sourcePackage(member.Decl.File) != sourcePackage(file) {
				return
			}
		} else if !object.Exported() && object.Pkg() != pkg {
			return
		}
		if selected, _, _ := types.LookupFieldOrMethod(receiver, true, pkg, object.Name()); selected == nil {
			return
		}
		candidates[object.Name()] = Completion{Name: object.Name(), Kind: kind, Detail: types.TypeString(object.Type(), func(pkg *types.Package) string { return pkg.Name() })}
	}
	if structure, ok := base.Underlying().(*types.Struct); ok {
		for i := 0; i < structure.NumFields(); i++ {
			addObject(structure.Field(i), "field")
		}
	}
	for _, typ := range []types.Type{receiver, types.NewPointer(base)} {
		methods := types.NewMethodSet(typ)
		for i := 0; i < methods.Len(); i++ {
			addObject(methods.At(i).Obj(), "method")
		}
	}
	if !q.protocolsUnavailable {
		// Inherent implementations with an already concrete target are known
		// without choosing a trait or satisfying a fresh generic obligation.
		for _, implementation := range q.result.Checked.Impls {
			if implementation.Trait != nil || len(implementation.TypeParams) != 0 || !types.Identical(implementation.Target, base) {
				continue
			}
			for _, member := range implementation.Methods {
				if member.Static || member.Symbol != nil && member.Symbol.Visibility != resolve.Public && sourcePackage(member.File) != sourcePackage(file) {
					continue
				}
				candidates[member.Name] = Completion{Name: member.Name, Kind: "method", Detail: types.TypeString(member.Signature, func(pkg *types.Package) string { return pkg.Name() })}
			}
		}
	}
	result := make([]Completion, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
