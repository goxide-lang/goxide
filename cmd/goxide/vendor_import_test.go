package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func vendorGo(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
	c.Dir = root
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("go %v: %v\n%s", args, e, b)
	}
	return string(b)
}
func vendorFixture(t *testing.T) (string, string, string) {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	base := t.TempDir()
	root := filepath.Join(base, "old")
	dep := filepath.Join(base, "dep")
	out := filepath.Join(base, "new")
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/dep\ngo 1.27\n", "dep.go": "package dep\nimport _ \"embed\"\n//go:embed assets/value.txt\nvar data string\nfunc Value()string{return data+Tag()}\n", "assets/value.txt": "original", "LICENSE": "fixture license\n", "opaque.bin": string([]byte{0xff, 0x00, 0xfe}), "tag.go": "//go:build !special\npackage dep\nfunc Tag()string{return \" host\"}\n", "tag_special.go": "//go:build special\npackage dep\nfunc Tag()string{return \" special\"}\n", "other_windows.go": "package dep\nvar WindowsOnly=1\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../dep\n", "main.go": "package main\nimport (\"fmt\";\"example.org/dep\")\nfunc main(){fmt.Println(dep.Value())}\n", "main_test.go": "package main\nimport (\"testing\";\"example.org/dep\")\nfunc TestValue(t *testing.T){if dep.Value()==\"\"{t.Fatal(\"empty\")}}\n"})
	vendorGo(t, root, "mod", "vendor")
	if e := os.Chmod(filepath.Join(root, "vendor/example.org/dep/LICENSE"), 0444); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(filepath.Join(root, "vendor/example.org/dep/dep.go"), 0664); e != nil {
		t.Fatal(e)
	}
	// Actual vendored bytes, not their remote origin, are the migration authority.
	if e := os.WriteFile(filepath.Join(root, "vendor/example.org/dep/assets/value.txt"), []byte("modified"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(dep, dep+"-unavailable"); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOMODCACHE", t.TempDir())
	return root, out, base
}
func TestVendorImportColdOffline(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI import-go vendor sources deferred by P6")
	root, out, _ := vendorFixture(t)
	// Unused historical replacement records must not make the imported snapshot
	// depend on an unavailable original directory.
	for name, extra := range map[string]string{"go.mod": "replace example.org/unused => ../unavailable\n", "vendor/modules.txt": "# example.org/unused => ../unavailable\n"} {
		f, e := os.OpenFile(filepath.Join(root, name), os.O_APPEND|os.O_WRONLY, 0644)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.WriteString(extra); e != nil {
			t.Fatal(e)
		}
		f.Close()
	}
	before := sourceHash(t, root)
	if b := vendorGo(t, root, "run", "-mod=vendor", "."); strings.TrimSpace(b) != "modified host" {
		t.Fatal(b)
	}
	vendorGo(t, root, "test", "-mod=vendor", "./...")
	if b, e := importCall(t, root, "--bin", "app=."); e != nil || !strings.Contains(b, "local-modules/") || !strings.Contains(b, "license") || !strings.Contains(b, "unknown (no fetch)") {
		t.Fatalf("plan %s %v", b, e)
	}
	if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e != nil {
		t.Fatalf("import %s %v", b, e)
	}
	if sourceHash(t, root) != before {
		t.Fatal("mutated source")
	}
	for _, name := range []string{"go.mod", "go.sum", "target"} {
		if _, e := os.Lstat(filepath.Join(out, name)); !os.IsNotExist(e) {
			t.Fatal("root artifact", name)
		}
	}
	for _, flags := range [][]string{{"run", "--offline", "--locked"}, {"run", "--offline", "--locked", "--tags", "special"}, {"test", "--offline", "--locked"}} {
		b, e := projectRun(t, out, flags...)
		if e != nil {
			t.Fatalf("%v: %s %v", flags, b, e)
		}
		if flags[0] == "run" && !strings.Contains(b, "modified") {
			t.Fatal(b)
		}
	}
	var audit project.SuppliedModules
	b, e := os.ReadFile(filepath.Join(out, "goxide-import.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &audit); e != nil {
		t.Fatal(e)
	}
	if audit.Snapshot.Files["example.org/dep/LICENSE"] == "" {
		t.Fatal("missing provenance")
	}
	lock, e := os.ReadFile(filepath.Join(out, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(lock), `"vendor"`) || strings.Contains(string(lock), ".goxide-import-") {
		t.Fatal("runtime vendor or staging path", string(lock))
	}
	for rel, outputs := range audit.Outputs {
		original, e := os.ReadFile(filepath.Join(root, "vendor", rel))
		if e != nil {
			t.Fatal(e)
		}
		for _, target := range outputs {
			data, e := os.ReadFile(filepath.Join(out, target))
			if e != nil || string(data) != string(original) {
				t.Fatal("supply changed", target, e)
			}
			info, _ := os.Stat(filepath.Join(out, target))
			input, _ := os.Stat(filepath.Join(root, "vendor", rel))
			if info.Mode().Perm() != input.Mode().Perm() {
				t.Fatal("mode changed", target)
			}
		}
	}
	// A normal local module remains editable under --locked.
	local := audit.Paths["example.org/dep"]
	if e = os.WriteFile(filepath.Join(out, local, "assets/value.txt"), []byte("changed"), 0644); e != nil {
		t.Fatal(e)
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e != nil || !strings.Contains(b, "changed") {
		t.Fatalf("local edit %s %v", b, e)
	}
	moved := out + "-moved"
	if e = os.Rename(out, moved); e != nil {
		t.Fatal(e)
	}
	if e = os.RemoveAll(root); e != nil {
		t.Fatal(e)
	}
	// Discard owned artifacts after move: rebuilding must use only author files/lock.
	os.RemoveAll(filepath.Join(moved, "target"))
	if b, e := projectRun(t, moved, "run", "--offline", "--locked"); e != nil || !strings.Contains(b, "changed") {
		t.Fatalf("moved %s %v", b, e)
	}
	projectWrite(t, moved, map[string]string{"main.go": "package main\nimport _ \"example.org/dep/missing\"\nfunc main(){}\n"})
	if b, e := projectRun(t, moved, "build", "--locked"); e == nil {
		t.Fatalf("missing package fetched: %s", b)
	}

}
func TestVendorImportRejections(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI import-go vendor sources deferred by P6")
	for _, kind := range []string{"missing", "extra-go", "extra-file", "metadata", "workspace", "explicit", "version", "replace", "symlink", "modules-symlink", "nested", "dual", "output"} {
		t.Run(kind, func(t *testing.T) {
			root, out, base := vendorFixture(t)
			mods := filepath.Join(root, "vendor/modules.txt")
			b, _ := os.ReadFile(mods)
			switch kind {
			case "missing":
				os.Remove(filepath.Join(root, "vendor/example.org/dep/dep.go"))
				os.Remove(filepath.Join(root, "vendor/example.org/dep/tag.go"))
				os.Remove(filepath.Join(root, "vendor/example.org/dep/tag_special.go"))
				os.Remove(filepath.Join(root, "vendor/example.org/dep/other_windows.go"))
			case "extra-go":
				projectWrite(t, root, map[string]string{"vendor/example.org/dep/extra/x.go": "package extra"})
			case "extra-file":
				projectWrite(t, root, map[string]string{"vendor/unknown/data": "unowned"})
			case "metadata":
				os.WriteFile(mods, append(b, []byte("## unknown-mode\n")...), 0644)
			case "workspace":
				os.WriteFile(mods, append([]byte("## workspace\n"), b...), 0644)
			case "explicit":
				os.WriteFile(mods, []byte(strings.ReplaceAll(string(b), "explicit; ", "")), 0644)
			case "version":
				os.WriteFile(mods, []byte(strings.ReplaceAll(string(b), "v1.0.0", "v1.1.0")), 0644)
			case "replace":
				os.WriteFile(mods, []byte(strings.ReplaceAll(string(b), "../dep", "../elsewhere")), 0644)
			case "modules-symlink":
				os.WriteFile(filepath.Join(base, "external-modules"), b, 0644)
				os.Remove(mods)
				os.Symlink(filepath.Join(base, "external-modules"), mods)
			case "symlink":
				os.WriteFile(filepath.Join(base, "outside"), []byte("outside"), 0644)
				os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "vendor/example.org/dep/link"))
			case "nested":
				projectWrite(t, root, map[string]string{"vendor/example.org/dep/go.mod": "module wrong\ngo 1.27\n"})
			case "dual":
				os.WriteFile(filepath.Join(root, "goxide.toml"), []byte("existing"), 0644)
			case "output":
				os.Mkdir(out, 0755)
				os.WriteFile(filepath.Join(out, "sentinel"), []byte("keep"), 0644)
			}
			before := sourceHash(t, root)
			if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e == nil {
				t.Fatalf("accepted %s: %s", kind, b)
			}
			if sourceHash(t, root) != before {
				t.Fatal("changed originals")
			}
			if kind != "output" {
				if _, e := os.Stat(out); !os.IsNotExist(e) {
					t.Fatal("partial publish")
				}
			} else {
				b, _ := os.ReadFile(filepath.Join(out, "sentinel"))
				if string(b) != "keep" {
					t.Fatal("overwritten")
				}
			}
		})
	}
}

func TestVendorVersionReplacementAndOriginalSums(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI import-go vendor sources deferred by P6")
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/fork", "v1.1.0", map[string]string{"go.mod": "module example.org/fork\ngo 1.27\n", "dep.go": "package dep\nfunc Value()string{return \"upstream\"}\n", "NOTICE": "fixture notice"})
	base := t.TempDir()
	root := filepath.Join(base, "old")
	out := filepath.Join(base, "new")
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\ngo 1.27\nrequire example.org/original v1.0.0\nreplace example.org/original v1.0.0 => example.org/fork v1.1.0\nreplace example.org/original v1.9.0 => example.org/fork v1.9.1\nexclude example.org/original v1.0.1\n", "main.go": "package main\nimport (\"fmt\";dep \"example.org/original\")\nfunc main(){fmt.Println(dep.Value())}\n"})
	vendorGo(t, root, "mod", "tidy")
	vendorGo(t, root, "mod", "vendor")
	sums, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil || !strings.Contains(string(sums), "h1:") {
		t.Fatalf("source sums: %s %v", sums, e)
	}
	if e = os.WriteFile(filepath.Join(root, "vendor/example.org/original/dep.go"), []byte("package dep\nfunc Value()string{return \"locally edited\"}\n"), 0644); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOPROXY", "off")
	before := sourceHash(t, root)
	old := vendorGo(t, root, "run", "-mod=vendor", ".")
	if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e != nil || b != old {
		t.Fatalf("%s %v want %s", b, e, old)
	}
	if sourceHash(t, root) != before {
		t.Fatal("changed original")
	}
	b, _ := os.ReadFile(filepath.Join(out, "goxide.lock"))
	var lock project.Lock
	json.Unmarshal(b, &lock)
	if lock.Vendor != nil || lock.Provenance == "" || strings.Contains(string(b), "h1:") {
		t.Fatalf("misrepresented vendor provenance: %s", b)
	}
}

