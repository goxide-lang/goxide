package expand

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func matcherTree(t *testing.T, source string) *ast.TokenTree {
	t.Helper()
	n, ds := parser.ParseExpr([]byte("m!(" + source + ")"))
	for _, d := range ds {
		if d.Severity == parser.Error {
			t.Fatalf("parse token tree %q: %v", source, d)
		}
	}
	return n.(*ast.MacroExpr).Args
}

func matcherTokens(tree *ast.TokenTree) []syntaxToken {
	out := make([]syntaxToken, len(tree.Tokens))
	for i, t := range tree.Tokens {
		out[i].Token = t.Token
		if t.Tree != nil {
			out[i].Tree = &syntaxTree{Delim: t.Tree.Delim, Tokens: matcherTokens(t.Tree), Origin: Origin{Span: t.Tree.Span()}}
		}
	}
	return out
}

func matcherParser() fragmentParser {
	e := &engine{
		r: &Result{
			Origins:  make(map[ast.Node]Origin),
			Scopes:   make(map[*ast.Ident]Scope),
			Contexts: make(map[ast.SyntaxCtx]Context),
		},
		trees: make(map[*ast.TokenTree][]syntaxToken),
	}
	return e.parseFragment
}

func runMatcher(t *testing.T, pattern, input string) (captures, error) {
	t.Helper()
	m, err := compileMatcher(matcherTree(t, pattern))
	if err != nil {
		t.Fatal(err)
	}
	return m.match(matcherTokens(matcherTree(t, input)), matcherParser(), nil)
}

func TestMatcherFragmentCategories(t *testing.T) {
	tests := []struct {
		kind  fragmentKind
		input string
		valid bool
	}{
		{exprKind, "a + 2", true},
		{exprKind, "type T int", false},
		{identKind, "r#type", true},
		{identKind, "12", false},
		{ttKind, "(a + 2)", true},
		{ttKind, "a b", false},
		{typeKind, "map[string][]int", true},
		{typeKind, "1 + 2", false},
		{itemKind, "func f() {}", true},
		{itemKind, "a + 2", false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.kind, tt.input), func(t *testing.T) {
			c, err := runMatcher(t, "$x:"+string(tt.kind), tt.input)
			if (err == nil) != tt.valid {
				t.Fatalf("valid = %v, error = %v", tt.valid, err)
			}
			if tt.valid && (c["x"].Leaf.Kind != tt.kind || len(c["x"].Leaf.Tokens) == 0) {
				t.Fatalf("capture lost category or input: %#v", c["x"])
			}
			if !tt.valid {
				var failure *matchError
				if !errors.As(err, &failure) || failure.Code != "category" && failure.Code != "unconsumed" {
					t.Fatalf("expected category or consumption diagnosis, got %v", err)
				}
			}
		})
	}
}

func TestMatcherUnannotatedCaptureDefaultsToExpression(t *testing.T) {
	c, err := runMatcher(t, "$x", "a + 2")
	if err != nil || c["x"].Leaf.Kind != exprKind {
		t.Fatalf("unannotated expression capture = %#v, %v", c, err)
	}
	if _, err := runMatcher(t, "$x", "type X int"); err == nil {
		t.Fatal("unannotated capture accepted a declaration")
	}
}

func TestMatcherLiteralTreesAndDollarEscape(t *testing.T) {
	for _, input := range []string{"tag $$ [name]", "tag $ (name)", "tag $ [other]"} {
		_, err := runMatcher(t, "tag $$ [name]", input)
		if err == nil {
			t.Fatalf("unexpected match: %s", input)
		}
	}
	if _, err := runMatcher(t, "tag $$ [name]", "tag $ [name]"); err != nil {
		t.Fatal(err)
	}
}

