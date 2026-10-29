package resolve_test

import (
	"bytes"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestProceduralAttributeConstGroupsKeepOwnBindings(t *testing.T) {
	for _, mode := range []string{"copied", "rebuilt", "captured_const_name"} {
		t.Run(mode, func(t *testing.T) { checkProceduralConstGroupBindings(t, mode) })
	}
}

func checkProceduralConstGroupBindings(t *testing.T, mode string) {
	declaration := "func original[T any,const N int](value [N]T) [N]T { return value }"
	if mode == "rebuilt" {
		declaration = "func original[T any](value T) T { return value }"
	}
	files := parseFiles(t, map[string]string{
		"macros/definition.gox": "package macros\npub func Copy() {}\n",
		"model/model.gox":       "package model; #[Copy] " + declaration,
	})
	definition := files["macros/definition.gox"]
	entry := pm.Entry{Name: "Copy", Attribute: func(ctx pm.Context, _, item public.TokenStream) pm.Output {
		var generics, element, length public.Token
		for _, node := range item.Tokens() {
			if node.Kind() == public.GroupKind && node.Delimiter() == public.Bracket {
				generics = node
				for _, parameter := range node.Children().Tokens() {
					switch parameter.Text() {
					case "T":
						element = parameter
					case "N":
						length = parameter
					}
				}
				break
			}
		}
		text := func(source string) public.TokenStream {
			stream, err := public.ParseText(source, ctx.DefSite())
			if err != nil {
				t.Fatal(err)
			}
			nodes := stream.Tokens()
			for len(nodes) != 0 && nodes[len(nodes)-1].Kind() == public.SemicolonKind {
				nodes = nodes[:len(nodes)-1]
			}
			return public.New(nodes...)
		}
		if mode != "copied" {
			var contents public.TokenStream
			if mode == "rebuilt" {
				length = text("N").Tokens()[0]
				contents = generics.Children().Append(text(", const N int").Tokens()...)
			} else {
				contents = public.New(element).Append(text("any, const").Tokens()...).Append(length).Append(text("int").Tokens()...)
			}
			mixed, err := public.NewGroup(public.Bracket, contents, ctx.DefSite())
			if err != nil {
				t.Fatal(err)
			}
			generics = mixed
		}
		array, err := public.NewGroup(public.Bracket, public.New(length), ctx.DefSite())
		if err != nil {
			t.Fatal(err)
		}
		params, err := public.NewGroup(public.Parenthesis, text("value").Append(array, element), ctx.DefSite())
		if err != nil {
			t.Fatal(err)
		}
		output := public.New()
		for _, name := range []string{"first", "second"} {
			generated := text("func "+name).Append(generics, params, array, element)
			generated = generated.Append(text("{ return value }").Tokens()...)
			output = output.Append(generated.Tokens()...).Append(public.NewSemicolon(ctx.DefSite()))
		}
		return pm.Success(output)
	}}
	resolver := genericDeriveResolver{macro: expand.Macro{
		File: definition,
		Proc: &procmacro.Entry{Name: entry.Name, Kind: pm.Attribute, Function: definition.Items[0].(*ast.FuncDecl), File: definition},
		Run: func(request pm.Request) ([]byte, error) {
			output, err := pm.Dispatch(request, []pm.Entry{entry})
			if err != nil {
				return nil, err
			}
			var wire bytes.Buffer
			err = pm.WriteResponse(&wire, request, output)
			return wire.Bytes(), err
		},
	}}
	input := files["model/model.gox"]
	expanded, err := expand.Expand([]*ast.File{input}, resolver, map[*ast.File]config.Config{
		input: {Environment: &config.Environment{Target: config.Target{OS: "linux", Arch: "amd64"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result := resolve.ResolveExpanded(files, expanded, nil)
	noDiagnostics(t, result)
	file := result.Files["model/model.gox"]
	seen := map[*resolve.Symbol]bool{}
	for _, item := range file.AST.Items {
		function := item.(*ast.FuncDecl)
		bindings := map[string]*resolve.Symbol{}
		for _, parameter := range function.Generics.Params {
			name := parameter.Names[0]
			binding := file.Definitions[name.ID()]
			kind := resolve.TypeParam
			if parameter.Const {
				kind = resolve.ConstParam
			}
			if binding == nil || binding.Kind != kind || seen[binding] {
				t.Fatal("copied generic groups shared a parameter binding")
			}
			seen[binding], bindings[name.Name] = true, binding
		}
		ast.Walk(function, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && bindings[name.Name] != nil && file.Definitions[name.ID()] == nil {
				if file.References[name.ID()].Symbol != bindings[name.Name] {
					t.Errorf("copied %s reference escaped its own declaration", name.Name)
				}
			}
			return true
		})
	}
	if len(seen) != 4 {
		t.Fatalf("two copied T/N bindings = %d, want 4", len(seen))
	}
}
