package compile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestPreparedProjectDoesNotPublishOrRetainLeases(t *testing.T) {
	root := smallProject(t)
	writeProjectFile(t, root, "app.gox", "package main\nfunc main() { value.\n}\n")
	p, err := Prepare(t.Context(), root, Options{Offline: true})
	if err != nil {
		t.Fatalf("source errors must not prevent project preparation: %v", err)
	}
	defer p.Close()
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("preparation published a lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.layoutRoot, "units")); !os.IsNotExist(err) {
		t.Fatalf("preparation published generated units: %v", err)
	}
	release, err := project.LockProject(root)
	if err != nil {
		t.Fatalf("prepared project retained the publication lock: %v", err)
	}
	release()
	layout, err := project.OpenLayout(root, "example.org/small", p.layoutRoot)
	if err != nil {
		t.Fatalf("prepared project retained the layout lease: %v", err)
	}
	layout.Close()
}

func TestProjectSnapshotIsolationAndClose(t *testing.T) {
	root := smallProject(t)
	filename := filepath.Join(root, "app.gox")
	writeProjectFile(t, root, "app.gox", "package main\nfunc main() {}\n")
	p, err := Prepare(t.Context(), root, Options{Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	first, err := p.Snapshot(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	writeProjectFile(t, root, "app.gox", "invalid disk contents")
	overlay := []byte("package main\nfunc main() { println(42) }\n")
	second, err := p.Snapshot(t.Context(), map[string][]byte{filename: overlay})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	// The caller may reuse its edit buffer after Snapshot returns.
	clear(overlay)
	for index, snapshot := range []*Snapshot{first, second} {
		result, err := analysis.Analyze(snapshot.Context, snapshot.Input)
		if err != nil {
			t.Fatal(err)
		}
		output, err := lower.Generate(snapshot.Context, result, lower.GenerationOptions{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for key, data := range output {
			if snapshot.SourceNames[key] == filename {
				found = true
				if strings.Contains(string(data), "println(42)") != (index == 1) {
					t.Fatalf("snapshot %d mixed source bytes: %s", index, data)
				}
			}
		}
		if !found {
			t.Fatal("file-target projection lost its original source name")
		}
	}
	if data, err := os.ReadFile(filename); err != nil || string(data) != "invalid disk contents" {
		t.Fatalf("snapshot analysis wrote author source: %q, %v", data, err)
	}
	finished := make(chan error, 1)
	go func() { finished <- p.Close() }()
	select {
	case <-second.Context.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel retained snapshot tasks")
	}
	if _, err := os.Stat(p.GoRoot()); err != nil {
		t.Fatalf("Close removed resources while snapshots retained them: %v", err)
	}
	first.Close()
	second.Close()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released snapshots did not permit project cleanup")
	}
	if _, err := os.Stat(p.GoRoot()); !os.IsNotExist(err) {
		t.Fatalf("closed project retained its private view: %v", err)
	}
	if _, err := p.Snapshot(context.Background(), nil); err == nil {
		t.Fatal("closed project accepted a new snapshot")
	}
}
