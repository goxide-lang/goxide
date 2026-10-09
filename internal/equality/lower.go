// Package equality implements explicit, typed equality derivation and calls.
// It does not grant equality by Go comparability or by finding a method name.
package equality

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

type Derive struct {
	PartialEq, Eq             bool
	ManualPartialEq, ManualEq bool
	File                      string
}
type Protocol struct {
	PackagePath, TypeName, Helper string
	PartialEq, Eq                 bool
}
type Config struct {
	Importer    types.Importer
	PackagePath string
	Derives     map[string]Derive
	Enums       []enums.Decl
	Imported    []Protocol
	Maps        map[string]*sourcemap.Map
	Native      map[string]bool
}

const intrinsic = "__goxide_enum_equals"
const shim = "\nfunc __goxide_enum_equals[T interface{}](a,b T)bool{return (0!=0)}\n"

type unit struct {
	fs       *token.FileSet
	names    []string
	trees    map[string]*ast.File
	list     []*ast.File
	pkg      *types.Package
	info     *types.Info
	evidence map[string]*sourcemap.ASTEvidence
	changed  map[string]bool
}

func parse(files map[string][]byte) (*unit, error) {
	u := &unit{fs: token.NewFileSet(), trees: map[string]*ast.File{}}
	for name := range files {
		u.names = append(u.names, name)
	}
	sort.Strings(u.names)
	for _, name := range u.names {
		f, e := parser.ParseFile(u.fs, name, files[name], parser.ParseComments)
		if e != nil {
			return nil, e
		}
		u.trees[name] = f
		u.list = append(u.list, f)
	}
	return u, nil
}

// Freeze the matching input AST/map before any helper insertion, renaming or
// call rewrite. Token-stream composition cannot preserve untouched expressions
// once a derive has appended declarations to the same file.
func (u *unit) capture(cfg Config) {
	u.evidence = map[string]*sourcemap.ASTEvidence{}
	u.changed = map[string]bool{}
	for _, name := range u.names {
		if !cfg.Native[name] && cfg.Maps[name] != nil {
			u.evidence[name] = cfg.Maps[name].CaptureAST(u.fs, u.trees[name])
		}
	}
}

func (u *unit) generatedIdent(file, name string, cause *ast.Ident) *ast.Ident {
	u.changed[file] = true
	id := ast.NewIdent(name)
	evidence := u.evidence[file]
	evidence.Generated(id, evidence.Context(cause, sourcemap.IdentToken))
	return id
}

func (u *unit) check(cfg Config, ignoreBodies, tolerate bool) error {
	u.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	path := cfg.PackagePath
	if path == "" {
		path = "goxide/generated"
	}
	conf := types.Config{Importer: cfg.Importer, IgnoreFuncBodies: ignoreBodies}
	if tolerate {
		conf.Error = func(error) {}
	}
	p, e := conf.Check(path, u.fs, u.list, u.info)
	u.pkg = p
	if tolerate {
		return nil
	}
	return e
}
func write(u *unit, files map[string][]byte, cfg Config, stage string) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, name := range u.names {
		if cfg.Native[name] {
			out[name] = files[name]
			continue
		}
		var b bytes.Buffer
		if e := format.Node(&b, u.fs, u.trees[name]); e != nil {
			return nil, e
		}
		out[name] = b.Bytes()
		if cfg.Maps[name] != nil {
			if u.changed[name] {
				cfg.Maps[name] = u.evidence[name].Print(u.trees[name], out[name], stage)
			} else {
				// This file was only printed. Keep the existing full-token proof,
				// including punctuation without an owned AST token slot.
				cfg.Maps[name] = cfg.Maps[name].Compose(files[name], out[name], stage)
			}
		}
	}
	return out, nil
}

type registered struct {
	typ        types.Type
	helper     string
	flags      Derive
	name, file string
	bound      string
	enum       *enums.Decl
}
type generator struct {
	u            *unit
	cfg          Config
	protocols    []*registered
	local        map[string]*registered
	state        map[string]int
	code         map[string]string
	serial       int
	file         string
	callBindings map[*ast.CallExpr]map[string]string
}