func TestMatcherMultipleCapturesRepeatSynchronously(t *testing.T) {
	c, err := runMatcher(t, "$($k:expr => $v:expr),* $(,)?", "a => 1, b => 2,")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"k", "v"} {
		if c[name].Depth != 1 || len(c[name].Values) != 2 {
			t.Fatalf("%s repetition = %#v", name, c[name])
		}
	}
	if got := c["v"].Values[1].Leaf.Tokens[0].Token.Text; got != "2" {
		t.Fatalf("second value = %q", got)
	}
	if _, err := runMatcher(t, "$($k:expr => $v:expr),*", "a => 1,"); err == nil {
		t.Fatal("accepted implicit trailing comma")
	}
	if _, err := runMatcher(t, "$($k:expr => $v:expr),*", "a => 1, b =>"); err == nil {
		t.Fatal("accepted incomplete repeated pair")
	}
}

func TestMatcherNestedAndEmptyRepetitions(t *testing.T) {
	c, err := runMatcher(t, "$(( $($x:ident),* ));*", "(a,b);();(c)")
	if err != nil {
		t.Fatal(err)
	}
	x := c["x"]
	if x.Depth != 2 || len(x.Values) != 3 {
		t.Fatalf("nested capture = %#v", x)
	}
	for i, count := range []int{2, 0, 1} {
		if x.Values[i].Depth != 1 || len(x.Values[i].Values) != count {
			t.Fatalf("nested capture %d = %#v", i, x.Values[i])
		}
	}
	c, err = runMatcher(t, "$($x:ident),*", "")
	if err != nil || c["x"].Depth != 1 || len(c["x"].Values) != 0 {
		t.Fatalf("empty repeat = %#v, %v", c, err)
	}
}

func TestMatcherPlusAndOptional(t *testing.T) {
	for _, tt := range []struct {
		pattern, input string
		valid          bool
	}{
		{"$($x:ident),+", "", false},
		{"$($x:ident),+", "a,b", true},
		{"$($x:ident)?", "", true},
		{"$($x:ident)?", "a", true},
		{"$($x:ident)?", "a b", false},
	} {
		_, err := runMatcher(t, tt.pattern, tt.input)
		if (err == nil) != tt.valid {
			t.Errorf("%s / %q: %v", tt.pattern, tt.input, err)
		}
	}
}

func TestMatcherCompleteMatchBacktracks(t *testing.T) {
	c, err := runMatcher(t, "$($x:ident),* , tail", "a,b,tail")
	if err != nil || len(c["x"].Values) != 2 {
		t.Fatalf("backtracking = %#v, %v", c, err)
	}
	_, err = runMatcher(t, "ok", "ok extra")
	var failure *matchError
	if !errors.As(err, &failure) || failure.Code != "unconsumed" {
		t.Fatalf("expected unconsumed diagnostic, got %v", err)
	}
}

func TestMatcherCapturePreservesOpaqueFragment(t *testing.T) {
	m, err := compileMatcher(matcherTree(t, "$x:expr"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fragment{Kind: exprKind, Node: &ast.PathExpr{Path: &ast.Path{Segments: []*ast.PathSegment{{Name: &ast.Ident{Name: "local", Ctx: 71}}}}}}
	c, err := m.match([]syntaxToken{{Fragment: f}}, matcherParser(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if c["x"].Leaf != f {
		t.Fatal("forwarded fragment identity changed")
	}
}

func TestMatcherBudgetStopsBacktracking(t *testing.T) {
	m, err := compileMatcher(matcherTree(t, "$($x:ident)* tail"))
	if err != nil {
		t.Fatal(err)
	}
	remaining := 5
	_, err = m.match(matcherTokens(matcherTree(t, "a b c d")), matcherParser(), func() error {
		remaining--
		if remaining == 0 {
			return errors.New("matching budget exceeded")
		}
		return nil
	})
	var failure *matchError
	if !errors.As(err, &failure) || failure.Code != "budget" {
		t.Fatalf("expected budget diagnostic, got %v", err)
	}
}

func TestMatcherInvalidPatterns(t *testing.T) {
	for _, source := range []string{"$x:unknown", "$caller:ident", "$x:ident $x:expr", "$( $(,)? )*", "$x:", "$(a)"} {
		if _, err := compileMatcher(matcherTree(t, source)); err == nil {
			t.Errorf("accepted invalid matcher: %s", source)
		}
	}
}
