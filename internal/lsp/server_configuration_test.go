package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
)

func TestLanguageServerConfigurationMatchesBuild(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const api = `package app
#[cfg(feature("selected"))]
func Value() string { return "selected" }
#[cfg(not(feature("selected")))]
func Value() int { return 0 }
`
	const native = "//go:build editorcfg\n\npackage app\nimport _ \"embed\"\n//go:embed asset.txt\nvar asset string\nfunc FromGo() string { return Value() + asset }\n"
	const external = `package app_test
use testing;
use crate as app;
func TestSource(t *testing::T) {
    if app::Value() != "selected" { t.Fatal("feature selection") }
}
`
	const goTest = "package app_test\nimport (\"testing\"; app \"example.org/editor-config/lib\")\nfunc TestNative(t *testing.T) { if app.FromGo() != \"selected resource\" { t.Fatal(app.Value()) } }\n"
	for name, content := range map[string]string{
		"goxide.toml":        "[go_mod]\nmodule='example.org/editor-config'\ngo='1.27'\n[lib]\nname='app'\npath='lib'\n[features]\nselected=[]\n",
		"lib/api.gox":        api,
		"lib/api_test.gox":   external,
		"lib/native.go":      native,
		"lib/native_test.go": goTest,
		"lib/asset.txt":      " resource",
		"lib/disabled.go":    "//go:build !editorcfg\n\npackage app\nfunc FromGo() bool { return false }\n",
		"lib/native_cgo.go":  "package app\nimport \"C\"\nimport _ \"example.org/not-selected\"\n",
		"unused/bad.gox":     "not part of this library target",
	} {
		writeLanguageFile(t, root, name, content)
	}
	options := compile.Options{Offline: true, Lib: true, Features: []string{"selected"}, Tags: []string{"editorcfg"}}
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: realGopls(t), Compiler: options})
	apiURI := openLanguageDocument(t, peer, filepath.Join(root, "lib/api.gox"), api)
	nativeURI := openLanguageDocument(t, peer, filepath.Join(root, "lib/native.go"), native)
	testURI := openLanguageDocument(t, peer, filepath.Join(root, "lib/api_test.gox"), external)
	goTestURI := openLanguageDocument(t, peer, filepath.Join(root, "lib/native_test.go"), goTest)
	for _, query := range []struct{ uri, text, needle string }{
		{nativeURI, native, "Value()"},
		{testURI, external, "Value()"},
		{goTestURI, goTest, "Value()"},
	} {
		params := map[string]any{"textDocument": map[string]string{"uri": query.uri}, "position": languagePosition(t, query.text, query.needle)}
		var locations []wireLocation
		raw := peer.call(t, "textDocument/definition", params)
		if err := json.Unmarshal(raw, &locations); err != nil || len(locations) != 1 || locations[0].URI != apiURI {
			t.Fatalf("selected configuration definition from %s: %s (%v); notifications=%v", query.uri, raw, err, peer.notifications)
		}
		if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("string")) {
			t.Fatalf("selected feature signature from %s: %s", query.uri, hover)
		}
	}
	var preview struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(peer.call(t, "goxide/generatedSource", map[string]string{"uri": apiURI}), &preview); err != nil {
		t.Fatal(err)
	}
	for _, uri := range []string{apiURI, nativeURI, testURI, goTestURI} {
		if diagnostics := latestLanguageDiagnostics(t, peer, uri); len(diagnostics) != 0 {
			t.Fatalf("valid selected input has diagnostics %s: %+v", uri, diagnostics)
		}
	}
	peer.call(t, "shutdown", nil)
	peer.notify(t, "exit", nil)
	awaitRPC(t, peer.done)
	var built *compile.Result
	for range 2 {
		built, err = compile.PrepareProject(root, options)
		if err != nil {
			t.Fatal(err)
		}
		generated, err := os.ReadFile(filepath.Join(built.Root, "lib/api.go"))
		if err != nil || string(generated) != preview.Content {
			t.Fatalf("CLI generation differs from editor or previous generation: %v\n%s\npreview:\n%s", err, generated, preview.Content)
		}
	}
	writeLanguageFile(t, built.Root, "cmd/check/main.go", "package main\nimport (\"fmt\"; app \"example.org/editor-config/lib\")\nfunc main(){fmt.Println(app.FromGo())}\n")
	for _, args := range [][]string{{"build", "-mod=readonly", "-tags=editorcfg", "./..."}, {"test", "-mod=readonly", "-tags=editorcfg", "-count=1", "./..."}, {"run", "-mod=readonly", "-tags=editorcfg", "./cmd/check"}} {
		command := exec.Command(built.Config.Environment.GoTool, args...)
		command.Dir, command.Env = built.Root, built.Config.Environment.ExecEnv()
		output, err := command.CombinedOutput()
		if err != nil || args[0] == "run" && string(output) != "selected resource\n" {
			t.Fatalf("generated Go %v: %v\n%s", args, err, output)
		}
	}
}