func registry(u *unit, cfg Config) (*generator, error) {
	g := &generator{u: u, cfg: cfg, local: map[string]*registered{}, state: map[string]int{}, code: map[string]string{}}
	names := make([]string, 0, len(cfg.Derives))
	for n := range cfg.Derives {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		flags := cfg.Derives[name]
		if flags.ManualPartialEq && !flags.PartialEq || flags.ManualEq && !flags.Eq {
			return nil, fmt.Errorf("equality: inconsistent manual protocol flags for %s", name)
		}
		if flags.ManualPartialEq && flags.Eq && !flags.ManualEq {
			return nil, fmt.Errorf("equality: derive Eq cannot prove manual PartialEq for %s; implement Eq explicitly", name)
		}
		if flags.Eq && !flags.PartialEq {
			return nil, fmt.Errorf("equality: Eq requires explicit PartialEq for %s", name)
		}
		if !flags.PartialEq && !flags.Eq {
			continue
		}
		obj, ok := u.pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			return nil, fmt.Errorf("equality: missing derive type %s", name)
		}
		t, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || obj.IsAlias() || t.TypeParams().Len() != 0 {
			return nil, fmt.Errorf("equality: derive requires a concrete defined type: %s", name)
		}
		file := u.fs.Position(obj.Pos()).Filename
		if flags.File != "" {
			file = flags.File
		}
		if u.trees[file] == nil {
			return nil, fmt.Errorf("equality: missing source for %s", name)
		}
		r := &registered{typ: t, helper: "Equals" + name, flags: flags, name: name, file: file}
		for i := range cfg.Enums {
			d := &cfg.Enums[i]
			tv, e := types.Eval(u.fs, u.pkg, token.NoPos, d.Name)
			if e == nil && types.Identical(tv.Type, t) {
				r.enum = d
				break
			}
		}
		if flags.ManualPartialEq && r.enum == nil {
			switch underlying := t.Underlying().(type) {
			case *types.Struct, *types.Array, *types.Slice, *types.Map:
			case *types.Basic:
				if !basic(underlying, false) {
					return nil, fmt.Errorf("equality: unsupported manual protocol target %s", name)
				}
			default:
				return nil, fmt.Errorf("equality: unsupported manual protocol target %s", name)
			}
		}
		g.protocols = append(g.protocols, r)
		g.local[name] = r
	}
	for _, p := range cfg.Imported {
		if p.Eq && !p.PartialEq {
			return nil, fmt.Errorf("equality: imported Eq requires PartialEq: %s", p.TypeName)
		}
		if !p.PartialEq && !p.Eq {
			continue
		}
		tv, e := types.Eval(u.fs, u.pkg, token.NoPos, p.TypeName)
		if e != nil {
			return nil, fmt.Errorf("equality: imported type binding %s: %w", p.TypeName, e)
		}
		hv, e := types.Eval(u.fs, u.pkg, token.NoPos, p.Helper)
		if e != nil {
			return nil, fmt.Errorf("equality: imported helper binding %s: %w", p.Helper, e)
		}
		sig, ok := hv.Type.(*types.Signature)
		if !ok || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 2 || sig.Results().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), tv.Type) || !types.Identical(sig.Params().At(1).Type(), tv.Type) || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool]) {
			return nil, fmt.Errorf("equality: imported helper signature mismatch: %s", p.Helper)
		}
		n, ok := types.Unalias(tv.Type).(*types.Named)
		if !ok || n.Obj().Pkg() == nil || p.PackagePath != "" && n.Obj().Pkg().Path() != p.PackagePath {
			return nil, fmt.Errorf("equality: imported type identity mismatch: %s", p.TypeName)
		}
		r := &registered{typ: tv.Type, helper: p.Helper, flags: Derive{PartialEq: p.PartialEq, Eq: p.Eq}}
		// Multiple file bindings for one nominal protocol are equivalent, provided
		// both capabilities agree. A package-level binding avoids caller shadowing.
		prior := g.find(tv.Type)
		if prior != nil {
			if prior.flags.PartialEq != r.flags.PartialEq || prior.flags.Eq != r.flags.Eq {
				return nil, fmt.Errorf("equality: conflicting imported protocol for %s", p.TypeName)
			}
			continue
		}
		g.protocols = append(g.protocols, r)
	}
	for i, r := range g.protocols {
		r.bound = fmt.Sprintf("__goxide_enum_eq_bound_%d", i)
		if r.name != "" {
			r.bound = r.helper
		}
	}
	return g, nil
}
func (g *generator) find(t types.Type) *registered {
	if t == nil {
		return nil
	}
	for _, p := range g.protocols {
		if types.Identical(types.Unalias(t), types.Unalias(p.typ)) {
			return p
		}
	}
	return nil
}

