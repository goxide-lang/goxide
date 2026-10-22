package compile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestDeclarationBoundaryAcrossSourceAndWASIMacros(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	macros := filepath.Join(root, "macros")
	writeProcBuildManifest(t, macros, sdk, "")
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
func declaration(ctx pm::Context) pm::Output {
 output, err := token::ParseText("declare impl Reader for Native;", ctx.CallSite())
 return pm::FromParse(output, err)
}
#[proc_macro]
pub func adoption(ctx pm::Context, input token::TokenStream) pm::Output { return declaration(ctx) }
#[proc_macro_attribute]
pub func adopt(ctx pm::Context, args token::TokenStream, item token::TokenStream) pm::Output { return declaration(ctx) }
#[proc_macro_derive(Adopt)]
pub func deriveAdopt(ctx pm::Context, item token::TokenStream) pm::Output { return declaration(ctx) }
`)
	for _, test := range []struct {
		name, source string
		kind         pm.Kind
		procedural   bool
	}{
		{name: "source", source: "declare impl Reader for Native;"},
		{name: "declarative", source: "macro_rules! adoption { () => { declare impl Reader for Native; }; }\nadoption!();"},
		{name: "function-like", source: "macros::adoption!();", kind: pm.Function, procedural: true},
		{name: "attribute", source: "#[macros::adopt]\nstruct Item {}", kind: pm.Attribute, procedural: true},
		{name: "derive", source: "#[derive(macros::Adopt)]\nstruct Item {}", kind: pm.Derive, procedural: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := filepath.Join(root, test.name)
			writeProjectFile(t, app, project.ManifestName, `[go_mod]
module="example.org/declaration-boundary"
go="1.27"
[lib]
name="boundary"
path="."
[dependencies.macros]
path="../macros"
`)
			writeProjectFile(t, app, "lib.gox", "package boundary\nuse macros;\ntrait Reader { func read(self Self) int }\nstruct Native {}\n"+test.source+"\n")
			calls := 0
			run := testWASI(t, func(request pm.Request) {
				calls++
				if !test.procedural || request.Kind() != test.kind {
					t.Fatalf("unexpected procedural request: %s", request.Kind())
				}
			})
			result, err := PrepareProject(app, Options{Lib: true, Offline: true, RunProcMacro: run})
			if result != nil || err == nil || !strings.Contains(err.Error(), "declare requires func or type") {
				t.Fatalf("declaration boundary = %v, %v", result, err)
			}
			wantCalls := 0
			if test.procedural {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("WASI executions = %d, want %d", calls, wantCalls)
			}
		})
	}
}