func TestVendorRootFeaturesTargetsAndTrust(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI import-go vendor sources deferred by P6")
	root, out, _ := vendorFixture(t)
	projectWrite(t, root, map[string]string{"cmd/second/main.go": "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"second\")}\n"})
	if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--bin", "second=cmd/second", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	m, e := project.Read(out)
	if e != nil {
		t.Fatal(e)
	}
	m.Features = map[string][]string{"default": {"on"}, "on": {}}
	m.Examples = []project.Target{{Name: "demo", Path: "cmd/second"}}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(out, project.ManifestName), b, 0644)
	os.WriteFile(filepath.Join(out, "flag.gox"), []byte("package main\nfunc Enabled()bool{return cfg!(feature(\"on\"))}\n"), 0644)
	os.WriteFile(filepath.Join(out, "main.go"), []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Println(Enabled())}\n"), 0644)
	if b, e := projectRun(t, out, "build", "--bin", "app", "--offline"); e != nil {
		t.Fatalf("refresh edited manifest %s %v", b, e)
	}
	for _, tc := range []struct {
		flags []string
		want  string
	}{{[]string{"--bin", "app"}, "true"}, {[]string{"--bin", "app", "--no-default-features"}, "false"}, {[]string{"--bin", "second"}, "second"}, {[]string{"--example", "demo"}, "second"}} {
		flags := append([]string{"run", "--offline", "--locked"}, tc.flags...)
		if b, e := projectRun(t, out, flags...); e != nil || strings.TrimSpace(b) != tc.want {
			t.Fatalf("%v %s %v", flags, b, e)
		}
	}
	// A root generator remains an explicit untrusted host action.
	f, e := os.OpenFile(filepath.Join(out, project.ManifestName), os.O_APPEND|os.O_WRONLY, 0644)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString("\n[build]\nentry='tools/build/main.go'\noutputs=['gen/out.go']\n")
	f.Close()
	projectWrite(t, out, map[string]string{"tools/build/main.go": "package main\nfunc main(){}\n"})
	t.Setenv("GOXIDE_TRUST_DIR", filepath.Join(t.TempDir(), "trust"))
	if b, e := projectRun(t, out, "build", "--bin", "app", "--offline"); e == nil || !strings.Contains(e.Error(), "trust") {
		t.Fatalf("script not gated: %s %v", b, e)
	}
}

