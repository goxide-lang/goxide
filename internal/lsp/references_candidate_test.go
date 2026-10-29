package lsp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
)

func TestRenameRejectsCandidateSourceNameCollisions(t *testing.T) {
	tool := realGopls(t)
	for _, sourceCall := range []bool{false, true} {
		name, expression := "native references only", "21"
		if sourceCall {
			name, expression = "source and native references", "Foo()"
		}
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package main\nstruct A{}\nfunc Source()int{return " + expression + "}\n"
			native := "package main\nfunc Foo()int{return 21}\nfunc main(){println(Foo()+Source())}\n"
			texts := map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}
			for filename, text := range texts {
				writeLanguageFile(t, root, filepath.Base(filename), text)
			}
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			runRenamedProgram(t, texts, "42\n")
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
			raw, failure := reviewRefactorCall(t, peer, "textDocument/rename", map[string]any{
				"textDocument": map[string]string{"uri": uri},
				"position":     languagePosition(t, native, "Foo()+"),
				"newName":      "A",
			})
			// Go sees the private source type as _A, so gopls alone accepts
			// this name even though it duplicates the source declaration A.
			if failure == nil || failure.Code != -32803 {
				t.Fatalf("rename must reject the candidate's source collision: %s; %+v", raw, failure)
			}
		})
	}
}

func TestRenameNativeDeclarationAcrossLanguages(t *testing.T) {
	tool := realGopls(t)
	for _, origin := range []string{"source", "native"} {
		t.Run(origin, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			gox := "package main\nfunc Source()int{return Native()}\n"
			native := "package main\nfunc Native()int{return 21}\nfunc main(){println(Native()+Source())}\n"
			writeLanguageFile(t, root, "lib.gox", gox)
			writeLanguageFile(t, root, "main.go", native)
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
			position := languagePosition(t, gox, "Native()")
			if origin == "native" {
				uri = openLanguageDocument(t, peer, filepath.Join(root, "main.go"), native)
				position = languagePosition(t, native, "Native()+")
			}
			params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position, "context": map[string]bool{"includeDeclaration": true}}
			var refs []wireLocation
			if err := json.Unmarshal(peer.call(t, "textDocument/references", params), &refs); err != nil || len(refs) != 3 {
				t.Fatalf("native declaration references: %+v, %v", refs, err)
			}
			params["newName"] = "Total"
			raw := peer.call(t, "textDocument/rename", params)
			updated := applyRenameWorkspace(t, map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}, raw)
			runRenamedProgram(t, updated, "42\n")
		})
	}
}

func TestRenameExplicitPublicFieldUpdatesNativeCalls(t *testing.T) {
	tool := realGopls(t)
	for _, keyedLiteral := range []bool{false, true} {
		for _, newName := range []string{"count", "Value"} {
			name, body := "selectors/", "return b.value"
			if keyedLiteral {
				name, body = "keyed literal/", "c:=Box{value:b.value};return c.value"
			}
			t.Run(name+newName, func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				gox := "package main\ntype Box struct{pub value int}\nfunc Source(b Box)int{" + body + "}\n"
				native := "package main\nfunc main(){b:=Box{Value:21};println(b.Value+Source(b))}\n"
				texts := map[string]string{filepath.Join(root, "lib.gox"): gox, filepath.Join(root, "main.go"): native}
				writeLanguageFile(t, root, "lib.gox", gox)
				writeLanguageFile(t, root, "main.go", native)
				writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
				runRenamedProgram(t, texts, "42\n")
				peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
				uri := openLanguageDocument(t, peer, filepath.Join(root, "lib.gox"), gox)
				params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, gox, "value}")}
				prepared := peer.call(t, "textDocument/prepareRename", params)
				if !bytes.Contains(prepared, []byte(`"placeholder":"value"`)) {
					t.Fatalf("public field prepareRename did not preserve its source name: %s", prepared)
				}
				params["newName"] = newName
				raw := peer.call(t, "textDocument/rename", params)
				updated := applyRenameWorkspace(t, texts, raw)
				goName := "Count"
				if newName == "Value" {
					goName = "Value"
				}
				if !strings.Contains(updated[filepath.Join(root, "lib.gox")], "pub "+newName+" int") || !strings.Contains(updated[filepath.Join(root, "lib.gox")], "."+newName+"}") || !strings.Contains(updated[filepath.Join(root, "main.go")], "Box{"+goName+":21};println(b."+goName+"+") {
					t.Fatalf("rename lost a source field spelling or its native Go name: %s", raw)
				}
				if keyedLiteral && !strings.Contains(updated[filepath.Join(root, "lib.gox")], "Box{"+newName+":") {
					t.Fatalf("rename omitted the source literal's field key: %s", raw)
				}
				runRenamedProgram(t, updated, "42\n")
			})
		}
	}
}

func TestNativeRenamePreservesPackageEntryPoints(t *testing.T) {
	tool := realGopls(t)
	for _, test := range []struct{ name, gox, native, selected, newName, output string }{
		{"source main", "package main\nfunc main(){println(42)}\n", "package main\nfunc invoke(){main()}\n", "main()", "Entry", "42\n"},
		{"native main", "package main\nfunc Source()int{return 42}\n", "package main\nfunc main(){println(Source())}\n", "main()", "Entry", "42\n"},
		{"native init", "package main\nfunc Source()int{return 42}\n", "package main\nfunc init(){println(1)}\nfunc main(){println(Source())}\n", "init()", "Initialize", "1\n42\n"},
		{"introduce init", "package main\nfunc Source()int{return 42}\n", "package main\nfunc helper(){println(1)}\nfunc main(){println(Source())}\n", "helper()", "init", "42\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			writeLanguageFile(t, root, "go.mod", "module example.org/refactor\n\ngo 1.27\n")
			writeLanguageFile(t, root, "lib.gox", test.gox)
			writeLanguageFile(t, root, "native.go", test.native)
			runRenamedProgram(t, map[string]string{filepath.Join(root, "lib.gox"): test.gox, filepath.Join(root, "native.go"): test.native}, test.output)
			peer := startLanguageServerOptions(t, root, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, filepath.Join(root, "native.go"), test.native)
			raw, failure := reviewRefactorCall(t, peer, "textDocument/rename", map[string]any{
				"textDocument": map[string]string{"uri": uri},
				"position":     languagePosition(t, test.native, test.selected),
				"newName":      test.newName,
			})
			if failure == nil || failure.Code != -32803 {
				t.Fatalf("rename changed package entry semantics: %s; %+v", raw, failure)
			}
		})
	}
}
