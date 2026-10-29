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

func TestProceduralIdentityAttributeKeepsItemScopes(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"function", `#[Identity] func Original[T any](value T) T {
 local := value
 closure := func(inner T) T { return inner }
 { nested := closure(local); _ = nested }
 return value
}`},
		{"stacked-attributes", `#[Identity] #[Identity] func Original[T any](value T) T { return value }`},
		{"type", `#[Identity] type Box[T any] struct { Value T }`},
		{"impl-member", `type Box[T any] struct { Value T }
impl[T any] Box[T] { #[Identity] func keep(self Self, value T) T { _ = self; return value } }`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := parseFiles(t, map[string]string{
				"macros/definition.gox": "package macros; pub func Identity() {}",
				"model/model.gox":       "package model; " + tc.source,
			})
			definition := files["macros/definition.gox"]
			entry := pm.Entry{Name: "Identity", Attribute: func(_ pm.Context, _, item public.TokenStream) pm.Output {
				return pm.Success(item)
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
			noDiagnostics(t, resolve.ResolveExpanded(files, expanded, nil))
			for _, invocation := range expanded.Expansions {
				if invocation.Input.File != input || invocation.Input.Span.Start > invocation.Call.Span.Start || invocation.Input.Span.End <= invocation.Call.Span.End {
					t.Fatalf("attribute input and diagnostic call ranges = %v / %v", invocation.Input, invocation.Call)
				}
			}
		})
	}
}
