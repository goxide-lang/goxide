package protocols

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
)

type lexeme struct {
	kind       token.Token
	text       string
	start, end int
	position   token.Position
}
type declaration struct {
	start, end int
	definition Definition
}
type implDecl struct {
	start, end       int
	protocol, target string
	members          []method
	memberError      error
}
type method struct {
	Member
	start, end, nameStart, nameEnd, bodyStart int
}
type function struct {
	name                           string
	start, end, argsStart, argsEnd int
	params                         []parameter
}
type parameter struct{ name, constraint string }
type parsed struct {
	packageName string
	definitions []declaration
	impls       []implDecl
	functions   []function
	targets     []target
}

func tokens(file string, source []byte) []lexeme {
	fs := token.NewFileSet()
	f := fs.AddFile(file, -1, len(source))
	var s scanner.Scanner
	s.Init(f, source, func(token.Position, string) {}, 0)
	var out []lexeme
	for {
		pos, k, text := s.Scan()
		if k == token.EOF {
			break
		}
		if text == "" {
			text = k.String()
		}
		begin := f.Offset(pos)
		end := begin + len(text)
		if k == token.SEMICOLON && text == "\n" {
			end = begin
		}
		out = append(out, lexeme{k, text, begin, end, fs.Position(pos)})
	}
	return out
}
func closeTokens(ts []lexeme, start int) (int, error) {
	end := map[token.Token]token.Token{token.LPAREN: token.RPAREN, token.LBRACK: token.RBRACK, token.LBRACE: token.RBRACE}[ts[start].kind]
	if end == token.ILLEGAL {
		return 0, fmt.Errorf("%s: expected opening delimiter", ts[start].position)
	}
	for i := start + 1; i < len(ts); i++ {
		if ts[i].kind == end {
			return i, nil
		}
		if ts[i].kind == token.LPAREN || ts[i].kind == token.LBRACK || ts[i].kind == token.LBRACE {
			n, e := closeTokens(ts, i)
			if e != nil {
				return 0, e
			}
			i = n
		}
	}
	return 0, fmt.Errorf("%s: unterminated protocol declaration", ts[start].position)
}
func spelling(ts []lexeme) string {
	var b strings.Builder
	for _, t := range ts {
		b.WriteString(t.text)
	}
	return strings.ReplaceAll(b.String(), ".", "::")
}

