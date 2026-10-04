package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
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
	root, out, _ := vendorFixture(t)
	before := sourceHash(t, root)
	if b := vendorGo(t, root, "run", "-mod=vendor", "."); strings.TrimSpace(b) != "modified host" {
		t.Fatal(b)
	}
	vendorGo(t, root, "test", "-mod=vendor", "./...")
	if b, e := importCall(t, root, "--bin", "app=."); e != nil || !strings.Contains(b, "[vendor]") || !strings.Contains(b, "license") || !strings.Contains(b, "unknown (no fetch)") {
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
	var lock project.Lock
	b, e := os.ReadFile(filepath.Join(out, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &lock); e != nil {
		t.Fatal(e)
	}
	if lock.Vendor == nil || lock.Vendor.Files["example.org/dep/LICENSE"] == "" || lock.PlanningSums != "" {
		t.Fatalf("bad vendor lock: %s", b)
	}
	beforeArtifact, e := projectRun(t, out, "build", "--offline", "--locked")
	if e != nil {
		t.Fatal(e)
	}
	oldDigest := lock.Vendor.Digest
	binary := filepath.Join(out, "vendor/example.org/dep/opaque.bin")
	original, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(binary, []byte{0xfe, 0x00, 0xfe}, 0644); e != nil {
		t.Fatal(e)
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e == nil {
		t.Fatalf("binary corruption accepted %s", b)
	}
	if e = os.WriteFile(binary, original, 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(out, "vendor/example.org/dep/assets/value.txt"), []byte("changed"), 0644); e != nil {
		t.Fatal(e)
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e == nil || !strings.Contains(e.Error(), "vendor") {
		t.Fatalf("locked mutation %s %v", b, e)
	}
	if b, e := projectRun(t, out, "run", "--offline"); e != nil || !strings.Contains(b, "changed") {
		t.Fatalf("updated source %s %v", b, e)
	}
	b, _ = os.ReadFile(filepath.Join(out, "goxide.lock"))
	json.Unmarshal(b, &lock)
	afterArtifact, e := projectRun(t, out, "build", "--offline", "--locked")
	if e != nil {
		t.Fatal(e)
	}
	if strings.TrimSpace(beforeArtifact) == strings.TrimSpace(afterArtifact) {
		t.Fatal("vendor mutation reused artifact configuration")
	}
	if oldDigest == lock.Vendor.Digest {
		t.Fatal("digest did not change")
	}
}
func TestVendorImportRejections(t *testing.T) {
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
	if lock.Vendor == nil || lock.Provenance == "" || strings.Contains(string(b), "h1:") {
		t.Fatalf("misrepresented vendor provenance: %s", b)
	}
}

func TestVendorRootFeaturesTargetsAndTrust(t *testing.T) {
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

func TestVendorDependencyDiagnosticAfterStabilization(t *testing.T) {
	proxy, _ := versionProxy(t)
	first := versionLibrary("v1")
	first["goxide.toml"] += "\n[vendor]\nmode='go-mod'\n"
	publishVersion(t, proxy, "example.org/goxidelib", "v1.0.0", first)
	publishVersion(t, proxy, "example.org/goxidelib", "v1.1.0", versionLibrary("v2"))
	publishVersion(t, proxy, "example.org/middle", "v1.0.0", map[string]string{"go.mod": "module example.org/middle\ngo 1.27\n", "goxide.toml": "[package]\nformat=1\n[lib]\nname='middle'\npath='.'\n[dependencies.lib]\nmodule='example.org/goxidelib'\nversion='v1.1.0'\n", "middle.go": "package middle\n"})
	root := versionApp(t, `{"module":"example.org/goxidelib","version":"v1.0.0"}`)
	if b, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "only for a root") {
		t.Fatalf("stable unsupported vendor: %s %v", b, e)
	}
	m, e := project.Read(root)
	if e != nil {
		t.Fatal(e)
	}
	m.Dependencies["middle"] = project.Dependency{Module: "example.org/middle", Version: "v1.0.0"}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, project.ManifestName), b, 0644)
	if b, e := projectRun(t, root, "run"); e != nil || !strings.Contains(b, "v2") {
		t.Fatalf("transient vendor rejected: %s %v", b, e)
	}
}
