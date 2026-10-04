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
	InferredFamilies *[]enums.Decl
	FileFamilies     map[string][]enums.Decl
	GoResultNodes    *int
	// MatchReferenceTemps receives only compiler-generated exhaustive match slots.
	MatchReferenceTemps map[string]bool
	Families            []enums.Decl
	From                []Conversion
	Importer            types.Importer
	Provenance          *sourcemap.Map
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
	checkErrors          []types.Error
	guardFreeDepth       int
	expressionFacts      map[ast.Expr]expressionFact
	fs                   *token.FileSet
	file                 *ast.File
	info                 *types.Info
	pkg                  *types.Package
	cfg                  Config
	serial               int
	questions            map[string]enums.Decl
	conversions          map[string]string
	companions           []*ast.File
	boundFamilies        map[string]enums.Decl
	familyIssue          string
	scalarNames          map[types.Type]string
	arrayValueHelper     string
	witnesses            map[ast.Expr]func() ast.Expr
	witnessHelpers       map[string]string
	comparableConstraint string
	placeFields          map[*ast.SelectorExpr]*types.Var
	functionBodies       map[*ast.FuncType]*ast.BlockStmt
	frameTypes           map[types.Type]string
	frameRuntime         string
	resultPointers       map[*ast.FuncType]string
	functionPrologues    map[*ast.FuncType][]ast.Stmt
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
func (l *lowering) temp() string { l.serial++; return fmt.Sprintf("__goxide_qtmp_%d", l.serial) }
func (l *lowering) check() {
	l.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	imp := l.cfg.Importer
	if imp == nil {
		imp = importer.Default()
	}
	l.checkErrors = nil
	c := types.Config{Importer: imp, Error: func(err error) {
		if e, ok := err.(types.Error); ok {
			l.checkErrors = append(l.checkErrors, e)
		}
	}}
	path := l.cfg.PackagePath
	if path == "" {
		path = "goxide/generated"
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
	// Prefer a local concrete payload validator over relayed registrations.
	for _, d := range l.cfg.Families {
		if d.Family != "" && d.PackagePath == "" && (n.Obj().Pkg() == l.pkg || n.Obj().Pkg().Path() == enums.SumPackage) && types.Identical(n, l.resolveType(d.Name)) {
			return d, true
		}
	}
	for _, d := range l.cfg.Families {
		if d.Family == "" || d.PackagePath == "" {
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
	bind := func(value string, kind token.Token) string {
		l.serial++
		name := fmt.Sprintf("__goxide_qimport_%s_%d", l.cfg.Namespace, l.serial)
		text := "var " + name + " = " + value
		if kind == token.TYPE {
			text = "type " + name + " = " + value
		} else if kind == token.FUNC {
			// The concrete family alias is already bound at package scope.
			// A named forwarding function retains definition-site binding while
			// exposing the trusted validator to Go's inline/escape analysis.
			text = "func " + name + "(__goxide_sum_value " + d.Name + ") { " + value + "(__goxide_sum_value) }"
		}
		decl, err := parser.ParseFile(l.fs, "<imported propagation bindings>", "package "+l.file.Name.Name+";"+text, 0)
		if err != nil {
			l.familyIssue = "cannot represent imported propagation binding: " + err.Error()
			return ""
		}
		l.file.Decls = append(l.file.Decls, decl.Decls...)
		return name
	}
	d.Name = bind(d.Name, token.TYPE)
	d.Validator = bind(d.Validator, token.FUNC)
	for i := range d.TypeArgs {
		d.TypeArgs[i] = bind(d.TypeArgs[i], token.TYPE)
	}
	for i := range d.Variants {
		if d.Variants[i].Payload != "" {
			d.Variants[i].CaseName = bind(d.Variants[i].CaseName, token.TYPE)
		}
		d.Variants[i].Constructor = bind(d.Variants[i].Constructor, token.VAR)
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
	// Exported transparent aliases may expose a private named payload. Resolve
	// by identity, not by the alias name or its underlying shape.
	for _, pkg := range l.pkg.Imports() {
		alias := aliases[pkg.Path()]
		if alias == "" {
			continue
		}
		for _, name := range pkg.Scope().Names() {
			if !ast.IsExported(name) {
				continue
			}
			obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
			if ok && types.Identical(t, obj.Type()) {
				return alias + "." + name, true
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
		return "__goxide_unavailable_package"
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
	if !bytes.Contains(source, []byte("__goxide_enum_pending_")) && !bytes.Contains(source, []byte("__goxide_enum_context_match")) && !bytes.Contains(source, []byte("__goxide_go_result")) && !bytes.Contains(source, []byte(marker+"(")) && !bytes.Contains(source, []byte("__goxide_enum_infer(")) && !bytes.Contains(source, []byte(matchInfer+"(")) && !bytes.Contains(source, []byte(matchContext+"(")) && !bytes.Contains(source, []byte(matchOperand+"(")) {
		return source, nil
	}
	if cfg.GoResultNodes == nil {
		cfg.GoResultNodes = new(int)
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
	seed, _ := parser.ParseFile(fs, "<propagation signatures>", "package "+f.Name.Name+";func "+marker+"[T interface{}](v T) T {return v};func __goxide_enum_infer[T interface{}](v T) {}", 0)
	f.Decls = append(f.Decls, seed.Decls...)
	matchSeed, _ := parser.ParseFile(fs, "<match result signatures>", "package "+f.Name.Name+";type "+matchPending+" interface{};func "+matchInfer+"(origin string) "+matchPending+"{return nil};func "+matchContext+"[T interface{}](v T,origin string)T{return v};func "+matchOperand+"[T interface{}](v T,kind,origin string)T{return v};func "+matchZero+"[T interface{}]()T{var v T;return v};func "+matchReturn+"() "+matchPending+"{return nil};func "+matchArgument+"() "+matchPending+"{return nil}", 0)
	f.Decls = append(f.Decls, matchSeed.Decls...)
	for {
		l.check()
		inferred, inferErr, inferPending := l.resolveSumConstructors()
		if inferErr != nil {
			return nil, inferErr
		}
		if inferred {
			continue
		}
		matched, matchErr, matchPending := l.resolveSumPatterns()
		if matchErr != nil {
			return nil, matchErr
		}
		if matched {
			// Expanded arm nodes were parsed in a separate fragment. Reparse the
			// active file so go/types scopes and CheckExpr use lexical positions
			// in the containing user function, including expression-match arms.
			var rewritten bytes.Buffer
			if err := format.Node(&rewritten, fs, f); err != nil {
				return nil, err
			}
			if cfg.Provenance != nil {
				next := cfg.Provenance.AST(fs, f, originalNodes, rewritten.Bytes(), "sum pattern expansion")
				*cfg.Provenance = *next
			}
			f, e = parser.ParseFile(fs, filename, rewritten.Bytes(), parser.ParseComments)
			if e != nil {
				return nil, e
			}
			l.file = f
			originalNodes = sourcemap.Nodes(f)
			continue
		}
		adapted, adapterError, adapterPending := l.resolveGoResult()
		if adapterError != nil {
			return nil, adapterError
		}
		if adapted {
			continue
		}
		changed := false
		var failure error
		var unresolved error
		ast.Inspect(f, func(n ast.Node) bool {
			if changed || failure != nil {
				return false
			}
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
			nested := pendingGoResult(c.Args[0])
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
				unresolved = l.fail(c, "cannot determine propagation operand type")
				return false
			}
			if pendingMatchType(typ) {
				unresolved = l.fail(c, "propagation operand depends on an unresolved match result")
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
			// The replacement changes the inferred type of subsequent locals.
			// Recheck before resolving another question from stale identity-stub facts.
			changed = true
			return false
		})
		if failure != nil {
			return nil, failure
		}
		if changed {
			continue
		}
		resolved, matchIssue := l.resolveMatchResult()
		if resolved {
			continue
		}
		if matchIssue != nil {
			unresolved = matchIssue
		}
		if unresolved == nil {
			unresolved = adapterPending
		}
		if unresolved == nil {
			unresolved = inferPending
		}
		if unresolved == nil {
			unresolved = matchPending
		}
		if unresolved != nil {
			return nil, unresolved
		}
		break
	}
	keptMatch := f.Decls[:0]
	for _, d := range f.Decls {
		if !matchSeedDecl(d) {
			keptMatch = append(keptMatch, d)
		}
	}
	f.Decls = keptMatch
	l.check()
	if err := l.inferMatches(); err != nil {
		return nil, err
	}
	l.captureExpressionFacts()
	l.bindLocalScalars()
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && !strings.HasPrefix(fn.Name.Name, "__goxide_qtmp_") && fn.Name.Name != marker && fn.Name.Name != "__goxide_enum_infer" {
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
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || fn.Name.Name == "__goxide_enum_infer" || strings.HasPrefix(fn.Name.Name, "__goxide_qtmp_")) {
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
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || fn.Name.Name == "__goxide_enum_infer" || strings.HasPrefix(fn.Name.Name, "__goxide_qtmp_")) {
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
		if matchAtom(n) != nil {
			found = true
			return false
		}
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
	if l.functionBodies == nil {
		l.functionBodies = map[*ast.FuncType]*ast.BlockStmt{}
	}
	if l.functionBodies[ft] == nil {
		if err := l.validateLoopLabels(b); err != nil {
			return err
		}
		l.functionBodies[ft] = b
	}
	// Each function literal has its own lexical return target.
	var err error
	ast.Inspect(b, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncLit); ok {
			if matchAtom(fn) != nil {
				return true
			}
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
	if l.functionBodies[ft] == b {
		out = append(l.functionPrologues[ft], out...)
		delete(l.functionPrologues, ft)
	}
	b.List = out
	return nil
}

// Bind local scalar identities before lowering any closure bodies: a closure
// may assign to a variable whose named type was declared in its outer function.
func (l *lowering) bindLocalScalars() {
	ast.Inspect(l.file, func(node ast.Node) bool {
		var list *[]ast.Stmt
		switch n := node.(type) {
		case *ast.BlockStmt:
			list = &n.List
		case *ast.CaseClause:
			list = &n.Body
		case *ast.CommClause:
			list = &n.Body
		default:
			return true
		}
		var out []ast.Stmt
		for _, statement := range *list {
			out = append(out, statement)
			for {
				label, ok := statement.(*ast.LabeledStmt)
				if !ok {
					break
				}
				statement = label.Stmt
			}
			decl, ok := statement.(*ast.DeclStmt)
			if !ok {
				continue
			}
			group, ok := decl.Decl.(*ast.GenDecl)
			if !ok || group.Tok != token.TYPE {
				continue
			}
			for _, spec := range group.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				obj := l.info.Defs[typeSpec.Name]
				if obj == nil {
					continue
				}
				t, named := obj.Type().(*types.Named)
				if !named || t.Obj() != obj {
					continue
				}
				if _, ok := t.Underlying().(*types.Basic); !ok {
					continue
				}
				name := l.temp()
				if l.scalarNames == nil {
					l.scalarNames = map[types.Type]string{}
				}
				l.scalarNames[t] = name
				out = append(out, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(name), Assign: token.Pos(1), Type: ast.NewIdent(typeSpec.Name.Name)}}}})
			}
		}
		*list = out
		return true
	})
}

func (l *lowering) statement(s ast.Stmt, ft *ast.FuncType) ([]ast.Stmt, error) {
	scopeMatch := false
	switch x := s.(type) {
	case *ast.ReturnStmt:
		scopeMatch = hasMatchAtom(x)
	case *ast.ExprStmt:
		scopeMatch = hasMatchAtom(x)
	case *ast.AssignStmt:
		scopeMatch = x.Tok == token.ASSIGN && hasMatchAtom(x)
	}

	if !contains(s, l.questions) {
		return []ast.Stmt{s}, nil
	}
	var pre []ast.Stmt
	lower := func(x ast.Expr) (ast.Expr, error) { p, y, e := l.expr(x, ft); pre = append(pre, p...); return y, e }
	switch x := s.(type) {

	case *ast.TypeSwitchStmt:
		return l.typeSwitchStatement(x, nil, ft)
	case *ast.SwitchStmt:
		return l.switchStatement(x, nil, ft)
	case *ast.ForStmt:
		return l.forStatement(x, nil, ft)
	case *ast.RangeStmt:
		return l.rangeStatement(x, nil, ft)
	case *ast.LabeledStmt:
		switch n := x.Stmt.(type) {
		case *ast.TypeSwitchStmt:
			return l.typeSwitchStatement(n, x.Label, ft)
		case *ast.SwitchStmt:
			return l.switchStatement(n, x.Label, ft)
		case *ast.ForStmt:
			return l.forStatement(n, x.Label, ft)
		case *ast.RangeStmt:
			return l.rangeStatement(n, x.Label, ft)
		}
		return nil, l.fail(x, "? in this labeled statement is unsupported")
	case *ast.AssignStmt:
		if l.needsAssignmentFrame(x) {
			if l.canPlanAssignment(x) {
				return l.plannedAssignment(x, ft)
			}
			if l.guardFreeDepth > 0 {
				return nil, l.fail(x, "this control-flow assignment needs an unimplemented guard-free propagation plan")
			}
			return l.assignmentFrame(x, ft)
		}
		for i, lhs := range x.Lhs {
			if x.Tok == token.DEFINE {
				if _, ok := lhs.(*ast.Ident); !ok {
					return nil, l.fail(lhs, "short declaration requires identifier destinations")
				}
				continue
			}
			p, target, witness, err := l.place(lhs, ft)
			if err != nil {
				return nil, err
			}
			pre = append(pre, p...)
			x.Lhs[i] = target
			if len(x.Lhs) == len(x.Rhs) && types.Identical(l.info.TypeOf(lhs), l.info.TypeOf(x.Rhs[i])) {
				l.registerWitness(x.Rhs[i], witness)
			}
		}
		p, values, e := l.values(x.Rhs, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		x.Rhs = values
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
		p, values, e := l.values(v.Values, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		v.Values = values
	case *ast.SendStmt:
		p, channel, e := l.expr(x.Chan, ft)
		if e != nil {
			return nil, e
		}
		pre = append(pre, p...)
		// Evaluate the channel before the value's propagation prelude. A nil
		// channel only blocks if the send itself is reached.
		x.Chan = l.freezeValue(x.Chan, channel, &pre)
		x.Value, e = lower(x.Value)
		if e != nil {
			return nil, e
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
	if scopeMatch && len(pre) > 0 {
		return []ast.Stmt{&ast.BlockStmt{List: append(pre, s)}}, nil
	}
	return append(pre, s), nil
}

// values stages simultaneous RHS expressions without writing any destination
// until every operand succeeds. Constants and nil keep their contextual types;
// a single multi-valued expression remains intact for Go's tuple assignment.
func (l *lowering) values(values []ast.Expr, ft *ast.FuncType) ([]ast.Stmt, []ast.Expr, error) {
	var pre []ast.Stmt
	out := make([]ast.Expr, len(values))
	for i, original := range values {
		p, value, err := l.expr(original, ft)
		if err != nil {
			return nil, nil, err
		}
		pre = append(pre, p...)
		if i < len(values)-1 {
			value = l.freezeValue(original, value, &pre)
		}
		out[i] = value
	}
	return pre, out, nil
}

func underlyingType(t types.Type) types.Type {
	if t == nil {
		return nil
	}
	return t.Underlying()
}

func (l *lowering) freezeValue(original, value ast.Expr, pre *[]ast.Stmt) ast.Expr {
	tv := l.info.Types[original]
	if tv.Value != nil {
		return value
	}
	if basic, ok := tv.Type.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
		return value
	}
	// A runtime shift such as 1<<n receives its type from the enclosing
	// assignment. Using := alone would default it to int before assignment.
	if tv.Type != nil {
		t := types.Unalias(tv.Type)
		if basic, ok := t.Underlying().(*types.Basic); ok && basic.Info()&types.IsUntyped == 0 {
			typeName := l.scalarNames[t]
			if typeName == "" {
				if spelling, ok := l.typeSpelling(t); ok {
					typeName = "__goxide_qscalar_" + l.cfg.Namespace + l.temp()
					l.file.Decls = append(l.file.Decls, &ast.GenDecl{Tok: token.TYPE, Specs: []ast.Spec{&ast.TypeSpec{Name: ast.NewIdent(typeName), Assign: token.Pos(1), Type: parseExpr(spelling)}}})
					if l.scalarNames == nil {
						l.scalarNames = map[types.Type]string{}
					}
					l.scalarNames[t] = typeName
				}
			}
			if typeName != "" {
				name := l.temp()
				*pre = append(*pre, &ast.DeclStmt{Decl: &ast.GenDecl{Tok: token.VAR, Specs: []ast.Spec{&ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent(name)}, Type: ast.NewIdent(typeName), Values: []ast.Expr{value}}}}})
				return ast.NewIdent(name)
			}
		}
	}
	// Some valid literal contexts use private named scalar types exported
	// through a public container. Obtain a zero of that type from its context,
	// never by evaluating a user expression or spelling the private type.
	_, scalar := underlyingType(tv.Type).(*types.Basic)
	if witness := l.witnesses[original]; witness != nil && scalar {
		name := l.temp()
		*pre = append(*pre,
			&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{witness()}},
			&ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{value}})
		return ast.NewIdent(name)
	}

	name := l.temp()
	*pre = append(*pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{value}})
	return ast.NewIdent(name)
}

// indexBase preserves array storage and value category. In particular, taking
// a temporary copy of an addressable array would make slicing detach storage,
// while indexing a temporary variable for an rvalue array would invent an
// addressable element (and permit otherwise illegal pointer-receiver calls).
func (l *lowering) indexBase(original ast.Expr, slicing bool, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	tv := l.info.Types[original]
	array := false
	if tv.Type != nil {
		_, array = tv.Type.Underlying().(*types.Array)
	}
	if slicing && array && !tv.Addressable() {
		return nil, nil, l.fail(original, "cannot slice an unaddressable array, including an array returned by ?")
	}
	pre, value, err := l.expr(original, ft)
	if err != nil {
		return nil, nil, err
	}
	if array && tv.Addressable() {
		name := l.temp()
		pre = append(pre, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.DEFINE, Rhs: []ast.Expr{&ast.UnaryExpr{Op: token.AND, X: value}}})
		return pre, ast.NewIdent(name), nil
	}
	value = l.freezeValue(original, value, &pre)
	if array {
		if l.arrayValueHelper == "" {
			l.arrayValueHelper = "__goxide_qarray_" + l.cfg.Namespace + l.temp()
			decl, err := parser.ParseFile(l.fs, "<array value category>", "package "+l.file.Name.Name+";func "+l.arrayValueHelper+"[T interface{}](value T) T {return value}", 0)
			if err != nil {
				return nil, nil, err
			}
			l.file.Decls = append(l.file.Decls, decl.Decls...)
		}
		value = &ast.CallExpr{Fun: ast.NewIdent(l.arrayValueHelper), Args: []ast.Expr{value}}
	}
	return pre, value, nil
}

// Keep ordinary generated packages dependency-free when the universe name is
// available. A real source declaration named comparable requires a qualified
// compiler-support constraint; a package-local alias would be captured too.
func (l *lowering) mapConstraint() string {
	if t := l.resolveType("comparable"); t != nil && types.Identical(t, types.Universe.Lookup("comparable").Type()) {
		return "comparable"
	}
	if l.comparableConstraint == "" {
		name := "__goxide_qconstraints_" + l.cfg.Namespace
		spec := &ast.ImportSpec{Name: ast.NewIdent(name), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote("github.com/hxsf/goxide/runtime")}}
		// Imports must precede declarations even when the original file had none.
		l.file.Decls = append([]ast.Decl{&ast.GenDecl{Tok: token.IMPORT, Specs: []ast.Spec{spec}}}, l.file.Decls...)
		l.file.Imports = append(l.file.Imports, spec)
		l.comparableConstraint = name + ".Comparable"
	}
	return l.comparableConstraint
}

// witnessCall builds compiler-only zero-value helpers. The argument is itself
// a zero witness or a static function identity, never an evaluated user result.
// Type inference can therefore retain an
// inaccessible named element type without reading storage or invoking methods.
func (l *lowering) witnessCall(kind string, arg ast.Expr) ast.Expr {
	if l.witnessHelpers == nil {
		l.witnessHelpers = map[string]string{}
	}
	name := l.witnessHelpers[kind]
	if name == "" {
		name = "__goxide_qzero_" + l.cfg.Namespace + l.temp()
		signature := "[T interface{}]() (zero T)"
		switch {
		case strings.HasPrefix(kind, "function-result-"):
			count, err := strconv.Atoi(strings.TrimPrefix(kind, "function-result-"))
			if err != nil {
				panic(err)
			}
			params := make([]string, count)
			for i := range params {
				params[i] = fmt.Sprintf("A%d", i)
			}
			types := append(append([]string(nil), params...), "R")
			signature = "[" + strings.Join(types, ",") + " interface{}](callee func(" + strings.Join(params, ",") + ") R) (zero R)"
		case kind == "slice":
			signature = "[S ~[]E, E interface{}](container S) (zero E)"
		case kind == "pointer":
			signature = "[P ~*E, E interface{}](container P) (zero E)"
		case kind == "map-key":
			signature = "[M ~map[K]V, K " + l.mapConstraint() + ", V interface{}](container M) (zero K)"
		case kind == "map-value":
			signature = "[M ~map[K]V, K " + l.mapConstraint() + ", V interface{}](container M) (zero V)"
		case strings.HasPrefix(kind, "array-"):
			signature = "[A ~[" + strings.TrimPrefix(kind, "array-") + "]E, E interface{}](container A) (zero E)"
		}
		decl, err := parser.ParseFile(l.fs, "<literal contextual type>", "package "+l.file.Name.Name+";func "+name+signature+"{return}", 0)
		if err != nil {
			panic(err)
		}
		l.file.Decls = append(l.file.Decls, decl.Decls...)
		l.witnessHelpers[kind] = name
	}
	if kind == "root" {
		return &ast.CallExpr{Fun: &ast.IndexExpr{X: ast.NewIdent(name), Index: arg}}
	}
	return &ast.CallExpr{Fun: ast.NewIdent(name), Args: []ast.Expr{arg}}
}

// Stage all literal leaves while retaining the enclosing literal. An omitted
// element type may not be nameable (e.g. []dep.private), and may imply &T.
// Keeping that syntax in its original context preserves both properties and
// leaves constant-key checks and map insertion order to Go.
func (l *lowering) literal(lit *ast.CompositeLit, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	return l.contextLiteral(lit, ft, nil)
}

func (l *lowering) contextLiteral(lit *ast.CompositeLit, ft *ast.FuncType, witness func() ast.Expr) ([]ast.Stmt, ast.Expr, error) {
	var pre []ast.Stmt
	var kind types.Type
	if typ := l.info.TypeOf(lit); typ != nil {
		kind = typ.Underlying()
	}
	if lit.Type != nil {
		typeExpr := lit.Type
		if array, ok := typeExpr.(*ast.ArrayType); ok {
			if _, ellipsis := array.Len.(*ast.Ellipsis); ellipsis {
				if t, ok := kind.(*types.Array); ok {
					typeExpr = &ast.ArrayType{Len: parseExpr(strconv.FormatInt(t.Len(), 10)), Elt: array.Elt}
				}
			}
		}
		witness = func() ast.Expr { return l.witnessCall("root", typeExpr) }
	}
	if pointer, ok := kind.(*types.Pointer); ok {
		kind = pointer.Elem().Underlying()
		if parent := witness; parent != nil {
			witness = func() ast.Expr { return l.witnessCall("pointer", parent()) }
		}
	}
	stage := func(original ast.Expr, context func() ast.Expr) (ast.Expr, error) {
		if nested, ok := original.(*ast.CompositeLit); ok {
			p, value, err := l.contextLiteral(nested, ft, context)
			pre = append(pre, p...)
			return value, err
		}
		if context != nil {
			expected := l.info.TypeOf(original)
			if l.witnesses == nil {
				l.witnesses = map[ast.Expr]func() ast.Expr{}
			}
			ast.Inspect(original, func(node ast.Node) bool {
				if _, closure := node.(*ast.FuncLit); closure {
					return false
				}
				if expr, ok := node.(ast.Expr); ok && expected != nil && types.Identical(l.info.TypeOf(expr), expected) {
					l.witnesses[expr] = context
				}
				return true
			})
		}
		p, value, err := l.expr(original, ft)
		if err != nil {
			return nil, err
		}
		pre = append(pre, p...)
		return l.freezeValue(original, value, &pre), nil
	}
	context := func(field string, key bool) func() ast.Expr {
		if witness == nil {
			return nil
		}
		switch t := kind.(type) {
		case *types.Struct:
			return func() ast.Expr { return &ast.SelectorExpr{X: witness(), Sel: ast.NewIdent(field)} }
		case *types.Slice:
			return func() ast.Expr { return l.witnessCall("slice", witness()) }
		case *types.Array:
			return func() ast.Expr { return l.witnessCall("array-"+strconv.FormatInt(t.Len(), 10), witness()) }
		case *types.Map:
			part := "map-value"
			if key {
				part = "map-key"
			}
			return func() ast.Expr { return l.witnessCall(part, witness()) }
		}
		return nil
	}
	_, isMap := kind.(*types.Map)
	for i, element := range lit.Elts {
		if pair, ok := element.(*ast.KeyValueExpr); ok {
			field := ""
			if isMap {
				key, err := stage(pair.Key, context("", true))
				if err != nil {
					return nil, nil, err
				}
				pair.Key = key
			} else if contains(pair.Key, l.questions) {
				return nil, nil, l.fail(pair.Key, "? cannot be a struct field selector or constant array/slice literal index")
			} else if _, structure := kind.(*types.Struct); structure {
				field = printNode(pair.Key)
			}
			value, err := stage(pair.Value, context(field, false))
			if err != nil {
				return nil, nil, err
			}
			pair.Value = value
		} else {
			field := ""
			if structure, ok := kind.(*types.Struct); ok && i < structure.NumFields() {
				field = structure.Field(i).Name()
			}
			value, err := stage(element, context(field, false))
			if err != nil {
				return nil, nil, err
			}
			lit.Elts[i] = value
		}
	}
	return pre, lit, nil
}

func (l *lowering) builtin(call *ast.CallExpr) *types.Builtin {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	}
	if id != nil {
		b, _ := l.info.Uses[id].(*types.Builtin)
		return b
	}
	return nil
}

