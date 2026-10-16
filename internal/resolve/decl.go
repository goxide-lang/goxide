package resolve

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

// Declarations are collected before any body is visited, so files and source
// order do not affect package-level lookup.
func (r *resolver) collectPackage() {
	for _, file := range r.pkg.Files {
		r.file = file
		for _, item := range file.AST.Items {
			r.declareItem(r.pkg.Scope, item)
		}
	}
}

func (r *resolver) declareItem(scope *Scope, item ast.Item) {
	r.declareItemWith(scope, item, nil, false, ast.Visibility{})
}

func (r *resolver) declareItemWith(scope *Scope, item ast.Item, inherited *Visibility, newSyntax bool, outer ast.Visibility) {
	switch item := item.(type) {
	case *ast.FuncDecl:
		if len(item.Recv) != 0 || item.Path != nil {
			return
		}
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), !newSyntax, inherited)
		if scope.Kind == PackageScope && item.Name != nil && item.Name.Name == "init" {
			// Go permits several init functions, none of which is referencable.
			sym := &Symbol{Name: Key{Name: item.Name.Name, Ctx: item.Name.Ctx}, Kind: Function, Decl: r.ref(item.Name), Visibility: Private, GoName: "init"}
			r.recordDeclaration(item.Name, sym)
			r.recordDeclaration(item, sym)
			return
		}
		r.declareNamed(scope, item, item.Name, Function, vis)
	case *ast.TypeDecl:
		if item.Grouped {
			r.file.Visibility[item.ID()] = visibilityMark(mergeVisibility(item.Vis, outer))
			for _, spec := range item.Specs {
				r.declareTypeSpecWith(scope, spec, mergeVisibility(item.Vis, outer), inherited, newSyntax)
			}
			return
		}
		goForm := !newSyntax && item.Form == ast.GoType
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), goForm, inherited)
		sym := r.declareNamed(scope, item, item.Name, Type, vis)
		r.declareTypeMembers(item, item.Type, sym, goForm)
	case *ast.EnumDecl:
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), false, inherited)
		sym := r.declareNamed(scope, item, item.Name, Enum, vis)
		members := r.memberScope(item, sym)
		for _, variant := range item.Variants {
			vsym := r.declareNamed(members, variant, variant.Name, Variant, vis)
			if variant.Kind == ast.Struct {
				fields := r.memberScope(variant, vsym)
				r.declareFields(fields, variant.Fields, false, &vis)
			} else {
				for _, field := range variant.Fields {
					r.file.Visibility[field.ID()] = vis
				}
			}
		}
	case *ast.TraitDecl:
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), false, inherited)
		sym := r.declareNamed(scope, item, item.Name, Trait, vis)
		members := r.memberScope(item, sym)
		for _, member := range item.Items {
			r.declareItemWith(members, member, &vis, true, ast.Visibility{})
		}
	case *ast.ModDecl:
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), false, inherited)
		sym := r.declareNamed(scope, item, item.Name, Module, vis)
		if !item.Inline {
			if sym != nil {
				sym.Target = strings.Split(path.Join(filepath.ToSlash(r.pkg.Dir), item.Name.Name), "/")
				if source, ok := r.importer.(interface{ SourcePackage([]string) bool }); ok && !source.SourcePackage(sym.Target) {
					r.diag(item.Name, "module-source", "mod must name a selected local source package")
				}
			}
			return
		}
		members := r.memberScope(item, sym)
		for _, child := range item.Items {
			r.declareItem(members, child)
		}
	case *ast.MacroRulesDecl:
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), false, inherited)
		r.declareNamed(scope, item, item.Name, Macro, vis)
	case *ast.ConstDecl:
		r.declareValues(scope, item, item.Specs, Constant, mergeVisibility(item.Vis, outer), inherited)
	case *ast.VarDecl:
		r.declareValues(scope, item, item.Specs, Variable, mergeVisibility(item.Vis, outer), inherited)
	case *ast.DeclareDecl:
		r.declareItemWith(scope, item.Decl, inherited, newSyntax, mergeVisibility(item.Vis, outer))
		if item.Decl != nil {
			if sym := r.file.Definitions[item.Decl.ID()]; sym != nil {
				r.recordDeclaration(item, sym)
			} else {
				r.file.Visibility[item.ID()] = r.file.Visibility[item.Decl.ID()]
			}
		}
	}
}

