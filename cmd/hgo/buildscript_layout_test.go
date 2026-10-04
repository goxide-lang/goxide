package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func scriptLayoutOutput(t *testing.T, root, mode, state string, args ...string) (string, string) {
	t.Helper()
	out, e := projectRun(t, root, append([]string{mode, "--offline"}, args...)...)
	if e != nil || !strings.Contains(out, "build-script "+state) {
		t.Fatalf("%s %v: %s %v", mode, args, out, e)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	path := strings.TrimSpace(lines[len(lines)-1])
	if !filepath.IsAbs(path) {
		t.Fatalf("expected absolute artifact path after logs: %s", out)
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatalf("artifact %s: %v", path, e)
	}
	return path, out
}
func scriptLayoutBinary(t *testing.T, path, want string) {
	t.Helper()
	out, e := exec.Command(path).CombinedOutput()
	if e != nil || !strings.Contains(string(out), want) {
		t.Fatalf("binary %s wanted %q: %s %v", path, want, out, e)
	}
}
func scriptLayoutFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	e := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		result[rel] = b
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return result
}
func scriptLayoutArtifacts(t *testing.T, root string) int {
	t.Helper()
	count := 0
	e := filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, d fs.DirEntry, e error) error {
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		if !d.IsDir() && d.Name() == "artifact.json" {
			count++
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return count
}
func TestBuildScriptManagedLayoutRetainsBundle(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	t.Setenv("HGO_TARGET_DIR", "")
	scriptAction(t, root, "trust-project")
	first, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one")
	if !strings.HasPrefix(first, filepath.Join(root, "target", "bin")+string(filepath.Separator)) {
		t.Fatalf("unexpected default final binary: %s", first)
	}
	scriptLayoutBinary(t, first, "APP_ONE schema-one/template-one/alpha//local embedded-resource")
	if n := scriptLayoutArtifacts(t, filepath.Join(root, "target")); n != 1 {
		t.Fatalf("wanted single package script artifact, got %d", n)
	}
	generated, _ := scriptLayoutOutput(t, root, "transpile", "cache hit", "--bin", "one")
	original := scriptLayoutFiles(t, generated)
	projectWrite(t, root, map[string]string{"schema.txt": "schema-new"})
	next, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one")
	if next != first {
		t.Fatalf("input-only change moved final binary: %s -> %s", first, next)
	}
	scriptLayoutBinary(t, next, "APP_ONE schema-new")
	retained := scriptLayoutFiles(t, generated)
	if len(retained) != len(original) {
		t.Fatal("old bundle file set changed")
	}
	for name, b := range original {
		if !bytes.Equal(b, retained[name]) {
			t.Fatalf("old bundle modified: %s", name)
		}
	}
	oldBinary := filepath.Join(t.TempDir(), "old-app")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-buildvcs=false", "-mod=readonly", "-o", oldBinary, "./cmd/one")
	cmd.Dir = generated
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("retained bundle build: %s %v", out, e)
	}
	scriptLayoutBinary(t, oldBinary, "APP_ONE schema-one/template-one/alpha//local embedded-resource")
	if _, e := os.Stat(filepath.Join(root, "gen")); !os.IsNotExist(e) {
		t.Fatal("generator outputs leaked into source root", e)
	}
	if _, e := os.Stat(filepath.Join(root, "hgo.lock")); e != nil {
		t.Fatal("lock must remain in source root", e)
	}
}
func TestBuildScriptManagedLayoutOverridesAndClean(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	t.Setenv("HGO_TARGET_DIR", "env-target")
	scriptAction(t, root, "trust-project")
	trustRoot := os.Getenv("HGO_TRUST_DIR")
	trust := scriptLayoutFiles(t, trustRoot)
	envBinary, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one")
	envTarget := filepath.Join(root, "env-target")
	if !strings.HasPrefix(envBinary, envTarget+string(filepath.Separator)) {
		t.Fatal(envBinary)
	}
	chosen := filepath.Join(root, "chosen-target")
	first, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one", "--target-dir", "chosen-target")
	second, _ := scriptLayoutOutput(t, root, "build", "cache hit", "--bin", "two", "--target-dir", "chosen-target")
	for _, binary := range []string{first, second} {
		if !strings.HasPrefix(binary, chosen+string(filepath.Separator)) {
			t.Fatal(binary)
		}
	}
	if n := scriptLayoutArtifacts(t, chosen); n != 1 {
		t.Fatalf("bins did not share package script cache: %d", n)
	}
	bundle, _ := scriptLayoutOutput(t, root, "transpile", "cache hit", "--bin", "one", "--target-dir", "chosen-target")
	beforeBundle := scriptLayoutFiles(t, bundle)
	projectWrite(t, root, map[string]string{"source-sentinel.txt": "untouched source"})
	scriptAction(t, root, "clean", "--target-dir", "chosen-target")
	if n := scriptLayoutArtifacts(t, chosen); n != 0 {
		t.Fatalf("script cache survived clean: %d", n)
	}
	if n := scriptLayoutArtifacts(t, envTarget); n != 1 {
		t.Fatalf("clean touched other target cache: %d", n)
	}
	scriptLayoutBinary(t, first, "APP_ONE")
	scriptLayoutBinary(t, second, "APP_TWO")
	scriptLayoutBinary(t, envBinary, "APP_ONE")
	afterBundle := scriptLayoutFiles(t, bundle)
	if len(beforeBundle) != len(afterBundle) {
		t.Fatal("clean removed retained bundle")
	}
	for name, b := range beforeBundle {
		if !bytes.Equal(b, afterBundle[name]) {
			t.Fatalf("clean modified bundle %s", name)
		}
	}
	afterTrust := scriptLayoutFiles(t, trustRoot)
	if len(trust) != len(afterTrust) {
		t.Fatal("clean changed trust files")
	}
	for name, b := range trust {
		if !bytes.Equal(b, afterTrust[name]) {
			t.Fatalf("clean changed trust %s", name)
		}
	}
	if !scriptPlan(t, root).RootTrusted {
		t.Fatal("clean revoked project trust")
	}
	if b, e := os.ReadFile(filepath.Join(root, "source-sentinel.txt")); e != nil || string(b) != "untouched source" {
		t.Fatalf("clean changed source: %q %v", b, e)
	}
	if strings.HasPrefix(trustRoot, chosen+string(filepath.Separator)) || strings.HasPrefix(trustRoot, envTarget+string(filepath.Separator)) {
		t.Fatal("trust inside cleanable output")
	}
	scriptLayoutOutput(t, root, "build", "run", "--bin", "two", "--target-dir", "chosen-target")
	scriptLayoutOutput(t, root, "build", "cache hit", "--bin", "one")
}
func TestBuildScriptManagedLayoutRejectsUnsafeDestinations(t *testing.T) {
	t.Run("edited binary", func(t *testing.T) {
		root := buildScriptFixture(t, false, false)
		t.Setenv("HGO_TARGET_DIR", "")
		scriptAction(t, root, "trust-project")
		binary, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one")
		if e := os.WriteFile(binary, []byte("user-owned edit"), 0755); e != nil {
			t.Fatal(e)
		}
		out, e := projectRun(t, root, "build", "--offline", "--bin", "one")
		if e == nil || !strings.Contains(e.Error(), "modified binary") {
			t.Fatalf("modified final binary accepted: %s %v", out, e)
		}
		if b, e := os.ReadFile(binary); e != nil || string(b) != "user-owned edit" {
			t.Fatalf("modified binary overwritten: %q %v", b, e)
		}
	})
	for _, kind := range []string{"unmanaged", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := buildScriptFixture(t, false, false)
			t.Setenv("HGO_TARGET_DIR", "")
			scriptAction(t, root, "trust-project")
			target := filepath.Join(root, "target")
			sentinel := filepath.Join(target, "sentinel")
			if kind == "symlink" {
				other := t.TempDir()
				sentinel = filepath.Join(other, "sentinel")
				if e := os.WriteFile(sentinel, []byte("keep"), 0644); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(other, target); e != nil {
					t.Skipf("symlink unavailable: %v", e)
				}
			} else {
				projectWrite(t, root, map[string]string{"target/sentinel": "keep"})
			}
			for _, args := range [][]string{{"build", "--offline", "--bin", "one"}, {"scripts", "--action", "clean", "--target-dir", "target"}} {
				out, e := projectRun(t, root, args...)
				if e == nil || strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
					t.Fatalf("unsafe destination accepted: %v: %s %v", args, out, e)
				}
			}
			if b, e := os.ReadFile(sentinel); e != nil || string(b) != "keep" {
				t.Fatalf("unsafe destination changed: %q %v", b, e)
			}
		})
	}
}