func parse(file string, source []byte) (parsed, error) {
	ts := tokens(file, source)
	var out parsed
	for i := 0; i < len(ts); i++ {
		t := ts[i]
		if t.kind == token.PACKAGE && i+1 < len(ts) {
			out.packageName = ts[i+1].text
			continue
		}
		start := i
		public := false
		declarationStart := i == 0 || ts[i-1].kind == token.SEMICOLON || ts[i-1].kind == token.RBRACE
		if declarationStart && t.text == "pub" && i+1 < len(ts) {
			public = true
			i++
			t = ts[i]
		}
		switch {
		case declarationStart && t.text == "trait":
			if i+2 >= len(ts) || ts[i+1].kind != token.IDENT || ts[i+2].kind != token.LBRACE {
				return out, fmt.Errorf("%s: expected trait Name { func Member(...) ... }", t.position)
			}
			end, e := closeTokens(ts, i+2)
			if e != nil {
				return out, e
			}
			members, e := parseMembers(file, source, ts, i+3, end, false)
			if e != nil {
				return out, e
			}
			d := Definition{ID: ID{Name: ts[i+1].text}, Public: public, File: file}
			for _, m := range members {
				d.Members = append(d.Members, m.Member)
			}
			if len(d.Members) == 0 {
				return out, fmt.Errorf("%s: associated protocol needs at least one member", t.position)
			}
			out.definitions = append(out.definitions, declaration{ts[start].start, ts[end].end, d})
			i = end
		case declarationStart && t.text == "impl":
			j := i + 1
			for j < len(ts) && ts[j].text != "for" && ts[j].kind != token.LBRACE && ts[j].kind != token.SEMICOLON {
				j++
			}
			if j >= len(ts) || ts[j].text != "for" {
				continue
			}
			k := j + 1
			for k < len(ts) && ts[k].kind != token.LBRACE && ts[k].kind != token.SEMICOLON {
				k++
			}
			if k >= len(ts) || ts[k].kind != token.LBRACE {
				return out, fmt.Errorf("%s: expected impl Protocol for Type { ... }", t.position)
			}
			end, e := closeTokens(ts, k)
			if e != nil {
				return out, e
			}
			proto := spelling(ts[i+1 : j])
			members, e := parseMembers(file, source, ts, k+1, end, true)
			out.impls = append(out.impls, implDecl{ts[start].start, ts[end].end, proto, spelling(ts[j+1 : k]), members, e})
			i = end
		case t.kind == token.FUNC:
			if i+2 < len(ts) && ts[i+1].kind == token.IDENT && ts[i+2].kind == token.LBRACK {
				end, e := closeTokens(ts, i+2)
				if e != nil {
					return out, e
				}
				fn := function{name: ts[i+1].text, start: t.start, argsStart: ts[i+2].start, argsEnd: ts[end].end}
				for j := i + 3; j < end; {
					if ts[j].kind != token.IDENT {
						return out, fmt.Errorf("%s: expected generic type parameter", ts[j].position)
					}
					k := j + 1
					for k < end && ts[k].kind != token.COMMA {
						if ts[k].kind == token.LBRACK || ts[k].kind == token.LPAREN || ts[k].kind == token.LBRACE {
							n, e := closeTokens(ts, k)
							if e != nil {
								return out, e
							}
							k = n
						}
						k++
					}
					fn.params = append(fn.params, parameter{ts[j].text, spelling(ts[j+1 : k])})
					j = k + 1
				}
				out.functions = append(out.functions, fn)
			}
		case t.kind == token.TYPE && i+2 < len(ts) && ts[i+1].kind == token.IDENT:
			kind := ts[i+2].text
			alias := kind == "="
			if alias && i+3 < len(ts) {
				kind = ts[i+3].text
			}
			if ts[i+2].kind == token.IDENT {
				kind = "named"
			}
			out.targets = append(out.targets, target{ts[i+1].text, file, alias, kind})
		case t.kind == token.TYPE && i+1 < len(ts) && ts[i+1].kind == token.LPAREN:
			end, e := closeTokens(ts, i+1)
			if e != nil {
				return out, e
			}
			view := "package p;" + string(paths.GoView(source[t.start:ts[end].end]))
			if f, e := parser.ParseFile(token.NewFileSet(), file, view, 0); e == nil {
				for _, raw := range f.Decls[0].(*ast.GenDecl).Specs {
					spec := raw.(*ast.TypeSpec)
					kind := "other"
					if _, ok := spec.Type.(*ast.StructType); ok {
						kind = "struct"
					}
					if spec.TypeParams != nil {
						kind = "generic"
					}
					out.targets = append(out.targets, target{spec.Name.Name, file, spec.Assign.IsValid(), kind})
				}
			}
			i = end
		case (t.text == "enum" || t.kind == token.STRUCT) && i+2 < len(ts) && ts[i+1].kind == token.IDENT:
			out.targets = append(out.targets, target{ts[i+1].text, file, false, t.text})
		}
		// Skip arbitrary package declaration bodies, including native Go
		// interface bodies; no method there acquires protocol meaning.
		if i < len(ts) && (ts[i].kind == token.LBRACE || ts[i].kind == token.LPAREN || ts[i].kind == token.LBRACK) {
			end, e := closeTokens(ts, i)
			if e != nil {
				return out, e
			}
			i = end
		}
	}
	return out, nil
}

