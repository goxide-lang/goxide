package frontend

import (
	"bytes"
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"

	"github.com/hxsf/goxide/internal/equality"
)

// This registry is source evidence. Merely declaring EqualsT never registers T.
type manualEquality struct {
	partial, eq *manualEqualitySite
}
type manualEqualitySite struct{ position token.Position }

// Only wrappers/names are rewritten: method body bytes and newlines are retained.
func lowerManualEquality(filename string, source []byte, provenance ...*sourcemap.Map) ([]byte, map[string]manualEquality, error) {
	type tok struct {
		kind       token.Token
		text       string
		start, end int
		pos        token.Position
	}
	fs := token.NewFileSet()
	file := fs.AddFile(filename, -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, source, func(token.Position, string) {}, 0)
	var ts []tok
	for {
		p, k, v := scan.Scan()
		if k == token.EOF {
			break
		}
		if v == "" {
			v = k.String()
		}
		off := file.Offset(p)
		end := off + len(v)
		if k == token.SEMICOLON && v == "\n" {
			end = off
		}
		if end > len(source) {
			end = len(source)
		}
		ts = append(ts, tok{k, v, off, end, fs.Position(p)})
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	blank := func(start, end int) string {
		b := append([]byte(nil), source[start:end]...)
		for i, c := range b {
			if c != '\n' && c != '\r' {
				b[i] = ' '
			}
		}
		return string(b)
	}
	result := map[string]manualEquality{}
	depth := 0
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if depth == 0 && t.text == "impl" && i+1 < len(ts) && (ts[i+1].text == "PartialEq" || ts[i+1].text == "Eq") {
			protocol := ts[i+1].text
			fail := func(msg string) ([]byte, map[string]manualEquality, error) {
				return nil, nil, fmt.Errorf("%s: manual %s: %s", t.pos, protocol, msg)
			}
			if i+4 >= len(ts) || ts[i+2].text != "for" || ts[i+3].kind != token.IDENT || ts[i+3].text == "_" || ts[i+4].kind != token.LBRACE {
				return fail("requires impl " + protocol + " for a local concrete defined type")
			}
			target := ts[i+3].text
			entry := result[target]
			site := &manualEqualitySite{position: t.pos}
			j := i + 5
			for j < len(ts) && ts[j].kind == token.SEMICOLON {
				j++
			}
			if protocol == "Eq" {
				if entry.eq != nil {
					return fail("duplicate implementation for " + target)
				}
				if j >= len(ts) || ts[j].kind != token.RBRACE {
					return fail("requires an empty marker body")
				}
				end := ts[j].end
				if j+1 < len(ts) && ts[j+1].kind == token.SEMICOLON && ts[j+1].text == ";" {
					j++
					end = ts[j].end
				}
				edits = append(edits, edit{t.start, end, blank(t.start, end)})
				entry.eq = site
				result[target] = entry
				i = j
				continue
			}
			if entry.partial != nil {
				return fail("duplicate implementation for " + target)
			}
			if j+2 >= len(ts) || ts[j].kind != token.FUNC || ts[j+1].text != "equals" || ts[j+2].kind != token.LPAREN {
				return fail("requires exactly func equals(self " + target + ", other " + target + ") bool; no static or generic method")
			}
			body := j + 2
			for body < len(ts) && ts[body].kind != token.LBRACE {
				body++
			}
			if body >= len(ts) {
				return fail("missing equals body")
			}
			signature := "package p\n" + string(source[ts[j].start:ts[body].start]) + "{}"
			parsed, e := parser.ParseFile(token.NewFileSet(), "<equality signature>", signature, 0)
			if e != nil {
				return fail("invalid equals signature: " + e.Error())
			}
			if len(parsed.Decls) != 1 {
				return fail("requires exactly one equals method")
			}
			fn, ok := parsed.Decls[0].(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Type.TypeParams != nil {
				return fail("equals must be a non-generic instance method")
			}
			var names []string
			for _, f := range fn.Type.Params.List {
				typ, ok := f.Type.(*ast.Ident)
				if !ok || typ.Name != target || len(f.Names) == 0 {
					return fail("equals requires two named parameters of exact value type " + target)
				}
				for _, n := range f.Names {
					names = append(names, n.Name)
				}
			}
			if len(names) != 2 || names[0] != "self" || names[1] == "self" {
				return fail("equals requires self first and one other named value parameter")
			}
			if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 || len(fn.Type.Results.List[0].Names) > 1 {
				return fail("equals must return one bool")
			}
			ret, ok := fn.Type.Results.List[0].Type.(*ast.Ident)
			if !ok || ret.Name != "bool" {
				return fail("equals must return one bool")
			}
			end := body + 1
			level := 1
			for end < len(ts) && level > 0 {
				switch ts[end].kind {
				case token.LBRACE:
					level++
				case token.RBRACE:
					level--
				}
				end++
			}
			if level != 0 {
				return fail("unterminated equals body")
			}
			bodyEnd := end - 1
			for end < len(ts) && ts[end].kind == token.SEMICOLON {
				end++
			}
			if end >= len(ts) || ts[end].kind != token.RBRACE {
				return fail("requires exactly one equals member")
			}
			edits = append(edits, edit{t.start, ts[j].start, blank(t.start, ts[j].start)}, edit{ts[j+1].start, ts[j+1].end, "Equals" + target}, edit{ts[bodyEnd].end, ts[end].end, blank(ts[bodyEnd].end, ts[end].end)})
			entry.partial = site
			result[target] = entry
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
	var mapped *sourcemap.Map
	if len(provenance) > 0 && provenance[0] != nil {
		mapped = &sourcemap.Map{Stages: append(append([]string{}, provenance[0].Stages...), "manualEquality.Lower")}
	}
	cursor := 0
	for _, e := range edits {
		if mapped != nil {
			mapped.Segments = append(mapped.Segments, provenance[0].Copy(cursor, e.start, out.Len())...)
		}
		out.Write(source[cursor:e.start])
		if mapped != nil && e.start < e.end && len(e.text) > 0 && e.text[0] != ' ' && e.text[0] != '\n' && e.text[0] != '\r' {
			origin := provenance[0].SpanFor(e.start, e.end)
			var stack []sourcemap.Expansion
			if seg := provenance[0].Lookup(e.start); seg != nil {
				stack = seg.Stack
			}
			mapped.Attach(out.Len(), out.Len()+len(e.text), origin, stack, "manual-equality-helper")
		}
		out.WriteString(e.text)
		cursor = e.end
	}
	if mapped != nil {
		mapped.Segments = append(mapped.Segments, provenance[0].Copy(cursor, len(source), out.Len())...)
	}
	out.Write(source[cursor:])
	if mapped != nil {
		mapped.Source = out.Bytes()
		*provenance[0] = *mapped
	}
	return out.Bytes(), result, nil
}

func mergeManualEquality(dst map[string]manualEquality, source map[string]manualEquality) error {
	for _, name := range sortedManualEquality(source) {
		in := source[name]
		old := dst[name]
		for _, p := range []struct {
			name        string
			prior, next *manualEqualitySite
		}{{"PartialEq", old.partial, in.partial}, {"Eq", old.eq, in.eq}} {
			if p.prior != nil && p.next != nil {
				return fmt.Errorf("%s: duplicate %s implementation for %s (also %s)", p.next.position, p.name, name, p.prior.position)
			}
		}
		if in.partial != nil {
			old.partial = in.partial
		}
		if in.eq != nil {
			old.eq = in.eq
		}
		dst[name] = old
	}
	return nil
}

// Include only the owning file's entry; another file may contain the Eq marker.
func applyManualEquality(filename string, derives map[string]equality.Derive, manual map[string]manualEquality, specs map[string]*ast.TypeSpec, models map[string]model) error {
	for _, name := range sortedManualEquality(manual) {
		entry := manual[name]
		site := entry.partial
		if site == nil {
			site = entry.eq
		}
		spec := specs[name]
		if spec == nil || spec.Assign.IsValid() || spec.TypeParams != nil {
			return fmt.Errorf("%s: manual equality requires a local defined non-generic type: %s", site.position, name)
		}
		model := models[name]
		derivePartial, deriveEq := model.attrs.derive["PartialEq"], model.attrs.derive["Eq"]
		if entry.partial != nil && derivePartial {
			return fmt.Errorf("%s: duplicate PartialEq implementation (manual and derive): %s", entry.partial.position, name)
		}
		if entry.eq != nil && deriveEq {
			return fmt.Errorf("%s: duplicate Eq implementation (manual and derive): %s", entry.eq.position, name)
		}
		if entry.partial != nil && deriveEq {
			return fmt.Errorf("%s: derive Eq cannot qualify manual PartialEq; use impl Eq for %s {}", entry.partial.position, name)
		}
		if entry.eq != nil && entry.partial == nil && !derivePartial {
			return fmt.Errorf("%s: manual Eq requires explicit PartialEq for %s", entry.eq.position, name)
		}
		d, local := derives[name]
		if entry.partial != nil {
			if entry.partial.position.Filename != filename {
				delete(derives, name)
				continue
			}
			d.PartialEq = true
			d.ManualPartialEq = true
			local = true
		}
		if local {
			if entry.eq != nil {
				d.Eq = true
				d.ManualEq = true
			}
			derives[name] = d
		}
	}
	return nil
}

func sortedManualEquality(entries map[string]manualEquality) []string {
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
