package compile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/procmacro"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func TestSnapshotMacroBindingIsolationAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	library := &analysis.ProcLibrary{PackagePath: "macro"}
	wasm := []byte("immutable program")
	snapshot := &Snapshot{Context: ctx, Input: analysis.Input{Modules: []analysis.Module{{ProcLibraries: []*analysis.ProcLibrary{library}}}}, macros: map[*analysis.ProcLibrary][]byte{library: wasm}}
	snapshot.BindProcMacros(func(program []byte, _ pm.Request) ([]byte, error) { clear(program); return []byte("first"), nil })
	bound := snapshot.Input.Modules[0].ProcLibraries[0]
	if bound == library || library.Run != nil {
		t.Fatal("snapshot changed the project's callback-bearing library")
	}
	if output, err := bound.Run(pm.Request{}); err != nil || string(output) != "first" {
		t.Fatalf("first binding: %q, %v", output, err)
	}
	if !bytes.Equal(wasm, []byte("immutable program")) {
		t.Fatal("macro host changed another snapshot's program")
	}
	snapshot.BindProcMacros(func(program []byte, _ pm.Request) ([]byte, error) { cancel(); return []byte("outdated response"), nil })
	bound = snapshot.Input.Modules[0].ProcLibraries[0]
	if output, err := bound.Run(pm.Request{}); !errors.Is(err, context.Canceled) || output != nil {
		t.Fatalf("canceled execution published a result: %q, %v", output, err)
	}
	called := false
	snapshot.BindProcMacros(func([]byte, pm.Request) ([]byte, error) { called = true; return nil, nil })
	if _, err := snapshot.Input.Modules[0].ProcLibraries[0].Run(pm.Request{}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("closed execution restarted: %v, called=%v", err, called)
	}
}

func TestPreparedMacroUsesUnsavedProducerAndDependencySources(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	app, macros, helper := filepath.Join(root, "app"), filepath.Join(root, "macros"), filepath.Join(root, "helper")
	writeProcBuildManifest(t, macros, sdk, `helper={path="../helper"}`)
	writeProjectFile(t, helper, "goxide.toml", "[go_mod]\nmodule='example.org/helper'\ngo='1.27'\n[lib]\nname='helper'\npath='.'\n")
	writeProjectFile(t, helper, "text.go", "package helper\nfunc Text()string{return \"11\"}\n")
	producer := `package macros
use syntax::token;
use syntax::proc_macro as pm;
use helper;
#[proc_macro]
pub func answer(ctx pm::Context,input token::TokenStream)pm::Output {
 var output,err=token::ParseText(helper::Text(),ctx.DefSite())
 return pm::FromParse(output,err)
}
`
	writeProjectFile(t, macros, "lib.gox", producer)
	writeProjectFile(t, app, "goxide.toml", "[go_mod]\nmodule='example.org/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\nmacros={path='../macros'}\n")
	consumer := "package main\nuse macros;\nfunc main(){println(macros::answer!())}\n"
	writeProjectFile(t, app, "main.gox", consumer)
	overlays := map[string][]byte{
		filepath.Join(macros, "lib.gox"): []byte(strings.ReplaceAll(producer, "answer(", "updated(")),
		filepath.Join(helper, "text.go"): []byte("package helper\nfunc Text()string{return \"22\"}\n"),
		filepath.Join(app, "main.gox"):   []byte(strings.ReplaceAll(consumer, "answer!", "updated!")),
	}
	prepared, err := Prepare(t.Context(), app, Options{Offline: true, Editor: true, SourceOverlays: overlays})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if !prepared.MacroSource(filepath.Join(helper, "text.go")) || !prepared.MacroSource(filepath.Join(macros, "lib.gox")) || prepared.MacroSource(filepath.Join(app, "main.gox")) {
		t.Fatal("macro dependency closure classification is incorrect")
	}
	snapshot, err := prepared.Snapshot(t.Context(), overlays)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	host, err := procmacro.NewWASIHostWithBudgets(t.Context(), procmacro.MaxInvocationTime*wasiTestBudgetMultiplier, procmacro.MaxCompilationTime*wasiTestBudgetMultiplier)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	session, err := host.NewSession(snapshot.Context)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	snapshot.BindProcMacros(session.Run)
	result, err := analysis.Analyze(snapshot.Context, snapshot.Input)
	if err != nil {
		t.Fatal(err)
	}
	output, err := lower.Generate(snapshot.Context, result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output["example.org/app/main.gox"]), "println(22)") {
		t.Fatalf("unsaved macro/dependency result:\n%s", output["example.org/app/main.gox"])
	}
	macroName := false
	for key, name := range snapshot.SourceNames {
		if name == filepath.Join(macros, "lib.gox") && result.Sources[key] != nil {
			macroName = true
		}
	}
	if !macroName {
		t.Fatal("macro declaration projection lost its author source mapping")
	}
	for name, want := range map[string]string{filepath.Join(macros, "lib.gox"): producer, filepath.Join(helper, "text.go"): "package helper\nfunc Text()string{return \"11\"}\n", filepath.Join(app, "main.gox"): consumer} {
		if data, err := os.ReadFile(name); err != nil || string(data) != want {
			t.Fatalf("unsaved overlay modified author source %s: %v", name, err)
		}
	}
}

func TestEditorRootMacroRetainsUnfinishedProducer(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	writeProcBuildManifest(t, root, sdk, "")
	text := `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func identity(ctx pm::Context,input token::TokenStream)pm::Output { input.
}
pub func Healthy(value int)int{return value}
`
	writeProjectFile(t, root, "lib.gox", text)
	prepared, err := Prepare(t.Context(), root, Options{Lib: true, Offline: true, Editor: true})
	if err != nil {
		t.Fatalf("unfinished target producer blocked editor preparation: %v", err)
	}
	defer prepared.Close()
	if prepared.MacroSource(filepath.Join(root, "lib.gox")) {
		t.Fatal("uncompiled editor producer requires a macro program rebuild")
	}
	first, err := prepared.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := prepared.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.Input.Modules[0].Producer == nil || first.Input.Modules[0].Producer == second.Input.Modules[0].Producer {
		t.Fatal("producer state was missing or shared across analyses")
	}
	first.Input.Options.Editor = true
	result, err := analysis.Analyze(first.Context, first.Input)
	if err != nil || result == nil || result.Err == nil {
		t.Fatalf("partial producer analysis = %v, %v", result, err)
	}
	hover, err := analysis.NewQueries(result).Hover("example.org/macros/lib.gox", uint32(strings.Index(text, "Healthy(")))
	if err != nil || hover == nil || !strings.Contains(hover.Text, "func Healthy(value int) int") {
		t.Fatalf("healthy producer declaration = %#v, %v; %v", hover, err, result.Diagnostics)
	}
	if first.SourceNames["example.org/macros/lib.gox"] != filepath.Join(root, "lib.gox") {
		t.Fatal("root producer source identity changed")
	}
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("editor producer published lock: %v", err)
	}
	if cli, err := Prepare(t.Context(), root, Options{Lib: true, Offline: true}); err == nil {
		cli.Close()
		t.Fatal("CLI preparation accepted unfinished producer")
	}
}
