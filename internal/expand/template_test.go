package expand

import (
	"errors"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/token"
)

func TestTemplateMultipleCaptures(t *testing.T) {
	file, tree := templateFixture(t, `$($key => $value),*`)
	keys, values := templateList("first", "second"), templateList("one", "two")
	out, err := renderTemplate(tree, captures{"key": keys, "value": values}, file, 12, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 7 || out[0].Fragment != keys.Values[0].Leaf || out[2].Fragment != values.Values[0].Leaf || out[4].Fragment != keys.Values[1].Leaf || out[6].Fragment != values.Values[1].Leaf {
		t.Fatalf("captures were not expanded together: %#v", out)
	}
	if out[1].Token.Kind != token.FATARROW || out[3].Token.Kind != token.COMMA || out[5].Token.Kind != token.FATARROW {
		t.Fatalf("template literals or separator lost: %#v", out)
	}
	for _, item := range out {
		if item.Origin.File != file || item.Origin.Span.Start >= item.Origin.Span.End {
			t.Fatalf("missing template origin: %#v", item.Origin)
		}
	}
}

func TestTemplateNestedRepetitions(t *testing.T) {
	file, tree := templateFixture(t, `$([$($x),*]);*`)
	rows := &capture{Depth: 2, Values: []*capture{templateList("a", "b"), templateList("c")}}
	out, err := renderTemplate(tree, captures{"x": rows}, file, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[1].Token.Kind != token.SEMICOLON || out[0].Tree.Delim != ast.Bracket || out[2].Tree.Delim != ast.Bracket {
		t.Fatalf("unexpected outer repetition: %#v", out)
	}
	first, second := out[0].Tree.Tokens, out[2].Tree.Tokens
	if len(first) != 3 || first[0].Fragment != rows.Values[0].Values[0].Leaf || first[2].Fragment != rows.Values[0].Values[1].Leaf || len(second) != 1 || second[0].Fragment != rows.Values[1].Values[0].Leaf {
		t.Fatalf("nested repetition did not keep row positions: %#v %#v", first, second)
	}
}

func TestTemplateRepetitionCardinality(t *testing.T) {
	for _, test := range []struct {
		name, body string
		count      int
		wantError  bool
	}{
		{"star zero", `$($x),*`, 0, false},
		{"star many", `$($x),*`, 2, false},
		{"plus one", `$($x),+`, 1, false},
		{"plus zero", `$($x),+`, 0, true},
		{"optional zero", `$($x)?`, 0, false},
		{"optional one", `$($x)?`, 1, false},
		{"optional many", `$($x)?`, 2, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, tree := templateFixture(t, test.body)
			c := templateList(make([]string, test.count)...)
			out, err := renderTemplate(tree, captures{"x": c}, file, 1, 0, nil)
			if test.wantError {
				var issue *templateError
				if !errors.As(err, &issue) || issue.Code != "repetition-length" {
					t.Fatalf("expected cardinality error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.count == 0 && len(out) != 0 || test.count > 0 && out[0].Fragment == nil {
				t.Fatalf("unexpected output: %#v", out)
			}
		})
	}
}

func TestTemplateRepetitionLengthMismatch(t *testing.T) {
	file, tree := templateFixture(t, `$($key => $value),*`)
	_, err := renderTemplate(tree, captures{"key": templateList("a", "b"), "value": templateList("c")}, file, 1, 0, nil)
	var issue *templateError
	if !errors.As(err, &issue) || issue.Code != "repetition-length" || issue.Span.Start >= issue.Span.End {
		t.Fatalf("expected located length mismatch, got %v", err)
	}
}

func TestTemplateNestedLengthMismatch(t *testing.T) {
	file, tree := templateFixture(t, `$([$($key => $value),*]);*`)
	keys := &capture{Depth: 2, Values: []*capture{templateList("a"), templateList("b", "c")}}
	values := &capture{Depth: 2, Values: []*capture{templateList("one"), templateList("two")}}
	_, err := renderTemplate(tree, captures{"key": keys, "value": values}, file, 1, 0, nil)
	var issue *templateError
	if !errors.As(err, &issue) || issue.Code != "repetition-length" {
		t.Fatalf("expected inner length mismatch, got %v", err)
	}
}

func TestTemplateCaptureDepth(t *testing.T) {
	for _, test := range []struct {
		name, body string
		caps       captures
	}{
		{"repeated outside loop", `$x`, captures{"x": templateList("a")}},
		{"scalar inside loop", `$($x)*`, captures{"x": templateLeaf("a")}},
		{"empty repeated at wrong depth", `$($x)*`, captures{"x": {Depth: 2}}},
		{"loop without capture", `$(,)?`, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, tree := templateFixture(t, test.body)
			_, err := renderTemplate(tree, test.caps, file, 1, 0, nil)
			var issue *templateError
			if !errors.As(err, &issue) || issue.Code != "repetition-depth" {
				t.Fatalf("expected depth error, got %v", err)
			}
		})
	}
}

func TestTemplateCallerEscapeAndContexts(t *testing.T) {
	file, tree := templateFixture(t, `$$ $caller::helpers[Local]::f($x, Local)`)
	x := templateLeaf("Local")
	x.Leaf.Node.(*ast.Ident).Ctx = 41
	out, err := renderTemplate(tree, captures{"x": x}, file, 19, 7, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 6 || out[0].Token.Kind != token.DOLLAR || out[1].Token.Text != "helpers" || out[4].Token.Text != "f" {
		t.Fatalf("unexpected $caller expansion: %#v", out)
	}
	if out[1].Ctx != 7 || out[4].Ctx != 7 || out[2].Tree.Tokens[0].Ctx != 19 || out[5].Tree.Tokens[2].Ctx != 19 {
		t.Fatalf("caller/template contexts mixed: %#v", out)
	}
	fragment := out[5].Tree.Tokens[0].Fragment
	if fragment != x.Leaf || fragment.Node.(*ast.Ident).Ctx != 41 || fragment.Node.(*ast.Ident).Name != "Local" {
		t.Fatalf("captured identity or spelling changed: %#v", fragment)
	}
	second, err := renderTemplate(tree, captures{"x": x}, file, 20, 8, nil)
	if err != nil || second[5].Tree.Tokens[2].Ctx != 20 || out[5].Tree.Tokens[2].Ctx != 19 {
		t.Fatalf("render changed an earlier result: %v", err)
	}
}

func TestTemplateOpaqueCategories(t *testing.T) {
	file, tree := templateFixture(t, `$T $item`)
	typeNode, typeErrors := parser.ParseType([]byte("[]int"))
	itemNode, itemErrors := parser.ParseItem([]byte("type Local int"))
	if len(typeErrors) != 0 || len(itemErrors) != 0 {
		t.Fatalf("invalid fragments: %v %v", typeErrors, itemErrors)
	}
	typeCapture := &capture{Leaf: &fragment{Kind: typeKind, Node: typeNode}}
	itemCapture := &capture{Leaf: &fragment{Kind: itemKind, Node: itemNode}}
	out, err := renderTemplate(tree, captures{"T": typeCapture, "item": itemCapture}, file, 1, 0, nil)
	if err != nil || len(out) != 2 || out[0].Fragment != typeCapture.Leaf || out[1].Fragment != itemCapture.Leaf {
		t.Fatalf("structured fragments were changed: %#v %v", out, err)
	}
}

func TestTemplateInvalidSyntax(t *testing.T) {
	for _, body := range []string{`$`, `$1`, `$missing`, `$caller`, `$caller::`, `$caller::1`, `$[x]*`, `$(x)`} {
		t.Run(body, func(t *testing.T) {
			file, tree := templateFixture(t, body)
			_, err := renderTemplate(tree, nil, file, 1, 0, nil)
			var issue *templateError
			if !errors.As(err, &issue) || issue.Code != "template" || issue.Span.Start >= issue.Span.End {
				t.Fatalf("expected located template error, got %v", err)
			}
		})
	}
}

func TestTemplateBudget(t *testing.T) {
	file, tree := templateFixture(t, `$($x),*`)
	remaining := 2
	want := errors.New("output budget exceeded")
	out, err := renderTemplate(tree, captures{"x": templateList("a", "b", "c")}, file, 1, 0, func() error {
		if remaining == 0 {
			return want
		}
		remaining--
		return nil
	})
	if !errors.Is(err, want) || out != nil || remaining != 0 {
		t.Fatalf("budget did not stop output: %#v %v", out, err)
	}
}

func templateFixture(t *testing.T, body string) (*ast.File, *ast.TokenTree) {
	t.Helper()
	source := "package p\nmacro_rules! example { () => { " + body + " }; }\n"
	file, diagnostics := parser.ParseFile("template.gox", []byte(source))
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			t.Fatalf("parse template %s: %v", strings.TrimSpace(body), diagnostics)
		}
	}
	return file, file.Items[0].(*ast.MacroRulesDecl).Rules[0].Template
}

func templateLeaf(name string) *capture {
	return &capture{Leaf: &fragment{Kind: exprKind, Node: &ast.Ident{Name: name}}}
}

func templateList(names ...string) *capture {
	c := &capture{Depth: 1}
	for _, name := range names {
		c.Values = append(c.Values, templateLeaf(name))
	}
	return c
}
