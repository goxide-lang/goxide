package frontend

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"strings"
)

// manualDefault is compiler-owned evidence from an explicit source impl. A Go
// function with the same helper spelling never creates this evidence.
type manualDefault struct {
	target   string
	position token.Position
	origin   *sourcemap.Span
	stack    []sourcemap.Expansion
}

// lowerManualDefaults preserves body bytes and line counts. Only the agreed
// concrete Default subset is accepted; this is not a general impl parser.
func lowerManualDefaults(filename string, source []byte) ([]byte, map[string]manualDefault, error) {
	type lexeme struct {
		kind       token.Token
		text       string
		start, end int
		pos        token.Position
	}
	fs := token.NewFileSet()
	f := fs.AddFile(filename, -1, len(source))
	var s scanner.Scanner
	s.Init(f, source, func(token.Position, string) {}, 0)
	var ts []lexeme
	for {
		p, k, l := s.Scan()
		if k == token.EOF {
			break
		}
		if l == "" {
			l = k.String()
		}
		start := f.Offset(p)
		end := start + len(l)
		if end > len(source) {
			end = len(source)
		}
		ts = append(ts, lexeme{k, l, start, end, fs.Position(p)})
	}
	defaults := map[string]manualDefault{}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	blank := func(b []byte) string {
		out := append([]byte(nil), b...)
		for i, c := range out {
			if c != '\n' && c != '\r' {
				out[i] = ' '
			}
		}
		return string(out)
	}
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if depth == 0 && t.text == "impl" && i+1 < len(ts) && ts[i+1].text == "Default" {
			fail := func(msg string) ([]byte, map[string]manualDefault, error) {
				return nil, nil, fmt.Errorf("%s: manual Default: %s", t.pos, msg)
			}
			if i+8 >= len(ts) || ts[i+2].text != "for" || ts[i+3].kind != token.IDENT || ts[i+4].kind != token.LBRACE {
				return fail("requires impl Default for a local concrete struct")
			}
			target := ts[i+3].text
			if _, ok := defaults[target]; ok {
				return fail("duplicate implementation for " + target)
			}
			j := i + 5
			for j < len(ts) && ts[j].kind == token.SEMICOLON {
				j++
			}
			if j+6 >= len(ts) || ts[j].kind != token.FUNC || ts[j+1].text != "default" {
				return fail("requires exactly func default() " + target + "; static and member attributes are unsupported")
			}
			if ts[j+2].kind != token.LPAREN || ts[j+3].kind != token.RPAREN {
				return fail("default must be an associated function with zero parameters (no self)")
			}
			if ts[j+4].kind != token.IDENT || ts[j+4].text != target || ts[j+5].kind != token.LBRACE {
				return fail("default must return the exact concrete target type " + target)
			}
			end := j + 5
			level := 1
			for end++; end < len(ts) && level > 0; end++ {
				switch ts[end].kind {
				case token.LBRACE:
					level++
				case token.RBRACE:
					level--
				}
			}
			if level != 0 {
				return fail("unterminated default body")
			}
			bodyEnd := end - 1
			for end < len(ts) && ts[end].kind == token.SEMICOLON {
				end++
			}
			if end >= len(ts) || ts[end].kind != token.RBRACE {
				return fail("requires exactly one default member")
			}
			edits = append(edits, edit{t.start, ts[j].start, blank(source[t.start:ts[j].start])}, edit{ts[j+1].start, ts[j+1].end, "Default" + target}, edit{ts[bodyEnd].end, ts[end].end, blank(source[ts[bodyEnd].end:ts[end].end])})
			defaults[target] = manualDefault{target: target, position: t.pos}
			i = end
			continue
		}
		switch t.kind {
		case token.LBRACE:
			depth++
		case token.RBRACE:
			depth--
		}
	}
	var out bytes.Buffer
	cursor := 0
	for _, e := range edits {
		out.Write(source[cursor:e.start])
		out.WriteString(e.text)
		cursor = e.end
	}
	out.Write(source[cursor:])
	return out.Bytes(), defaults, nil
}

