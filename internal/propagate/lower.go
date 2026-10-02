package propagate

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

type Conversion struct{ Source, Target, Helper string }
type Config struct {
	Families   []enums.Decl
	From       []Conversion
	Importer   types.Importer
	Provenance *sourcemap.Map
	// Files contains lowered companion files in the same package, not source
	// fragments. Each file retains its own import scope. The active filename,
	// if present, is ignored in favor of Lower's source argument.
	Files       map[string][]byte
	PackagePath string
	// Namespace is a stable per-file identifier for package-level generated
	// bindings. Package callers must give distinct values for distinct files.
	Namespace string
}
type lowering struct {
	fs            *token.FileSet
	file          *ast.File
	info          *types.Info
	pkg           *types.Package
	cfg           Config
	serial        int
	questions     map[string]enums.Decl
	conversions   map[string]string
	companions    []*ast.File
	boundFamilies map[string]enums.Decl
	familyIssue   string
}

func printNode(n any) string {
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), n)
	return b.String()
}
func parseExpr(s string) ast.Expr {
	x, e := parser.ParseExpr(s)
	if e != nil {
		panic(e)
	}
	return x
}
func parseStatements(s string) []ast.Stmt {
	f, e := parser.ParseFile(token.NewFileSet(), "", "package p;func f(){"+s+"}", 0)
	if e != nil {
		panic(e)
	}
	return f.Decls[0].(*ast.FuncDecl).Body.List
}
func (l *lowering) fail(n ast.Node, s string) error {
	return fmt.Errorf("%s: %s", l.fs.Position(n.Pos()), s)
}
func (l *lowering) temp() string { l.serial++; return fmt.Sprintf("__hgo_qtmp_%d", l.serial) }
func (l *lowering) check() {
	l.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	imp := l.cfg.Importer
	if imp == nil {
		imp = importer.Default()
	}
	c := types.Config{Importer: imp, Error: func(error) {}}
	path := l.cfg.PackagePath
	if path == "" {
		path = "hgo/generated"
	}
	files := append([]*ast.File{l.file}, l.companions...)
	l.pkg, _ = c.Check(path, l.fs, files, l.info)
}

// Resolve metadata in the same file/package scope as the checked program.
// String equality is insufficient: aliases (including macro definition bindings)
// and alternate import names can spell the same nominal instantiation differently.
func (l *lowering) resolveType(name string) types.Type {
	tv, err := types.Eval(l.fs, l.pkg, l.file.Pos(), name)
	if err != nil {
		return nil
	}
	return tv.Type
}
func (l *lowering) family(t types.Type) (enums.Decl, bool) {
	l.familyIssue = ""
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return enums.Decl{}, false
	}
	for _, d := range l.cfg.Families {
		if d.Family == "" {
			continue
		}
		if d.PackagePath == "" {
			if n.Obj().Pkg() == l.pkg && types.Identical(n, l.resolveType(d.Name)) {
				return d, true
			}
			continue
		}
		// Imported family metadata is supplied only by a compiled producer. Both
		// canonical package identity and the resolved declaration must agree.
		if n.Obj().Pkg().Path() != d.PackagePath {
			continue
		}
		decl, ok := types.Unalias(l.resolveType(d.Name)).(*types.Named)
		if !ok || !types.Identical(n.Origin(), decl.Origin()) {
			continue
		}
		if decl.TypeArgs().Len() > 0 {
			if types.Identical(n, decl) {
				d.TypeArgs = append([]string(nil), d.TypeArgs...)
				d.Variants = append([]enums.Variant(nil), d.Variants...)
				return l.bindImportedFamily(d)
			}
			continue
		}
		count := 1
		if d.Family == "Result" {
			count = 2
		}
		if (d.Family != "Result" && d.Family != "Option") || n.TypeArgs().Len() != count || len(d.Variants) != 2 {
			continue
		}
		args := make([]string, count)
		for i := range args {
			var visible bool
			args[i], visible = l.typeSpelling(n.TypeArgs().At(i))
			if !visible {
				l.familyIssue = "imported sum payload type is not nameable in this file; this prototype requires its exported type package to be imported"
				return enums.Decl{}, false
			}
		}
		suffix := "[" + strings.Join(args, ",") + "]"
		d.Name += suffix
		validator := ""
		for _, instance := range d.Instances {
			sig, ok := l.resolveType(instance.Validator).(*types.Signature)
			if ok && sig.Params().Len() == 1 && sig.Results().Len() == 0 && !sig.Variadic() && types.Identical(n, sig.Params().At(0).Type()) {
				// Different source aliases may cause the trusted producer to emit
				// several validators for the same nominal instantiation. They all
				// enforce that instance's payload contract, not competing From rules.
				validator = instance.Validator
				break
			}
		}
		if validator == "" {
			l.familyIssue = "imported sum instance has no matching producer validation metadata; this prototype cannot synthesize cross-package payload validation"
			return enums.Decl{}, false
		}
		d.Validator = validator
		d.TypeArgs = args
		d.Variants = append([]enums.Variant(nil), d.Variants...)
		for i := range d.Variants {
			d.Variants[i].CaseName += suffix
			d.Variants[i].Constructor += suffix
			if d.Family == "Result" || i == 0 {
				d.Variants[i].Payload = args[i]
				d.Variants[i].Type = parseExpr(args[i])
			}
		}
		return l.bindImportedFamily(d)
	}
	return enums.Decl{}, false
}

