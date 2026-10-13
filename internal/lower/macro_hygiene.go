package lower

import (
	"fmt"
	goast "go/ast"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
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
		}
		prelude := func(name string) {
			if symbol := pkg.Scope.Parent.Bindings[resolve.Key{Name: name}]; symbol != nil {
				add(symbol)
				relevant[symbol.GoName] = true
			}
		}
		for _, request := range result.Derives {
			if request.Helper != nil && filepath.Dir(request.File) == pkg.Dir {
				add(request.Helper)
				relevant[request.Helper.GoName] = true
			}
		}
		for _, symbol := range pkg.Scope.Bindings {
			fixed[symbol] = true
		}
		for _, symbol := range pkg.Scope.Parent.Bindings {
			fixed[symbol] = true
		}
		for _, file := range pkg.Files {
			for _, scope := range file.Scopes {
				if scope.Kind == resolve.MemberScope {
					for _, symbol := range scope.Bindings {
						fixed[symbol] = true
					}
				}
			}
			syntax.Walk(file.AST, func(node syntax.Node) bool {
				if call, ok := node.(*syntax.CallExpr); ok {
					if typ := info.Equalities[call]; typ != nil {
						if _, basic := types.Unalias(typ).(*types.Basic); basic {
							prelude("bool")
							macroTypeNames(typ, prelude)
						}
					}
					if _, print := result.Prints[call]; print {
						for index, argument := range call.Args {
							if index > 0 {
								macroTypeNames(info.Types[argument], prelude)
							}
						}
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
				if !fixed[symbol] && symbol.Decl.File != "" && filepath.Dir(symbol.Decl.File) == pkg.Dir && symbol.Kind != resolve.Import && symbol.Kind != resolve.Module && symbol.Kind != resolve.Field {
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

func (e *emitter) macroReference(id *syntax.Ident, symbol *resolve.Symbol) (goast.Expr, bool) {
	if symbol == nil || symbol.Decl.File == "" || len(symbol.Target) > 0 {
		return nil, false
	}
	if symbol.AliasOf != nil {
		symbol = symbol.AliasOf
	}
	definition := filepath.ToSlash(filepath.Dir(symbol.Decl.File))
	caller := filepath.ToSlash(filepath.Dir(e.name))
	if definition == caller {
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
	if e.err != nil || source == nil || output == nil || e.result.Origins[resolve.Ref{File: e.name, ID: source.ID()}].Expn == 0 {
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

func macroTypeNames(t types.Type, add func(string)) {
	if t == nil {
		return
	}
	switch t := types.Unalias(types.Default(t)).(type) {
	case *types.Basic:
		add(t.Name())
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
	}
}

// A manual enum implementation keeps its function frame, but its parameters
// cannot capture names used by the validation inserted before the user's body.
func (e *emitter) manualEnumValidation(d *check.Derive, fn *syntax.FuncDecl) []goast.Stmt {
	protected := map[string]bool{"nil": true, "panic": true}
	for _, pkg := range e.result.Packages {
		if pkg.Dir != filepath.Dir(e.name) {
			continue
		}
		for _, scope := range []*resolve.Scope{pkg.Scope, pkg.Scope.Parent} {
			for _, symbol := range scope.Bindings {
				if symbol.Kind == resolve.Type || symbol.Kind == resolve.Enum {
					protected[symbol.GoName] = true
				}
			}
		}
	}
	for _, enum := range e.info.Enums {
		if enum.Type.Obj().Pkg().Path() == filepath.ToSlash(filepath.Dir(e.name)) {
			for _, variant := range enum.Variants {
				protected[variant.GoName] = true
			}
		}
	}
	for _, fields := range [][]*syntax.Param{fn.Params, fn.Results} {
		for _, p := range fields {
			for _, id := range p.Names {
				if protected[e.ident(id).Name] {
					if symbol := e.file.Definitions[id.ID()]; symbol != nil {
						e.symbolNames[symbol] = e.fresh("Argument").Name
					}
				}
			}
		}
	}
	var prefix strings.Builder
	for _, p := range fn.Params {
		for _, id := range p.Names {
			prefix.WriteString(e.deriveValidation(d.Type, e.ident(id).Name, map[types.Type]bool{}))
		}
	}
	declarations := e.deriveParse(d.Node, "func validate() {"+prefix.String()+"}")
	if len(declarations) == 0 {
		return nil
	}
	return declarations[0].(*goast.FuncDecl).Body.List
}
