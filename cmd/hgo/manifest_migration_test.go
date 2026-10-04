package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestMigrationNeverFallsBack(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "both"}[both], func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.org/old\n\ngo 1.27\n", "main.go": "package main\nfunc main(){}\n", "hgo.json": `{"version":1,"build":{"entry":"danger.go"}}`}
			if both {
				files["goxide.toml"] = "[package]\nformat=1\n"
			}
			projectWrite(t, root, files)
			for _, mode := range []string{"run", "build", "transpile", "test"} {
				if b, e := projectRun(t, root, mode); e == nil || !strings.Contains(e.Error(), "hgo.json") {
					t.Fatalf("%s %s %v", mode, b, e)
				}
			}
		})
	}
}

func TestManifestMigrationDependencySources(t *testing.T) {
	for _, source := range []string{"path", "git", "version"} {
		for _, both := range []bool{false, true} {
			t.Run(source+map[bool]string{false: "-legacy", true: "-both"}[both], func(t *testing.T) {
				dep := t.TempDir()
				files := map[string]string{"go.mod": "module example.org/hgolib\n\ngo 1.27\n", "lib.go": "package lib\nfunc Value()string{return \"native-fallback-is-wrong\"}\n", "hgo.json": `{"version":1,"features":{"secret":[]},"lib":{"name":"lib","path":"."}}`}
				if both {
					files["goxide.toml"] = "[package]\nformat=1\n[lib]\nname='lib'\npath='.'\n"
				}
				d := map[string]any{"module": "example.org/hgolib"}
				switch source {
				case "path":
					projectWrite(t, dep, files)
					d["path"] = dep
				case "git":
					projectWrite(t, dep, files)
					migrationGit(t, dep, "init")
					migrationGit(t, dep, "add", ".")
					migrationGit(t, dep, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "fixture")
					d["git"] = dep
					d["rev"] = "HEAD"
				case "version":
					proxy, _ := versionProxy(t)
					publishVersion(t, proxy, "example.org/hgolib", "v1.0.0", files)
					d["version"] = "v1.0.0"
				}
				root := t.TempDir()
				projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\n", "main.go": "package main\nfunc main(){}\n", "goxide.toml": projectManifest(map[string]any{"version": 1, "bins": []any{map[string]string{"name": "app", "path": "main.go"}}, "dependencies": map[string]any{"old": d}})})
				if b, e := projectRun(t, root, "run"); e == nil || !strings.Contains(e.Error(), "hgo.json") {
					t.Fatalf("%s %v", b, e)
				}
			})
		}
	}
}

func TestManifestMigrationHostHelper(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "both"}[both], func(t *testing.T) {
			root := buildScriptFixture(t, false, true)
			helper := filepath.Join(filepath.Dir(root), "helper")
			if !both {
				if e := os.Remove(filepath.Join(helper, "goxide.toml")); e != nil {
					t.Fatal(e)
				}
			}
			projectWrite(t, helper, map[string]string{"hgo.json": `{"version":1,"build":{"entry":"evil.go"}}`})
			if b, e := projectRun(t, root, "run", "--bin", "one"); e == nil || !strings.Contains(e.Error(), "hgo.json") {
				t.Fatalf("%s %v", b, e)
			}
		})
	}
}

func migrationGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("git: %s %v", b, e)
	}
}