func (r *resolver) declareTypeSpec(scope *Scope, spec *ast.TypeSpec, outer ast.Visibility) {
	r.declareTypeSpecWith(scope, spec, outer, nil, false)
}

func (r *resolver) declareTypeSpecWith(scope *Scope, spec *ast.TypeSpec, outer ast.Visibility, inherited *Visibility, newSyntax bool) {
	goForm := !newSyntax && spec.Form == ast.GoType
	vis := declarationVisibility(spec.Name, mergeVisibility(spec.Vis, outer), goForm, inherited)
	sym := r.declareNamed(scope, spec, spec.Name, Type, vis)
	r.declareTypeMembers(spec, spec.Type, sym, goForm)
}

func (r *resolver) declareValues(scope *Scope, owner ast.Node, specs []*ast.ValueSpec, kind Kind, outer ast.Visibility, inherited *Visibility) {
	ownerVis := Private
	singleOwner := len(specs) == 1
	switch decl := owner.(type) {
	case *ast.VarDecl:
		singleOwner = len(decl.Specs) == 1
	case *ast.ConstDecl:
		singleOwner = len(decl.Specs) == 1
	}
	for _, spec := range specs {
		specVis := Private
		for _, name := range spec.Names {
			vis := declarationVisibility(name, mergeVisibility(spec.Vis, outer), true, inherited)
			sym := r.declareNamed(scope, nil, name, kind, vis)
			if sym != nil {
				vis = sym.Visibility
			}
			if len(spec.Names) == 1 && sym != nil {
				r.recordDeclaration(spec, sym)
				if singleOwner {
					r.recordDeclaration(owner, sym)
				}
			}
			if vis == Public {
				specVis = Public
			}
		}
		r.file.Visibility[spec.ID()] = specVis
		if specVis == Public {
			ownerVis = Public
		}
	}
	r.file.Visibility[owner.ID()] = ownerVis
}

func (r *resolver) declareNamed(scope *Scope, owner ast.Node, name *ast.Ident, kind Kind, vis Visibility) *Symbol {
	if name == nil {
		return nil
	}
	if scope.Kind == FunctionScope || scope.Kind == BlockScope {
		vis = Private
	}
	sym := r.define(scope, name, name, kind)
	if sym == nil {
		return nil
	}
	sym.Visibility = vis
	sym.GoName = name.Name
	if name.Name != "_" && scope.Kind != FunctionScope && scope.Kind != BlockScope {
		var ok bool
		sym.GoName, ok = generatedName(name.Name, vis)
		if !ok {
			r.diag(name, "go-name", fmt.Sprintf("public name %q has no uppercase initial", name.Name))
		}
		r.checkGoName(scope, name, sym)
	}
	r.recordDeclaration(name, sym)
	if owner != nil {
		r.recordDeclaration(owner, sym)
	}
	return sym
}

func generatedName(name string, vis Visibility) (string, bool) {
	first, size := utf8.DecodeRuneInString(name)
	if vis == Public {
		upper := unicode.ToUpper(first)
		if !unicode.IsUpper(upper) {
			return name, false
		}
		return string(upper) + name[size:], true
	}
	if unicode.IsUpper(first) {
		return "_" + name, true
	}
	return name, true
}

func (r *resolver) recordDeclaration(node ast.Node, sym *Symbol) {
	r.file.Definitions[node.ID()] = sym
	r.file.Visibility[node.ID()] = sym.Visibility
	r.file.GoNames[node.ID()] = sym.GoName
}

