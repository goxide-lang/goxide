// Package matchexpr normalizes a finite expression-match subset to statement match.
package matchexpr

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
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
	callees := map[string][]string{}
	shadowNames := map[string]bool{}
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
				if d.Recv != nil {
					for _, field := range d.Recv.List {
						for _, name := range field.Names {
							shadowNames[name.Name] = true
						}
					}
				}
				if d.Recv == nil && (ft.TypeParams == nil || len(ft.TypeParams.List) == 0) {
					var params []string
					valid := true
					for _, field := range ft.Params.List {
						if _, variadic := field.Type.(*ast.Ellipsis); variadic {
							valid = false
						}
						var b bytes.Buffer
						format.Node(&b, token.NewFileSet(), field.Type)
						count := len(field.Names)
						if count == 0 {
							count = 1
						}
						for n := 0; n < count; n++ {
							params = append(params, b.String())
						}
					}
					// Preserve compiler call contracts that require direct arguments.
					previous := 0
					if i > 0 {
						previous = ts[i-1].b
					}
					if strings.Contains(string(src[previous:t.a]), "//go:") {
						valid = false
					}
					if valid {
						callees[d.Name.Name] = params
					}
				}
			}
		}
		if ft == nil {
			// An hgo-only signature (for example &T) is not host-parseable.
			// Its identifiers cannot establish a static callee identity.
			for n := i + 1; n < j; n++ {
				if ts[n].k == token.IDENT {
					shadowNames[ts[n].s] = true
				}
			}
		}
		if ft != nil {
			for _, fields := range []*ast.FieldList{ft.Params, ft.Results} {
				if fields != nil {
					for _, field := range fields.List {
						for _, name := range field.Names {
							shadowNames[name.Name] = true
						}
					}
				}
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
	// A deliberately finite resolution proof: any non-call occurrence means
	// the name may be shadowed or used as a value. Ordinary Go is unchanged.
	for i, t := range ts {
		if (t.k != token.VAR && t.k != token.TYPE && t.k != token.CONST) || i+1 >= len(ts) {
			continue
		}
		j, end := i+1, len(ts)
		grouped := ts[j].k == token.LPAREN
		if grouped {
			end = pairs[j]
			j++
		}
		for j < end {
			if ts[j].k == token.SEMICOLON {
				j++
				continue
			}
			for j < end && ts[j].k == token.IDENT {
				shadowNames[ts[j].s] = true
				j++
				if j >= end || ts[j].k != token.COMMA {
					break
				}
				j++
			}
			if !grouped {
				break
			}
			for j < end && ts[j].k != token.SEMICOLON {
				if e, ok := pairs[j]; ok {
					j = e
				}
				j++
			}
		}
	}

	for name := range callees {
		if shadowNames[name] {
			delete(callees, name)
			continue
		}
		for n, t := range ts {
			if t.k == token.IDENT && t.s == name && (n+1 == len(ts) || ts[n+1].k != token.LPAREN) {
				delete(callees, name)
				break
			}
		}
	}
	var edits []edit
	c := composer{ts: ts, pairs: pairs, copy: copySource, fail: fail, callees: callees, hash: fmt.Sprintf("%x", sha256.Sum256([]byte(filename)))[:24]}

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
		// Find the outermost direct call enclosing this match. Only a whole
		// return expression or expression statement is hoisted here.
		root, close := -1, -1
		for n := i - 1; owner >= 0 && n > funcs[owner].open; n-- {
			if ts[n].k == token.LPAREN && pairs[n] > i && n > 0 && ts[n-1].k == token.IDENT {
				if _, ok := callees[ts[n-1].s]; ok {
					root, close = n-1, pairs[n]
				}
			}
		}
		if root >= 0 && owner >= 0 && root > 0 && (ts[root-1].k == token.RETURN || ts[root-1].k == token.LBRACE || ts[root-1].k == token.SEMICOLON) && (close+1 == len(ts) || ts[close+1].k == token.SEMICOLON || ts[close+1].k == token.RBRACE) {
			pre, call, err := c.call(root, close+1)
			if err != nil {
				return nil, err
			}
			start := root
			prefix := ""
			if ts[root-1].k == token.RETURN {
				start--
				prefix = "return "
			}
			edits = append(edits, edit{ts[start].a, ts[close].b, "{\n" + pre + prefix + call + "\n}"})
			i = close
			continue
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
		tmp := c.temp()
		var b strings.Builder
		fmt.Fprintf(&b, "var %s %s;\n", tmp, typ)
		body, err := c.assign(i, end+1, tmp, typ)
		if err != nil {
			return nil, err
		}
		b.WriteString(body)

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
		out.WriteString(copySource(at, e.a))
		out.WriteString(e.s)
		at = e.b
	}
	out.WriteString(copySource(at, len(src)))
	out.WriteString(c.aliases.String())
	if provenance != nil {
		data, m := fragments.Finish(out.String(), "matchexpr.Lower")
		*provenance = *m
		return data, nil
	}
	return out.Bytes(), nil
}
