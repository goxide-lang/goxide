package protocols

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
)

type edit struct {
	start, end int
	text       string
	kind       string
}

func (p *Package) lowerFile(file string, source []byte, mapping *sourcemap.Map) (Result, error) {
	unit, err := parse(file, source)
	if err != nil {
		return Result{}, err
	}
	r := Result{GenericFunctions: map[string]bool{}, GeneratedIdentifiers: map[string]bool{"__goxide_protocol_typeinfo": true}}
	var edits []edit
	var suffix, late strings.Builder
	for _, raw := range unit.definitions {
		d, ok := p.declarations[raw.definition.Name]
		if !ok {
			return r, fmt.Errorf("%s: protocol declaration appeared after package collection", file)
		}
		edits = append(edits, edit{raw.start, raw.end, blank(source[raw.start:raw.end]), "protocol declaration"})
		self := "__goxide_protocol_Self"
		r.GeneratedIdentifiers[self] = true
		fmt.Fprintf(&late, "\ntype %s[%s interface{}] interface {\n", d.OpsName, self)
		for _, m := range d.Members {
			fmt.Fprintf(&late, "%s%s\n", m.GoName, memberSignature(m, self))
		}
		fmt.Fprintf(&late, "%s()\n}\n", d.MarkerName)
		r.GeneratedSymbols = append(r.GeneratedSymbols, d.OpsName)
		r.GeneratedIdentifiers[d.OpsName] = true
		r.GeneratedIdentifiers[d.MarkerName] = true
	}
	for _, raw := range unit.impls {
		d, ok, err := p.lookupProtocol(file, raw.protocol)
		if err != nil {
			return r, err
		}
		if !ok {
			continue
		}
		var impl *Implementation
		for i := range p.Implementations {
			candidate := &p.Implementations[i]
			if candidate.File == file && candidate.Protocol == d.ID && candidate.Target.Name == raw.target {
				impl = candidate
				break
			}
		}
		if impl == nil {
			return r, fmt.Errorf("%s: implementation appeared after package collection: %s for %s", file, raw.protocol, raw.target)
		}
		if len(raw.members) == 0 {
			return r, fmt.Errorf("%s: implementation has no associated members", file)
		}
		edits = append(edits, edit{raw.start, raw.members[0].start, blank(source[raw.start:raw.members[0].start]), "protocol implementation"})
		for _, m := range raw.members {
			edits = append(edits, edit{m.nameStart, m.nameStart, "(_ " + impl.CarrierName + ") ", "protocol carrier receiver"})
			for _, e := range selfEdits(file, source[m.start:m.end], raw.target) {
				e.start += m.start
				e.end += m.start
				edits = append(edits, e)
			}
		}
		last := raw.members[len(raw.members)-1].end
		edits = append(edits, edit{last, raw.end, blank(source[last:raw.end]), "protocol implementation"})
		fmt.Fprintf(&suffix, "\nfunc (_ %s) %s() {}\n", impl.CarrierName, d.MarkerName)
		r.GeneratedIdentifiers[impl.CarrierName] = true
		r.GeneratedIdentifiers[d.MarkerName] = true
		first := true
		for i := range p.Implementations {
			earlier := &p.Implementations[i]
			if earlier == impl {
				break
			}
			if earlier.Target == impl.Target {
				first = false
				break
			}
		}
		if first {
			fmt.Fprintf(&suffix, "\ntype %s struct{}\nfunc (_ %s) %s(%s) %s { return %s{} }\n", impl.CarrierName, raw.target, AnchorName, raw.target, impl.CarrierName, impl.CarrierName)
			r.GeneratedSymbols = append(r.GeneratedSymbols, impl.CarrierName)
			r.AnchorTargets = append(r.AnchorTargets, raw.target)
		}
	}
	needImport := false
	for _, fn := range unit.functions {
		bounds := p.GenericBindings[file][fn.name]
		if len(bounds) == 0 {
			continue
		}
		var params, hidden []string
		for _, param := range fn.params {
			constraint := param.constraint
			for _, b := range bounds {
				if b.TypeParam == param.name {
					constraint = "__goxide_protocol_typeinfo.CarrierFor[" + param.name + ", " + b.CarrierParam + "]"
					route := b.Protocol.Route
					if route == "" {
						route = b.Protocol.OpsName
					}
					hidden = append(hidden, b.CarrierParam+" interface { ~struct{}; "+route+"["+param.name+"] }")
					r.GeneratedIdentifiers[b.CarrierParam] = true
					break
				}
			}
			params = append(params, param.name+" "+constraint)
		}
		params = append(params, hidden...)
		edits = append(edits, edit{fn.argsStart, fn.argsEnd, "[" + strings.Join(params, ", ") + "]", "protocol generic bound"})
		r.GenericFunctions[fn.name] = true
		needImport = true
	}
	if needImport {
		r.GeneratedIdentifiers["__goxide_protocol_typeinfo"] = true
		present := false
		if f, e := parser.ParseFile(token.NewFileSet(), file, source, parser.ImportsOnly); e == nil {
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				if path == TypeInfoPackage && imp.Name != nil && imp.Name.Name == "__goxide_protocol_typeinfo" {
					present = true
				}
			}
		}
		if !present {
			ts := tokens(file, source)
			for i, t := range ts {
				if t.kind == token.PACKAGE && i+1 < len(ts) {
					at := ts[i+1].end
					if i+2 < len(ts) && ts[i+2].kind == token.SEMICOLON && ts[i+2].text == ";" {
						at = ts[i+2].end
					}
					edits = append(edits, edit{at, at, "\nimport __goxide_protocol_typeinfo " + strconv.Quote(TypeInfoPackage) + "\n", "protocol import"})
					break
				}
			}
		}
	}
	r.GeneratedIdentifiers[AnchorName] = true
	if suffix.Len() > 0 {
		edits = append(edits, edit{len(source), len(source), suffix.String(), "protocol carrier declarations"})
	}
	r.Source, err = applyEdits(source, mapping, edits)
	if err != nil {
		return r, err
	}
	r.LateDeclarations = []byte(late.String())
	return r, nil
}

