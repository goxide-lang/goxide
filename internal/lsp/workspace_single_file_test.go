package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
)

func TestWorkspaceExplicitFileSelection(t *testing.T) {
	for _, mode := range []string{"native", "manifest"} {
		for _, extension := range []string{".gox", ".go"} {
			t.Run(mode+extension, func(t *testing.T) {
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if mode == "native" {
					writeLanguageFile(t, root, "go.mod", "module example.org/selection\ngo 1.27\n")
				} else {
					writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/selection'\ngo='1.27'\n[lib]\nname='app'\npath='.'\n")
				}
				input := filepath.Join(root, "value"+extension)
				text := "package app\npub func Value() int { return sibling() + nativeSibling() }\n"
				if extension == ".go" {
					text = "package app\nfunc Value() int { return 42 }\n"
				}
				writeLanguageFile(t, root, "value"+extension, text)
				writeLanguageFile(t, root, "sibling.gox", "package app\nfunc sibling() int { return 1 }\n")
				writeLanguageFile(t, root, "native.go", "package app\nfunc nativeSibling() int { return 2 }\n")
				w, err := prepareWorkspace(t.Context(), input, compile.Options{Offline: true})
				if err != nil {
					t.Fatal(err)
				}
				defer w.close()
				if w.native == nil || w.project != nil {
					t.Fatal("explicit source selected its ancestor project target")
				}
				for _, name := range []string{"sibling.gox", "native.go"} {
					if w.ownsSource(filepath.Join(root, name)) {
						t.Fatalf("explicit source owns sibling %s", name)
					}
				}
				documents := NewDocumentStore("utf-16")
				snapshot, err := w.snapshot(t.Context(), documents.Snapshot())
				if err != nil {
					t.Fatal(err)
				}
				if len(snapshot.names) != 1 {
					t.Fatalf("explicit source names include siblings: %v", snapshot.names)
				}
				for _, name := range snapshot.names {
					if name != input {
						t.Fatalf("source identity lost: %q", name)
					}
				}
				if extension == ".gox" {
					_, err := analysis.Analyze(t.Context(), snapshot.input)
					if err == nil || !strings.Contains(err.Error(), "unresolved name") {
						t.Fatalf("explicit source analysis accepted sibling declarations: %v", err)
					}
				}
				snapshot.close()
				for _, name := range []string{"sibling.gox", "native.go"} {
					uri, err := PathToURI(filepath.Join(root, name))
					if err != nil {
						t.Fatal(err)
					}
					if _, err := documents.Open(uri, 1, "invalid sibling overlay"); err != nil {
						t.Fatal(err)
					}
				}
				uri, err := PathToURI(input)
				if err != nil {
					t.Fatal(err)
				}
				if extension == ".gox" {
					text = "package app\npub func Value() int { return 42 }\n"
				}
				if _, err := documents.Open(uri, 1, text); err != nil {
					t.Fatal(err)
				}
				snapshot, err = w.snapshot(t.Context(), documents.Snapshot())
				if err != nil {
					t.Fatal(err)
				}
				defer snapshot.close()
				if len(snapshot.names) != 1 {
					t.Fatalf("sibling overlay entered source snapshot: %v", snapshot.names)
				}
				if _, err := analysis.Analyze(t.Context(), snapshot.input); err != nil {
					t.Fatalf("selected overlay analysis: %v", err)
				}
				// Directory and explicit-manifest inputs keep the complete target.
				inputs := []string{root}
				if mode == "manifest" {
					inputs = append(inputs, filepath.Join(root, "goxide.toml"))
				}
				for _, directoryInput := range inputs {
					whole, err := prepareWorkspace(t.Context(), directoryInput, compile.Options{Offline: true})
					if err != nil {
						t.Fatal(err)
					}
					if mode == "manifest" && whole.project == nil {
						whole.close()
						t.Fatal("directory or manifest selection lost the project target")
					}
					for _, name := range []string{"value" + extension, "sibling.gox", "native.go"} {
						if !whole.ownsSource(filepath.Join(root, name)) {
							whole.close()
							t.Fatalf("directory selection excluded %s", name)
						}
					}
					whole.close()
				}
			})
		}
	}
}

