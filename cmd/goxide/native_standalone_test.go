package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
)

func TestNativeWorkspaceCopiedLockStandalone(t *testing.T) {
	t.Skip("MVP CLI workspace import lock replay deferred by P6")
	proxy, _ := versionProxy(t)
	for _, v := range []string{"v1.0.0", "v1.1.0"} {
		publishVersion(t, proxy, "example.org/d", v, map[string]string{"go.mod": "module example.org/d\ngo 1.27\n", "d.go": "package d\nconst Value=\"" + v + "\"\n"})
	}
	root := nativeWorkspaceFixture(t)
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\ngodebug panicnil=0\nrequire example.org/d v1.0.0\nreplace example.org/lib => ../lib\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/d v1.1.0\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\";\"runtime/debug\")\nfunc main(){fmt.Println(d.Value);b,_:=debug.ReadBuildInfo();for _,s:=range b.Settings{if s.Key==\"DefaultGODEBUG\"{fmt.Println(s.Value)}}}\n")
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	split := t.TempDir()
	// The existing local-replace contract validates declared paths even when
	// unused. Materialize that path; this regression isolates lock identity,
	// without adding lazy discovery of missing replacement directories.
	if err := os.MkdirAll(filepath.Join(filepath.Dir(split), "m1"), 0755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"goxide.toml", "main.go"} {
		b, e := os.ReadFile(filepath.Join(dest, "members/m0", name))
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, split, name, string(b))
	}
	locked, e := os.ReadFile(filepath.Join(dest, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, split, "goxide.lock", string(locked))
	out, e := projectRun(t, split, "run", "--locked", "--offline")
	if e != nil || !strings.Contains(out, "v1.1.0") || !strings.Contains(out, "panicnil=0") {
		t.Fatalf("%s %v", out, e)
	}
	after, e := os.ReadFile(filepath.Join(split, "goxide.lock"))
	if e != nil || string(after) != string(locked) {
		t.Fatal("copied lock rewritten", e)
	}
	for _, kind := range []string{"member", "module", "checksum"} {
		var bad project.Lock
		if e = json.Unmarshal(locked, &bad); e != nil {
			t.Fatal(e)
		}
		switch kind {
		case "member":
			delete(bad.NativeWorkspace.Members, "example.org/app")
		case "module":
			delete(bad.NativeWorkspace.Modules, "example.org/d")
		case "checksum":
			pin := bad.NativeWorkspace.Modules["example.org/d"]
			pin.Sum = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			bad.NativeWorkspace.Modules["example.org/d"] = pin
		}
		data, e := json.Marshal(bad)
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, split, "goxide.lock", string(data))
		if out, e := projectRun(t, split, "run", "--locked", "--offline"); e == nil {
			t.Fatalf("accepted corrupt %s: %s", kind, out)
		}
	}
	workspaceWrite(t, split, "goxide.lock", string(locked))
	// Unrelated source is unavailable; split must not consult its files.
	os.RemoveAll(dest)
	out, e = projectRun(t, split, "run", "--locked", "--offline")
	if e != nil || !strings.Contains(out, "v1.1.0") {
		t.Fatalf("%s %v", out, e)
	}
}

