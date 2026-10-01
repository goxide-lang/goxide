// Package macros implements a deliberately bounded, local declarative macro pass.
// Captures retain caller tokens; template locals are resolved by the Go parser
// before allocating output names. Unsupported definition-site names are errors.
package macros

import (
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

type TokenOrigin struct {
	OutputStart, OutputEnd int
	File                   string
	Line, Column           int
	InvocationLine         int
	SourceStart, SourceEnd int
	Stack                  []sourcemap.Expansion
	Original               bool
}
type Result struct {
	ExpansionCount, EmittedTokens int
	Source                        []byte
	Origins                       []TokenOrigin
}
type tok struct {
	text, before string
	pos          token.Position
	invocation   int
	stack        []sourcemap.Expansion
	original     bool
	scope        *scope
	sourceEnd    int
}
type rule struct{ pattern, body []tok }
type definition struct {
	name   string
	rules  []rule
	pos    token.Position
	end    int
	public bool
	scope  *scope
}
type engine struct {
	pkg                       *Package
	active                    *scope
	callerImports             map[string]string
	addedImports              map[string]string
	usedMacroImports          map[string]bool
	sumHelpers                map[string]string
	intrinsics                map[string]bool
	importPaths               map[string]string
	importCache               map[string]*types.Package
	importer                  types.Importer
	file                      string
	defs                      map[string]definition
	funcs                     map[string]bool
	imports                   map[string]bool
	aliases                   map[string]string
	prefix                    string
	next, expansions, emitted int
}

const maxDepth = 64
const maxExpansions = 4096
const maxTokens = 200000

func Expand(filename string, source []byte) ([]byte, error) {
	r, e := ExpandWithMap(filename, source)
	return r.Source, e
}
func ExpandWithMap(filename string, source []byte) (Result, error) {
	return ExpandWithIntrinsics(filename, source, nil)
}

// ExpandWithIntrinsics is for compiler-produced markers only. The caller must
// reject these reserved names in user input before preparing the source.
func ExpandWithIntrinsics(filename string, source []byte, names []string) (Result, error) {
	return expandPackage(filename, source, names, nil, nil)
}
func expandPackage(filename string, source []byte, names []string, pkg *Package, imp types.Importer) (Result, error) {
	ts, tail, err := lex(filename, source)
	if err != nil {
		return Result{}, err
	}
	if len(ts) > maxTokens {
		return Result{}, fmt.Errorf("%s: macro input token budget exceeded", filename)
	}
	e := engine{importPaths: map[string]string{}, importCache: map[string]*types.Package{}, importer: importer.Default(), file: filename, defs: map[string]definition{}, funcs: map[string]bool{}, imports: map[string]bool{}, aliases: map[string]string{}, prefix: "hgoMacroGenerated"}
	e.intrinsics = map[string]bool{}
	for _, n := range names {
		e.intrinsics[n] = true
	}
	for strings.Contains(string(source), e.prefix) {
		e.prefix += "X"
	}
	clean, tail, err := e.readDefinitions(ts, tail)
	if err != nil {
		return Result{}, err
	}
	if len(e.defs) == 0 && pkg == nil {
		return Result{Source: source}, nil
	}
	for i := 0; i+2 < len(clean); i++ {
		if clean[i].text == "func" && ident(clean[i+1].text) && clean[i+2].text == "(" {
			e.funcs[clean[i+1].text] = true
		}
	}
	fs := token.NewFileSet()
	if f, _ := parser.ParseFile(fs, filename, render(clean), parser.ImportsOnly); f != nil {
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			parts := strings.Split(p, "/")
			name := parts[len(parts)-1]
			if im.Name != nil {
				name = im.Name.Name
			}
			if name != "_" && name != "." {
				e.imports[name] = true
				e.importPaths[name] = p
			}
		}
	}
	if pkg != nil {
		e.pkg = pkg
		e.active = pkg.scopes[filename]
		if e.active == nil {
			return Result{}, fmt.Errorf("unknown macro source file %s", filename)
		}
		e.prefix = fmt.Sprintf("%sF%dN", pkg.prefix, e.active.index)
		e.defs = pkg.defs
		e.funcs = pkg.funcs
		if imp != nil {
			e.importer = imp
		}
		e.callerImports = map[string]string{}
		e.addedImports = map[string]string{}
		e.usedMacroImports = map[string]bool{}
		for _, d := range pkg.defs {
			if d.scope == e.active {
				for _, r := range d.rules {
					for i := 0; i+1 < len(r.body); i++ {
						if r.body[i+1].text == "." && e.active.imports[r.body[i].text] != "" {
							e.usedMacroImports[r.body[i].text] = true
						}
					}
				}
			}
		}
		for alias, path := range e.active.imports {
			if old := e.callerImports[path]; old == "" || alias < old {
				e.callerImports[path] = alias
			}
		}
		for i := range clean {
			clean[i].scope = e.active
		}
	}
	out, err := e.expand(clean, 0)
	if err != nil {
		return Result{}, err
	}
	if pkg != nil {
		out = e.finishImports(out)
	}
	var b strings.Builder
	var origins []TokenOrigin
	for _, t := range out {
		b.WriteString(t.before)
		a := b.Len()
		b.WriteString(t.text)
		origins = append(origins, TokenOrigin{a, b.Len(), t.pos.Filename, t.pos.Line, t.pos.Column, t.invocation, t.pos.Offset, t.sourceEnd, t.stack, t.original})
	}
	b.WriteString(tail)
	keys := make([]string, 0, len(e.aliases))
	for key := range e.aliases {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.HasPrefix(key, "func ") {
			b.WriteString("\n" + e.sumHelpers[key] + "\n")
		} else if strings.HasPrefix(key, "type ") {
			fmt.Fprintf(&b, "\ntype %s = %s\n", e.aliases[key], strings.TrimPrefix(key, "type "))
		} else if strings.HasPrefix(key, "const ") {
			fmt.Fprintf(&b, "\nconst %s = %s\n", e.aliases[key], strings.TrimPrefix(key, "const "))
		} else {
			fmt.Fprintf(&b, "\nvar %s = %s\n", e.aliases[key], key)
		}
	}
	return Result{Source: []byte(b.String()), Origins: origins, ExpansionCount: e.expansions, EmittedTokens: e.emitted}, nil
}
func (e *engine) readDefinitions(ts []tok, tail string) ([]tok, string, error) {
	var clean []tok
	for i := 0; i < len(ts); {
		public := ts[i].text == "pub" && i+1 < len(ts) && ts[i+1].text == "macro_rules"
		if public {
			i++
		}
		if ts[i].text != "macro_rules" {
			clean = append(clean, ts[i])
			i++
			continue
		}
		start := ts[i]
		if i+3 >= len(ts) || ts[i+1].text != "!" || !ident(ts[i+2].text) || ts[i+3].text != "{" {
			return nil, tail, at(start, "expected macro_rules! name { rules }")
		}
		name := ts[i+2].text
		if _, ok := e.defs[name]; ok {
			return nil, tail, at(start, "duplicate macro "+name)
		}
		end, x := closeGroup(ts, i+3)
		if x != nil {
			return nil, tail, x
		}
		d := definition{name: name, pos: start.pos, end: ts[end].sourceEnd, public: public}
		for j := i + 4; j < end; {
			if ts[j].text == ";" {
				j++
				continue
			}
			if ts[j].text != "(" {
				return nil, tail, at(ts[j], "expected parenthesized macro pattern")
			}
			pe, x := closeGroup(ts, j)
			if x != nil {
				return nil, tail, x
			}
			if pe+3 >= end || ts[pe+1].text != "=" || ts[pe+2].text != ">" || ts[pe+3].text != "{" {
				return nil, tail, at(ts[j], "expected => { template }")
			}
			te, x := closeGroup(ts, pe+3)
			if x != nil {
				return nil, tail, x
			}
			if te > end {
				return nil, tail, at(ts[j], "template exceeds macro definition")
			}
			p := append([]tok(nil), ts[j+1:pe]...)
			if _, x := parsePattern(p, false); x != nil {
				return nil, tail, x
			}
			d.rules = append(d.rules, rule{p, append([]tok(nil), ts[pe+4:te]...)})
			j = te + 1
		}
		if len(d.rules) == 0 {
			return nil, tail, at(start, "macro has no rules")
		}
		e.defs[name] = d
		// Keep source line count for untouched declarations and derive attributes.
		trivia := start.before
		if public {
			trivia = ts[i-1].before
		}
		for _, t := range ts[i : end+1] {
			trivia += strings.Repeat("\n", strings.Count(t.before+t.text, "\n"))
		}
		if end+1 < len(ts) {
			ts[end+1].before = trivia + ts[end+1].before
		} else {
			tail = trivia + tail
		}
		i = end + 1
	}
	return clean, tail, nil
}
func at(t tok, s string) error {
	return fmt.Errorf("%s:%d:%d: macro: %s", t.pos.Filename, t.pos.Line, t.pos.Column, s)
}
func ident(s string) bool { return token.IsIdentifier(s) }
func lex(file string, source []byte) ([]tok, string, error) {
	fs := token.NewFileSet()
	f := fs.AddFile(file, -1, len(source))
	var sc scanner.Scanner
	var problem error
	sc.Init(f, source, func(p token.Position, msg string) {
		if strings.Contains(msg, "U+0024") || strings.Contains(msg, "U+0023") {
			return
		}
		if problem == nil {
			problem = fmt.Errorf("%s: %s", p, msg)
		}
	}, scanner.ScanComments)
	var ts []tok
	end := 0
	for {
		p, k, s := sc.Scan()
		if k == token.EOF {
			break
		}
		if k == token.SEMICOLON && s == "\n" {
			continue
		}
		if s == "" {
			s = k.String()
		}
		off := f.Offset(p)
		n := len(s)
		// Go's scanner removes CR bytes from raw-string literals. Token spans
		// must use original source bytes, otherwise trailing backticks are replayed.
		if k == token.STRING && source[off] == '`' {
			endRaw := off + 1
			for endRaw < len(source) && source[endRaw] != '`' {
				endRaw++
			}
			if endRaw < len(source) {
				n = endRaw - off + 1
				s = string(source[off : off+n])
			}
		}
		if k == token.COMMENT {
			continue
		}
		ts = append(ts, tok{text: s, before: string(source[end:off]), pos: fs.PositionFor(p, false), sourceEnd: off + n})
		end = off + n
	}
	return ts, string(source[end:]), problem
}
func render(ts []tok) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.before)
		b.WriteString(t.text)
	}
	return b.String()
}
func closeGroup(ts []tok, i int) (int, error) { return closeGroupDepth(ts, i, 0) }
func closeGroupDepth(ts []tok, i, depth int) (int, error) {
	if depth > 256 {
		return 0, at(ts[i], "token tree depth budget exceeded")
	}
	pairs := map[string]string{"(": ")", "[": "]", "{": "}"}
	want, ok := pairs[ts[i].text]
	if !ok {
		return 0, at(ts[i], "expected token group")
	}
	for j := i + 1; j < len(ts); j++ {
		if _, ok := pairs[ts[j].text]; ok {
			n, e := closeGroupDepth(ts, j, depth+1)
			if e != nil {
				return 0, e
			}
			j = n
			continue
		}
		if ts[j].text == want {
			return j, nil
		}
		if ts[j].text == ")" || ts[j].text == "]" || ts[j].text == "}" {
			return 0, at(ts[j], "mismatched closing delimiter")
		}
	}
	return 0, at(ts[i], "unclosed token group")
}