// Local helper function values must not be stored in package variables: a
// recursive manual body would make those variables form an initialization cycle.
// Rename only lexical bindings, by object identity, before emitting direct calls.
func (g *generator) protectLocalHelpers() {
	helpers := map[string]bool{}
	for _, r := range g.protocols {
		if r.name != "" {
			helpers[r.helper] = true
		}
	}
	used := map[string]bool{}
	editable := map[*ast.Ident]bool{}
	owners := map[*ast.Ident]string{}
	for _, name := range g.u.names {
		ast.Inspect(g.u.trees[name], func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				used[id.Name] = true
				if !g.cfg.Native[name] {
					editable[id] = true
					owners[id] = name
				}
			}
			return true
		})
	}
	renamed := map[types.Object]string{}
	serial := 0
	ids := make([]*ast.Ident, 0, len(g.u.info.Defs))
	for id := range g.u.info.Defs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Pos() < ids[j].Pos() })
	for _, id := range ids {
		obj := g.u.info.Defs[id]
		if obj == nil || !editable[id] || !helpers[obj.Name()] || obj.Parent() == g.u.pkg.Scope() {
			continue
		}
		switch o := obj.(type) {
		case *types.Var:
			if o.IsField() {
				continue
			}
		case *types.TypeName:
			if _, ok := o.Type().(*types.TypeParam); !ok {
				continue
			}
		case *types.Const:
		default:
			continue
		}
		for {
			serial++
			candidate := fmt.Sprintf("__goxide_enum_eq_local_%d", serial)
			if !used[candidate] {
				used[candidate] = true
				renamed[obj] = candidate
				break
			}
		}
	}
	for id, obj := range g.u.info.Defs {
		if name := renamed[obj]; name != "" && editable[id] {
			g.u.changed[owners[id]] = true
			id.Name = name
		}
	}
	for id, obj := range g.u.info.Uses {
		if name := renamed[obj]; name != "" && editable[id] {
			g.u.changed[owners[id]] = true
			id.Name = name
		}
	}
	// Local defined type names are observable through reflection. Preserve them,
	// capturing the helper in the enclosing function before their lexical scopes.
	g.callBindings = map[*ast.CallExpr]map[string]string{}
	for _, file := range g.u.names {
		if g.cfg.Native[file] {
			continue
		}
		ast.Inspect(g.u.trees[file], func(node ast.Node) bool {
			var body *ast.BlockStmt
			switch f := node.(type) {
			case *ast.FuncDecl:
				body = f.Body
			case *ast.FuncLit:
				body = f.Body
			default:
				return true
			}
			if body == nil {
				return false
			}
			conflicts := map[string]bool{}
			ast.Inspect(body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if obj, ok := g.u.info.Defs[id].(*types.TypeName); ok && helpers[obj.Name()] {
						if _, param := obj.Type().(*types.TypeParam); !param {
							conflicts[obj.Name()] = true
						}
					}
				}
				return true
			})
			names := []string{}
			for name := range conflicts {
				names = append(names, name)
			}
			sort.Strings(names)
			bindings := map[string]string{}
			var prefix []ast.Stmt
			for _, name := range names {
				var alias string
				for {
					serial++
					alias = fmt.Sprintf("__goxide_enum_eq_local_%d", serial)
					if !used[alias] {
						used[alias] = true
						break
					}
				}
				bindings[name] = alias
				prefix = append(prefix, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent(alias)}, Tok: token.DEFINE, Rhs: []ast.Expr{ast.NewIdent(name)}}, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent(alias)}})
			}
			if len(bindings) > 0 {
				g.u.changed[file] = true
				// These bindings serve all calls in the function, with no unique
				// macro cause. Register only the closed, newly generated prefix.
				for _, stmt := range prefix {
					g.u.evidence[file].GeneratedTree(stmt, sourcemap.ASTContext{})
				}
				ast.Inspect(body, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						g.callBindings[call] = bindings
					}
					return true
				})
				body.List = append(prefix, body.List...)
			}
			return false
		})
	}

}
func (g *generator) callHelper(call *ast.CallExpr, r *registered) string {
	if name := g.callBindings[call][r.helper]; name != "" {
		return name
	}
	return r.bound
}
func basic(t types.Type, eq bool) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	if !ok {
		return false
	}
	if b.Info()&(types.IsBoolean|types.IsInteger|types.IsString|types.IsFloat|types.IsComplex) == 0 {
		return false
	}
	return !eq || b.Info()&(types.IsFloat|types.IsComplex) == 0
}
func (g *generator) expression(t types.Type, a, b string, eq bool) (string, error) {
	t = types.Unalias(t)
	if r := g.find(t); r != nil {
		if eq && !r.flags.Eq {
			return "", fmt.Errorf("field type %s has no explicit Eq protocol", t)
		}
		if r.name != "" {
			if e := g.generate(r); e != nil {
				return "", e
			}
		}
		return fmt.Sprintf("%s(%s,%s)", r.helper, a, b), nil
	}
	if basic(t, eq) {
		return "(" + a + " == " + b + ")", nil
	}
	if n, ok := t.(*types.Named); ok {
		if expression, known, e := g.sumExpression(n, a, b, eq); known {
			return expression, e
		}
		return "", fmt.Errorf("named type %s requires an explicit %s protocol", n, equalityKind(eq))
	}
	return g.structural(t, a, b, eq)
}
func equalityKind(eq bool) string {
	if eq {
		return "Eq"
	}
	return "PartialEq"
}
func (g *generator) structural(t types.Type, a, b string, eq bool) (string, error) {
	if basic(t, eq) {
		return "(" + a + " == " + b + ")", nil
	}
	switch t := t.(type) {
	case *types.Struct:
		parts := []string{}
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if f.Name() == "_" || f.Embedded() {
				return "", fmt.Errorf("blank or embedded fields have no derived equality protocol")
			}
			x, e := g.expression(f.Type(), a+"."+f.Name(), b+"."+f.Name(), eq)
			if e != nil {
				return "", fmt.Errorf("field %s: %w", f.Name(), e)
			}
			parts = append(parts, x)
		}
		if len(parts) == 0 {
			return "(0==0)", nil
		}
		return "(" + strings.Join(parts, " && ") + ")", nil
	case *types.Slice:
		if e := g.containerBuiltins(); e != nil {
			return "", e
		}
		g.serial++
		idx := fmt.Sprintf("__goxide_enum_eq_i%d", g.serial)
		x, e := g.expression(t.Elem(), a+"["+idx+"]", b+"["+idx+"]", eq)
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("func()bool{if (%s==nil)!=(%s==nil){return (0!=0)};if len(%s)!=len(%s){return (0!=0)};for %s:=range %s{if !(%s){return (0!=0)}};return (0==0)}()", a, b, a, b, idx, a, x), nil
	case *types.Map:
		if e := g.containerBuiltins(); e != nil {
			return "", e
		}
		if e := g.mapKey(t.Key(), eq); e != nil {
			return "", e
		}
		g.serial++
		prefix := fmt.Sprintf("__goxide_enum_eq_m%d", g.serial)
		key, left, right, ok := prefix+"k", prefix+"a", prefix+"b", prefix+"ok"
		x, e := g.expression(t.Elem(), left, right, eq)
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("func()bool{if (%s==nil)!=(%s==nil){return (0!=0)};if len(%s)!=len(%s){return (0!=0)};for %s,%s:=range %s{%s,%s:=%s[%s];if !%s{return (0!=0)};if !(%s){return (0!=0)}};return (0==0)}()", a, b, a, b, key, left, a, right, ok, b, key, ok, x), nil
	case *types.Array:
		g.serial++
		idx := fmt.Sprintf("__goxide_eq_i%d", g.serial)
		x, e := g.expression(t.Elem(), a+"["+idx+"]", b+"["+idx+"]", eq)
		if e != nil {
			return "", e
		}
		return fmt.Sprintf("func()bool{for %s:=0;%s<%d;%s++{if !(%s){return (0!=0)}};return (0==0)}()", idx, idx, t.Len(), idx, x), nil
	}
	return "", fmt.Errorf("type %s has no automatic %s protocol", t, equalityKind(eq))
}
func (g *generator) generate(r *registered) error {
	if g.state[r.name] == 2 {
		return nil
	}
	if g.state[r.name] == 1 {
		return fmt.Errorf("equality: recursive protocol dependency at %s", r.name)
	}
	if r.flags.ManualPartialEq {
		g.state[r.name] = 2
		return nil
	}
	g.state[r.name] = 1
	previousFile := g.file
	g.file = r.file
	defer func() { g.file = previousFile }()
	requireEq := r.flags.Eq && !r.flags.ManualEq
	var body string
	if r.enum != nil {
		d := r.enum
		if d.Family != "" {
			return fmt.Errorf("equality: generic builtin derive is unsupported")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s(a);%s(b);switch x:=a.(type){", d.Validator, d.Validator)
		for _, v := range d.Variants {
			tv, e := types.Eval(g.u.fs, g.u.pkg, token.NoPos, v.CaseName)
			if e != nil {
				return fmt.Errorf("equality: enum case %s: %w", v.CaseName, e)
			}
			st, ok := types.Unalias(tv.Type).Underlying().(*types.Struct)
			if !ok {
				return fmt.Errorf("equality: invalid enum value case %s", v.CaseName)
			}
			fmt.Fprintf(&b, "case %s:y,ok:=b.(%s);if !ok{return (0!=0)};_=x;_=y;", v.CaseName, v.CaseName)
			if v.Payload == "" {
				b.WriteString("return (0==0);")
			} else {
				if st.NumFields() != 1 || st.Field(0).Name() != "Value" {
					return fmt.Errorf("equality: invalid enum payload case %s", v.CaseName)
				}
				x, e := g.expression(st.Field(0).Type(), "x.Value", "y.Value", requireEq)
				if e != nil {
					return fmt.Errorf("equality: %s::%s: %w", r.name, v.Name, e)
				}
				fmt.Fprintf(&b, "return %s;", x)
			}
		}
		b.WriteString("};return (0!=0)")
		body = b.String()
	} else {
		x, e := g.structural(r.typ.Underlying(), "a", "b", requireEq)
		if e != nil {
			return fmt.Errorf("equality: derive %s for %s: %w", equalityKind(r.flags.Eq), r.name, e)
		}
		body = "return " + x
	}
	g.code[r.name] = fmt.Sprintf("\nfunc %s(a,b %s)bool{%s}\n", r.helper, r.name, body)
	g.state[r.name] = 2
	return nil
}

// prepareManual validates the sole public implementation and guards that very
// entry point for enums; calls from native Go therefore cannot bypass validation.
func (g *generator) prepareManual(r *registered) error {
	obj, ok := g.u.pkg.Scope().Lookup(r.helper).(*types.Func)
	if !ok {
		return fmt.Errorf("equality: manual PartialEq requires function %s", r.helper)
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 2 || sig.Results().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), r.typ) || !types.Identical(sig.Params().At(1).Type(), r.typ) || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool]) {
		return fmt.Errorf("equality: manual helper signature mismatch: %s", r.helper)
	}
	var decl *ast.FuncDecl
	var file string
	for _, name := range g.u.names {
		for _, d := range g.u.trees[name].Decls {
			if f, ok := d.(*ast.FuncDecl); ok && g.u.info.Defs[f.Name] == obj {
				decl = f
				file = name
			}
		}
	}
	if decl == nil || decl.Body == nil || g.cfg.Native[file] {
		return fmt.Errorf("equality: manual helper %s requires an editable function body", r.helper)
	}
	if r.enum == nil {
		return nil
	}
	g.u.changed[file] = true
	vt, e := g.eval(r.enum.Validator)
	vs, ok := vt.(*types.Signature)
	if e != nil || !ok || vs.Variadic() || vs.TypeParams().Len() != 0 || vs.Params().Len() != 1 || vs.Results().Len() != 0 || !types.Identical(vs.Params().At(0).Type(), r.typ) {
		return fmt.Errorf("equality: invalid manual enum validator for %s", r.name)
	}
	bound := "__goxide_enum_eq_manual_validator_" + r.name
	// Bind outside the function so a user parameter named like the validator
	// cannot capture the compiler-inserted call.
	g.code[r.name] = "\nvar " + bound + " = " + r.enum.Validator + "\n"
	var args []*ast.Ident
	for _, f := range decl.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == "_" {
				n.Name = fmt.Sprintf("__goxide_enum_eq_manual_arg_%d", len(args))
			}
			args = append(args, n)
		}
	}
	if len(args) != 2 {
		return fmt.Errorf("equality: manual enum helper requires named parameters: %s", r.helper)
	}
	prefix := []ast.Stmt{}
	for _, arg := range args {
		stmt := &ast.ExprStmt{X: &ast.CallExpr{Fun: ast.NewIdent(bound), Args: []ast.Expr{ast.NewIdent(arg.Name)}}}
		g.u.evidence[file].GeneratedTree(stmt, sourcemap.ASTContext{})
		prefix = append(prefix, stmt)
	}
	decl.Body.List = append(prefix, decl.Body.List...)
	return nil
}
func appendDeclarations(u *unit, name, text string) error {
	f, e := parser.ParseFile(u.fs, "<equality helpers>", "package "+u.trees[name].Name.Name+"\n"+text, parser.ParseComments)
	if e != nil {
		return e
	}
	// Shared declarations are not caused by any one macro invocation. Their
	// parser positions belong to a synthetic file, not the destination source.
	for _, decl := range f.Decls {
		u.evidence[name].GeneratedTree(decl, sourcemap.ASTContext{})
	}
	if len(f.Decls) > 0 {
		u.changed[name] = true
	}
	u.trees[name].Decls = append(u.trees[name].Decls, f.Decls...)
	return nil
}
func candidate(call *ast.CallExpr) (*ast.SelectorExpr, bool) {
	s, ok := call.Fun.(*ast.SelectorExpr)
	return s, ok && s.Sel.Name == "equals" && len(call.Args) == 1 && !call.Ellipsis.IsValid()
}