// Package-level bindings prevent caller locals from capturing generated uses
// of an import alias, payload type, validator or constructor.
func (l *lowering) bindImportedFamily(d enums.Decl) (enums.Decl, bool) {
	key := d.PackagePath + "|" + d.Name + "|" + d.Validator
	if cached, ok := l.boundFamilies[key]; ok {
		return cached, true
	}
	bind := func(value string, isType bool) string {
		l.serial++
		name := fmt.Sprintf("__hgo_qimport_%s_%d", l.cfg.Namespace, l.serial)
		text := "var " + name + " = " + value
		if isType {
			text = "type " + name + " = " + value
		}
		decl, err := parser.ParseFile(l.fs, "<imported propagation bindings>", "package "+l.file.Name.Name+";"+text, 0)
		if err != nil {
			l.familyIssue = "cannot represent imported propagation binding: " + err.Error()
			return ""
		}
		l.file.Decls = append(l.file.Decls, decl.Decls...)
		return name
	}
	d.Name = bind(d.Name, true)
	d.Validator = bind(d.Validator, false)
	for i := range d.TypeArgs {
		d.TypeArgs[i] = bind(d.TypeArgs[i], true)
	}
	for i := range d.Variants {
		if d.Variants[i].Payload != "" {
			d.Variants[i].CaseName = bind(d.Variants[i].CaseName, true)
		}
		d.Variants[i].Constructor = bind(d.Variants[i].Constructor, false)
	}
	if l.familyIssue != "" {
		return enums.Decl{}, false
	}
	if l.boundFamilies == nil {
		l.boundFamilies = map[string]enums.Decl{}
	}
	l.boundFamilies[key] = d
	return d, true
}

func (l *lowering) typeSpelling(t types.Type) (string, bool) {
	aliases := map[string]string{}
	for _, spec := range l.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		for _, pkg := range l.pkg.Imports() {
			if pkg.Path() != path {
				continue
			}
			name := pkg.Name()
			if spec.Name != nil {
				name = spec.Name.Name
			}
			if name != "_" && name != "." {
				aliases[path] = name
			}
		}
	}
	visible := true
	spelling := types.TypeString(t, func(pkg *types.Package) string {
		if pkg == l.pkg {
			return ""
		}
		if name := aliases[pkg.Path()]; name != "" {
			return name
		}
		visible = false
		return "__hgo_unavailable_package"
	})
	// Check the resulting spelling as a type in this file's scope. In
	// particular, an unexported named payload cannot be invented in a caller.
	resolved := l.resolveType(spelling)
	return spelling, visible && resolved != nil && types.Identical(t, resolved)
}

