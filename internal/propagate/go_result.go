package propagate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func goResultMarker(expr ast.Expr) (string, ast.Expr, bool) {
	var explicit ast.Expr
	if ix, ok := expr.(*ast.IndexExpr); ok {
		explicit = ix.Index
		expr = ix.X
	}
	id, ok := expr.(*ast.Ident)
	if !ok {
		return "", nil, false
	}
	switch id.Name {
	case "__hgo_go_result", "__hgo_go_result_keep":
		return id.Name, explicit, true
	case "__hgo_go_result_auto", "__hgo_go_result_keep_auto":
		return id.Name, nil, true
	}
	return "", nil, false
}

func pendingGoResult(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if _, _, yes := goResultMarker(call.Fun); yes {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// Resolve one adapter per fresh type check. Unknown results never become an
// invented Result[any,error], and native calls are never moved into helper bodies.
func (l *lowering) resolveGoResult() (bool, error, error) {
	var changed bool
	var failure, pending error
	ast.Inspect(l.file, func(n ast.Node) bool {
		if changed || failure != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, explicit, ok := goResultMarker(call.Fun)
		if !ok {
			return true
		}
		bad := func(message string) bool { failure = l.fail(call, "Go result macro: "+message); return false }
		if len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return bad("expected one call returning E or (T, E)")
		}
		arg := call.Args[0]
		for {
			p, ok := arg.(*ast.ParenExpr)
			if !ok {
				break
			}
			arg = p.X
		}
		native, ok := arg.(*ast.CallExpr)
		if !ok {
			return bad("operand must be a call returning E or (T, E)")
		}
		nested := pendingGoResult(native)
		ast.Inspect(native, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == marker {
				nested = true
			}
			return true
		})
		if nested {
			pending = l.fail(call, "Go result macro depends on an unresolved adapter or question")
			return true
		}
		nativeType := l.info.TypeOf(native.Fun)
		if nativeType == nil {
			pending = l.fail(call, "Go result macro: cannot resolve native call signature")
			return true
		}
		sig, ok := nativeType.Underlying().(*types.Signature)
		if !ok {
			return bad("operand must be a function call")
		}
		count := sig.Results().Len()
		if count < 1 || count > 2 || explicit != nil && count != 2 {
			return bad("expected E or exactly (T, E); explicit T requires two results; wrap multiple values in a struct")
		}
		e := sig.Results().At(count - 1).Type()
		t := types.Type(types.NewStruct(nil, nil))
		if count == 2 {
			t = sig.Results().At(0).Type()
		}
		if containsTypeParam(t) || containsTypeParam(e) || pendingMatchType(t) || pendingMatchType(e) {
			pending = l.fail(call, "Go result macro: native generic or match result types remain unresolved")
			return true
		}
		errInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
		if !types.Implements(e, errInterface) {
			return bad("last result E must implement the predeclared error interface")
		}
		nilable := false
		switch u := e.Underlying().(type) {
		case *types.Interface, *types.Pointer, *types.Slice, *types.Map, *types.Signature, *types.Chan:
			nilable = true
		case *types.Basic:
			nilable = u.Kind() == types.UnsafePointer
		}
		if !nilable {
			return bad("error result E must be nilable; a non-nilable error has no implicit success value")
		}
		if explicit != nil {
			tv, err := types.Eval(l.fs, l.pkg, explicit.Pos(), printNode(explicit))
			if err != nil || !tv.IsType() || !types.Identical(t, tv.Type) {
				return bad("explicit T must be identical to the first result type")
			}
		}
		ts, ok := l.goResultType(t)
		if !ok {
			return bad("T must be nameable at package scope for the current concrete Result instance registry")
		}
		es, ok := l.goResultType(e)
		if !ok {
			return bad("E must be nameable at package scope for the current concrete Result instance registry")
		}
		if l.pkg.Scope().Lookup("nil") != nil {
			return bad("package declaration shadows the predeclared nil required by adapter checking")
		}
		keep := strings.Contains(name, "_keep")
		payload := es
		if keep {
			payload = "struct{Value " + ts + "; Err " + es + "}"
		}
		// Package-level bindings prevent caller-local type/import/Result capture.
		l.serial++
		helper := fmt.Sprintf("__hgo_go_result_%s_%d", l.cfg.Namespace, l.serial)
		tAlias, eAlias := helper+"_T", helper+"_E"
		aliasSource := "type " + tAlias + " = " + ts + ";type " + eAlias + " = " + payload + ";"
		d, err := enums.Builtin("Result", []string{tAlias, eAlias})
		if err != nil {
			return bad(err.Error())
		}
		aliases, err := parser.ParseFile(l.fs, "<Go result types>", "package "+l.file.Name.Name+";"+aliasSource, 0)
		if err != nil {
			return bad(err.Error())
		}
		named := map[string]ast.Expr{}
		for _, file := range append(append([]*ast.File{l.file}, l.companions...), aliases) {
			for _, decl := range file.Decls {
				if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.TYPE {
					for _, spec := range g.Specs {
						sp := spec.(*ast.TypeSpec)
						if sp.TypeParams == nil {
							named[sp.Name.Name] = enums.BindPayloadType(sp.Type, l.cfg.FileFamilies[l.fs.Position(file.Pos()).Filename])
						}
					}
				}
			}
		}
		declarations := append([]enums.Decl(nil), l.cfg.Families...)
		owners := make([]string, 0, len(l.cfg.FileFamilies))
		for owner := range l.cfg.FileFamilies {
			owners = append(owners, owner)
		}
		sort.Strings(owners)
		for _, owner := range owners {
			for _, imported := range l.cfg.FileFamilies[owner] {
				if imported.Family == "" {
					imported.LookupName = ""
					declarations = append(declarations, imported)
				}
			}
		}
		validator, err := enums.InstantiateBuiltin(d, declarations, named)
		if err != nil {
			return bad(err.Error())
		}
		params := "value " + tAlias + ",err " + es
		setup := ""
		if count == 1 {
			params = "err " + es
		}
		if count == 1 {
			setup = "var value " + tAlias + ";"
		}
		failed := "err"
		if keep {
			failed = eAlias + "{Value:value,Err:err}"
		}
		body := fmt.Sprintf("func %s(%s)%s{%s if err!=nil{return %s(%s)};return %s(value)}", helper, params, d.Name, setup, d.Variants[1].Constructor, failed, d.Variants[0].Constructor)
		generated, err := parser.ParseFile(l.fs, "<Go result adapter>", "package "+l.file.Name.Name+";"+body+";"+validator, 0)
		if err != nil {
			return bad(err.Error())
		}
		nodes := 0
		ast.Inspect(aliases, func(ast.Node) bool { nodes++; return true })
		ast.Inspect(generated, func(ast.Node) bool { nodes++; return true })
		if l.cfg.GoResultNodes != nil {
			*l.cfg.GoResultNodes += nodes
			if *l.cfg.GoResultNodes > 200000 {
				return bad("typed adapter generated-node budget exceeded")
			}
		}
		l.file.Decls = append(l.file.Decls, aliases.Decls...)
		l.file.Decls = append(l.file.Decls, generated.Decls...)
		l.cfg.Families = append(l.cfg.Families, d)
		if l.cfg.InferredFamilies != nil {
			*l.cfg.InferredFamilies = append(*l.cfg.InferredFamilies, d)
		}
		call.Fun = ast.NewIdent(helper)
		changed = true
		return false
	})
	return changed, failure, pending
}

