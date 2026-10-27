package lower_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
)

func TestMaterializeRejectsEditorOnlyProjection(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "input.gox")
	if err := os.WriteFile(original, []byte("incomplete source"), 0600); err != nil {
		t.Fatal(err)
	}
	result := &analysis.Result{ProjectionOnly: true, DiskNames: map[string]string{"input.gox": original}, DiskOutputs: map[string]string{"input.gox": filepath.Join(root, "output", "input.go")}}
	err := lower.Materialize(context.Background(), result, map[string][]byte{"input.gox": []byte("package fixture\n")}, lower.MaterializeOptions{RemoveSources: true})
	if err == nil || !strings.Contains(err.Error(), "editor-only projection") {
		t.Fatalf("editor projection accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "output")); !os.IsNotExist(err) {
		t.Fatalf("projection created output directory: %v", err)
	}
	if data, err := os.ReadFile(original); err != nil || string(data) != "incomplete source" {
		t.Fatalf("projection removed source: %s, %v", data, err)
	}
}
