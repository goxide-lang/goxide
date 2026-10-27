package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/source"
)

func realGopls(t *testing.T) string {
	t.Helper()
	tool, err := exec.LookPath("gopls")
	if err != nil {
		t.Skip("real gopls not installed")
	}
	return tool
}

func openLanguageDocument(t *testing.T, peer *languagePeer, name, text string) string {
	t.Helper()
	uri, err := PathToURI(name)
	if err != nil {
		t.Fatal(err)
	}
	peer.notify(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": text}})
	return uri
}

func changeLanguageDocument(t *testing.T, peer *languagePeer, uri, text string, version int) {
	t.Helper()
	peer.notify(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []ContentChange{{Text: text}}})
}

func TestLanguageServerGoProjectionNavigation(t *testing.T) {
	tool := realGopls(t)
	for _, mode := range []string{"native", "manifest"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package app\r\nfunc Answer() int { return 41 }\r\nfunc sourceCall() int { return Native() }\r\n"
			goText := "package app\r\n// 😀 native caller\r\nfunc Native() int { return Answer() }\r\n"
			writeLanguageFile(t, root, "lib.gox", gox)
			writeLanguageFile(t, root, "native.go", goText)
			if mode == "native" {
				writeLanguageFile(t, root, "go.mod", "module example.org/navigation\n\ngo 1.27\n")
				// A stale owned output must be overlaid without changing the disk.
				writeLanguageFile(t, root, "lib_gox.go", source.GeneratedHeader+"package app\nfunc Answer() bool { return false }\n")
			} else {
				writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/navigation'\ngo='1.27'\n[lib]\nname='app'\npath='lib.gox'\n")
			}
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			goxURI := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
			goURI := openLanguageDocument(t, peer, filepath.Join(root, "native.go"), goText)
			params := map[string]any{"textDocument": map[string]string{"uri": goURI}, "position": languagePosition(t, goText, "Answer()")}
			raw := peer.call(t, "textDocument/definition", params)
			var locations []wireLocation
			if err := json.Unmarshal(raw, &locations); err != nil {
				t.Fatal(err)
			}
			if len(locations) != 1 || locations[0].URI != goxURI || locations[0].Range.Start.Line != 1 {
				t.Fatalf("Go to source definition: %s; notifications=%v", raw, peer.notifications)
			}
			hover := peer.call(t, "textDocument/hover", params)
			if !bytes.Contains(hover, []byte("int")) || bytes.Contains(hover, []byte("bool")) {
				t.Fatalf("stale disk declaration: %s", hover)
			}
			completion := peer.call(t, "textDocument/completion", params)
			if !bytes.Contains(completion, []byte(`"label":"Answer"`)) {
				t.Fatalf("native Go completion: %s", completion)
			}
			symbols := peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": goURI}})
			if !bytes.Contains(symbols, []byte(`"name":"Native"`)) {
				t.Fatalf("native Go symbols: %s", symbols)
			}
			preview := peer.call(t, "goxide/generatedSource", map[string]string{"uri": goxURI})
			if !bytes.Contains(preview, []byte("func Answer() int")) {
				t.Fatalf("current Go preview: %s", preview)
			}
			params["textDocument"] = map[string]string{"uri": goxURI}
			params["position"] = languagePosition(t, gox, "Native()")
			raw = peer.call(t, "textDocument/definition", params)
			if err := json.Unmarshal(raw, &locations); err != nil {
				t.Fatal(err)
			}
			if len(locations) != 1 || locations[0].URI != goURI {
				t.Fatalf("source to Go definition: %s", raw)
			}
			goxEdit := strings.ReplaceAll(strings.Replace(gox, "return 41", "return \"current\"", 1), " int ", " string ")
			goEdit := strings.ReplaceAll(goText, " int ", " string ")
			changeLanguageDocument(t, peer, goxURI, goxEdit, 2)
			changeLanguageDocument(t, peer, goURI, goEdit, 2)
			params["textDocument"] = map[string]string{"uri": goURI}
			params["position"] = languagePosition(t, goEdit, "Answer()")
			hover = peer.call(t, "textDocument/hover", params)
			if !bytes.Contains(hover, []byte("string")) {
				t.Fatalf("unsaved source/Go mismatch: %s", hover)
			}
			for name, expected := range map[string]string{"lib.gox": gox, "native.go": goText} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(data) != expected {
					t.Fatalf("author file changed: %s %v", name, err)
				}
			}
		})
	}
}

func TestLanguageServerExternalGoNavigation(t *testing.T) {
	root := t.TempDir()
	text := "package main\nuse fmt;\nfunc main() { fmt::Println(\"😀\") }\n"
	writeLanguageFile(t, root, "main.gox", text)
	peer := startLanguageServerOptions(t, filepath.Join(root, "main.gox"), ServerOptions{Gopls: realGopls(t), Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "main.gox"), text)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, text, "Println(")}
	raw := peer.call(t, "textDocument/definition", params)
	var definitions []wireLocation
	if err := json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || !strings.HasSuffix(definitions[0].URI, "/src/fmt/print.go") {
		t.Fatalf("external Go declaration: %s; notifications=%v", raw, peer.notifications)
	}
}