// Prepare generates typed helpers using declarations only, then normalizes
// protocol method syntax so propagation can see its bool result. Native methods
// resolved by Go are preserved; unresolved calls are decided in Lower.
func Prepare(files map[string][]byte, cfg Config) (map[string][]byte, error) {
	if len(cfg.Derives) == 0 {
		active := false
		for name, source := range files {
			if !cfg.Native[name] && bytes.Contains(source, []byte("equals")) {
				active = true
				break
			}
		}
		if !active {
			return files, nil
		}
	}
	u, e := parse(files)
	if e != nil {
		return nil, e
	}
	if len(u.names) == 0 {
		return files, nil
	}
	u.capture(cfg)
	if e = u.check(cfg, true, true); e != nil {
		return nil, e
	}
	g, e := registry(u, cfg)
	if e != nil {
		return nil, e
	}
	if len(g.local) > 0 {
		if e = checkBoolName(u); e != nil {
			return nil, e
		}
	}

	names := make([]string, 0, len(g.local))
	for n := range g.local {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if g.local[n].flags.ManualPartialEq {
			if e = g.prepareManual(g.local[n]); e != nil {
				return nil, e
			}
		} else if u.pkg.Scope().Lookup(g.local[n].helper) != nil {
			return nil, fmt.Errorf("equality: generated helper name collision: %s", g.local[n].helper)
		}
		if e = g.generate(g.local[n]); e != nil {
			return nil, e
		}
	}
	for _, n := range names {
		if e = appendDeclarations(u, g.local[n].file, g.code[n]); e != nil {
			return nil, e
		}
	}
	anchor := ""
	for _, name := range u.names {
		if !cfg.Native[name] {
			anchor = name
			break
		}
	}
	if anchor == "" {
		return files, nil
	}
	for _, r := range g.protocols {
		if r.name != "" {
			continue
		}
		if u.pkg.Scope().Lookup(r.bound) != nil {
			return nil, fmt.Errorf("equality: reserved binding collision: %s", r.bound)
		}
		if e = appendDeclarations(u, anchor, "var "+r.bound+" = "+r.helper); e != nil {
			return nil, e
		}
	}
	// Best-effort body typing recognizes ordinary native methods without requiring
	// question propagation or compiler-owned intrinsics to have run already.
	_ = u.check(cfg, false, true)
	g, e = registry(u, cfg)
	if e != nil {
		return nil, e
	}
	g.protectLocalHelpers()
	needed := false
	for _, name := range u.names {
		if cfg.Native[name] {
			continue
		}
		ast.Inspect(u.trees[name], func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := candidate(call)
			if !ok {
				return true
			}
			receiver := g.receiverType(sel.X)
			r := g.find(receiver)
			if r == nil {
				if u.info.Selections[sel] != nil {
					return true
				}
				if receiver != nil {
					if member, _, _ := types.LookupFieldOrMethod(receiver, true, u.pkg, "equals"); member != nil {
						return true
					}
				}
			}
			if r != nil {
				call.Fun = u.generatedIdent(name, g.callHelper(call, r), sel.Sel)
				call.Args = append([]ast.Expr{sel.X}, call.Args...)
				return true
			}
			if receiver != nil && !basic(receiver, false) {
				return true
			}
			call.Fun = u.generatedIdent(name, intrinsic, sel.Sel)
			call.Args = append([]ast.Expr{sel.X}, call.Args...)
			needed = true
			return true
		})
	}
	if needed {
		if e = checkBoolName(u); e != nil {
			return nil, e
		}
		if u.pkg.Scope().Lookup(intrinsic) != nil {
			return nil, fmt.Errorf("equality: reserved intrinsic name collision")
		}
		if e = appendDeclarations(u, anchor, shim); e != nil {
			return nil, e
		}
	}
	return write(u, files, cfg, "equality.Prepare")
}

