package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/source"
)

type languagePeer struct {
	*rpcPeer
	next          int
	notifications []rpcMessage
	log           *bytes.Buffer
}

func startLanguageServer(t *testing.T, input string) *languagePeer {
	return startLanguageServerOptions(t, input, ServerOptions{Compiler: compile.Options{Offline: true}, Gopls: "gopls-unavailable-for-source-query-test"})
}

func startLanguageServerOptions(t *testing.T, input string, options ServerOptions) *languagePeer {
	t.Helper()
	in, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &languagePeer{rpcPeer: &rpcPeer{input: writer, output: reader, reader: bufio.NewReader(reader), done: make(chan struct{})}, log: new(bytes.Buffer)}
	// Initial project preparation may include a cold Go/WASM compilation.
	// Warm interactive budgets are measured separately in performance_test.go.
	p.readTimeout = 30 * time.Second
	options.Directory, options.Log = input, p.log
	go func() {
		p.err = Serve(t.Context(), in, out, options)
		close(p.done)
	}()
	t.Cleanup(func() {
		writer.Close()
		reader.Close()
		awaitRPC(t, p.done)
		if p.err != nil && !errors.Is(p.err, context.Canceled) {
			t.Errorf("server stopped: %v\n%s", p.err, p.log.String())
		}
	})
	p.call(t, "initialize", map[string]any{"capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}}})
	p.notify(t, "initialized", map[string]any{})
	return p
}
func (p *languagePeer) notify(t *testing.T, method string, params any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	p.send(t, string(data))
}
func (p *languagePeer) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	p.next++
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": p.next, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	p.send(t, string(data))
	for {
		message := p.receive(t)
		if len(message.ID) == 0 {
			p.notifications = append(p.notifications, message)
			continue
		}
		if string(message.ID) != fmt.Sprint(p.next) {
			t.Fatalf("unexpected response: %+v", message)
		}
		if message.Error != nil {
			t.Fatalf("%s: %+v", method, message.Error)
		}
		return message.Result
	}
}
func writeLanguageFile(t *testing.T, root, name, text string) {
	t.Helper()
	filename := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func languagePosition(t *testing.T, text, needle string) source.Position {
	t.Helper()
	offset := strings.LastIndex(text, needle)
	if offset < 0 {
		t.Fatalf("no %s in %s", needle, text)
	}
	position, err := source.New(text).Position(offset+1, source.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	return position
}

func TestLanguageServerCurrentSourceQueries(t *testing.T) {
	for _, mode := range []string{"single", "native", "manifest"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			text := "package main\r\nfunc value() int { return 41 }\r\nfunc main() { _ = \"😀\"; println(value()) }\r\n"
			writeLanguageFile(t, root, "main.gox", text)
			input := root
			switch mode {
			case "single":
				input = filepath.Join(root, "main.gox")
			case "native":
				writeLanguageFile(t, root, "go.mod", "module example.org/language\n\ngo 1.27\n")
			case "manifest":
				writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/language'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n")
			}
			peer := startLanguageServer(t, input)
			uri, _ := PathToURI(filepath.Join(root, "main.gox"))
			peer.notify(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": text}})
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, text, "value()")}
			hover := peer.call(t, "textDocument/hover", params)
			if !bytes.Contains(hover, []byte("int")) {
				t.Fatalf("hover lost current signature: %s; diagnostics=%v", hover, peer.notifications)
			}
			definitions := peer.call(t, "textDocument/definition", params)
			var locations []wireLocation
			if err := json.Unmarshal(definitions, &locations); err != nil {
				t.Fatal(err)
			}
			if len(locations) != 1 || locations[0].URI != uri || locations[0].Range.Start.Line != 1 {
				t.Fatalf("definition: %s", definitions)
			}
			symbols := peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": uri}})
			if !bytes.Contains(symbols, []byte(`"name":"value"`)) || !bytes.Contains(symbols, []byte(`"name":"main"`)) {
				t.Fatalf("document symbols: %s", symbols)
			}
			edited := strings.Replace(text, "int { return 41 }", "string { return \"now\" }", 1)
			peer.notify(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []ContentChange{{Text: edited}}})
			params["position"] = languagePosition(t, edited, "value()")
			hover = peer.call(t, "textDocument/hover", params)
			if !bytes.Contains(hover, []byte("string")) || bytes.Contains(hover, []byte("int")) {
				t.Fatalf("new signature returned old facts: %s", hover)
			}
			completion := peer.call(t, "textDocument/completion", params)
			if !bytes.Contains(completion, []byte(`"label":"value"`)) {
				t.Fatalf("completion: %s", completion)
			}
			disk, err := os.ReadFile(filepath.Join(root, "main.gox"))
			if err != nil || string(disk) != text {
				t.Fatalf("language service wrote source: %v, %s", err, disk)
			}
			peer.call(t, "shutdown", nil)
			peer.notify(t, "exit", nil)
			awaitRPC(t, peer.done)
		})
	}
}

func TestLanguageServerCompleteGoDiagnostics(t *testing.T) {
	root := t.TempDir()
	text := "package main\nfunc value() int {}\nfunc main() {}\n"
	writeLanguageFile(t, root, "main.gox", text)
	peer := startLanguageServer(t, filepath.Join(root, "main.gox"))
	uri, _ := PathToURI(filepath.Join(root, "main.gox"))
	peer.notify(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1, "text": text}})
	peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": uri}})
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, message := range peer.notifications {
			if message.Method == "textDocument/publishDiagnostics" && bytes.Contains(message.Params, []byte("missing return")) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("full Go check omitted missing return: %+v", peer.notifications)
		}
		peer.notifications = append(peer.notifications, peer.receive(t))
	}
}
