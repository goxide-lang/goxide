package main

import (
	"sort"
	"strings"
	"unicode"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/lexer"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func featureNames(value string) []string {
	seen := map[string]bool{}
	for _, name := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
		seen[name] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nativeFeatures(value string) config.Features {
	features := config.Features{Enabled: map[string]bool{}}
	for _, name := range featureNames(value) {
		features.Enabled[name] = true
	}
	return features
}

// Partial generation leaves sibling files untouched. Conservatively detect
// configuration names in macro input too: an ident capture can produce either
// cfg! or #[cfg] without either spelling appearing literally in the source.
func sourceUsesConfiguration(source []byte) bool {
	configurationName := func(name string) bool { return name == "cfg" || name == "cfg_attr" }
	file, diagnostics := parser.ParseFile("", source)
	used := false
	ast.Walk(file, func(node ast.Node) bool {
		if used {
			return false
		}
		var path *ast.Path
		switch n := node.(type) {
		case *ast.Attr:
			path = n.Path
		case *ast.MacroExpr:
			path = n.Path
		case *ast.MacroType:
			path = n.Path
		case *ast.MacroItem:
			path = n.Path
		case *ast.TokenTree:
			for _, part := range n.Tokens {
				if part.Token != nil && part.Token.Kind == token.IDENT && configurationName(part.Token.Text) {
					used = true
				}
			}
		}
		if path != nil && len(path.Segments) == 1 && configurationName(path.Segments[0].Name.Name) {
			used = true
		}
		return !used
	})
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != parser.Error {
			continue
		}
		// An unselected malformed sibling remains outside compilation, but
		// parser recovery must not hide configuration behind that error.
		tokens, _ := lexer.Lex(source)
		for _, tok := range tokens {
			if tok.Kind == token.IDENT && configurationName(tok.Text) {
				return true
			}
		}
		break
	}
	return used
}
