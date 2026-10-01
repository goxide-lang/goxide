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
}
type lowering struct {
	fs          *token.FileSet
	file        *ast.File
	info        *types.Info
	pkg         *types.Package
	cfg         Config
	serial      int
	questions   map[string]enums.Decl
	conversions map[string]string
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
	l.pkg, _ = c.Check("hgo/generated", l.fs, []*ast.File{l.file}, l.info)
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
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() != l.pkg {
		return enums.Decl{}, false
	}
	for _, d := range l.cfg.Families {
		if d.Family != "" && types.Identical(n, l.resolveType(d.Name)) {
			return d, true
		}
	}
	return enums.Decl{}, false
}

// Lower replaces the internal markers with branches in the original function.
// It intentionally rejects unsupported expression contexts instead of moving
// evaluation or return into a hidden closure.
func Lower(filename string, source []byte, cfg Config) ([]byte, error) {
	if !bytes.Contains(source, []byte(marker+"(")) {
		return source, nil
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, filename, source, parser.ParseComments)
	if e != nil {
		return nil, e
	}
	originalNodes := sourcemap.Nodes(f)
	l := &lowering{fs: fs, file: f, cfg: cfg, questions: map[string]enums.Decl{}, conversions: map[string]string{}}
	// Seed a temporary identity declaration: this makes operand types available,
	// including operands with inference, before replacing each marker's result.
	seed, _ := parser.ParseFile(fs, "<propagation signatures>", "package "+f.Name.Name+";func "+marker+"[T any](v T) T {return v}", 0)
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
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil && !strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_") && fn.Name.Name != marker {
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
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_")) {
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
		if fn, ok := decl.(*ast.FuncDecl); ok && (fn.Name.Name == marker || strings.HasPrefix(fn.Name.Name, "__hgo_qtmp_")) {
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
		y, e := lower(x.X)
		if e != nil {
			return nil, e
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
		if x.Init != nil && contains(x.Init, l.questions) {
			return nil, l.fail(x.Init, "? in if initializer is not supported; use a preceding declaration")
		}
		if x.Init != nil && contains(x.Cond, l.questions) {
			return nil, l.fail(x, "? condition with if initializer is not supported")
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
						bound := l.conversions[helper]
						if bound == "" {
							l.serial++
							bound = fmt.Sprintf("__hgo_qfrom_%d", l.serial)
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
		p, y, e := l.expr(n.Fun, ft)
		if e != nil {
			return nil, nil, e
		}
		pre = append(pre, p...)
		n.Fun = freeze(n.Fun, y)
		for i, a := range n.Args {
			p, y, e = l.expr(a, ft)
			if e != nil {
				return nil, nil, e
			}
			pre = append(pre, p...)
			if i < len(n.Args)-1 {
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
