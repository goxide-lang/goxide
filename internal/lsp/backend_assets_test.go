package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
)

const ordinaryEmbedSource = `package main
import ("embed"; "fmt")
//go:embed data/*.txt
var assets embed.FS
func Value() string { data, _ := assets.ReadFile("data/a.txt"); return string(data) }
func main() { fmt.Print(Value()) }
`

func singleFileAssetProjection(t *testing.T, server *Server, w *workspace, text string) (*publishedAnalysis, *goProjection) {
	t.Helper()
	name, _, _ := w.native.SingleFile()
	documents := NewDocumentStore("utf-16")
	uri, err := PathToURI(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := documents.Open(uri, 1, text); err != nil {
		t.Fatal(err)
	}
	snapshot, err := w.snapshot(t.Context(), documents.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	snapshot.input.Options.Editor = true
	snapshot.input.Options.PreserveSyntax = true
	result, err := analysis.Analyze(t.Context(), snapshot.input)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := lower.Generate(t.Context(), result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	current := &publishedAnalysis{result: result, names: snapshot.names, diskNames: snapshot.input.DiskNames, generated: generated}
	if _, err := server.prepareGoView(t.Context(), w); err != nil {
		t.Fatal(err)
	}
	projection, _, err := server.projectGo(current, w, nil)
	if err != nil {
		t.Fatal(err)
	}
	return current, projection
}

func TestSingleFileAssetsPreserveBytesAndCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell Go observer")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/assets\ngo 1.27\n")
	writeLanguageFile(t, root, "main.go", ordinaryEmbedSource)
	payload := "first\x00line\n"
	writeLanguageFile(t, root, "data/a.txt", payload)
	writeLanguageFile(t, root, "data/b.txt", "second\n")
	writeLanguageFile(t, root, "sibling.go", "package main\nfunc main() { panic(\"unselected\") }\n")
	counter := filepath.Join(t.TempDir(), "queries")
	tool := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ncase \"$*\" in *goxide_embed_*) printf 'query\\n' >> \"$GOXIDE_EMBED_COUNTER\";; esac\nexec \"$GOXIDE_EMBED_REAL_GO\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOXIDE_EMBED_COUNTER", counter)
	t.Setenv("GOXIDE_EMBED_REAL_GO", filepath.Join(runtime.GOROOT(), "bin", "go"))
	options := compile.Options{GoTool: tool, Offline: true}
	w, err := prepareWorkspace(t.Context(), filepath.Join(root, "main.go"), options)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	server := &Server{ctx: t.Context(), options: ServerOptions{Compiler: options}}
	t.Cleanup(func() { os.RemoveAll(server.goView) })
	current, projection := singleFileAssetProjection(t, server, w, ordinaryEmbedSource)
	if err := server.syncSingleFileAssets(t.Context(), current, w, projection); err != nil {
		t.Fatal(err)
	}
	view := filepath.Join(server.goView, "module")
	for name, expected := range map[string]string{"data/a.txt": payload, "data/b.txt": "second\n"} {
		if data, err := os.ReadFile(filepath.Join(view, name)); err != nil || string(data) != expected {
			t.Fatalf("resource bytes: %s, %v; cache=%+v names=%v projection=%v", data, err, w.assets, current.names, projection.files)
		}
	}
	if _, err := os.Stat(filepath.Join(view, "sibling.go")); !os.IsNotExist(err) {
		t.Fatalf("unselected sibling entered private package: %v", err)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", ".")
	command.Dir, command.Env = view, compile.NativeEnvironment(w.native.Config().Environment.ExecEnv())
	if data, err := command.CombinedOutput(); err != nil || string(data) != payload {
		t.Fatalf("private package changed embedded bytes: %q, %v", data, err)
	}
	queryCount := func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			t.Fatal(err)
		}
		return bytes.Count(data, []byte("query\n"))
	}
	if queryCount() != 1 {
		t.Fatalf("initial resource inventory count = %d", queryCount())
	}
	for range 3 {
		for uri, text := range projection.files {
			projection.files[uri] = strings.Replace(text, "return string(data)", "return string(data) + \"body edit\"", 1)
		}
		if err := server.syncSingleFileAssets(t.Context(), current, w, projection); err != nil {
			t.Fatal(err)
		}
	}
	if queryCount() != 1 {
		t.Fatalf("ordinary body edits reloaded asset inventory %d times", queryCount())
	}
	for uri, text := range projection.files {
		projection.files[uri] = strings.Replace(text, "data/*.txt", "data/a.txt", 1)
	}
	if err := server.syncSingleFileAssets(t.Context(), current, w, projection); err != nil {
		t.Fatal(err)
	}
	if queryCount() != 2 {
		t.Fatalf("changed embed directive did not reload inventory: %d", queryCount())
	}
	if _, err := os.Stat(filepath.Join(view, "data/b.txt")); !os.IsNotExist(err) {
		t.Fatalf("removed resource remained in private package: %v", err)
	}
	// Resource events replace the workspace; unchanged directives must then
	// inventory and copy the new disk bytes into the same stable Go view.
	writeLanguageFile(t, root, "data/a.txt", "updated\n")
	fresh, err := prepareWorkspace(t.Context(), filepath.Join(root, "main.go"), options)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.close()
	current, projection = singleFileAssetProjection(t, server, fresh, ordinaryEmbedSource)
	if err := server.syncSingleFileAssets(t.Context(), current, fresh, projection); err != nil {
		t.Fatal(err)
	}
	if queryCount() != 3 {
		t.Fatalf("resource refresh inventory count = %d", queryCount())
	}
	if data, err := os.ReadFile(filepath.Join(view, "data/a.txt")); err != nil || string(data) != "updated\n" {
		t.Fatalf("resource event left stale bytes: %s, %v", data, err)
	}
}

