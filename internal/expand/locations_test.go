package expand

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func TestCopiesKeepIndependentDiagnosticLocations(t *testing.T) {
	source, display := &ast.File{}, &ast.File{}
	origin := Origin{source, ast.Span{Start: 10, End: 20}}
	location := Origin{display, ast.Span{Start: 50, End: 60}}
	tok := &token.Token{Kind: token.IDENT, Text: "name", Start: 11, End: 15}
	tree := &ast.TokenTree{Base: ast.Base{Loc: origin.Span}, Delim: ast.Paren, Tokens: []ast.TokenOrTree{{Token: tok}}}
	context := TokenContext{Ctx: 7, Scope: Scope{source, 7}, Origin: origin, Location: location}
	e := &engine{r: &Result{
		Origins:       map[ast.Node]Origin{tree: origin},
		Locations:     map[ast.Node]Origin{tree: location},
		TokenContexts: map[*token.Token]TokenContext{tok: context},
	}}
	first := e.copyNode(tree, 3).(*ast.TokenTree)
	second := e.copyNode(tree, 4).(*ast.TokenTree)
	if first == tree || first == second || first.Tokens[0].Token == second.Tokens[0].Token {
		t.Fatal("copies share AST nodes or token pointers")
	}
	for _, copy := range []*ast.TokenTree{first, second} {
		if e.r.Origins[copy] != origin || e.location(copy) != location || copy.Span() != origin.Span {
			t.Fatal("copy changed semantic source or lost diagnostic position")
		}
		if e.r.TokenContexts[copy.Tokens[0].Token] != context {
			t.Fatal("copy lost token identity or diagnostic position")
		}
	}
	e.r.Locations[first] = origin
	e.r.TokenContexts[first.Tokens[0].Token] = TokenContext{Location: origin}
	if e.location(tree) != location || e.location(second) != location ||
		e.r.TokenContexts[tok] != context || e.r.TokenContexts[second.Tokens[0].Token] != context {
		t.Fatal("changing one copy affected another copy or its input")
	}
	delete(e.r.Locations, first)
	if e.location(first) != origin {
		t.Fatal("missing diagnostic override did not fall back to semantic origin")
	}
}

func TestTreeInputPreservesTokenIdentityAndDisplayBoundaries(t *testing.T) {
	source, display := &ast.File{}, &ast.File{}
	origin := Origin{source, ast.Span{Start: 10, End: 20}}
	location := Origin{display, ast.Span{Start: 50, End: 60}}
	tok := &token.Token{Kind: token.IDENT, Text: "name", Start: 11, End: 15}
	child := &ast.TokenTree{Base: ast.Base{Loc: origin.Span}, Delim: ast.Paren, Tokens: []ast.TokenOrTree{{Token: tok}}}
	tree := &ast.TokenTree{Tokens: []ast.TokenOrTree{{Tree: child}}}
	context := TokenContext{Ctx: 7, Scope: Scope{source, 7}, Origin: origin, Location: location}
	e := &engine{r: &Result{
		Origins:       map[ast.Node]Origin{child: origin},
		Locations:     map[ast.Node]Origin{child: location},
		TokenContexts: map[*token.Token]TokenContext{tok: context},
	}}
	group := e.treeInput(tree, source, 2)[0]
	if group.Origin != origin || group.Location != location || group.Tree.Origin != origin || group.Tree.Location != location {
		t.Fatal("group display position replaced its semantic source")
	}
	if group.Tree.Open != (Origin{display, ast.Span{Start: 50, End: 51}}) ||
		group.Tree.Close != (Origin{display, ast.Span{Start: 59, End: 60}}) {
		t.Fatalf("delimiter positions = %#v / %#v", group.Tree.Open, group.Tree.Close)
	}
	value := group.Tree.Tokens[0]
	if value.Origin != origin || value.Location != location || value.Ctx != context.Ctx || value.Scope != context.Scope {
		t.Fatal("token wrapping lost source identity or display position")
	}
	delete(e.r.Locations, child)
	delete(e.r.TokenContexts, tok)
	group = e.treeInput(tree, source, 2)[0]
	if group.Location != origin || group.Tree.Open.Span != (ast.Span{Start: 10, End: 11}) ||
		group.Tree.Close.Span != (ast.Span{Start: 19, End: 20}) {
		t.Fatal("source group did not fall back to original delimiter positions")
	}
}