func blank(b []byte) string {
	out := append([]byte(nil), b...)
	for i, c := range out {
		if c != '\n' && c != '\r' {
			out[i] = ' '
		}
	}
	return string(out)
}

// A parser binding decides whether Self denotes the implicit type. Locally
// declared variables named Self have ast.Objects and are never substituted.
func selfEdits(file string, source []byte, target string) []edit {
	prefix := "package p;\n"
	fs := token.NewFileSet()
	f, _ := parser.ParseFile(fs, file, prefix+string(paths.GoView(source)), parser.AllErrors)
	if f == nil {
		return nil
	}
	var out []edit
	var visit func(ast.Node) bool
	visit = func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			ast.Inspect(s.X, visit)
			return false
		}
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			ast.Inspect(kv.Value, visit)
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != "Self" || id.Obj != nil {
			return true
		}
		off := fs.Position(id.Pos()).Offset - len(prefix)
		if off >= 0 && off+len("Self") <= len(source) {
			out = append(out, edit{off, off + len("Self"), target, "protocol Self type"})
		}
		return true
	}
	ast.Inspect(f, visit)
	return out
}

func memberSignature(m Member, replacement string) string {
	prefix := "func method"
	source := []byte(prefix + m.Params + " " + m.Results + "{}")
	out, _ := applyEdits(source, nil, selfEdits("signature", source, replacement))
	return strings.TrimSpace(string(out[len(prefix) : len(out)-2]))
}

func applyEdits(source []byte, mapping *sourcemap.Map, edits []edit) ([]byte, error) {
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out []byte
	next := &sourcemap.Map{}
	cursor := 0
	for _, e := range edits {
		if e.start < cursor || e.end < e.start || e.end > len(source) {
			return nil, fmt.Errorf("overlapping protocol source edits")
		}
		if mapping != nil {
			next.Segments = append(next.Segments, mapping.Copy(cursor, e.start, len(out))...)
		}
		out = append(out, source[cursor:e.start]...)
		begin := len(out)
		out = append(out, e.text...)
		if mapping != nil && e.end > e.start {
			if span := mapping.SpanFor(e.start, e.end); span != nil {
				segment := mapping.Lookup(e.start)
				next.Attach(begin, len(out), span, segment.Stack, e.kind)
			}
		}
		cursor = e.end
	}
	if mapping != nil {
		next.Segments = append(next.Segments, mapping.Copy(cursor, len(source), len(out))...)
	}
	out = append(out, source[cursor:]...)
	if mapping != nil {
		next.Source = out
		next.Stages = append(append([]string(nil), mapping.Stages...), "protocols.Lower")
		*mapping = *next
	}
	return out, nil
}