// Lower resolves the deferred equality calls after question/match typing.
func Lower(files map[string][]byte, cfg Config) (map[string][]byte, error) {
	active := false
	for _, s := range files {
		active = active || bytes.Contains(s, []byte(intrinsic))
	}
	if !active {
		return files, nil
	}
	u, e := parse(files)
	if e != nil {
		return nil, e
	}
	u.capture(cfg)
	_ = u.check(cfg, false, true)
	g, e := registry(u, cfg)
	if e != nil {
		return nil, e
	}
	g.protectLocalHelpers()
	needScalarHelper := false
	for _, name := range u.names {
		ast.Inspect(u.trees[name], func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || id.Name != intrinsic || len(call.Args) != 2 {
				return true
			}
			if r := g.find(u.info.TypeOf(call.Args[0])); r != nil {
				call.Fun = u.generatedIdent(name, g.callHelper(call, r), id)
			} else if basic(u.info.TypeOf(call.Args[0]), false) {
				call.Fun = u.generatedIdent(name, "__goxide_enum_scalar_equals", id)
				needScalarHelper = true
			} else {
				call.Fun = &ast.SelectorExpr{X: call.Args[0], Sel: u.generatedIdent(name, "equals", id)}
				call.Args = call.Args[1:]
			}
			return true
		})
		keep := u.trees[name].Decls[:0]
		for _, d := range u.trees[name].Decls {
			if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == intrinsic {
				u.changed[name] = true
				continue
			}
			keep = append(keep, d)
		}
		u.trees[name].Decls = keep
	}
	if needScalarHelper {
		if u.pkg.Scope().Lookup("comparable") != nil {
			return nil, fmt.Errorf("equality: scalar comparison requires unshadowed predeclared comparable")
		}
		anchor := ""
		for _, name := range u.names {
			if !cfg.Native[name] {
				anchor = name
				break
			}
		}
		if anchor == "" {
			return nil, fmt.Errorf("equality: no generated source for scalar helper")
		}
		if e = appendDeclarations(u, anchor, "func __goxide_enum_scalar_equals[T comparable](a,b T)bool{return a==b}"); e != nil {
			return nil, e
		}
	}
	out, e := write(u, files, cfg, "equality.Lower")
	if e != nil {
		return nil, e
	}
	checked, e := parse(out)
	if e != nil {
		return nil, e
	}
	if e = checked.check(cfg, false, false); e != nil {
		return nil, e
	}
	return out, nil
}

