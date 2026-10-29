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
	root := sourceTempDir(t)
	wsWrite(t, root, "goxide.toml", `[workspace]
members=["app"]
[workspace.go_mod]
go="1.27"
toolchain="go1.27.0"
[workspace.dependencies]
lib={module="example.org/lib",git = "https://example.org/lib.git", branch = "main",features=["json"],default_features=false}
`)
	wsWrite(t, root, "app/goxide.toml", `[go_mod]
module="example.org/app"
go.workspace=true
toolchain.workspace=true
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
	if w.Root != root || m.GoVersion != "1.27" || m.Toolchain != "go1.27.0" {
		t.Fatal(w, m)
	}
	wsWrite(t, root, "app/goxide.lock", "{}")
	if _, e = DiscoverWorkspace(root); e == nil || !strings.Contains(e.Error(), "WS-MEMBER-LOCK") {
		t.Fatal(e)
	}
}
func TestWorkspaceNoImplicitInheritance(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, "goxide.toml", `[workspace]
members=["app"]
[workspace.dependencies]
lib={module="example.org/lib",path="lib"}
`)
	wsWrite(t, root, "app/goxide.toml", `[go_mod]
module="example.org/app"
go="1.27"
[dependencies]
lib={go = "example.org/lib", version = "v1.2.0"}
`)
	m, e := Read(filepath.Join(root, "app"))
	if e != nil {
		t.Fatal(e)
	}
	if m.Dependencies["lib"].Path != "" || m.Dependencies["lib"].Version != "v1.2.0" {
		t.Fatal(m)
	}
}

func TestWorkspaceStrictBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, root, member, extra, want string }{
		{"case", "[workspace]\nMembers=[\"app\"]\n", "", "", "unknown"},
		{"feature type", "[workspace]\nmembers=[\"app\"]\n[workspace.dependencies]\nd={module=\"example.org/d\",path=\"d\"}\n", "[dependencies]\nd={workspace=true,features=\"oops\"}\n", "", "features"},
		{"crate root", "[workspace]\nmembers=[\"app\"]\n[workspace.dependencies.crate]\ngo='fmt'\n", "", "", "invalid dependency \"crate\""},
		{"nested", "[workspace]\nmembers=[\"nested/app\"]\n", "", "nested", "another workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := sourceTempDir(t)
			wsWrite(t, root, "goxide.toml", tc.root)
			manifest := "[go_mod]\nmodule=\"example.org/app\"\ngo=\"1.27\"\n" + tc.member
			wsWrite(t, root, "app/goxide.toml", manifest)
			if tc.extra == "nested" {
				wsWrite(t, root, "nested/goxide.toml", "[workspace]\nmembers=[\"app\"]\n")
				wsWrite(t, root, "nested/app/goxide.toml", manifest)
			}
			if _, e := DiscoverWorkspace(root); e == nil || !strings.Contains(strings.ToLower(e.Error()), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, e)
			}
		})
	}
	root := sourceTempDir(t)
	wsWrite(t, root, "goxide.toml", "[workspace]\nmembers=[\"app\"]\n")
	wsWrite(t, root, "app/goxide.toml", "[go_mod]\nmodule=\"example.org/app\"\ngo=\"1.27\"\n")
	w, e := DiscoverWorkspace(root)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Select(filepath.Join(root, "unlisted"), nil, false); e == nil || !strings.Contains(e.Error(), "WS-NOT-MEMBER") {
		t.Fatal(e)
	}
	wsWrite(t, root, "goxide.toml", "[workspace]\nmembers=[\"link/*\"]\n")
	if e = os.Symlink(filepath.Join(root, "app"), filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e = DiscoverWorkspace(root); e == nil || !strings.Contains(e.Error(), "symlink") {
		t.Fatal(e)
	}
}

func TestWorkspaceMembersShareLease(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, "goxide.toml", "[workspace]\nmembers=[\"a\",\"b\"]\n")
	for _, name := range []string{"a", "b"} {
		wsWrite(t, root, name+"/goxide.toml", "[go_mod]\nmodule=\"example.org/"+name+"\"\ngo=\"1.27\"\n")
	}
	release, e := LockProject(filepath.Join(root, "a"))
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if other, e := LockProject(filepath.Join(root, "b")); e == nil {
		other()
		t.Fatal("second member bypassed workspace lease")
	}
	if _, e = os.Stat(filepath.Join(root, "goxide.lock.guard")); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "a", "goxide.lock.guard")); !os.IsNotExist(e) {
		t.Fatal("member acquired separate lease", e)
	}
	release()
	other, e := LockProject(filepath.Join(root, "b"))
	if e != nil {
		t.Fatal(e)
	}
	other()
}

func TestWorkspaceOwnerCannotCollideWithModule(t *testing.T) {
	root := t.TempDir()
	layout, e := OpenLayout(root, "workspace", "")
	if e != nil {
		t.Fatal(e)
	}
	layout.Close()
	if other, e := OpenLayout(root, WorkspaceOwner, ""); e == nil {
		other.Close()
		t.Fatal("workspace reused standalone module owner")
	}
}

func TestWorkspaceSourceIdentityPrecedesGoProjection(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, "goxide.toml", "[workspace]\nmembers=[\"app\",\"other\"]\n")
	for _, dir := range []string{"app", "other"} {
		wsWrite(t, root, dir+"/goxide.toml", "[go_mod]\nmodule=\"example.org/shared\"\ngo=\"1.27\"\n")
	}
	w, err := DiscoverWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Members) != 2 || len(w.Defaults) != 2 {
		t.Fatalf("distinct source members were merged: %#v", w)
	}
	selected, err := w.Select(filepath.Join(root, "app"), nil, false)
	if err != nil || len(selected) != 1 || w.Members[selected[0]] != filepath.Join(root, "app") {
		t.Fatalf("source selection = %v, %v", selected, err)
	}
	if _, err = w.Select(root, []string{"example.org/shared"}, false); err == nil || !strings.Contains(err.Error(), "ambiguous workspace package") {
		t.Fatalf("expected ambiguous module selector, got %v", err)
	}
	if _, err = w.GoModules(); err == nil || !strings.Contains(err.Error(), "Go module projection conflict") {
		t.Fatalf("expected Go projection conflict, got %v", err)
	}
}
