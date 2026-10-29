package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
)

func TestLanguageServerSavedCloseKeepsDisk(t *testing.T) {
	for _, mode := range []string{"single", "native", "manifest"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			original := "package main\nfunc value() int { return 1 }\nfunc main() { println(value()) }\n"
			saved := "package main\nfunc value() string { return \"now\" }\nfunc main() { println(value()) }\n"
			writeLanguageFile(t, root, "main.gox", original)
			input := root
			switch mode {
			case "single":
				input = filepath.Join(root, "main.gox")
			case "native":
				writeLanguageFile(t, root, "go.mod", "module example.org/lifecycle\n\ngo 1.27\n")
			case "manifest":
				writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/lifecycle'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n")
			}
			peer := startLanguageServer(t, input)
			uri, _ := PathToURI(filepath.Join(root, "main.gox"))
			peer.notify(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": original}})
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, original, "value()")}
			peer.call(t, "textDocument/hover", params)
			peer.notify(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []ContentChange{{Text: saved}}})
			params["position"] = languagePosition(t, saved, "value()")
			if got := peer.call(t, "textDocument/hover", params); !bytes.Contains(got, []byte("string")) {
				t.Fatalf("edited hover: %s", got)
			}
			writeLanguageFile(t, root, "main.gox", saved)
			peer.notify(t, "textDocument/didSave", map[string]any{"textDocument": map[string]string{"uri": uri}})
			peer.notify(t, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}})
			if got := peer.call(t, "textDocument/hover", params); !bytes.Contains(got, []byte("string")) {
				t.Fatalf("saved and closed source reverted to original snapshot: %s", got)
			}
		})
	}
}

// publish reads document versions under Server.mu. A document commit must not
// become visible before its invalidation epoch while that same lock is held.
func TestLanguageServerDocumentEpochAtomic(t *testing.T) {
	uri, _ := PathToURI(filepath.Join(t.TempDir(), "input.gox"))
	documents := NewDocumentStore(source.UTF16)
	if _, err := documents.Open(uri, 1, "1"); err != nil {
		t.Fatal(err)
	}
	s := &Server{initialized: true, epoch: 1, documents: documents, changed: make(chan struct{}), work: make(chan struct{}, 1), options: ServerOptions{Log: io.Discard}}
	done := make(chan error, 1)
	go func() {
		for version := 2; version <= 1000; version++ {
			data, err := json.Marshal(map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []ContentChange{{Text: fmt.Sprint(version)}}})
			if err == nil {
				err = s.notification(context.Background(), "textDocument/didChange", data)
			}
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	defer func() {
		// Always join the writer, including when an inconsistent snapshot fails.
		if done != nil {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	}()
	for {
		s.mu.Lock()
		runtime.Gosched()
		document, _ := s.documents.Get(uri)
		epoch := s.epoch
		s.mu.Unlock()
		if uint64(document.Version) != epoch {
			t.Fatalf("new document version %d visible under old epoch %d", document.Version, epoch)
		}
		select {
		case err := <-done:
			done = nil
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
}
