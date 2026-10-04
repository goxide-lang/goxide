package frontend

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
)

// ConfigureSource runs before import discovery, macro collection, and type
// checking. It preserves offsets by padding replacements with whitespace.
// Disabled sources validate lexical token trees and the package/import prefix,
// without expanding macros or attempting full declaration/expression parsing.
func ConfigureSource(filename string, src []byte, features map[string]bool) ([]byte, bool, error) {
	out := append([]byte(nil), src...)
	fs := token.NewFileSet()
	file := fs.AddFile(filename, -1, len(src))
	var s scanner.Scanner
	var scanErr error
	s.Init(file, src, func(p token.Position, msg string) {
		if msg != "illegal character U+0023 '#'" && msg != "illegal character U+0024 '$'" && msg != "illegal character U+003F '?'" && scanErr == nil {
			scanErr = fmt.Errorf("%s: %s", p, msg)
		}
	}, scanner.ScanComments)
	type item struct {
		tok token.Token
		lit string
		off int
	}
	tokens := []item{}
	stack := []token.Token{}
	selected := true
	beforePackage := true
	for {
		p, t, l := s.Scan()
		if t == token.EOF {
			break
		}
		off := file.Offset(p)
		if t == token.COMMENT && strings.HasPrefix(l, "//hgo:cfg") {
			if !beforePackage {
				return nil, false, fmt.Errorf("%s: //hgo:cfg must precede package", fs.Position(p))
			}
			expr := strings.TrimSpace(strings.TrimPrefix(l, "//hgo:cfg"))
			v, e := evalCfg(expr, features)
			if e != nil {
				return nil, false, fmt.Errorf("%s: %w", fs.Position(p), e)
			}
			selected = selected && v
		}
		if t == token.PACKAGE {
			beforePackage = false
		}
		switch t {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			stack = append(stack, t)
		case token.RPAREN, token.RBRACK, token.RBRACE:
			want := map[token.Token]token.Token{token.RPAREN: token.LPAREN, token.RBRACK: token.LBRACK, token.RBRACE: token.LBRACE}[t]
			if len(stack) == 0 || stack[len(stack)-1] != want {
				return nil, false, fmt.Errorf("%s: unbalanced conditional source", fs.Position(p))
			}
			stack = stack[:len(stack)-1]
		}
		if t != token.COMMENT {
			tokens = append(tokens, item{t, l, off})
		}
	}
	if scanErr != nil {
		return nil, false, scanErr
	}
	if len(stack) != 0 {
		return nil, false, fmt.Errorf("%s: unclosed delimiter in conditional source", filename)
	}
	if !selected {
		// Reuse the hgo import-prefix parser: it stops before DSL declarations
		// and validates syntax without loading packages or resolving names.
		// The scanner and delimiter checks above cover the entire token tree.
		// Do not inspect macro templates/uses/cfg! in code that is removed.
		if _, e := parseSourceImports(filename, src); e != nil {
			return nil, false, fmt.Errorf("disabled cfg file package/import syntax: %w", e)
		}
		return out, false, nil
	}
	for i := 0; i < len(tokens); i++ {
		if tokens[i].tok != token.IDENT || tokens[i].lit != "use" || i+3 >= len(tokens) || tokens[i+1].tok != token.IDENT || tokens[i+2].tok != token.COLON || tokens[i+3].tok != token.COLON {
			continue
		}
		j := i + 1
		for j < len(tokens) && tokens[j].tok != token.SEMICOLON {
			j++
		}
		if j > i+1 && tokens[j-1].lit == "cfg" {
			return nil, false, fmt.Errorf("%s: cfg is a reserved project binding name", fs.Position(file.Pos(tokens[j-1].off)))
		}
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].lit == "macro_rules" && tokens[i+1].tok == token.NOT && tokens[i+2].lit == "cfg" {
			return nil, false, fmt.Errorf("%s: cfg is a reserved project macro name", fs.Position(file.Pos(tokens[i+2].off)))
		}
		if i > 0 && tokens[i-1].tok == token.PERIOD {
			continue
		}
		if tokens[i].tok != token.IDENT || tokens[i].lit != "cfg" || tokens[i+1].tok != token.NOT || tokens[i+2].tok != token.LPAREN {
			continue
		}
		j, depth := i+3, 1
		for ; j < len(tokens); j++ {
			if tokens[j].tok == token.LPAREN {
				depth++
			}
			if tokens[j].tok == token.RPAREN {
				depth--
			}
			if depth == 0 {
				break
			}
		}
		if j == len(tokens) {
			return nil, false, fmt.Errorf("%s: unterminated cfg!", filename)
		}
		v, e := evalCfg(string(src[tokens[i+2].off+1:tokens[j].off]), features)
		if e != nil {
			return nil, false, fmt.Errorf("%s: %w", filename, e)
		}
		a, b := tokens[i].off, tokens[j].off+1
		if strings.ContainsAny(string(src[a:b]), "\r\n") {
			return nil, false, fmt.Errorf("%s: cfg! predicate must be on one line in v1", fs.Position(file.Pos(a)))
		}
		for k := a; k < b; k++ {
			out[k] = ' '
		}
		value := "false"
		if v {
			value = "true"
		}
		copy(out[a:b], value)

		i = j
	}

	return out, selected, nil
}
func evalCfg(text string, features map[string]bool) (bool, error) {
	expr, e := parser.ParseExpr(text)
	if e != nil {
		return false, fmt.Errorf("invalid cfg predicate: %w", e)
	}
	var eval func(ast.Expr) (bool, error)
	eval = func(x ast.Expr) (bool, error) {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return false, fmt.Errorf("cfg expects feature(\"name\"), all(...), any(...), or not(...)")
		}
		name, ok := call.Fun.(*ast.Ident)
		if !ok || call.Ellipsis.IsValid() {
			return false, fmt.Errorf("invalid cfg predicate")
		}
		if name.Name == "feature" {
			if len(call.Args) != 1 {
				return false, fmt.Errorf("feature expects one string")
			}
			s, ok := call.Args[0].(*ast.BasicLit)
			if !ok || s.Kind != token.STRING {
				return false, fmt.Errorf("feature expects one string")
			}
			n, e := strconv.Unquote(s.Value)
			if e != nil {
				return false, e
			}
			v, ok := features[n]
			if !ok {
				return false, fmt.Errorf("unknown cfg feature %q", n)
			}
			return v, nil
		}
		if name.Name != "all" && name.Name != "any" && name.Name != "not" {
			return false, fmt.Errorf("unknown cfg predicate %s", name.Name)
		}
		if name.Name == "not" && len(call.Args) != 1 {
			return false, fmt.Errorf("not expects one predicate")
		}
		v := name.Name == "all"
		for _, a := range call.Args {
			q, e := eval(a)
			if e != nil {
				return false, e
			}
			switch name.Name {
			case "all":
				v = v && q
			case "any":
				v = v || q
			case "not":
				v = !q
			}
		}
		return v, nil
	}
	return eval(expr)
}
