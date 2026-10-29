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

type genericDeriveResolver struct{ macro expand.Macro }

func (r genericDeriveResolver) LookupMacro(*ast.Path, expand.Scope) (expand.Macro, error) {
	return r.macro, nil
}

func TestProceduralDeriveCopiedGenericGroupsKeepOwnBindings(t *testing.T) {
	files := parseFiles(t, map[string]string{
		"macros/definition.gox": "package macros\npub func Generate() {}\n",
		"model/model.gox": `package model
#[derive(Generate)]
pub struct Parcel[T any] { pub Value T }
`,
	})
	definition := files["macros/definition.gox"]
	entry := pm.Entry{Name: "Generate", Derive: func(ctx pm.Context, item public.TokenStream) pm.Output {
		var name, generics public.Token
		for _, node := range item.Tokens() {
			if node.Text() == "Parcel" {
				name = node
			}
			if node.Kind() == public.GroupKind && node.Delimiter() == public.Bracket {
				generics = node
			}
		}
		parameter := generics.Children().Tokens()[0]
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
		argument, err := public.NewGroup(public.Bracket, public.New(parameter), ctx.DefSite())
		if err != nil {
			t.Fatal(err)
		}
		var output public.TokenStream
		for _, method := range []string{"first", "second"} {
			generated := text("impl").Append(generics, name, argument)
			generated = generated.Append(text("where").Tokens()...).Append(parameter)
			generated = generated.Append(text(": any { func " + method + "(self Self) Self { return self } }").Tokens()...)
			output = output.Append(generated.Tokens()...).Append(public.NewSemicolon(ctx.DefSite()))
		}
		return pm.Success(output)
	}}
	resolver := genericDeriveResolver{macro: expand.Macro{
		File: definition,
		Proc: &procmacro.Entry{Name: entry.Name, Kind: pm.Derive, Function: definition.Items[0].(*ast.FuncDecl), File: definition},
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
		var generic *ast.GenericParams
		switch item := item.(type) {
		case *ast.TypeDecl:
			generic = item.Generics
		case *ast.ImplDecl:
			generic = item.Generics
		}
		if generic == nil {
			continue
		}
		binding := file.Definitions[generic.Params[0].Names[0].ID()]
		if binding == nil || seen[binding] {
			t.Fatal("copied generic groups shared a type parameter binding")
		}
		seen[binding] = true
		ast.Walk(item, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && name.Name == "T" && file.Definitions[name.ID()] == nil {
				if file.References[name.ID()].Symbol != binding {
					t.Error("copied generic reference escaped its own declaration")
				}
			}
			return true
		})
	}
	if len(seen) != 3 {
		t.Fatalf("original and two derived parameter bindings = %d", len(seen))
	}
}
