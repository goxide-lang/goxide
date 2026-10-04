package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScriptCachePackageSharingAndIsolation(t *testing.T) {
	root := t.TempDir()
	l, e := OpenLayout(root, "example.test/root", "")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	cache, e := l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/root")
	if e != nil {
		t.Fatal(e)
	}
	again, e := l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/root")
	if e != nil || again != cache {
		t.Fatalf("cache identity: %s %v", again, e)
	}
	if cache != ScriptCachePath(l.Root, "linux-amd64", "linux-amd64", root, "example.test/root") {
		t.Fatal("path mismatch")
	}
	if _, e = os.Stat(cache); !os.IsNotExist(e) {
		t.Fatal("layout unexpectedly created runner cache")
	}
	other, e := l.ScriptCache("linux-amd64", "linux-arm64", root, "example.test/root")
	if e != nil || other == cache {
		t.Fatal("target cache isolation")
	}
	other, e = l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/helper")
	if e != nil || other == cache {
		t.Fatal("module cache isolation")
	}
	if _, e = l.ScriptCache("../escape", "host", root, "module"); e == nil {
		t.Fatal("escaping platform")
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/root"); e == nil {
		t.Fatal("closed lease accepted")
	}
}
func TestScriptCleanPreflightAndPreservesUnits(t *testing.T) {
	root := t.TempDir()
	l, e := OpenLayout(root, "example.test/root", "")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	unit, e := l.Begin(layoutSpec())
	if e != nil {
		t.Fatal(e)
	}
	cache, e := l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/root")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(cache, 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(cache, "fixture"), []byte("owned-by-callback"), 0600); e != nil {
		t.Fatal(e)
	}
	bad := filepath.Join(l.Root, "scripts", "zz-unexpected")
	os.WriteFile(bad, []byte("keep"), 0600)
	calls := 0
	clean := func(path string) error { calls++; return os.RemoveAll(path) }
	if e = l.CleanScriptCaches(clean); e == nil || calls != 0 {
		t.Fatalf("preflight %v calls %d", e, calls)
	}
	os.Remove(bad)
	if e = l.CleanScriptCaches(clean); e != nil || calls != 1 {
		t.Fatalf("clean %v calls %d", e, calls)
	}
	if _, e = os.Stat(unit.Run); e != nil {
		t.Fatal("unrelated unit removed", e)
	}
	if _, e = os.Stat(filepath.Join(filepath.Dir(cache), ".hgo-unit.json")); e != nil {
		t.Fatal("ownership container removed", e)
	}
}
func TestScriptCleanRejectsForeignAndSymlink(t *testing.T) {
	for _, kind := range []string{"marker", "unknown", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "symlink" && runtime.GOOS == "windows" {
				t.Skip("symlink privilege")
			}
			root := t.TempDir()
			l, e := OpenLayout(root, "example.test/root", "")
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			cache, e := l.ScriptCache("linux-amd64", "linux-amd64", root, "example.test/root")
			if e != nil {
				t.Fatal(e)
			}
			os.Mkdir(cache, 0700)
			switch kind {
			case "marker":
				os.WriteFile(filepath.Join(filepath.Dir(cache), ".hgo-unit.json"), []byte("{}"), 0600)
			case "unknown":
				os.WriteFile(filepath.Join(filepath.Dir(cache), "notes"), []byte("keep"), 0600)
			case "symlink":
				if e = os.Symlink(t.TempDir(), filepath.Join(cache, "escape")); e != nil {
					t.Fatal(e)
				}
			}
			calls := 0
			if e = l.CleanScriptCaches(func(string) error { calls++; return nil }); e == nil || calls != 0 {
				t.Fatalf("accepted %s: %v/%d", kind, e, calls)
			}
		})
	}
}
