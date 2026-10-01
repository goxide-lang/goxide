// Package enums lowers the deliberately finite local enum/match prototype.
package enums

import (
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

type Variant struct {
	Name, Payload, CaseName, Constructor string
	Type                                 ast.Expr
	Position                             token.Position
}
type Decl struct {
	Name, Marker, Validator string
	Family                  string
	TypeParams, TypeArgs    []string
	Variants                []Variant
	Position                token.Position
}
type Result struct {
	Source []byte
	Enums  []Decl
}
type tok struct {
	kind       token.Token
	text       string
	start, end int
}
type edit struct {
	start, end int
	text       string
}
type lowerer struct {
	filename      string
	src           []byte
	ts            []tok
	decls         []Decl
	byName        map[string]*Decl
	serial        int
	payloadChecks map[string]string
	fragments     *sourcemap.Fragments
}

func Lower(filename string, source []byte) (Result, error) {
	return LowerMapped(filename, source, nil)
}

func LowerMapped(filename string, source []byte, provenance *sourcemap.Map) (Result, error) {
	if strings.IndexByte(string(source), 0) >= 0 {
		return Result{}, fmt.Errorf("%s: NUL byte in source", filename)
	}
	l := &lowerer{filename: filename, src: source, byName: map[string]*Decl{}}
	if provenance != nil {
		l.fragments = &sourcemap.Fragments{Input: provenance}
	}
	fs := token.NewFileSet()
	f := fs.AddFile(filename, -1, len(source))
	var s scanner.Scanner
	s.Init(f, source, func(token.Position, string) {}, scanner.ScanComments)
	for {
		p, k, v := s.Scan()
		if k == token.EOF {
			break
		}
		if k == token.COMMENT {
			continue
		}
		if v == "" {
			v = k.String()
		}
		off := f.Offset(p)
		n := len(v)
		// go/scanner removes CR bytes from raw-string token text; source spans must not.
		if k == token.STRING && off < len(source) && source[off] == '`' {
			if close := strings.IndexByte(string(source[off+1:]), '`'); close >= 0 {
				n = close + 2
			}
		}
		if k == token.SEMICOLON && v == "\n" {
			n = 0
		}
		l.ts = append(l.ts, tok{k, v, off, off + n})
	}
	for _, t := range l.ts {
		if t.kind == token.IDENT && strings.HasPrefix(t.text, "__hgo_enum_") {
			return Result{}, l.err(t.start, "reserved generated enum identifier")
		}
	}
	edits := []edit{}
	for i := 0; i < len(l.ts); i++ {
		if l.ts[i].text != "enum" {
			continue
		}
		start := i
		if i > 0 && l.ts[i-1].text == "pub" {
			return Result{}, l.err(l.ts[i-1].start, "pub enum requires cross-package ABI support; this prototype supports local enums only")
		}
		d, end, e := l.parseDecl(i)
		if e != nil {
			return Result{}, e
		}
		if _, ok := l.byName[d.Name]; ok {
			return Result{}, l.err(l.ts[i].start, "duplicate enum "+d.Name)
		}
		l.decls = append(l.decls, d)
		l.byName[d.Name] = &l.decls[len(l.decls)-1]
		edits = append(edits, edit{l.ts[start].start, l.ts[end].end, ""})
		i = end
	}
	localCount := len(l.decls)
	if err := l.collectBuiltins(); err != nil {
		return Result{}, err
	}
	if err := l.checkBuiltinBindings(); err != nil {
		return Result{}, err
	}
	if err := l.checkCompositePayloads(); err != nil {
		return Result{}, err
	}
	// Refresh pointers after slice growth.
	for i := range l.decls {
		l.byName[l.decls[i].Name] = &l.decls[i]
	}
	for _, d := range l.decls[:localCount] {
		for _, v := range d.Variants {
			if v.Payload != "" && !scalar(v.Payload) && l.byName[v.Payload] == nil {
				return Result{}, l.err(v.Position.Offset, "unsupported enum payload: use scalar or local non-generic enum")
			}
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(n string) error {
		if visiting[n] {
			return l.err(l.byName[n].Position.Offset, "recursive enum payload requires a future explicit container design")
		}
		if done[n] {
			return nil
		}
		visiting[n] = true
		for _, v := range l.byName[n].Variants {
			if l.byName[v.Payload] != nil {
				if e := visit(v.Payload); e != nil {
					return e
				}
			}
		}
		visiting[n] = false
		done[n] = true
		return nil
	}
	for _, d := range l.decls[:localCount] {
		if e := visit(d.Name); e != nil {
			return Result{}, e
		}
	}
	for i := range edits {
		edits[i].text = l.declaration(l.decls[i])
	}
	out, e := l.region(0, len(source), edits)
	if e != nil {
		return Result{}, e
	}
	out += l.builtinDeclarations()
	if provenance != nil {
		data, m := l.fragments.Finish(out, "enums.Lower")
		*provenance = *m
		out = string(data)
	}
	return Result{[]byte(out), l.decls}, nil
}
func scalar(s string) bool {
	switch s {
	case "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune", "float32", "float64", "complex64", "complex128":
		return true
	}
	return false
}
func (l *lowerer) err(off int, msg string) error {
	line := 1
	col := 1
	for _, c := range l.src[:off] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return fmt.Errorf("%s:%d:%d: enum: %s", l.filename, line, col, msg)
}
func (l *lowerer) position(off int) token.Position {
	line := 1
	col := 1
	for _, c := range l.src[:off] {
		if c == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return token.Position{Filename: l.filename, Offset: off, Line: line, Column: col}
}
func (l *lowerer) close(i int) (int, error) {
	pairs := map[string]string{"(": ")", "{": "}", "[": "]"}
	end := pairs[l.ts[i].text]
	if end == "" {
		return 0, l.err(l.ts[i].start, "expected opening delimiter")
	}
	for j := i + 1; j < len(l.ts); j++ {
		if l.ts[j].text == end {
			return j, nil
		}
		if pairs[l.ts[j].text] != "" {
			k, e := l.close(j)
			if e != nil {
				return 0, e
			}
			j = k
		}
	}
	return 0, l.err(l.ts[i].start, "unclosed delimiter")
}
func (l *lowerer) parseDecl(i int) (Decl, int, error) {
	d := Decl{Position: l.position(l.ts[i].start)}
	if i+2 >= len(l.ts) || l.ts[i+1].kind != token.IDENT || l.ts[i+2].text != "{" {
		return d, 0, l.err(l.ts[i].start, "expected enum Name { ... }; generics unsupported")
	}
	d.Name = l.ts[i+1].text
	if d.Name == "Option" || d.Name == "Result" {
		return d, 0, l.err(l.ts[i+1].start, "reserved builtin sum type name")
	}
	d.Marker = "__hgo_enum_marker_" + d.Name
	d.Validator = "__hgo_enum_validate_" + d.Name
	end, e := l.close(i + 2)
	if e != nil {
		return d, 0, e
	}
	seen := map[string]bool{}
	for j := i + 3; j < end; {
		if l.ts[j].text == "," || l.ts[j].kind == token.SEMICOLON {
			j++
			continue
		}
		t := l.ts[j]
		if t.kind != token.IDENT || t.text == "_" {
			return d, 0, l.err(t.start, "expected variant name")
		}
		if seen[t.text] {
			return d, 0, l.err(t.start, "duplicate variant")
		}
		seen[t.text] = true
		v := Variant{Name: t.text, Position: l.position(t.start), CaseName: "__hgo_enum_case_" + strconv.Itoa(len(d.Name)) + "_" + d.Name + "_" + t.text, Constructor: "__hgo_enum_new_" + strconv.Itoa(len(d.Name)) + "_" + d.Name + "_" + t.text}
		j++
		if j < end && l.ts[j].text == "(" {
			k, e := l.close(j)
			if e != nil {
				return d, 0, e
			}
			v.Payload = strings.TrimSpace(string(l.src[l.ts[j].end:l.ts[k].start]))
			v.Type, e = parser.ParseExpr(v.Payload)
			if e != nil || v.Payload == "" {
				return d, 0, l.err(l.ts[j].start, "expected one payload type")
			}
			j = k + 1
		}
		d.Variants = append(d.Variants, v)
		if j < end && l.ts[j].text != "," && l.ts[j].kind != token.SEMICOLON {
			return d, 0, l.err(l.ts[j].start, "expected comma after variant")
		}
	}
	if len(d.Variants) == 0 {
		return d, 0, l.err(d.Position.Offset, "empty enum unsupported")
	}
	return d, end, nil
}
func (l *lowerer) declaration(d Decl) string {
	var b strings.Builder
	fmt.Fprintf(&b, "type %s interface { %s() };\n", d.Name, d.Marker)
	for _, v := range d.Variants {
		payload := ""
		arg := ""
		value := ""
		if v.Payload != "" {
			payload = "Value " + v.Payload
			arg = "v " + v.Payload
			value = "Value:v"
		}
		fmt.Fprintf(&b, "type %s struct { %s }; func (%s) %s() {}; func %s(%s) %s { return %s{%s} };\n", v.CaseName, payload, v.CaseName, d.Marker, v.Constructor, arg, d.Name, v.CaseName, value)
	}
	fmt.Fprintf(&b, "func %s(v %s) { switch x := v.(type) {\n", d.Validator, d.Name)
	for _, v := range d.Variants {
		fmt.Fprintf(&b, "case %s: _ = x;", v.CaseName)
		if nested := l.byName[v.Payload]; nested != nil {
			fmt.Fprintf(&b, "%s(x.Value);", nested.Validator)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "default: panic(%q) } };\n", "hgo: invalid enum "+d.Name)
	return b.String()
}
func (l *lowerer) region(start, end int, edits []edit) (string, error) {
	var b strings.Builder
	cursor := start
	for i := 0; i < len(l.ts); i++ {
		t := l.ts[i]
		if t.start < cursor || t.start >= end {
			continue
		}
		found := false
		for _, e := range edits {
			if t.start == e.start {
				b.WriteString(l.copySource(cursor, e.start))
				b.WriteString(e.text)
				cursor = e.end
				found = true
				break
			}
		}
		if found {
			continue
		}
		if t.text == "match" { // Statement only; assignment/return/call contexts must not be silently changed.
			if i > 0 {
				p := l.ts[i-1]
				if p.kind != token.SEMICOLON && p.text != "{" && p.text != "}" {
					return "", l.err(t.start, "expression match unsupported; use a statement match")
				}
			}
			text, last, e := l.match(i)
			if e != nil {
				return "", e
			}
			b.WriteString(l.copySource(cursor, t.start))
			b.WriteString(text)
			cursor = l.ts[last].end
			i = last
			continue
		}
		d, colon, qualified, qe := l.qualified(i)
		if qe != nil {
			return "", qe
		}
		if qualified {
			if d == nil {
				return "", l.err(t.start, "unknown or cross-package enum constructor")
			}
			name := l.ts[colon+2].text
			var v *Variant
			for j := range d.Variants {
				if d.Variants[j].Name == name {
					v = &d.Variants[j]
				}
			}
			if v == nil {
				return "", l.err(t.start, "unknown enum variant "+name)
			}
			b.WriteString(l.copySource(cursor, t.start))
			b.WriteString(v.Constructor)
			cursor = l.ts[colon+2].end
			i = colon + 2
			if v.Payload == "" {
				if i+1 < len(l.ts) && l.ts[i+1].text == "(" {
					return "", l.err(t.start, "unit variant takes no arguments")
				}
				b.WriteString("()")
			} else if i+1 >= len(l.ts) || l.ts[i+1].text != "(" {
				return "", l.err(t.start, "payload constructor requires argument")
			}
			continue
		}
	}
	b.WriteString(l.copySource(cursor, end))
	return b.String(), nil
}

type arm struct {
	variant, bind, guard, body string
	pos                        int
}

func (l *lowerer) match(i int) (string, int, error) {
	j := i + 1
	for j < len(l.ts) && l.ts[j].text != "{" {
		if l.ts[j].text == "(" || l.ts[j].text == "[" {
			k, e := l.close(j)
			if e != nil {
				return "", 0, e
			}
			j = k
		}
		j++
	}
	if j >= len(l.ts) {
		return "", 0, l.err(l.ts[i].start, "match missing body")
	}
	end, e := l.close(j)
	if e != nil {
		return "", 0, e
	}
	target, e := l.region(l.ts[i].end, l.ts[j].start, nil)
	if e != nil {
		return "", 0, e
	}
	var d *Decl
	arms := []arm{}
	for k := j + 1; k < end; {
		if l.ts[k].text == "," || l.ts[k].kind == token.SEMICOLON {
			k++
			continue
		}
		a := arm{pos: l.ts[k].start}
		nd, colon, qualified, qe := l.qualified(k)
		if qe != nil {
			return "", 0, qe
		}
		if qualified {
			if nd == nil {
				return "", 0, l.err(a.pos, "unknown enum in pattern")
			}
			if d != nil && d != nd {
				return "", 0, l.err(a.pos, "mixed enum identities in match")
			}
			d = nd
			a.variant = l.ts[colon+2].text
			k = colon + 3
			if k < end && l.ts[k].text == "(" {
				p, e := l.close(k)
				if e != nil {
					return "", 0, e
				}
				if p != k+2 || l.ts[k+1].kind != token.IDENT {
					return "", 0, l.err(a.pos, "nested or complex patterns unsupported")
				}
				a.bind = l.ts[k+1].text
				k = p + 1
			}
		} else if l.ts[k].kind == token.IDENT {
			a.bind = l.ts[k].text
			k++
		} else {
			return "", 0, l.err(a.pos, "unsupported pattern")
		}
		if k < end && l.ts[k].text == "if" {
			g := k + 1
			k = g
			for k+1 < end && !(l.ts[k].text == "=" && l.ts[k+1].text == ">") {
				if l.ts[k].text == "(" || l.ts[k].text == "[" || l.ts[k].text == "{" {
					p, e := l.close(k)
					if e != nil {
						return "", 0, e
					}
					k = p
				}
				k++
			}
			if k >= end {
				return "", 0, l.err(a.pos, "missing =>")
			}
			a.guard, e = l.region(l.ts[g].start, l.ts[k].start, nil)
			if e != nil {
				return "", 0, e
			}
		}
		if k+2 >= end || l.ts[k].text != "=" || l.ts[k+1].text != ">" || l.ts[k+2].text != "{" {
			return "", 0, l.err(a.pos, "statement arm requires => { ... }")
		}
		p, e := l.close(k + 2)
		if e != nil {
			return "", 0, e
		}
		a.body, e = l.region(l.ts[k+2].end, l.ts[p].start, nil)
		if e != nil {
			return "", 0, e
		}
		arms = append(arms, a)
		k = p + 1
	}
	if d == nil {
		return "", 0, l.err(l.ts[i].start, "match requires at least one qualified local enum pattern in this prototype")
	}
	covered := map[string]bool{}
	all := false
	for _, a := range arms {
		if all || len(covered) == len(d.Variants) || a.variant != "" && covered[a.variant] {
			return "", 0, l.err(a.pos, "unreachable match arm")
		}
		if a.variant != "" {
			var v *Variant
			for x := range d.Variants {
				if d.Variants[x].Name == a.variant {
					v = &d.Variants[x]
				}
			}
			if v == nil {
				return "", 0, l.err(a.pos, "unknown variant")
			}
			if (v.Payload == "") != (a.bind == "") {
				return "", 0, l.err(a.pos, "payload pattern arity mismatch")
			}
		}
		if a.guard == "" {
			if a.variant == "" {
				all = true
			} else {
				covered[a.variant] = true
			}
		}
	}
	if !all && len(covered) != len(d.Variants) {
		return "", 0, l.err(l.ts[i].start, "non-exhaustive match; guards do not contribute coverage")
	}
	l.serial++
	id := strconv.Itoa(l.serial)
	saved := "__hgo_enum_saved_" + id
	matched := "__hgo_enum_matched_" + id
	var b strings.Builder
	fmt.Fprintf(&b, "{ var %s %s = (%s); %s(%s); %s := (0 != 0); _ = %s;\n", saved, d.Name, target, d.Validator, saved, matched, matched)
	for _, a := range arms {
		fmt.Fprintf(&b, "if !%s {\n", matched)
		if a.variant != "" {
			var v Variant
			for _, x := range d.Variants {
				if x.Name == a.variant {
					v = x
				}
			}
			tmp := "__hgo_enum_payload_" + id
			if d.Family == "Option" && v.Name == "None" {
				fmt.Fprintf(&b, "if %s == %s() {\n", saved, v.Constructor)
			} else {
				fmt.Fprintf(&b, "if %s, __hgo_enum_ok_%s := %s.(%s); __hgo_enum_ok_%s { _ = %s;\n", tmp, id, saved, v.CaseName, id, tmp)
			}
			if a.bind != "" && a.bind != "_" {
				fmt.Fprintf(&b, "%s := %s.Value; _ = %s;\n", a.bind, tmp, a.bind)
			}
		} else if a.bind != "_" {
			fmt.Fprintf(&b, "%s := %s; _ = %s;\n", a.bind, saved, a.bind)
		}
		if a.guard != "" {
			fmt.Fprintf(&b, "if %s {\n", a.guard)
		}
		fmt.Fprintf(&b, "%s = (0 == 0); %s\n", matched, a.body)
		if a.guard != "" {
			b.WriteString("}\n")
		}
		if a.variant != "" {
			b.WriteString("}\n")
		}
		b.WriteString("}\n")
	}
	b.WriteString("}")
	return b.String(), end, nil
}

// Generated validation needs the real panic builtin and real predeclared scalar
// payload types. Source locals may shadow these freely; package-level bindings
// would silently change generated declaration semantics and are rejected.
func (l *lowerer) checkBuiltinBindings() error {
	if len(l.decls) == 0 {
		return nil
	}
	needed := map[string]bool{"panic": true}
	for _, d := range l.decls {
		if d.Family != "" {
			needed["Option"] = true
			needed["Result"] = true
			needed["nil"] = true
			needed["any"] = true
			needed["error"] = true
		}
	}
	for _, d := range l.decls {
		for _, v := range d.Variants {
			if scalar(v.Payload) {
				needed[v.Payload] = true
			}
		}
	}
	check := func(t tok) error {
		if needed[t.text] {
			return l.err(t.start, "package binding shadows enum-required builtin "+t.text)
		}
		return nil
	}
	depth := 0
	for i := 0; i < len(l.ts); i++ {
		t := l.ts[i]
		if t.text == "{" {
			depth++
			continue
		}
		if t.text == "}" {
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if t.kind == token.FUNC {
			if i+1 < len(l.ts) && l.ts[i+1].kind == token.IDENT {
				if e := check(l.ts[i+1]); e != nil {
					return e
				}
			}
			continue
		}
		if t.kind != token.TYPE && t.kind != token.VAR && t.kind != token.CONST && t.kind != token.IMPORT {
			continue
		}
		j := i + 1
		if j >= len(l.ts) {
			continue
		}
		group := l.ts[j].text == "("
		end := j
		if group {
			var e error
			end, e = l.close(j)
			if e != nil {
				return e
			}
			j++
		} else {
			end = len(l.ts)
		}
		first := true
		for ; j < end; j++ {
			q := l.ts[j]
			if q.kind == token.SEMICOLON {
				if !group {
					break
				}
				first = true
				continue
			}
			if first {
				if q.kind == token.IDENT {
					if e := check(q); e != nil {
						return e
					}
				}
				if t.kind == token.IMPORT && q.kind == token.STRING {
					path, e := strconv.Unquote(q.text)
					if e == nil {
						parts := strings.Split(path, "/")
						name := parts[len(parts)-1]
						if needed[name] {
							return l.err(q.start, "import shadows enum-required builtin "+name)
						}
					}
				}
				first = false
				if t.kind == token.VAR || t.kind == token.CONST {
					for j+2 < end && l.ts[j+1].text == "," && l.ts[j+2].kind == token.IDENT {
						j += 2
						if e := check(l.ts[j]); e != nil {
							return e
						}
					}
				}
			}
			if q.text == "(" || q.text == "[" || q.text == "{" {
				k, e := l.close(j)
				if e != nil {
					return e
				}
				j = k
			}
		}
	}
	return nil
}

func (l *lowerer) copySource(a, b int) string {
	if l.fragments != nil {
		return l.fragments.Copy(a, b)
	}
	return string(l.src[a:b])
}
