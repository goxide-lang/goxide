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
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

type Derive struct {
	PartialEq, Eq bool
	File          string
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

const intrinsic = "__hgo_enum_equals"
const shim = "\nfunc __hgo_enum_equals[T interface{}](a,b T)bool{return (0!=0)}\n"

type unit struct {
	fs    *token.FileSet
	names []string
	trees map[string]*ast.File
	list  []*ast.File
	pkg   *types.Package
	info  *types.Info
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
func (u *unit) check(cfg Config, ignoreBodies, tolerate bool) error {
	u.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	path := cfg.PackagePath
	if path == "" {
		path = "hgo/generated"
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
			cfg.Maps[name] = cfg.Maps[name].Compose(files[name], out[name], stage)
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
	u         *unit
	cfg       Config
	protocols []*registered
	local     map[string]*registered
	state     map[string]int
	code      map[string]string
	serial    int
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
		r.bound = fmt.Sprintf("__hgo_enum_eq_bound_%d", i)
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
	case *types.Array:
		g.serial++
		idx := fmt.Sprintf("__hgo_eq_i%d", g.serial)
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
	g.state[r.name] = 1
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
				x, e := g.expression(st.Field(0).Type(), "x.Value", "y.Value", r.flags.Eq)
				if e != nil {
					return fmt.Errorf("equality: %s::%s: %w", r.name, v.Name, e)
				}
				fmt.Fprintf(&b, "return %s;", x)
			}
		}
		b.WriteString("};return (0!=0)")
		body = b.String()
	} else {
		x, e := g.structural(r.typ.Underlying(), "a", "b", r.flags.Eq)
		if e != nil {
			return fmt.Errorf("equality: derive %s for %s: %w", equalityKind(r.flags.Eq), r.name, e)
		}
		body = "return " + x
	}
	g.code[r.name] = fmt.Sprintf("\nfunc %s(a,b %s)bool{%s}\n", r.helper, r.name, body)
	g.state[r.name] = 2
	return nil
}
func appendDeclarations(u *unit, name, text string) error {
	f, e := parser.ParseFile(u.fs, "<equality helpers>", "package "+u.trees[name].Name.Name+"\n"+text, parser.ParseComments)
	if e != nil {
		return e
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
		if u.pkg.Scope().Lookup(g.local[n].helper) != nil {
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
				call.Fun = ast.NewIdent(r.bound)
				call.Args = append([]ast.Expr{sel.X}, call.Args...)
				return true
			}
			if receiver != nil && !basic(receiver, false) {
				return true
			}
			call.Fun = ast.NewIdent(intrinsic)
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
	_ = u.check(cfg, false, true)
	g, e := registry(u, cfg)
	if e != nil {
		return nil, e
	}
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
				call.Fun = ast.NewIdent(r.bound)
			} else if basic(u.info.TypeOf(call.Args[0]), false) {
				call.Fun = ast.NewIdent("__hgo_enum_scalar_equals")
				needScalarHelper = true
			} else {
				call.Fun = &ast.SelectorExpr{X: call.Args[0], Sel: ast.NewIdent("equals")}
				call.Args = call.Args[1:]
			}
			return true
		})
		keep := u.trees[name].Decls[:0]
		for _, d := range u.trees[name].Decls {
			if f, ok := d.(*ast.FuncDecl); ok && f.Name.Name == intrinsic {
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
		if e = appendDeclarations(u, anchor, "func __hgo_enum_scalar_equals[T comparable](a,b T)bool{return a==b}"); e != nil {
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
	if !ok || id.Name != "__hgo_question" {
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