func parseMembers(file string, source []byte, ts []lexeme, start, end int, bodies bool) ([]method, error) {
	var out []method
	seen := map[string]bool{}
	for i := start; i < end; {
		if ts[i].kind == token.SEMICOLON {
			i++
			continue
		}
		if ts[i].kind != token.FUNC || i+2 >= end || ts[i+1].kind != token.IDENT || ts[i+2].kind != token.LPAREN {
			return nil, fmt.Errorf("%s: associated protocol members require func Name(...); generic or instance members are not in this slice", ts[i].position)
		}
		name := ts[i+1].text
		if seen[name] {
			return nil, fmt.Errorf("%s: duplicate associated member %s", ts[i+1].position, name)
		}
		seen[name] = true
		paramsEnd, e := closeTokens(ts, i+2)
		if e != nil {
			return nil, e
		}
		if i+3 < paramsEnd && ts[i+3].text == "self" {
			return nil, fmt.Errorf("%s: instance self members are not associated functions", ts[i+3].position)
		}
		j := paramsEnd + 1
		for j < end && ts[j].kind != token.SEMICOLON && ts[j].kind != token.LBRACE {
			if (ts[j].kind == token.STRUCT || ts[j].kind == token.INTERFACE) && j+1 < end && ts[j+1].kind == token.LBRACE {
				n, e := closeTokens(ts, j+1)
				if e != nil {
					return nil, e
				}
				j = n + 1
				continue
			}
			if ts[j].kind == token.LPAREN || ts[j].kind == token.LBRACK {
				n, e := closeTokens(ts, j)
				if e != nil {
					return nil, e
				}
				j = n
			}
			j++
		}
		bodyStart := -1
		memberEnd := ts[paramsEnd].end
		resultEnd := memberEnd
		if j > paramsEnd+1 {
			resultEnd = ts[j-1].end
		}
		if bodies {
			if j >= end || ts[j].kind != token.LBRACE {
				return nil, fmt.Errorf("%s: implementation member requires a body", ts[i].position)
			}
			bodyStart = ts[j].start
			last, e := closeTokens(ts, j)
			if e != nil {
				return nil, e
			}
			memberEnd = ts[last].end
			j = last + 1
		} else {
			if j < end && ts[j].kind == token.LBRACE {
				return nil, fmt.Errorf("%s: protocol declaration cannot contain a body", ts[j].position)
			}
			memberEnd = resultEnd
		}
		params := string(source[ts[i+2].start:ts[paramsEnd].end])
		results := strings.TrimSpace(string(source[ts[paramsEnd].end:resultEnd]))
		view := "package p;func " + name + string(paths.GoView([]byte(params+" "+results))) + " {}"
		f, e := parser.ParseFile(token.NewFileSet(), file, view, 0)
		if e != nil {
			return nil, fmt.Errorf("%s: invalid associated signature: %v", ts[i].position, e)
		}
		fn := f.Decls[0].(*ast.FuncDecl)
		out = append(out, method{Member: Member{Name: name, GoName: name, Params: params, Results: results, Signature: fn.Type, Position: ts[i].position}, start: ts[i].start, end: memberEnd, nameStart: ts[i+1].start, nameEnd: ts[i+1].end, bodyStart: bodyStart})
		i = j
	}
	return out, nil
}

