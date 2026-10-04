// Package matchexpr normalizes a finite expression-match subset to statement match.
package matchexpr

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

type tok struct {
	k    token.Token
	s    string
	a, b int
}
type edit struct {
	a, b int
	s    string
}

// Lower accepts explicitly typed local initializers, single-result returns,
// complete nested arms and a finite same-file direct-call argument subset.
// Statement match validation and pattern semantics remain in enums.Lower.
func Lower(filename string, src []byte) ([]byte, error) {
	return LowerMapped(filename, src, nil)
}

func LowerMapped(filename string, src []byte, provenance *sourcemap.Map) ([]byte, error) {
	if bytes.IndexByte(src, 0) >= 0 {
		return nil, fmt.Errorf("%s: NUL byte in source", filename)
	}
	var fragments *sourcemap.Fragments
	if provenance != nil {
		fragments = &sourcemap.Fragments{Input: provenance}
	}
	copySource := func(a, b int) string {
		if fragments != nil {
			return fragments.Copy(a, b)
		}
		return string(src[a:b])
	}
	fs := token.NewFileSet()
	f := fs.AddFile(filename, -1, len(src))
	var sc scanner.Scanner
	sc.Init(f, src, func(token.Position, string) {}, scanner.ScanComments)
	var ts []tok
	for {
		p, k, s := sc.Scan()
		if k == token.EOF {
			break
		}
		if k == token.COMMENT {
			continue
		}
		if s == "" {
			s = k.String()
		}
		a := f.Offset(p)
		b := a + len(s)
		if k == token.SEMICOLON && s == "\n" {
			b = a
		}
		if k == token.STRING && src[a] == '`' {
			if n := bytes.IndexByte(src[a+1:], '`'); n >= 0 {
				b = a + n + 2
			}
		}
		ts = append(ts, tok{k, s, a, b})
	}
	fail := func(i int, msg string) error {
		return fmt.Errorf("%s: expression match: %s", f.Position(token.Pos(f.Base()+ts[i].a)), msg)
	}
	pairs := map[int]int{}
	var stack []int
	for i, t := range ts {
		switch t.k {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			stack = append(stack, i)
		case token.RPAREN, token.RBRACK, token.RBRACE:
			if len(stack) > 0 {
				j := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				pairs[j] = i
			}
		}
		if t.k == token.IDENT && strings.HasPrefix(t.s, "__goxide_match_expr_") {
			return nil, fail(i, "reserved generated identifier")
		}
	}
	// Locate function bodies by parsing only signatures: enum and attribute syntax
	// elsewhere cannot perturb the host parser. Innermost function owns return.
	type fn struct {
		open, close int
		result      string
	}
	var funcs []fn
	for i, t := range ts {
		if t.k != token.FUNC {
			continue
		}
		j := i + 1
		for j < len(ts) && ts[j].k != token.LBRACE && ts[j].k != token.SEMICOLON {
			if ts[j].k == token.LPAREN || ts[j].k == token.LBRACK {
				if e, ok := pairs[j]; ok {
					j = e
				}
			}
			j++
		}
		if j == len(ts) || ts[j].k != token.LBRACE {
			continue
		}
		end, ok := pairs[j]
		if !ok {
			continue
		}
		header := string(src[t.a:ts[j].a])
		var ft *ast.FuncType
		if ex, e := parser.ParseExpr(header + "{}"); e == nil {
			if fl, ok := ex.(*ast.FuncLit); ok {
				ft = fl.Type
			}
		} else if file, e := parser.ParseFile(token.NewFileSet(), "", "package p\n"+header+"{}", 0); e == nil {
			if d, ok := file.Decls[0].(*ast.FuncDecl); ok {
				ft = d.Type
			}
		}
		result := ""
		if ft != nil && ft.Results != nil && len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) <= 1 {
			var b bytes.Buffer
			format.Node(&b, token.NewFileSet(), ft.Results.List[0].Type)
			result = b.String()
		}
		funcs = append(funcs, fn{j, end, result})
	}
	var edits []edit
	c := composer{source: src, ts: ts, pairs: pairs, copy: copySource, fail: fail}

	c.origin = func(i int) string {
		origin := f.Position(token.Pos(f.Base() + ts[i].a)).String()
		if provenance != nil {
			origin = "[generated/unmapped] " + origin
			if seg := provenance.Lookup(ts[i].a); seg != nil && seg.Origin != nil {
				o := seg.Origin
				origin = fmt.Sprintf("%s:%d:%d", o.File, o.Line, o.Column)
			}
		}
		return origin
	}

	for i := 0; i < len(ts); i++ {
		if ts[i].s != "match" || ts[i].k != token.IDENT {
			continue
		}
		start := i
		mode := ""
		typ := ""
		name := ""
		owner := -1
		for n, v := range funcs {
			if v.open < i && i < v.close && (owner < 0 || v.open > funcs[owner].open) {
				owner = n
			}
		}
		if owner >= 0 {
			serial := c.serial
			e, end, ok, err := c.embedded(i, funcs[owner].open, funcs[owner].result)
			if err != nil {
				return nil, err
			}
			if ok {
				edits = append(edits, e)
				i = end
				continue
			}
			c.serial = serial
		}
		if i > 0 && ts[i-1].k == token.RETURN {
			mode = "return"
			start = i - 1
			if owner < 0 || funcs[owner].result == "" {
				return nil, fail(i, "return requires one concrete result type")
			}
			typ = funcs[owner].result
		} else if i > 1 && (ts[i-1].k == token.ASSIGN || ts[i-1].k == token.DEFINE) {
			if owner < 0 {
				return nil, fail(i, "package-level match initializer unsupported")
			}
			j := i - 2
			for j >= 0 && ts[j].k != token.SEMICOLON && ts[j].k != token.LBRACE && ts[j].k != token.RBRACE && ts[j].k != token.VAR {
				j--
			}
			if j >= 0 && ts[j].k == token.VAR && ts[i-1].k == token.ASSIGN {
				if j+2 > i-1 || ts[j+1].k != token.IDENT {
					return nil, fail(i, "use a single local var initializer")
				}
				start = j
				name = ts[j+1].s
				mode = "var"
				if j+2 == i-1 {
					mode = "varinfer"
				} else {
					typ = strings.TrimSpace(string(src[ts[j+2].a:ts[i-1].a]))
					if _, e := parser.ParseExpr(typ); e != nil {
						return nil, fail(i, "invalid or unsupported explicit result type")
					}
				}
			} else {
				start = i - 2
				if ts[start].k != token.IDENT || ts[start].s == "_" || start == 0 || (ts[start-1].k != token.SEMICOLON && ts[start-1].k != token.LBRACE && ts[start-1].k != token.RBRACE) {
					return nil, fail(i, "match assignment requires a single local identifier in a statement; use a typed local var for complex targets or control headers")
				}
				name = ts[start].s
				mode = "assign"
				if ts[i-1].k == token.DEFINE {
					mode = "infer"
				}
			}
		} else {
			if i > 0 {
				switch ts[i-1].k {
				case token.LBRACE, token.RBRACE, token.SEMICOLON, token.COLON:
				default:
					return nil, fail(i, "embedded expression match unsupported; use a typed local var")
				}
			}
			continue
		}
		j := i + 1
		for j < len(ts) && ts[j].k != token.LBRACE {
			if e, ok := pairs[j]; ok {
				j = e
			}
			j++
		}
		end, ok := pairs[j]
		if !ok {
			return nil, fail(i, "missing match body")
		}
		if end+1 < len(ts) && ts[end+1].k != token.SEMICOLON && ts[end+1].k != token.RBRACE {
			return nil, fail(i, "match must be the entire initializer or return expression")
		}
		tmp := c.temp()
		var b strings.Builder
		if mode == "assign" || mode == "infer" || mode == "varinfer" {
			pos := f.Position(token.Pos(f.Base() + ts[i].a))
			origin := pos.String()
			if provenance != nil {
				origin = "[generated/unmapped] " + origin
				if seg := provenance.Lookup(ts[i].a); seg != nil && seg.Origin != nil {
					o := seg.Origin
					origin = fmt.Sprintf("%s:%d:%d", o.File, o.Line, o.Column)
				}
			}
			if mode == "assign" {
				fmt.Fprintf(&b, "var %s = __goxide_match_expr_context(%s,%s);\n", tmp, copySource(ts[start].a, ts[start].b), strconv.Quote(origin))
			} else {
				fmt.Fprintf(&b, "var %s = __goxide_match_expr_infer(%s);\n", tmp, strconv.Quote(origin))
			}
		} else {
			if mode == "return" {
				fmt.Fprintf(&b, "var %s = __goxide_match_expr_operand_context(__goxide_match_expr_return(),%q,%q);\n", tmp, "direct", c.origin(i))
			} else {
				fmt.Fprintf(&b, "var %s %s;\n", tmp, typ)
			}
		}
		body, err := c.assign(i, end+1, tmp, typ)
		if err != nil {
			return nil, err
		}
		b.WriteString(body)

		if mode == "return" {
			fmt.Fprintf(&b, "return %s", tmp)
		} else if mode == "assign" {
			fmt.Fprintf(&b, "%s = %s", copySource(ts[start].a, ts[start].b), tmp)
		} else if mode == "infer" {
			fmt.Fprintf(&b, "%s := %s", copySource(ts[start].a, ts[start].b), tmp)
		} else if mode == "varinfer" {
			fmt.Fprintf(&b, "var %s = %s", name, tmp)
		} else {
			fmt.Fprintf(&b, "var %s %s = %s", name, typ, tmp)
		}
		text := b.String()
		if mode == "assign" || mode == "return" {
			text = "{\n" + text + "\n}"
		}
		edits = append(edits, edit{ts[start].a, ts[end].b, text})
		i = end
	}
	var out bytes.Buffer
	at := 0
	for _, e := range edits {
		out.WriteString(copySource(at, e.a))
		out.WriteString(e.s)
		at = e.b
	}
	out.WriteString(copySource(at, len(src)))
	if provenance != nil {
		data, m := fragments.Finish(out.String(), "matchexpr.Lower")
		*provenance = *m
		return data, nil
	}
	return out.Bytes(), nil
}
