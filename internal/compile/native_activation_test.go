package compile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

func TestNativeLazyActivationPreservesAuthoredChecksums(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	library, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, library, "goxide.toml", "[go_mod]\nmodule='example.org/library'\ngo='1.27'\n[lib]\nname='library'\npath='src'\n")
	writeProjectFile(t, library, "src/lib.gox", "package library\npub func Value()int{return 7}\n")
	sums, err := os.ReadFile("../../go.sum")
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string][]byte{
		"go.mod":    []byte("module example.org/native\ngo 1.27\nrequire example.org/library v0.0.0\nreplace example.org/library => " + filepath.ToSlash(library) + "\n"),
		"go.sum":    sums,
		"main.gox":  []byte("package main\nuse library::Value\nfunc main(){println(Value()+nativeValue())}\n"),
		"native.go": []byte("package main\nfunc nativeValue()int{return 1}\n"),
	}
	for name, data := range originals {
		writeProjectFile(t, root, name, string(data))
	}
	// Check authored bytes after cleanup as well as after successful activation.
	assertOriginals := func() {
		t.Helper()
		for name, want := range originals {
			got, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("activation changed authored %s: %q, %v", name, got, err)
			}
		}
	}
	defer assertOriginals()
	for _, selection := range []string{"directory", "selected-sources"} {
		t.Run(selection, func(t *testing.T) {
			options := NativeOptions{Automatic: true}
			if selection == "selected-sources" {
				options.Sources = map[string][]byte{
					filepath.Join(root, "main.gox"):  originals["main.gox"],
					filepath.Join(root, "native.go"): originals["native.go"],
				}
			}
			p, err := PrepareNative(t.Context(), root, options)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if p.project != nil || within(root, p.directory) || !within(p.owner.Temporary, p.directory) {
				t.Fatalf("fixture did not start with an owned private native view: %q", p.directory)
			}
			projectedSum := filepath.Join(p.directory, "go.sum")
			if got, err := os.ReadFile(projectedSum); err != nil || len(got) == 0 || !bytes.Equal(got, p.sumBytes) {
				t.Fatalf("plain native view did not retain its prepared checksum copy: %v", err)
			}
			snapshot, err := p.Snapshot(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, demandErr := analysis.Analyze(snapshot.Context, snapshot.Input)
			snapshot.Close()
			var demand *NativeRootDemand
			if !errors.As(demandErr, &demand) {
				t.Fatalf("expected lazy source dependency activation: %v", demandErr)
			}
			if activated, err := p.Activate(t.Context(), demandErr, nil); err != nil || !activated {
				t.Fatalf("activate source dependency with authored checksums: %v, %v", activated, err)
			}
			if _, err := os.Stat(projectedSum); !os.IsNotExist(err) {
				t.Fatalf("synthesized Goxide authoring root retained private go.sum: %v", err)
			}
			assertOriginals()
			snapshot, err = p.Snapshot(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			result, err := analysis.Analyze(snapshot.Context, snapshot.Input)
			if err != nil {
				t.Fatal(err)
			}
			generated, err := lower.Generate(snapshot.Context, result, lower.GenerationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if err := typecheck.CheckGeneratedGraph(snapshot.Context, typecheck.GeneratedSources(result, generated), result.ExternalImporter(), result.Config.Sizes()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