func TestSourceDependencyDiagnosticAfterStabilization(t *testing.T) {
	proxy, _ := versionProxy(t)
	first := plainVersionLibrary("v1")
	first["goxide.toml"] = "[go_mod]\n"
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", first)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", plainVersionLibrary("v2"))
	publishVersion(t, proxy, "example.org/middle", "v1.0.0", map[string]string{"go.mod": "module example.org/middle\ngo 1.27\n", "goxide.toml": "[go_mod]\n[lib]\nname='middle'\npath='.'\n[dependencies.lib]\ngo = 'example.org/goxidelib'\nversion = 'v1.1.0' \n", "middle.go": "package middle\n"})
	root := versionApp(t, `{"go":"example.org/goxidelib","version":"v1.0.0"}`)
	if b, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "must declare lib") {
		t.Fatalf("stable missing library: %s %v", b, e)
	}
	m, e := project.Read(root)
	if e != nil {
		t.Fatal(e)
	}
	m.Dependencies["middle"] = project.Dependency{Go: "example.org/middle", Module: "example.org/middle", Version: "v1.0.0"}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, project.ManifestName), b, 0644)
	if b, e := projectRun(t, root, "run"); e != nil || !strings.Contains(b, "v2") {
		t.Fatalf("superseded library source rejected: %s %v", b, e)
	}
}