func TestLanguageServerNativeModuleConfigurationRefresh(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"first": "func Value()int{return 1}", "second": "func Value()string{return \"new\"}"} {
		writeLanguageFile(t, root, name+"/go.mod", "module example.org/d\n\ngo 1.27\n")
		writeLanguageFile(t, root, name+"/d.go", "package d\n"+body+"\n")
	}
	mod := "module example.org/configuration\n\ngo 1.27\nrequire example.org/d v0.0.0\nreplace example.org/d => ./first\n"
	writeLanguageFile(t, root, "go.mod", mod)
	writeLanguageFile(t, root, "root.gox", "package app\nfunc Root(){}\n")
	native := "package app\nimport \"example.org/d\"\nfunc Use(){ _=d.Value() }\n"
	writeLanguageFile(t, root, "use.go", native)
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: realGopls(t), Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "use.go"), native)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, native, "Value()")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("int")) {
		t.Fatalf("initial selection: %s", hover)
	}
	mod = strings.Replace(mod, "./first", "./second", 1)
	writeLanguageFile(t, root, "go.mod", mod)
	modURI, _ := PathToURI(filepath.Join(root, "go.mod"))
	peer.notify(t, "workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": modURI, "type": 2}}})
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("new replace selection reused old metadata: %s; notifications=%v", hover, peer.notifications)
	}
	if got, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || string(got) != mod {
		t.Fatalf("language server changed native module: %s %v", got, err)
	}
}

func latestLanguageDiagnostics(t *testing.T, peer *languagePeer, uri string) []wireDiagnostic {
	t.Helper()
	var latest []wireDiagnostic
	for _, message := range peer.notifications {
		if message.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params struct {
			URI         string           `json:"uri"`
			Diagnostics []wireDiagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(message.Params, &params); err != nil {
			t.Fatal(err)
		}
		if params.URI == uri {
			latest = params.Diagnostics
		}
	}
	return latest
}

func TestLanguageServerGeneratedPackageLifecycle(t *testing.T) {
	tool := realGopls(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := "package a\nfunc Value() int { return 1 }\n"
	c := "package app\nimport \"example.org/state/a\"\nfunc Consumer() string { return a.Value() }\n"
	writeLanguageFile(t, root, "go.mod", "module example.org/state\n\ngo 1.27\n")
	writeLanguageFile(t, root, "a/value.gox", a)
	writeLanguageFile(t, root, "consumer.go", c)
	// This stale owned file is excluded from the compiler and backend views,
	// even after the last source file is removed or the server is restarted.
	old := source.GeneratedHeader + "package a\nfunc Value() bool {return true}\n"
	writeLanguageFile(t, root, "a/value_gox.go", old)
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	aURI := openLanguageDocument(t, peer, filepath.Join(root, "a/value.gox"), a)
	cURI := openLanguageDocument(t, peer, filepath.Join(root, "consumer.go"), c)
	params := map[string]any{"textDocument": map[string]string{"uri": cURI}, "position": languagePosition(t, c, "Value()")}
	peer.call(t, "textDocument/hover", params)
	if diagnostics := latestLanguageDiagnostics(t, peer, cURI); len(diagnostics) == 0 {
		t.Fatal("unchanged Go consumer omitted initial type error")
	}
	aNew := "package a\nfunc Value() string { return \"new\" }\n"
	changeLanguageDocument(t, peer, aURI, aNew, 2)
	hover := peer.call(t, "textDocument/hover", params)
	if !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("updated dependency: %s", hover)
	}
	if diagnostics := latestLanguageDiagnostics(t, peer, cURI); len(diagnostics) != 0 {
		t.Fatalf("unchanged consumer retained old diagnostics: %+v", diagnostics)
	}
	b := "package b\nfunc Value() string { return \"virtual\" }\n"
	bURI := openLanguageDocument(t, peer, filepath.Join(root, "b/value.gox"), b)
	cNew := strings.ReplaceAll(strings.Replace(c, "/a\"", "/b\"", 1), "a.Value()", "b.Value()")
	changeLanguageDocument(t, peer, cURI, cNew, 2)
	params["position"] = languagePosition(t, cNew, "Value()")
	var definitions []wireLocation
	raw := peer.call(t, "textDocument/definition", params)
	if err := json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 || definitions[0].URI != bURI {
		t.Fatalf("new virtual package: %s", raw)
	}
	peer.notify(t, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": bURI}})
	peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": cURI}})
	if diagnostics := latestLanguageDiagnostics(t, peer, cURI); len(diagnostics) == 0 {
		t.Fatal("closed virtual package did not disappear")
	}
	// The next complete generation must not resurrect a removed source from its
	// disk output. Open documents retain their own bytes until explicitly closed.
	if err := os.Remove(filepath.Join(root, "a/value.gox")); err != nil {
		t.Fatal(err)
	}
	peer.notify(t, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": aURI}})
	changeLanguageDocument(t, peer, cURI, c, 3)
	peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": cURI}})
	if diagnostics := latestLanguageDiagnostics(t, peer, cURI); len(diagnostics) == 0 {
		t.Fatal("owned disk output revived deleted last package file")
	}
	data, err := os.ReadFile(filepath.Join(root, "a/value_gox.go"))
	if err != nil || string(data) != old {
		t.Fatal("language service changed disk generated output")
	}
	peer.call(t, "shutdown", nil)
	peer.notify(t, "exit", nil)
	awaitRPC(t, peer.done)
	restarted := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	openLanguageDocument(t, restarted, filepath.Join(root, "consumer.go"), c)
	restarted.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": cURI}})
	if diagnostics := latestLanguageDiagnostics(t, restarted, cURI); len(diagnostics) == 0 {
		t.Fatal("server restart revived orphan output")
	}
}
