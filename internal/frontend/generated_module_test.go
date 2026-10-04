package frontend

import (
	"context"
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedModuleGoxideAndReservedSidecar(t *testing.T) {
	root := t.TempDir()
	if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/generated\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	src := []byte("package generated\nfunc Value() int { return 42 }\n")
	destination := filepath.Join(t.TempDir(), "module")
	result, err := CompileModuleWithOptions(root, destination, ModuleOptions{GeneratedFiles: map[string][]byte{"value.gox": src}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(result.Root, "value.go"))
	if err != nil || !strings.Contains(string(b), "42") {
		t.Fatalf("generated goxide lowering: %s %v", b, err)
	}
	_, err = CompileModuleWithOptions(root, filepath.Join(t.TempDir(), "module"), ModuleOptions{GeneratedFiles: map[string][]byte{"value.gox": src, "value.go.map.json": []byte("resource")}})
	if err == nil || !strings.Contains(err.Error(), "source-map") {
		t.Fatalf("reserved sidecar accepted: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = CompileModuleWithOptions(root, filepath.Join(t.TempDir(), "module"), ModuleOptions{Context: cancelled})
	if err != context.Canceled {
		t.Fatalf("cancelled module accepted: %v", err)
	}
}
