// Package frontend implements the deliberately small executable macro prototype.
package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/deriveextras"
	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/macros"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Result struct {
	Go, SourceMap, Expanded []byte
	Enums                   []enums.Decl
}
type attrs struct {
	derive          map[string]bool
	rename          string
	message         string
	source, from    bool
	hasRename       bool
	hasMessage      bool
	skip, def, deny bool
}
type field struct {
	name, wire string
	typ        ast.Expr
	skip, def  bool
	line       int
}
type model struct {
	name         string
	fields       []field
	attrs        attrs
	line         int
	spec         *ast.TypeSpec
	source, from string
}
type generator struct {
	fs      *token.FileSet
	file    string
	models  map[string]model
	alias   string
	helpers strings.Builder
	next    int
}

var attribute = regexp.MustCompile(`^\s*#\[(.*)\]\s*$`)
var renameAttr = regexp.MustCompile(`^rename\s*=\s*("(?:[^"\\]|\\.)*")$`)

func Compile(filename string, source []byte) (Result, error) {
	expanded, expandErr := macros.ExpandWithMap(filename, source)
	if expandErr != nil {
		return Result{}, expandErr
	}
	source = expanded.Source
	// Reject user access to reserved generated names before adding enum helpers.
	reserveFS := token.NewFileSet()
	reserveFile := reserveFS.AddFile(filename, -1, len(source))
	var reserveScanner scanner.Scanner
	reserveScanner.Init(reserveFile, source, func(token.Position, string) {}, 0)
	for {
		pos, tok, lit := reserveScanner.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.IDENT && strings.HasPrefix(lit, "__hgo_") {
			return Result{}, fmt.Errorf("%s: reserved generated identifier %s", reserveFS.Position(pos), lit)
		}
	}
	enumResult, err := enums.Lower(filename, source)
	if err != nil {
		return Result{}, err
	}
	source = enumResult.Source
	fail := func(line int, msg string) (Result, error) {
		return Result{}, fmt.Errorf("%s:%d: %s", filename, line, msg)
	}
	lines := strings.Split(string(source), "\n")
	protected := protectedLines(source)
	pending := attrs{}
	byLine := map[int]attrs{}
	for i, l := range lines {
		if protected[i+1] {
			continue
		}
		if m := attribute.FindStringSubmatch(l); m != nil {
			body := m[1]
			if strings.HasPrefix(body, "derive(") && strings.HasSuffix(body, ")") {
				if pending.derive != nil {
					return fail(i+1, "duplicate derive")
				}
				pending.derive = map[string]bool{}
				for _, x := range strings.Split(body[7:len(body)-1], ",") {
					x = strings.TrimSpace(x)
					if x != "Serialize" && x != "Deserialize" && x != "Debug" && x != "Clone" && x != "Error" {
						return fail(i+1, "unsupported derive "+x)
					}
					if pending.derive[x] {
						return fail(i+1, "duplicate derive "+x)
					}
					pending.derive[x] = true
				}
			} else if strings.HasPrefix(body, "error(") && strings.HasSuffix(body, ")") {
				v, e := strconv.Unquote(body[6 : len(body)-1])
				if e != nil {
					return fail(i+1, "error requires a quoted message")
				}
				if pending.hasMessage {
					return fail(i+1, "duplicate error message")
				}
				pending.message = v
				pending.hasMessage = true
			} else if body == "source" {
				pending.source = true
			} else if body == "from" {
				pending.from = true
				pending.source = true
			} else if strings.HasPrefix(body, "serde(") && strings.HasSuffix(body, ")") {
				for _, x := range splitAttrs(body[6 : len(body)-1]) {
					x = strings.TrimSpace(x)
					switch x {
					case "skip":
						pending.skip = true
					case "default":
						pending.def = true
					case "deny_unknown_fields":
						pending.deny = true
					default:
						if m := renameAttr.FindStringSubmatch(x); m != nil {
							v, e := strconv.Unquote(m[1])
							if e != nil {
								return fail(i+1, e.Error())
							}
							if pending.hasRename {
								return fail(i+1, "duplicate rename")
							}
							pending.rename = v
							pending.hasRename = true
						} else {
							return fail(i+1, "unsupported serde attribute "+x)
						}
					}
				}
			} else {
				return fail(i+1, "unsupported attribute "+body)
			}
			lines[i] = ""
			continue
		}
		if strings.TrimSpace(l) == "" || strings.HasPrefix(strings.TrimSpace(l), "//") {
			continue
		}
		if pending.derive != nil || pending.hasRename || pending.skip || pending.def || pending.deny || pending.hasMessage || pending.source || pending.from {
			byLine[i+1] = pending
			pending = attrs{}
		}
		if strings.HasPrefix(strings.TrimSpace(l), "struct ") {
			lines[i] = strings.Replace(l, "struct ", "type ", 1)
			j := strings.Index(lines[i], "{")
			if j >= 0 {
				lines[i] = lines[i][:j] + "struct " + lines[i][j:]
			}
		}
	}
	if pending.derive != nil || pending.hasRename || pending.skip || pending.def || pending.deny || pending.hasMessage || pending.source || pending.from {
		return fail(len(lines), "attribute has no target")
	}
	fs := token.NewFileSet()
	f, e := parser.ParseFile(fs, filename, strings.Join(lines, "\n"), parser.ParseComments)
	if e != nil {
		return Result{}, e
	}
	g := generator{fs: fs, file: filename, models: map[string]model{}, alias: "__hgo_serde"}
	var collision string
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.HasPrefix(id.Name, "__hgo_") && !strings.HasPrefix(id.Name, "__hgo_enum_") {
			collision = id.Name
		}
		return true
	})
	if collision != "" {
		return fail(1, "reserved generated identifier "+collision)
	}
	var ordered []model
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		if gd.Lparen.IsValid() && byLine[fs.Position(gd.Pos()).Line].derive != nil {
			return fail(fs.Position(gd.Pos()).Line, "derive on a grouped type declaration is unsupported; annotate each type separately")
		}
		for _, sp := range gd.Specs {
			ts := sp.(*ast.TypeSpec)
			line := fs.Position(ts.Pos()).Line
			a := byLine[line]
			if a.derive == nil {
				a = byLine[fs.Position(gd.Pos()).Line]
			}
			if a.derive == nil {
				continue
			}
			delete(byLine, line)
			delete(byLine, fs.Position(gd.Pos()).Line)
			if a.hasRename || a.skip || a.def || a.source || a.from {
				return fail(line, "field attribute on type")
			}
			hasSerde := a.derive["Serialize"] || a.derive["Deserialize"]
			if a.deny && !hasSerde {
				return fail(line, "serde attributes require Serialize or Deserialize derive")
			}
			if ts.Assign.IsValid() {
				return fail(line, "derive on aliases is unsupported")
			}
			if ts.TypeParams != nil {
				return fail(line, "generic derive is not supported in this prototype")
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return fail(line, "derive requires struct; enum and aliases are not supported")
			}
			m := model{name: ts.Name.Name, attrs: a, line: line, spec: ts}
			seen := map[string]bool{}
			for _, af := range st.Fields.List {
				fl := fs.Position(af.Pos()).Line
				fa := byLine[fl]
				delete(byLine, fl)
				if (fa.hasRename || fa.skip || fa.def) && !hasSerde {
					return fail(fl, "serde attributes require Serialize or Deserialize derive")
				}
				if fa.derive != nil || fa.deny || fa.hasMessage {
					return fail(fl, "container attribute on field")
				}
				if len(af.Names) != 1 {
					return fail(fl, "derive requires one named field per declaration")
				}
				if af.Tag != nil {
					return fail(fl, "use serde attributes instead of Go field tags")
				}
				name := af.Names[0].Name
				if name == "_" {
					return fail(fl, "blank fields cannot be derived")
				}
				wire := name
				if fa.hasRename {
					wire = fa.rename
				}
				if seen[wire] {
					return fail(fl, "duplicate serialized field "+wire)
				}
				seen[wire] = true
				if fa.source {
					if m.source != "" {
						return fail(fl, "multiple source fields")
					}
					m.source = name
				}
				if fa.from {
					m.from = name
				}
				m.fields = append(m.fields, field{name, wire, af.Type, fa.skip, fa.def, fl})
			}
			g.models[m.name] = m
			ordered = append(ordered, m)
		}
	}
	if len(byLine) > 0 {
		for line := range byLine {
			return fail(line, "attribute on unsupported target")
		}
	}

	// Public derive helper names may not replace user declarations.
	declared := map[string]bool{}
	for _, d := range f.Decls {
		switch x := d.(type) {
		case *ast.FuncDecl:
			if x.Recv == nil {
				declared[x.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, sp := range x.Specs {
				switch v := sp.(type) {
				case *ast.TypeSpec:
					declared[v.Name.Name] = true
				case *ast.ValueSpec:
					for _, n := range v.Names {
						declared[n.Name] = true
					}
				}
			}
		}
	}
	for _, m := range ordered {
		for _, n := range []string{"bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "byte", "rune", "error"} {
			if declared[n] {
				return fail(m.line, "predeclared type shadowing is unsupported by derive: "+n)
			}
		}
		if m.attrs.derive["Error"] {
			for _, f := range m.fields {
				if f.name == "Error" || (f.name == "Unwrap" && m.source != "") {
					return fail(f.line, "derived method conflicts with field "+f.name)
				}
			}
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Recv == nil {
					continue
				}
				rt := fd.Recv.List[0].Type
				if p, ok := rt.(*ast.StarExpr); ok {
					rt = p.X
				}
				id, ok := rt.(*ast.Ident)
				if ok && id.Name == m.name && (fd.Name.Name == "Error" || (m.source != "" && fd.Name.Name == "Unwrap")) {
					return fail(m.line, "derived method conflicts with existing "+fd.Name.Name)
				}
			}
		}
	}
	for _, m := range ordered {
		for _, kind := range []string{"Serialize", "Deserialize", "Debug", "Clone"} {
			if m.attrs.derive[kind] && declared[kind+m.name] {
				return fail(m.line, "generated helper conflicts with declaration "+kind+m.name)
			}
		}
		if m.from != "" && declared["From"+m.name] {
			return fail(m.line, "generated helper conflicts with declaration From"+m.name)
		}
	}
	imports := map[string]string{}
	for _, m := range ordered {
		if m.attrs.derive["Serialize"] || m.attrs.derive["Deserialize"] {
			imports[g.alias] = "github.com/hxsf/goxide/serde"
			if e := g.emitModel(m); e != nil {
				return Result{}, e
			}
		}
		if (m.attrs.hasMessage || m.source != "") && !m.attrs.derive["Error"] {
			return fail(m.line, "error attributes require Error derive")
		}
		for _, kind := range []string{"Debug", "Clone", "Error"} {
			if !m.attrs.derive[kind] {
				continue
			}
			var code string
			var im map[string]string
			var e error
			if kind != "Error" {
				for _, f := range m.fields {
					if e := g.validateExtra(f.typ, kind); e != nil {
						return Result{}, e
					}
				}
			}
			if kind == "Error" {
				code, im, e = deriveextras.GenerateError(m.spec, m.attrs.message, m.source, m.from)
			} else {
				code, im, e = deriveextras.Generate(kind, m.spec)
			}
			if e != nil {
				return fail(m.line, e.Error())
			}
			g.helpers.WriteString("\n//line " + filename + ":" + strconv.Itoa(m.line) + "\n" + code)
			for k, v := range im {
				imports[k] = v
			}
		}
	}
	var original bytes.Buffer
	if e := format.Node(&original, fs, f); e != nil {
		return Result{}, e
	}
	// Insert a distinct import declaration after package; other imports remain intact.
	text := original.String()
	idx := strings.Index(text, "\n")
	var ib strings.Builder
	keys := []string{}
	for k := range imports {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&ib, "\nimport %s %q\n", k, imports[k])
	}
	text = text[:idx+1] + ib.String() + text[idx+1:] + g.helpers.String()
	out, e := format.Source([]byte(text))
	if e != nil {
		return Result{}, fmt.Errorf("internal expansion error: %w\n%s", e, text)
	}
	sm := []map[string]any{}
	for _, m := range ordered {
		for _, kind := range []string{"Serialize", "Deserialize", "Debug", "Clone"} {
			if m.attrs.derive[kind] {
				name := kind + m.name
				offset := bytes.Index(out, []byte("func "+name+"("))
				sm = append(sm, map[string]any{"source": filename, "line": m.line, "helper": name, "generatedByteOffset": offset})
			}
		}
		if m.attrs.derive["Error"] {
			sm = append(sm, map[string]any{"source": filename, "line": m.line, "helper": m.name + ".Error", "generatedByteOffset": bytes.Index(out, []byte("func (v "+m.name+") Error("))})
		}
	}
	mapping, _ := json.MarshalIndent(map[string]any{"derives": sm, "macroTokens": expanded.Origins, "note": "derive locations use expanded input; macro token origins map pre-format expansion offsets"}, "", "  ")
	return Result{Go: out, Expanded: out, SourceMap: append(mapping, '\n'), Enums: enumResult.Enums}, nil
}
func (g *generator) typeName(t ast.Expr) string {
	var b bytes.Buffer
	_ = format.Node(&b, g.fs, t)
	return b.String()
}
func (g *generator) err(t ast.Expr, msg string) error {
	return fmt.Errorf("%s:%d: %s", g.file, g.fs.Position(t.Pos()).Line, msg)
}
func (g *generator) emitModel(m model) error {
	var ser, des strings.Builder
	count := 0
	for _, f := range m.fields {
		if !f.skip {
			count++
		}
	}
	fmt.Fprintf(&ser, "\n//line %s:%d\nfunc Serialize%s(s %s.Serializer,v *%s) error { if v==nil {return %s.NewError(\"Type\",\"nil struct pointer\")};w,e:=s.Struct(%q,%d);if e!=nil{return e};", g.file, m.line, m.name, g.alias, m.name, g.alias, m.name, count)
	fmt.Fprintf(&des, "\n//line %s:%d\nfunc Deserialize%s(d %s.Deserializer)(%s,error){var v %s;", g.file, m.line, m.name, g.alias, m.name, m.name)
	for i, f := range m.fields {
		if !f.skip {
			fmt.Fprintf(&des, "var seen%d bool;", i)
		}
	}
	fmt.Fprintf(&des, "e:=d.Struct(%q,[]string{", m.name)
	for _, f := range m.fields {
		if !f.skip {
			fmt.Fprintf(&des, "%q,", f.wire)
		}
	}
	fmt.Fprintf(&des, "},%s.VisitorFuncs{OnMap:func(a %s.MapAccess)error{for{var k string;ok,e:=a.NextKey(func(d %s.Deserializer)error{var e error;k,e=%s.ReadString(d);return e});if e!=nil{return e};if !ok{break};switch k{", g.alias, g.alias, g.alias, g.alias)
	for i, f := range m.fields {
		if f.skip {
			fmt.Fprintf(&des, "case %q:if e:=a.NextValue(func(d %s.Deserializer)error{return d.Skip()});e!=nil{return e};", f.wire, g.alias)
			continue
		}
		s, r, e := g.codec(f.typ, m.attrs.derive["Serialize"], m.attrs.derive["Deserialize"])
		if e != nil {
			return e
		}
		fmt.Fprintf(&ser, "if e:=w.Field(%q,func(s %s.Serializer)error{return %s(s,v.%s)});e!=nil{return e};", f.wire, g.alias, s, f.name)
		fmt.Fprintf(&des, "case %q:if seen%d{return %s.NewError(\"DuplicateField\",\"duplicate field \"+k)};seen%d=true;if e:=a.NextValue(func(d %s.Deserializer)error{var e error;v.%s,e=%s(d);return e});e!=nil{return e};", f.wire, i, g.alias, i, g.alias, f.name, r)
	}
	if m.attrs.deny {
		fmt.Fprintf(&des, "default:return %s.AtField(%s.NewError(\"UnknownField\",\"unknown field\"),k);", g.alias, g.alias)
	} else {
		des.WriteString("default:if e:=a.NextValue(func(d " + g.alias + ".Deserializer)error{return d.Skip()});e!=nil{return e};")
	}
	des.WriteString("}};")
	for i, f := range m.fields {
		if !f.skip && !f.def {
			fmt.Fprintf(&des, "if !seen%d{return %s.AtField(%s.NewError(\"MissingField\",\"required field missing\"),%q)};", i, g.alias, g.alias, f.wire)
		}
	}
	des.WriteString("return nil}});return v,e}\n")
	ser.WriteString("return w.End()}\n")
	if m.attrs.derive["Serialize"] {
		g.helpers.WriteString(ser.String())
	}
	if m.attrs.derive["Deserialize"] {
		g.helpers.WriteString(des.String())
	}
	return nil
}
func (g *generator) codec(t ast.Expr, wantS, wantR bool) (string, string, error) {
	g.next++
	s := fmt.Sprintf("__hgo_write%d", g.next)
	r := fmt.Sprintf("__hgo_read%d", g.next)
	typ := g.typeName(t)
	a := g.alias
	var w, b string
	switch t := t.(type) {
	case *ast.Ident:
		switch t.Name {
		case "bool", "string":
			method := map[string]string{"bool": "Bool", "string": "String"}[t.Name]
			w = "return s." + method + "(v)"
			b = "return " + a + ".Read" + method + "(d)"
		case "int", "int8", "int16", "int32", "int64":
			w = "return s.Int64(int64(v))"
			b = "x,e:=" + a + ".ReadInt64(d);if e!=nil{return v,e};v=" + typ + "(x);if int64(v)!=x{return v," + a + ".NewError(\"Range\",\"integer overflow\")};return v,nil"
		case "uint", "uint8", "uint16", "uint32", "uint64":
			w = "return s.Uint64(uint64(v))"
			b = "x,e:=" + a + ".ReadUint64(d);if e!=nil{return v,e};v=" + typ + "(x);if uint64(v)!=x{return v," + a + ".NewError(\"Range\",\"integer overflow\")};return v,nil"
		case "byte":
			return g.codec(ast.NewIdent("uint8"), wantS, wantR)
		case "rune":
			return g.codec(ast.NewIdent("int32"), wantS, wantR)
		case "float32", "float64":
			w = "return s.Float64(float64(v))"
			b = "x,e:=" + a + ".ReadFloat64(d);if e!=nil{return v,e};"
			if t.Name == "float32" {
				b += "if x>3.4028234663852886e38||x< -3.4028234663852886e38||(x!=0&&float32(x)==0){return v," + a + ".NewError(\"Range\",\"float32 overflow\")};"
			}
			b += "return " + typ + "(x),nil"
		default:
			m, ok := g.models[t.Name]
			if !ok {
				return "", "", g.err(t, "unsupported field type "+typ)
			}
			if wantS && !m.attrs.derive["Serialize"] || wantR && !m.attrs.derive["Deserialize"] {
				return "", "", g.err(t, "nested type lacks required derive: "+typ)
			}
			w = "return Serialize" + typ + "(s,&v)"
			b = "return Deserialize" + typ + "(d)"
		}
	case *ast.StarExpr:
		cs, cr, e := g.codec(t.X, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "if v==nil{return s.None()};return s.Some(func(s " + a + ".Serializer)error{return " + cs + "(s,*v)})"
		b = "e=d.Option(" + a + ".VisitorFuncs{OnNone:func()error{return nil},OnSome:func(d " + a + ".Deserializer)error{x,e:=" + cr + "(d);if e!=nil{return e};v=&x;return nil}});return v,e"
	case *ast.ArrayType:
		cs, cr, e := g.codec(t.Elt, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "q,e:=s.Seq(len(v));if e!=nil{return e};for i:=range v{if e:=q.Element(func(s " + a + ".Serializer)error{return " + cs + "(s,v[i])});e!=nil{return e}};return q.End()"
		init := ""
		assign := "v=append(v,x)"
		finish := ""
		if t.Len != nil {
			init = "if i>=len(v){return " + a + ".NewError(\"Type\",\"too many array elements\")};"
			assign = "v[i]=x"
			finish = "if i!=len(v){return " + a + ".NewError(\"Type\",\"wrong array length\")};"
		} else {
			b = "v=make(" + typ + ",0);"
		}
		b += "e=d.Seq(" + a + ".VisitorFuncs{OnSeq:func(q " + a + ".SeqAccess)error{i:=0;for{ok,e:=q.NextElement(func(d " + a + ".Deserializer)error{" + init + "x,e:=" + cr + "(d);if e!=nil{return e};" + assign + ";return nil});if e!=nil{return e};if !ok{break};i++};" + finish + "return nil}});return v,e"
	case *ast.MapType:
		k, ok := t.Key.(*ast.Ident)
		if !ok || k.Name != "string" {
			return "", "", g.err(t, "only map[string]T is supported")
		}
		cs, cr, e := g.codec(t.Value, wantS, wantR)
		if e != nil {
			return "", "", e
		}
		w = "q,e:=s.Map(len(v));if e!=nil{return e};for k,x:=range v{if e:=q.Entry(func(s " + a + ".Serializer)error{return s.String(k)},func(s " + a + ".Serializer)error{return " + cs + "(s,x)});e!=nil{return e}};return q.End()"
		b = "v=make(" + typ + ");e=d.Map(" + a + ".VisitorFuncs{OnMap:func(q " + a + ".MapAccess)error{for{var k string;ok,e:=q.NextKey(func(d " + a + ".Deserializer)error{var e error;k,e=" + a + ".ReadString(d);return e});if e!=nil{return e};if !ok{break};if _,ok:=v[k];ok{return " + a + ".AtField(" + a + ".NewError(\"DuplicateField\",\"duplicate map key\"),k)};if e:=q.NextValue(func(d " + a + ".Deserializer)error{x,e:=" + cr + "(d);if e!=nil{return e};v[k]=x;return nil});e!=nil{return e}};return nil}});return v,e"
	default:
		return "", "", g.err(t, "unsupported field type "+typ)
	}
	if wantS {
		fmt.Fprintf(&g.helpers, "\nfunc %s(s %s.Serializer,v %s)error{%s}\n", s, a, typ, w)
	}
	if wantR {
		fmt.Fprintf(&g.helpers, "\nfunc %s(d %s.Deserializer)(v %s,e error){%s}\n", r, a, typ, b)
	}
	return s, r, nil
}

// splitAttrs respects commas inside quoted attribute strings.
func splitAttrs(s string) []string {
	var out []string
	start := 0
	quoted, escape := false, false
	for i, c := range s {
		if escape {
			escape = false
			continue
		}
		if quoted && c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			quoted = !quoted
		}
		if c == ',' && !quoted {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
func protectedLines(source []byte) map[int]bool {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var s scanner.Scanner
	s.Init(f, source, func(token.Position, string) {}, scanner.ScanComments)
	out := map[int]bool{}
	for {
		p, t, l := s.Scan()
		if t == token.EOF {
			break
		}
		if (t == token.STRING || t == token.COMMENT) && strings.Contains(l, "\n") {
			first := f.Position(p).Line
			for n := 0; n <= strings.Count(l, "\n"); n++ {
				out[first+n] = true
			}
		}
	}
	return out
}
func (g *generator) validateExtra(t ast.Expr, kind string) error {
	switch x := t.(type) {
	case *ast.Ident:
		switch x.Name {
		case "bool", "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64", "byte", "rune":
			return nil
		}
		m, ok := g.models[x.Name]
		if !ok || !m.attrs.derive[kind] {
			return g.err(t, "nested type requires "+kind+" derive: "+x.Name)
		}
	case *ast.ArrayType:
		return g.validateExtra(x.Elt, kind)
	case *ast.MapType:
		if e := g.validateExtra(x.Key, kind); e != nil {
			return e
		}
		return g.validateExtra(x.Value, kind)
	}
	return nil
}
