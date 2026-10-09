package propagate

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
)

type sharedInstance struct {
	typ  types.Type
	decl enums.Decl
}

// PayloadEnum is a frontend-authorized public route to an enum and its complete
// validator. The resolver checks artifact provenance and actual Go identity.
type PayloadEnum struct {
	TypePath, TypeName       string
	ValidatorPath, Validator string
}

func (l *lowering) bindPayloadEnum(t types.Type, route PayloadEnum) (enums.Decl, error) {
	for _, instance := range l.sharedInstances {
		if types.Identical(t, instance.typ) {
			return instance.decl, nil
		}
	}
	fresh := func() string {
		l.serial++
		return fmt.Sprintf("__goxide_qpayload_%s_%d", l.cfg.Namespace, l.serial)
	}
	imports := map[string]string{}
	importPath := func(path string) string {
		if alias := imports[path]; alias != "" {
			return alias
		}
		alias := fresh()
		spec := &ast.ImportSpec{Name: ast.NewIdent(alias), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}}
		l.file.Imports = append(l.file.Imports, spec)
		l.file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, l.file.Decls...)
		imports[path] = alias
		return alias
	}
	d := enums.Decl{Name: fresh(), Validator: fresh(), PackagePath: types.Unalias(t).(*types.Named).Obj().Pkg().Path()}
	typePath := importPath(route.TypePath)
	validatorPath := importPath(route.ValidatorPath)
	source := fmt.Sprintf("package %s;type %s = %s.%s;func %s(v %s){%s.%s(v)}", l.file.Name.Name, d.Name, typePath, route.TypeName, d.Validator, d.Name, validatorPath, route.Validator)
	f, err := parser.ParseFile(l.fs, "<payload enum bindings>", source, 0)
	if err != nil {
		return d, err
	}
	l.file.Decls = append(l.file.Decls, f.Decls...)
	l.cfg.Families = append(l.cfg.Families, d)
	l.sharedInstances = append(l.sharedInstances, sharedInstance{t, d})
	return d, nil
}

// instantiateSharedFamily is reached only after family has checked the real
// canonical origin against trusted metadata. std owns the representation check;
// this consumer owns the concrete, finite payload projection check.
func (l *lowering) instantiateSharedFamily(t *types.Named, family enums.Decl) (enums.Decl, error) {
	for _, instance := range l.sharedInstances {
		if types.Identical(t, instance.typ) {
			return instance.decl, nil
		}
	}
	for _, active := range l.sharedActive {
		if types.Identical(t, active) {
			return enums.Decl{}, fmt.Errorf("builtin payload projection unsupported: recursive value projection")
		}
	}
	l.sharedActive = append(l.sharedActive, t)
	defer func() { l.sharedActive = l.sharedActive[:len(l.sharedActive)-1] }()

	fresh := func() string {
		l.serial++
		return fmt.Sprintf("__goxide_qsum_%s_%d", l.cfg.Namespace, l.serial)
	}
	var bindings strings.Builder
	bind := func(spelling string) string {
		name := fresh()
		fmt.Fprintf(&bindings, "type %s = %s;\n", name, spelling)
		return name
	}
	spelling, ok := l.typeSpelling(t)
	if !ok {
		return enums.Decl{}, fmt.Errorf("shared sum instance must be nameable at package scope")
	}
	name := bind(spelling)
	named := map[string]ast.Expr{}
	seen := map[types.Type]string{}
	args := make([]string, t.TypeArgs().Len())
	for i := range args {
		arg := t.TypeArgs().At(i)
		if containsTypeParam(arg) || l.pendingMatchType(arg) {
			return enums.Decl{}, fmt.Errorf("sum type remains unresolved")
		}
		s, ok := l.typeSpelling(arg)
		if !ok {
			return enums.Decl{}, fmt.Errorf("sum payload must be nameable at package scope")
		}
		args[i] = bind(s)
		projection, err := l.sharedPayloadType(arg, named, seen, fresh)
		if err != nil {
			return enums.Decl{}, err
		}
		named[args[i]] = projection
	}
	d, err := enums.Builtin(family.Family, args)
	if err != nil {
		return d, err
	}
	d.Name = name
	// The imported generic helpers and all payload types are bound at package
	// scope. A caller's local import/type names cannot capture generated uses.
	suffix := "[" + strings.Join(args, ",") + "]"
	d.Tag, err = l.bindProjectionFunction(family.Tag+suffix, args)
	if err != nil {
		return d, err
	}
	for i := range d.Variants {
		if d.Variants[i].Payload != "" {
			d.Variants[i].Value, err = l.bindProjectionFunction(family.Variants[i].Value+suffix, args)
			if err != nil {
				return d, err
			}
			project, err := l.bindProjectionFunction(family.Variants[i].Project+suffix, args)
			if err != nil {
				return d, err
			}
			d.Variants[i].Project = project
		}
		d.Variants[i].Constructor = family.Variants[i].Constructor + suffix
	}
	// Resolve the shape validator from the already-authorized canonical origin,
	// independently of an exposed partial/reordered/concrete alias.
	origin := strings.TrimSuffix(strings.Split(family.Name, "[")[0], "."+family.Family)
	shape := origin + ".Validate" + family.Family + suffix
	declarations := append([]enums.Decl(nil), l.cfg.Families...)
	text, err := enums.InstantiateBuiltin(d, declarations, named, shape)
	if err != nil {
		return d, err
	}
	f, err := parser.ParseFile(l.fs, "<shared sum instance>", "package "+l.file.Name.Name+";"+bindings.String()+text, 0)
	if err != nil {
		return d, err
	}
	if l.cfg.GoResultNodes != nil {
		ast.Inspect(f, func(n ast.Node) bool {
			if n != nil {
				*l.cfg.GoResultNodes++
			}
			return true
		})
		if *l.cfg.GoResultNodes > 200000 {
			return d, fmt.Errorf("typed sum generated-node budget exceeded")
		}
	}
	l.file.Decls = append(l.file.Decls, f.Decls...)
	l.cfg.Families = append(l.cfg.Families, d)
	l.sharedInstances = append(l.sharedInstances, sharedInstance{t, d})
	if l.cfg.InferredFamilies != nil {
		*l.cfg.InferredFamilies = append(*l.cfg.InferredFamilies, d)
	}
	return d, nil
}

