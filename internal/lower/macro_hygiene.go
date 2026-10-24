package lower

import (
	"crypto/sha256"
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Names are chosen after resolution, so spelling cannot change macro bindings.
func macroNames(info *check.Info) map[*resolve.Symbol]string {
	names := map[*resolve.Symbol]string{}
	if info == nil || info.Resolve == nil {
		return names
	}
	result := info.Resolve
	scopes := map[*resolve.Symbol]*resolve.Scope{}
	for _, pkg := range result.Packages {
		for _, scope := range []*resolve.Scope{pkg.Scope, pkg.Scope.Parent} {
			for _, symbol := range scope.Bindings {
				scopes[symbol] = scope
			}
		}
		for _, file := range pkg.Files {
			for _, scope := range file.Scopes {
				for _, symbol := range scope.Bindings {
					scopes[symbol] = scope
				}
			}
		}
	}
	for _, pkg := range result.Packages {
		fixed := map[*resolve.Symbol]bool{}
		inPackage := func(name string) bool {
			file := result.Files[name]
			return file != nil && file.Scope.Parent == pkg.Scope
		}
		reserved := map[string]bool{}
		relevant := map[string]bool{}
		groups := map[string]map[*resolve.Symbol]bool{}
		add := func(symbol *resolve.Symbol) {
			if symbol == nil || symbol.GoName == "" || symbol.GoName == "_" {
				return
			}
			reserved[symbol.GoName] = true
			if groups[symbol.GoName] == nil {
				groups[symbol.GoName] = map[*resolve.Symbol]bool{}
			}
			groups[symbol.GoName][symbol] = true
			if symbol.Decl.File == "" {
				// A definition-package prelude is visible throughout the caller,
				// even though its resolver scope belongs to another package.
				scopes[symbol] = pkg.Scope.Parent
				fixed[symbol] = true
				if local := pkg.Scope.Parent.Bindings[resolve.Key{Name: symbol.Name.Name}]; local != nil {
					groups[symbol.GoName][local] = true
				}
			}
		}
		prelude := func(name string) {
			if symbol := pkg.Scope.Parent.Bindings[resolve.Key{Name: name}]; symbol != nil {
				add(symbol)
				relevant[symbol.GoName] = true
			}
		}
		typeSymbols := map[types.Type]*resolve.Symbol{}
		for symbol, typ := range info.Symbols {
			if !inPackage(symbol.Decl.File) || symbol.Kind != resolve.Type && symbol.Kind != resolve.Enum && symbol.Kind != resolve.TypeParam {
				continue
			}
			if _, alias := typ.(*types.Alias); alias {
				continue
			}
			if named, ok := typ.(*types.Named); ok {
				typ = named.Origin()
			}
			typeSymbols[typ] = symbol
		}
		typeReference := func(typ types.Type) {
			if basic, ok := typ.(*types.Basic); ok {
				prelude(basic.Name())
				return
			}
			if named, ok := typ.(*types.Named); ok {
				typ = named.Origin()
			}
			if symbol := typeSymbols[typ]; symbol != nil {
				add(symbol)
				relevant[symbol.GoName] = true
			}
		}
		enumReferences := func(instance *check.Instance) {
			macroTypeNames(instance.Enum.Type, typeReference)
			for _, argument := range instance.Args {
				macroTypeNames(argument, typeReference)
			}
			if owner := typeSymbols[instance.Enum.Type.Origin()]; owner != nil {
				file := result.Files[owner.Decl.File]
				for _, variant := range instance.Enum.Variants {
					if variant.Decl == nil {
						continue
					}
					if symbol := file.Definitions[variant.Decl.Name.ID()]; symbol != nil {
						// Source variants belong to an enum member scope; their Go
						// structs occupy the package scope referenced by type switches.
						scopes[symbol], fixed[symbol] = pkg.Scope, true
						add(symbol)
						relevant[symbol.GoName] = true
					}
				}
			}
		}
		for _, symbol := range pkg.Scope.Bindings {
			fixed[symbol] = true
		}
		for _, symbol := range pkg.Scope.Parent.Bindings {
			fixed[symbol] = true
		}
		for _, file := range pkg.Files {
			probe := &emitter{info: info, result: result, file: file, name: file.Name, tree: file.AST}
			for _, scope := range file.Scopes {
				if scope.Kind == resolve.MemberScope {
					for _, symbol := range scope.Bindings {
						fixed[symbol] = true
					}
				}
			}
			syntax.Walk(file.AST, func(node syntax.Node) bool {
				switch node := node.(type) {
				case *syntax.MatchExpr:
					if match := info.Matches[node]; match != nil && match.Enum != nil {
						prelude("panic")
						enumReferences(match.Enum)
					}
				case *syntax.TryExpr:
					prelude("panic")
				case *syntax.ImplDecl:
					if impl := info.Impls[node]; impl != nil {
						for _, member := range impl.Methods {
							if member.Trait == nil && !member.Static && info.EnumOf(impl.Target) != nil {
								prelude("panic")
								prelude("nil")
								prelude("bool")
								if instance := info.EnumOf(member.Impl.Target); instance != nil {
									enumReferences(instance)
								}
							}
						}
					}
				}
				if call, ok := node.(*syntax.CallExpr); ok {
					if instance := info.EnumValid[call]; instance != nil {
						prelude("bool")
						prelude("nil")
						enumReferences(instance)
					}
					if typ := info.EnumAssertions[call]; typ != nil {
						prelude("panic")
						prelude("nil")
						macroEnumAssertionNames(probe, typ, typeReference, enumReferences)
					}
				}
				id, ok := node.(*syntax.Ident)
				if !ok {
					return true
				}
				symbol := file.Definitions[id.ID()]
				if symbol == nil {
					symbol = file.References[id.ID()].Symbol
				}
				add(symbol)
				if symbol != nil && id.Expn != 0 {
					relevant[symbol.GoName] = true
				}
				return true
			})
		}
		var ordered []string
		for name := range relevant {
			ordered = append(ordered, name)
		}
		sort.Strings(ordered)
		for _, name := range ordered {
			group := groups[name]
			if len(group) < 2 {
				continue
			}
			preserveTypeName := false
			for symbol := range group {
				preserveTypeName = preserveTypeName || symbol.Kind == resolve.Type && symbol.Decl.File != "" && !fixed[symbol]
			}
			if preserveTypeName {
				if _, basic := types.Universe.Lookup(name).(*types.TypeName); basic {
					for symbol := range group {
						if symbol.Decl.File == "" {
							names[symbol] = "goxide" + strings.ToUpper(name[:1]) + name[1:]
						}
					}
					continue
				}
			}
			var symbols []*resolve.Symbol
			for symbol := range group {
				conflict := false
				for other := range group {
					if symbol != other && macroScopesOverlap(scopes[symbol], scopes[other]) {
						conflict = true
						break
					}
				}
				if !conflict {
					continue
				}
				if !fixed[symbol] && inPackage(symbol.Decl.File) && symbol.Kind != resolve.Type && symbol.Kind != resolve.Import && symbol.Kind != resolve.Module && symbol.Kind != resolve.Field {
					symbols = append(symbols, symbol)
				}
			}
			sort.Slice(symbols, func(i, j int) bool {
				if symbols[i].Decl.File != symbols[j].Decl.File {
					return symbols[i].Decl.File < symbols[j].Decl.File
				}
				return symbols[i].Decl.ID < symbols[j].Decl.ID
			})
			for _, symbol := range symbols {
				candidate := name
				for index := 2; reserved[candidate]; index++ {
					candidate = fmt.Sprintf("%s%d", name, index)
				}
				names[symbol], reserved[candidate] = candidate, true
			}
		}
	}
	return names
}

func (e *emitter) checkMacroNames() {
	called := map[syntax.NodeID]bool{}
	var panicSite syntax.Node
	syntax.Walk(e.tree, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.MatchExpr:
			if match := e.info.Matches[node]; match != nil && match.Enum != nil {
				panicSite = node
			}
		case *syntax.TryExpr:
			panicSite = node
		case *syntax.ImplDecl:
			if impl := e.info.Impls[node]; impl != nil {
				for _, member := range impl.Methods {
					if member.Trait == nil && !member.Static && e.info.EnumOf(impl.Target) != nil {
						panicSite = node
					}
				}
			}
		}
		if call, ok := node.(*syntax.CallExpr); ok {
			if e.info.EnumAssertions[call] != nil {
				panicSite = call
			}
			fun := call.Fun
			for {
				paren, ok := fun.(*syntax.ParenExpr)
				if !ok {
					break
				}
				fun = paren.X
			}
			called[fun.ID()] = true
		}
		return true
	})
	if panicSite != nil {
		for _, symbol := range e.file.Scope.Parent.Bindings {
			if symbol.GoName == "panic" {
				e.fail(panicSite, "enum dispatch requires the builtin panic; package declaration shadows panic")
				return
			}
		}
	}
	syntax.Walk(e.tree, func(node syntax.Node) bool {
		id, ok := node.(*syntax.Ident)
		if !ok || id.Expn == 0 {
			return true
		}
		symbol := e.file.References[id.ID()].Symbol
		for symbol != nil && symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		e.checkMacroName(id, symbol)
		if symbol != nil && symbol.Decl.File == "" && symbol.Kind == resolve.Builtin && !called[id.ID()] {
			if _, builtin := types.Universe.Lookup(symbol.Name.Name).(*types.Builtin); builtin {
				e.fail(id, "builtin "+symbol.Name.Name+" must be called")
			}
		}
		return true
	})
}

