package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
)

func TestLanguageServerVersionOnlyChangeRebindsNewEdits(t *testing.T) {
	for _, change := range []string{"empty", "same_text", "round_trip"} {
		t.Run(change, func(t *testing.T) {
			server, original, _, native := editFixture(t, source.UTF16)
			server.ctx = t.Context()
			server.initialized = true
			server.changed = make(chan struct{})
			server.work = make(chan struct{}, 1)
			server.options.Log = io.Discard
			original.complete = true
			canceled := false
			server.workCancel = func() { canceled = true }
			sequence := func(value uint64) context.Context {
				return context.WithValue(t.Context(), receiveSequenceKey{}, value)
			}
			before, err := server.awaitComplete(sequence(10))
			if err != nil {
				t.Fatal(err)
			}
			uri, _ := PathToURI(original.names[editNativeKey])
			document := original.documents.Documents[uri]
			var changes []ContentChange
			switch change {
			case "same_text":
				changes = []ContentChange{{Text: document.Text}}
			case "round_trip":
				changes = []ContentChange{{Text: "temporary text"}, {Text: document.Text}}
			}
			params, _ := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri, "version": document.Version + 1}, "contentChanges": changes})
			if err := server.notification(sequence(11), "textDocument/didChange", params); err != nil {
				t.Fatal(err)
			}
			if server.epoch != original.epoch || canceled || len(server.work) != 0 {
				t.Fatalf("same bytes invalidated analysis: epoch=%d, canceled=%t, queued=%d", server.epoch, canceled, len(server.work))
			}
			if _, err := server.awaitComplete(sequence(10)); !isStaleEdit(err) {
				t.Fatalf("a queued old request acquired the new document version: %v", err)
			}
			current, err := server.awaitComplete(sequence(12))
			if err != nil {
				t.Fatal(err)
			}
			if current.result != original.result || current.goProjection != original.goProjection || current.indexes[editNativeKey] != original.indexes[editNativeKey] || current.epoch != original.epoch {
				t.Fatal("rebinding a document version replaced analysis, projection, or index")
			}
			if current.documents.Documents[uri].Version != document.Version+1 || current.documents.Documents[uri].Index != document.Index || before.documents.Documents[uri].Version != document.Version {
				t.Fatalf("new and retained snapshot versions: current=%+v before=%+v", current.documents.Documents[uri], before.documents.Documents[uri])
			}
			mapped, err := server.guardGoEdits(current, []goEdit{native})
			if err != nil || len(mapped.DocumentChanges) != 1 || mapped.DocumentChanges[0].TextDocument.Version == nil || *mapped.DocumentChanges[0].TextDocument.Version != document.Version+1 {
				t.Fatalf("new edit did not bind the current version: %+v, %v", mapped, err)
			}
			raw, _ := json.Marshal([]any{map[string]any{"label": "current", "textEdit": native.Edit}})
			completion, err := server.goCompletions(current, editNativeKey, raw)
			encoded, _ := json.Marshal(completion)
			if err != nil || !bytes.Contains(encoded, []byte(`"label":"current"`)) {
				t.Fatalf("new completion disappeared after a version-only edit: %s, %v", encoded, err)
			}
			// An in-flight backend response still carries its original immutable
			// snapshot and must not be rebound merely because its bytes match.
			if _, err := server.goCompletions(before, editNativeKey, raw); !isStaleEdit(err) {
				t.Fatalf("old in-flight completion was accepted: %v", err)
			}
			again, err := server.awaitComplete(sequence(13))
			if err != nil || again != current {
				t.Fatal("unchanged new requests repeatedly copied the document snapshot")
			}
			// The already-running analysis worker can publish its complete result
			// after the version notification. Its old revision must also rebind.
			late := *original
			server.publish(&late, nil)
			latest, err := server.awaitComplete(sequence(14))
			if err != nil || latest.documents.Documents[uri].Version != document.Version+1 {
				t.Fatalf("late publication revived an old document version: %+v, %v", latest, err)
			}
			if _, err := server.guardGoEdits(latest, []goEdit{native}); err != nil {
				t.Fatalf("late publication blocked a current edit: %v", err)
			}
		})
	}
}
