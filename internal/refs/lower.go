// Package refs checks concrete source references and structural projection permissions.
// Its named pointer types are an analysis IR, never a runtime wrapper.
package refs

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

type Permission string

const (
	Read  Permission = "read"
	Write Permission = "write"
)

type Ref struct {
	Element    string
	Permission Permission
}
type Reference struct {
	Position token.Position
	Type     Ref
	Symbol   string
}
type Result struct {
	Source     []byte
	References []Reference
}

const prefix = "__hgo_ref_"

var scalars = []string{"bool", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "float32", "float64", "complex64", "complex128", "string"}

func scalar(s string) string {
	if s == "byte" {
		return "uint8"
	}
	if s == "rune" {
		return "int32"
	}
	for _, v := range scalars {
		if v == s {
			return s
		}
	}
	return ""
}
func name(r Ref) string {
	e := r.Element
	if scalar(e) == "" {
		e = "x" + hex.EncodeToString([]byte(e))
	}
	return prefix + string(r.Permission) + "_" + e
}
func element(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p.Path() == "hgo.local/refs" {
			return ""
		}
		return p.Name()
	})
}
func refType(t types.Type) (Ref, bool) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return Ref{}, false
	}
	s := n.Obj().Name()
	for _, p := range []Permission{Read, Write} {
		q := prefix + string(p) + "_"
		if strings.HasPrefix(s, q) {
			raw := strings.TrimPrefix(s, q)
			e := scalar(raw)
			if strings.HasPrefix(raw, "x") {
				if b, err := hex.DecodeString(raw[1:]); err == nil {
					e = string(b)
				}
			}
			if e != "" {
				return Ref{e, p}, true
			}
		}
	}
	return Ref{}, false
}

type edit struct {
	start, end int
	text       string
}
type mapped struct {
	text   []byte
	origin []int
}

func original(b []byte) mapped {
	m := mapped{text: append([]byte(nil), b...)}
	for i := range b {
		m.origin = append(m.origin, i)
	}
	return m
}
func (m mapped) apply(es []edit) mapped {
	sort.Slice(es, func(i, j int) bool { return es[i].start < es[j].start })
	var o mapped
	at := 0
	for _, e := range es {
		o.text = append(o.text, m.text[at:e.start]...)
		o.origin = append(o.origin, m.origin[at:e.start]...)
		o.text = append(o.text, e.text...)
		pos := 0
		if e.start < len(m.origin) {
			pos = m.origin[e.start]
		}
		for range []byte(e.text) {
			o.origin = append(o.origin, pos)
		}
		at = e.end
	}
	o.text = append(o.text, m.text[at:]...)
	o.origin = append(o.origin, m.origin[at:]...)
	return o
}

type lex struct {
	kind       token.Token
	text       string
	start, end int
}

func tokens(src []byte) []lex {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, func(token.Position, string) {}, scanner.ScanComments)
	var out []lex
	for {
		p, k, t := s.Scan()
		if k == token.EOF {
			break
		}
		if k == token.COMMENT {
			continue
		}
		if t == "" {
			t = k.String()
		}
		off := f.Offset(p)
		end := off + len(t)
		if k == token.SEMICOLON && t == "\n" {
			end = off
		}
		out = append(out, lex{k, t, off, end})
	}
	return out
}

// protectedAt keeps attribute-looking text inside Go literals/comments untouched.
func protectedAt(source []byte) func(int) bool {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var scan scanner.Scanner
	scan.Init(f, source, func(token.Position, string) {}, scanner.ScanComments)
	var ranges [][2]int
	for {
		p, k, text := scan.Scan()
		if k == token.EOF {
			break
		}
		if k != token.STRING && k != token.CHAR && k != token.COMMENT {
			continue
		}
		start := f.Offset(p)
		end := start + len(text)
		if source[start] == '`' {
			if j := bytes.IndexByte(source[start+1:], '`'); j >= 0 {
				end = start + j + 2
			}
		}
		if k == token.COMMENT && bytes.HasPrefix(source[start:], []byte("/*")) {
			if j := bytes.Index(source[start+2:], []byte("*/")); j >= 0 {
				end = start + j + 4
			}
		}
		ranges = append(ranges, [2]int{start, end})
	}
	return func(off int) bool {
		for _, r := range ranges {
			if off >= r[0] && off < r[1] {
				return true
			}
		}
		return false
	}
}
func declarations() string {
	var b strings.Builder
	for _, e := range scalars {
		for _, p := range []Permission{Read, Write} {
			fmt.Fprintf(&b, "\ntype %s *%s\n", name(Ref{e, p}), e)
		}
	}
	return b.String()
}

