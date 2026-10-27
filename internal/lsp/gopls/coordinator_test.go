package gopls

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func coordinatorSnapshot(t *testing.T, c *Coordinator, epoch uint64) helperSnapshot {
	t.Helper()
	result, err := c.Query(t.Context(), epoch, "test/snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot helperSnapshot
	if err := json.Unmarshal(result, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestCoordinatorOnlySendsChangesAndMasksOwnedDisk(t *testing.T) {
	c := NewCoordinator(startHelper(t, "normal", nil))
	const a, b, ghost, old = "file:///a.go", "file:///b.go", "file:///virtual.go", "file:///old.go"
	p := Projection{
		Epoch:  1,
		Files:  map[string]string{a: "package p\nconst A = 1\n", b: "package p\nconst B = 1\n", ghost: "package p\n"},
		Masked: map[string]string{a: "p", old: "p"},
	}
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	initial := coordinatorSnapshot(t, c, 1)
	if len(initial.Changes) != 4 || initial.Files[a].Text != p.Files[a] || !strings.Contains(initial.Files[old].Text, "ignore && !ignore") {
		t.Fatalf("initial complete set: %+v", initial)
	}
	p.Epoch = 2
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	unchanged := coordinatorSnapshot(t, c, 2)
	if len(unchanged.Changes) != len(initial.Changes) || unchanged.Files[a].Version != initial.Files[a].Version {
		t.Fatalf("unchanged bytes were sent again: %+v", unchanged)
	}
	delete(p.Files, a)
	delete(p.Files, ghost)
	p.Epoch = 3
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	deleted := coordinatorSnapshot(t, c, 3)
	if len(deleted.Changes) != 6 || !strings.Contains(deleted.Files[a].Text, "ignore && !ignore") || deleted.Files[b].Version != initial.Files[b].Version {
		t.Fatalf("delete delta: %+v", deleted)
	}
	if _, exists := deleted.Files[ghost]; exists {
		t.Fatal("overlay-only deleted file remained open")
	}
	for _, change := range deleted.Changes[len(initial.Changes):] {
		if change.URI == a && change.Method != "textDocument/didChange" {
			t.Fatalf("owned disk file was closed instead of masked: %+v", change)
		}
	}
	p.Epoch = 4
	p.Files[ghost] = "package p\n"
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	reopened := coordinatorSnapshot(t, c, 4)
	if reopened.Files[ghost].Version <= initial.Files[ghost].Version {
		t.Fatalf("reopened URI version did not increase: %d <= %d", reopened.Files[ghost].Version, initial.Files[ghost].Version)
	}
}

func TestCoordinatorInvalidatesBlockedQueryAndIgnoresLateReply(t *testing.T) {
	events := make(chan Notification, 8)
	client := startHelper(t, "ignore-cancel", func(ctx context.Context, n Notification) {
		select {
		case events <- n:
		case <-ctx.Done():
		}
	})
	c := NewCoordinator(client)
	if err := c.Sync(t.Context(), Projection{Epoch: 1, Files: map[string]string{"file:///a.go": "package p\n"}}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := c.Query(t.Context(), 1, "test/hold", nil)
		result <- err
	}()
	awaitNotification(t, events, func(n Notification) bool { return n.Method == "test/holding" })
	c.Invalidate(2)
	select {
	case err := <-result:
		if !errors.Is(err, ErrStale) {
			t.Fatalf("old query: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalidation waited for the old server response")
	}
	if _, err := c.Query(t.Context(), 2, "test/echo", nil); !errors.Is(err, ErrStale) {
		t.Fatalf("unsynchronized current epoch: %v", err)
	}
	if err := c.Sync(t.Context(), Projection{Epoch: 2, Files: map[string]string{"file:///a.go": "package p\nconst New = 1\n"}}); err != nil {
		t.Fatal(err)
	}
	value, err := c.Query(t.Context(), 2, "test/release", nil)
	if err != nil || string(value) != `"released"` {
		t.Fatalf("late reply was associated with the new epoch: %s, %v", value, err)
	}
	if err := c.Sync(t.Context(), Projection{Epoch: 1}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale sync was accepted: %v", err)
	}
	if got := coordinatorSnapshot(t, c, 2).Files["file:///a.go"].Text; !strings.Contains(got, "const New") {
		t.Fatalf("stale synchronization overwrote current files: %q", got)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Query(t.Context(), 2, "test/echo", nil); err == nil {
		t.Fatal("terminated process still accepted queries")
	}
}

func TestCoordinatorDiagnosticsDiscardsIncompleteBatch(t *testing.T) {
	events := make(chan Notification, 8)
	c := NewCoordinator(startHelper(t, "hold-diagnostics", func(ctx context.Context, n Notification) {
		select {
		case events <- n:
		case <-ctx.Done():
		}
	}))
	files := map[string]string{"file:///a.go": "package p\n", "file:///b.go": "package p\n"}
	if err := c.Sync(t.Context(), Projection{Epoch: 1, Files: files}); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		values map[string]json.RawMessage
		err    error
	}
	result := make(chan outcome, 1)
	go func() {
		values, err := c.Diagnostics(t.Context(), 1, []string{"file:///a.go", "file:///b.go"})
		result <- outcome{values, err}
	}()
	awaitNotification(t, events, func(n Notification) bool { return n.Method == "test/holding" })
	c.Invalidate(2)
	select {
	case got := <-result:
		if !errors.Is(got.err, ErrStale) || got.values != nil {
			t.Fatalf("obsolete diagnostic batch: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("obsolete diagnostic batch did not stop")
	}
	if err := c.Sync(t.Context(), Projection{Epoch: 2, Files: files}); err != nil {
		t.Fatal(err)
	}
	snapshot := coordinatorSnapshot(t, c, 2)
	if len(snapshot.Diagnostics) != 1 || snapshot.Diagnostics[0] != "file:///a.go" {
		t.Fatalf("obsolete batch continued requesting later files: %+v", snapshot.Diagnostics)
	}
}

func TestGoplsCoordinatorDeletionSurvivesRestart(t *testing.T) {
	main := "package main\nimport \"example.test/lsp/p\"\nvar Value int = p.Value\nfunc main() {}\n"
	generated := "package p\nconst Value = 7\n"
	client, root, _ := startGopls(t, map[string]string{"main.go": main, "p/generated_gox.go": generated})
	mainURI := fileURI(filepath.Join(root, "main.go"))
	generatedPath := filepath.Join(root, "p/generated_gox.go")
	generatedURI := fileURI(generatedPath)
	c := NewCoordinator(client)
	p := Projection{Epoch: 1, Files: map[string]string{mainURI: main, generatedURI: generated}, Masked: map[string]string{generatedURI: "p"}}
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if diagnostics := coordinatedDiagnostics(t, c, 1, mainURI); len(diagnostics) != 0 {
		t.Fatalf("initial projection: %+v", diagnostics)
	}
	p.Epoch = 2
	delete(p.Files, generatedURI)
	c.Invalidate(2)
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if diagnostics := coordinatedDiagnostics(t, c, 2, mainURI); !diagnosticContains(diagnostics, "could not import") {
		t.Fatalf("deleted last generated package: %+v", diagnostics)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := client.Close(ctx); err != nil {
		t.Fatal(err)
	}
	restarted, _, _ := startGoplsAt(t, root)
	c = NewCoordinator(restarted)
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if diagnostics := coordinatedDiagnostics(t, c, 2, mainURI); !diagnosticContains(diagnostics, "could not import") {
		t.Fatalf("restart restored stale disk output: %+v", diagnostics)
	}
	p.Epoch = 3
	p.Files[generatedURI] = generated
	if err := c.Sync(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if diagnostics := coordinatedDiagnostics(t, c, 3, mainURI); len(diagnostics) != 0 {
		t.Fatalf("current projection did not replace tombstone: %+v", diagnostics)
	}
	if data, err := os.ReadFile(generatedPath); err != nil || string(data) != generated {
		t.Fatalf("coordinator mutated disk output: %q, %v", data, err)
	}
}

func coordinatedDiagnostics(t *testing.T, c *Coordinator, epoch uint64, uri string) []diagnostic {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	results, err := c.Diagnostics(ctx, epoch, []string{uri})
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Items []diagnostic `json:"items"`
	}
	if err := json.Unmarshal(results[uri], &report); err != nil {
		t.Fatal(err)
	}
	return report.Items
}
