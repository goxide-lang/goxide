package expand

import (
	"fmt"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

type templateError struct {
	Span          ast.Span
	Code, Message string
}

func (e *templateError) Error() string { return e.Message }

type templatePart struct {
	Token   *token.Token
	Tree    *templateGroup
	Capture string
	Repeat  *templateRepeat
	Span    ast.Span
	Caller  bool
}

type templateGroup struct {
	Delim ast.Delim
	Parts []templatePart
	Span  ast.Span
}

type templateRepeat struct {
	Parts []templatePart
	Names []string
	Sep   *token.Token
	Op    token.Kind
}

func renderTemplate(tree *ast.TokenTree, caps captures, definition *ast.File, templateCtx, callerCtx ast.SyntaxCtx, budget func() error) ([]syntaxToken, error) {
	parts, err := compileTemplate(tree.Tokens, caps, 0)
	if err != nil {
		return nil, err
	}
	r := templateRenderer{caps: caps, definition: definition, templateCtx: templateCtx, callerCtx: callerCtx, budget: budget}
	return r.render(parts, nil)
}

func compileTemplate(tokens []ast.TokenOrTree, caps captures, depth int) ([]templatePart, error) {
	var out []templatePart
	for i := 0; i < len(tokens); i++ {
		current := tokens[i]
		if current.Tree != nil {
			parts, err := compileTemplate(current.Tree.Tokens, caps, depth)
			if err != nil {
				return nil, err
			}
			out = append(out, templatePart{Tree: &templateGroup{Delim: current.Tree.Delim, Parts: parts, Span: current.Tree.Span()}, Span: current.Tree.Span()})
			continue
		}
		t := current.Token
		span := ast.Span{Start: t.Start, End: t.End}
		if t.Kind != token.DOLLAR {
			out = append(out, templatePart{Token: t, Span: span})
			continue
		}
		if i+1 == len(tokens) {
			return nil, templateProblem(span, "template", "expected a capture, repetition, or $$ after $")
		}
		next := tokens[i+1]
		if next.Tree != nil {
			if next.Tree.Delim != ast.Paren {
				return nil, templateProblem(span, "template", "template repetition requires $(...)")
			}
			j := i + 2
			var sep *token.Token
			if j < len(tokens) && tokens[j].Token != nil && !templateRepeatOp(tokens[j].Token.Kind) {
				sep = tokens[j].Token
				j++
			}
			if j == len(tokens) || tokens[j].Token == nil || !templateRepeatOp(tokens[j].Token.Kind) {
				return nil, templateProblem(span, "template", "template repetition requires *, +, or ?")
			}
			span.End = tokens[j].Token.End
			parts, err := compileTemplate(next.Tree.Tokens, caps, depth+1)
			if err != nil {
				return nil, err
			}
			var names []string
			for _, name := range templateCaptures(parts) {
				if caps[name].Depth > depth {
					names = append(names, name)
				}
			}
			if len(names) == 0 {
				return nil, templateProblem(span, "repetition-depth", "template repetition has no capture to determine its length")
			}
			out = append(out, templatePart{Repeat: &templateRepeat{Parts: parts, Names: names, Sep: sep, Op: tokens[j].Token.Kind}, Span: span})
			i = j
			continue
		}
		if next.Token.Kind == token.DOLLAR {
			out = append(out, templatePart{Token: t, Span: span})
			i++
			continue
		}
		if next.Token.Kind != token.IDENT {
			return nil, templateProblem(span, "template", "expected a capture name after $")
		}
		span.End = next.Token.End
		if next.Token.Text == "caller" {
			j := i + 2
			if !templateTokenIs(tokens, j, token.DCOLON) || !templateTokenIs(tokens, j+1, token.IDENT) {
				return nil, templateProblem(span, "template", "$caller requires :: followed by a path")
			}
			j++
			for {
				name := tokens[j].Token
				out = append(out, templatePart{Token: name, Span: ast.Span{Start: name.Start, End: name.End}, Caller: true})
				j++
				if j < len(tokens) && tokens[j].Tree != nil && tokens[j].Tree.Delim == ast.Bracket {
					args := tokens[j].Tree
					parts, err := compileTemplate(args.Tokens, caps, depth)
					if err != nil {
						return nil, err
					}
					out = append(out, templatePart{Tree: &templateGroup{Delim: args.Delim, Parts: parts, Span: args.Span()}, Span: args.Span()})
					j++
				}
				if !templateTokenIs(tokens, j, token.DCOLON) || !templateTokenIs(tokens, j+1, token.IDENT) {
					break
				}
				colon := tokens[j].Token
				out = append(out, templatePart{Token: colon, Span: ast.Span{Start: colon.Start, End: colon.End}})
				j++
			}
			i = j - 1
			continue
		}
		name := next.Token.Text
		c := caps[name]
		if c == nil {
			return nil, templateProblem(span, "template", "unknown capture $"+name)
		}
		if c.Depth > depth {
			return nil, templateProblem(span, "repetition-depth", fmt.Sprintf("capture $%s has repetition depth %d, template uses depth %d", name, c.Depth, depth))
		}
		out = append(out, templatePart{Capture: name, Span: span})
		i++
	}
	return out, nil
}

func templateRepeatOp(k token.Kind) bool {
	return k == token.MUL || k == token.ADD || k == token.QUESTION
}

func templateTokenIs(tokens []ast.TokenOrTree, i int, kind token.Kind) bool {
	return i < len(tokens) && tokens[i].Token != nil && tokens[i].Token.Kind == kind
}

func templateProblem(span ast.Span, code, message string) error {
	return &templateError{Span: span, Code: code, Message: message}
}

func templateCaptures(parts []templatePart) []string {
	var names []string
	seen := make(map[string]bool)
	var visit func([]templatePart)
	visit = func(parts []templatePart) {
		for _, part := range parts {
			if part.Capture != "" && !seen[part.Capture] {
				seen[part.Capture] = true
				names = append(names, part.Capture)
			}
			if part.Tree != nil {
				visit(part.Tree.Parts)
			}
			if part.Repeat != nil {
				visit(part.Repeat.Parts)
			}
		}
	}
	visit(parts)
	return names
}

type templateRenderer struct {
	caps                   captures
	definition             *ast.File
	templateCtx, callerCtx ast.SyntaxCtx
	budget                 func() error
}

func (r *templateRenderer) render(parts []templatePart, indices []int) ([]syntaxToken, error) {
	var out []syntaxToken
	for _, part := range parts {
		if part.Repeat != nil {
			count := -1
			for _, name := range part.Repeat.Names {
				c := r.captureAt(name, indices)
				if count >= 0 && count != len(c.Values) {
					return nil, templateProblem(part.Span, "repetition-length", "captures in template repetition have different lengths")
				}
				count = len(c.Values)
			}
			if part.Repeat.Op == token.ADD && count == 0 || part.Repeat.Op == token.QUESTION && count > 1 {
				return nil, templateProblem(part.Span, "repetition-length", "capture count does not satisfy template repetition "+part.Repeat.Op.String())
			}
			for i := 0; i < count; i++ {
				if i > 0 && part.Repeat.Sep != nil {
					sep := part.Repeat.Sep
					if err := r.charge(); err != nil {
						return nil, err
					}
					out = append(out, syntaxToken{Token: sep, Ctx: r.templateCtx, Origin: Origin{File: r.definition, Span: ast.Span{Start: sep.Start, End: sep.End}}})
				}
				items, err := r.render(part.Repeat.Parts, append(indices, i))
				if err != nil {
					return nil, err
				}
				out = append(out, items...)
			}
			continue
		}
		if err := r.charge(); err != nil {
			return nil, err
		}
		item := syntaxToken{Origin: Origin{File: r.definition, Span: part.Span}, Ctx: r.templateCtx}
		switch {
		case part.Token != nil:
			item.Token = part.Token
			if part.Caller {
				item.Ctx = r.callerCtx
			}
		case part.Tree != nil:
			items, err := r.render(part.Tree.Parts, indices)
			if err != nil {
				return nil, err
			}
			item.Tree = &syntaxTree{Delim: part.Tree.Delim, Tokens: items, Origin: item.Origin}
		case part.Capture != "":
			item.Fragment = r.captureAt(part.Capture, indices).Leaf
			item.Ctx = r.callerCtx
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *templateRenderer) captureAt(name string, indices []int) *capture {
	c := r.caps[name]
	for _, i := range indices[:min(len(indices), c.Depth)] {
		c = c.Values[i]
	}
	return c
}

func (r *templateRenderer) charge() error {
	if r.budget != nil {
		return r.budget()
	}
	return nil
}
