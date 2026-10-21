package expand

import (
	"fmt"
	"reflect"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

// syntax recovers complete structured units from a parsed output range. A
// fragment remains opaque and each group retains its independent boundaries.
func (s *skeleton) syntax(span ast.Span) []syntaxToken {
	ends := map[uint32]uint32{}
	for group := range s.groups {
		ends[group.Start] = group.End
	}
	var out []syntaxToken
	var skip uint32
	for _, p := range s.pieces {
		if p.start < span.Start || p.end > span.End || p.start < skip {
			continue
		}
		t := p.t
		if tr := s.trees[p.start]; tr != nil && ends[p.start] <= span.End {
			t.Tree, t.Location = tr, tr.Location
			skip = ends[p.start]
		}
		out = append(out, t)
	}
	return out
}

// itemInput uses the current attribute list and the retained body tokens. This
// allows removing an executable attribute without re-lexing or changing the
// identities of body fragments forwarded through earlier macros.
func (e *engine) itemInput(item ast.Item, file *ast.File) ([]syntaxToken, error) {
	body, ok := e.itemBodies[item]
	if !ok {
		original := item
		if n, ok := e.originals[item].(ast.Item); ok {
			original = n
		}
		origin := e.r.Origins[original]
		if origin.File == nil {
			origin = Origin{file, original.Span()}
		}
		span := origin.Span
		attrs := reflect.ValueOf(original).Elem().FieldByName("Attrs").Interface().([]*ast.Attr)
		if len(attrs) != 0 {
			span.Start = attrs[len(attrs)-1].Span().End
		}
		var tokens []token.Token
		for _, tok := range origin.File.Tokens {
			if tok.Kind != token.EOF && tok.Start >= span.Start && tok.End <= span.End {
				tokens = append(tokens, tok)
			}
		}
		var err error
		body, err = sourceSyntax(tokens, origin.File)
		if err != nil {
			return nil, err
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("declaration has no retained source tokens")
		}
		if e.itemBodies == nil {
			e.itemBodies = map[ast.Item][]syntaxToken{}
		}
		e.itemBodies[item] = body
	}
	var out []syntaxToken
	attrs := reflect.ValueOf(item).Elem().FieldByName("Attrs").Interface().([]*ast.Attr)
	for _, attr := range attrs {
		out = append(out, e.attributeInput(attr, file)...)
	}
	return append(out, body...), nil
}

func sourceSyntax(tokens []token.Token, file *ast.File) ([]syntaxToken, error) {
	var out []syntaxToken
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		origin := Origin{file, ast.Span{Start: tok.Start, End: tok.End}}
		t := syntaxToken{Token: &tok, Origin: origin, Location: origin, Scope: Scope{File: file}, Lexical: Scope{File: file}}
		var delim ast.Delim
		switch tok.Kind {
		case token.LPAREN:
			delim = ast.Paren
		case token.LBRACK:
			delim = ast.Bracket
		case token.LBRACE:
			delim = ast.Brace
		}
		if delim != ast.NoDelim {
			_, close := delimiterKinds(delim)
			depth, j := 1, i+1
			for ; j < len(tokens); j++ {
				if tokens[j].Kind == tok.Kind {
					depth++
				}
				if tokens[j].Kind == close {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			if depth != 0 {
				return nil, fmt.Errorf("unbalanced retained declaration tokens")
			}
			children, err := sourceSyntax(tokens[i+1:j], file)
			if err != nil {
				return nil, err
			}
			end := Origin{file, ast.Span{Start: tokens[j].Start, End: tokens[j].End}}
			whole := Origin{file, ast.Span{Start: tok.Start, End: tokens[j].End}}
			t.Token = nil
			t.Origin, t.Location = whole, whole
			t.Tree = &syntaxTree{Delim: delim, Tokens: children, Origin: whole, Location: whole, Open: origin, Close: end}
			i = j
		}
		out = append(out, t)
	}
	return out, nil
}

func (e *engine) attributeInput(attr *ast.Attr, file *ast.File) []syntaxToken {
	origin := e.r.Origins[attr]
	if origin.File == nil {
		origin = Origin{file, attr.Span()}
	}
	location := e.location(attr)
	if location.File == nil {
		location = origin
	}
	lexical := e.lexical[attr]
	if lexical.File == nil {
		lexical = Scope{File: origin.File}
	}
	makeToken := func(kind token.Kind, text string) syntaxToken {
		return syntaxToken{Token: &token.Token{Kind: kind, Text: text}, Origin: origin, Location: location, Ctx: lexical.Ctx, Scope: lexical, Lexical: lexical}
	}
	var inner []syntaxToken
	if attr.Path != nil {
		for i, segment := range attr.Path.Segments {
			if i > 0 {
				inner = append(inner, makeToken(token.DCOLON, "::"))
			}
			id := segment.Name
			t := makeToken(token.IDENT, id.Name)
			t.Token.Raw, t.Ctx = id.Raw, id.Ctx
			if !id.Raw && token.Lookup(id.Name).IsKeyword() {
				t.Token.Kind = token.Lookup(id.Name)
			}
			if o := e.r.Origins[id]; o.File != nil {
				t.Origin = o
			}
			if l := e.location(id); l.File != nil {
				t.Location = l
			}
			if scope, ok := e.r.Scopes[id]; ok {
				t.Scope = scope
			}
			if retained := e.lexical[id]; retained.File != nil {
				t.Lexical = retained
			}
			inner = append(inner, t)
		}
	}
	if attr.Args != nil {
		a := attr.Args
		o := e.r.Origins[a]
		if o.File == nil {
			o = Origin{origin.File, a.Span()}
		}
		l := e.location(a)
		if l.File == nil {
			l = o
		}
		inner = append(inner, syntaxToken{Tree: &syntaxTree{Delim: a.Delim, Tokens: e.treeInput(a, origin.File, lexical.Ctx), Origin: o, Location: l}, Origin: o, Location: l, Ctx: lexical.Ctx, Scope: lexical, Lexical: lexical})
	}
	group := syntaxToken{Tree: &syntaxTree{Delim: ast.Bracket, Tokens: inner, Origin: origin, Location: location}, Origin: origin, Location: location, Ctx: lexical.Ctx, Scope: lexical, Lexical: lexical}
	return []syntaxToken{makeToken(token.HASH, "#"), group}
}
