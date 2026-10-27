package compile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
)

func TestMacroWorkConcurrentInputsAndStableSlots(t *testing.T) {
	sdk := compilerSDKForTest(t)
	root := t.TempDir()
	app, macros := filepath.Join(root, "app"), filepath.Join(root, "macros")
	writeProcBuildManifest(t, macros, sdk, "")
	producer := `package macros
use syntax::token;
use syntax::proc_macro as pm;
#[proc_macro]
pub func answer(ctx pm::Context,input token::TokenStream)pm::Output {
 var output,err=token::ParseText("11",ctx.DefSite())
 return pm::FromParse(output,err)
}
`
	writeProjectFile(t, macros, "lib.gox", producer)
	writeProjectFile(t, app, "goxide.toml", "[go_mod]\nmodule='example.org/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\nmacros={path='../macros'}\n")
	writeProjectFile(t, app, "main.gox", "package main\nuse macros;\nfunc main(){println(macros::answer!())}\n")
	overlay := func(value string) map[string][]byte {
		return map[string][]byte{filepath.Join(macros, "lib.gox"): []byte(strings.ReplaceAll(producer, `"11"`, fmt.Sprintf("%q", value)))}
	}
	entered, release := make(chan struct{}, 2), make(chan struct{})
	type outcome struct {
		project *Project
		err     error
		editor  bool
	}
	done := make(chan outcome, 2)
	start := func(editor bool) {
		go func() {
			options := Options{Offline: true, Editor: editor, PrepareNativePackage: func([]analysis.Module) error { entered <- struct{}{}; <-release; return nil }}
			if editor {
				options.SourceOverlays = overlay("22")
			}
			p, err := Prepare(t.Context(), app, options)
			done <- outcome{p, err, editor}
		}()
	}
	waitPrepared := func() {
		t.Helper()
		select {
		case <-entered:
		case result := <-done:
			close(release)
			if result.project != nil {
				result.project.Close()
			}
			t.Fatalf("prepare failed before concurrent publication: %v", result.err)
		case <-time.After(30 * time.Second):
			close(release)
			t.Fatal("concurrent macro preparation did not reach publication")
		}
	}
	start(true)
	waitPrepared()
	start(false)
	waitPrepared()
	close(release)
	projects := map[bool]*Project{}
	for range 2 {
		result := <-done
		if result.err != nil {
			t.Fatal(result.err)
		}
		projects[result.editor] = result.project
		t.Cleanup(func() { result.project.Close() })
	}
	for _, p := range projects {
		if err := p.publishMacroWork(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	program := func(p *Project) []byte {
		t.Helper()
		for _, graph := range p.graphs {
			if stage := p.stages[graph]; len(stage.wasm) > 0 {
				return stage.wasm
			}
		}
		t.Fatal("macro program missing")
		return nil
	}
	editorProgram, cliProgram := bytes.Clone(program(projects[true])), bytes.Clone(program(projects[false]))
	if bytes.Equal(editorProgram, cliProgram) {
		t.Fatal("unsaved and disk macro inputs shared program bytes")
	}
	workSlots := func() []string {
		t.Helper()
		paths, err := filepath.Glob(filepath.Join(projects[true].layoutRoot, "units", "*", "proc-macro", "*", "*", "intermediate", "macro", "*"))
		if err != nil {
			t.Fatal(err)
		}
		return paths
	}
	slots := workSlots()
	if len(slots) != 2 {
		t.Fatalf("work slots=%v", slots)
	}
	var editorSlot string
	for _, slot := range slots {
		want := cliProgram
		if filepath.Base(slot) == "editor" {
			editorSlot = slot
			want = editorProgram
		}
		if data, err := os.ReadFile(filepath.Join(slot, "macro.wasm")); err != nil || !bytes.Equal(data, want) {
			t.Fatalf("published slot mixed programs: %s: %v", slot, err)
		}
	}
	artifact := filepath.Join(editorSlot, "macro.wasm")
	oldTime := time.Unix(1234567890, 0)
	if err := os.Chtimes(artifact, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	again, err := Prepare(t.Context(), app, Options{Offline: true, Editor: true, SourceOverlays: overlay("22")})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if info, err := os.Stat(artifact); err != nil || !info.ModTime().Equal(oldTime) {
		t.Fatalf("unchanged macro rebuild rewrote its slot: %v, %v", info, err)
	}
	updated, err := Prepare(t.Context(), app, Options{Offline: true, Editor: true, SourceOverlays: overlay("33")})
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Close()
	if len(workSlots()) != 2 {
		t.Fatal("source-only edit added a persistent configuration directory")
	}
	if data, err := os.ReadFile(artifact); err != nil || !bytes.Equal(data, program(updated)) || bytes.Equal(data, editorProgram) {
		t.Fatalf("edited macro did not refresh the same slot: %v", err)
	}
	if !bytes.Equal(program(projects[true]), editorProgram) || !bytes.Equal(program(projects[false]), cliProgram) {
		t.Fatal("shared publication changed an active private program")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := updated.publishMacroWork(canceled); err != context.Canceled {
		t.Fatalf("canceled publication = %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("macro work saved the dependency lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "target", "bin")); !os.IsNotExist(err) {
		t.Fatalf("macro work published target output: %v", err)
	}
}
