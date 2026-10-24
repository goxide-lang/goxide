package expand

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

const stmtKind fragmentKind = "statements"
const singleStmtKind fragmentKind = "statement"
const itemsKind fragmentKind = "items"
const traitMembersKind fragmentKind = "trait members"
const implMembersKind fragmentKind = "impl members"

type parseError struct {
	code, message string
	origin        Origin
}

func (e *parseError) Error() string { return e.message }

type piece struct {
	start, end uint32
	t          syntaxToken
	// tt tokens may be parsed, but never acquire type-fragment privileges.
	tt  bool
	use Origin
}
type skeleton struct {
	tokens   []token.Token
	pieces   []piece
	trees    map[uint32]*syntaxTree
	ttRanges map[ast.Span]Origin
	groups   map[ast.Span]Origin
	offset   uint32
	previous token.Kind
}

// Virtual coordinates identify output occurrences without changing the source
// coordinates kept in pieces. Macro bangs retain their syntactic adjacency.
func (s *skeleton) append(tok token.Token) token.Token {
	if len(s.tokens) > 0 && tok.Kind != token.NOT && s.previous != token.NOT {
		s.offset++
	}
	width := len(tok.Text)
	if tok.Raw {
		width += 2
	}
	if width == 0 {
		width = len(tok.Kind.String())
	}
	tok.Start, tok.End = s.offset, s.offset+uint32(width)
	s.offset, s.previous = tok.End, tok.Kind
	s.tokens = append(s.tokens, tok)
	return tok
}

func (s *skeleton) atom(tok token.Token, t syntaxToken, tt bool, use Origin) {
	tok = s.append(tok)
	s.pieces = append(s.pieces, piece{tok.Start, tok.End, t, tt, use})
}

func tokenLocation(t syntaxToken) Origin {
	if t.Location.File != nil {
		return t.Location
	}
	return t.Origin
}

func (s *skeleton) write(ts []syntaxToken, tt bool, use, location Origin) {
	for _, t := range ts {
		if location.File != nil {
			t.Location = location
		}
		if f := t.Fragment; f != nil {
			if f.Kind == ttKind {
				first := len(s.pieces)
				s.write(f.Tokens, true, tokenLocation(t), t.Location)
				if len(s.pieces) > first {
					if s.ttRanges == nil {
						s.ttRanges = map[ast.Span]Origin{}
					}
					s.ttRanges[ast.Span{Start: s.pieces[first].start, End: s.pieces[len(s.pieces)-1].end}] = tokenLocation(t)
				}
				continue
			}
			marker := token.Token{Kind: token.IDENT, Text: "fragment"}
			if f.Kind == identKind {
				id := f.Node.(*ast.Ident)
				marker.Text, marker.Raw = id.Name, id.Raw
			}
			call := f.Kind == exprKind && (s.previous == token.GO || s.previous == token.DEFER)
			start := s.append(marker).Start
			if f.Kind == itemKind {
				s.append(token.Token{Kind: token.NOT, Text: "!"})
			}
			if f.Kind == itemKind || call {
				s.append(token.Token{Kind: token.LPAREN, Text: "("})
				s.append(token.Token{Kind: token.RPAREN, Text: ")"})
			}
			s.pieces = append(s.pieces, piece{start, s.offset, t, tt, use})
		} else if tr := t.Tree; tr != nil {
			if location.File != nil {
				tr = syntaxLocations([]syntaxToken{t}, location)[0].Tree
				t.Tree = tr
			}
			if t.Location.File == nil && tr.Location.File != nil {
				t.Location = tr.Location
			}
			if tr.Delim == ast.NoDelim {
				s.write(tr.Tokens, tt, use, location)
				continue
			}
			open, close := delimiterKinds(tr.Delim)
			boundary := t
			boundary.Location = tr.Open
			if boundary.Location.File == nil {
				boundary.Location = tokenLocation(t)
				if boundary.Location.Span.End > boundary.Location.Span.Start {
					boundary.Location.Span.End = boundary.Location.Span.Start + 1
				}
			}
			s.atom(token.Token{Kind: open, Text: open.String()}, boundary, tt, use)
			start := s.pieces[len(s.pieces)-1].start
			s.trees[start] = tr
			s.write(tr.Tokens, tt, use, location)
			boundary = t
			boundary.Location = tr.Close
			if boundary.Location.File == nil {
				boundary.Location = tokenLocation(t)
				if boundary.Location.Span.End > boundary.Location.Span.Start {
					boundary.Location.Span.Start = boundary.Location.Span.End - 1
				}
			}
			s.atom(token.Token{Kind: close, Text: close.String()}, boundary, tt, use)
			if s.groups == nil {
				s.groups = map[ast.Span]Origin{}
			}
			s.groups[ast.Span{Start: start, End: s.offset}] = tokenLocation(t)
		} else if tok := t.Token; tok != nil {
			s.atom(*tok, t, tt, use)
		}
	}
}