func containsTypeParam(t types.Type) bool {
	switch v := types.Unalias(t).(type) {
	case *types.TypeParam:
		return true
	case *types.Named:
		for i := 0; i < v.TypeArgs().Len(); i++ {
			if containsTypeParam(v.TypeArgs().At(i)) {
				return true
			}
		}
	case *types.Pointer:
		return containsTypeParam(v.Elem())
	case *types.Slice:
		return containsTypeParam(v.Elem())
	case *types.Array:
		return containsTypeParam(v.Elem())
	case *types.Map:
		return containsTypeParam(v.Key()) || containsTypeParam(v.Elem())
	}
	return false
}

func (l *lowering) goResultType(t types.Type) (string, bool) {
	if d, ok := l.matchedEnum(t); ok {
		return d.Name, true
	}
	// Keep named container identity; recurse only through unnamed containers so
	// nested trusted enum aliases participate in the existing payload validator.
	switch v := types.Unalias(t).(type) {
	case *types.Pointer:
		s, ok := l.goResultType(v.Elem())
		return "*" + s, ok
	case *types.Slice:
		s, ok := l.goResultType(v.Elem())
		return "[]" + s, ok
	case *types.Array:
		s, ok := l.goResultType(v.Elem())
		return fmt.Sprintf("[%d]%s", v.Len(), s), ok
	case *types.Map:
		k, ok := l.goResultType(v.Key())
		if !ok {
			return "", false
		}
		vstr, ok := l.goResultType(v.Elem())
		return "map[" + k + "]" + vstr, ok
	}
	return l.typeSpelling(t)
}

// PrepareGoResults resolves available adapters across the package before the
// per-file question/effect passes. It preserves unresolved nested questions for
// those passes, while package initializers and cross-file users see real types.
func PrepareGoResults(files map[string][]byte, cfg Config, maps map[string]*sourcemap.Map) (map[string][]byte, map[string][]enums.Decl, error) {
	result := map[string][]byte{}
	for n, s := range files {
		result[n] = s
	}
	added := map[string][]enums.Decl{}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	round := 0
	for {
		round++
		progress := false
		for index, name := range names {
			if !strings.HasSuffix(name, ".hgo") || !bytes.Contains(result[name], []byte("__hgo_go_result")) {
				continue
			}
			fs := token.NewFileSet()
			f, err := parser.ParseFile(fs, name, result[name], parser.ParseComments)
			if err != nil {
				return nil, nil, err
			}
			originals := sourcemap.Nodes(f)
			local := cfg
			local.Namespace = fmt.Sprintf("p%d_r%d", index, round)
			local.Families = append(append([]enums.Decl(nil), cfg.Families...), cfg.FileFamilies[name]...)
			newFamilies := []enums.Decl{}
			local.InferredFamilies = &newFamilies
			l := &lowering{fs: fs, file: f, cfg: local}
			for _, other := range names {
				if other != name {
					comp, err := parser.ParseFile(fs, other, result[other], 0)
					if err != nil {
						return nil, nil, err
					}
					l.companions = append(l.companions, comp)
				}
			}
			l.check()
			changed, err, _ := l.resolveGoResult()
			if err != nil {
				return nil, nil, err
			}
			if !changed {
				continue
			}
			var out bytes.Buffer
			if err := format.Node(&out, fs, f); err != nil {
				return nil, nil, err
			}
			if maps[name] != nil {
				maps[name] = maps[name].AST(fs, f, originals, out.Bytes(), "Go result package inference")
			}
			result[name] = out.Bytes()
			added[name] = append(added[name], newFamilies...)
			cfg.Families = append(cfg.Families, newFamilies...)
			progress = true
		}
		if !progress {
			return result, added, nil
		}
	}
}