func (r *resolver) checkGoName(scope *Scope, name *ast.Ident, sym *Symbol) {
	var others []*Symbol
	for _, other := range scope.Bindings {
		if other != sym && other.Name.Ctx == sym.Name.Ctx && other.Name.Name != sym.Name.Name {
			others = append(others, other)
		}
	}
	sort.Slice(others, func(i, j int) bool {
		if others[i].Name.Name != others[j].Name.Name {
			return others[i].Name.Name < others[j].Name.Name
		}
		return others[i].Name.Ctx < others[j].Name.Ctx
	})
	for _, other := range others {
		if sym.GoName == other.GoName || sym.GoName == other.Name.Name || (other.GoName != "" && other.GoName == sym.Name.Name) {
			r.diag(name, "go-name-conflict", fmt.Sprintf("Go name %q conflicts with declaration %q", sym.GoName, other.Name.Name))
			return
		}
	}
}

func (r *resolver) memberScope(node ast.Node, sym *Symbol) *Scope {
	var scope *Scope
	if sym != nil {
		scope = sym.Members
	}
	if scope == nil {
		scope = newScope(MemberScope, nil)
		if sym != nil {
			sym.Members = scope
		}
	}
	r.file.Scopes[node.ID()] = scope
	return scope
}

func (r *resolver) declareTypeMembers(owner ast.Node, typ ast.Type, sym *Symbol, goForm bool) {
	members := r.memberScope(owner, sym)
	for {
		paren, ok := typ.(*ast.ParenType)
		if !ok {
			break
		}
		typ = paren.Type
	}
	switch typ := typ.(type) {
	case *ast.StructType:
		r.file.Scopes[typ.ID()] = members
		r.declareFields(members, typ.Fields, goForm, nil)
	case *ast.InterfaceType:
		r.file.Scopes[typ.ID()] = members
		for _, method := range typ.Methods {
			if method.Name != nil {
				vis := declarationVisibility(method.Name, ast.Visibility{}, goForm, nil)
				r.declareNamed(members, method, method.Name, Function, vis)
			} else {
				r.file.Visibility[method.ID()] = Private
			}
		}
	}
}

func (r *resolver) declareFields(scope *Scope, fields []*ast.Field, goForm bool, inherited *Visibility) {
	for _, field := range fields {
		names := field.Names
		if len(names) == 0 {
			if name := embeddedName(field.Type); name != nil {
				names = []*ast.Ident{name}
			}
		}
		fieldVis := visibilityMark(field.Vis)
		if inherited != nil {
			fieldVis = *inherited
		}
		for _, name := range names {
			vis := declarationVisibility(name, field.Vis, goForm, inherited)
			sym := r.declareNamed(scope, nil, name, Field, vis)
			if len(names) == 1 && sym != nil {
				r.recordDeclaration(field, sym)
			}
			if vis == Public {
				fieldVis = Public
			}
		}
		r.file.Visibility[field.ID()] = fieldVis
	}
}

func embeddedName(typ ast.Type) *ast.Ident {
	switch typ := typ.(type) {
	case *ast.PathType:
		if typ.Path != nil && len(typ.Path.Segments) != 0 {
			return typ.Path.Segments[len(typ.Path.Segments)-1].Name
		}
	case *ast.PointerType:
		return embeddedName(typ.Elem)
	case *ast.RefType:
		return embeddedName(typ.Elem)
	case *ast.ParenType:
		return embeddedName(typ.Type)
	}
	return nil
}

func declarationVisibility(name *ast.Ident, vis ast.Visibility, goForm bool, inherited *Visibility) Visibility {
	if inherited != nil {
		return *inherited
	}
	if vis.Kind == ast.VisPub {
		return Public
	}
	if goForm && name != nil {
		first, _ := utf8.DecodeRuneInString(name.Name)
		if unicode.IsUpper(first) {
			return Public
		}
	}
	return Private
}

func mergeVisibility(inner, outer ast.Visibility) ast.Visibility {
	if inner.Kind == ast.VisNone {
		return outer
	}
	return inner
}

func visibilityMark(vis ast.Visibility) Visibility {
	if vis.Kind == ast.VisPub {
		return Public
	}
	return Private
}

