// Package matchexpr normalizes a finite expression-match subset to statement match.
package matchexpr

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
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

// Lower accepts a single explicitly typed local var initializer or a whole return
// expression in a function with one unnamed result. It leaves statement matches
// untouched, delegating all pattern semantics to enums.Lower.
func Lower(filename string, src []byte) ([]byte, error) {
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
		if t.k == token.IDENT && strings.HasPrefix(t.s, "__hgo_match_expr_") {
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
		if ft != nil && ft.Results != nil && len(ft.Results.List) == 1 && len(ft.Results.List[0].Names) == 0 {
			var b bytes.Buffer
			format.Node(&b, token.NewFileSet(), ft.Results.List[0].Type)
			result = b.String()
		}
		funcs = append(funcs, fn{j, end, result})
	}
	var edits []edit
	serial := 0
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
		if i > 0 && ts[i-1].k == token.RETURN {
			mode = "return"
			start = i - 1
			if owner < 0 || funcs[owner].result == "" {
				return nil, fail(i, "return requires one unnamed concrete result type")
			}
			typ = funcs[owner].result
		} else if i > 0 && ts[i-1].k == token.ASSIGN {
			j := i - 2
			for j >= 0 && ts[j].k != token.SEMICOLON && ts[j].k != token.LBRACE && ts[j].k != token.RBRACE {
				if ts[j].k == token.VAR {
					break
				}
				j--
			}
			if j < 0 || ts[j].k != token.VAR || j+3 >= i || ts[j+1].k != token.IDENT {
				return nil, fail(i, "use a single explicitly typed local var initializer")
			}
			if owner < 0 {
				return nil, fail(i, "package-level match initializer unsupported")
			}
			start = j
			mode = "var"
			name = ts[j+1].s
			typ = strings.TrimSpace(string(src[ts[j+2].a:ts[i-1].a]))
			if _, e := parser.ParseExpr(typ); e != nil {
				return nil, fail(i, "invalid or unsupported explicit result type")
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
		serial++
		tmp := fmt.Sprintf("__hgo_match_expr_%d", serial)
		var b strings.Builder
		fmt.Fprintf(&b, "var %s %s;\nmatch %s {\n", tmp, typ, src[ts[i].b:ts[j].a])
		for k := j + 1; k < end; {
			if ts[k].k == token.COMMA || ts[k].k == token.SEMICOLON {
				k++
				continue
			}
			arm := k
			for k+1 < end && !(ts[k].k == token.ASSIGN && ts[k+1].k == token.GTR) {
				if e, ok := pairs[k]; ok {
					k = e
				}
				k++
			}
			if k+2 >= end {
				return nil, fail(arm, "missing expression arm")
			}
			expr := k + 2
			if ts[expr].k == token.LBRACE {
				return nil, fail(expr, "block expression arms unsupported")
			}
			p := expr
			for p < end && ts[p].k != token.COMMA && ts[p].k != token.SEMICOLON {
				if ts[p].s == "match" {
					return nil, fail(p, "nested expression match unsupported")
				}
				if e, ok := pairs[p]; ok {
					p = e
				}
				p++
			}
			if p == expr {
				return nil, fail(expr, "empty arm expression")
			}
			fmt.Fprintf(&b, "%s => { %s = (%s) },\n", src[ts[arm].a:ts[k].a], tmp, src[ts[expr].a:ts[p-1].b])
			k = p
		}
		b.WriteString("}\n")
		if mode == "return" {
			fmt.Fprintf(&b, "return %s", tmp)
		} else {
			fmt.Fprintf(&b, "var %s %s = %s", name, typ, tmp)
		}
		edits = append(edits, edit{ts[start].a, ts[end].b, b.String()})
		i = end
	}
	var out bytes.Buffer
	at := 0
	for _, e := range edits {
		out.Write(src[at:e.a])
		out.WriteString(e.s)
		at = e.b
	}
	out.Write(src[at:])
	return out.Bytes(), nil
}