func (e *emitter) checkMacroName(id *syntax.Ident, symbol *resolve.Symbol) {
	if id.Expn == 0 || symbol == nil || symbol.Kind == resolve.Builtin && symbol.Name.Name == "enum_valid" {
		return
	}
	expansion := e.result.Expansions[id.Expn]
	context := e.result.Contexts[expansion.Ctx]
	if context == nil || id.Ctx != expansion.Ctx {
		return
	}
	if symbol.Decl.File != "" {
		if object := types.Universe.Lookup(symbol.Name.Name); object != nil && (symbol.Kind == resolve.Type || symbol.Kind == resolve.Constant) {
			if source := e.result.Files[symbol.Decl.File]; source != nil && source.AST.NativeGo {
				e.fail(id, "macro primitive conflicts with a definition-site declaration of "+symbol.Name.Name)
			}
		}
		return
	}
	// Boolean constants have a name-free Go representation. reference emits
	// 0 == 0 or 0 != 0 when a caller declaration shadows their spelling.
	if symbol.Kind == resolve.Constant && (symbol.Name.Name == "true" || symbol.Name.Name == "false") {
		return
	}
	if symbol.Kind != resolve.Builtin && symbol.Kind != resolve.Type && symbol.Kind != resolve.Constant {
		return
	}
	seen := map[*resolve.Scope]bool{}
	for scope := context.Caller; scope != nil && !seen[scope]; {
		seen[scope] = true
		if scope.Kind == resolve.PreludeScope {
			break
		}
		for _, local := range scope.Bindings {
			if local.GoName != symbol.GoName || local.Decl.File == "" {
				continue
			}
			if scope.Kind == resolve.PackageScope {
				e.fail(id, "macro primitive conflicts with a caller package declaration of "+symbol.Name.Name)
				return
			}
			if symbol.Kind == resolve.Builtin && local.Kind == resolve.Type {
				e.fail(id, "macro builtin "+symbol.Name.Name+" is shadowed by a local named type")
				return
			}
		}
		if scope.Kind == resolve.ContextScope {
			scope = scope.Caller
		} else {
			scope = scope.Parent
		}
	}
}

