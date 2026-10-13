package expand

import (
	"fmt"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

type fragmentKind string

const (
	exprKind  fragmentKind = "expr"
	identKind fragmentKind = "ident"
	ttKind    fragmentKind = "tt"
	typeKind  fragmentKind = "type"
	itemKind  fragmentKind = "item"
)

type capture struct {
	Values []*capture
	Leaf   *fragment
	Depth  int
}

type captures map[string]*capture

type matchError struct {
	Message string
	Span    ast.Span
	Code    string
}

func (e *matchError) Error() string { return e.Message }

type matchTerm struct {
	literal *token.Token
	group   *matchGroup
	repeat  *matchRepeat
	name    string
	kind    fragmentKind
	span    ast.Span
}

type matchGroup struct {
	delim ast.Delim
	terms []matchTerm
}

type matchRepeat struct {
	terms    []matchTerm
	sep      *token.Token
	op       token.Kind
	bindings map[string]int
}

type matcher struct {
	terms []matchTerm
}

func compileMatcher(tree *ast.TokenTree) (*matcher, error) {
	terms, err := compileMatchTerms(tree.Tokens, make(map[string]bool))
	if err != nil {
		return nil, err
	}
	return &matcher{terms: terms}, nil
}

func compileMatchTerms(tokens []ast.TokenOrTree, names map[string]bool) ([]matchTerm, error) {
	var terms []matchTerm
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if t.Tree != nil {
			body, err := compileMatchTerms(t.Tree.Tokens, names)
			if err != nil {
				return nil, err
			}
			terms = append(terms, matchTerm{group: &matchGroup{delim: t.Tree.Delim, terms: body}, span: t.Tree.Span()})
			continue
		}
		span := ast.Span{Start: t.Token.Start, End: t.Token.End}
		if t.Token.Kind != token.DOLLAR {
			terms = append(terms, matchTerm{literal: t.Token, span: span})
			continue
		}
		fail := func(message string) ([]matchTerm, error) {
			return nil, &matchError{Code: "matcher", Message: message, Span: span}
		}
		i++
		if i == len(tokens) {
			return fail("expected capture, repetition, or $$ after $")
		}
		next := tokens[i]
		if next.Tree != nil {
			if next.Tree.Delim != ast.Paren {
				return fail("matcher repetition requires $(...)")
			}
			body, err := compileMatchTerms(next.Tree.Tokens, names)
			if err != nil {
				return nil, err
			}
			r := &matchRepeat{terms: body}
			i++
			if i < len(tokens) && tokens[i].Token != nil && !repeatOperator(tokens[i].Token.Kind) {
				r.sep = tokens[i].Token
				i++
			}
			if i == len(tokens) || tokens[i].Token == nil || !repeatOperator(tokens[i].Token.Kind) {
				return fail("matcher repetition requires *, +, or ?")
			}
			r.op = tokens[i].Token.Kind
			r.bindings = matchBindings(body)
			if matchNullable(body) {
				return fail("matcher repetition must consume input")
			}
			terms = append(terms, matchTerm{repeat: r, span: span})
			continue
		}
		if next.Token.Kind == token.DOLLAR {
			terms = append(terms, matchTerm{literal: next.Token, span: span})
			continue
		}
		if next.Token.Kind != token.IDENT {
			return fail("expected $name or $name:category in matcher")
		}
		name := next.Token.Text
		if name == "caller" {
			return fail("$caller is reserved for call-site paths")
		}
		kind := exprKind
		if i+1 < len(tokens) && tokens[i+1].Token != nil && tokens[i+1].Token.Kind == token.COLON {
			if i+2 >= len(tokens) || tokens[i+2].Token == nil {
				return fail("expected fragment category after :")
			}
			kind = fragmentKind(tokens[i+2].Token.Text)
			i += 2
		}
		switch kind {
		case exprKind, identKind, ttKind, typeKind, itemKind:
		default:
			return fail(fmt.Sprintf("unknown fragment category %q", kind))
		}
		if names[name] {
			return fail(fmt.Sprintf("duplicate capture $%s", name))
		}
		names[name] = true
		terms = append(terms, matchTerm{name: name, kind: kind, span: span})
	}
	return terms, nil
}

func repeatOperator(k token.Kind) bool {
	return k == token.MUL || k == token.ADD || k == token.QUESTION
}

func matchBindings(terms []matchTerm) map[string]int {
	bindings := make(map[string]int)
	for _, t := range terms {
		switch {
		case t.name != "":
			bindings[t.name] = 0
		case t.group != nil:
			for name, depth := range matchBindings(t.group.terms) {
				bindings[name] = depth
			}
		case t.repeat != nil:
			for name, depth := range t.repeat.bindings {
				bindings[name] = depth + 1
			}
		}
	}
	return bindings
}

func matchNullable(terms []matchTerm) bool {
	for _, t := range terms {
		if t.repeat == nil || t.repeat.op == token.ADD {
			return false
		}
	}
	return true
}

type fragmentParser func(fragmentKind, []syntaxToken) (*fragment, error)

type matchState struct {
	parse    fragmentParser
	budget   func() error
	best     *matchError
	position int
	fatal    error
}

func (m *matcher) match(input []syntaxToken, parse fragmentParser, budget func() error) (captures, error) {
	s := &matchState{parse: parse, budget: budget, position: -1}
	var result captures
	s.sequence(m.terms, input, 0, make(captures), func(pos int, c captures) bool {
		if pos != len(input) {
			s.record(pos, &matchError{Code: "unconsumed", Message: "macro matcher did not consume all input", Span: syntaxTokenSpan(input[pos])})
			return false
		}
		result = c
		return true
	})
	if s.fatal != nil {
		return nil, s.fatal
	}
	if result != nil {
		return result, nil
	}
	if s.best == nil {
		s.best = &matchError{Code: "match", Message: "macro input does not match this rule"}
	}
	return nil, s.best
}