// Question operands already expose their nominal sum type before propagation.
// Resolve only families supplied by the trusted frontend registry; this lets an
// ordinary native equals method keep its actual (possibly non-bool) result.
func (g *generator) receiverType(x ast.Expr) types.Type {
	if p, ok := x.(*ast.ParenExpr); ok {
		return g.receiverType(p.X)
	}
	if t := g.u.info.TypeOf(x); t != nil {
		if b, ok := t.(*types.Basic); !ok || b.Kind() != types.Invalid {
			return t
		}
	}
	call, ok := x.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "__goxide_question" {
		return nil
	}
	operand := g.receiverType(call.Args[0])
	if operand == nil {
		return nil
	}
	n, ok := types.Unalias(operand).(*types.Named)
	if !ok || n.TypeArgs().Len() == 0 {
		return nil
	}
	for _, d := range g.cfg.Enums {
		if d.Family != "Option" && d.Family != "Result" {
			continue
		}
		tv, e := types.Eval(g.u.fs, g.u.pkg, x.Pos(), d.Name)
		if e != nil {
			continue
		}
		declared, ok := types.Unalias(tv.Type).(*types.Named)
		if ok && types.Identical(n.Origin(), declared.Origin()) {
			return n.TypeArgs().At(0)
		}
	}
	return nil
}

func checkBoolName(u *unit) error {
	if obj := u.pkg.Scope().Lookup("bool"); obj != nil {
		if tn, ok := obj.(*types.TypeName); !ok || !types.Identical(tn.Type(), types.Typ[types.Bool]) {
			return fmt.Errorf("equality: generated comparison requires unshadowed predeclared bool")
		}
	}
	return nil
}

func (g *generator) containerBuiltins() error {
	for _, name := range []string{"len", "nil"} {
		if g.u.pkg.Scope().Lookup(name) != nil {
			return fmt.Errorf("equality: container comparison requires unshadowed predeclared %s", name)
		}
	}
	return nil
}

// Native map lookup must implement the same equivalence relation as key equals.
// Imported helper signatures alone do not prove that semantic property.
func (g *generator) mapKey(t types.Type, eq bool) error {
	t = types.Unalias(t)
	if basic(t, true) {
		return nil
	}
	if n, ok := t.(*types.Named); ok {
		r := g.find(n)
		if r == nil {
			return fmt.Errorf("map key %s requires an explicit native-consistent equality protocol", t)
		}
		if eq && !r.flags.Eq {
			return fmt.Errorf("map key %s has no explicit Eq protocol", t)
		}
		if r.flags.ManualPartialEq {
			return fmt.Errorf("manual map key %s has no proven native-key consistency", t)
		}
		if r.name == "" {
			return fmt.Errorf("imported map key %s has no proven native-key consistency", t)
		}
		if r.enum != nil {
			return fmt.Errorf("enum map key %s has no native-key equality protocol", t)
		}
		return g.mapKey(n.Underlying(), eq)
	}
	switch t := t.(type) {
	case *types.Array:
		return g.mapKey(t.Elem(), eq)
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			if f.Name() == "_" || f.Embedded() {
				return fmt.Errorf("blank or embedded map key fields are unsupported")
			}
			if e := g.mapKey(f.Type(), eq); e != nil {
				return fmt.Errorf("map key field %s: %w", f.Name(), e)
			}
		}
		return nil
	}
	return fmt.Errorf("map key %s has no proven native-key equality protocol", t)
}
func (g *generator) eval(text string) (types.Type, error) {
	if f := g.u.trees[g.file]; f != nil {
		tv, e := types.Eval(g.u.fs, g.u.pkg, f.Pos(), text)
		if e == nil {
			return tv.Type, nil
		}
	}
	tv, e := types.Eval(g.u.fs, g.u.pkg, token.NoPos, text)
	if e == nil {
		return tv.Type, nil
	}
	for _, name := range g.u.names {
		tv, err := types.Eval(g.u.fs, g.u.pkg, g.u.trees[name].Pos(), text)
		if err == nil {
			return tv.Type, nil
		}
	}
	return nil, e
}

