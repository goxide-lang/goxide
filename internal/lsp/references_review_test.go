package lsp

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
)

func reviewRefactorCall(t *testing.T, peer *languagePeer, method string, params any) (json.RawMessage, *RPCError) {
	t.Helper()
	peer.next++
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": peer.next, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	peer.send(t, string(data))
	for {
		message := peer.receive(t)
		if len(message.ID) == 0 {
			peer.notifications = append(peer.notifications, message)
			continue
		}
		if string(message.ID) != fmt.Sprint(peer.next) {
			t.Fatalf("unexpected response: %+v", message)
		}
		return message.Result, message.Error
	}
}

func TestReviewRenamePreservesExplicitPublicGoName(t *testing.T) {
	tool := realGopls(t)
	for _, origin := range []string{"source", "native"} {
		t.Run(origin, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package main\npub func Answer()int{return 42}\n"
			native := "package main\nfunc main(){println(Answer())}\n"
			writeLanguageFile(t, root, "lib.gox", gox)
			writeLanguageFile(t, root, "main.go", native)
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
			position := languagePosition(t, gox, "Answer()")
			if origin == "native" {
				uri = openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
				position = languagePosition(t, native, "Answer()")
			}
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position, "newName": "answer"}
			raw, failure := reviewRefactorCall(t, peer, "textDocument/rename", params)
			if origin == "native" {
				if failure == nil || failure.Code != -32803 {
					t.Fatalf("native rename must reject a Go name incompatible with pub: %s; %+v", raw, failure)
				}
				return
			}
			if failure != nil {
				t.Fatalf("source rename preserving its public Go name failed: %+v", failure)
			}
			updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
			if string(raw) == "null" || len(updated) == 0 {
				t.Fatalf("rename returned no result: %s", raw)
			}
			if !strings.Contains(updated[filepath.Join(root, "lib.gox")], "pub func answer()") {
				t.Fatalf("successful rename did not update the authored identifier: %s", raw)
			}
			runRenamedProgram(t, updated, "42\n")
		})
	}
}

func TestReviewFieldReferencesRenameAcrossNativeSource(t *testing.T) {
	tool := realGopls(t)
	for _, origin := range []string{"source", "native"} {
		t.Run(origin, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package main\ntype Box struct{Value int}\nfunc Twice(b Box)int{return b.Value*2}\n"
			native := "package main\nfunc main(){b:=Box{Value:14};println(b.Value+Twice(b))}\n"
			writeLanguageFile(t, root, "lib.gox", gox)
			writeLanguageFile(t, root, "main.go", native)
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
			position := languagePosition(t, gox, "Value*2")
			if origin == "native" {
				uri = openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
				position = languagePosition(t, native, "Value+Twice")
			}
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position, "context": map[string]bool{"includeDeclaration": true}}
			var references []wireLocation
			if err := json.Unmarshal(peer.call(t, "textDocument/references", params), &references); err != nil || len(references) != 4 {
				t.Fatalf("field references: %#v; %v", references, err)
			}
			params["newName"] = "Count"
			raw := peer.call(t, "textDocument/rename", params)
			updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
			runRenamedProgram(t, updated, "42\n")
		})
	}
}

func TestRenameExplicitPublicLowercaseUpdatesNativeCalls(t *testing.T) {
	tool := realGopls(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gox := "package main\npub func alpha()int{return 21}\nfunc source()int{return alpha()}\n"
	native := "package main\nfunc main(){println(Alpha()+source())}\n"
	writeLanguageFile(t, root, "lib.gox", gox)
	writeLanguageFile(t, root, "main.go", native)
	writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
	raw := peer.call(t, "textDocument/rename", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, gox, "alpha()}"), "newName": "beta"})
	updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
	if !strings.Contains(updated[filepath.Join(root, "lib.gox")], "pub func beta()") || !strings.Contains(updated[filepath.Join(root, "main.go")], "Beta()+source()") {
		t.Fatalf("rename lost source or generated name: %s", raw)
	}
	runRenamedProgram(t, updated, "42\n")
}

func TestNativeRenameAllowsGoIdentifierReservedOnlyInGoxide(t *testing.T) {
	tool := realGopls(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gox := "package main\nfunc Source()int{return 42}\n"
	native := "package main\nfunc main(){value:=Source();println(value)}\n"
	writeLanguageFile(t, root, "lib.gox", gox)
	writeLanguageFile(t, root, "main.go", native)
	writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
	peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
	uri := openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
	raw := peer.call(t, "textDocument/rename", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, native, "value:=Source"), "newName": "match"})
	updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
	if !strings.Contains(updated[filepath.Join(root, "main.go")], "match:=Source();println(match)") {
		t.Fatalf("native rename: %s", raw)
	}
	runRenamedProgram(t, updated, "42\n")
}

func TestNativeRenameReplansPublicSourceSpelling(t *testing.T) {
	tool := realGopls(t)
	for _, test := range []struct{ name, gox, native, selected, newName, declaration string }{
		{"function", "package main\npub func alpha()int{return 21}\nfunc Source()int{return alpha()}\n", "package main\nfunc main(){println(Alpha()+Source())}\n", "Alpha()+", "Beta", "pub func Beta()"},
		{"field", "package main\ntype Box struct{pub value int}\nfunc Source(b Box)int{c:=Box{value:b.value};return c.value}\n", "package main\nfunc main(){b:=Box{Value:21};println(b.Value+Source(b))}\n", "Value+", "Count", "pub Count int"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			writeLanguageFile(t, root, "lib.gox", test.gox)
			writeLanguageFile(t, root, "main.go", test.native)
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "main.go"), test.native)
			raw := peer.call(t, "textDocument/rename", map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, test.native, test.selected), "newName": test.newName})
			updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): test.gox, filepath.Join(root, "main.go"): test.native}, raw)
			if !strings.Contains(updated[filepath.Join(root, "lib.gox")], test.declaration) {
				t.Fatalf("missing replanned source name: %s", raw)
			}
			runRenamedProgram(t, updated, "42\n")
		})
	}
}