// Build a projection-only view from actual type bindings, so a named container
// keeps its Go identity while its contents use the existing validator rules.
// Synthetic names here are never emitted as new Go types. In particular, a
// different file's import spelling cannot change which enum a field denotes.
func (l *lowering) sharedPayloadType(t types.Type, named map[string]ast.Expr, seen map[types.Type]string, fresh func() string) (ast.Expr, error) {
	t = types.Unalias(t)
	// Imported enum aliases are package bindings even though source imports are
	// file-local. Recover companion fields by the actual alias type identity.
	owners := make([]string, 0, len(l.cfg.FileFamilies))
	for owner := range l.cfg.FileFamilies {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		source, err := l.enumDeclarationFile(owner)
		if err != nil {
			return nil, err
		}
		for _, d := range l.cfg.FileFamilies[owner] {
			if d.Family != "" {
				continue
			}
			value, err := types.Eval(l.fs, l.pkg, source.Pos(), d.Name)
			if err == nil && types.Identical(t, value.Type) {
				bound, err := l.bindCompanionEnum(d, owner)
				if err != nil {
					return nil, err
				}
				d = bound
				l.cfg.Families = append(l.cfg.Families, d)
				return parseExpr(d.Name), nil
			}
		}
	}
	if d, ok := l.matchedEnum(t); ok {
		return parseExpr(d.Name), nil
	} else if l.familyIssue != "" {
		return nil, fmt.Errorf("builtin payload projection unsupported: %s", l.familyIssue)
	}
	if n, ok := t.(*types.Named); ok {
		if l.cfg.ResolvePayloadEnum != nil && n.Obj().Pkg() != l.pkg {
			route, ok, err := l.cfg.ResolvePayloadEnum(t)
			if err != nil {
				return nil, fmt.Errorf("builtin payload projection unsupported: %w", err)
			}
			if ok {
				d, err := l.bindPayloadEnum(t, route)
				return ast.NewIdent(d.Name), err
			}
		}
		if name, ok := seen[n]; ok {
			return ast.NewIdent(name), nil
		}
		name := fresh()
		seen[n] = name
		x, err := l.sharedPayloadType(n.Underlying(), named, seen, fresh)
		named[name] = x
		return ast.NewIdent(name), err
	}
	child := func(t types.Type) (ast.Expr, error) { return l.sharedPayloadType(t, named, seen, fresh) }
	switch t := t.(type) {
	case *types.Array:
		x, err := child(t.Elem())
		return &ast.ArrayType{Len: &ast.BasicLit{Kind: token.INT, Value: fmt.Sprint(t.Len())}, Elt: x}, err
	case *types.Slice:
		x, err := child(t.Elem())
		return &ast.ArrayType{Elt: x}, err
	case *types.Map:
		k, err := child(t.Key())
		if err != nil {
			return nil, err
		}
		v, err := child(t.Elem())
		return &ast.MapType{Key: k, Value: v}, err
	case *types.Pointer:
		x, err := child(t.Elem())
		return &ast.StarExpr{X: x}, err
	case *types.Chan:
		x, err := child(t.Elem())
		return &ast.ChanType{Value: x}, err
	case *types.Struct:
		fields := &ast.FieldList{}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			x, err := child(f.Type())
			if err != nil {
				return nil, err
			}
			if !f.Exported() && f.Pkg() != nil && f.Pkg() != l.pkg && l.sharedProjectionContainsEnum(x, named, map[string]bool{}) {
				return nil, fmt.Errorf("builtin payload projection unsupported: inaccessible enum-bearing field %s.%s", f.Pkg().Path(), f.Name())
			}
			field := &ast.Field{Type: x}
			if !f.Embedded() {
				field.Names = []*ast.Ident{ast.NewIdent(f.Name())}
			}
			fields.List = append(fields.List, field)
		}
		return &ast.StructType{Fields: fields}, nil
	}
	// Interfaces, functions and scalar types have no finite enum projection.
	return ast.NewIdent("__goxide_non_enum_payload"), nil
}

func (l *lowering) sharedProjectionContainsEnum(expr ast.Expr, named map[string]ast.Expr, seen map[string]bool) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if found {
			return false
		}
		if f, ok := node.(*ast.Field); ok {
			found = l.sharedProjectionContainsEnum(f.Type, named, seen)
			return false
		}
		if id, ok := node.(*ast.Ident); ok {
			for _, d := range l.cfg.Families {
				if d.Name == id.Name {
					found = true
					return false
				}
			}
			if shape := named[id.Name]; shape != nil && !seen[id.Name] {
				seen[id.Name] = true
				found = l.sharedProjectionContainsEnum(shape, named, seen)
			}
		}
		return true
	})
	return found
}