type part struct {
	literal    string
	name, kind string
	body       []part
	sep        string
	plus       bool
	where      tok
}

func parsePattern(ts []tok, nested bool) ([]part, error) {
	var ps []part
	seen := map[string]bool{}
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.text != "$" {
			ps = append(ps, part{literal: t.text, where: t})
			continue
		}
		if i+1 >= len(ts) {
			return nil, at(t, "dangling $")
		}
		if ts[i+1].text == "(" {
			if nested {
				return nil, at(t, "nested pattern repetition is not supported")
			}
			end, e := closeGroup(ts, i+1)
			if e != nil {
				return nil, e
			}
			body, e := parsePattern(ts[i+2:end], true)
			if e != nil {
				return nil, e
			}
			j := end + 1
			sep := ""
			if j < len(ts) && ts[j].text != "*" && ts[j].text != "+" {
				sep = ts[j].text
				j++
			}
			if j >= len(ts) || (ts[j].text != "*" && ts[j].text != "+") {
				return nil, at(t, "repetition needs * or +")
			}
			if sep != "," || len(body) != 1 || body[0].name == "" {
				return nil, at(t, "prototype repetition requires $( $name:fragment ),* or ,+")
			}
			if seen[body[0].name] {
				return nil, at(t, "duplicate capture "+body[0].name)
			}
			seen[body[0].name] = true
			ps = append(ps, part{body: body, sep: sep, plus: ts[j].text == "+", where: t})
			i = j
			continue
		}
		if i+3 >= len(ts) || !ident(ts[i+1].text) || ts[i+2].text != ":" {
			return nil, at(t, "expected $name:fragment")
		}
		name, kind := ts[i+1].text, ts[i+3].text
		if seen[name] {
			return nil, at(t, "duplicate capture "+name)
		}
		seen[name] = true
		if kind != "expr" && kind != "ident" && kind != "tt" {
			return nil, at(t, "unsupported fragment "+kind)
		}
		ps = append(ps, part{name: name, kind: kind, where: t})
		i += 3
	}
	for i, p := range ps {
		if len(p.body) > 0 && i != len(ps)-1 {
			return nil, at(p.where, "repetition must be terminal in this prototype")
		}
		if p.kind == "expr" && i+1 < len(ps) && ps[i+1].literal != "," {
			return nil, at(p.where, "expr must be followed by comma or end; ambiguous pattern")
		}
	}
	return ps, nil
}

