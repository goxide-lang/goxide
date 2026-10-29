// Package format formats authored Goxide without rewriting its lexical units.
package format

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/lexer"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type Options struct {
	TabSize      int
	InsertSpaces bool
}

// Source preserves comments, literal bytes, explicit separators and macro token
// trees. Existing line breaks remain meaningful; indentation follows delimiters.
func Source(ctx context.Context, filename, source string, options Options) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, diagnostics := parser.ParseFile(filename, []byte(source))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			return "", fmt.Errorf("formatting unavailable: %s", diagnostic.Error())
		}
	}
	indent := "\t"
	if options.InsertSpaces {
		if options.TabSize < 1 || options.TabSize > 32 {
			return "", fmt.Errorf("formatting tab size must be between 1 and 32")
		}
		indent = strings.Repeat(" ", options.TabSize)
	}
	newline := "\n"
	if first := strings.IndexByte(source, '\n'); first > 0 && source[first-1] == '\r' {
		newline = "\r\n"
	}
	tokens, _ := lexer.Lex([]byte(source))
	f := layout{source: source, open: map[uint32]bool{}, close: map[uint32]bool{}, list: map[uint32]bool{}, tight: map[uint32]bool{}, unary: map[uint32]bool{}, header: map[uint32]bool{}}
	protected := map[uint32]uint32{}
	ast.Walk(file, func(node ast.Node) bool {
		if tree, ok := node.(*ast.TokenTree); ok {
			protected[tree.Span().Start] = tree.Span().End
			return false
		}
		return true
	})
	for _, tok := range tokens {
		if tok.Implicit || tok.Kind == token.EOF {
			continue
		}
		if len(f.tokens) != 0 && tok.Start < f.tokens[len(f.tokens)-1].End {
			continue
		}
		piece := unit{Token: tok, last: tok.Kind}
		if end, ok := protected[tok.Start]; ok && end > tok.End {
			piece.End, piece.opaque = end, true
			piece.last = matching(tok.Kind)
		}
		f.tokens = append(f.tokens, piece)
	}
	f.boundaries(file)
	var out strings.Builder
	if strings.HasPrefix(source, "\ufeff") {
		out.WriteString("\ufeff")
	}
	depth := 0
	var previous unit
	for i, current := range f.tokens {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		if !current.opaque && closing(current.Kind) {
			depth--
		}
		gap := source[previous.End:current.Start]
		lines := strings.Count(gap, "\n")
		if lines > 2 {
			lines = 2
		}
		if i > 0 && current.Kind != token.COMMENT && lines == 0 {
			if f.open[previous.Start] && current.Kind != matching(previous.Kind) || f.close[current.Start] && previous.Kind != matching(current.Kind) || previous.Kind == token.SEMICOLON && !f.header[previous.Start] || previous.Kind == token.COMMA && f.list[previous.Start] {
				lines = 1
			}
		}
		lineDirective := current.Kind == token.COMMENT && strings.HasPrefix(current.Text, "//line ")
		if i == 0 && lines == 0 && lineDirective {
			out.WriteString(strings.TrimPrefix(gap, "\ufeff"))
		} else if lines > 0 {
			out.WriteString(strings.Repeat(newline, lines))
			if lineDirective {
				// A line directive is active only at the beginning of its line.
				out.WriteString(gap[strings.LastIndexByte(gap, '\n')+1:])
			} else {
				out.WriteString(strings.Repeat(indent, max(0, depth)))
			}
		} else if i > 0 && f.space(previous, current, gap) {
			out.WriteByte(' ')
		}
		out.WriteString(source[current.Start:current.End])
		if !current.opaque && opening(current.Kind) {
			depth++
		}
		previous = current
	}
	out.WriteString(newline)
	formatted := out.String()
	next, diagnostics := parser.ParseFile(filename, []byte(formatted))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			return "", fmt.Errorf("formatting unavailable: layout would change source syntax: %s", diagnostic.Error())
		}
	}
	if !sameSyntax(reflect.ValueOf(file), reflect.ValueOf(next)) {
		return "", fmt.Errorf("formatting unavailable: layout would change source syntax or directive attachment")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return formatted, nil
}

type unit struct {
	token.Token
	last   token.Kind
	opaque bool
}

type layout struct {
	source                          string
	tokens                          []unit
	open, close, list, tight, unary map[uint32]bool
	header                          map[uint32]bool
}

func opening(kind token.Kind) bool {
	return kind == token.LPAREN || kind == token.LBRACK || kind == token.LBRACE
}
func closing(kind token.Kind) bool {
	return kind == token.RPAREN || kind == token.RBRACK || kind == token.RBRACE
}
func matching(kind token.Kind) token.Kind {
	switch kind {
	case token.LPAREN:
		return token.RPAREN
	case token.RPAREN:
		return token.LPAREN
	case token.LBRACK:
		return token.RBRACK
	case token.RBRACK:
		return token.LBRACK
	case token.LBRACE:
		return token.RBRACE
	case token.RBRACE:
		return token.LBRACE
	}
	return token.ILLEGAL
}

