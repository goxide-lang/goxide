package lsp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/source"
)

func signaturePosition(t *testing.T, text, needle string) source.Position {
	t.Helper()
	offset := strings.LastIndex(text, needle)
	if offset < 0 {
		t.Fatal("missing signature position", needle)
	}
	position, err := source.New(text).Position(offset+len(needle), source.UTF16)
	if err != nil {
		t.Fatal(err)
	}
	return position
}
func TestLanguageServerSignatureHelpUsesCurrentSource(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "package main\r\nfunc add(value int, label string) int{return value}\r\nfunc main(){_=\"😀\";_=add(7, \"x\")}\r\n"
	name := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	peer := startLanguageServer(t, name)
	uri := openLanguageDocument(t, peer, name, text)
	params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": signaturePosition(t, text, "add(7, ")}
	raw := peer.call(t, "textDocument/signatureHelp", params)
	var help struct {
		ActiveParameter int `json:"activeParameter"`
		Signatures      []struct {
			Label      string `json:"label"`
			Parameters []any  `json:"parameters"`
		} `json:"signatures"`
	}
	if err := json.Unmarshal(raw, &help); err != nil {
		t.Fatal(err)
	}
	if help.ActiveParameter != 1 || len(help.Signatures) != 1 || help.Signatures[0].Label != "add(value int, label string) int" || len(help.Signatures[0].Parameters) != 2 {
		t.Fatalf("help=%s", raw)
	}
	edited := strings.ReplaceAll(text, "value int", "value float64")
	edited = strings.ReplaceAll(edited, ") int", ") float64")
	changeLanguageDocument(t, peer, uri, edited, 2)
	params["position"] = signaturePosition(t, edited, "add(7, ")
	raw = peer.call(t, "textDocument/signatureHelp", params)
	if !bytes.Contains(raw, []byte("value float64")) || bytes.Contains(raw, []byte("value int")) {
		t.Fatalf("stale help=%s", raw)
	}
}
func TestLanguageServerSignatureHelpForNativeGo(t *testing.T) {
	tool := realGopls(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/signatures\n\ngo 1.27\n")
	writeLanguageFile(t, root, "lib.gox", "package app\nfunc Add(value int, label string) int{return value}\n")
	text := "package app\r\nfunc Native()int{_=\"😀\";return Add(7, \"x\")}\r\n"
	name := filepath.Join(root, "native.go")
	writeLanguageFile(t, root, "native.go", text)
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, name, text)
	raw := peer.call(t, "textDocument/signatureHelp", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": signaturePosition(t, text, "Add(7, "), "context": map[string]any{"triggerKind": 1}})
	if !bytes.Contains(raw, []byte("Add(value int, label string) int")) || !bytes.Contains(raw, []byte(`"activeParameter":1`)) {
		t.Fatalf("native signature=%s diagnostics=%v log=%s", raw, peer.notifications, peer.log.String())
	}
}
