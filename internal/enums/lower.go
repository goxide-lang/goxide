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
	SerdeRename, SerdeValue              string
	HasSerdeRename, HasSerdeValue        bool
	Name, Payload, CaseName, Constructor string
	Type                                 ast.Expr
	Message                              string
	HasMessage, Source, From, Default    bool
	Position                             token.Position
}
type Decl struct {
	Name, Marker, Validator string
	// ValidatorPath records an external instance owner independently of its family origin.
	ValidatorPath string
	// ExportValidator is the stable full-digest ABI name. Validator is private
	// generated spelling and must never be used to reconstruct this identity.
	ExportValidator string
	// PackagePath is set only by the module compiler for trusted producer metadata.
	PackagePath, LookupName string
	ConstructorType         string
	Instances               []Decl
	Public                  bool
	Family                  string
	TypeParams, TypeArgs    []string
	Variants                []Variant
	Position                token.Position
}
type Result struct {
	Source []byte
	Enums  []Decl
	Types  map[string]ast.Expr
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
	packageTypes  map[string]ast.Expr
	fragments     *sourcemap.Fragments
}

func Lower(filename string, source []byte) (Result, error) {
	return LowerMapped(filename, source, nil)
}

func LowerMapped(filename string, source []byte, provenance *sourcemap.Map) (Result, error) {
	return lowerPackage(filename, source, provenance, nil, false)
}

// Collect parses declaration metadata without merging file text or import scopes.
func Collect(filename string, source []byte) ([]Decl, error) {
	r, err := lowerPackage(filename, source, nil, nil, true)
	return r.Enums, err
}

// CollectPackage also returns independently parsed named payload types.
func CollectPackage(filename string, source []byte) (Result, error) {
	return lowerPackageTypes(filename, source, nil, nil, map[string]ast.Expr{}, true)
}