// sumExpression accepts only a concrete instance of a registered builtin family
// with its producer validator. It does not synthesize a protocol from spelling.
func (g *generator) sumExpression(t *types.Named, a, b string, eq bool) (string, bool, error) {
	for _, d := range g.cfg.Enums {
		if d.Family != "Option" && d.Family != "Result" {
			continue
		}
		if d.PackagePath != "" && (t.Obj().Pkg() == nil || t.Obj().Pkg().Path() != d.PackagePath) {
			continue
		}
		declared, e := g.eval(d.Name)
		if e != nil {
			continue
		}
		n, ok := types.Unalias(declared).(*types.Named)
		if !ok || !types.Identical(n.Origin(), t.Origin()) {
			continue
		}
		if n.TypeArgs().Len() > 0 && !types.Identical(n, t) {
			continue
		}
		count := 1
		if d.Family == "Result" {
			count = 2
		}
		if t.TypeArgs().Len() != count || len(d.Variants) != 2 {
			return "", true, fmt.Errorf("equality: invalid trusted sum family %s", d.Name)
		}
		validator := ""
		candidates := append([]enums.Decl{{Validator: d.Validator}}, d.Instances...)
		for _, instance := range candidates {
			if instance.Validator == "" {
				continue
			}
			vt, e := g.eval(instance.Validator)
			if e != nil {
				continue
			}
			sig, ok := vt.(*types.Signature)
			if ok && sig.Params().Len() == 1 && sig.Results().Len() == 0 && types.Identical(sig.Params().At(0).Type(), t) {
				validator = instance.Validator
				break
			}
		}
		if validator == "" {
			return "", true, fmt.Errorf("equality: sum instance has no matching trusted validation metadata: %s", t)
		}
		g.serial++
		prefix := fmt.Sprintf("__goxide_enum_eq_sum%d", g.serial)
		left, right, matched := prefix+"a", prefix+"b", prefix+"ok"
		var text strings.Builder
		fmt.Fprintf(&text, "func()bool{%s(%s);%s(%s);switch %s:=(%s).(type){", validator, a, validator, b, left, a)
		if d.Family == "Option" {
			if g.u.pkg.Scope().Lookup("nil") != nil {
				return "", true, fmt.Errorf("equality: Option comparison requires unshadowed predeclared nil")
			}
			fmt.Fprintf(&text, "case nil:return %s==nil;", b)
		}
		for i, v := range d.Variants {
			if d.Family == "Option" && i == 1 {
				continue
			}
			caseName := v.CaseName
			ct, e := g.eval(caseName)
			if e != nil {
				return "", true, fmt.Errorf("equality: missing trusted sum case %s: %w", caseName, e)
			}
			named, ok := types.Unalias(ct).(*types.Named)
			if !ok {
				return "", true, fmt.Errorf("equality: invalid trusted sum case %s", caseName)
			}
			if named.TypeArgs().Len() == 0 || !types.Implements(ct, t.Underlying().(*types.Interface)) {
				args := make([]string, count)
				for ai := range args {
					args[ai], e = g.typeSpelling(t.TypeArgs().At(ai))
					if e != nil {
						return "", true, e
					}
				}
				caseName += "[" + strings.Join(args, ",") + "]"
				ct, e = g.eval(caseName)
				if e != nil {
					return "", true, e
				}
			}
			st, ok := ct.Underlying().(*types.Struct)
			if !ok || st.NumFields() != 1 || !types.Identical(st.Field(0).Type(), t.TypeArgs().At(i)) || !types.Implements(ct, t.Underlying().(*types.Interface)) {
				return "", true, fmt.Errorf("equality: sum case payload identity mismatch: %s", caseName)
			}
			x, e := g.expression(st.Field(0).Type(), left+".Value", right+".Value", eq)
			if e != nil {
				return "", true, e
			}
			fmt.Fprintf(&text, "case %s:%s,%s:=(%s).(%s);if !%s{return (0!=0)};return %s;", caseName, right, matched, b, caseName, matched, x)
		}
		text.WriteString("};return (0!=0)}()")
		return text.String(), true, nil
	}
	return "", false, nil
}