func TestBuildScriptManagedLayoutInsideScriptDirectory(t *testing.T) {
	root := buildScriptFixture(t, false, false)
	t.Setenv("HGO_TARGET_DIR", "")
	scriptAction(t, root, "trust-project")
	// An owned layout nested beneath the generator directory is output, not an
	// additional host source/input. Its own growth must not invalidate the script.
	target := filepath.Join("tools", "build", "artifacts")
	first, _ := scriptLayoutOutput(t, root, "build", "run", "--bin", "one", "--target-dir", target)
	second, _ := scriptLayoutOutput(t, root, "build", "cache hit", "--bin", "one", "--target-dir", target)
	if first != second {
		t.Fatalf("stable build moved output: %s -> %s", first, second)
	}
	scriptLayoutBinary(t, second, "APP_ONE schema-one/template-one/alpha//local embedded-resource")
	if n := scriptLayoutArtifacts(t, filepath.Join(root, target)); n != 1 {
		t.Fatalf("owned output was fed back into cache inputs: %d artifacts", n)
	}
}

func TestBuildScriptManagedLayoutRejectsTrustInsideHelper(t *testing.T) {
	root := buildScriptFixture(t, false, true)
	t.Setenv("HGO_TARGET_DIR", "")
	t.Chdir(root)
	t.Setenv("HGO_TRUST_DIR", filepath.Join("..", "helper", "trust"))
	trust := filepath.Join(filepath.Dir(root), "helper", "trust")
	for _, args := range [][]string{
		{"scripts", "--action", "trust-project"},
		{"scripts", "--action", "plan"},
		{"build", "--offline", "--bin", "one"},
	} {
		out, e := projectRun(t, root, args...)
		if e == nil || !strings.Contains(e.Error(), "B-TRUST-PATH") {
			t.Fatalf("trust under host helper accepted or wrong diagnostic: %v: %s %v", args, out, e)
		}
		if strings.Contains(out, "FIXTURE_GENERATOR_EXECUTED") {
			t.Fatalf("unsafe trust path executed generator: %s", out)
		}
		if _, e := os.Lstat(trust); !os.IsNotExist(e) {
			t.Fatalf("unsafe trust path created files: %s: %v", trust, e)
		}
	}
}
