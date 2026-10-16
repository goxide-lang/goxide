package typecheck

import (
	"bytes"
	"context"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImporterWithModFileUsesSourceGraph(t *testing.T) {
	root := t.TempDir()
	consumer := filepath.Join(root, "consumer")
	dependency := filepath.Join(root, "dependency")
	const module = "module importer.example/consumer\n\ngo 1.27\n\nrequire importer.example/dependency v0.0.0\nreplace importer.example/dependency => ../dependency\n"
	const sums = "importer.example/unused v0.0.0 h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n"
	files := map[string]string{
		"consumer/go.mod":                module,
		"consumer/go.sum":                sums,
		"consumer/cmd/input.gox":         "package main\nfunc main() {}\n",
		"consumer/cmd/broken_sibling.go": "this sibling must never be selected\n",
		"consumer/native/native.go":      "package native\nimport (\"importer.example/dependency\"; \"importer.example/consumer/internal/leaf\")\ntype Value = dependency.Value\nvar Hidden leaf.Value\n",
		"consumer/internal/leaf/leaf.go": "package leaf\ntype Value struct { N int }\n",
		"dependency/go.mod":              "module importer.example/dependency\n\ngo 1.27\n",
		"dependency/value.go":            "package dependency\ntype Value struct { N int }\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Keep relative replacements unchanged and the alternate checksum file owned
	// by the caller, outside the source tree.
	owned := t.TempDir()
	modFile := filepath.Join(owned, "consumer.mod")
	if err := os.WriteFile(modFile, []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "consumer.sum"), []byte(sums), 0600); err != nil {
		t.Fatal(err)
	}
	environment := append(os.Environ(), "GOFLAGS=-goxide-invalid-ambient-flag", "GOWORK="+filepath.Join(root, "missing.work"))
	cfg := testConfig(t, environment)
	imp := NewImporter(filepath.Join(consumer, "cmd", "input.gox"), ImporterOptions{Context: context.Background(), Config: cfg, ModFile: modFile})
	native, err := imp.Import("importer.example/consumer/native")
	if err != nil {
		t.Fatal(err)
	}
	dep, err := imp.Import("importer.example/dependency")
	if err != nil {
		t.Fatal(err)
	}
	if !types.Identical(native.Scope().Lookup("Value").Type(), dep.Scope().Lookup("Value").Type()) {
		t.Fatal("relative replacement lost canonical type identity")
	}
	again, err := imp.Import("importer.example/dependency")
	if err != nil || again != dep {
		t.Fatalf("importer cache lost package identity: %v", err)
	}
	provider, ok := imp.(interface {
		PackageFiles(string) (string, []string, error)
		PackageFingerprint(string) (string, error)
	})
	if !ok {
		t.Fatal("module importer has no artifact source provider")
	}
	dir, names, err := provider.PackageFiles("importer.example/dependency")
	if err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(dependency)
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil || dir != wantDir || len(names) != 1 || names[0] != "value.go" {
		t.Fatalf("wrong replacement source selection: %q %v (%v)", dir, names, err)
	}
	fingerprint, err := provider.PackageFingerprint("importer.example/consumer/native")
	if err != nil || len(fingerprint) != 64 {
		t.Fatalf("source fingerprint unavailable: %q (%v)", fingerprint, err)
	}
	repeated, err := provider.PackageFingerprint("importer.example/consumer/native")
	if err != nil || repeated != fingerprint {
		t.Fatalf("unstable cached fingerprint: %q (%v)", repeated, err)
	}
	// A fresh importer must resolve the same source closure without the first
	// importer's in-memory cache or changes to the ambient environment.
	t.Setenv("GOARCH", "386")
	fresh := NewImporter(filepath.Join(consumer, "cmd", "input.gox"), ImporterOptions{Context: context.Background(), Config: cfg, ModFile: modFile})
	freshProvider := fresh.(interface{ PackageFingerprint(string) (string, error) })
	if got, err := freshProvider.PackageFingerprint("importer.example/consumer/native"); err != nil || got != fingerprint {
		t.Fatalf("fresh source fingerprint differs: %q (%v)", got, err)
	}
	if _, err := imp.Import("importer.example/not-required"); err == nil || !strings.Contains(err.Error(), "-mod=readonly") {
		t.Fatalf("missing requirement did not fail in readonly mode: %v", err)
	}
	for name, contents := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(data, []byte(contents)) {
			t.Errorf("source file changed: %s (%v)", name, err)
		}
	}
	if data, err := os.ReadFile(modFile); err != nil || !bytes.Equal(data, []byte(module)) {
		t.Fatalf("owned module graph changed: %s (%v)", data, err)
	}
}

func TestImporterWithModFileRequiresAbsolutePath(t *testing.T) {
	imp := NewImporter("input.gox", ImporterOptions{Config: testConfig(t, nil), ModFile: "relative.mod"})
	if _, err := imp.Import("fmt"); err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("relative module file was accepted: %v", err)
	}
}