func (g *generator) typeSpelling(t types.Type) (string, error) {
	for _, name := range g.u.pkg.Scope().Names() {
		obj, ok := g.u.pkg.Scope().Lookup(name).(*types.TypeName)
		if ok && types.Identical(obj.Type(), t) {
			return name, nil
		}
	}
	if file := g.u.trees[g.file]; file != nil {
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if g.cfg.Importer == nil {
				continue
			}
			pkg, e := g.cfg.Importer.Import(path)
			if e != nil {
				continue
			}
			alias := pkg.Name()
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias == "_" {
				continue
			}
			for _, name := range pkg.Scope().Names() {
				if !token.IsExported(name) {
					continue
				}
				obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
				if ok && types.Identical(obj.Type(), t) {
					if alias == "." {
						return name, nil
					}
					return alias + "." + name, nil
				}
			}
		}
	}
	// Recover arguments from the actual alias expansion; never assume a public
	// generic alias uses its origin's parameter order.
	for _, name := range g.u.pkg.Scope().Names() {
		if obj, ok := g.u.pkg.Scope().Lookup(name).(*types.TypeName); ok {
			if spelling, ok := g.genericSpelling(obj, name, t); ok {
				return spelling, nil
			}
		}
	}
	if file := g.u.trees[g.file]; file != nil && g.cfg.Importer != nil {
		for _, spec := range file.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			pkg, e := g.cfg.Importer.Import(path)
			if e != nil {
				continue
			}
			alias := pkg.Name()
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias == "_" {
				continue
			}
			for _, name := range pkg.Scope().Names() {
				if !token.IsExported(name) {
					continue
				}
				obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
				if !ok {
					continue
				}
				spelling := alias + "." + name
				if alias == "." {
					spelling = name
				}
				if spelling, ok := g.genericSpelling(obj, spelling, t); ok {
					return spelling, nil
				}
			}
		}
	}
	switch t := types.Unalias(t).(type) {
	case *types.Basic:
		name := t.Name()
		if g.u.pkg.Scope().Lookup(name) == nil {
			return name, nil
		}
	case *types.Slice:
		elem, e := g.typeSpelling(t.Elem())
		if e == nil {
			return "[]" + elem, nil
		}
	case *types.Array:
		elem, e := g.typeSpelling(t.Elem())
		if e == nil {
			return fmt.Sprintf("[%d]%s", t.Len(), elem), nil
		}
	case *types.Map:
		key, e := g.typeSpelling(t.Key())
		value, e2 := g.typeSpelling(t.Elem())
		if e == nil && e2 == nil {
			return "map[" + key + "]" + value, nil
		}
	case *types.Pointer:
		elem, e := g.typeSpelling(t.Elem())
		if e == nil {
			return "*" + elem, nil
		}
	}
	return "", fmt.Errorf("equality: imported sum payload type is not nameable in the helper source: %s", t)
}

func (g *generator) genericSpelling(obj *types.TypeName, name string, target types.Type) (string, bool) {
	var params *types.TypeParamList
	switch t := obj.Type().(type) {
	case *types.Named:
		params = t.TypeParams()
	case *types.Alias:
		params = t.TypeParams()
	}
	if params.Len() == 0 {
		return "", false
	}
	symbolic := make([]types.Type, params.Len())
	for i := range symbolic {
		symbolic[i] = params.At(i)
	}
	pattern, e := types.Instantiate(nil, obj.Type(), symbolic, true)
	if e != nil {
		return "", false
	}
	bound := make([]types.Type, len(symbolic))
	var unify func(types.Type, types.Type) bool
	unify = func(p, a types.Type) bool {
		p = types.Unalias(p)
		a = types.Unalias(a)
		if tp, ok := p.(*types.TypeParam); ok {
			for i, x := range symbolic {
				if types.Identical(tp, x) {
					if bound[i] == nil {
						bound[i] = a
						return true
					}
					return types.Identical(bound[i], a)
				}
			}
			return types.Identical(p, a)
		}
		switch p := p.(type) {
		case *types.Named:
			n, ok := a.(*types.Named)
			if !ok || !types.Identical(p.Origin(), n.Origin()) || p.TypeArgs().Len() != n.TypeArgs().Len() {
				return false
			}
			for i := 0; i < p.TypeArgs().Len(); i++ {
				if !unify(p.TypeArgs().At(i), n.TypeArgs().At(i)) {
					return false
				}
			}
			return true
		case *types.Slice:
			n, ok := a.(*types.Slice)
			return ok && unify(p.Elem(), n.Elem())
		case *types.Array:
			n, ok := a.(*types.Array)
			return ok && p.Len() == n.Len() && unify(p.Elem(), n.Elem())
		case *types.Map:
			n, ok := a.(*types.Map)
			return ok && unify(p.Key(), n.Key()) && unify(p.Elem(), n.Elem())
		case *types.Pointer:
			n, ok := a.(*types.Pointer)
			return ok && unify(p.Elem(), n.Elem())
		}
		return types.Identical(p, a)
	}
	if !unify(pattern, target) {
		return "", false
	}
	for _, arg := range bound {
		if arg == nil {
			return "", false
		}
	}
	instance, e := types.Instantiate(nil, obj.Type(), bound, true)
	if e != nil || !types.Identical(instance, target) {
		return "", false
	}
	args := make([]string, len(bound))
	for i, arg := range bound {
		if types.Identical(arg, target) {
			return "", false
		}
		args[i], e = g.typeSpelling(arg)
		if e != nil {
			return "", false
		}
	}
	spelling := name + "[" + strings.Join(args, ",") + "]"
	actual, e := g.eval(spelling)
	return spelling, e == nil && types.Identical(actual, target)
}