func (f *layout) boundaries(file *ast.File) {
	pairs := map[uint32]int{}
	var stack []int
	for i, tok := range f.tokens {
		if tok.opaque {
			continue
		}
		if opening(tok.Kind) {
			stack = append(stack, i)
		} else if closing(tok.Kind) && len(stack) != 0 {
			pairs[tok.End] = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		}
	}
	block := func(node ast.Node, commas bool) {
		start, exists := pairs[node.Span().End]
		if !exists {
			return
		}
		f.open[f.tokens[start].Start] = true
		f.close[node.Span().End-1] = true
		if commas {
			depth := 0
			last := token.ILLEGAL
			for _, tok := range f.tokens[start+1:] {
				if tok.End >= node.Span().End {
					break
				}
				if !tok.opaque && opening(tok.Kind) {
					depth++
				} else if !tok.opaque && closing(tok.Kind) {
					depth--
				}
				if depth == 0 && tok.Kind == token.COMMA {
					f.list[tok.Start] = true
				}
				last = tok.Kind
			}
			if _, enum := node.(*ast.EnumDecl); enum && last != token.COMMA {
				// Enum syntax requires a comma before a newly inserted newline.
				delete(f.close, node.Span().End-1)
			}
		}
	}
	header := func(start, end uint32) {
		for _, tok := range f.tokens {
			if tok.Start >= start && tok.End <= end && tok.Kind == token.SEMICOLON {
				f.header[tok.Start] = true
			}
		}
	}
	ast.Walk(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.TokenTree:
			return false
		case *ast.Block, *ast.StructType, *ast.InterfaceType, *ast.TraitDecl, *ast.ImplDecl, *ast.ModDecl, *ast.MacroRulesDecl, *ast.SelectStmt:
			block(node, false)
		case *ast.SwitchStmt, *ast.TypeSwitchStmt:
			block(node, false)
			if start, ok := pairs[node.Span().End]; ok {
				header(node.Span().Start, f.tokens[start].Start)
			}
		case *ast.EnumDecl, *ast.MatchExpr:
			block(node, true)
		case *ast.ConstDecl:
			if n.Grouped {
				block(node, false)
			}
		case *ast.VarDecl:
			if n.Grouped {
				block(node, false)
			}
		case *ast.TypeDecl:
			if n.Grouped {
				block(node, false)
			}
		case *ast.ForStmt:
			header(n.Span().Start, n.Body.Span().Start)
		case *ast.IfStmt:
			header(n.Span().Start, n.Body.Span().Start)
		case *ast.UnaryExpr, *ast.RefExpr, *ast.StarExpr, *ast.PointerType, *ast.RefType, *ast.ApproxType:
			f.unary[node.Span().Start] = true
		case *ast.ArrayType:
			f.tight[n.Elem.Span().Start] = true
		case *ast.SliceType:
			f.tight[n.Elem.Span().Start] = true
		case *ast.MapType:
			f.tight[n.Value.Span().Start] = true
		}
		return true
	})
}

func (f *layout) space(previous, current unit, original string) bool {
	a, b := previous.last, current.Kind
	if a == token.COMMENT || b == token.COMMENT {
		return true
	}
	tight := f.tight[current.Start] || f.unary[previous.Start]
	switch b {
	case token.COMMA, token.SEMICOLON, token.RPAREN, token.RBRACK, token.PERIOD, token.DCOLON, token.COLON, token.QUESTION:
		tight = true
	case token.LPAREN:
		tight = a == token.IDENT || a == token.FUNC || closing(a) || a == token.NOT
	case token.LBRACK:
		tight = a == token.IDENT || a == token.MAP || closing(a) || a == token.HASH || a == token.NOT
	case token.NOT:
		tight = a == token.IDENT || closing(a)
	case token.RBRACE:
		tight = a == token.LBRACE
	}
	switch a {
	case token.LPAREN, token.LBRACK, token.PERIOD, token.DCOLON, token.HASH:
		tight = true
	}
	if current.opaque && a == token.NOT {
		tight = true
	}
	if !tight {
		return true
	}
	if original != "" {
		// Separately written operators and raw identifiers must not merge.
		left, right := f.source[previous.Start:previous.End], f.source[current.Start:current.End]
		if len(left) <= 3 && len(right) <= 3 {
			tokens, _ := lexer.Lex([]byte(left + right))
			if len(tokens) == 0 || tokens[0].End > uint32(len(left)) {
				return true
			}
		}
	}
	return false
}

// Positions and lexical indexes change with whitespace. Everything describing
// syntax, including documentation and directive attachment, must remain equal.
func sameSyntax(a, b reflect.Value) bool {
	if a.Type() != b.Type() {
		return false
	}
	if a.Type() == reflect.TypeFor[ast.Base]() || a.Type() == reflect.TypeFor[ast.Span]() {
		return true
	}
	switch a.Kind() {
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return sameSyntax(a.Elem(), b.Elem())
	case reflect.Slice:
		if a.Len() != b.Len() || a.IsNil() != b.IsNil() {
			return false
		}
		for i := 0; i < a.Len(); i++ {
			if !sameSyntax(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			field := a.Type().Field(i).Name
			if a.Type() == reflect.TypeFor[ast.File]() && (field == "Tokens" || field == "LineStarts") || a.Type() == reflect.TypeFor[token.Token]() && (field == "Start" || field == "End") {
				continue
			}
			if !sameSyntax(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}