type checker struct {
	companions          map[string][]byte
	filename            string
	source              []byte
	m                   mapped
	fs                  *token.FileSet
	file                *ast.File
	info                *types.Info
	parents             map[ast.Node]ast.Node
	mut                 map[int]bool
	imp                 types.Importer
	synthetic           map[int]bool
	references          []Reference
	elements            map[string]bool
	qualifications      map[types.Object]*qualification
	pointQualifications map[*ast.Ident]*qualification
	conservativeObjects map[types.Object]bool
}

func (c *checker) offset(p token.Pos) int { return c.fs.PositionFor(p, false).Offset }
func (c *checker) position(p token.Pos) token.Position {
	off := c.offset(p)
	if off < len(c.m.origin) {
		off = c.m.origin[off]
	} else {
		off = len(c.source)
	}
	fs := token.NewFileSet()
	f := fs.AddFile(c.filename, -1, len(c.source))
	f.SetLinesForContent(c.source)
	return f.Position(f.Pos(off))
}
func (c *checker) fail(n ast.Node, msg string) error {
	return fmt.Errorf("%s: refs: %s", c.position(n.Pos()), msg)
}
func (c *checker) parse() error {
	c.fs = token.NewFileSet()
	f, err := parser.ParseFile(c.fs, c.filename, c.m.text, parser.AllErrors|parser.ParseComments)
	c.file = f
	return err
}
func (c *checker) checkTypes() error {
	c.info = &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Instances: map[*ast.Ident]types.Instance{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	imp := c.imp
	if imp == nil {
		imp = importer.Default()
	}
	files := []*ast.File{c.file}
	for _, name := range sourceNames(c.companions) {
		if name == c.filename {
			continue
		}
		peer, err := parser.ParseFile(c.fs, name, c.companions[name], parser.AllErrors)
		if err != nil {
			return err
		}
		// All peer bodies are checked in their own turn. Their permission-bearing
		// declarations remain nominal until every source file passes validation.
		for _, d := range peer.Decls {
			if f, ok := d.(*ast.FuncDecl); ok {
				f.Body = &ast.BlockStmt{List: []ast.Stmt{&ast.ForStmt{Body: &ast.BlockStmt{}}}}
			}
		}
		files = append(files, peer)
	}
	deduplicateIR(files)
	conf := types.Config{Importer: imp, DisableUnusedImportCheck: len(c.companions) > 0}
	_, err := conf.Check("hgo.local/refs", c.fs, files, c.info)
	if err != nil {
		return fmt.Errorf("%s: refs: source type checking failed: %w", c.filename, err)
	}
	return nil
}

// Lower checks references and projection permissions before erasing to Go pointers.
// Files without the new reference syntax are returned byte-for-byte unchanged.
func Lower(filename string, source []byte) (Result, error) {
	return lower(filename, source, nil, false)
}

func lower(filename string, source []byte, imp types.Importer, prepare bool) (Result, error) {
	return lowerWithFiles(filename, source, imp, prepare, nil)
}
func lowerWithFiles(filename string, source []byte, imp types.Importer, prepare bool, files map[string][]byte) (Result, error) {
	c := &checker{companions: files, filename: filename, source: source, imp: imp, m: original(source), mut: map[int]bool{}, synthetic: map[int]bool{}, elements: map[string]bool{}}
	// Attributes belong to the later derive pass; preserve their exact source lines.
	var attrs []struct {
		line int
		text string
	}
	off := 0
	protected := protectedAt(source)
	for i, line := range bytes.SplitAfter(source, []byte("\n")) {
		if strings.HasPrefix(strings.TrimSpace(string(line)), "#[") && !protected(off) {
			attrs = append(attrs, struct {
				line int
				text string
			}{i, string(bytes.TrimSuffix(line, []byte("\n")))})
			for j := off; j < off+len(line); j++ {
				if c.m.text[j] != '\n' && c.m.text[j] != '\r' {
					c.m.text[j] = ' '
				}
			}
		}
		off += len(line)
	}
	// A lexically bound host-Go identifier named mut keeps its existing meaning.
	boundMut := map[int]bool{}
	hostFS := token.NewFileSet()
	hostFile, _ := parser.ParseFile(hostFS, filename, c.m.text, parser.AllErrors)
	if hostFile != nil {
		ast.Inspect(hostFile, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "mut" && id.Obj != nil {
				boundMut[hostFS.PositionFor(id.Pos(), false).Offset] = true
			}
			return true
		})
	}
	ts := tokens(c.m.text)
	reserved := ""
	var es []edit
	for i, t := range ts {
		if t.kind == token.IDENT && strings.HasPrefix(t.text, prefix) {
			reserved = t.text
		}
		if t.text == "mut" && i+1 < len(ts) && ts[i+1].kind == token.AND && !boundMut[t.start] && (i == 0 || ts[i-1].kind != token.PERIOD) {
			es = append(es, edit{t.start, t.end, strings.Repeat(" ", t.end-t.start)})
			c.mut[ts[i+1].start] = true
		}
	}
	c.m = c.m.apply(es)
	active := len(es) > 0 || len(files) > 0
	// Let the Go parser distinguish expression & from a reference in a type position.
	for attempts := 0; ; attempts++ {
		if attempts > 1000 {
			return Result{}, fmt.Errorf("%s: refs: too many reference type declarations", filename)
		}
		err := c.parse()
		if err == nil {
			break
		}
		list, ok := err.(scanner.ErrorList)
		if !ok {
			return Result{}, err
		}
		changed := false
		for _, pe := range list {
			pos := pe.Pos.Offset
			ts = tokens(c.m.text)
			for i, t := range ts {
				if t.start != pos || t.kind != token.AND {
					continue
				}
				if i+1 >= len(ts) {
					continue
				}
				e, end := referenceElement(c.m.text, ts, i+1)
				if e == "" {
					return Result{}, fmt.Errorf("%s: refs: unsupported reference element", pe.Pos)
				}
				c.elements[e] = true
				perm := Read
				if c.mut[c.m.origin[pos]] {
					perm = Write
					delete(c.mut, c.m.origin[pos])
				}
				c.m = c.m.apply([]edit{{pos, end, name(Ref{e, perm})}})
				active = true
				changed = true
				break
			}
			if changed {
				break
			}
		}
		if !changed {
			if !active {
				return Result{Source: source}, nil
			}
			return Result{}, fmt.Errorf("%s: refs: unsupported reference syntax: %w", filename, err)
		}
	}
	if !active {
		return Result{Source: source}, nil
	}
	if reserved != "" {
		return Result{}, fmt.Errorf("%s: refs: reserved analysis identifier %s", filename, reserved)
	}
	if prepare {
		es = nil
		ast.Inspect(c.file, func(n ast.Node) bool {
			u, ok := n.(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				return true
			}
			at := c.offset(u.Pos())
			if c.mut[c.m.origin[at]] {
				es = append(es, edit{at, at, "__hgo_ref_make_write("}, edit{c.offset(u.End()), c.offset(u.End()), ")"})
			}
			return true
		})
		c.m = c.m.apply(es)
		lines := bytes.Split(c.m.text, []byte("\n"))
		for _, a := range attrs {
			lines[a.line] = []byte(a.text)
		}
		out := bytes.Join(lines, []byte("\n"))
		out = append(out, []byte(c.declarations()+"\ntype __hgo_ref_write[T any] *T\nfunc __hgo_ref_make_write[T any](v *T) __hgo_ref_write[T] { return __hgo_ref_write[T](v) }\n")...)
		return Result{Source: out}, nil
	}
	bodyLen := len(c.m.text)
	c.m.text = append(c.m.text, []byte(c.declarations())...)
	if err := c.parse(); err != nil {
		return Result{}, err
	}
	if err := c.checkTypes(); err != nil {
		return Result{}, err
	}
	// Establish nominal write types at creation sites before following copies/calls.
	es = nil
	var firstErr error
	ast.Inspect(c.file, func(n ast.Node) bool {
		u, ok := n.(*ast.UnaryExpr)
		if !ok || u.Op != token.AND {
			return true
		}
		at := c.offset(u.Pos())
		if at >= bodyLen || !c.mut[c.m.origin[at]] {
			return true
		}
		p, ok := c.info.TypeOf(u).(*types.Pointer)
		if !ok {
			firstErr = c.fail(u, "write reference requires addressable storage")
			return false
		}
		e := element(p.Elem())
		c.elements[e] = true
		c.synthetic[c.m.origin[at]] = true
		r := Ref{e, Write}
		es = append(es, edit{at, at, name(r) + "("}, edit{c.offset(u.End()), c.offset(u.End()), ")"})
		return true
	})
	if firstErr != nil {
		return Result{}, firstErr
	}
	c.m = c.m.apply(es)
	c.m.text = append(c.m.text[:len(c.m.origin)], []byte(c.declarations())...)
	if err := c.parse(); err != nil {
		return Result{}, err
	}
	if err := c.checkTypes(); err != nil {
		return Result{}, err
	}
	// A reborrow from either permission is explicitly read-only, even with :=.
	for rounds := 0; ; rounds++ {
		if rounds > 1000 {
			return Result{}, fmt.Errorf("%s: refs: reborrow nesting limit exceeded", filename)
		}
		es = nil
		ast.Inspect(c.file, func(n ast.Node) bool {
			u, ok := n.(*ast.UnaryExpr)
			if !ok || u.Op != token.AND {
				return true
			}
			s, ok := unparen(u.X).(*ast.StarExpr)
			if !ok {
				return true
			}
			r, ok := refType(c.info.TypeOf(s.X))
			if !ok {
				return true
			}
			at := c.offset(u.Pos())
			if at >= len(c.m.origin) {
				return true
			}
			if c.synthetic[c.m.origin[at]] {
				return true
			}
			r.Permission = Read
			c.synthetic[c.m.origin[at]] = true
			es = append(es, edit{at, at, name(r) + "("}, edit{c.offset(u.End()), c.offset(u.End()), ")"})
			return true
		})
		c.m = c.m.apply(es)
		if err := c.parse(); err != nil {
			return Result{}, err
		}
		if err := c.checkTypes(); err != nil {
			return Result{}, err
		}
		if len(es) == 0 {
			break
		}
	}
	c.parents = map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(c.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			c.parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	if err := c.checkQualifications(); err != nil {
		return Result{}, err
	}
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	// Remove analysis declarations and casts; named types become ordinary pointers.
	es = nil
	ast.Inspect(c.file, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if c.offset(n.Pos()) >= len(c.m.origin) {
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if r, yes := refType(c.info.TypeOf(id)); yes {
				if _, isType := c.info.Uses[id].(*types.TypeName); isType {
					es = append(es, edit{c.offset(id.Pos()), c.offset(id.End()), "*" + r.Element})
				}
			}
		}
		if call, ok := n.(*ast.CallExpr); ok && c.isIRCast(call) {
			es = append(es, edit{c.offset(call.Pos()), c.offset(call.Args[0].Pos()), "("}, edit{c.offset(call.Args[0].End()), c.offset(call.End()), ")"})
			return true
		}
		return true
	})
	// Cast callee identifiers overlap removal edits; discard their type replacement.
	filtered := es[:0]
	for i, e := range es {
		contained := false
		for j, o := range es {
			if i != j && e.start >= o.start && e.end <= o.end && (e.start != o.start || e.end != o.end) {
				contained = true
				break
			}
		}
		if !contained {
			filtered = append(filtered, e)
		}
	}
	// Synthetic declarations have no original source mapping.
	end := len(c.m.origin)
	c.m.text = c.m.text[:end]
	c.m = c.m.apply(filtered)
	out := c.m.text
	// Keep line-preserving source for the subsequent attribute parser.
	lines := bytes.Split(out, []byte("\n"))
	for _, a := range attrs {
		if a.line < len(lines) {
			lines[a.line] = []byte(a.text)
		}
	}
	out = bytes.Join(lines, []byte("\n"))
	if len(attrs) == 0 {
		formatted, err := format.Source(out)
		if err != nil {
			return Result{}, fmt.Errorf("%s: refs: internal lowering error: %w", filename, err)
		}
		out = formatted
	}
	return Result{Source: out, References: c.references}, nil
}
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
func (c *checker) isIRCast(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	at := c.offset(call.Pos())
	return ok && strings.HasPrefix(id.Name, prefix) && len(call.Args) == 1 && at < len(c.m.origin) && c.synthetic[c.m.origin[at]]
}