// Receivers and impl targets may be declared in another file or imported, so
// attaching their members happens after package declarations and use bindings.
func (r *resolver) attachMembers() {
	for _, file := range r.pkg.Files {
		r.file = file
		for _, item := range file.AST.Items {
			r.attachItem(item, ast.Visibility{})
		}
	}
}

func (r *resolver) attachItem(item ast.Item, outer ast.Visibility) {
	switch item := item.(type) {
	case *ast.DeclareDecl:
		r.attachItem(item.Decl, mergeVisibility(item.Vis, outer))
		if item.Decl != nil {
			if sym := r.file.Definitions[item.Decl.ID()]; sym != nil {
				r.recordDeclaration(item, sym)
			} else {
				r.file.Visibility[item.ID()] = r.file.Visibility[item.Decl.ID()]
			}
		}
	case *ast.FuncDecl:
		var owner *Symbol
		if len(item.Recv) > 0 {
			owner = r.boundType(r.file.Scope, item.Recv[0].Type).Symbol
		} else if item.Path != nil && len(item.Path.Segments) > 1 {
			owner = r.boundPath(r.file.Scope, item.Path.Segments[:len(item.Path.Segments)-1]).Symbol
		} else {
			return
		}
		members := r.memberScope(item, owner)
		vis := declarationVisibility(item.Name, mergeVisibility(item.Vis, outer), true, nil)
		r.declareNamed(members, item, item.Name, Function, vis)
	case *ast.ImplDecl:
		var inherited *Visibility
		var members *Scope
		if item.Trait != nil {
			vis := Public
			trait := r.boundType(r.file.Scope, item.Trait)
			if trait.Symbol != nil {
				vis = trait.Symbol.Visibility
				if trait.Symbol.Kind == Import && r.importer == nil {
					vis = Public
				}
			}
			inherited = &vis
			members = newScope(MemberScope, nil)
			r.file.Visibility[item.ID()] = vis
		} else {
			target := r.boundType(r.file.Scope, item.Type).Symbol
			members = r.memberScope(item, target)
			r.file.Visibility[item.ID()] = visibilityMark(mergeVisibility(item.Vis, outer))
		}
		r.file.Scopes[item.ID()] = members
		for _, member := range item.Items {
			r.declareItemWith(members, member, inherited, true, ast.Visibility{})
		}
	case *ast.ModDecl:
		for _, child := range item.Items {
			r.attachItem(child, ast.Visibility{})
		}
	}
}

// These lookups deliberately ignore generic arguments: binding the receiver
// precedes walking its signature and must not resolve type parameters early.
func (r *resolver) boundType(scope *Scope, typ ast.Type) Resolution {
	switch typ := typ.(type) {
	case *ast.PathType:
		if typ.Path != nil {
			return r.boundPath(scope, typ.Path.Segments)
		}
	case *ast.PointerType:
		return r.boundType(scope, typ.Elem)
	case *ast.RefType:
		return r.boundType(scope, typ.Elem)
	case *ast.ParenType:
		return r.boundType(scope, typ.Type)
	case *ast.ProjectionType:
		return r.boundMember(r.boundType(scope, typ.X), typ.Item)
	}
	return Resolution{}
}

func (r *resolver) boundPath(scope *Scope, segments []*ast.PathSegment) Resolution {
	if len(segments) == 0 || segments[0].Name == nil {
		return Resolution{}
	}
	name := segments[0].Name
	res := r.lookup(r.identScope(scope, name), key(name), true)
	if res.Symbol != nil && res.Symbol.AliasOf != nil {
		res = Resolution{Symbol: res.Symbol.AliasOf}
	}
	for _, segment := range segments[1:] {
		res = r.boundMember(res, segment.Name)
	}
	return r.importTarget(res)
}

func (r *resolver) boundMember(base Resolution, name *ast.Ident) Resolution {
	if name == nil {
		return Resolution{}
	}
	return r.member(base, name)
}