func delimiterKinds(d ast.Delim) (token.Kind, token.Kind) {
	switch d {
	case ast.Paren:
		return token.LPAREN, token.RPAREN
	case ast.Bracket:
		return token.LBRACK, token.RBRACK
	case ast.Brace:
		return token.LBRACE, token.RBRACE
	}
	return token.ILLEGAL, token.ILLEGAL
}

func (e *engine) parseFragment(kind fragmentKind, ts []syntaxToken) (*fragment, error) {
	if len(ts) == 1 && ts[0].Fragment != nil {
		f := ts[0].Fragment
		if f.Kind == kind || f.Kind == identKind && kind == exprKind {
			return f, nil
		}
		return nil, fmt.Errorf("expected %s fragment, found %s", kind, f.Kind)
	}
	if kind == ttKind {
		return &fragment{Kind: kind, Tokens: ts}, nil
	}
	if kind == identKind {
		if len(ts) != 1 || ts[0].Token == nil || ts[0].Token.Kind != token.IDENT {
			return nil, fmt.Errorf("expected ident fragment")
		}
	}
	n, err := e.parse(ts, kind, 0, Scope{})
	if err != nil {
		return nil, err
	}
	if kind == identKind {
		if _, ok := n.(*ast.Ident); !ok {
			return nil, fmt.Errorf("expected ident fragment")
		}
	}
	return &fragment{Kind: kind, Node: n, Tokens: ts}, nil
}

func (e *engine) parse(ts []syntaxToken, kind fragmentKind, expn ast.ExpnID, caller Scope) (ast.Node, error) {
	if e.r.Locations == nil {
		e.r.Locations = map[ast.Node]Origin{}
	}
	if e.r.Captures == nil {
		e.r.Captures = map[ast.Node]Capture{}
	}
	if e.r.Allocations == nil {
		e.r.Allocations = map[*ast.CallExpr]bool{}
	}
	s := &skeleton{trees: map[uint32]*syntaxTree{}}
	if kind == stmtKind {
		s.append(token.Token{Kind: token.LBRACE, Text: "{"})
	}
	s.write(ts, false, Origin{}, Origin{})
	if kind == stmtKind {
		s.append(token.Token{Kind: token.RBRACE, Text: "}"})
	}
	var n ast.Node
	var ds []parser.Diagnostic
	switch kind {
	case exprKind, identKind:
		n, ds = parser.ParseExprTokens(s.tokens)
	case typeKind:
		n, ds = parser.ParseTypeTokens(s.tokens)
	case itemKind:
		n, ds = parser.ParseItemTokens(s.tokens)
	case itemsKind:
		var items []ast.Item
		items, ds = parser.ParseItemsTokens(s.tokens)
		n = &ast.File{Items: items}
	case traitMembersKind, implMembersKind:
		var items []ast.Item
		items, ds = parser.ParseMembersTokens(s.tokens, kind == traitMembersKind)
		n = &ast.File{Items: items}
	case stmtKind:
		n, ds = parser.ParseStmtTokens(s.tokens)
	default:
		return nil, fmt.Errorf("unknown fragment category %s", kind)
	}
	for _, d := range ds {
		if d.Severity == parser.Error {
			p := s.at(d.Span.Start)
			origin := tokenLocation(p.t)
			if p.tt && p.use.File != nil {
				origin = p.use
			}
			return nil, &parseError{code: "category", message: fmt.Sprintf("expected %s: %s", kind, d.Message), origin: origin}
		}
	}
	v, err := e.restore(reflect.ValueOf(n), s, expn, caller, false, false)
	if err != nil {
		return nil, err
	}
	n = v.Interface().(ast.Node)
	if expn != 0 {
		if err := e.count(n); err != nil {
			return nil, err
		}
	}
	return n, nil
}

func (s *skeleton) at(start uint32) piece {
	i := sort.Search(len(s.pieces), func(i int) bool { return s.pieces[i].end > start })
	if i < len(s.pieces) {
		return s.pieces[i]
	}
	return piece{}
}
func (s *skeleton) exact(span ast.Span) (piece, bool) {
	p := s.at(span.Start)
	return p, p.start == span.Start && p.end == span.End
}
func (s *skeleton) origin(span ast.Span) Origin {
	return s.rangeOrigin(span, false)
}

