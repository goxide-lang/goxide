package frontend

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/deriveextras"
	"github.com/hxsf/goxide/internal/enums"
)

type externalSumProtocol struct {
	family enumFamilyExport
	origin *types.Package
	owners map[string]*types.Package
}

// Use the same lowered/registered declarations and actual Go type identity as
// match/equality. A spelling resembling Option or Result grants no capability.
func (g *generator) prepareSumProtocols(f *ast.File, cfg *compileConfig, imp types.Importer) {
	files := []*ast.File{f}
	if cfg != nil {
		paths := make([]string, 0, len(cfg.enumRegistry))
		for path := range cfg.enumRegistry {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			for _, family := range cfg.enumRegistry[path].enumMetadata.Families {
				if imp == nil || !enumOriginImportAllowed(cfg.packagePath, family.DefinitionPath) {
					continue
				}
				origin, err := imp.Import(family.DefinitionPath)
				if err == nil && origin != nil {
					owners := map[string]*types.Package{family.DefinitionPath: origin}
					for _, instance := range family.Instances {
						if instance.ValidatorPath != "" && enumOriginImportAllowed(cfg.packagePath, instance.ValidatorPath) {
							if owner, err := imp.Import(instance.ValidatorPath); err == nil {
								owners[instance.ValidatorPath] = owner
							}
						}
					}
					g.sumExternal = append(g.sumExternal, externalSumProtocol{family, origin, owners})
				}
			}
		}
		names := make([]string, 0, len(cfg.builtinTypes))
		for name := range cfg.builtinTypes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if name == g.file {
				continue
			}
			ctx := cfg.builtinTypes[name]
			var b bytes.Buffer
			fmt.Fprintf(&b, "package %s\n", f.Name.Name)
			for _, im := range ctx.Imports {
				b.WriteString("import ")
				_ = format.Node(&b, g.fs, im)
				b.WriteByte('\n')
			}
			typesNames := make([]string, 0, len(ctx.Types))
			for n := range ctx.Types {
				if f.Scope.Lookup(n) == nil {
					typesNames = append(typesNames, n)
				}
			}
			sort.Strings(typesNames)
			for _, n := range typesNames {
				b.WriteString("type ")
				_ = format.Node(&b, g.fs, ctx.Types[n])
				b.WriteByte('\n')
			}
			for _, fn := range cfg.protocolFunctions[name] {
				_ = format.Node(&b, g.fs, fn)
				b.WriteByte('\n')
			}
			extra, err := parser.ParseFile(g.fs, name+".sumtypes", b.Bytes(), 0)
			if err == nil {
				files = append(files, extra)
			}
		}
		g.protocolSums = append(g.protocolSums, cfg.imported[g.file]...)
		g.protocolSums = append(g.protocolSums, cfg.sumDeclarations...)
	}
	names := make([]string, 0, len(g.enumDecls))
	for n := range g.enumDecls {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := g.enumDecls[n]
		if d.Family != "" {
			g.protocolSums = append(g.protocolSums, *d)
		}
	}
	conf := types.Config{Importer: imp, IgnoreFuncBodies: true, Error: func(error) {}}
	g.protocolPkg, _ = conf.Check("hgo/derive", g.fs, files, nil)
	g.protocolFile = f
	g.sumHelpers = map[string]string{}
	g.sumImports = map[string]string{}
}

func (g *generator) protocolType(t ast.Expr) types.Type {
	if g.protocolPkg == nil {
		return nil
	}
	v, e := types.Eval(g.fs, g.protocolPkg, g.protocolFile.Pos(), g.typeName(t))
	if e != nil {
		return nil
	}
	return types.Unalias(v.Type)
}