// Lower replaces the internal markers with branches in the original function.
// It intentionally rejects unsupported expression contexts instead of moving
// evaluation or return into a hidden closure.
func Lower(filename string, source []byte, cfg Config) ([]byte, error) {
	if cfg.Namespace != "" && !token.IsIdentifier(cfg.Namespace) {
		return nil, fmt.Errorf("%s: invalid propagation namespace", filename)
	}
	if !bytes.Contains(source, []byte(marker+"(")) && !bytes.Contains(source, []byte("__hgo_enum_infer(")) {
		return source, nil
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, filename, source, parser.ParseComments)
	if e != nil {
		return nil, e
	}
	originalNodes := sourcemap.Nodes(f)
	l := &lowering{fs: fs, file: f, cfg: cfg, questions: map[string]enums.Decl{}, conversions: map[string]string{}}
	names := make([]string, 0, len(cfg.Files))
	for name := range cfg.Files {
		if name != filename {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		companion, err := parser.ParseFile(fs, name, cfg.Files[name], parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if companion.Name.Name != f.Name.Name {
			return nil, fmt.Errorf("%s: companion belongs to package %s, expected %s", name, companion.Name.Name, f.Name.Name)
		}
		l.companions = append(l.companions, companion)
	}
	// Seed a temporary identity declaration: this makes operand types available,
	// including operands with inference, before replacing each marker's result.
	seed, _ := parser.ParseFile(fs, "<propagation signatures>", "package "+f.Name.Name+";func "+marker+"[T interface{}](v T) T {return v};func __hgo_enum_infer[T interface{}](v T) {}", 0)
	f.Decls = append(f.Decls, seed.Decls...)
	for {
		l.check()
		changed := false
		var failure error
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok || id.Name != marker {
				return true
			}
			if len(c.Args) != 1 {
				failure = l.fail(c, "propagation requires one operand")
				return false
			}
			// Process inner markers before outer markers.
			nested := false
			ast.Inspect(c.Args[0], func(n ast.Node) bool {
				if x, ok := n.(*ast.Ident); ok && x.Name == marker {
					nested = true
				}
				return true
			})
			if nested {
				return true
			}
			typ := l.info.TypeOf(c.Args[0])
			if typ == nil {
				failure = l.fail(c, "cannot determine propagation operand type")
				return false
			}
			d, ok := l.family(typ)
			if !ok {
				if l.familyIssue != "" {
					failure = l.fail(c, l.familyIssue)
					return false
				}
				failure = l.fail(c, "? requires the builtin nominal Option or Result, not a native tuple or other type")
				return false
			}
			name := l.temp()
			id.Name = name
			l.questions[name] = d
			decl, err := parser.ParseFile(fs, "<propagation signature>", "package "+f.Name.Name+";func "+name+"(v "+d.Name+") "+d.TypeArgs[0]+" {panic(\"typing only\")}", 0)
			if err != nil {
				failure = err
				return false
			}
			f.Decls = append(f.Decls, decl.Decls...)
			changed = true
			return true
		})
		if failure != nil {
			return nil, failure
		}
		if !changed {
			break
		}
	}
	l.check()
	if err := l.inferMatches(); err != nil {
		return nil, err
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && !strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_") && fn.Name.Name != marker && fn.Name.Name != "__hgo_enum_infer" {
			if fn.Type.TypeParams != nil && contains(fn.Body, l.questions) {
				return nil, l.fail(fn, "generic functions with ? are not supported by this prototype")
			}
			if e = l.body(fn.Body, fn.Type); e != nil {
				return nil, e
			}
		}
	}
	var residual ast.Node
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || fn.Name.Name == "__hgo_enum_infer" || strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_")) {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if _, yes := l.questions[id.Name]; yes {
					residual = id
				}
			}
			return true
		})
	}
	if residual != nil {
		return nil, l.fail(residual, "? in a package initializer is unsupported; use a named function")
	}
	kept := f.Decls[:0]
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || fn.Name.Name == "__hgo_enum_infer" || strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_")) {
			continue
		}
		kept = append(kept, decl)
	}
	f.Decls = kept
	var out bytes.Buffer
	if e = format.Node(&out, fs, f); e != nil {
		return nil, e
	}
	if cfg.Provenance != nil {
		next := cfg.Provenance.AST(fs, f, originalNodes, out.Bytes(), "propagate.Lower")
		*cfg.Provenance = *next
	}
	return out.Bytes(), nil
}
func contains(n ast.Node, questions map[string]enums.Decl) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if _, ok = questions[id.Name]; ok {
				found = true
			}
		}
		return true
	})
	return found
}
func (l *lowering) target(ft *ast.FuncType) (enums.Decl, error) {
	if ft.Results == nil || len(ft.Results.List) != 1 || len(ft.Results.List[0].Names) > 1 {
		return enums.Decl{}, l.fail(ft, "? target must return exactly one builtin Option or Result")
	}
	t := l.info.TypeOf(ft.Results.List[0].Type)
	if t == nil {
		return enums.Decl{}, l.fail(ft, "unknown propagation return type")
	}
	d, ok := l.family(t)
	if !ok {
		if l.familyIssue != "" {
			return d, l.fail(ft, l.familyIssue)
		}
		return d, l.fail(ft, "? target must return builtin Option or Result")
	}
	return d, nil
}
func (l *lowering) body(b *ast.BlockStmt, ft *ast.FuncType) error {
	// Each function literal has its own lexical return target.
	var err error
	ast.Inspect(b, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncLit); ok {
			err = l.body(fn.Body, fn.Type)
			return false
		}
		return err == nil
	})
	if err != nil {
		return err
	}
	var out []ast.Stmt
	for _, s := range b.List {
		ss, e := l.statement(s, ft)
		if e != nil {
			return e
		}
		out = append(out, ss...)
	}
	b.List = out
	return nil
}
func (l *lowering) statement(s ast.Stmt, ft *ast.FuncType) ([]ast.Stmt, error) {
	if !contains(s, l.questions) {
		return []ast.Stmt{s}, nil
	}
	var pre []ast.Stmt
	lower := func(x ast.Expr) (ast.Expr, error) { p, y, e := l.expr(x, ft); pre = append(pre, p...); return y, e }
	switch x := s.(type) {
	case *ast.AssignStmt:
		if len(x.Rhs) > 1 {
			return nil, l.fail(x, "? in parallel assignment is unsupported; split the evaluation into explicit statements")
		}
		for _, lhs := range x.Lhs {
			if _, ok := lhs.(*ast.Ident); !ok {
				return nil, l.fail(lhs, "? assignment currently requires identifier destinations")
			}
		}
		for i, a := range x.Rhs {
			y, e := lower(a)
			if e != nil {
				return nil, e
			}
			x.Rhs[i] = y
		}
	case *ast.ReturnStmt:
		if len(x.Results) > 1 {
			return nil, l.fail(x, "? with multiple return expressions is unsupported")
		}
		for i, a := range x.Results {
			y, e := lower(a)
			if e != nil {
				return nil, e
			}
			x.Results[i] = y
		}
	case *ast.ExprStmt:
		// A bare propagation consumes the sum for its failure branch and
		// discards the success payload. Its lowered value is not a Go
		// expression statement (and may otherwise be an unused temporary).
		original := x.X
		for {
			paren, ok := original.(*ast.ParenExpr)
			if !ok {
				break
			}
			original = paren.X
		}
		discard := false
		if call, ok := original.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok {
				_, discard = l.questions[id.Name]
			}
		}
		y, e := lower(x.X)
		if e != nil {
			return nil, e
		}
		if discard {
			return append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{y}}), nil
		}
		x.X = y
	case *ast.DeferStmt:
		y, e := lower(x.Call)
		if e != nil {
			return nil, e
		}
		x.Call = y.(*ast.CallExpr)
	case *ast.GoStmt:
		y, e := lower(x.Call)
		if e != nil {
			return nil, e
		}
		x.Call = y.(*ast.CallExpr)
	case *ast.DeclStmt:
		d, ok := x.Decl.(*ast.GenDecl)
		if !ok || d.Tok != token.VAR || len(d.Specs) != 1 {
			return nil, l.fail(x, "? declaration requires one var specification")
		}
		v := d.Specs[0].(*ast.ValueSpec)
		if len(v.Values) > 1 {
			return nil, l.fail(v, "? in parallel variable initialization is unsupported")
		}
		for i, a := range v.Values {
			y, e := lower(a)
			if e != nil {
				return nil, e
			}
			v.Values[i] = y
		}
	case *ast.BlockStmt:
		if e := l.body(x, ft); e != nil {
			return nil, e
		}
	case *ast.IfStmt:
		if x.Init != nil && (contains(x.Init, l.questions) || contains(x.Cond, l.questions)) {
			// Keep the initializer's implicit scope around both branches and all
			// condition temporaries. An ordinary block preserves lexical return,
			// defer and loop control without introducing a closure.
			init, e := l.statement(x.Init, ft)
			if e != nil {
				return nil, e
			}
			x.Init = nil
			rest, e := l.statement(x, ft)
			if e != nil {
				return nil, e
			}
			return []ast.Stmt{&ast.BlockStmt{List: append(init, rest...)}}, nil
		}
		y, e := lower(x.Cond)
		if e != nil {
			return nil, e
		}
		x.Cond = y
		if e = l.body(x.Body, ft); e != nil {
			return nil, e
		}
		if x.Else != nil {
			ss, e := l.statement(x.Else, ft)
			if e != nil {
				return nil, e
			}
			if len(ss) == 1 {
				x.Else = ss[0]
			} else {
				x.Else = &ast.BlockStmt{List: ss}
			}
		}
	default:
		return nil, l.fail(s, "? in this statement context is unsupported; use an explicit match or a preceding statement")
	}
	return append(pre, s), nil
}
func (l *lowering) expr(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	if !contains(x, l.questions) {
		return nil, x, nil
	}
	switch n := x.(type) {
	case *ast.ParenExpr:
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	case *ast.CallExpr:
		if id, ok := n.Fun.(*ast.Ident); ok {
			if d, yes := l.questions[id.Name]; yes {
				p, arg, e := l.expr(n.Args[0], ft)
				if e != nil {
					return nil, nil, e
				}
				target, e := l.target(ft)
				if e != nil {
					return nil, nil, e
				}
				if target.Family != d.Family {
					return nil, nil, l.fail(n, "Option and Result cannot propagate into each other")
				}
				tmp, val := l.temp(), l.temp()
				code := tmp + " := " + printNode(arg) + "; " + d.Validator + "(" + tmp + "); var " + val + " " + d.TypeArgs[0] + "; "
				if d.Family == "Option" {
					code += "if " + tmp + " == " + d.Variants[1].Constructor + "() { return " + target.Variants[1].Constructor + "() }; " + val + " = " + tmp + ".(" + d.Variants[0].CaseName + ").Value"
				} else {
					errVal := "__hgo_failure.Value"
					srcType := l.info.TypeOf(n.Args[0])
					srcNamed, _ := types.Unalias(srcType).(*types.Named)
					targetType := l.info.TypeOf(ft.Results.List[0].Type)
					dstNamed, _ := types.Unalias(targetType).(*types.Named)
					if srcNamed == nil || dstNamed == nil {
						return nil, nil, l.fail(n, "unresolved Result error types")
					}
					se, de := srcNamed.TypeArgs().At(1), dstNamed.TypeArgs().At(1)
					if !types.AssignableTo(se, de) {
						var choices []Conversion
						for _, c := range l.cfg.From {
							if types.Identical(se, l.resolveType(c.Source)) && types.Identical(de, l.resolveType(c.Target)) {
								choices = append(choices, c)
							}
						}
						if len(choices) != 1 {
							return nil, nil, l.fail(n, "Result error requires one unique explicit From conversion; conversion chains are not searched")
						}
						helper := choices[0].Helper
						sig, ok := l.resolveType(helper).(*types.Signature)
						if !ok || sig.Variadic() || sig.Params().Len() != 1 || sig.Results().Len() != 1 || sig.TypeParams().Len() != 0 || !types.Identical(sig.Params().At(0).Type(), se) || !types.Identical(sig.Results().At(0).Type(), de) {
							return nil, nil, l.fail(n, "registered From helper must have the exact static source and target types")
						}
						bound := l.conversions[helper]
						if bound == "" {
							l.serial++
							bound = fmt.Sprintf("__hgo_qfrom_%d", l.serial)
							if l.cfg.Namespace != "" {
								bound = fmt.Sprintf("__hgo_qfrom_%s_%d", l.cfg.Namespace, l.serial)
							}
							l.conversions[helper] = bound
							// Bind the registered package function outside every user local scope.
							l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(bound)}, Values: []ast.Expr{parseExpr(helper)}}}})
						}
						errVal = bound + "(" + errVal + ")"
					}
					code += "switch __hgo_failure := " + tmp + ".(type) {case " + d.Variants[0].CaseName + ": " + val + " = __hgo_failure.Value; case " + d.Variants[1].CaseName + ": return " + target.Variants[1].Constructor + "(" + errVal + "); default: panic(\"hgo: invalid Result at ?\") }"
				}
				return append(p, parseStatements(code)...), ast.NewIdent(val), nil
			}
		}
		// Freeze earlier evaluated runtime operands before lowering a later ?. Constants
		// and type expressions retain contextual typing rather than defaulting to int.
		var pre []ast.Stmt
		freeze := func(original, y ast.Expr) ast.Expr {
			tv := l.info.Types[original]
			if tv.IsType() || tv.IsBuiltin() || tv.Value != nil {
				return y
			}
			if b, ok := tv.Type.(*types.Basic); ok && b.Kind() == types.UntypedNil {
				return y
			}
			if id, ok := original.(*ast.Ident); ok {
				switch l.info.Uses[id].(type) {
				case *types.Builtin, *types.Func:
					// A declared function has no mutable runtime value to freeze.
					return y
				}
			}
			name := l.temp()
			pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{y}})
			return ast.NewIdent(name)
		}
		printPlan := false
		if id, ok := n.Fun.(*ast.Ident); ok {
			printPlan = id.Name == "__hgo_print" || id.Name == "__hgo_println"
		}
		p, y, e := l.expr(n.Fun, ft)
		if e != nil {
			return nil, nil, e
		}
		pre = append(pre, p...)
		if printPlan {
			n.Fun = y
		} else {
			n.Fun = freeze(n.Fun, y)
		}
		for i, a := range n.Args {
			p, y, e = l.expr(a, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			if i < len(n.Args)-1 && !(printPlan && i == 0) {
				y = freeze(a, y)
			}
			n.Args[i] = y
		}
		return pre, n, nil
	case *ast.BinaryExpr:
		p, left, e := l.expr(n.X, ft)
		if e != nil {
			return nil, nil, e
		}
		q, right, e := l.expr(n.Y, ft)
		if e != nil {
			return nil, nil, e
		}
		if n.Op == token.LAND || n.Op == token.LOR {
			name := l.temp()
			p = append(p, parseStatements(name+" := "+printNode(left))...)
			cond := ast.Expr(ast.NewIdent(name))
			if n.Op == token.LOR {
				cond = &ast.UnaryExpr{Op: token.NOT, X: cond}
			}
			q = append(q, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{right}})
			p = append(p, &ast.IfStmt{Cond: cond, Body: &ast.BlockStmt{List: q}})
			return p, ast.NewIdent(name), nil
		}
		if len(q) > 0 && l.info.Types[n.X].Value == nil {
			name := l.temp()
			p = append(p, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{left}})
			left = ast.NewIdent(name)
		}
		n.X, n.Y = left, right
		return append(p, q...), n, nil
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			return nil, nil, l.fail(n, "taking the address of a propagation expression is unsupported")
		}
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	case *ast.SelectorExpr:
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	default:
		return nil, nil, l.fail(x, "? in this expression context is unsupported; bind its result in a preceding statement")
	}
}