func (e *emitter) primitiveAlias(name string) *goast.Ident {
	symbol := e.file.Scope.Parent.Parent.Bindings[resolve.Key{Name: name}]
	if e.symbolNames[symbol] == "" {
		return goast.NewIdent(name)
	}
	if e.primitiveAliases == nil {
		e.primitiveAliases = map[string]*goast.Ident{}
	}
	if alias := e.primitiveAliases[name]; alias != nil {
		return alias
	}
	hash := sha256.Sum256([]byte(e.name))
	alias := e.fresh(fmt.Sprintf("%s%x_", strings.TrimPrefix(e.symbolNames[symbol], "goxide"), hash[:4]))
	e.primitiveAliases[name] = alias
	return alias
}

func (e *emitter) booleanShadowed(name string) bool {
	for _, file := range e.result.Files {
		if file.Scope.Parent != e.file.Scope.Parent {
			continue
		}
		for _, symbol := range file.Definitions {
			if symbol.GoName == name && e.symbolNames[symbol] == "" && symbol.Kind != resolve.Field {
				return true
			}
		}
	}
	return false
}

func (e *emitter) addPrimitiveAliases(file *goast.File) {
	var names []string
	for name := range e.primitiveAliases {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		file.Decls = append(file.Decls, &goast.GenDecl{Tok: gotoken.TYPE, Specs: []goast.Spec{
			&goast.TypeSpec{Name: e.primitiveAliases[name], Assign: 1, Type: goast.NewIdent(name)},
		}})
	}
}

func (e *emitter) macroReference(id *syntax.Ident, symbol *resolve.Symbol) (goast.Expr, bool) {
	if symbol == nil || symbol.Decl.File == "" || len(symbol.Target) > 0 {
		return nil, false
	}
	if symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	definition := filepath.ToSlash(filepath.Dir(symbol.Decl.File))
	if e.samePackage(symbol.Decl.File) {
		return nil, false
	}
	if symbol.Visibility != resolve.Public {
		e.fail(id, "宏展开不能访问定义包的私有成员 "+symbol.Name.Name)
		return nil, true
	}
	if e.info != nil && e.info.PackagePaths[definition] != "" {
		definition = e.info.PackagePaths[definition]
	}
	target := append(strings.Split(definition, "/"), symbol.Name.Name)
	return e.external(id, target, symbol.GoName, ""), true
}