func TestLanguageServerSingleFileEmbedResources(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell gopls observer")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/embedded\ngo 1.27\n")
	writeLanguageFile(t, root, "main.go", ordinaryEmbedSource)
	writeLanguageFile(t, root, "data/a.txt", "original\n")
	writeLanguageFile(t, root, "sibling.go", "package main\nfunc Value() bool { return false }\n")
	viewPath := filepath.Join(t.TempDir(), "view")
	tool := filepath.Join(t.TempDir(), "gopls")
	t.Setenv("GOXIDE_ASSET_GOPLS_VIEW", viewPath)
	t.Setenv("GOXIDE_ASSET_REAL_GOPLS", realGopls(t))
	if err := os.WriteFile(tool, []byte("#!/bin/sh\npwd > \"$GOXIDE_ASSET_GOPLS_VIEW\"\nexec \"$GOXIDE_ASSET_REAL_GOPLS\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	peer := startLanguageServerOptions(t, filepath.Join(root, "main.go"), ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "main.go"), ordinaryEmbedSource)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, ordinaryEmbedSource, "Value())")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("string")) {
		t.Fatalf("embedded single-file hover: %s; log=%s", hover, peer.log)
	}
	if diagnostics := latestLanguageDiagnostics(t, peer, uri); len(diagnostics) != 0 {
		t.Fatalf("valid single file has diagnostics: %+v", diagnostics)
	}
	view, err := os.ReadFile(viewPath)
	if err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(strings.TrimSpace(string(view)), "data/a.txt")
	if data, err := os.ReadFile(asset); err != nil || string(data) != "original\n" {
		t.Fatalf("gopls view lacks exact resource: %q, %v", data, err)
	}
	writeLanguageFile(t, root, "data/a.txt", "resource event\n")
	assetURI, err := PathToURI(filepath.Join(root, "data/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	peer.notify(t, "workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": assetURI, "type": 2}}})
	peer.call(t, "textDocument/hover", params)
	if data, err := os.ReadFile(asset); err != nil || string(data) != "resource event\n" {
		t.Fatalf("resource watcher left stale private bytes: %q, %v", data, err)
	}

}

func TestLanguageServerEmbeddedNativeSourceKeepsCompilerQueries(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/unsupported\ngo 1.27\n")
	poison := "package main\nfunc main() { panic(\"resource only\") }\n"
	writeLanguageFile(t, root, "sibling.go", poison)
	text := "package main\nuse embed as _;\nuse fmt;\n//go:embed sibling.go\nvar Data string\nfunc Local() int { return 42 }\nfunc main() { fmt::Print(Data); _ = Local() }\n"
	input := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	peer := startLanguageServerOptions(t, input, ServerOptions{Gopls: realGopls(t), Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, input, text)
	var preview struct{ Content string }
	if err := json.Unmarshal(peer.call(t, "goxide/generatedSource", map[string]string{"uri": uri}), &preview); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Content, "//go:embed sibling.go") || !strings.Contains(peer.log.String(), "embedded native source sibling.go") {
		t.Fatalf("missing preserved preview or backend limitation: %s; log=%s", preview.Content, peer.log)
	}
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, text, "Local() }")}
	if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("int")) {
		t.Fatalf("backend limitation disabled source hover: %s", hover)
	}
	if diagnostics := latestLanguageDiagnostics(t, peer, uri); len(diagnostics) != 0 {
		t.Fatalf("backend limitation became a source error: %+v", diagnostics)
	}
	// The same generated file remains valid in the CLI's named-file Go mode,
	// and its embedded data must be the original sibling bytes.
	writeLanguageFile(t, root, "selected.go", preview.Content)
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "selected.go")
	command.Dir, command.Env = root, compile.NativeEnvironment(os.Environ())
	if data, err := command.CombinedOutput(); err != nil || string(data) != poison {
		t.Fatalf("named-file compilation/data semantics: %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(root, "sibling.go")); err != nil || string(data) != poison {
		t.Fatalf("backend rewrote embedded source data: %q, %v", data, err)
	}
	bad := strings.Replace(text, "return 42", "return \"bad\"", 1)
	changeLanguageDocument(t, peer, uri, bad, 2)
	peer.call(t, "textDocument/definition", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, bad, "Print(Data)")})
	if diagnostics := latestLanguageDiagnostics(t, peer, uri); len(diagnostics) == 0 {
		t.Fatal("backend limitation suppressed later compiler diagnostics")
	}
}