func TestSuppliedModuleCycleAndOfflineNoProbes(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI import-go vendor sources deferred by P6")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "network forbidden", 500) }))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", t.TempDir())
	base := t.TempDir()
	root := filepath.Join(base, "original")
	projectWrite(t, filepath.Join(base, "d"), map[string]string{"go.mod": "module example.org/d\ngo 1.27\nrequire example.org/e v1.0.0\n", "p/p.go": "package p\nimport \"example.org/e/q\"\nconst Value=q.Value\n", "s/s.go": "package s\nconst Value=\"cycle\"\n"})
	projectWrite(t, filepath.Join(base, "e"), map[string]string{"go.mod": "module example.org/e\ngo 1.27\nrequire example.org/d v1.0.0\n", "q/q.go": "package q\nconst Value=\"modules\"\n", "r/r.go": "package r\nimport \"example.org/d/s\"\nconst Value=s.Value\n"})
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\ngo 1.27\nrequire (\nexample.org/d v1.0.0\nexample.org/e v1.0.0\n)\nreplace example.org/d => ../d\nreplace example.org/e => ../e\n", "main.go": "package main\nimport(\"fmt\";\"example.org/d/p\";\"example.org/e/r\")\nfunc main(){fmt.Println(p.Value,r.Value)}\n"})
	vendorGo(t, root, "mod", "vendor")
	os.RemoveAll(filepath.Join(base, "d"))
	os.RemoveAll(filepath.Join(base, "e"))
	before := requests.Load()
	if b, e := importCall(t, root, "--plan", "--offline", "--bin", "app=."); e != nil {
		t.Fatalf("plan %s %v", b, e)
	}
	out := filepath.Join(base, "new")
	if b, e := importCall(t, root, "--out", out, "--offline", "--bin", "app=."); e != nil {
		t.Fatalf("import %s %v", b, e)
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "modules cycle" {
		t.Fatalf("module cycle: %s %v", b, e)
	}
	projectWrite(t, root, map[string]string{"vendor/example.org/d/p/p.go": "package p\nimport _ \"example.org/missing\"\n"})
	if b, e := importCall(t, root, "--plan", "--offline", "--bin", "app=."); e == nil {
		t.Fatalf("missing supplied source accepted %s", b)
	}
	if requests.Load() != before {
		t.Fatalf("offline performed %d requests", requests.Load()-before)
	}
}
