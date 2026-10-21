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
	"github.com/goxide-lang/syntax/syn"
	public "github.com/goxide-lang/syntax/token"
)

func TestProceduralDeriveCapturedFieldTypesKeepOwnBindings(t *testing.T) {
	structSource := "#[derive(Generate)] pub struct Parcel[T any] { pub Values []T; pub Pair [2]T }"
	enumSource := "#[derive(Generate)] pub enum Parcel[T any] { Values([]T), Pair([2]T) }"
	for _, test := range []struct {
		name, source string
		bindings     int
	}{
		{"source_struct", structSource, 3},
		{"source_enum", enumSource, 3},
		{"declarative_struct", "macro_rules! generate { () => {" + structSource + "}; }\ngenerate!()", 3},
		{"declarative_enum", "macro_rules! generate { () => {" + enumSource + "}; }\ngenerate!()", 3},
		{"repeated_declarative", `macro_rules! generate { ($name:ident) => {
            #[derive(Generate)] pub struct $name[T any] { pub Values []T; pub Pair [2]T }
        }; }
        generate!(Parcel)
        generate!(Other)`, 6},
	} {
		t.Run(test.name, func(t *testing.T) { checkProceduralCapturedFieldTypes(t, test.source, test.bindings) })
	}
}

func checkProceduralCapturedFieldTypes(t *testing.T, source string, wantBindings int) {
	files := parseFiles(t, map[string]string{
		"macros/definition.gox": "package macros\ntype T int; pub func Generate() {}\n",
		"model/model.gox":       "package model\n" + source + "\n",
	})
	definition := files["macros/definition.gox"]
	entry := pm.Entry{Name: "Generate", Derive: func(ctx pm.Context, item public.TokenStream) pm.Output {
		parsed, err := syn.Parse(item)
		if err != nil {
			t.Fatal(err)
		}
		// Rebuild the parsed name as syntax, retaining its binding context.
		// An opaque ident fragment cannot fill a whole type slot.
		name, err := public.NewIdent(parsed.Name().Text(), parsed.Name().Raw(), parsed.Name().Context())
		if err != nil {
			t.Fatal(err)
		}
		generics := parsed.Generics().Tokens()[0]
		parameter := generics.Children().Tokens()[0]
		fields, _ := parsed.Type().StructFields()
		for _, variant := range parsed.Variants() {
			fields = append(fields, variant.Fields()...)
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
		argument, err := public.NewGroup(public.Bracket, public.New(parameter), ctx.DefSite())
		if err != nil {
			t.Fatal(err)
		}
		var output public.TokenStream
		for _, method := range []string{"first", "second"} {
			generated := text("impl").Append(generics, name, argument)
			generated = generated.Append(text("where").Tokens()...)
			for i, field := range fields {
				if i != 0 {
					generated = generated.Append(text(",").Tokens()...)
				}
				generated = generated.Append(field.Type().ToTokens().Tokens()...).Append(text(": any").Tokens()...)
			}
			generated = generated.Append(text("{ func " + method + "(self Self, definition T) Self { _ = definition; return self } }").Tokens()...)
			output = output.Append(generated.Tokens()...).Append(public.NewSemicolon(ctx.DefSite()))
		}
		return pm.Success(output)
	}}
	resolver := genericDeriveResolver{macro: expand.Macro{
		File: definition,
		Proc: &procmacro.Entry{Name: entry.Name, Kind: pm.Derive, Function: definition.Items[1].(*ast.FuncDecl), File: definition},
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
	definitionFile := result.Files["macros/definition.gox"]
	definitionT := definitionFile.Definitions[definitionFile.AST.Items[0].(*ast.TypeDecl).Name.ID()]
	seen := map[*resolve.Symbol]bool{}
	for _, item := range file.AST.Items {
		var generic *ast.GenericParams
		switch item := item.(type) {
		case *ast.TypeDecl:
			generic = item.Generics
		case *ast.ImplDecl:
			generic = item.Generics
		case *ast.EnumDecl:
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
				if name.Ctx == generic.Params[0].Names[0].Ctx {
					if file.References[name.ID()].Symbol != binding {
						t.Error("captured field type escaped its copied parameter binding")
					}
				} else if file.References[name.ID()].Symbol != definitionT {
					t.Error("definition-site T was captured by a generated generic parameter")
				}
			}
			return true
		})
	}
	if len(seen) != wantBindings {
		t.Fatalf("original and derived parameter bindings = %d, want %d", len(seen), wantBindings)
	}
}
