package frontend

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

// The generic origin is registry-authorized. Alias argument mappings come from
// go/types, including partial/reordered aliases; names never imply a protocol.
func importedFamily(f enumFamilyExport, pkg *types.Package, alias, origin, prefix string) (enums.Decl, string, string) {
	d := enums.Decl{PackagePath: f.DefinitionPath, Name: origin + "." + f.DefinitionName, LookupName: alias + "." + f.Name, Family: f.Family, Marker: f.Marker}
	for _, v := range f.Variants {
		payload := ""
		if v.PayloadIndex >= 0 {
			payload = "__hgo_imported_payload"
		}
		d.Variants = append(d.Variants, enums.Variant{Name: v.Name, Payload: payload, CaseName: prefix + "_" + v.Name, Constructor: prefix + "_new" + v.Name})
	}
	var b strings.Builder
	b.WriteByte('\n') // Separate generated declarations even without a source EOF newline.
	for _, v := range f.Instances {
		// Keep the trusted producer function's identity until the typed consumer
		// pass can name its concrete parameter. A function-value alias here
		// would hide the target from Go escape analysis at every `?` call.
		d.Instances = append(d.Instances, enums.Decl{Validator: origin + "." + v.Validator})
	}
	count := 1
	if f.Family == "Result" {
		count = 2
	}
	params, args := []string{}, []string{}
	for i := 0; i < count; i++ {
		n := fmt.Sprintf("__HgoT%d", i)
		params = append(params, n+" any")
		args = append(args, n)
	}
	ps, as := "["+strings.Join(params, ",")+"]", "["+strings.Join(args, ",")+"]"
	for _, v := range f.Variants {
		input, call := "", ""
		if v.PayloadIndex >= 0 {
			fmt.Fprintf(&b, "type %s_%s%s = %s.%s%s;\n", prefix, v.Name, ps, origin, v.Case, as)
			input, call = "value "+args[v.PayloadIndex], "value"
		}
		fmt.Fprintf(&b, "func %s_new%s%s(%s) %s%s {return %s.%s%s(%s)};\n", prefix, v.Name, ps, input, d.Name, as, origin, v.Constructor, as, call)
	}
	if pkg == nil {
		return d, b.String(), "sum alias type information unavailable"
	}
	obj, ok := pkg.Scope().Lookup(f.Name).(*types.TypeName)
	if !ok {
		return d, b.String(), "sum alias type information unavailable"
	}
	var tp *types.TypeParamList
	switch t := obj.Type().(type) {
	case *types.Alias:
		tp = t.TypeParams()
	case *types.Named:
		tp = t.TypeParams()
	}
	for i := 0; i < tp.Len(); i++ {
		d.TypeParams = append(d.TypeParams, tp.At(i).Obj().Name())
	}
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		return d, b.String(), "sum alias is not a named family"
	}
	for i := 0; i < count; i++ {
		var t types.Type
		if named.TypeArgs().Len() > 0 {
			t = named.TypeArgs().At(i)
		} else {
			t = named.TypeParams().At(i)
		}
		spelling, ok := familyAliasType(t, pkg, alias)
		if !ok && named.Obj().Pkg() != pkg {
			spelling, ok = familyAliasType(t, named.Obj().Pkg(), origin)
		}
		if !ok {
			return d, b.String(), "imported sum alias payload type is not nameable; use the producer's public Go API"
		}
		d.TypeArgs = append(d.TypeArgs, spelling)
	}
	return d, b.String(), ""
}

func familyAliasType(t types.Type, pkg *types.Package, alias string) (string, bool) {
	// An exported transparent alias can make a private payload nameable without
	// serializing it or granting access to the producer's private identifier.
	for _, n := range pkg.Scope().Names() {
		if !ast.IsExported(n) {
			continue
		}
		x, ok := pkg.Scope().Lookup(n).(*types.TypeName)
		if ok && types.Identical(t, x.Type()) {
			return alias + "." + n, true
		}
	}
	switch x := t.(type) {
	case *types.TypeParam:
		return x.Obj().Name(), true
	case *types.Basic:
		return x.Name(), true
	case *types.Slice:
		s, ok := familyAliasType(x.Elem(), pkg, alias)
		return "[]" + s, ok
	case *types.Pointer:
		s, ok := familyAliasType(x.Elem(), pkg, alias)
		return "*" + s, ok
	case *types.Array:
		s, ok := familyAliasType(x.Elem(), pkg, alias)
		return fmt.Sprintf("[%d]%s", x.Len(), s), ok
	case *types.Map:
		k, ok := familyAliasType(x.Key(), pkg, alias)
		v, ok2 := familyAliasType(x.Elem(), pkg, alias)
		return "map[" + k + "]" + v, ok && ok2
	}
	visible := true
	s := types.TypeString(t, func(p *types.Package) string {
		if p == pkg {
			return alias
		}
		visible = false
		return p.Name()
	})
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && !ast.IsExported(n.Obj().Name()) {
		visible = false
	}
	return s, visible
}