func TestConfigurationLocationDoesNotChangeFeatureOwner(t *testing.T) {
	source := expandFile(t, "definition.gox", `var value = cfg!(feature("enabled"))`)
	display := &ast.File{}
	n := expandValue(t, source, "value").(*ast.MacroExpr)
	n.Expn = 7
	origin := Origin{source, n.Span()}
	location := Origin{display, ast.Span{Start: 50, End: 60}}
	call := Origin{display, ast.Span{Start: 70, End: 80}}
	e := &engine{
		r: &Result{
			Origins:    map[ast.Node]Origin{n: origin},
			Locations:  map[ast.Node]Origin{n: location},
			Expansions: map[ast.ExpnID]Expansion{7: {Call: call}},
		},
		configs: map[*ast.File]config.Config{
			source:  {Features: config.Features{Enabled: map[string]bool{"enabled": true}}},
			display: {Features: config.Features{Enabled: map[string]bool{"enabled": false}}},
		},
	}
	out, handled, err := e.cfg(n, n.Args, exprKind, display, 7)
	if err != nil || !handled || !out.(*ast.BoolLit).Value {
		t.Fatalf("cfg read the diagnostic file's features: output=%#v, error=%v", out, err)
	}
	if e.r.Origins[out] != origin || e.location(out) != location || out.Span() != n.Span() {
		t.Fatal("cfg replacement lost diagnostic position or changed semantic origin")
	}
	_, _, err = e.cfg(n, n.Args, typeKind, display, 7)
	d, ok := err.(*Diagnostic)
	if !ok || d.Location != location || d.Definition != origin || d.Call != call {
		t.Fatalf("cfg diagnostic mixed display position and expansion stack: %#v", err)
	}
}

func TestConfigurationAttributeKeepsTokenLocations(t *testing.T) {
	source := expandFile(t, "definition.gox", `#[cfg_attr(all(), outer::inner = "value")] struct Value {}`)
	display := &ast.File{}
	parent := source.Items[0].(*ast.TypeDecl).Attrs[0]
	arguments, err := configArguments(parent.Args.Tokens)
	if err != nil {
		t.Fatal(err)
	}
	e := &engine{r: &Result{
		Origins:       map[ast.Node]Origin{parent: {source, parent.Span()}},
		Locations:     map[ast.Node]Origin{},
		Scopes:        map[*ast.Ident]Scope{},
		TokenContexts: map[*token.Token]TokenContext{},
	}}
	var contexts []TokenContext
	for _, part := range arguments[1] {
		if tok := part.Token; tok != nil && tok.Kind == token.IDENT {
			context := TokenContext{
				Ctx: 7, Scope: Scope{source, 7},
				Origin:   Origin{source, ast.Span{Start: tok.Start, End: tok.End}},
				Location: Origin{display, ast.Span{Start: uint32(50 + len(contexts)), End: uint32(51 + len(contexts))}},
			}
			contexts = append(contexts, context)
			e.r.TokenContexts[tok] = context
		}
	}
	attr, err := e.selection(source).attribute(arguments[1], parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(attr.Path.Segments) != 2 || len(contexts) != 2 {
		t.Fatal("attribute path did not retain both segments")
	}
	for i, segment := range attr.Path.Segments {
		context := contexts[i]
		if segment.Name.Ctx != context.Ctx || e.r.Scopes[segment.Name] != context.Scope ||
			e.r.Origins[segment.Name] != context.Origin || e.location(segment.Name) != context.Location ||
			e.r.Origins[segment] != context.Origin || e.location(segment) != context.Location {
			t.Fatalf("attribute segment %d lost identity or display position", i)
		}
	}
	if e.location(attr) != contexts[0].Location || e.location(attr.Path) != contexts[0].Location ||
		e.location(attr.Args) != contexts[0].Location {
		t.Fatal("attribute wrappers lost the first token's diagnostic position")
	}
}