// The concrete validator's exact parameter pins both origin and all type args.
func (g *generator) registeredSum(t ast.Expr) (enums.Decl, *types.Named, string, bool, bool) {
	target, ok := g.protocolType(t).(*types.Named)
	if !ok || target.TypeArgs().Len() == 0 {
		return enums.Decl{}, nil, "", false, false
	}
	for _, d := range g.protocolSums {
		if d.Family == "" {
			continue
		}
		validators := []string{d.Validator}
		for _, x := range d.Instances {
			validators = append(validators, x.Validator)
		}
		for _, validator := range validators {
			if validator == "" {
				continue
			}
			v, err := types.Eval(g.fs, g.protocolPkg, g.protocolFile.Pos(), validator)
			if err != nil {
				continue
			}
			sig, ok := v.Type.(*types.Signature)
			if ok && !sig.Variadic() && sig.TypeParams().Len() == 0 && sig.Params().Len() == 1 && sig.Results().Len() == 0 && types.Identical(sig.Params().At(0).Type(), target) {
				return d, target, validator, false, true
			}
		}
	}
	// A native .go alias may be the sole import of this family. Its verified
	// metadata remains available without requiring a source hgo selector.
	for _, entry := range g.sumExternal {
		for _, instance := range entry.family.Instances {
			owner := entry.origin
			if instance.ValidatorPath != "" {
				owner = entry.owners[instance.ValidatorPath]
			}
			if owner == nil {
				continue
			}
			fn, ok := owner.Scope().Lookup(instance.Validator).(*types.Func)
			if !ok {
				continue
			}
			sig := fn.Type().(*types.Signature)
			if sig.Params().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), target) {
				continue
			}
			d := enums.Decl{Family: entry.family.Family, PackagePath: entry.family.DefinitionPath, ValidatorPath: owner.Path()}
			for _, v := range entry.family.Variants {
				payload := ""
				if v.PayloadIndex >= 0 {
					payload = "registered"
				}
				d.Variants = append(d.Variants, enums.Variant{Name: v.Name, Payload: payload, CaseName: v.Case, Constructor: v.Constructor})
			}
			return d, target, instance.Validator, true, true
		}
	}
	return enums.Decl{}, nil, "", false, false
}

// Prefer visible transparent aliases, then recursively spell only types actually
// available in this file's import scope; verify the spelling before using it.
func (g *generator) protocolTypeName(t types.Type) (string, error) {
	valid := func(s string) bool {
		v, e := types.Eval(g.fs, g.protocolPkg, g.protocolFile.Pos(), s)
		return e == nil && types.Identical(v.Type, t)
	}
	_, named := types.Unalias(t).(*types.Named)
	for _, name := range g.protocolPkg.Scope().Names() {
		if !named {
			break
		}
		if strings.HasPrefix(name, "__hgo_") {
			continue
		}
		if obj, ok := g.protocolPkg.Scope().Lookup(name).(*types.TypeName); ok && types.Identical(obj.Type(), t) && valid(name) {
			return name, nil
		}
	}
	aliases := make([]string, 0, len(g.defaultImportPaths))
	for alias := range g.defaultImportPaths {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		if !named {
			break
		}
		for _, pkg := range g.protocolPkg.Imports() {
			if pkg.Path() != g.defaultImportPaths[alias] {
				continue
			}
			for _, name := range pkg.Scope().Names() {
				if !ast.IsExported(name) {
					continue
				}
				if x, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok && types.Identical(x.Type(), t) && valid(alias+"."+name) {
					return alias + "." + name, nil
				}
			}
		}
	}
	var s string
	switch x := types.Unalias(t).(type) {
	case *types.Basic:
		s = x.Name()
	case *types.Slice:
		e, err := g.protocolTypeName(x.Elem())
		if err != nil {
			return "", err
		}
		s = "[]" + e
	case *types.Array:
		e, err := g.protocolTypeName(x.Elem())
		if err != nil {
			return "", err
		}
		s = fmt.Sprintf("[%d]%s", x.Len(), e)
	case *types.Map:
		k, err := g.protocolTypeName(x.Key())
		if err != nil {
			return "", err
		}
		v, err := g.protocolTypeName(x.Elem())
		if err != nil {
			return "", err
		}
		s = "map[" + k + "]" + v
	default:
		s = types.TypeString(t, func(p *types.Package) string {
			if p == g.protocolPkg {
				return ""
			}
			for _, alias := range aliases {
				if g.defaultImportPaths[alias] == p.Path() {
					return alias
				}
			}
			return p.Name()
		})
	}
	if !valid(s) {
		return "", fmt.Errorf("sum payload type is not nameable: %s", t)
	}
	return s, nil
}

