package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
)

func TestLanguageServerCodingActionsUseVersionedSourceEdits(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "package main\r\ntrait Read {func read(self &Self, value int) int}\r\ntype Value struct{}\r\nfunc main(){_=\"😀\"}; impl Read for Value{}\r\n"
	name := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	writeCandidateRuntime(t, root)
	peer := startLanguageServer(t, name)
	uri := openLanguageDocument(t, peer, name, text)
	position, err := source.New(text).Position(strings.Index(text, "impl Read"), source.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "range": source.Range{Start: position, End: position}, "context": map[string]any{"diagnostics": []any{}}}
	raw := peer.call(t, "textDocument/codeAction", params)
	var actions []struct {
		Title string              `json:"title"`
		Kind  string              `json:"kind"`
		Edit  sourceWorkspaceEdit `json:"edit"`
	}
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Title != "Implement read" || actions[0].Kind != "quickfix" || len(actions[0].Edit.DocumentChanges) != 1 {
		t.Fatalf("actions=%s diagnostics=%v", raw, peer.notifications)
	}
	group := actions[0].Edit.DocumentChanges[0]
	if group.TextDocument.URI != uri || group.TextDocument.Version == nil || *group.TextDocument.Version != 1 {
		t.Fatalf("unversioned edit: %s", raw)
	}
	edits := group.Edits
	sort.Slice(edits, func(i, j int) bool { return positionBefore(edits[j].Range.Start, edits[i].Range.Start) })
	edited := text
	for _, edit := range edits {
		start, end, err := source.New(edited).Offsets(edit.Range, source.UTF16)
		if err != nil {
			t.Fatal(err)
		}
		edited = edited[:start] + edit.NewText + edited[end:]
	}
	if !strings.Contains(edited, "func read(self &Self, value int) int") || strings.Contains(strings.ReplaceAll(edited, "\r\n", ""), "\n") {
		t.Fatalf("bad edits:\n%s", edited)
	}
	changeLanguageDocument(t, peer, uri, edited, 2)
	raw = peer.call(t, "textDocument/codeAction", params)
	if string(raw) != "[]" {
		t.Fatalf("already implemented action repeated: %s", raw)
	}
	preview := peer.call(t, "goxide/generatedSource", map[string]string{"uri": uri})
	if !bytes.Contains(preview, []byte("TODO: implement read")) {
		t.Fatalf("applied edit did not generate: %s", preview)
	}
	disk, err := os.ReadFile(name)
	if err != nil || string(disk) != text {
		t.Fatalf("server edited authored disk: %s %v", disk, err)
	}
}

func TestLanguageServerCodingActionsKeepExpectedPartialDiagnostics(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "package main\ntrait Read{type Item;func read(self Self)Self::Item;func write(self Self)}\ntype Value struct{}\nimpl Read for Value{}\nfunc main(){}\n"
	name := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	peer := startLanguageServerOptions(t, name, ServerOptions{Gopls: "off"})
	uri := openLanguageDocument(t, peer, name, text)
	position := languagePosition(t, text, "impl Read")
	raw := peer.call(t, "textDocument/codeAction", map[string]any{"textDocument": map[string]string{"uri": uri}, "range": source.Range{Start: position, End: position}, "context": map[string]any{"diagnostics": []any{}}})
	var actions []struct {
		Title string              `json:"title"`
		Edit  sourceWorkspaceEdit `json:"edit"`
	}
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 3 {
		t.Fatalf("partial actions disappeared: %s", raw)
	}
	for _, action := range actions {
		if len(action.Edit.DocumentChanges) != 1 || len(action.Edit.DocumentChanges[0].Edits) != 1 || !strings.Contains(action.Edit.DocumentChanges[0].Edits[0].NewText, "type Item = __GoxideTODO_Item") {
			t.Fatalf("partial action lost its explicit placeholder: %s", raw)
		}
	}
}