func (s *skeleton) location(span ast.Span) Origin {
	if location, ok := s.groups[span]; ok {
		return location
	}
	return s.rangeOrigin(span, true)
}

func (s *skeleton) rangeOrigin(span ast.Span, display bool) Origin {
	position := func(t syntaxToken) Origin {
		if display {
			return tokenLocation(t)
		}
		return t.Origin
	}
	o := position(s.at(span.Start).t)
	if span.End > span.Start {
		end := position(s.at(span.End - 1).t)
		if o.File == end.File && end.Span.End >= o.Span.Start {
			o.Span.End = end.Span.End
		}
	}
	return o
}

var typeInterface = reflect.TypeOf((*ast.Type)(nil)).Elem()
var identPointer = reflect.TypeOf((*ast.Ident)(nil))

func (e *engine) restore(v reflect.Value, s *skeleton, expn ast.ExpnID, caller Scope, typeSlot, identSlot bool) (reflect.Value, error) {
	if !v.IsValid() || (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) && v.IsNil() {
		return v, nil
	}
	if v.Kind() == reflect.Interface {
		x, err := e.restore(v.Elem(), s, expn, caller, typeSlot || v.Type() == typeInterface, false)
		if err != nil {
			return v, err
		}
		out := reflect.New(v.Type()).Elem()
		if !x.Type().AssignableTo(v.Type()) {
			return v, &parseError{code: "category", message: "fragment does not fit template syntax slot"}
		}
		out.Set(x)
		return out, nil
	}
	if v.Kind() == reflect.Pointer {
		n, ok := v.Interface().(ast.Node)
		if !ok {
			return v, nil
		}
		span := n.Span()
		// A positional argument in an expression path may denote a const.
		// Restore a captured expression as a value; the checker knows the
		// declaration's parameter kind and validates its capture category.
		if argument, ok := n.(*ast.GenericArg); ok && !typeSlot && argument.Name == nil {
			if path, ok := argument.Type.(*ast.PathType); ok && len(path.Path.Segments) == 1 && path.Path.Segments[0].Args == nil {
				piece, exact := s.exact(path.Span())
				_, tokens := s.ttRanges[path.Span()]
				if tokens || exact && piece.t.Fragment != nil && piece.t.Fragment.Kind != typeKind {
					argument.Type, argument.Value = nil, path.Path.Segments[0].Name
				}
			}
		}
		var body []syntaxToken
		_, groupedType := n.(*ast.TypeSpec)
		if _, item := n.(ast.Item); item || groupedType {
			start := span.Start
			attrs := reflect.ValueOf(n).Elem().FieldByName("Attrs").Interface().([]*ast.Attr)
			if len(attrs) != 0 {
				start = attrs[len(attrs)-1].Span().End
			}
			body = s.syntax(ast.Span{Start: start, End: span.End})
		}
		if use, ok := s.ttRanges[span]; ok {
			e.r.Captures[n] = Capture{Kind: string(ttKind), Use: use}
			if _, isType := n.(ast.Type); typeSlot || isType {
				return v, &parseError{code: "category", message: "template type slot requires type fragment, found tt", origin: use}
			}
			_, expr := n.(ast.Expr)
			_, stmt := n.(ast.Stmt)
			if expr || stmt {
				// The complete captured expression/block owns its internal types;
				// they are not type slots written by the template.
				for i := range s.pieces {
					p := &s.pieces[i]
					if p.start >= span.Start && p.end <= span.End {
						p.tt = false
					}
				}
				delete(s.ttRanges, span)
			}
		}
		if tree, ok := n.(*ast.TokenTree); ok {
			if tr, ok := s.trees[span.Start]; ok {
				return reflect.ValueOf(e.materializeTree(tr, expn)), nil
			}
			// A placeholder item's synthetic () is consumed with its parent.
			_ = tree
		}
		if p, exact := s.exact(span); exact && p.t.Fragment != nil {
			if identSlot && p.t.Fragment.Kind != identKind {
				return v, &parseError{code: "category", message: "template identifier slot requires ident fragment", origin: tokenLocation(p.t)}
			}
			x, err := e.splice(n, p.t.Fragment, typeSlot, tokenLocation(p.t), expn)
			if err != nil {
				return v, err
			}
			if x != nil {
				if p.t.Location.File != nil {
					e.locateFragment(x, p.t.Location)
				}
				return reflect.ValueOf(x), nil
			}
		}
		_, isType := n.(ast.Type)
		if call, ok := n.(*ast.CallExpr); ok && expn != 0 && e.r.Captures[n].Kind == "" {
			if id, ok := call.Fun.(*ast.Ident); ok && (id.Name == "make" || id.Name == "new") {
				p := s.at(id.Span().Start)
				if p.t.Fragment == nil && !p.tt && p.t.Ctx == e.r.Expansions[expn].Ctx {
					e.r.Allocations[call] = true
				}
			}
		}
		if typeSlot || isType {
			for _, p := range s.pieces {
				if p.start >= span.Start && p.end <= span.End && p.tt {
					// Children validate their own slots. This leaves delimiters and
					// keywords here, including anonymous types with no named leaf.
					if !childContains(n, ast.Span{Start: p.start, End: p.end}) {
						return v, &parseError{code: "category", message: "template type slot requires type fragment, found tt", origin: p.use}
					}
				}
			}
		}
		pathPart := false
		switch n.(type) {
		case *ast.PathType, *ast.Path, *ast.PathSegment, *ast.GenericArgs, *ast.GenericArg:
			pathPart = typeSlot || isType
		}
		elem := v.Elem()
		for i := 0; i < elem.NumField(); i++ {
			f := elem.Field(i)
			if f.Type() == baseType {
				continue
			}
			x, err := e.restore(f, s, expn, caller, pathPart, f.Type() == identPointer)
			if err != nil {
				return v, err
			}
			if !x.Type().AssignableTo(f.Type()) {
				return v, &parseError{code: "category", message: "fragment does not fit template syntax slot", origin: s.location(span)}
			}
			f.Set(x)
		}
		o := s.origin(span)
		b := elem.FieldByName("Base")
		base := b.Interface().(ast.Base)
		base.Loc = o.Span
		base.Expn = expn
		b.Set(reflect.ValueOf(base))
		e.r.Origins[n] = o
		e.r.Locations[n] = s.location(span)
		if len(body) != 0 {
			if e.itemBodies == nil {
				e.itemBodies = map[ast.Node][]syntaxToken{}
			}
			e.itemBodies[n] = body
		}
		if e.lexical == nil {
			e.lexical = map[ast.Node]Scope{}
		}
		e.lexical[n] = s.at(span.Start).t.Lexical
		if id, ok := n.(*ast.Ident); ok {
			p := s.at(span.Start)
			id.Ctx = p.t.Ctx
			scope := p.t.Scope
			if scope.File != nil {
				// Explicit token scope survives forwarding through nested macros.
			} else if c, ok := e.r.Contexts[id.Ctx]; ok {
				scope = c.Definition
			} else if expn != 0 && id.Ctx == caller.Ctx {
				scope = caller
			} else {
				scope = Scope{o.File, id.Ctx}
			}
			e.r.Scopes[id] = scope
		}
		return v, nil
	}
	switch v.Kind() {
	case reflect.Struct:
		if v.Type() == baseType {
			return v, nil
		}
		for i := 0; i < v.NumField(); i++ {
			x, err := e.restore(v.Field(i), s, expn, caller, typeSlot, v.Field(i).Type() == identPointer)
			if err != nil {
				return v, err
			}
			v.Field(i).Set(x)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			x, err := e.restore(v.Index(i), s, expn, caller, typeSlot, v.Index(i).Type() == identPointer)
			if err != nil {
				return v, err
			}
			if !x.Type().AssignableTo(v.Index(i).Type()) {
				return v, &parseError{code: "category", message: "fragment does not fit template syntax slot"}
			}
			v.Index(i).Set(x)
		}
	}
	return v, nil
}