type capture struct {
	kind     string
	repeated bool
	values   [][]tok
}

func take(kind string, ts []tok) ([]tok, int, bool) {
	if len(ts) == 0 {
		return nil, 0, false
	}
	if kind == "ident" {
		return ts[:1], 1, ident(ts[0].text)
	}
	if kind == "tt" {
		if ts[0].text == "(" || ts[0].text == "[" || ts[0].text == "{" {
			n, e := closeGroup(ts, 0)
			return ts[:n+1], n + 1, e == nil
		}
		return ts[:1], 1, true
	}
	n := 0
	for n < len(ts) && ts[n].text != "," {
		if ts[n].text == "(" || ts[n].text == "[" || ts[n].text == "{" {
			end, e := closeGroup(ts, n)
			if e != nil {
				return nil, 0, false
			}
			n = end
		}
		n++
	}
	if n == 0 {
		return nil, 0, false
	}
	check := append([]tok(nil), ts[:n]...)
	for i := 0; i+2 < len(check); i++ {
		if ident(check[i].text) && check[i+1].text == "!" && (check[i+2].text == "(" || check[i+2].text == "{" || check[i+2].text == "[") {
			end, e := closeGroup(check, i+2)
			if e != nil {
				return nil, 0, false
			}
			check = append(check[:i+1], check[end+1:]...)
		}
	}
	_, e := parser.ParseExpr(render(check))
	return ts[:n], n, e == nil
}
func match(ps []part, ts []tok) (map[string]capture, bool) {
	cs := map[string]capture{}
	i := 0
	for _, p := range ps {
		if p.literal != "" {
			if i >= len(ts) || ts[i].text != p.literal {
				return nil, false
			}
			i++
			continue
		}
		if len(p.body) > 0 {
			c := capture{kind: p.body[0].kind, repeated: true}
			for i < len(ts) {
				v, n, ok := take(p.body[0].kind, ts[i:])
				if !ok {
					return nil, false
				}
				c.values = append(c.values, v)
				i += n
				if i < len(ts) {
					if ts[i].text != p.sep {
						return nil, false
					}
					i++
					if i == len(ts) {
						return nil, false
					}
				}
			}
			if p.plus && len(c.values) == 0 {
				return nil, false
			}
			cs[p.body[0].name] = c
			continue
		}
		v, n, ok := take(p.kind, ts[i:])
		if !ok {
			return nil, false
		}
		cs[p.name] = capture{kind: p.kind, values: [][]tok{v}}
		i += n
	}
	return cs, i == len(ts)
}
func (e *engine) fresh() string { e.next++; return fmt.Sprintf("%s%d", e.prefix, e.next) }
func (e *engine) expand(ts []tok, depth int) ([]tok, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("%s: macro expansion depth budget exceeded (%d)", e.file, maxDepth)
	}
	var out []tok
	for i := 0; i < len(ts); i++ {
		start := i
		qualified := i+4 < len(ts) && ident(ts[i].text) && ts[i+1].text == "." && ident(ts[i+2].text) && ts[i+3].text == "!"
		if qualified {
			if e.pkg == nil {
				return nil, at(ts[i], "cross-package macros require directory compilation")
			}
			i += 2
		}
		if i+2 >= len(ts) || !ident(ts[i].text) || ts[i+1].text != "!" || (ts[i+2].text != "(" && ts[i+2].text != "[" && ts[i+2].text != "{") {
			i = start
			out = append(out, ts[i])
			continue
		}
		call := ts[i]
		d, ok := e.defs[call.text]
		if e.pkg != nil {
			var err error
			d, err = e.resolveMacro(ts[start], call.text, qualified)
			if err != nil {
				return nil, err
			}
			ok = true
		}
		if !ok {
			return nil, at(call, "unknown macro "+call.text)
		}
		if qualified {
			call.before = ts[start].before
			call.pos = ts[start].pos
		}
		end, x := closeGroup(ts, i+2)
		if x != nil {
			return nil, x
		}
		e.expansions++
		if e.expansions > maxExpansions {
			return nil, at(call, "expansion count budget exceeded")
		}
		var chosen *rule
		var cs map[string]capture
		for n := range d.rules {
			ps, _ := parsePattern(d.rules[n].pattern, false)
			if m, ok := match(ps, ts[i+3:end]); ok {
				chosen = &d.rules[n]
				cs = m
				break
			}
		}
		if chosen == nil {
			return nil, at(call, "no rule matches "+d.name)
		}
		frame := sourcemap.Expansion{DefinitionOriginal: d.scope != nil, CallsiteOriginal: call.original, Macro: d.name, Definition: sourcemap.Span{File: d.pos.Filename, Start: d.pos.Offset, End: d.end, Line: d.pos.Line, Column: d.pos.Column}, Callsite: sourcemap.Span{File: call.pos.Filename, Start: call.pos.Offset, End: ts[end].sourceEnd, Line: call.pos.Line, Column: call.pos.Column}}
		call.stack = append(append([]sourcemap.Expansion{}, call.stack...), frame)
		placeholders := map[string][]tok{}
		body, x := e.instantiate(chosen.body, cs, -1, placeholders, call)
		if x != nil {
			return nil, x
		}
		body, x = e.expand(body, depth+1)
		if x != nil {
			return nil, fmt.Errorf("%w (expanding %s invoked at %s:%d:%d)", x, d.name, call.pos.Filename, call.pos.Line, call.pos.Column)
		}
		saved := e.active
		if d.scope != nil {
			e.active = d.scope
		}
		body, x = e.hygiene(body, placeholders)
		e.active = saved
		if x != nil {
			return nil, fmt.Errorf("%w (invoked at %s:%d:%d)", x, call.pos.Filename, call.pos.Line, call.pos.Column)
		}
		var filled []tok
		for _, t := range body {
			if value, ok := placeholders[t.text]; ok {
				v := append([]tok(nil), value...)
				if len(v) > 0 {
					v[0].before = t.before + " " + v[0].before
					for j := range v {
						v[j].stack = append([]sourcemap.Expansion{}, t.stack...)
					}
				}
				filled = append(filled, v...)
			} else {
				filled = append(filled, t)
			}
		}
		body, x = e.expand(filled, depth+1)
		if x != nil {
			return nil, fmt.Errorf("%w (expanding %s invoked at %s:%d:%d)", x, d.name, call.pos.Filename, call.pos.Line, call.pos.Column)
		}
		if len(body) > 0 {
			body[0].before = call.before + body[0].before
		}
		e.emitted += len(body)
		if e.emitted > maxTokens {
			return nil, at(call, "output token budget exceeded")
		}
		out = append(out, body...)
		i = end
	}
	return out, nil
}
func (e *engine) instantiate(ts []tok, cs map[string]capture, index int, ph map[string][]tok, call tok) ([]tok, error) {
	var out []tok
	for i := 0; i < len(ts); i++ {
		if len(out) > maxTokens {
			return nil, at(call, "output token budget exceeded")
		}
		t := ts[i]
		t.invocation = call.pos.Line
		t.stack = append([]sourcemap.Expansion{}, call.stack...)
		if t.text != "$" {
			out = append(out, t)
			continue
		}
		if i+1 >= len(ts) {
			return nil, at(t, "dangling template $")
		}
		if ts[i+1].text == "(" {
			if index >= 0 {
				return nil, at(t, "nested template repetition unsupported")
			}
			end, x := closeGroup(ts, i+1)
			if x != nil {
				return nil, x
			}
			j := end + 1
			sep := ""
			if j < len(ts) && ts[j].text != "*" && ts[j].text != "+" {
				sep = ts[j].text
				j++
			}
			if j >= len(ts) || (ts[j].text != "*" && ts[j].text != "+") {
				return nil, at(t, "template repetition requires * or +")
			}
			count := -1
			for k := i + 2; k+1 < end; k++ {
				if ts[k].text == "$" {
					c, ok := cs[ts[k+1].text]
					if ok && c.repeated {
						if count >= 0 && count != len(c.values) {
							return nil, at(t, "repetition length mismatch")
						}
						count = len(c.values)
					}
				}
			}
			if count < 0 {
				return nil, at(t, "template repetition has no repeated capture")
			}
			if ts[j].text == "+" && count == 0 {
				return nil, at(t, "empty + template repetition")
			}
			for n := 0; n < count; n++ {
				if n > 0 && sep != "" {
					out = append(out, tok{text: sep, before: " ", pos: t.pos, invocation: call.pos.Line})
				}
				piece, x := e.instantiate(ts[i+2:end], cs, n, ph, call)
				if x != nil {
					return nil, x
				}
				out = append(out, piece...)
				if len(out) > maxTokens {
					return nil, at(call, "output token budget exceeded")
				}
			}
			i = j
			continue
		}
		name := ts[i+1].text
		c, ok := cs[name]
		if !ok {
			return nil, at(t, "unknown template capture "+name)
		}
		n := 0
		if c.repeated {
			if index < 0 {
				return nil, at(t, "repeated capture used outside repetition")
			}
			n = index
		}
		key := e.fresh()
		value := append([]tok(nil), c.values[n]...)
		if c.kind == "tt" {
			if len(value) > 0 && value[0].text == "(" {
				if _, err := parser.ParseExpr("f" + render(value)); err != nil {
					return nil, at(t, "tt supports parenthesized expression arguments, not statement/type injection")
				}
				value = value[1 : len(value)-1]
				c.kind = "expr"
			} else if _, err := parser.ParseExpr(render(value)); err != nil {
				return nil, at(t, "tt supports expression tokens or parenthesized argument groups")
			}
		}
		if c.kind == "expr" {
			value = append([]tok{{text: "(", pos: t.pos, invocation: call.pos.Line}}, value...)
			value = append(value, tok{text: ")", pos: t.pos, invocation: call.pos.Line})
		}
		ph[key] = value
		if cs[name].kind == "tt" && len(c.values[n]) > 0 && c.values[n][0].text == "(" {
			// Preserve the argument-group skeleton for binding resolution; its contents
			// remain opaque caller syntax. Strip the capture's synthetic parentheses.
			ph[key] = value[1 : len(value)-1]
			open := t
			open.text = "("
			out = append(out, open)
			t.before = ""
			t.text = key
			out = append(out, t)
			close := t
			close.text = ")"
			out = append(out, close)
		} else {
			t.text = key
			out = append(out, t)
		}
		i++
	}
	return out, nil
}
