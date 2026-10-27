package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestEditorPlanningUsesImmutableSharedGitAndPrivateMisses(t *testing.T) {
	root, repository := sourceTempDir(t), sourceTempDir(t)
	wsWrite(t, repository, "library/"+ManifestName, roleManifest("example.org/editor-dependency", false))
	wsWrite(t, repository, "library/lib.gox", "package library\nfunc Value() int { return 42 }\n")
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}} {
		if _, err := git(repository, args...); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := git(repository, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	wsWrite(t, root, ManifestName, roleManifest("example.org/editor-root", false)+fmt.Sprintf("[dependencies.dep]\ngit=%q\nrev=%q\nsubdir='library'\n", repository, revision))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	plans, err := LoadBuildPlansContext(t.Context(), root, tool, nil, false, false, false, "target", "linux/amd64", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Save(); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	shared := plans.Target.Root.Children["dep"]
	index := filepath.Join(shared.CheckoutRoot, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(index)
	if err != nil {
		t.Fatal(err)
	}
	// A stat change would make ordinary git status refresh the shared index.
	if err := os.Chtimes(filepath.Join(shared.Root, "lib.gox"), time.Unix(1234567890, 0), time.Unix(1234567890, 0)); err != nil {
		t.Fatal(err)
	}
	release, err := LockProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	layout, err := OpenLayout(root, "example.org/editor-root", "target")
	if err != nil {
		t.Fatal(err)
	}
	defer layout.Close()
	private := sourceTempDir(t)
	editor, err := LoadEditorBuildPlansContext(t.Context(), root, tool, nil, false, true, true, "target", private, "linux/amd64", false, false)
	if err != nil {
		t.Fatal(err)
	}
	selected := editor.Target.Root.Children["dep"]
	if selected.CheckoutRoot != shared.CheckoutRoot || selected.Root != shared.Root || editor.Target.PlanID != plans.Target.PlanID {
		t.Fatalf("editor changed the selected Git source or portable identity: %+v", selected)
	}
	if after, err := os.ReadFile(index); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("editor wrote shared Git index: %v", err)
	}
	if after, err := os.Stat(index); err != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Fatalf("editor refreshed shared Git index timestamp: %v", err)
	}
	if _, err := os.Stat(filepath.Join(private, "cache")); !os.IsNotExist(err) {
		t.Fatalf("cached checkout was cloned into the private cache: %v", err)
	}
	missing := filepath.Join(root, "missing-target")
	privateMiss := sourceTempDir(t)
	acquired, err := LoadEditorBuildPlansContext(t.Context(), root, tool, nil, false, false, true, missing, privateMiss, "linux/amd64", false, false)
	if err != nil {
		t.Fatal(err)
	}
	newSource := acquired.Target.Root.Children["dep"]
	if !contained(privateMiss, newSource.CheckoutRoot) || acquired.Target.PlanID != plans.Target.PlanID {
		t.Fatalf("editor cache miss did not remain private with the same source identity: %+v", newSource)
	}
	if _, err := os.Stat(filepath.Join(newSource.Root, "lib.gox")); err != nil {
		t.Fatalf("returned plans lost their private Git checkout: %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("editor claimed or changed the shared output directory: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(root, "goxide.lock")); err != nil || !bytes.Equal(lock, after) {
		t.Fatalf("editor changed published dependency lock: %v", err)
	}
}

func TestEditorPlanningExcludesPublishingFilesAndUnfinishedTarget(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, roleManifest("example.org/editor-scan", true))
	wsWrite(t, root, "lib.gox", "package library\nfunc Value() int { return 42 }\n")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	load := func() *BuildPlans {
		t.Helper()
		plans, err := LoadEditorBuildPlansContext(t.Context(), root, tool, nil, false, true, false, "target", sourceTempDir(t), "linux/amd64", true, false)
		if err != nil {
			t.Fatal(err)
		}
		return plans
	}
	before := load()
	wsWrite(t, root, ".goxide-lock-123456", "partial atomic lock publication")
	wsWrite(t, root, "target/"+LayoutMarker, "{")
	wsWrite(t, root, "target/private.go", "not authored source")
	if after := load(); after.Target.PlanID != before.Target.PlanID {
		t.Fatal("temporary lock/output publication entered the source identity")
	}
	wsWrite(t, root, ".goxide-lock-note", "authored resource")
	if after := load(); after.Target.PlanID == before.Target.PlanID {
		t.Fatal("non-temporary lookalike resource was excluded")
	}
}