// Associate only this output occurrence with a diagnostic position. Nested
// macro calls also carry cached token streams, so AST locations alone do not
// cover the next expansion's input. Source identities remain untouched.
func (e *engine) locateFragment(n ast.Node, location Origin) {
	ast.Walk(n, func(child ast.Node) bool {
		e.r.Locations[child] = location
		if item, ok := child.(ast.Item); ok {
			if body, ok := e.itemBodies[item]; ok {
				e.itemBodies[item] = syntaxLocations(body, location)
			}
		}
		if tree, ok := child.(*ast.TokenTree); ok {
			if tokens, ok := e.trees[tree]; ok {
				e.trees[tree] = syntaxLocations(tokens, location)
			}
			for _, item := range tree.Tokens {
				if item.Token == nil {
					continue
				}
				if context, ok := e.r.TokenContexts[item.Token]; ok {
					context.Location = location
					e.r.TokenContexts[item.Token] = context
				}
			}
		}
		return true
	})
}

func syntaxLocations(tokens []syntaxToken, location Origin) []syntaxToken {
	out := append([]syntaxToken(nil), tokens...)
	for i := range out {
		t := &out[i]
		t.Location = location
		if t.Tree != nil {
			tree := *t.Tree
			tree.Location, tree.Open, tree.Close = location, location, location
			tree.Tokens = syntaxLocations(tree.Tokens, location)
			t.Tree = &tree
		}
		if t.Fragment != nil {
			fragment := *t.Fragment
			fragment.Tokens = syntaxLocations(fragment.Tokens, location)
			t.Fragment = &fragment
		}
	}
	return out
}

