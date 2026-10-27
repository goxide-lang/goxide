package compile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestNativePreparationSnapshotsAndLazyDependencies(t *testing.T) {
	root := t.TempDir()
	library := t.TempDir()
	writeProjectFile(t, library, "goxide.toml", "[go_mod]\nmodule='example.org/library'\ngo='1.27'\n[lib]\nname='library'\npath='src'\n")
	writeProjectFile(t, library, "src/lib.gox", "package library\npub func Value() int { return 7 }\n")
	mod := "module example.org/native\ngo 1.27\nrequire example.org/library v0.0.0\nreplace example.org/library => " + filepath.ToSlash(library) + "\n"
	writeProjectFile(t, root, "go.mod", mod)
	file := filepath.Join(root, "main.gox")
	writeProjectFile(t, root, "main.gox", "package main\nuse library::Value\nfunc main() { println(Value()) }\n")
	p, err := PrepareNative(t.Context(), root, NativeOptions{Automatic: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.plan.Manifest.Dependencies) != 0 {
		t.Fatal("candidate discovery activated unused source")
	}
	s, err := p.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := analysis.Analyze(s.Context, s.Input)
	s.Close()
	var demand *NativeRootDemand
	if !errors.As(err, &demand) {
		t.Fatalf("expected pre-check root demand, got %v", err)
	}
	if result != nil && result.Checked != nil {
		t.Fatal("dependency discovery reached semantic checking")
	}
	active, err := p.Activate(t.Context(), err, nil)
	if err != nil || !active {
		t.Fatalf("activate: %v, %v", active, err)
	}
	overlay := []byte("package main\nuse library::Value\nfunc main() { println(Value()+1) }\n")
	s, err = p.Snapshot(t.Context(), map[string][]byte{file: overlay})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	clear(overlay)
	result, err = analysis.Analyze(s.Context, s.Input)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := lower.Generate(s.Context, result, lower.GenerationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.SourceNames["example.org/native/main.gox"] != file {
		t.Fatalf("source identity: %v", s.SourceNames)
	}
	if !strings.Contains(string(outputs["example.org/native/main.gox"]), "Value() + 1") {
		t.Fatalf("overlay lost: %s", outputs["example.org/native/main.gox"])
	}
	if data, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || string(data) != mod {
		t.Fatalf("changed author module: %s, %v", data, err)
	}
}

func TestStandalonePreparationDiscoversAncestorRoots(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "goxide.roots.toml", "[roots.fmt]\npackage='fmt'\nsource='toolchain'\n")
	writeProjectFile(t, root, "nested/main.gox", "package main\nuse fmt::Println\nfunc main() { Println(42) }\n")
	input := filepath.Join(root, "nested/main.gox")
	p, err := PrepareNative(t.Context(), input, NativeOptions{Automatic: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	s, err := p.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := analysis.Analyze(s.Context, s.Input); err != nil {
		t.Fatal(err)
	}
	if s.SourceNames["goxide/standalone/main.gox"] != input {
		t.Fatalf("source identity: %v", s.SourceNames)
	}
}

func TestNativePreparationTargetGoOverlayAndClose(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root, "go.mod", "module example.org/overlay\ngo 1.27\n")
	writeProjectFile(t, root, "goxide.toml", "[codegen]\ngo_sugar=false\n")
	writeProjectFile(t, root, "native.go", "package library\nfunc Native() int { return 1 }\n")
	writeProjectFile(t, root, "value_linux_386.gox", "package library\npub func Value() int { return Native() }\n")
	writeProjectFile(t, root, "value_windows_amd64.gox", "this platform is not selected")
	writeProjectFile(t, root, "testdata/fixture.gox", "this fixture is not selected")
	p, err := PrepareNative(t.Context(), root, NativeOptions{Options: Options{Target: "linux/386"}, Automatic: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if cfg := p.Config(); cfg.Environment.Target.OS != "linux" || cfg.Environment.Target.Arch != "386" || cfg.GoSugar == nil || *cfg.GoSugar {
		t.Fatalf("lost selected configuration: %+v", cfg)
	}
	if filepath.Base(p.GoRoot()) != "module" || within(root, p.GoRoot()) {
		t.Fatalf("not a stable private Go root: %s", p.GoRoot())
	}
	filename := filepath.Join(root, "value_linux_386.gox")
	overlay := map[string][]byte{
		filepath.Join(root, "native.go"): []byte("package library\nfunc Native() string { return \"updated\" }\n"),
		filename:                         []byte("package library\npub func Value() string { return Native() }\n"),
	}
	first, err := p.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := p.Snapshot(t.Context(), overlay)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	failures := make(chan error, 2)
	for _, snapshot := range []*Snapshot{first, second} {
		go func() { _, err := analysis.Analyze(snapshot.Context, snapshot.Input); failures <- err }()
	}
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	finished := make(chan error, 1)
	go func() { finished <- p.Close() }()
	<-first.Context.Done()
	if _, err := os.Stat(p.GoRoot()); err != nil {
		t.Fatalf("removed retained root: %v", err)
	}
	first.Close()
	second.Close()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if _, err := p.Snapshot(t.Context(), nil); err == nil {
		t.Fatal("closed native project accepted a snapshot")
	}
}

func TestNativeDependencyPreparationCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	tool := filepath.Join(root, "go")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	environment.GoTool = tool
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = NativeSourceDependencies(ctx, root, filepath.Join(root, "consumer.mod"), nil, config.Config{Environment: environment}, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation not preserved: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %s", elapsed)
	}
}

func TestNativePreparationExplicitFileSelection(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "go.mod", "module example.org/selected\ngo 1.27\n")
	sums, err := os.ReadFile("../../go.sum")
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "go.sum", string(sums))
	main := filepath.Join(root, "value.gox")
	sibling := filepath.Join(root, "sibling.gox")
	native := filepath.Join(root, "native.go")
	mainText := []byte("package app\npub func Value() int { return sibling() + nativeSibling() }\n")
	siblingText := []byte("package app\nfunc sibling() int { return 1 }\n")
	nativeText := []byte("package app\nfunc nativeSibling() int { return 2 }\n")
	for name, data := range map[string][]byte{main: mainText, sibling: siblingText, native: nativeText} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, selection := range []string{"file", "directory", "provided-sources"} {
		t.Run(selection, func(t *testing.T) {
			input := main
			options := NativeOptions{}
			if selection == "directory" {
				input = root
			} else if selection == "provided-sources" {
				options.Sources = map[string][]byte{main: mainText, sibling: siblingText, native: nativeText}
			}
			p, err := PrepareNative(t.Context(), input, options)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if copied, err := os.ReadFile(filepath.Join(p.GoRoot(), "go.sum")); err != nil || string(copied) != string(sums) {
				t.Fatalf("private Go view lost authored checksums: %q, %v", copied, err)
			}
			selected := selection != "file"
			if selection == "directory" && p.NativeRoot() != root || selection == "file" && p.NativeRoot() != "" {
				t.Fatalf("wrong native backend root for %s: %q", selection, p.NativeRoot())
			}
			if file, module, packagePath := p.SingleFile(); selection == "file" && (file != main || module != "example.org/selected" || packagePath != module) {
				t.Fatalf("single-file import context: %q %q %q", file, module, packagePath)
			}
			if !p.OwnsTargetSource(main) || p.OwnsTargetSource(sibling) != selected || p.OwnsTargetSource(native) != selected {
				t.Fatalf("selection %s admitted the wrong source set", selection)
			}
			snapshot, err := p.Snapshot(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, analysisErr := analysis.Analyze(snapshot.Context, snapshot.Input)
			snapshot.Close()
			if selected && analysisErr != nil {
				t.Fatal(analysisErr)
			}
			if !selected && (analysisErr == nil || !strings.Contains(analysisErr.Error(), "unresolved name")) {
				t.Fatalf("single-file analysis accepted sibling declarations: %v", analysisErr)
			}
			if selected {
				return
			}
			snapshot, err = p.Snapshot(t.Context(), map[string][]byte{
				main:    []byte("package app\npub func Value() int { return 42 }\n"),
				sibling: []byte("invalid sibling overlay"),
				native:  []byte("invalid native sibling overlay"),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			if len(snapshot.SourceNames) != 1 {
				t.Fatalf("single-file source names include siblings: %v", snapshot.SourceNames)
			}
			for _, name := range snapshot.SourceNames {
				if name != main {
					t.Fatalf("selected file identity changed: %q", name)
				}
			}
			if _, err := analysis.Analyze(snapshot.Context, snapshot.Input); err != nil {
				t.Fatalf("selected overlay was lost or a sibling overlay was included: %v", err)
			}
		})
	}
}
