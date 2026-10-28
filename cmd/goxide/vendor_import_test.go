package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
