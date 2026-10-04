package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wsWrite(t *testing.T, root, path, body string) {
	t.Helper()
	p := filepath.Join(root, path)
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(body), 0644); e != nil {
		t.Fatal(e)
	}
}
func TestWorkspaceExplicitInheritance(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "goxide.toml", `[workspace]
format=1
members=["app"]
[workspace.package]
go="1.27"
[workspace.dependencies]
lib={module="example.org/lib",git="https://example.org/lib.git",rev="main",features=["json"],default_features=false}
`)
	wsWrite(t, root, "app/goxide.toml", `[package]
format=2
module="example.org/app"
go.workspace=true
[dependencies]
lib={workspace=true,features=["extra"]}
`)
	w, e := DiscoverWorkspace(filepath.Join(root, "app"))
	if e != nil {
		t.Fatal(e)
	}
	m, e := Read(filepath.Join(root, "app"))
	if e != nil {
		t.Fatal(e)
	}
	d := m.Dependencies["lib"]
	if d.Workspace || d.Git != "https://example.org/lib.git" || len(d.Features) != 2 || d.DefaultFeatures == nil || *d.DefaultFeatures {
		t.Fatalf("bad inheritance: %#v", d)
	}
	if w.Root != root || m.GoVersion != "1.27" {
		t.Fatal(w, m)
	}
	wsWrite(t, root, "app/goxide.lock", "{}")
	if _, e = DiscoverWorkspace(root); e == nil || !strings.Contains(e.Error(), "WS-MEMBER-LOCK") {
		t.Fatal(e)
	}
}
func TestWorkspaceNoImplicitInheritance(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "goxide.toml", `[workspace]
format=1
members=["app"]
[workspace.dependencies]
lib={module="example.org/lib",path="lib"}
`)
	wsWrite(t, root, "app/goxide.toml", `[package]
format=2
module="example.org/app"
go="1.27"
[dependencies]
lib={module="example.org/lib",version="v1.2.0"}
`)
	m, e := Read(filepath.Join(root, "app"))
	if e != nil {
		t.Fatal(e)
	}
	if m.Dependencies["lib"].Path != "" || m.Dependencies["lib"].Version != "v1.2.0" {
		t.Fatal(m)
	}
}
