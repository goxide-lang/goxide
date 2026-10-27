package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMacroWorkSlotsRetainOwnershipAndUnchangedTimes(t *testing.T) {
	root, staged := t.TempDir(), t.TempDir()
	layout, err := OpenLayout(root, "example.org/app", "")
	if err != nil {
		t.Fatal(err)
	}
	defer layout.Close()
	spec := layoutSpec()
	spec.Kind = "proc-macro"
	unit, err := layout.Begin(spec)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(staged, "macro.wasm"), "first")
	write(filepath.Join(staged, "module/go.mod"), "module fixture\nreplace dependency => "+filepath.Join(staged, "deps/dependency")+"\n")
	if err := unit.PublishMacroWork(t.Context(), staged, true); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(unit.Intermediate, "macro/editor")
	artifact := filepath.Join(work, "macro.wasm")
	oldTime := time.Unix(1234567890, 0)
	if err := os.Chtimes(artifact, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := unit.PublishMacroWork(t.Context(), staged, true); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(artifact); err != nil || !info.ModTime().Equal(oldTime) {
		t.Fatalf("unchanged macro work mtime: %v, %v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(work, "module/go.mod")); err != nil || strings.Contains(string(data), staged) || !strings.Contains(string(data), work) {
		t.Fatalf("macro work retained temporary replace path: %q, %v", data, err)
	}
	if err := unit.PublishMacroWork(t.Context(), staged, false); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(staged, "macro.wasm"), "updated")
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := unit.PublishMacroWork(canceled, staged, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication: %v", err)
	}
	if data, _ := os.ReadFile(artifact); string(data) != "first" {
		t.Fatal("canceled publication modified the previous program")
	}
	if err := unit.PublishMacroWork(t.Context(), staged, true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(unit.Intermediate, "macro/cli/macro.wasm")); string(data) != "first" {
		t.Fatal("editor publication overwrote the CLI work slot")
	}
	write(artifact, "user change")
	if err := unit.PublishMacroWork(t.Context(), staged, true); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("modified macro work accepted: %v", err)
	}
	if data, _ := os.ReadFile(artifact); string(data) != "user change" {
		t.Fatal("ownership rejection changed user data")
	}
	if _, err := os.Stat(unit.Bundle); !os.IsNotExist(err) {
		t.Fatalf("macro work published a formal bundle: %v", err)
	}
}
