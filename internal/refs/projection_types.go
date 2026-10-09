package refs

import (
	"fmt"
	"go/ast"
	"go/types"
	"strconv"
	"strings"
)

// EnumTypeID is the canonical identity key for registered enum instances and
// payloads. Transparent aliases inside generic arguments do not create a new
// permission category (Box[PublicAlias] and Box[private] may be identical).
func EnumTypeID(t types.Type) string {
	seen := map[types.Type]types.Type{}
	var canonical func(types.Type) types.Type
	canonical = func(t types.Type) types.Type {
		t = types.Unalias(t)
		if known := seen[t]; known != nil {
			return known
		}
		seen[t] = t
		var result types.Type = t
		switch v := t.(type) {
		case *types.Named:
			if v.TypeArgs().Len() > 0 {
				args := make([]types.Type, v.TypeArgs().Len())
				for i := range args {
					args[i] = canonical(v.TypeArgs().At(i))
				}
				if instantiated, err := types.Instantiate(nil, v.Origin(), args, false); err == nil {
					result = instantiated
				}
			}
		case *types.Pointer:
			result = types.NewPointer(canonical(v.Elem()))
		case *types.Slice:
			result = types.NewSlice(canonical(v.Elem()))
		case *types.Array:
			result = types.NewArray(canonical(v.Elem()), v.Len())
		case *types.Map:
			result = types.NewMap(canonical(v.Key()), canonical(v.Elem()))
		case *types.Chan:
			result = types.NewChan(v.Dir(), canonical(v.Elem()))
		case *types.Struct:
			fields, tags := make([]*types.Var, v.NumFields()), make([]string, v.NumFields())
			for i := range fields {
				f := v.Field(i)
				fields[i] = types.NewField(f.Pos(), f.Pkg(), f.Name(), canonical(f.Type()), f.Embedded())
				tags[i] = v.Tag(i)
			}
			result = types.NewStruct(fields, tags)
		}
		seen[t] = result
		return result
	}
	return typeID(canonical(t))
}

// projectionTypeSpelling uses actual public aliases before spelling a private
// nominal type. This also permits a concrete exported alias of Option[private]
// without exposing, guessing, or redefining its type argument.
func projectionTypeSpelling(t types.Type, localPath string, routes []*types.Package, qualifier func(*types.Package) string) (string, error) {
	for _, pkg := range routes {
		for _, name := range pkg.Scope().Names() {
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if !ok || !obj.IsAlias() || !ast.IsExported(name) || !types.Identical(obj.Type(), t) {
				continue
			}
			q := qualifier(pkg)
			if q != "" {
				return q + "." + name, nil
			}
			return name, nil
		}
	}
	t = types.Unalias(t)
	switch v := t.(type) {
	case *types.Basic:
		return v.Name(), nil
	case *types.Named:
		obj := v.Obj()
		name := obj.Name()
		if obj.Pkg() != nil && obj.Pkg().Path() != localPath {
			if !obj.Exported() {
				return "", fmt.Errorf("enum projection payload has no accessible public type alias for %s", typeID(t))
			}
			name = qualifier(obj.Pkg()) + "." + name
		}
		if v.TypeArgs().Len() > 0 {
			args := make([]string, v.TypeArgs().Len())
			for i := range args {
				var err error
				args[i], err = projectionTypeSpelling(v.TypeArgs().At(i), localPath, routes, qualifier)
				if err != nil {
					return "", err
				}
			}
			name += "[" + strings.Join(args, ",") + "]"
		}
		return name, nil
	case *types.Pointer:
		elem, err := projectionTypeSpelling(v.Elem(), localPath, routes, qualifier)
		return "*" + elem, err
	case *types.Slice:
		elem, err := projectionTypeSpelling(v.Elem(), localPath, routes, qualifier)
		return "[]" + elem, err
	case *types.Array:
		elem, err := projectionTypeSpelling(v.Elem(), localPath, routes, qualifier)
		return "[" + strconv.FormatInt(v.Len(), 10) + "]" + elem, err
	case *types.Map:
		key, err := projectionTypeSpelling(v.Key(), localPath, routes, qualifier)
		if err != nil {
			return "", err
		}
		elem, err := projectionTypeSpelling(v.Elem(), localPath, routes, qualifier)
		return "map[" + key + "]" + elem, err
	}
	return types.TypeString(t, qualifier), nil
}
