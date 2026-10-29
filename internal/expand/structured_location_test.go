package expand

import (
	"reflect"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func TestNestedFragmentLocationSurvivesTokenTreeForwarding(t *testing.T) {
	for _, kind := range []fragmentKind{exprKind, ttKind} {
		t.Run(string(kind), func(t *testing.T) {
			source, display := &ast.File{}, &ast.File{}
			e := &engine{r: &Result{
				Origins:  map[ast.Node]Origin{},
				Scopes:   map[*ast.Ident]Scope{},
				Contexts: map[ast.SyntaxCtx]Context{},
			}, trees: map[*ast.TokenTree][]syntaxToken{}}
			text := "inner!(missing)"
			if kind == ttKind {
				text = "(" + text + ")"
			}
			input := e.treeInput(matcherTree(t, text), source, 7)
			original, err := e.parseFragment(exprKind, input)
			if err != nil {
				t.Fatal(err)
			}
			captured := &fragment{Kind: kind, Node: original.Node, Tokens: original.Tokens}

			var locations func([]syntaxToken) []Origin
			locations = func(tokens []syntaxToken) []Origin {
				var result []Origin
				for _, tok := range tokens {
					result = append(result, tok.Origin, tok.Location)
					if tok.Tree != nil {
						result = append(result, tok.Tree.Origin, tok.Tree.Location, tok.Tree.Open, tok.Tree.Close)
						result = append(result, locations(tok.Tree.Tokens)...)
					}
					if tok.Fragment != nil {
						result = append(result, locations(tok.Fragment.Tokens)...)
					}
				}
				return result
			}
			inputLocations := locations(input)
			treeLocations := map[*ast.TokenTree][]Origin{}
			for tree, tokens := range e.trees {
				treeLocations[tree] = locations(tokens)
			}
			contexts := map[*token.Token]TokenContext{}
			for tok, context := range e.r.TokenContexts {
				contexts[tok] = context
			}

			overrides := []Origin{
				{display, ast.Span{Start: 100, End: 110}},
				{display, ast.Span{Start: 200, End: 210}},
			}
			var copies []*ast.MacroExpr
			for i, location := range overrides {
				out, err := e.parse([]syntaxToken{{
					Fragment: captured, Origin: Origin{source, original.Node.Span()}, Location: location,
				}}, exprKind, ast.ExpnID(i+1), Scope{source, 7})
				if err != nil {
					t.Fatal(err)
				}
				if paren, ok := out.(*ast.ParenExpr); ok {
					out = paren.X
				}
				copies = append(copies, out.(*ast.MacroExpr))
			}
			if copies[0] == copies[1] || copies[0].Args == copies[1].Args ||
				copies[0].Args.Tokens[0].Token == copies[1].Args.Tokens[0].Token {
				t.Fatal("repeated fragments share AST or materialized token pointers")
			}
			for i, copy := range copies {
				location := overrides[i]
				if e.location(copy) != location || e.location(copy.Args) != location {
					t.Fatalf("copy %d lost the fragment display position", i)
				}
				tokens := e.treeInput(copy.Args, source, 7)
				if len(tokens) != 1 || tokenLocation(tokens[0]) != location {
					t.Fatalf("copy %d cached input reverted to its old display position: %#v", i, tokens)
				}
				context := e.r.TokenContexts[copy.Args.Tokens[0].Token]
				if context.Location != location || context.Origin.File != source || context.Ctx != 7 ||
					context.Scope != (Scope{source, 7}) {
					t.Fatalf("copy %d materialized token lost position or semantic identity: %#v", i, context)
				}
				nested, err := e.parse(tokens, exprKind, ast.ExpnID(i+3), Scope{source, 7})
				if err != nil {
					t.Fatal(err)
				}
				name := nested.(*ast.Ident)
				if name.Name != "missing" || name.Ctx != 7 || e.r.Scopes[name] != (Scope{source, 7}) ||
					e.r.Origins[name].File != source || e.location(name) != location {
					t.Fatalf("copy %d forwarded name changed source identity or lost display position", i)
				}
			}
			if !reflect.DeepEqual(locations(input), inputLocations) {
				t.Fatal("redirecting a fragment changed the original structured input")
			}
			for tree, want := range treeLocations {
				if !reflect.DeepEqual(locations(e.trees[tree]), want) {
					t.Fatal("redirecting a fragment changed an original token-tree cache")
				}
			}
			for tok, want := range contexts {
				if e.r.TokenContexts[tok] != want {
					t.Fatal("redirecting a fragment changed an original token context")
				}
			}
		})
	}
}
