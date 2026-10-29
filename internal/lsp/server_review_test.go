package lsp

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
)

func TestReviewSnapshotPreservesSelectedSourceBoundary(t *testing.T) {
	for _, mode := range []string{"native", "manifest", "standalone"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			input := root
			options := compile.Options{Offline: true}
			writeLanguageFile(t, root, "main.gox", "package main\nfunc main() {}\n")
			switch mode {
			case "native":
				writeLanguageFile(t, root, "go.mod", "module example.org/review\ngo 1.27\n")
			case "manifest":
				writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/review'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n[[bin]]\nname='other'\npath='other.gox'\n")
				writeLanguageFile(t, root, "other.gox", "package main\nfunc main() {}\n")
				options.Bin = "app"
			case "standalone":
				input = filepath.Join(root, "main.gox")
				writeLanguageFile(t, root, "other.gox", "package main\nfunc main() {}\n")
			}
			writeLanguageFile(t, root, "nested/go.mod", "module example.org/nested\ngo 1.27\n")
			writeLanguageFile(t, root, "nested/ignored.gox", "not selected disk source")
			writeLanguageFile(t, root, "owned_gox.go", source.GeneratedHeader+"package main\nfunc Owned() {}\n")
			w, err := prepareWorkspace(t.Context(), input, options)
			if err != nil {
				t.Fatal(err)
			}
			defer w.close()
			names := []string{"testdata/fixture.gox", ".hidden/file.gox", "_ignored.gox", "nested/ignored.gox", "owned_gox.go"}
			if mode != "native" {
				names = append(names, "other.gox")
			}
			for _, name := range names {
				t.Run(name, func(t *testing.T) {
					file := filepath.Join(root, name)
					uri, err := PathToURI(file)
					if err != nil {
						t.Fatal(err)
					}
					documents := NewDocumentStore("utf-16")
					if _, err := documents.Open(uri, 1, "not selected overlay source"); err != nil {
						t.Fatal(err)
					}
					snapshot, err := w.snapshot(t.Context(), documents.Snapshot())
					if err != nil {
						t.Fatal(err)
					}
					defer snapshot.close()
					snapshot.input.Options.Editor = true
					result, err := analysis.Analyze(t.Context(), snapshot.input)
					if err != nil {
						t.Fatal(err)
					}
					for key := range result.Sources {
						if snapshot.names[key] == file {
							t.Errorf("excluded document entered source snapshot: mode=%s source=%s key=%s analysis=%v", mode, name, key, result.Err)
						}
					}
				})
			}
		})
	}
}

func TestReviewNativeSnapshotCancelsExportImporter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/review\ngo 1.27\n")
	writeLanguageFile(t, root, "lib.gox", "package review\npub func Answer() int { return 42 }\n")
	writeLanguageFile(t, root, "native.go", "package review\nimport \"fmt\"\nfunc Native() string { return fmt.Sprint(Answer()) }\n")
	started := filepath.Join(t.TempDir(), "started")
	t.Setenv("GOXIDE_TEST_STARTED", started)
	t.Setenv("GOXIDE_TEST_REAL_GO", filepath.Join(runtime.GOROOT(), "bin", "go"))
	tool := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\ncase \" $* \" in\n  *\" -export \"*) : > \"$GOXIDE_TEST_STARTED\"; exec sleep 2;;\nesac\nexec \"$GOXIDE_TEST_REAL_GO\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	environment.GoTool = tool
	w, err := prepareWorkspace(t.Context(), root, compile.Options{Offline: true, Config: config.Config{Environment: environment}})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	snapshot, err := w.snapshot(ctx, NewDocumentStore(source.UTF16).Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	done := make(chan error, 1)
	go func() {
		_, err := analysis.Analyze(ctx, snapshot.input)
		done <- err
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("analysis did not reach the controlled exporter: %v", err)
		case <-deadline.C:
			t.Fatal("analysis did not start export command")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		<-done
		t.Fatal("snapshot cancellation did not interrupt native Go export import")
	}
}

func TestReviewInitialOverlaysPreserveSelectionAndNewFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/review'\ngo='1.27'\n[lib]\nname='review'\npath='src'\n[[bin]]\nname='other'\npath='other'\n")
	writeLanguageFile(t, root, "src/lib.gox", "package review\npub func Answer() int { return Extra() }\n")
	writeLanguageFile(t, root, "other/main.gox", "package main\nfunc main() {}\n")
	writeLanguageFile(t, root, "nested/go.mod", "module example.org/nested\ngo 1.27\n")
	writeLanguageFile(t, root, "nested/ignored.gox", "ignored disk source")
	writeLanguageFile(t, root, "src/owned_gox.go", source.GeneratedHeader+"package review\nfunc Owned() {}\n")
	ignored := []string{"testdata/fixture.gox", ".hidden/file.gox", "src/_ignored.gox", "nested/ignored.gox", "other/main.gox", "other/new.gox", "src/owned_gox.go"}
	overlays := map[string][]byte{filepath.Join(root, "src/new.gox"): []byte("package review\nfunc Extra() int { return 42 }\n")}
	documents := NewDocumentStore(source.UTF16)
	for _, relative := range ignored {
		overlays[filepath.Join(root, relative)] = []byte("not selected source")
	}
	for filename, content := range overlays {
		uri, err := PathToURI(filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := documents.Open(uri, 1, string(content)); err != nil {
			t.Fatal(err)
		}
	}
	w, err := prepareWorkspace(t.Context(), root, compile.Options{Offline: true, Lib: true, SourceOverlays: overlays})
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	snapshot, err := w.snapshot(t.Context(), documents.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	result, err := analysis.Analyze(t.Context(), snapshot.input)
	if err != nil {
		t.Fatal(err)
	}
	for key := range result.Sources {
		for _, relative := range ignored {
			if snapshot.names[key] == filepath.Join(root, relative) {
				t.Errorf("initial excluded overlay entered source snapshot: %s", relative)
			}
		}
	}
	generated, err := lower.Generate(t.Context(), result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(generated["example.org/review/src/new.gox"]) == 0 {
		t.Fatal("selected new source file was discarded")
	}
}