func typeSpecs(file *ast.File) map[string]*ast.TypeSpec {
	out := map[string]*ast.TypeSpec{}
	for _, d := range file.Decls {
		if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
			for _, sp := range gd.Specs {
				t := sp.(*ast.TypeSpec)
				out[t.Name.Name] = t
			}
		}
	}
	return out
}

// checkDefaultCycles uses resolved function objects, not identifier spelling.
// It rejects static function-reference cycles reachable from a Default helper;
// arbitrary dynamic callbacks and general termination analysis are out of scope.
func checkDefaultCycles(path string, sources map[string][]byte, imp types.Importer, helpers map[string]bool, maps map[string]*sourcemap.Map) error {
	if len(helpers) == 0 {
		return nil
	}
	fs := token.NewFileSet()
	var files []*ast.File
	for _, name := range sortedSources(sources) {
		f, e := parser.ParseFile(fs, name, sources[name], 0)
		if e != nil {
			return e
		}
		files = append(files, f)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(path, fs, files, info)
	if err != nil {
		return err
	}
	graph := map[*types.Func][]*types.Func{}
	positions := map[*types.Func]token.Pos{}
	for _, file := range files {
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			obj, _ := info.Defs[fn.Name].(*types.Func)
			positions[obj] = fn.Name.Pos()
			if mapping := maps[fs.Position(fn.Pos()).Filename]; mapping != nil {
				if seg := mapping.Lookup(fs.Position(fn.Name.Pos()).Offset); seg == nil || seg.Origin == nil {
					positions[obj] = fn.Pos()
				}
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					if callee, ok := info.Uses[id].(*types.Func); ok && callee.Pkg() == pkg {
						graph[obj] = append(graph[obj], callee)
					}
				}
				return true
			})
		}
	}
	for _, name := range sortedSourcesBool(helpers) {
		root, _ := pkg.Scope().Lookup(name).(*types.Func)
		if root == nil {
			continue
		}
		active := map[*types.Func]bool{}
		done := map[*types.Func]bool{}
		var stack []string
		var visit func(*types.Func) error
		visit = func(fn *types.Func) error {
			if active[fn] {
				p := fs.Position(positions[root])
				mapped := &sourcemap.Error{Message: fmt.Sprintf("recursive Default protocol dependency: %s -> %s (static recursion is unsupported)", strings.Join(stack, " -> "), fn.Name()), Generated: p}
				if mapping := maps[p.Filename]; mapping != nil {
					if seg := mapping.Lookup(p.Offset); seg != nil {
						mapped.Origin = seg.Origin
						mapped.Stack = seg.Stack
					}
				}
				return mapped
			}
			if done[fn] {
				return nil
			}
			active[fn] = true
			stack = append(stack, fn.Name())
			for _, next := range graph[fn] {
				if e := visit(next); e != nil {
					return e
				}
			}
			stack = stack[:len(stack)-1]
			delete(active, fn)
			done[fn] = true
			return nil
		}
		if e := visit(root); e != nil {
			return e
		}
	}
	return nil
}
func sortedSourcesBool(values map[string]bool) []string {
	m := map[string][]byte{}
	for k := range values {
		m[k] = nil
	}
	return sortedSources(m)
}

// Preserve causal source evidence for the renamed manual helper. Its generated
// identifier has no unchanged token to recover through ordinary composition.
func attachManualDefaultOrigins(mapping *sourcemap.Map, source []byte, entries map[string]manualDefault) {
	if len(entries) == 0 {
		return
	}
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "generated.go", source, 0)
	if err != nil {
		return
	}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv != nil {
			continue
		}
		for n, entry := range entries {
			if fn.Name.Name == "Default"+n {
				mapping.Attach(fs.Position(fn.Name.Pos()).Offset, fs.Position(fn.Name.End()).Offset, entry.origin, entry.stack, "manual Default helper")
			}
		}
	}
}
