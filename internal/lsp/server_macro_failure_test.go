package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestReviewBrokenMacroDependencyKeepsHealthySourceQueries(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	command := exec.CommandContext(t.Context(), tool, "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", procmacro.SDKModule)
	command.Env = project.GoEnv(true)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	sdk := strings.TrimSpace(string(output))
	identity, err := procmacro.ReadSDKIdentity(t.Context(), tool, ".", project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app, macros, helper := filepath.Join(root, "app"), filepath.Join(root, "macros"), filepath.Join(root, "helper")
	writeLanguageFile(t, app, "goxide.toml", "[go_mod]\nmodule='example.org/review'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\nmacros={path='../macros'}\n")
	writeLanguageFile(t, macros, "goxide.toml", fmt.Sprintf("[go_mod]\nmodule='example.org/review-macros'\ngo='1.27'\n[lib]\nname='macros'\npath='.'\nproc_macro=true\n[dependencies]\nsyntax={path=%q}\nhelper={path='../helper'}\n", filepath.ToSlash(sdk)))
	writeLanguageFile(t, helper, "goxide.toml", "[go_mod]\nmodule='example.org/review-helper'\ngo='1.27'\n[lib]\nname='helper'\npath='.'\n")
	writeLanguageFile(t, helper, "lib.gox", "package helper\n")
	nativeHelper := "package helper\nfunc Value() int { return 1 }\n"
	writeLanguageFile(t, helper, "native.go", nativeHelper)
	producer := "package macros\nuse syntax::token;\nuse syntax::proc_macro as pm;\nuse helper;\n#[proc_macro]\npub func identity(ctx pm::Context,input token::TokenStream)pm::Output { var _ = helper::Value(); return pm::Success(input) }\n"
	writeLanguageFile(t, macros, "lib.gox", producer)
	consumer := "package main\nuse macros;\nmod stable;\nfunc main(){ println(stable::Value()); println(macros::identity!(42)) }\n"
	writeLanguageFile(t, app, "main.gox", consumer)
	stable := "package stable\npub func Value() int { return 42 }\n"
	writeLanguageFile(t, app, "stable/lib.gox", stable)
	peer := startLanguageServerOptions(t, app, ServerOptions{Compiler: compile.Options{Offline: true}, Gopls: "off"})
	uri := openLanguageDocument(t, peer, filepath.Join(app, "stable/lib.gox"), stable)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, stable, "Value()")}
	if raw := peer.call(t, "textDocument/hover", params); !bytes.Contains(raw, []byte("int")) {
		t.Fatalf("initial independent package query: %s; diagnostics=%v", raw, peer.notifications)
	}
	macroURI := openLanguageDocument(t, peer, filepath.Join(macros, "lib.gox"), producer)
	consumerURI := openLanguageDocument(t, peer, filepath.Join(app, "main.gox"), consumer)
	defer func() {
		if t.Failed() {
			t.Logf("last consumer diagnostics: %v", latestLanguageDiagnostics(t, peer, consumerURI))
		}
	}()
	for index, broken := range []string{
		strings.Replace(producer, "return pm::Success(input)", "var incomplete = (", 1),
		strings.Replace(producer, "input token::TokenStream", "input ", 1),
		strings.Replace(producer, "#[proc_macro]", "#[proc_macro]\n#[proc_macro]", 1),
		strings.Replace(producer, "return pm::Success(input)", "return noSuchValue", 1),
	} {
		changeLanguageDocument(t, peer, macroURI, broken, index+2)
		current := strings.Replace(stable, "return 42", fmt.Sprintf("return %d", index+1), 1)
		changeLanguageDocument(t, peer, uri, current, index+2)
		params["position"] = languagePosition(t, current, "Value()")
		if raw := peer.call(t, "textDocument/hover", params); !bytes.Contains(raw, []byte("int")) {
			t.Fatalf("broken macro dependency removed healthy independent package query: %s; diagnostics=%v", raw, peer.notifications)
		}
		if diagnostics := latestLanguageDiagnostics(t, peer, macroURI); len(diagnostics) == 0 {
			t.Fatalf("producer error has no authored source diagnostic, case %d: %v", index, peer.notifications)
		}
		reviewExpectRequestError(t, peer, "goxide/generatedSource", map[string]string{"uri": consumerURI}, CodeInvalidParams)
	}
	current := strings.Replace(stable, "int { return 42 }", "string { return \"current\" }", 1)
	changeLanguageDocument(t, peer, uri, current, 6)
	params["position"] = languagePosition(t, current, "Value()")
	if raw := peer.call(t, "textDocument/hover", params); !bytes.Contains(raw, []byte("string")) {
		t.Fatalf("healthy package reused previous signature while macro was unavailable: %s", raw)
	}
	changeLanguageDocument(t, peer, macroURI, producer, 6)
	helperURI := openLanguageDocument(t, peer, filepath.Join(helper, "native.go"), nativeHelper)
	for index, badReturn := range []string{"return (", "return \"wrong\""} {
		changeLanguageDocument(t, peer, helperURI, strings.Replace(nativeHelper, "return 1", badReturn, 1), index+2)
		if raw := peer.call(t, "textDocument/hover", params); !bytes.Contains(raw, []byte("string")) {
			t.Fatalf("macro helper failure removed independent package facts: %s", raw)
		}
		if diagnostics := latestLanguageDiagnostics(t, peer, helperURI); len(diagnostics) == 0 {
			t.Fatalf("macro helper Go error lost its original source location: %v", peer.notifications)
		}
		reviewExpectRequestError(t, peer, "goxide/generatedSource", map[string]string{"uri": consumerURI}, CodeInvalidParams)
	}
	changeLanguageDocument(t, peer, helperURI, nativeHelper, 4)
	// Rename the executable entry as well as its consumer input: stale WASM
	// cannot dispatch "updated", even if its old result happened to be valid.
	fixed := strings.Replace(producer, "func identity(", "func updated(", 1)
	changeLanguageDocument(t, peer, macroURI, fixed, 7)
	changeLanguageDocument(t, peer, consumerURI, strings.Replace(consumer, "identity!(42)", "updated!(7)", 1), 2)
	if raw := peer.call(t, "goxide/generatedSource", map[string]string{"uri": consumerURI}); !bytes.Contains(raw, []byte("println(7)")) {
		t.Fatalf("fixed macro did not publish its new expansion: %s", raw)
	}
	if diagnostics := latestLanguageDiagnostics(t, peer, macroURI); len(diagnostics) != 0 {
		t.Fatalf("fixed macro retained old diagnostics: %v", diagnostics)
	}
	prepared, err := compile.Prepare(t.Context(), app, compile.Options{Offline: true, SourceOverlays: map[string][]byte{filepath.Join(macros, "lib.gox"): []byte(strings.Replace(producer, "return pm::Success(input)", "var incomplete = (", 1))}})
	if prepared != nil {
		prepared.Close()
	}
	if err == nil {
		t.Fatal("CLI preparation accepted a broken macro dependency")
	}
	for file, original := range map[string]string{filepath.Join(macros, "lib.gox"): producer, filepath.Join(helper, "native.go"): nativeHelper, filepath.Join(app, "main.gox"): consumer} {
		if data, err := os.ReadFile(file); err != nil || string(data) != original {
			t.Fatalf("macro failure recovery changed authored file %s: %v", file, err)
		}
	}
}

func reviewExpectRequestError(t *testing.T, peer *languagePeer, method string, params any, code int) {
	t.Helper()
	peer.next++
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": peer.next, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	peer.send(t, string(data))
	for {
		message := peer.receive(t)
		if len(message.ID) == 0 || string(message.ID) == "null" {
			peer.notifications = append(peer.notifications, message)
			continue
		}
		if string(message.ID) != fmt.Sprint(peer.next) || message.Error == nil || message.Error.Code != code {
			t.Fatalf("%s must reject unavailable facts with code %d: %+v", method, code, message)
		}
		return
	}
}