func collectPackage(owner string, sources map[string][]byte, options Options) (*Package, error) {
	p := &Package{Owner: owner, options: options, declarations: map[string]Definition{}, targets: map[string]target{}, GenericBindings: map[string]map[string][]Bound{}}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	files := map[string]parsed{}
	for _, file := range names {
		var src parsed
		var e error
		if !options.Single && strings.HasSuffix(file, ".go") {
			src, e = parseNative(file, sources[file])
		} else {
			src, e = parse(file, sources[file])
		}
		if e != nil {
			return nil, e
		}
		files[file] = src
		if p.Name == "" {
			p.Name = src.packageName
		}
		for _, t := range src.targets {
			p.targets[t.Name] = t
		}
		for _, pd := range src.definitions {
			d := pd.definition
			d.Owner = owner
			d.OpsName = generatedName(d.ID, "Ops", d.Public)
			d.MarkerName = generatedName(d.ID, "Evidence", d.Public)
			if _, ok := p.declarations[d.Name]; ok {
				return nil, fmt.Errorf("%s: duplicate protocol %s", file, d.Name)
			}
			p.declarations[d.Name] = d
			p.Definitions = append(p.Definitions, d)
		}
	}
	implSeen := map[string]bool{}
	for name, d := range p.declarations {
		if t, ok := p.targets[name]; ok {
			return nil, fmt.Errorf("%s: protocol %s conflicts with type declaration in %s", d.File, name, t.File)
		}
	}
	for _, file := range names {
		src := files[file]
		for _, raw := range src.impls {
			d, ok, e := p.lookupProtocol(file, raw.protocol)
			if e != nil {
				return nil, e
			}
			if !ok {
				// Not owned by this registry; existing implementation passes
				// retain their original source without a name-based dispatch.
				continue
			}
			if raw.memberError != nil {
				return nil, raw.memberError
			}
			t, ok := p.targets[raw.target]
			if !ok || t.Alias || (t.Kind != "struct" && t.Kind != "enum" && t.Kind != "named") {
				return nil, fmt.Errorf("%s: protocol impl target %s must be a local defined struct or enum", file, raw.target)
			}
			key := d.ID.String() + " for " + raw.target
			if implSeen[key] {
				return nil, fmt.Errorf("%s: duplicate implementation %s", file, key)
			}
			implSeen[key] = true
			impl := Implementation{Protocol: d.ID, Target: ID{owner, raw.target}, CarrierName: generatedName(ID{owner, raw.target}, "Carrier", ast.IsExported(raw.target)), File: file}
			for _, want := range d.Members {
				found := false
				for _, actual := range raw.members {
					if want.Name == actual.Name {
						impl.Members = append(impl.Members, actual.Member)
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("%s: implementation %s is missing member %s", file, key, want.Name)
				}
			}
			if len(impl.Members) != len(raw.members) {
				return nil, fmt.Errorf("%s: implementation %s has an unknown member", file, key)
			}
			p.Implementations = append(p.Implementations, impl)
		}
		for _, fn := range src.functions {
			gf := GenericFunction{Name: fn.name}
			var bounds []Bound
			for _, param := range fn.params {
				gf.SourceParams = append(gf.SourceParams, param.name)
				d, ok, e := p.lookupProtocol(file, param.constraint)
				if e != nil {
					return nil, e
				}
				if !ok {
					continue
				}
				hidden := fmt.Sprintf("__goxide_protocol_dictionary_%s_%s", fn.name, param.name)
				b := Bound{Function: fn.name, TypeParam: param.name, CarrierParam: hidden, Protocol: d, File: file}
				bounds = append(bounds, b)
				gf.HiddenParams = append(gf.HiddenParams, hidden)
			}
			if len(bounds) > 0 {
				if p.GenericBindings[file] == nil {
					p.GenericBindings[file] = map[string][]Bound{}
				}
				p.GenericBindings[file][fn.name] = bounds
				gf.Bounds = bounds
				p.Functions = append(p.Functions, gf)
			}
		}
	}
	p.promoteEvidence()
	return p, nil
}

func parseNative(file string, source []byte) (parsed, error) {
	f, e := parser.ParseFile(token.NewFileSet(), file, source, 0)
	if e != nil {
		return parsed{}, e
	}
	out := parsed{packageName: f.Name.Name}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, raw := range gd.Specs {
			spec := raw.(*ast.TypeSpec)
			kind := "other"
			switch spec.Type.(type) {
			case *ast.StructType:
				kind = "struct"
			case *ast.Ident, *ast.SelectorExpr:
				kind = "named"
			}
			if spec.TypeParams != nil {
				kind = "generic"
			}
			out.targets = append(out.targets, target{spec.Name.Name, file, spec.Assign.IsValid(), kind})
		}
	}
	return out, nil
}

// Public Go APIs may mention a source-private protocol's generated constraint.
// Export only the compiler evidence needed for that API; source visibility is
// retained independently in Definition.Public.
func (p *Package) promoteEvidence() {
	needed := map[ID]bool{}
	for _, i := range p.Implementations {
		if ast.IsExported(i.Target.Name) {
			needed[i.Protocol] = true
		}
	}
	for _, f := range p.Functions {
		if ast.IsExported(f.Name) {
			for _, b := range f.Bounds {
				needed[b.Protocol.ID] = true
			}
		}
	}
	for i := range p.Definitions {
		d := &p.Definitions[i]
		if needed[d.ID] && !d.Public {
			d.ExportedEvidence = true
		}
		d.OpsName, d.MarkerName = DefinitionNames(d.ID, d.Public || d.ExportedEvidence)
		p.declarations[d.Name] = *d
	}
	refresh := func(b Bound) Bound {
		if b.Protocol.Owner == p.Owner {
			if d, ok := p.declarations[b.Protocol.Name]; ok {
				b.Protocol = d
			}
		}
		return b
	}
	for file, funcs := range p.GenericBindings {
		for name, bounds := range funcs {
			for i, b := range bounds {
				bounds[i] = refresh(b)
			}
			p.GenericBindings[file][name] = bounds
		}
	}
	for i := range p.Functions {
		for j, b := range p.Functions[i].Bounds {
			p.Functions[i].Bounds[j] = refresh(b)
		}
	}
}

func (p *Package) lookupProtocol(file, name string) (Definition, bool, error) {
	if d, ok := p.declarations[name]; ok {
		return d, true, nil
	}
	if p.options.LookupProtocol != nil {
		return p.options.LookupProtocol(file, name)
	}
	return Definition{}, false, nil
}
