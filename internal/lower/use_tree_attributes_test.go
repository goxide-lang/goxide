package lower_test

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestUseTreeAttributesResolveBeforeSplitting(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"same_namespace", `use macros; #[macros::keep] use macros::{identity, Describe}; func main(){println(identity!(42))}`},
		{"empty_group", `use macros; #[macros::keep] use macros::{}; func main(){println(42)}`},
		{"root_group", `use macros; #[macros::keep] use {macros::{identity}, strings::{Repeat}}; func main(){println(identity!(Repeat("42",1)))}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, library, imports := compileProcFixture(t, procFixtureSource)
			calls := 0
			library.Run = procFixtureHost(
				pm.Entry{Name: "keep", Attribute: func(_ pm.Context, _, input public.TokenStream) pm.Output {
					calls++
					if text := input.Display(); strings.Count(text, "use") != 1 || !strings.Contains(text, "{") {
						t.Fatalf("attribute lost the complete use group: %s", text)
					}
					return pm.Success(input)
				}},
				pm.Entry{Name: "identity", Function: func(_ pm.Context, input public.TokenStream) pm.Output { return pm.Success(input) }},
			)
			roots := paths.Roots{
				"macros":  {PackagePath: procFixturePackage, ModulePath: procFixturePackage},
				"strings": {PackagePath: "strings", Source: "toolchain"},
			}
			sources := map[string][]byte{"main.gox": []byte("package main\n" + test.source + "\n")}
			output, err := lower.CompilePackage(sources, "consumer", imports, roots, lower.PackageOptions{Config: testConfig(t), ProcLibraries: []*lower.ProcLibrary{library}})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("attribute calls = %d, want 1", calls)
			}
			directory := t.TempDir()
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module consumer\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, directory, "main.go", output["main.gox"])
			runGo(t, directory, "build", "./...")
			if got := runGo(t, directory, "run", "."); got != "42\n" {
				t.Fatalf("output = %q, want 42", got)
			}
		})
	}
}