func TestNativeWorkspaceCopiedLockMaterializedMembers(t *testing.T) {
	t.Skip("MVP CLI workspace import lock replay deferred by P6")
	root := nativeWorkspaceFixture(t)
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	w, e := project.DiscoverWorkspace(dest)
	if e != nil {
		t.Fatal(e)
	}
	split := t.TempDir()
	app := filepath.Join(split, "app")
	lib := filepath.Join(split, "lib")
	for module, old := range w.Members {
		m := w.Manifests[old]
		to := lib
		if module == "example.org/app" {
			to = app
			d := m.Dependencies["member1"]
			d.Path = "../lib"
			m.Dependencies["member1"] = d
		}
		data, e := project.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, to, "goxide.toml", string(data))
		file := "lib.go"
		if to == app {
			file = "main.go"
		}
		data, e = os.ReadFile(filepath.Join(old, file))
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, to, file, string(data))
	}
	locked, e := os.ReadFile(filepath.Join(dest, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, app, "goxide.lock", string(locked))
	os.RemoveAll(dest)
	out, e := projectRun(t, app, "run", "--locked", "--offline")
	if e != nil || !strings.Contains(out, "work") {
		t.Fatalf("%s %v", out, e)
	}
	b, e := os.ReadFile(filepath.Join(lib, "goxide.toml"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, lib, "goxide.toml", string(b)+"\n[features]\nchanged=[]\n")
	if out, e := projectRun(t, app, "run", "--locked", "--offline"); e == nil {
		t.Fatalf("changed child manifest accepted: %s", out)
	}
}

func TestNativeWorkspaceSuppliedLocalStandalone(t *testing.T) {
	t.Skip("MVP CLI workspace import lock replay deferred by P6")
	root := nativeWorkspaceFixture(t)
	for _, name := range []string{"d", "e"} {
		workspaceWrite(t, root, name+"/go.mod", "module example.org/"+name+"\ngo 1.27\n")
		workspaceWrite(t, root, name+"/lib.go", "package "+name+"\nconst Value=1\n")
	}
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\nrequire example.org/d v1.0.0\nreplace example.org/d => ../d\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\")\nfunc main(){fmt.Println(d.Value)}\n")
	workspaceWrite(t, root, "lib/go.mod", "module example.org/lib\ngo 1.27\nrequire example.org/e v1.0.0\nreplace example.org/e => ../e\n")
	workspaceWrite(t, root, "lib/lib.go", "package lib\nimport \"example.org/e\"\nfunc Value()int{return e.Value}\n")
	nativeFixtureCommand(t, root, "work", "vendor")
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	split := t.TempDir()
	for _, name := range []string{"goxide.toml", "main.go"} {
		b, e := os.ReadFile(filepath.Join(dest, "members/m0", name))
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, split, name, string(b))
	}
	locked, e := os.ReadFile(filepath.Join(dest, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, split, "goxide.lock", string(locked))
	// Materialize local paths, copy the ordinary local module trees, retain root lock.
	m, e := project.Read(split)
	if e != nil {
		t.Fatal(e)
	}
	for i := range m.Native.Replace {
		r := &m.Native.Replace[i]
		r.Path = "./local-modules/" + project.Digest(r.Module)[:24]
	}
	data, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, split, "goxide.toml", string(data))
	if e = copyImportVendor(filepath.Join(dest, "local-modules"), filepath.Join(split, "local-modules")); e != nil {
		t.Fatal(e)
	}
	os.RemoveAll(dest)
	os.RemoveAll(root)
	if out, e := projectRun(t, split, "run", "--locked", "--offline"); e != nil || strings.TrimSpace(out) != "1" {
		t.Fatalf("independent local modules: %s %v", out, e)
	}

}

func TestNativeWorkspaceCopiedLockRejectsLostWorkOverride(t *testing.T) {
	t.Skip("MVP CLI workspace import lock replay deferred by P6")
	root := nativeWorkspaceFixture(t)
	for _, name := range []string{"member-source", "work-source"} {
		workspaceWrite(t, root, name+"/go.mod", "module example.org/d\ngo 1.27\n")
		workspaceWrite(t, root, name+"/lib.go", "package d\nconst Value=\""+name+"\"\n")
	}
	workspaceWrite(t, root, "go.work", "go 1.27\nuse (\n./app\n./lib\n)\nreplace example.org/d => ./work-source\n")
	workspaceWrite(t, root, "app/go.mod", "module example.org/app\ngo 1.27\nrequire example.org/d v1.0.0\nreplace example.org/d => ../member-source\n")
	workspaceWrite(t, root, "app/main.go", "package main\nimport(\"fmt\";\"example.org/d\")\nfunc main(){fmt.Println(d.Value)}\n")
	dest := filepath.Join(t.TempDir(), "new")
	if out, e := importCall(t, root, "--workspace", "--out", dest, "--offline"); e != nil {
		t.Fatalf("%s %v", out, e)
	}
	split := t.TempDir()
	for _, name := range []string{"goxide.toml", "main.go"} {
		b, e := os.ReadFile(filepath.Join(dest, "members/m0", name))
		if e != nil {
			t.Fatal(e)
		}
		workspaceWrite(t, split, name, string(b))
	}
	locked, e := os.ReadFile(filepath.Join(dest, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	workspaceWrite(t, split, "goxide.lock", string(locked))
	if out, e := projectRun(t, split, "run", "--locked", "--offline"); e == nil || strings.Contains(out, "work-source") {
		t.Fatalf("lost work override silently retained: %s %v", out, e)
	}
}
