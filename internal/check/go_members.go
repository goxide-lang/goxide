package check

import (
	goast "go/ast"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

type memberLookup struct {
	name string
	pkg  *types.Package
}

func memberOrigin(object types.Object) types.Object {
	switch object := object.(type) {
	case *types.Var:
		if object == nil {
			return nil
		}
		return object.Origin()
	case *types.Func:
		if object == nil {
			return nil
		}
		return object.Origin()
	default:
		return object
	}
}

func (c *checker) declareGoMember(object types.Object, id *ast.Ident) {
	name := id.Name
	symbol := c.symbol(id)
	if field, ok := object.(*types.Var); ok && field.Embedded() {
		symbol = c.file.References[id.ID()].Symbol
		for symbol != nil && symbol.AliasOf != nil {
			symbol = symbol.AliasOf
		}
		if symbol != nil && symbol.Kind != resolve.Type {
			symbol = nil
		}
	}
	if symbol != nil && symbol.GoName != "" {
		name = symbol.GoName
	}
	c.out.goMemberNames[memberOrigin(object)] = name
	if c.sourceMembers == nil {
		c.sourceMembers = map[string][]memberLookup{}
		c.nativeMembers = map[string][]memberLookup{}
	}
	candidate := memberLookup{object.Name(), object.Pkg()}
	add := func(index map[string][]memberLookup, name string) {
		for _, old := range index[name] {
			if old == candidate {
				return
			}
		}
		index[name] = append(index[name], candidate)
	}
	add(c.sourceMembers, id.Name)
	add(c.nativeMembers, name)
}

func (r *Info) copyGoMember(to, from types.Object) {
	if r == nil {
		return
	}
	if name := r.goMemberNames[memberOrigin(from)]; name != "" {
		r.goMemberNames[memberOrigin(to)] = name
	}
}

// GoMemberName returns the emitted name for a checked field or Go method.
func (r *Info) GoMemberName(object types.Object) string {
	if name := r.goMemberNames[memberOrigin(object)]; name != "" {
		return name
	}
	return object.Name()
}

func (c *checker) recordGoMember(id *ast.Ident, object types.Object) {
	if memberOrigin(object) != nil {
		c.out.GoMembers[id] = c.out.GoMemberName(object)
	}
}

// Source members keep their source spelling in the semantic type graph. The
// package/name index preserves Go's promotion and ambiguity rules while finding
// exported source members whose spelling starts with a lowercase letter.
func (c *checker) lookupSourceMember(t types.Type, addressable bool, name string, native bool) (types.Object, []int) {
	candidates := c.sourceMembers[name]
	if native {
		candidates = c.nativeMembers[name]
	}
	var found types.Object
	var indices []int
	ambiguous := false
	lookup := func(candidate memberLookup) {
		object, path, _ := types.LookupFieldOrMethod(t, addressable, candidate.pkg, candidate.name)
		if object == nil {
			return
		}
		goName := c.out.goMemberNames[memberOrigin(object)]
		if native && goName != "" && goName != name || !native && object.Name() != name {
			return
		}
		if object.Pkg() != c.pkg && goName != "" && !goast.IsExported(goName) {
			return
		}
		if found == nil || len(path) < len(indices) {
			found, indices, ambiguous = object, path, false
		} else if len(path) == len(indices) && memberOrigin(object) != memberOrigin(found) {
			ambiguous = true
		}
	}
	lookup(memberLookup{name, c.pkg})
	for _, candidate := range candidates {
		if candidate.name != name || candidate.pkg != c.pkg {
			lookup(candidate)
		}
	}
	if ambiguous {
		return nil, nil
	}
	return found, indices
}

func embeddedMemberIdent(typ ast.Type) *ast.Ident {
	switch typ := typ.(type) {
	case *ast.PathType:
		if len(typ.Path.Segments) > 0 {
			return typ.Path.Segments[len(typ.Path.Segments)-1].Name
		}
	case *ast.PointerType:
		return embeddedMemberIdent(typ.Elem)
	case *ast.RefType:
		return embeddedMemberIdent(typ.Elem)
	case *ast.ProjectionType:
		return typ.Item
	case *ast.ParenType:
		return embeddedMemberIdent(typ.Type)
	}
	return nil
}

func (c *checker) literalField(t types.Type, name string) *types.Var {
	if !valid(t) {
		return nil
	}
	if pointer, ok := coreType(t, nil).(*types.Pointer); ok {
		t = pointer.Elem()
	}
	if structure, ok := coreType(t, nil).(*types.Struct); ok {
		for i := 0; i < structure.NumFields(); i++ {
			field := structure.Field(i)
			fieldName := field.Name()
			if c.file.AST.NativeGo {
				if mapped := c.out.goMemberNames[memberOrigin(field)]; mapped != "" {
					fieldName = mapped
				}
			}
			if fieldName == name {
				return field
			}
		}
	}
	return nil
}