// LowerPackage resolves enum names against independently parsed package declarations.
func LowerPackage(filename string, source []byte, provenance *sourcemap.Map, declarations []Decl, named map[string]ast.Expr) (Result, error) {
	return lowerPackageTypes(filename, source, provenance, declarations, named, false)
}
func lowerPackage(filename string, source []byte, provenance *sourcemap.Map, declarations []Decl, collect bool) (Result, error) {
	return lowerPackageTypes(filename, source, provenance, declarations, nil, collect)
}
func lowerPackageTypes(filename string, source []byte, provenance *sourcemap.Map, declarations []Decl, named map[string]ast.Expr, collect bool) (Result, error) {
	if strings.IndexByte(string(source), 0) >= 0 {
		return Result{}, fmt.Errorf("%s: NUL byte in source", filename)
	}
	l := &lowerer{filename: filename, src: source, byName: map[string]*Decl{}, packageTypes: named}
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
		if t.kind == token.IDENT && strings.HasPrefix(t.text, "__goxide_enum_") {
			return Result{}, l.err(t.start, "reserved generated enum identifier")
		}
	}
	edits := []edit{}
	var separators []edit
	for i := 0; i < len(l.ts); i++ {
		if l.ts[i].text != "enum" {
			continue
		}
		start := i
		public := i > 0 && l.ts[i-1].text == "pub"
		if public {
			start = i - 1
			if declarations == nil && !collect {
				return Result{}, l.err(l.ts[start].start, "pub enum requires directory package compilation")
			}
		}
		d, end, e := l.parseDecl(i)
		if e != nil {
			return Result{}, e
		}
		d.Public = public
		if public && !ast.IsExported(d.Name) {
			return Result{}, l.err(l.ts[i].start, "pub enum requires an exported Go name")
		}
		if _, ok := l.byName[d.Name]; ok {
			return Result{}, l.err(l.ts[i].start, "duplicate enum "+d.Name)
		}
		l.decls = append(l.decls, d)
		l.byName[d.Name] = &l.decls[len(l.decls)-1]
		edits = append(edits, edit{l.ts[start].start, l.ts[end].end, ""})
		// Generated declarations already terminate themselves. Remove only the
		// explicit source separator, preserving comments between it and the enum.
		if end+1 < len(l.ts) && l.ts[end+1].kind == token.SEMICOLON && l.ts[end+1].text == ";" {
			separator := l.ts[end+1]
			separators = append(separators, edit{separator.start, separator.end, ""})
		}
		i = end
	}
	if collect {
		named, err := l.collectNamedTypes()
		return Result{Enums: l.decls, Types: named}, err
	}
	for i := range declarations {
		d := &declarations[i]
		if l.byName[d.Name] == nil {
			l.byName[d.Name] = d
		}
		if d.LookupName != "" {
			l.byName[d.LookupName] = d
		}
	}
	localCount := len(l.decls)
	if err := l.collectBuiltins(); err != nil {
		return Result{}, err
	}
	if err := CheckBuiltinValidatorNames(append(append([]Decl(nil), declarations...), l.decls...)); err != nil {
		return Result{}, l.err(0, err.Error())
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
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(n string) error {
		if visiting[n] {
			return fmt.Errorf("%s: enum: recursive enum payload requires a future explicit container design", l.byName[n].Position)
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
	for _, d := range l.decls {
		if d.Family != "" {
			for i, t := range l.ts {
				if t.text == "package" && i+1 < len(l.ts) {
					end := l.ts[i+1].end
					edits = append(edits, edit{t.start, end, string(source[t.start:end]) + ";import __goxide_enum_std " + strconv.Quote(SumPackage)})
					break
				}
			}
			break
		}
	}
	edits = append(edits, separators...)
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
	return Result{Source: []byte(out), Enums: l.decls}, nil
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
	if i+3 < len(l.ts) && l.ts[i+1].kind == token.IDENT && l.ts[i+2].kind == token.IDENT && l.ts[i+3].text == "{" {
		return d, 0, l.err(l.ts[i+2].start, "raw/C enum underlying types are unsupported; use a closed variant enum")
	}
	if i+2 >= len(l.ts) || l.ts[i+1].kind != token.IDENT || l.ts[i+2].text != "{" {
		return d, 0, l.err(l.ts[i].start, "expected enum Name { ... }; generics unsupported")
	}
	d.Name = l.ts[i+1].text
	if d.Name == "Option" || d.Name == "Result" {
		return d, 0, l.err(l.ts[i+1].start, "reserved builtin sum type name")
	}
	d.Marker = "__goxide_enum_marker_" + d.Name
	d.Validator = "__goxide_enum_validate_" + d.Name
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
		attr := Variant{}
		for j < end && l.ts[j].text == "#" {
			if j+1 >= end || l.ts[j+1].text != "[" {
				return d, 0, l.err(l.ts[j].start, "invalid variant attribute")
			}
			k, e := l.close(j + 1)
			if e != nil {
				return d, 0, e
			}
			body := strings.TrimSpace(string(l.src[l.ts[j+1].end:l.ts[k].start]))
			switch {
			case strings.HasPrefix(body, "serde(") && strings.HasSuffix(body, ")"):
				if err := parseSerdeVariantAttr(body, &attr); err != nil {
					return d, 0, l.err(l.ts[j].start, err.Error())
				}
			case strings.HasPrefix(body, "error(") && strings.HasSuffix(body, ")"):
				if attr.HasMessage {
					return d, 0, l.err(l.ts[j].start, "duplicate error message")
				}
				msg, e := strconv.Unquote(body[6 : len(body)-1])
				if e != nil {
					return d, 0, l.err(l.ts[j].start, "error requires quoted message")
				}
				attr.Message = msg
				attr.HasMessage = true
			case body == "default":
				if attr.Default {
					return d, 0, l.err(l.ts[j].start, "duplicate default attribute")
				}
				attr.Default = true
			case body == "source":
				if attr.Source {
					return d, 0, l.err(l.ts[j].start, "duplicate source attribute")
				}
				attr.Source = true
			case body == "from":
				if attr.From {
					return d, 0, l.err(l.ts[j].start, "duplicate from attribute")
				}
				attr.From = true
			default:
				return d, 0, l.err(l.ts[j].start, "enum variant attributes are unsupported: "+body)
			}
			j = k + 1
			for j < end && l.ts[j].kind == token.SEMICOLON {
				j++
			}
		}
		if j >= end {
			return d, 0, l.err(d.Position.Offset, "variant attribute has no target")
		}
		t := l.ts[j]
		if t.kind != token.IDENT || t.text == "_" {
			return d, 0, l.err(t.start, "expected variant name")
		}
		if seen[t.text] {
			return d, 0, l.err(t.start, "duplicate variant")
		}
		seen[t.text] = true
		v := Variant{Name: t.text, Position: l.position(t.start), CaseName: "__goxide_enum_case_" + strconv.Itoa(len(d.Name)) + "_" + d.Name + "_" + t.text, Constructor: "__goxide_enum_new_" + strconv.Itoa(len(d.Name)) + "_" + d.Name + "_" + t.text}
		v.Message, v.HasMessage, v.Source, v.From, v.Default = attr.Message, attr.HasMessage, attr.Source, attr.From, attr.Default
		v.SerdeRename, v.SerdeValue, v.HasSerdeRename, v.HasSerdeValue = attr.SerdeRename, attr.SerdeValue, attr.HasSerdeRename, attr.HasSerdeValue
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
	methods := ""
	for _, v := range d.Variants {
		if v.HasMessage {
			methods = "Error() string; Unwrap() error;"
		}
	}
	fmt.Fprintf(&b, "type %s interface { %s(); %s };\n", d.Name, d.Marker, methods)
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
		b.WriteString(l.payloadChecks[d.Name+"/"+v.Name])
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "default: panic(%q) } };\n", "goxide: invalid enum "+d.Name)
	if d.Public {
		for _, v := range d.Variants {
			fmt.Fprintf(&b, "type GoxideCase%d_%s_%s = %s;\n", len(d.Name), d.Name, v.Name, v.CaseName)
			arg, call := "", ""
			if v.Payload != "" {
				arg, call = "v "+v.Payload, "v"
			}
			fmt.Fprintf(&b, "func New%s%s(%s) %s { return %s(%s) };\n", d.Name, v.Name, arg, d.Name, v.Constructor, call)
		}
		fmt.Fprintf(&b, "func Validate%s(v %s) { %s(v) };\n", d.Name, d.Name, d.Validator)
	}
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
		if (t.text == "Option" || t.text == "Result") && i+3 < len(l.ts) && l.ts[i+1].text == ":" && l.ts[i+2].text == ":" {
			variant := l.ts[i+3].text
			valid := (t.text == "Option" && (variant == "Some" || variant == "None")) || (t.text == "Result" && (variant == "Ok" || variant == "Err"))
			if !valid {
				return "", l.err(t.start, "unknown builtin variant "+variant)
			}
			b.WriteString(l.copySource(cursor, t.start))
			b.WriteString("__goxide_enum_pending_" + t.text + "_" + variant)
			cursor = l.ts[i+3].end
			i += 3
			if variant == "None" {
				b.WriteString("()")
			} else if i+1 >= len(l.ts) || l.ts[i+1].text != "(" {
				return "", l.err(t.start, "payload constructor requires argument")
			}
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
			if d.ConstructorType != "" {
				end := colon + 2
				payload := ""
				if v.Payload == "" {
					if end+1 < len(l.ts) && l.ts[end+1].text == "(" {
						return "", l.err(t.start, "unit variant takes no arguments")
					}
				} else {
					if end+1 >= len(l.ts) || l.ts[end+1].text != "(" {
						return "", l.err(t.start, "payload constructor requires argument")
					}
					close, err := l.close(end + 1)
					if err != nil {
						return "", err
					}
					payload, err = l.region(l.ts[end+1].end, l.ts[close].start, nil)
					if err != nil {
						return "", err
					}
					end = close
				}
				b.WriteString(l.copySource(cursor, t.start))
				// Keep the exact source alias instantiation in Go's type checker;
				// canonical helpers must not erase its type-parameter constraints.
				b.WriteString("(" + d.ConstructorType + ")(" + v.Constructor + "(" + payload + "))")
				cursor = l.ts[end].end
				i = end
				continue
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
	// Preserve the arm source until the real scrutinee type is available.
	for _, head := range contextPatternHeads(l.ts[j : end+1]) {
		k := j + head
		if k+2 < end && (l.ts[k].text == "Option" || l.ts[k].text == "Result") && l.ts[k+1].text == ":" && l.ts[k+2].text == ":" {
			body := string(l.src[l.ts[j].start:l.ts[end].end])
			return "__goxide_enum_context_match(" + target + "," + strconv.Quote(body) + ")", end, nil
		}
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
			if d != nil && d != nd && !(d.PackagePath != "" && d.PackagePath == nd.PackagePath && d.Marker == nd.Marker) {
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
	inferred := d == nil
	if inferred {
		// Identity is resolved from go/types after the target has been evaluated
		// into one saved value. A catch-all is not permission to skip validation.
		d = &Decl{}
	}
	covered := map[string]bool{}
	all := false
	for _, a := range arms {
		if all || (!inferred && len(covered) == len(d.Variants)) || a.variant != "" && covered[a.variant] {
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
	if inferred && !all {
		return "", 0, l.err(l.ts[i].start, "non-exhaustive match; an unguarded wildcard or whole-value binding is required, guards do not contribute coverage")
	}

	if !all && len(covered) != len(d.Variants) {
		return "", 0, l.err(l.ts[i].start, "non-exhaustive match; guards do not contribute coverage")
	}
	l.serial++
	id := strconv.Itoa(l.serial)
	saved := "__goxide_enum_saved_" + id
	matched := "__goxide_enum_matched_" + id
	var b strings.Builder
	if inferred {
		fmt.Fprintf(&b, "{ %s := (%s); __goxide_enum_infer(%s);", saved, target, saved)
	} else {
		fmt.Fprintf(&b, "{ var %s %s = (%s);", saved, d.Name, target)
		fmt.Fprintf(&b, "%s(%s);", d.Validator, saved)
	}
	// A plain variant chain needs no matched flag. Unlike a type switch, an
	// if/else chain does not intercept a user arm's unlabelled break/continue.
	plain := !inferred
	for _, a := range arms {
		plain = plain && a.variant != "" && a.guard == ""
	}
	if plain {
		for index, a := range arms {
			var v Variant
			for _, candidate := range d.Variants {
				if candidate.Name == a.variant {
					v = candidate
					break
				}
			}
			if index > 0 {
				b.WriteString(" else ")
			}
			tmp := "__goxide_enum_payload_" + id
			if d.Family == "Option" && v.Name == "None" {
				// Keep the constructor rather than spelling nil in caller scope.
				fmt.Fprintf(&b, "if __goxide_enum_none_%s := %s(); %s == __goxide_enum_none_%s {", id, v.Constructor, saved, id)
			} else {
				fmt.Fprintf(&b, "if %s, __goxide_enum_ok_%s := %s.(%s); __goxide_enum_ok_%s { _ = %s;", tmp, id, saved, v.CaseName, id, tmp)
			}
			if a.bind != "" && a.bind != "_" {
				fmt.Fprintf(&b, "%s := %s.Value; _ = %s;", a.bind, tmp, a.bind)
			}
			b.WriteString(a.body)
			b.WriteString("\n}")
		}
		b.WriteString("\n}")
		return b.String(), end, nil
	}
	fmt.Fprintf(&b, "%s := (0 != 0); _ = %s;\n", matched, matched)
	for _, a := range arms {
		fmt.Fprintf(&b, "if !%s {\n", matched)
		if a.variant != "" {
			var v Variant
			for _, x := range d.Variants {
				if x.Name == a.variant {
					v = x
				}
			}
			tmp := "__goxide_enum_payload_" + id
			if d.Family == "Option" && v.Name == "None" {
				fmt.Fprintf(&b, "__goxide_enum_none_%s := %s(); if %s == __goxide_enum_none_%s {\n", id, v.Constructor, saved, id)
			} else {
				fmt.Fprintf(&b, "if %s, __goxide_enum_ok_%s := %s.(%s); __goxide_enum_ok_%s { _ = %s;\n", tmp, id, saved, v.CaseName, id, tmp)
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