func hasRef(t types.Type) bool {
	seen := map[types.Type]bool{}
	var visit func(types.Type) bool
	visit = func(t types.Type) bool {
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		if _, ok := refType(t); ok {
			return true
		}
		switch t := t.(type) {
		case *types.Pointer:
			return visit(t.Elem())
		case *types.Slice:
			return visit(t.Elem())
		case *types.Array:
			return visit(t.Elem())
		case *types.Map:
			return visit(t.Key()) || visit(t.Elem())
		case *types.Chan:
			return visit(t.Elem())
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				if visit(t.TypeArgs().At(i)) {
					return true
				}
			}
			return visit(t.Underlying())
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				if visit(t.At(i).Type()) {
					return true
				}
			}
		case *types.Signature:
			return visit(t.Params()) || visit(t.Results())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				if visit(t.Field(i).Type()) {
					return true
				}
			}
		}
		return false
	}
	return visit(t)
}
func (c *checker) function(n ast.Node) *ast.FuncDecl {
	for p := n; p != nil; p = c.parents[p] {
		if _, ok := p.(*ast.FuncLit); ok {
			return nil
		}
		if f, ok := p.(*ast.FuncDecl); ok {
			return f
		}
	}
	return nil
}
func (c *checker) typ(e ast.Expr) types.Type { return c.info.TypeOf(e) }
func (c *checker) binding(id *ast.Ident) types.Object {
	if o := c.info.Defs[id]; o != nil {
		return o
	}
	return c.info.Uses[id]
}
func (c *checker) transfer(dst types.Type, src ast.Expr) error {
	dr, dok := refType(dst)
	sr, sok := refType(c.typ(src))
	if !dok && !sok {
		if hasRef(dst) || hasRef(c.typ(src)) {
			return c.fail(src, "unsupported container/function/tuple carrying references")
		}
		return nil
	}
	if dok && sok {
		if dr != sr {
			return c.fail(src, "reference permission/type mismatch; use &(*w) to explicitly narrow write access")
		}
		return nil
	}
	if sok {
		return c.fail(src, "reference permission would be erased by a raw pointer/interface/native boundary; bridging is unsupported")
	}
	if dr.Permission == Read {
		if u, ok := unparen(src).(*ast.UnaryExpr); ok && u.Op == token.AND {
			if p, ok := c.typ(u).(*types.Pointer); ok {
				if element(p.Elem()) == dr.Element {
					c.references = append(c.references, Reference{c.position(u.Pos()), dr, ""})
					return nil
				}
			}
		}
	}
	return c.fail(src, "reference initialization requires explicit &value (read) or mut& value (write); nil/raw-pointer adoption is unsupported")
}
func (c *checker) validate() error {
	var err error
	ast.Inspect(c.file, func(n ast.Node) bool {
		if n == nil || err != nil {
			return false
		}
		if c.offset(n.Pos()) >= len(c.m.origin) {
			return false
		}
		fail := func(s string) bool { err = c.fail(n, s); return false }
		switch x := n.(type) {
		case *ast.TypeSpec:
			bad := false
			ast.Inspect(x.Type, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, prefix) {
					bad = true
				}
				return true
			})
			if bad {
				return fail("reference aliases/defined types are unsupported")
			}
		case *ast.Ident:
			if instance, ok := c.info.Instances[x]; ok {
				for i := 0; i < instance.TypeArgs.Len(); i++ {
					if hasRef(instance.TypeArgs.At(i)) {
						return fail("generic instantiations carrying references are unsupported")
					}
				}
			}
			if c.info.Defs[x] != nil && scalar(x.Name) != "" {
				return fail("shadowing built-in scalar names in a reference-bearing file is unsupported")
			}
			if obj := c.binding(x); obj != nil {
				if sig, ok := obj.Type().(*types.Signature); ok && hasRef(sig) {
					switch p := c.parents[x].(type) {
					case *ast.FuncDecl:
						if p.Name == x {
							break
						}
						return fail("reference methods are unsupported")
					case *ast.CallExpr:
						if unparen(p.Fun) != x {
							return fail("reference function values/callbacks are unsupported")
						}
					default:
						return fail("reference function values/callbacks are unsupported")
					}
				}
			}
		case *ast.FuncDecl:
			sig, ok := c.info.Defs[x.Name].Type().(*types.Signature)
			if ok && hasRef(sig) {
				if x.Recv != nil || x.Type.TypeParams != nil {
					return fail("reference methods/generics are unsupported")
				}
				if sig.Results().Len() > 1 {
					return fail("multiple results carrying references are unsupported")
				}
				for _, list := range []*ast.FieldList{x.Type.Params, x.Type.Results} {
					if list == nil {
						continue
					}
					for _, f := range list.List {
						if hasRef(c.typ(f.Type)) {
							if _, yes := refType(c.typ(f.Type)); !yes {
								return fail("reference containers/callback signatures are unsupported")
							}
							if list == x.Type.Results && len(f.Names) > 0 {
								return fail("named reference result slots are unsupported")
							}
						}
					}
				}
			}
		case *ast.Field:
			if hasRef(c.typ(x.Type)) {
				p, ok := c.parents[x].(*ast.FieldList)
				if !ok {
					return fail("unsupported reference field")
				}
				ft, ok := c.parents[p].(*ast.FuncType)
				if !ok {
					return fail("references in containers/struct fields are unsupported")
				}
				if _, ok := c.parents[ft].(*ast.FuncDecl); !ok {
					return fail("reference callbacks/function types are unsupported")
				}
			}
		case *ast.ValueSpec:
			refs := false
			for _, id := range x.Names {
				if obj := c.info.Defs[id]; obj != nil && hasRef(obj.Type()) {
					refs = true
				}
			}
			if refs {
				local := false
				for p := c.parents[x]; p != nil; p = c.parents[p] {
					switch p.(type) {
					case *ast.FuncDecl, *ast.FuncLit:
						local = true
					}
				}
				if !local {
					return fail("package/global references are unsupported")
				}
				if len(x.Values) != len(x.Names) {
					return fail("uninitialized/tuple reference declarations are unsupported")
				}
			}
			if len(x.Values) == len(x.Names) {
				for i, v := range x.Values {
					obj := c.info.Defs[x.Names[i]]
					if obj != nil {
						if e := c.transfer(obj.Type(), v); e != nil {
							err = e
							return false
						}
					}
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				for _, e := range append(append([]ast.Expr{}, x.Lhs...), x.Rhs...) {
					if hasRef(c.typ(e)) {
						return fail("tuple/multiple-result reference assignment is unsupported")
					}
				}
				break
			}
			for i, rhs := range x.Rhs {
				lhs := unparen(x.Lhs[i])
				if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
					continue
				}
				if e := c.transfer(c.typ(lhs), rhs); e != nil {
					err = e
					return false
				}
				if e := c.writable(lhs); e != nil {
					err = e
					return false
				}
			}
		case *ast.RangeStmt:
			for _, e := range []ast.Expr{x.Key, x.Value} {
				if e != nil {
					if hasRef(c.typ(e)) {
						return fail("references as range destinations are unsupported")
					}
					if er := c.writable(unparen(e)); er != nil {
						err = er
						return false
					}
				}
			}
		case *ast.IncDecStmt:
			if e := c.writable(unparen(x.X)); e != nil {
				err = e
				return false
			}
		case *ast.ReturnStmt:
			f := c.function(x)
			for i, v := range x.Results {
				if f == nil {
					if hasRef(c.typ(v)) {
						return fail("reference return from closures is unsupported")
					}
					continue
				}
				sig := c.info.Defs[f.Name].Type().(*types.Signature)
				if len(x.Results) == sig.Results().Len() {
					if e := c.transfer(sig.Results().At(i).Type(), v); e != nil {
						err = e
						return false
					}
				} else if hasRef(c.typ(v)) {
					return fail("tuple reference return is unsupported")
				}
			}
		case *ast.CallExpr:
			if c.isIRCast(x) {
				u, ok := unparen(x.Args[0]).(*ast.UnaryExpr)
				if !ok || u.Op != token.AND {
					return fail("invalid internal reference construction")
				}
				r, _ := refType(c.typ(x))
				if s, ok := unparen(u.X).(*ast.StarExpr); ok {
					if from, yes := refType(c.typ(s.X)); yes && r.Permission == Write && from.Permission == Read {
						return fail("cannot obtain write access through a read reference")
					}
				}
				break
			}
			if c.info.Types[x.Fun].IsType() && hasRef(c.typ(x)) {
				return fail("explicit reference type conversion/adoption is unsupported")
			}
			involved := hasRef(c.typ(x))
			for _, a := range x.Args {
				involved = involved || hasRef(c.typ(a))
			}
			if involved {
				generic := false
				ast.Inspect(x.Fun, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok {
						if _, yes := c.info.Instances[id]; yes {
							generic = true
						}
					}
					return true
				})
				if generic {
					return fail("generic calls carrying references are unsupported")
				}
			}
			sig, _ := c.typ(x.Fun).(*types.Signature)
			for i, a := range x.Args {
				if sig != nil {
					p := i
					if p >= sig.Params().Len() {
						p = sig.Params().Len() - 1
					}
					if p < 0 {
						continue
					}
					target := sig.Params().At(p).Type()
					if sig.Variadic() && p == sig.Params().Len()-1 && !x.Ellipsis.IsValid() {
						target = target.(*types.Slice).Elem()
					}
					if e := c.transfer(target, a); e != nil {
						err = e
						return false
					}
				} else if hasRef(c.typ(a)) {
					return fail("reference conversion/builtin/native boundary is unsupported")
				}
			}
		}
		if expr, ok := n.(ast.Expr); ok {
			t := c.typ(expr)
			if r, yes := refType(t); yes {
				if id, ok := expr.(*ast.Ident); ok {
					if _, isType := c.info.Uses[id].(*types.TypeName); isType {
						return true
					}
					if c.info.Defs[id] != nil {
						return true
					}
				}
				sym := ""
				if id, ok := expr.(*ast.Ident); ok {
					if obj := c.binding(id); obj != nil {
						sym = fmt.Sprintf("%s@%s", obj.Name(), c.position(obj.Pos()))
					}
				}
				c.references = append(c.references, Reference{c.position(expr.Pos()), r, sym})
				switch p := c.parents[n].(type) {
				case *ast.ParenExpr:
					return true
				case *ast.SelectorExpr:
					if p.X == n {
						return true
					}
				case *ast.IndexExpr:
					if p.X == n {
						return true
					}
				case *ast.StarExpr:
					if p.X == n {
						return true
					}
				case *ast.AssignStmt:
					return true
				case *ast.ValueSpec:
					return true
				case *ast.ReturnStmt:
					return true
				case *ast.CallExpr:
					for _, a := range p.Args {
						if a == n {
							return true
						}
					}
				}
				return fail("unsupported reference use; only supported projections, local copy/rebinding, and direct checked calls/returns are supported")
			} else if hasRef(t) {
				switch n.(type) {
				case *ast.FuncType, *ast.Ident: // function identity checked above
				default:
					return fail("reference containers, tuples, indirect functions and nested references are unsupported")
				}
			}
		}
		return true
	})
	return err
}
func (c *checker) writable(e ast.Expr) error {
	if s, ok := e.(*ast.StarExpr); ok {
		if r, yes := refType(c.typ(s.X)); yes && r.Permission == Read {
			return c.fail(e, "cannot write through a read reference")
		}
	}
	return nil
}