// Template offsets belong to their definition file, not the generated Go file.
func (e *emitter) macroLayout(source syntax.Node, output goast.Node) {
	if e.lineDirectives || e.err != nil || source == nil || output == nil || e.result.Origins[resolve.Ref{File: e.name, ID: source.ID()}].Expn == 0 {
		return
	}
	var ellipses []*goast.CallExpr
	var aliases []*goast.TypeSpec
	var groups []*goast.GenDecl
	goast.Inspect(output, func(node goast.Node) bool {
		switch n := node.(type) {
		case *goast.CallExpr:
			if n.Ellipsis != 0 {
				ellipses = append(ellipses, n)
			}
		case *goast.TypeSpec:
			if n.Assign != 0 {
				aliases = append(aliases, n)
			}
		case *goast.GenDecl:
			if n.Lparen != 0 {
				groups = append(groups, n)
			}
		}
		return true
	})
	clearGoPositions(reflect.ValueOf(output))
	for _, call := range ellipses {
		call.Ellipsis = 1
	}
	for _, alias := range aliases {
		alias.Assign = 1
	}
	for _, group := range groups {
		group.Lparen = 1
	}
}

func macroScopesOverlap(left, right *resolve.Scope) bool {
	if left == nil || right == nil {
		return false
	}
	for scope := left; scope != nil; scope = scope.Parent {
		if scope == right {
			return true
		}
	}
	for scope := right; scope != nil; scope = scope.Parent {
		if scope == left {
			return true
		}
	}
	return false
}

// Follow only the types spelled in a generated Go type expression. Named
// types contribute their name and arguments, not their hidden representation.
func macroTypeNames(t types.Type, add func(types.Type)) {
	if t == nil {
		return
	}
	t = types.Unalias(types.Default(t))
	add(t)
	switch t := t.(type) {
	case *types.Named:
		for i := 0; i < t.TypeArgs().Len(); i++ {
			macroTypeNames(t.TypeArgs().At(i), add)
		}
	case *types.Pointer:
		macroTypeNames(t.Elem(), add)
	case *types.Array:
		macroTypeNames(t.Elem(), add)
	case *types.Slice:
		macroTypeNames(t.Elem(), add)
	case *types.Map:
		macroTypeNames(t.Key(), add)
		macroTypeNames(t.Elem(), add)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			macroTypeNames(t.Field(i).Type(), add)
		}
	case *types.Chan:
		macroTypeNames(t.Elem(), add)
	case *types.Signature:
		macroTypeNames(t.Params(), add)
		macroTypeNames(t.Results(), add)
	case *types.Tuple:
		for i := 0; i < t.Len(); i++ {
			macroTypeNames(t.At(i).Type(), add)
		}
	case *types.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			macroTypeNames(t.ExplicitMethod(i).Type(), add)
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			macroTypeNames(t.EmbeddedType(i), add)
		}
	}
}

// The assertion inserts recursive type switches that have no source identifier
// nodes. Reserve their bindings along the same finite path as the emitted walk.
// A foreign private representation is handled by its owner helper, whose fresh
// parameters do not share the caller's lexical scope.
func macroEnumAssertionNames(e *emitter, typ types.Type, addType func(types.Type), addEnum func(*check.Instance)) {
	var walk func(types.Type, []types.Type)
	walk = func(typ types.Type, seen []types.Type) {
		typ = types.Unalias(typ)
		if !e.enumAssertionContains(typ, seen) {
			return
		}
		if named, ok := typ.(*types.Named); ok && e.enumAssertionNeedsHelper(named, seen) {
			return
		}
		seen = append(append([]types.Type(nil), seen...), typ)
		if instance := e.info.EnumOf(typ); instance != nil {
			addEnum(instance)
			for _, variant := range instance.Enum.Variants {
				for _, field := range instance.Fields(variant) {
					walk(field.Type, seen)
				}
			}
			return
		}
		if array, ok := e.info.ArrayOf(typ); ok {
			walk(array.Elem, seen)
			return
		}
		if named, ok := typ.(*types.Named); ok {
			typ = named.Underlying()
		}
		switch typ := typ.(type) {
		case *types.Struct:
			for i := 0; i < typ.NumFields(); i++ {
				walk(typ.Field(i).Type(), seen)
			}
		case *types.Slice:
			walk(typ.Elem(), seen)
		case *types.Map:
			walk(typ.Elem(), seen)
		}
	}
	if named, ok := types.Unalias(typ).(*types.Named); !ok || !e.enumAssertionNeedsHelper(named, nil) {
		macroTypeNames(typ, addType)
	}
	walk(typ, nil)
}