func (e *engine) splice(slot ast.Node, f *fragment, typeSlot bool, use Origin, expn ast.ExpnID) (ast.Node, error) {
	fail := func() (ast.Node, error) {
		return nil, &parseError{code: "category", message: fmt.Sprintf("template %T slot cannot receive %s fragment", slot, f.Kind), origin: use}
	}
	_, typed := slot.(ast.Type)
	if (typeSlot || typed) && f.Kind != typeKind {
		return nil, &parseError{code: "category", message: "template type slot requires type fragment; captured types from " + string(f.Kind) + " fragments are not allowed", origin: use}
	}
	copy := func() ast.Node {
		n := e.copyNode(f.Node, expn)
		e.r.Captures[n] = Capture{Kind: string(f.Kind), Use: use}
		if e.captured == nil {
			e.captured = map[ast.Node]bool{}
		}
		ast.Walk(n, func(n ast.Node) bool { e.captured[n] = true; return true })
		return n
	}
	switch slot.(type) {
	case *ast.PathType:
		if f.Kind != typeKind {
			return fail()
		}
		return copy(), nil
	case *ast.MacroExpr:
		return fail()
	case *ast.MacroItem:
		if f.Kind != itemKind {
			return fail()
		}
		return copy(), nil
	case *ast.ExprStmt:
		if f.Kind != itemKind {
			return nil, nil
		}
		decl := copy().(ast.Item)
		switch decl.(type) {
		case *ast.VarDecl, *ast.ConstDecl, *ast.TypeDecl:
		default:
			return fail()
		}
		out := &ast.DeclStmt{StmtBase: ast.StmtBase{Base: ast.Base{Loc: decl.Span(), Expn: expn}}, Decl: decl}
		e.r.Origins[out] = e.r.Origins[decl]
		e.r.Locations[out] = e.location(decl)
		return out, nil
	case *ast.Ident:
		if f.Kind == exprKind || f.Kind == identKind {
			return copy(), nil
		}
		if f.Kind == typeKind {
			t := copy().(ast.Type)
			n := &ast.TypeExpr{Base: ast.Base{Loc: t.Span(), Expn: expn}, Type: t}
			e.r.Origins[n] = e.r.Origins[t]
			e.r.Locations[n] = e.location(t)
			e.r.Captures[n] = Capture{Kind: string(f.Kind), Use: use}
			return n, nil
		}
		return fail()
	case *ast.CallExpr:
		if f.Kind == exprKind {
			return copy(), nil
		}
	}
	return nil, nil
}

func childContains(n ast.Node, span ast.Span) bool {
	found := false
	ast.Walk(n, func(child ast.Node) bool {
		if child == n {
			return true
		}
		loc := child.Span()
		if loc.Start <= span.Start && loc.End >= span.End {
			found = true
		}
		return false
	})
	return found
}

// TokenTree has no Ctx field. Keep structured tokens beside the materialized
// tree so a nested invocation receives the exact captures, not parser markers.
func (e *engine) materializeTree(tr *syntaxTree, expn ast.ExpnID) *ast.TokenTree {
	out := &ast.TokenTree{Base: ast.Base{Loc: tr.Origin.Span, Expn: expn}, Delim: tr.Delim}
	e.r.Origins[out] = tr.Origin
	if tr.Location.File != nil {
		e.r.Locations[out] = tr.Location
	}
	e.trees[out] = tr.Tokens
	var appendTokens func([]syntaxToken)
	appendTokens = func(ts []syntaxToken) {
		for _, t := range ts {
			if t.Fragment != nil {
				appendTokens(t.Fragment.Tokens)
			} else if t.Tree != nil {
				out.Tokens = append(out.Tokens, ast.TokenOrTree{Tree: e.materializeTree(t.Tree, expn)})
			} else if t.Token != nil {
				tok := *t.Token
				if e.r.TokenContexts == nil {
					e.r.TokenContexts = map[*token.Token]TokenContext{}
				}
				e.r.TokenContexts[&tok] = TokenContext{Ctx: t.Ctx, Scope: t.Scope, Origin: t.Origin, Location: t.Location}
				out.Tokens = append(out.Tokens, ast.TokenOrTree{Token: &tok})
			}
		}
	}
	appendTokens(tr.Tokens)
	return out
}
