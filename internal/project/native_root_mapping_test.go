package project

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func nativeRootMappingFixture(t *testing.T) (*Workspace, Manifest) {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "app")
	m, err := Parse("native.toml", []byte(mappingHeader+`
[dependencies.dep]
go = "example.org/dep"
version = "v1.0.0"
package="example.org/dep"
optional=true
[dependencies.fmt]
go = "fmt"
[[go.require]]
module="example.org/dep"
version="v1.0.0"
[[go.replace]]
module="example.org/dep"
path="../local-modules/dep"
[features]
with_dep=["dep:dep"]
`))
	if err != nil {
		t.Fatal(err)
	}
	w := &Workspace{Root: root, Config: WorkspaceConfig{Go: &NativeWorkspaceConfig{Version: "1.27"}}, Members: map[string]string{pathSource(app).key(): app}, Manifests: map[string]Manifest{app: m}}
	return w, m
}

func TestNativeExternalRootGraph(t *testing.T) {
	w, m := nativeRootMappingFixture(t)
	closure, err := NativeClosure(w, m.Module)
	if err != nil || !reflect.DeepEqual(closure, map[string]bool{m.Module: true}) {
		t.Fatalf("external root became a member edge: %v, %v", closure, err)
	}
	// The declared requirement is a minimum. A higher Go MVS selection is still
	// authoritative; the mapping must not overwrite it with its matching require.
	selection := Selection{Path: "example.org/dep", Version: "v1.2.0", Replace: &Selection{Path: "local:example.org/dep"}}
	lock := Lock{NativeWorkspace: &NativeWorkspaceLock{Modules: map[string]Selection{selection.Path: selection}}}
	for _, features := range [][]string{nil, {"with_dep"}} {
		graphs, err := NativeFeatureGraphs(w, []string{m.Module}, "unused-go-tool", map[string][]string{m.Module: features}, false, lock)
		if err != nil {
			t.Fatal(err)
		}
		g := graphs[m.Module]
		if len(g.Nodes) != 1 || len(g.Root.Sources) != 0 || len(g.Root.Children) != 0 || len(g.Root.Requests) != 1+len(features) {
			t.Fatalf("external mapping altered native member graph: %+v", g.Root)
		}
		if !reflect.DeepEqual(g.Plan[selection.Path], selection) {
			t.Fatalf("mapping changed Go's selected version/source: %+v", g.Plan)
		}
	}
	delete(lock.NativeWorkspace.Modules, selection.Path)
	if _, err := NativeFeatureGraphs(w, []string{m.Module}, "unused-go-tool", map[string][]string{m.Module: {"with_dep"}}, false, lock); err == nil || !strings.Contains(err.Error(), "selected native module") {
		t.Fatalf("exposed an external root absent from selected native graph: %v", err)
	}
}

func TestNativeExternalRootDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Workspace, *Manifest)
		want string
	}{
		{"missing require", func(_ *Workspace, m *Manifest) { m.Native.Require = nil }, "match this member's go.require"},
		{"different require version", func(_ *Workspace, m *Manifest) { m.Native.Require[0].Version = "v1.1.0" }, "match this member's go.require"},
		{"different require module", func(_ *Workspace, m *Manifest) { m.Native.Require[0].Module = "example.org/other" }, "match this member's go.require"},
		{"external path", func(_ *Workspace, m *Manifest) {
			d := m.Dependencies["dep"]
			d.Go, d.Version, d.Path = "", "", "../local-modules/dep"
			m.Dependencies["dep"] = d
		}, "configure sources with go.replace"},
		{"external git", func(_ *Workspace, m *Manifest) {
			d := m.Dependencies["dep"]
			d.Go, d.Version, d.Git, d.Rev = "", "", "https://example.org/dep", "main"
			m.Dependencies["dep"] = d
		}, "configure sources with go.replace"},
		{"dependency feature", func(_ *Workspace, m *Manifest) {
			d := m.Dependencies["dep"]
			d.Features = []string{"enhanced"}
			m.Dependencies["dep"] = d
		}, "cannot receive feature requests"},
		{"dormant forwarded feature", func(_ *Workspace, m *Manifest) { m.Features["never"] = []string{"dep/enhanced"} }, "cannot receive feature requests"},
		{"member version", func(w *Workspace, m *Manifest) {
			w.Members[pathSource(filepath.Join(w.Root, "dep")).key()] = filepath.Join(w.Root, "dep")
			w.Manifests[filepath.Join(w.Root, "dep")] = Manifest{Module: "example.org/dep"}
		}, "explicit member path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, m := nativeRootMappingFixture(t)
			tc.edit(w, &m)
			w.Manifests[filepath.Join(w.Root, "app")] = m
			if _, err := NativeClosure(w, m.Module); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestNativeExternalRootPreservesMemberEdges(t *testing.T) {
	w, m := nativeRootMappingFixture(t)
	lib := filepath.Join(w.Root, "lib")
	wsWrite(t, lib, "lib.go", "package lib\n")
	w.Members[pathSource(lib).key()] = lib
	w.Manifests[lib] = Manifest{Module: "example.org/lib", GoVersion: "1.27"}
	m.Dependencies["member"] = Dependency{Path: "../lib", Package: "example.org/lib"}
	w.Manifests[filepath.Join(w.Root, "app")] = m
	lock := Lock{NativeWorkspace: &NativeWorkspaceLock{Modules: map[string]Selection{"example.org/dep": {Path: "example.org/dep", Version: "v1.0.0"}}}}
	graphs, err := NativeFeatureGraphs(w, []string{m.Module}, "unused-go-tool", map[string][]string{m.Module: {"with_dep"}}, false, lock)
	if err != nil {
		t.Fatal(err)
	}
	g := graphs[m.Module]
	if len(g.Nodes) != 2 || len(g.Root.Requests) != 3 || len(g.Root.Sources) != 1 || len(g.Root.Children) != 1 || g.Root.Children["member"].Module != "example.org/lib" {
		t.Fatalf("lost member edge or added external edge: %+v", g.Root)
	}
}

func TestNativeExternalRootOfflineLocalReplacement(t *testing.T) {
	w, m := nativeRootMappingFixture(t)
	wsWrite(t, w.Root, ManifestName, "[workspace]\nmembers=['app']\n[workspace.go]\ngo='1.27'\nconfiguration={}\n")
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	wsWrite(t, w.Root, "app/"+ManifestName, string(data))
	wsWrite(t, w.Root, "app/main.go", "package main\nimport (\"fmt\"; \"example.org/dep\")\nfunc main(){fmt.Print(dep.Value())}\n")
	wsWrite(t, w.Root, "local-modules/dep/go.mod", "module example.org/dep\ngo 1.27\n")
	wsWrite(t, w.Root, "local-modules/dep/dep.go", "package dep\nfunc Value() string { return \"supplied-native-source\" }\n")
	t.Setenv("GOMODCACHE", t.TempDir())
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	lock := Lock{NativeWorkspace: &NativeWorkspaceLock{Sums: map[string]string{}}}
	view := t.TempDir()
	if _, err := NativeView(w, view, map[string]bool{m.Module: true}, lock.NativeWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := ReadNativeSelections(w, tool, view, lock.NativeWorkspace, true); err != nil {
		t.Fatal(err)
	}
	graphs, err := NativeFeatureGraphs(w, []string{m.Module}, tool, map[string][]string{m.Module: {"with_dep"}}, false, lock)
	if err != nil {
		t.Fatal(err)
	}
	selection := graphs[m.Module].Plan["example.org/dep"]
	if selection.Version != "v1.0.0" || selection.Replace == nil || selection.Replace.Path != "local:example.org/dep" || len(graphs[m.Module].Root.Sources) != 0 {
		t.Fatalf("native source policy changed: %+v", selection)
	}
	output, err := NativeGo(tool, view, true, "run", "-mod=readonly", "./app")
	if err != nil || string(output) != "supplied-native-source" {
		t.Fatalf("offline native build lost supplied source: %s, %v", output, err)
	}
}
