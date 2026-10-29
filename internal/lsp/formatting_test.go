package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
)

func TestFormattingUsesOpenSourceWithoutAnalysis(t *testing.T) {
	server := &Server{documents: NewDocumentStore(source.UTF16), encoding: source.UTF16}
	const uri = "file:///project/main.gox"
	const original = "package main\r\nfunc main(){println(\"😀\")}"
	if _, err := server.documents.Open(uri, 7, original); err != nil {
		t.Fatal(err)
	}
	params := json.RawMessage(`{"textDocument":{"uri":"file:///project/main.gox"},"options":{"tabSize":2,"insertSpaces":true}}`)
	answer, err := server.formatting(t.Context(), params)
	if err != nil {
		t.Fatal(err)
	}
	edits := answer.([]wireTextEdit)
	if len(edits) != 1 || edits[0].Range.End != (source.Position{Line: 1, Character: 26}) || !strings.Contains(edits[0].NewText, "\r\n  println(\"😀\")\r\n") {
		t.Fatalf("source edit or UTF-16 range = %+v", edits)
	}
	if current, _ := server.documents.Get(uri); current.Version != 7 || current.Text != original {
		t.Fatal("formatting mutated the document before the client applied its edit")
	}
	if _, _, err := server.documents.Change(uri, 8, []ContentChange{{Text: edits[0].NewText}}); err != nil {
		t.Fatal(err)
	}
	answer, err = server.formatting(t.Context(), params)
	if err != nil || len(answer.([]wireTextEdit)) != 0 {
		t.Fatalf("already formatted document = %v, %v", answer, err)
	}
}

func TestFormattingUnavailableNeverReturnsAnEdit(t *testing.T) {
	for _, test := range []struct{ name, uri, text string }{
		{"syntax error", "file:///project/main.gox", "package p\nfunc unfinished("},
		{"native Go", "file:///project/main.go", "package p"},
		{"closed document", "file:///project/closed.gox", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := &Server{documents: NewDocumentStore(source.UTF16), encoding: source.UTF16}
			if test.text != "" {
				if _, err := server.documents.Open(test.uri, 1, test.text); err != nil {
					t.Fatal(err)
				}
			}
			params, _ := json.Marshal(map[string]any{"textDocument": map[string]string{"uri": test.uri}, "options": map[string]any{"tabSize": 4}})
			answer, err := server.formatting(t.Context(), params)
			var rpc *RPCError
			if answer != nil || !errors.As(err, &rpc) || rpc.Code != -32803 {
				t.Fatalf("unavailable formatting = %v, %v", answer, err)
			}
		})
	}
	server := &Server{documents: NewDocumentStore(source.UTF16), encoding: source.UTF16}
	server.documents.Open("file:///project/main.gox", 1, "package p\n")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if answer, err := server.formatting(ctx, json.RawMessage(`{"textDocument":{"uri":"file:///project/main.gox"},"options":{"tabSize":4}}`)); answer != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled formatting = %v, %v", answer, err)
	}
}

func TestFormattingRejectsQueuedStaleVersion(t *testing.T) {
	server := &Server{documents: NewDocumentStore(source.UTF16), encoding: source.UTF16, sequence: 3}
	if _, err := server.documents.Open("file:///project/main.gox", 2, "package p\n"); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(t.Context(), receiveSequenceKey{}, uint64(2))
	answer, err := server.formatting(ctx, json.RawMessage(`{"textDocument":{"uri":"file:///project/main.gox"},"options":{"tabSize":4}}`))
	if answer != nil || !isStaleEdit(err) {
		t.Fatalf("queued old version received formatting edits: %v, %v", answer, err)
	}
}

func TestFormattingRejectsExcessiveIndentAllocation(t *testing.T) {
	server := &Server{documents: NewDocumentStore(source.UTF16), encoding: source.UTF16}
	if _, err := server.documents.Open("file:///project/main.gox", 1, "package p\nfunc f(){println(1)}\n"); err != nil {
		t.Fatal(err)
	}
	answer, err := server.formatting(t.Context(), json.RawMessage(`{"textDocument":{"uri":"file:///project/main.gox"},"options":{"tabSize":1073741824,"insertSpaces":true}}`))
	var rpc *RPCError
	if answer != nil || !errors.As(err, &rpc) || rpc.Code != CodeInvalidParams {
		t.Fatalf("unbounded indentation request = %v, %v", answer, err)
	}
}

func TestFormattingProtocolUsesUnsavedSourceAndRejectsErrors(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "main.gox", "package main\nfunc main(){println(\"disk\")}\n")
	peer := startLanguageServer(t, filepath.Join(root, "main.gox"))
	uri, _ := PathToURI(filepath.Join(root, "main.gox"))
	text := "package main\r\nfunc main(){println(\"😀 unsaved\")}"
	peer.notify(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "goxide", "version": 1, "text": text}})
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "options": map[string]any{"tabSize": 2, "insertSpaces": true}}
	var edits []wireTextEdit
	if err := json.Unmarshal(peer.call(t, "textDocument/formatting", params), &edits); err != nil || len(edits) != 1 || !strings.Contains(edits[0].NewText, "\r\n  println(\"😀 unsaved\")\r\n") {
		t.Fatalf("formatting RPC did not use current unsaved source: %+v, %v", edits, err)
	}
	peer.notify(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []ContentChange{{Range: &edits[0].Range, Text: edits[0].NewText}}})
	if result := peer.call(t, "textDocument/formatting", params); string(result) != "[]" {
		t.Fatalf("second protocol formatting = %s", result)
	}
	peer.notify(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []ContentChange{{Text: "package main\nfunc incomplete("}}})
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "broken-format", "method": "textDocument/formatting", "params": params})
	peer.send(t, string(request))
	for {
		response := peer.receive(t)
		if len(response.ID) == 0 {
			continue
		}
		if string(response.ID) != `"broken-format"` || response.Error == nil || response.Error.Code != -32803 || len(response.Result) != 0 {
			t.Fatalf("incomplete source produced an edit or lost its diagnostic: %+v", response)
		}
		break
	}
	peer.call(t, "shutdown", nil)
	peer.notify(t, "exit", nil)
}