func TestLanguageServerSingleFileNativeImports(t *testing.T) {
	tool := realGopls(t)
	for _, fixture := range []struct{ extension, module string }{{".gox", "example.org/selected/v2"}, {".go", "gopkg.in/selected.v1"}} {
		t.Run(fixture.extension, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(base, "consumer")
			mod := "module " + fixture.module + "\ngo 1.27\nrequire example.org/external v0.0.0\nreplace example.org/external => ../external\n"
			writeLanguageFile(t, root, "go.mod", mod)
			writeLanguageFile(t, base, "external/go.mod", "module example.org/external\ngo 1.27\n")
			writeLanguageFile(t, base, "external/external.go", "package external\nfunc Other() int { return 2 }\n")
			writeLanguageFile(t, root, "pkg/internal/leaf/leaf.go", "package leaf\nfunc Number() int { return 40 }\n")
			writeLanguageFile(t, root, "goxide.roots.toml", "[roots.leaf]\nmodule='"+fixture.module+"'\npackage='"+fixture.module+"/pkg/internal/leaf'\n[roots.external]\nmodule='example.org/external'\npackage='example.org/external'\n")
			// Neither source may enter the selected package or be masked on disk.
			poisonGo := "package client\nfunc Value() bool { return false }\n"
			poisonGox := "not valid Goxide source\n"
			writeLanguageFile(t, root, "pkg/client/sibling.go", poisonGo)
			writeLanguageFile(t, root, "pkg/client/sibling.gox", poisonGox)
			text := "package client\nuse leaf;\nuse external;\npub func Value() int { return leaf::Number() + external::Other() }\n"
			if fixture.extension == ".go" {
				text = "package client\nimport (leaf \"" + fixture.module + "/pkg/internal/leaf\"; external \"example.org/external\")\nfunc Value() int { return leaf.Number() + external.Other() }\n"
			}
			input := filepath.Join(root, "pkg/client/value"+fixture.extension)
			writeLanguageFile(t, root, "pkg/client/value"+fixture.extension, text)
			peer := startLanguageServerOptions(t, input, ServerOptions{Gopls: tool, Compiler: compile.Options{Offline: true}})
			uri := openLanguageDocument(t, peer, input, text)
			for _, query := range []struct{ needle, destination string }{{"Number()", filepath.Join(root, "pkg/internal/leaf/leaf.go")}, {"Other()", filepath.Join(base, "external/external.go")}} {
				params := map[string]any{"textDocument": map[string]string{"uri": uri}, "position": languagePosition(t, text, query.needle)}
				var definitions []wireLocation
				raw := peer.call(t, "textDocument/definition", params)
				if err := json.Unmarshal(raw, &definitions); err != nil {
					t.Fatal(err)
				}
				destination, err := PathToURI(query.destination)
				if err != nil {
					t.Fatal(err)
				}
				if len(definitions) != 1 || definitions[0].URI != destination {
					t.Fatalf("single-file import definition: %s; notifications=%v", raw, peer.notifications)
				}
				if fixture.extension == ".go" {
					if hover := peer.call(t, "textDocument/hover", params); !bytes.Contains(hover, []byte("int")) {
						t.Fatalf("single-file import hover: %s", hover)
					}
				}
			}
			if diagnostics := latestLanguageDiagnostics(t, peer, uri); len(diagnostics) != 0 {
				t.Fatalf("isolated valid file has diagnostics: %+v", diagnostics)
			}
			for name, original := range map[string]string{"go.mod": mod, "pkg/client/sibling.go": poisonGo, "pkg/client/sibling.gox": poisonGox} {
				if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != original {
					t.Fatalf("single-file view changed author file %s: %s, %v", name, data, err)
				}
			}
		})
	}
}

func TestWorkspaceCodegenManifestKeepsNativeDirectory(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/codegen\ngo 1.27\n")
	writeLanguageFile(t, root, "goxide.toml", "[codegen]\ngo_sugar=false\n")
	writeLanguageFile(t, root, "value.gox", "package app\npub func Value() int { return Native() }\n")
	writeLanguageFile(t, root, "native.go", "package app\nfunc Native() int { return 42 }\n")
	w, err := prepareWorkspace(t.Context(), filepath.Join(root, "goxide.toml"), compile.Options{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	if w.native == nil || w.native.NativeRoot() != root || w.native.Config().GoSugar == nil || *w.native.Config().GoSugar {
		t.Fatal("codegen manifest did not retain the native module configuration")
	}
	for _, name := range []string{"value.gox", "native.go"} {
		if !w.ownsSource(filepath.Join(root, name)) {
			t.Fatalf("codegen manifest excluded native source %s", name)
		}
	}
}
