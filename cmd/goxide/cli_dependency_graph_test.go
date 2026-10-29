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

// Generated modules promote selected native modules to explicit roots. That can
// unprune legacy go.mod requirements even when no package imports the legacy
// module; stabilize that graph before freezing/downloading the selected plan.
func TestCLINativePromotedLegacyGraphOffline(t *testing.T) {
	proxy, _ := versionProxy(t)
	modules := []string{"example.org/nativea", "example.org/nativeb", "example.org/legacyc", "example.org/leafd"}
	for i, module := range modules {
		version := "1.27"
		if i == 2 {
			version = "1.15"
		}
		mod := "module " + module + "\n\ngo " + version + "\n"
		if i+1 < len(modules) {
			mod += "require " + modules[i+1] + " v1.0.0\n"
		}
		publishVersion(t, proxy, module, "v1.0.0", map[string]string{
			"go.mod": mod,
			"lib.go": "package native\nfunc Value() int { return 42 }\n",
		})
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	seed := t.TempDir()
	projectWrite(t, seed, map[string]string{"go.mod": "module example.org/seed\n\ngo 1.27\nrequire example.org/nativea v1.0.0\n"})
	goCommand := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(tool, args...)
		cmd.Dir = seed
		cmd.Env = project.GoEnv(false)
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("Go fixture %v: %s: %v", args, b, err)
		}
		return string(b)
	}
	initial := goCommand("list", "-mod=mod", "-m", "all")
	if !strings.Contains(initial, modules[1]+" v1.0.0") || strings.Contains(initial, modules[3]+" v1.0.0") {
		t.Fatalf("fixture does not exercise pruned legacy requirements: %s", initial)
	}
	// Populate only this isolated cache from the file proxy, then disable even
	// that transport. The actual CLI graph and generated Go run must be offline.
	for _, module := range modules {
		goCommand("mod", "download", module+"@v1.0.0")
	}
	t.Setenv("GOPROXY", "off")
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"goxide.toml": `[go_mod]
module = 'example.org/promotedapp'
go = '1.27'

[[go.require]]
module = 'example.org/nativea'
version = 'v1.0.0'

[dependencies.nativea]
go = 'example.org/nativea'
version = 'v1.0.0'

[[bin]]
name = 'app'
path = 'main.gox'
`,
		"main.gox": "package main\nuse fmt;\nuse nativea as a;\nfunc main() { fmt::Println(a::Value()) }\n",
	})
	if out, err := projectRun(t, root, "run", "--offline"); err != nil || strings.TrimSpace(out) != "42" {
		t.Fatalf("offline run: %s: %v", out, err)
	}
	lockPath := filepath.Join(root, "goxide.lock")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	g, err := project.Load(root, tool, nil, true, true, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range modules {
		if g.Plan[module].Version != "v1.0.0" {
			t.Fatalf("promoted dependency missing from stable plan: %s: %#v", module, g.Plan)
		}
	}
	if out, err := projectRun(t, root, "run", "--offline", "--locked"); err != nil || strings.TrimSpace(out) != "42" {
		t.Fatalf("offline locked reuse: %s: %v", out, err)
	}
	after, err := os.ReadFile(lockPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("locked reuse changed lock: %v", err)
	}
}
