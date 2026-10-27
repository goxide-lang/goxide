package compile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

func TestProceduralLibraryBuildWithoutHost(t *testing.T) {
	sdk := compilerSDKForTest(t)
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("all_entries_disabled=%t", disabled), func(t *testing.T) {
			root := t.TempDir()
			writeProcBuildManifest(t, root, sdk, "")
			attribute := ""
			if disabled {
				attribute = "#[cfg(any())]\n"
			}
			writeProjectFile(t, root, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
`+attribute+`#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 return pm::Success(input)
}
pub func helper() int { return 1 }
`)
			result, err := PrepareProject(root, Options{Lib: true, Offline: true})
			if err != nil {
				t.Fatal(err)
			}
			if result.Kind != "proc-macro" {
				t.Fatalf("library result kind = %q", result.Kind)
			}
			readProcBuildArtifact(t, result)
		})
	}
}

func TestProceduralLibraryRejectsSelectedSDKBeforeBuilding(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	selected := filepath.Join(root, "selected-sdk")
	mod, err := os.ReadFile(filepath.Join(sdk, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, selected, "go.mod", string(mod))
	for _, directory := range []string{"proc_macro", "token", "internal/lex"} {
		if err := os.CopyFS(filepath.Join(selected, directory), os.DirFS(filepath.Join(sdk, directory))); err != nil {
			t.Fatal(err)
		}
	}
	writeProjectFile(t, selected, "proc_macro/changed.go", "package proc_macro\nconst ChangedProtocolContent = true\n")
	macros := filepath.Join(root, "macros")
	writeProcBuildManifest(t, macros, selected, "")
	// If SDK validation ran after producer compilation, this body would fail
	// first. The mismatch must be diagnosed before compiling a macro program.
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context, input token::TokenStream) pm::Output {
 return notDeclared
}
`)
	_, err = PrepareProject(macros, Options{Lib: true, Offline: true})
	if err == nil || !strings.Contains(err.Error(), "procedural macro SDK content mismatch") {
		t.Fatalf("selected SDK mismatch error = %v", err)
	}
}

func TestProceduralLibraryLockedHelperContentRebuild(t *testing.T) {
	sdk := compilerSDKForTest(t)
	run := testWASI(t, nil)
	root := t.TempDir()
	macros, helper := filepath.Join(root, "macros"), filepath.Join(root, "helper")
	writeProcBuildManifest(t, macros, sdk, `helper={path="../helper"}`)
	writeProjectFile(t, helper, "goxide.toml", `[go_mod]
module="example.org/helper"
go="1.27"
[lib]
name="helper"
path="."
`)
	writeProjectFile(t, macros, "lib.gox", `package macros
use syntax::token;
use syntax::proc_macro as pm;
use helper;
#[proc_macro]
pub func answer(ctx pm::Context, input token::TokenStream) pm::Output {
 var output, err = token::ParseText(helper::Text(), ctx.DefSite())
 return pm::FromParse(output, err)
}
`)
	writeProjectFile(t, helper, "lib.gox", "package helper\npub func Text() string { return \"11\" }\n")
	first, err := PrepareProject(macros, Options{Lib: true, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	before := readProcBuildArtifact(t, first)
	lock, err := os.ReadFile(filepath.Join(macros, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	scope := token.NewScope("helper-rebuild")
	span, err := scope.NewSpan(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	request, err := pm.NewRequest(scope, pm.RequestConfig{
		Entry: "answer", Kind: pm.Function, Position: pm.ExpressionPosition,
		DefSite: scope.NewContext(), CallSite: scope.NewContext(), DefSpan: span, CallSpan: span,
		Target: pm.Target{OS: runtime.GOOS, Arch: runtime.GOARCH},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkOutput := func(wasm []byte, want string) {
		t.Helper()
		data, err := run(wasm, request)
		if err != nil {
			t.Fatal(err)
		}
		output, err := pm.ReadResponse(bytes.NewReader(data), request)
		if err != nil || strings.TrimSpace(output.Tokens.Display()) != want {
			t.Fatalf("macro output = %q, %v; want %s", output.Tokens.Display(), err, want)
		}
	}
	checkOutput(before, "11")
	writeProjectFile(t, helper, "lib.gox", "package helper\npub func Text() string { return \"22\" }\n")
	second, err := PrepareProject(macros, Options{Lib: true, Offline: true, Locked: true})
	if err != nil {
		t.Fatal(err)
	}
	after := readProcBuildArtifact(t, second)
	if first.ProcMacro != second.ProcMacro || first.Root != second.Root {
		t.Fatal("local helper content edit changed the stable macro output path")
	}
	if bytes.Equal(before, after) {
		t.Fatal("local helper content edit reused the old macro bytes")
	}
	checkOutput(after, "22")
	updated, err := os.ReadFile(filepath.Join(macros, "goxide.lock"))
	if err != nil || !bytes.Equal(lock, updated) {
		t.Fatalf("locked helper rebuild changed dependency lock: %v", err)
	}
}

func writeProcBuildManifest(t *testing.T, root, sdk, dependencies string) {
	t.Helper()
	writeProjectFile(t, root, "goxide.toml", fmt.Sprintf(`[go_mod]
module="example.org/macros"
go="1.27"
[lib]
name="macros"
path="."
proc_macro=true
[dependencies]
syntax={path=%q}
%s
`, filepath.ToSlash(sdk), dependencies))
}

func readProcBuildArtifact(t *testing.T, result *Result) []byte {
	t.Helper()
	if result.ProcMacro == "" {
		t.Fatal("proc-macro lib did not return its built artifact")
	}
	data, err := os.ReadFile(result.ProcMacro)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
		t.Fatal("proc-macro lib artifact is not a WASM module")
	}
	return data
}