func (s *matchState) step() bool {
	if s.fatal != nil {
		return false
	}
	if s.budget != nil {
		if err := s.budget(); err != nil {
			s.fatal = &matchError{Code: "budget", Message: err.Error()}
			return false
		}
	}
	return true
}

func (s *matchState) record(pos int, err *matchError) {
	if pos > s.position || pos == s.position && matchErrorRank(err.Code) > matchErrorRank(s.best.Code) {
		s.position, s.best = pos, err
	}
}

func matchErrorRank(code string) int {
	switch code {
	case "unconsumed":
		return 3
	case "category":
		return 2
	default:
		return 1
	}
}

// Each continuation is tried greedily, then backtracked until the entire rule
// matches. Captures are persistent across alternatives; input syntax is read-only.
func (s *matchState) sequence(terms []matchTerm, input []syntaxToken, pos int, c captures, yield func(int, captures) bool) bool {
	if !s.step() {
		return false
	}
	if len(terms) == 0 {
		return yield(pos, c)
	}
	t := terms[0]
	next := func(end int, captures captures) bool {
		return s.sequence(terms[1:], input, end, captures, yield)
	}
	if t.repeat != nil {
		return s.repetition(t.repeat, input, pos, c, next)
	}
	if pos == len(input) {
		code := "match"
		message := "macro input ended before the matcher"
		if t.name != "" {
			code, message = "category", fmt.Sprintf("expected %s fragment for $%s", t.kind, t.name)
		}
		s.record(pos, &matchError{Code: code, Message: message, Span: t.span})
		return false
	}
	if t.literal != nil {
		if matchLiteral(t.literal, input[pos]) {
			return next(pos+1, c)
		}
		s.record(pos, &matchError{Code: "match", Message: fmt.Sprintf("expected literal %q", t.literal.Text), Span: t.span})
		return false
	}
	if t.group != nil {
		in := input[pos].Tree
		if in == nil || in.Delim != t.group.delim {
			s.record(pos, &matchError{Code: "match", Message: "expected matching delimiter group", Span: t.span})
			return false
		}
		return s.sequence(t.group.terms, in.Tokens, 0, c, func(end int, inner captures) bool {
			if end != len(in.Tokens) {
				s.record(pos, &matchError{Code: "unconsumed", Message: "macro matcher did not consume all input in delimiter group", Span: t.span})
				return false
			}
			return next(pos+1, inner)
		})
	}
	end := len(input)
	if t.kind == identKind || t.kind == ttKind {
		end = pos + 1
	}
	for ; end > pos; end-- {
		if !s.step() {
			return false
		}
		if t.kind == identKind && !matchIdent(input[pos]) {
			break
		}
		f, err := s.parse(t.kind, input[pos:end])
		if err != nil {
			continue
		}
		bound := cloneCaptures(c)
		bound[t.name] = &capture{Leaf: f}
		if next(end, bound) {
			return true
		}
	}
	s.record(pos, &matchError{Code: "category", Message: fmt.Sprintf("expected %s fragment for $%s", t.kind, t.name), Span: t.span})
	return false
}

func (s *matchState) repetition(r *matchRepeat, input []syntaxToken, pos int, outer captures, yield func(int, captures) bool) bool {
	initial := make(captures)
	for name, depth := range r.bindings {
		initial[name] = &capture{Depth: depth + 1}
	}
	var iterate func(int, int, captures) bool
	iterate = func(count, at int, collected captures) bool {
		if !s.step() {
			return false
		}
		if r.op != token.QUESTION || count == 0 {
			start := at
			canContinue := true
			if count > 0 && r.sep != nil {
				canContinue = start < len(input) && matchLiteral(r.sep, input[start])
				start++
			}
			if canContinue && s.sequence(r.terms, input, start, make(captures), func(end int, body captures) bool {
				if end == start {
					return false
				}
				more := make(captures, len(collected))
				for name, previous := range collected {
					values := make([]*capture, len(previous.Values)+1)
					copy(values, previous.Values)
					values[len(previous.Values)] = body[name]
					more[name] = &capture{Depth: previous.Depth, Values: values}
				}
				return iterate(count+1, end, more)
			}) {
				return true
			}
		}
		if count > 0 || r.op != token.ADD {
			all := cloneCaptures(outer)
			for name, value := range collected {
				all[name] = value
			}
			return yield(at, all)
		}
		return false
	}
	return iterate(0, pos, initial)
}

func cloneCaptures(c captures) captures {
	out := make(captures, len(c))
	for name, value := range c {
		out[name] = value
	}
	return out
}

func matchIdent(t syntaxToken) bool {
	return t.Token != nil && t.Token.Kind == token.IDENT || t.Fragment != nil && t.Fragment.Kind == identKind
}

func matchLiteral(want *token.Token, got syntaxToken) bool {
	return got.Token != nil && want.Kind == got.Token.Kind && want.Text == got.Token.Text
}

func syntaxTokenSpan(t syntaxToken) ast.Span {
	if t.Token != nil {
		return ast.Span{Start: t.Token.Start, End: t.Token.End}
	}
	if t.Tree != nil {
		return t.Tree.Origin.Span
	}
	if t.Fragment != nil && t.Fragment.Node != nil {
		return t.Fragment.Node.Span()
	}
	return t.Origin.Span
}
