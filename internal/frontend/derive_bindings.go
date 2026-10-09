package frontend

import (
	"go/token"

	"github.com/hxsf/goxide/internal/paths"
)

// localDeclarationBindings records package-level source names before DSL
// lowering. It is only a lexical shadow/conflict table, not type metadata or a
// substitute for Go identity checking. Native Go declarations share the same
// package scope; native imports and method names do not.
func localDeclarationBindings(owner string, sources map[string][]byte) map[string]paths.Binding {
	bindings := map[string]paths.Binding{}
	add := func(name, kind string) {
		if name != "_" && name != "init" {
			bindings[name] = paths.Binding{Kind: kind, OwnerPath: owner, Name: name, RoutePath: owner, RouteName: name}
		}
	}
	for _, file := range sortedSources(sources) {
		ts := pathTokens(file, sources[file])
		depth := 0
		afterPrefix := false
		for i := 0; i < len(ts); i++ {
			t := ts[i]
			start := afterPrefix || i == 0 || ts[i-1].kind == token.SEMICOLON || ts[i-1].kind == token.RBRACE
			afterPrefix = false
			// Attributes and visibility can precede declarations on the same
			// line; neither is a declaration or a new lexical scope.
			if depth == 0 && start && t.text == "#" && i+1 < len(ts) && ts[i+1].kind == token.LBRACK {
				brackets := 1
				for i += 2; i < len(ts); i++ {
					if ts[i].kind == token.LBRACK {
						brackets++
					} else if ts[i].kind == token.RBRACK {
						brackets--
						if brackets == 0 {
							break
						}
					}
				}
				afterPrefix = true
				continue
			}
			if depth == 0 && start && t.text == "pub" {
				afterPrefix = true
				continue
			}
			if depth == 0 && start {
				kind := ""
				switch {
				case t.kind == token.TYPE || t.kind == token.STRUCT || t.text == "enum":
					kind = "type"
				case t.kind == token.VAR:
					kind = "variable"
				case t.kind == token.CONST:
					kind = "constant"
				case t.kind == token.FUNC:
					if i+1 < len(ts) && ts[i+1].kind == token.IDENT {
						add(ts[i+1].text, "function")
					}
				case t.text == "macro_rules":
					if i+2 < len(ts) && ts[i+1].kind == token.NOT && ts[i+2].kind == token.IDENT {
						add(ts[i+2].text, "macro")
					}
				}
				if kind != "" && i+1 < len(ts) {
					if ts[i+1].kind == token.LPAREN {
						i = declarationGroupNames(ts, i+1, kind, add)
						continue
					}
					declarationNames(ts, i+1, kind, add)
				}
			}
			switch t.kind {
			case token.LPAREN, token.LBRACK, token.LBRACE:
				depth++
			case token.RPAREN, token.RBRACK, token.RBRACE:
				depth--
			}
		}
	}
	return bindings
}

func declarationNames(ts []useToken, start int, kind string, add func(string, string)) {
	for i := start; i < len(ts) && ts[i].kind == token.IDENT; i += 2 {
		add(ts[i].text, kind)
		if kind == "type" || i+1 >= len(ts) || ts[i+1].kind != token.COMMA {
			break
		}
	}
}

func declarationGroupNames(ts []useToken, start int, kind string, add func(string, string)) int {
	depth, atName := 1, true
	for i := start + 1; i < len(ts); i++ {
		if depth == 1 && atName && ts[i].kind == token.IDENT {
			declarationNames(ts, i, kind, add)
			atName = false
		}
		switch ts[i].kind {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
			if depth == 0 {
				return i
			}
		case token.SEMICOLON:
			if depth == 1 {
				atName = true
			}
		}
	}
	return len(ts)
}
