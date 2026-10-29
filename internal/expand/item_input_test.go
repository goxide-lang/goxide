package expand

import (
	"reflect"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestProceduralItemInputPreservesUngroupedHelperArguments(t *testing.T) {
	var seen []string
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "keep", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output {
			return pm.Success(item)
		}},
		{Name: "Custom", Derive: func(_ pm.Context, item public.TokenStream) pm.Output {
			var visit func(public.TokenStream)
			visit = func(stream public.TokenStream) {
				for _, node := range stream.Tokens() {
					if node.Kind() != public.GroupKind {
						continue
					}
					children := node.Children().Tokens()
					if len(children) == 3 && children[0].Text() == "note" {
						if children[1].Kind() != public.PunctKind || children[1].Text() != "=" || children[2].Kind() != public.LiteralKind {
							t.Fatalf("helper arguments changed: %s", node.Children().Display())
						}
						seen = append(seen, children[2].Text())
					}
					visit(node.Children())
				}
			}
			visit(item)
			return pm.Success(public.New())
		}},
	}, map[string][]string{"Custom": {"note"}})
	file := expandFile(t, "helper_args.gox", `#[keep] #[derive(Custom)] #[note = "item"] type Value struct { #[note = "field"] Field int }`)
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil || !reflect.DeepEqual(seen, []string{`"item"`, `"field"`}) {
		t.Fatalf("helper arguments = %v, error=%v", seen, err)
	}
	ast.Walk(result.Files[0], func(node ast.Node) bool {
		if _, ok := node.(*ast.Attr); ok {
			t.Fatal("consumed helper remained in generated declaration")
		}
		return true
	})
}

func TestProceduralItemInputPreservesDefaultHelperKeyword(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "Custom", Derive: func(_ pm.Context, item public.TokenStream) pm.Output {
		called = true
		tokens := item.Tokens()
		if len(tokens) < 2 || tokens[1].Kind() != public.GroupKind {
			t.Fatalf("missing helper attribute: %s", item.Display())
		}
		name := tokens[1].Children().Tokens()
		if len(name) != 1 || name[0].Kind() != public.KeywordKind || name[0].Text() != "default" || name[0].Raw() {
			t.Fatalf("default helper lost its lexical kind: %s", item.Display())
		}
		return pm.Success(public.New())
	}}}, map[string][]string{"Custom": {"default"}})
	file := expandFile(t, "default_item.gox", "#[default] #[derive(Custom)] type Value int")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil || !called {
		t.Fatalf("derive called=%v, error=%v", called, err)
	}
	if len(result.Files[0].Items[0].(*ast.TypeDecl).Attrs) != 0 {
		t.Fatal("consumed helper remained on the output declaration")
	}
}

func TestProceduralItemInputPreservesForwardedCallSite(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "outer", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output {
			return pm.FromParse(public.ParseText("#[middle] #[last] type Value int", ctx.CallSite()))
		}},
		{Name: "middle", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output {
			return pm.Success(item)
		}},
		{Name: "last", Attribute: func(ctx pm.Context, _, _ public.TokenStream) pm.Output {
			return pm.FromParse(public.ParseText("const Result = callerOnly", ctx.CallSite()))
		}},
	}, nil)
	file := expandFile(t, "caller.gox", "const callerOnly = 42; #[outer] type Value int")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range result.Files[0].Items {
		declaration, ok := item.(*ast.ConstDecl)
		if !ok || declaration.Specs[0].Names[0].Name != "Result" {
			continue
		}
		id := declaration.Specs[0].Values[0].(*ast.Ident)
		if id.Name != "callerOnly" || result.Scopes[id].File != file {
			t.Fatal("forwarding the generated attribute changed its CallSite binding owner")
		}
		if result.Origins[id].File != resolver.macros["last"].File {
			t.Fatal("CallSite binding overwrote the generated token's source origin")
		}
		return
	}
	t.Fatal("last attribute did not emit Result")
}
