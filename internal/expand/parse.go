package expand

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/token"
)

const stmtKind fragmentKind = "statements"

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
	text     strings.Builder
	pieces   []piece
	trees    map[uint32]*syntaxTree
	ttRanges map[ast.Span]Origin
	previous token.Kind
}

func (s *skeleton) atom(text string, t syntaxToken, tt bool, use Origin, kind token.Kind) {
	if s.text.Len() > 0 && kind != token.NOT && s.previous != token.NOT {
		s.text.WriteByte(' ')
	}
	start := uint32(s.text.Len())
	s.text.WriteString(text)
	s.pieces = append(s.pieces, piece{start, uint32(s.text.Len()), t, tt, use})
	s.previous = kind
}
func (s *skeleton) write(ts []syntaxToken, tt bool, use Origin) {
	for _, t := range ts {
		if f := t.Fragment; f != nil {
			if f.Kind == ttKind {
				first := len(s.pieces)
				s.write(f.Tokens, true, t.Origin)
				if len(s.pieces) > first {
					if s.ttRanges == nil {
						s.ttRanges = map[ast.Span]Origin{}
					}
					s.ttRanges[ast.Span{Start: s.pieces[first].start, End: s.pieces[len(s.pieces)-1].end}] = t.Origin
				}
				continue
			}
			name := "fragment"
			if f.Kind == identKind {
				id := f.Node.(*ast.Ident)
				name = id.Name
				if id.Raw {
					name = "r#" + name
				}
			}
			if f.Kind == itemKind {
				name += "!()"
			}
			if f.Kind == exprKind && (s.previous == token.GO || s.previous == token.DEFER) {
				name += "()"
			}
			s.atom(name, t, tt, use, token.IDENT)
		} else if tr := t.Tree; tr != nil {
			open, close := delimiters(tr.Delim)
			if tr.Delim == ast.NoDelim {
				s.write(tr.Tokens, tt, use)
				continue
			}
			s.atom(open, t, tt, use, token.LPAREN)
			s.trees[s.pieces[len(s.pieces)-1].start] = tr
			s.write(tr.Tokens, tt, use)
			s.atom(close, t, tt, use, token.RPAREN)
		} else if tok := t.Token; tok != nil {
			text := tok.Text
			if text == "" {
				text = tok.Kind.String()
			}
			if tok.Raw {
				text = "r#" + text
			}
			s.atom(text, t, tt, use, tok.Kind)
		}
	}
}
func delimiters(d ast.Delim) (string, string) {
	switch d {
	case ast.Paren:
		return "(", ")"
	case ast.Bracket:
		return "[", "]"
	case ast.Brace:
		return "{", "}"
	}
	return "", ""
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
	s := &skeleton{trees: map[uint32]*syntaxTree{}}
	if kind == stmtKind {
		s.text.WriteString("{ ")
	}
	s.write(ts, false, Origin{})
	if kind == stmtKind {
		s.text.WriteString(" }")
	}
	var n ast.Node
	var ds []parser.Diagnostic
	src := []byte(s.text.String())
	switch kind {
	case exprKind, identKind:
		n, ds = parser.ParseExpr(src)
	case typeKind:
		n, ds = parser.ParseType(src)
	case itemKind:
		n, ds = parser.ParseItem(src)
	case stmtKind:
		n, ds = parser.ParseStmt(src)
	default:
		return nil, fmt.Errorf("unknown fragment category %s", kind)
	}
	for _, d := range ds {
		if d.Severity == parser.Error {
			p := s.at(d.Span.Start)
			origin := p.t.Origin
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
	p := s.at(span.Start)
	o := p.t.Origin
	if span.End > span.Start {
		end := s.at(span.End - 1).t.Origin
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
		if use, ok := s.ttRanges[span]; ok {
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
				return v, &parseError{code: "category", message: "template identifier slot requires ident fragment", origin: p.t.Origin}
			}
			x, err := e.splice(n, p.t.Fragment, typeSlot, p.t.Origin, expn)
			if err != nil {
				return v, err
			}
			if x != nil {
				return reflect.ValueOf(x), nil
			}
		}
		_, isType := n.(ast.Type)
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
		// Multi-argument indexing is unambiguously generic instantiation.
		idx, _ := n.(*ast.IndexExpr)
		pathPart := false
		switch n.(type) {
		case *ast.PathType, *ast.Path, *ast.PathSegment:
			pathPart = typeSlot || isType
		}
		elem := v.Elem()
		for i := 0; i < elem.NumField(); i++ {
			f := elem.Field(i)
			if f.Type() == baseType {
				continue
			}
			forceType := pathPart || idx != nil && len(idx.Indices) > 1 && elem.Type().Field(i).Name == "Indices"
			x, err := e.restore(f, s, expn, caller, forceType, f.Type() == identPointer)
			if err != nil {
				return v, err
			}
			if !x.Type().AssignableTo(f.Type()) {
				return v, &parseError{code: "category", message: "fragment does not fit template syntax slot", origin: s.origin(span)}
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

func (e *engine) splice(slot ast.Node, f *fragment, typeSlot bool, use Origin, expn ast.ExpnID) (ast.Node, error) {
	fail := func() (ast.Node, error) {
		return nil, &parseError{code: "category", message: fmt.Sprintf("template %T slot cannot receive %s fragment", slot, f.Kind), origin: use}
	}
	if typeSlot && f.Kind != typeKind {
		return fail()
	}
	copy := func() ast.Node {
		n := e.copyNode(f.Node, expn)
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
		return out, nil
	case *ast.Ident:
		if f.Kind == exprKind || f.Kind == identKind {
			return copy(), nil
		}
		if f.Kind == typeKind {
			t := copy().(ast.Type)
			n := &ast.TypeExpr{Base: ast.Base{Loc: t.Span(), Expn: expn}, Type: t}
			e.r.Origins[n] = e.r.Origins[t]
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
				e.r.TokenContexts[&tok] = TokenContext{Ctx: t.Ctx, Scope: t.Scope, Origin: t.Origin}
				out.Tokens = append(out.Tokens, ast.TokenOrTree{Token: &tok})
			}
		}
	}
	appendTokens(tr.Tokens)
	return out
}