func (g *generator) sumProtocol(kind string, t ast.Expr) (string, bool, error) {
	if g.sumHelpers == nil {
		return "", false, nil
	}
	key := kind + "." + g.typeName(t)
	if helper, ok := g.sumHelpers[key]; ok {
		return helper, true, nil
	}
	original, target, validator, fromMetadata, ok := g.registeredSum(t)
	if !ok {
		return "", false, nil
	}
	if kind == "Default" && original.Family != "Option" {
		return "", true, g.err(t, "Result has no implicit Default protocol")
	}
	if kind == "Default" {
		// Registered Option's canonical None is nil. No payload name, Default,
		// constructor instantiation, or import is needed for this branch.
		g.sumHelpers[key] = "nil"
		return "nil", true, nil
	}
	if fromMetadata {
		g.next++
		alias := fmt.Sprintf("__hgo_enum_sum_origin_f%d_%d", g.deriveFile, g.next)
		g.sumImports[alias] = original.PackagePath
		ownerAlias := alias
		if original.ValidatorPath != "" && original.ValidatorPath != original.PackagePath {
			ownerAlias = alias + "_validator"
			g.sumImports[ownerAlias] = original.ValidatorPath
		}
		validator = ownerAlias + "." + validator
		for i := range original.Variants {
			original.Variants[i].CaseName = alias + "." + original.Variants[i].CaseName
			original.Variants[i].Constructor = alias + "." + original.Variants[i].Constructor
		}
	}
	args := make([]string, target.TypeArgs().Len())
	for i := range args {
		var err error
		args[i], err = g.protocolTypeName(target.TypeArgs().At(i))
		if err != nil {
			return "", true, g.err(t, err.Error())
		}
	}
	for _, arg := range args {
		x, err := parser.ParseExpr(arg)
		if err != nil {
			return "", true, err
		}
		if err := g.validateExtra(x, kind); err != nil {
			return "", true, g.err(t, fmt.Sprintf("%s payload requires %s: %v", original.Family, kind, err))
		}
	}
	g.next++
	prefix := fmt.Sprintf("__hgo_enum_sum_f%d_%d", g.deriveFile, g.next)
	d := original
	d.Name = prefix + "_type"
	d.Validator = prefix + "_validate"
	d.Variants = append([]enums.Variant(nil), original.Variants...)
	var prelude strings.Builder
	fmt.Fprintf(&prelude, "\ntype %s = %s\nvar %s = %s\n", d.Name, g.typeName(t), d.Validator, validator)
	for i := range d.Variants {
		v := &d.Variants[i]
		ctor := v.Constructor
		cas := v.CaseName
		suffix := "[" + strings.Join(args, ",") + "]"
		ctor = strings.Split(ctor, "[")[0] + suffix
		cas = strings.Split(cas, "[")[0] + suffix
		v.Constructor = fmt.Sprintf("%s_new%d", prefix, i)
		fmt.Fprintf(&prelude, "var %s = %s\n", v.Constructor, ctor)
		if original.Family == "Option" && i == 1 {
			v.Type = nil
			v.Payload = ""
			v.CaseName = "nil"
			continue
		}
		v.Type, _ = parser.ParseExpr(args[i])
		v.Payload = args[i]
		v.CaseName = fmt.Sprintf("%s_case%d", prefix, i)
		fmt.Fprintf(&prelude, "type %s = %s\n", v.CaseName, cas)
	}
	helper := prefix + "_" + kind
	code, imports, err := deriveextras.GenerateSum(kind, d, helper, g.deriveProtocol)
	if err != nil {
		return "", true, g.err(t, err.Error())
	}
	g.helpers.WriteString(prelude.String() + code)
	for alias, path := range imports {
		g.sumImports[alias] = path
	}
	g.sumHelpers[key] = helper
	return helper, true, nil
}
