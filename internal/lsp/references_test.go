package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
)

func applyRenameWorkspace(t *testing.T, texts map[string]string, raw json.RawMessage) map[string]string {
	t.Helper()
	var edit sourceWorkspaceEdit
	if err := json.Unmarshal(raw, &edit); err != nil {
		t.Fatal(err)
	}
	result := map[string]string{}
	for name, text := range texts {
		result[name] = text
	}
	for _, document := range edit.DocumentChanges {
		name, err := URIToPath(document.TextDocument.URI)
		if err != nil {
			t.Fatal(err)
		}
		text, exists := result[name]
		if !exists {
			t.Fatalf("rename escaped authored files: %s", name)
		}
		index := source.New(text)
		type replacement struct {
			start, end int
			text       string
		}
		var replacements []replacement
		for _, edit := range document.Edits {
			start, end, err := index.Offsets(edit.Range, source.UTF16)
			if err != nil {
				t.Fatal(err)
			}
			replacements = append(replacements, replacement{start, end, edit.NewText})
		}
		sort.Slice(replacements, func(i, j int) bool { return replacements[i].start > replacements[j].start })
		for _, change := range replacements {
			text = text[:change.start] + change.text + text[change.end:]
		}
		result[name] = text
	}
	return result
}

func runRenamedProgram(t *testing.T, texts map[string]string, expected string) {
	t.Helper()
	inputs := map[string][]byte{}
	for name, text := range texts {
		inputs[filepath.Base(name)] = []byte(text)
	}
	result, err := analysis.Analyze(t.Context(), analysis.Input{PackagePath: "example.org/renamed", Sources: inputs})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := lower.Generate(t.Context(), result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := lower.Generate(t.Context(), result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for key, data := range generated {
		if !bytes.Equal(data, repeated[key]) {
			t.Fatal("rename generation was not deterministic")
		}
		writeLanguageFile(t, root, strings.TrimSuffix(filepath.Base(key), ".gox")+"_gox.go", string(data))
	}
	for name, text := range texts {
		if filepath.Ext(name) == ".go" {
			writeLanguageFile(t, root, filepath.Base(name), text)
		}
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/renamed\n\ngo 1.27\n")
	command := exec.CommandContext(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "-mod=readonly", ".")
	command.Dir = root
	command.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != expected {
		t.Fatalf("run renamed program: %v\n%s", err, output)
	}
}

func TestReferencesAndRenameAcrossClosedSourceFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := "package main\r\nfunc main(){_ = \"😀\"; println(value())}\r\n"
	helper := "package main\nfunc value()int{return 42}\n"
	writeLanguageFile(t, root, "main.gox", main)
	writeLanguageFile(t, root, "helper.gox", helper)
	writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
	peer := startLanguageServer(t, root)
	uri := openLanguageDocument(t, peer, filepath.Join(root, "main.gox"), main)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, main, "value()"), "context": map[string]bool{"includeDeclaration": true}}
	var refs []wireLocation
	if err := json.Unmarshal(peer.call(t, "textDocument/references", params), &refs); err != nil || len(refs) != 2 {
		t.Fatalf("source references: %+v, %v", refs, err)
	}
	prepared := peer.call(t, "textDocument/prepareRename", params)
	if !bytes.Contains(prepared, []byte(`"placeholder":"value"`)) {
		t.Fatalf("prepare rename: %s", prepared)
	}
	params["newName"] = "answer"
	raw := peer.call(t, "textDocument/rename", params)
	if !bytes.Contains(raw, []byte(`"version":null`)) || !bytes.Contains(raw, []byte(`"version":1`)) {
		t.Fatalf("lost source versions: %s", raw)
	}
	texts := map[string]string{filepath.Join(root, "main.gox"): main, filepath.Join(root, "helper.gox"): helper}
	updated := applyRenameWorkspace(t, texts, raw)
	for name, before := range texts {
		data, err := os.ReadFile(name)
		if err != nil || string(data) != before {
			t.Fatalf("server modified %s: %v", name, err)
		}
	}
	if strings.Contains(updated[filepath.Join(root, "main.gox")], "value()") || strings.Contains(updated[filepath.Join(root, "helper.gox")], "value()") {
		t.Fatalf("rename omitted a file: %v", updated)
	}
	runRenamedProgram(t, updated, "42\n")
}

func TestGoAndSourceReferencesRenameOneCurrentProgram(t *testing.T) {
	tool := realGopls(t)
	for _, origin := range []string{"source", "native"} {
		t.Run(origin, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package main\r\nfunc Answer()int{return 42}\r\nfunc Source()int{return Answer()}\r\n"
			native := "package main\nfunc main(){println(Answer()+Source())}\n"
			writeLanguageFile(t, root, "lib.gox", gox)
			writeLanguageFile(t, root, "main.go", native)
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
			position := languagePosition(t, gox, "Answer()}")
			if origin == "native" {
				uri = openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
				position = languagePosition(t, native, "Answer()+")
			}
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position, "context": map[string]bool{"includeDeclaration": true}}
			var refs []wireLocation
			if err := json.Unmarshal(peer.call(t, "textDocument/references", params), &refs); err != nil || len(refs) != 3 {
				t.Fatalf("mixed references: %+v, %v", refs, err)
			}
			params["newName"] = "Total"
			raw := peer.call(t, "textDocument/rename", params)
			updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
			for name, text := range updated {
				if strings.Contains(text, "Answer") {
					t.Fatalf("rename omitted %s: %s", name, text)
				}
			}
			runRenamedProgram(t, updated, "84\n")
		})
	}
}