func (l *lowering) expr(x ast.Expr, ft *ast.FuncType) ([]ast.Stmt, ast.Expr, error) {
	if fn := matchAtom(x); fn != nil {
		return l.matchAtomExpr(fn, ft)
	}
	if !contains(x, l.questions) {
		return nil, x, nil
	}
	switch n := x.(type) {
	case *ast.ParenExpr:
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	case *ast.CallExpr:
		builtin := l.builtin(n)
		if builtin != nil && builtin.Pkg() != nil && builtin.Pkg().Path() == "unsafe" && (builtin.Name() == "Sizeof" || builtin.Name() == "Alignof" || builtin.Name() == "Offsetof") {
			return nil, nil, l.fail(n, "? in unsafe compile-time operands is unsupported; lowering must not evaluate an unevaluated operand")
		}
		forceArrayArgument := false
		if builtin != nil && (builtin.Name() == "len" || builtin.Name() == "cap") && len(n.Args) == 1 && l.info.Types[n].Value == nil {
			if typ := l.info.TypeOf(n.Args[0]); typ != nil {
				underlying := typ.Underlying()
				if pointer, ok := underlying.(*types.Pointer); ok {
					underlying = pointer.Elem().Underlying()
				}
				_, forceArrayArgument = underlying.(*types.Array)
			}
		}
		if id, ok := n.Fun.(*ast.Ident); ok {
			if d, yes := l.questions[id.Name]; yes {
				p, arg, e := l.expr(n.Args[0], ft)
				if e != nil {
					return nil, nil, e
				}
				flow, e := l.questionFlow(n, arg, ft, d)
				if e != nil {
					return nil, nil, e
				}
				return append(p, flow.emit(returnFailure)...), flow.value, nil
			}
		}
		// Freeze earlier evaluated runtime operands before lowering a later ?. Constants
		// and type expressions retain contextual typing rather than defaulting to int.
		var pre []ast.Stmt
		freeze := func(original, y ast.Expr) ast.Expr {
			tv := l.info.Types[original]
			if tv.IsType() || tv.IsBuiltin() {
				return y
			}
			if l.staticEffectCallee(original) {
				return y
			}

			return l.freezeValue(original, y, &pre)
		}
		printPlan := false
		if id, ok := n.Fun.(*ast.Ident); ok {
			printPlan = id.Name == "__goxide_print" || id.Name == "__goxide_println"
		}
		var p []ast.Stmt
		var y ast.Expr
		var e error
		if selector, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok && l.info.Selections[selector] != nil && l.info.Selections[selector].Kind() == types.MethodVal {
			p, y, e = l.methodReceiver(selector.X, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			selector.X = y
		} else {
			p, y, e = l.expr(n.Fun, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			if printPlan {
				n.Fun = y
			} else {
				n.Fun = freeze(n.Fun, y)
			}
		}
		for i, a := range n.Args {
			p, y, e = l.expr(a, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			if (i < len(n.Args)-1 || forceArrayArgument) && !(printPlan && i == 0) {
				y = freeze(a, y)
			}
			n.Args[i] = y
		}
		return pre, n, nil
	case *ast.BinaryExpr:
		// A private named bool cannot be spelled in a temporary declaration.
		// When an untyped left operand gets its type from the right arm, reuse
		// the preflighted static type witness before either arm is rewritten.
		// The witness does not execute the unselected arm or introduce a frame.
		if n.Op == token.LAND || n.Op == token.LOR {
			fact := l.expressionFacts[n]
			if fact.typ != nil && !fact.untyped && l.expressionFacts[n.X].untyped {
				if _, nameable := l.typeSpelling(fact.typ); !nameable && !l.hasUnsafePointerConversion(n) && l.canEffect(n) {
					return l.effectExpr(n, ft)
				}
			}
		}
		p, left, e := l.expr(n.X, ft)
		if e != nil {
			return nil, nil, e
		}
		q, right, e := l.expr(n.Y, ft)
		if e != nil {
			return nil, nil, e
		}
		if n.Op == token.LAND || n.Op == token.LOR {
			stored := l.freezeValue(n, left, &p)
			name := stored.(*ast.Ident).Name
			cond := ast.Expr(ast.NewIdent(name))
			if n.Op == token.LOR {
				cond = &ast.UnaryExpr{Op: token.NOT, X: cond}
			}
			q = append(q, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(name)}, Tok: token.ASSIGN, Rhs: []ast.Expr{right}})
			p = append(p, &ast.IfStmt{Cond: cond, Body: &ast.BlockStmt{List: q}})
			return p, ast.NewIdent(name), nil
		}
		if len(q) > 0 && l.info.Types[n.X].Value == nil {
			left = l.freezeValue(n.X, left, &p)
		}
		n.X, n.Y = left, right
		return append(p, q...), n, nil
	case *ast.IndexExpr:
		pre, base, err := l.indexBase(n.X, false, ft)
		if err != nil {
			return nil, nil, err
		}
		p, index, err := l.expr(n.Index, ft)
		if err != nil {
			return nil, nil, err
		}
		n.X, n.Index = base, index
		return append(pre, p...), n, nil
	case *ast.SliceExpr:
		pre, base, err := l.indexBase(n.X, true, ft)
		if err != nil {
			return nil, nil, err
		}
		n.X = base
		for _, bound := range []*ast.Expr{&n.Low, &n.High, &n.Max} {
			if *bound == nil {
				continue
			}
			original := *bound
			p, value, err := l.expr(original, ft)
			if err != nil {
				return nil, nil, err
			}
			pre = append(pre, p...)
			*bound = l.freezeValue(original, value, &pre)
		}
		return pre, n, nil
	case *ast.CompositeLit:
		return l.literal(n, ft)
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			if _, literal := ast.Unparen(n.X).(*ast.CompositeLit); !literal {
				return nil, nil, l.fail(n, "taking the address of a propagation expression is unsupported except for a composite literal")
			}
		}
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	case *ast.StarExpr:
		p, y, e := l.expr(n.X, ft)
		n.X = y
		return p, n, e
	case *ast.TypeAssertExpr:
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
