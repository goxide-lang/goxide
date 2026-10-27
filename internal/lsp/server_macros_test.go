package lsp

import (
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

func TestLanguageServerUnsavedProceduralMacro(t *testing.T) {
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
	writeLanguageFile(t, app, "goxide.toml", "[go_mod]\nmodule='example.org/editor'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\nmacros={path='../macros'}\n")
	writeLanguageFile(t, macros, "goxide.toml", fmt.Sprintf("[go_mod]\nmodule='example.org/editor-macros'\ngo='1.27'\n[lib]\nname='macros'\npath='.'\nproc_macro=true\n[dependencies]\nsyntax={path=%q}\nhelper={path='../helper'}\n", filepath.ToSlash(sdk)))
	writeLanguageFile(t, helper, "goxide.toml", "[go_mod]\nmodule='example.org/editor-helper'\ngo='1.27'\n[lib]\nname='helper'\npath='.'\n")
	help := "package helper\nfunc Text()string{return \"41\"}\n"
	writeLanguageFile(t, helper, "text.go", help)
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
	writeLanguageFile(t, macros, "lib.gox", producer)
	consumer := "package main\nuse macros;\nfunc main(){println(macros::answer!())}\n"
	writeLanguageFile(t, app, "main.gox", consumer)
	peer := startLanguageServerOptions(t, app, ServerOptions{Gopls: "off", Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(app, "main.gox"), consumer)
	preview := func(want string) {
		t.Helper()
		raw := peer.call(t, "goxide/generatedSource", map[string]string{"uri": uri})
		var result struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Content, want) {
			t.Fatalf("macro result want %q: %s; notifications=%v", want, raw, peer.notifications)
		}
	}
	preview("println(41)")
	macroURI := openLanguageDocument(t, peer, filepath.Join(macros, "lib.gox"), producer)
	helperURI := openLanguageDocument(t, peer, filepath.Join(helper, "text.go"), help)
	// Three current buffers are prepared as one generation: declaration name,
	// transitive Go dependency and caller all change without touching disk.
	changeLanguageDocument(t, peer, macroURI, strings.Replace(producer, "answer(", "updated(", 1), 2)
	changeLanguageDocument(t, peer, helperURI, strings.Replace(help, "41", "42", 1), 2)
	consumer2 := strings.Replace(consumer, "answer!", "updated!", 1)
	changeLanguageDocument(t, peer, uri, consumer2, 2)
	preview("println(42)")
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, consumer2, "updated!")}
	raw := peer.call(t, "textDocument/definition", params)
	if !strings.Contains(string(raw), macroURI) {
		t.Fatalf("macro declaration navigation: %s", raw)
	}
	for file, want := range map[string]string{filepath.Join(macros, "lib.gox"): producer, filepath.Join(helper, "text.go"): help, filepath.Join(app, "main.gox"): consumer} {
		got, err := os.ReadFile(file)
		if err != nil || string(got) != want {
			t.Fatalf("language server wrote authored macro source %s: %v", file, err)
		}
	}
}
