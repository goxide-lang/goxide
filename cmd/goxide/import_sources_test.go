package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func sourceFixture(t *testing.T, fork, modified bool) (string, string, string) {
	t.Helper()
	proxy, cache := versionProxy(t)
	remote := "example.org/d"
	if fork {
		remote = "example.org/fork"
	}
	publishVersion(t, proxy, remote, "v1.2.0", map[string]string{"go.mod": "module " + remote + "\ngo 1.27\n", "d.go": "package d\nconst Value=\"remote\"\n", "LICENSE": "license\n"})
	root := filepath.Join(canonicalTempDir(t), "old")
	mod := "module example.org/app\ngo 1.27\nrequire example.org/d v1.2.0\n"
	if fork {
		mod += "replace example.org/d => " + remote + " v1.2.0\n"
	}
	projectWrite(t, root, map[string]string{"go.mod": mod, "main.go": "package main\nimport (\"fmt\";\"example.org/d\")\nfunc main(){fmt.Println(d.Value)}\n"})
	vendorGo(t, root, "mod", "tidy")
	vendorGo(t, root, "mod", "vendor")
	if modified {
		projectWrite(t, root, map[string]string{"vendor/example.org/d/d.go": "package d\nconst Value=\"supplied modification\"\n"})
	}
	return root, proxy, cache
}
func sourceRequest(root, out string) ImportRequest {
	return ImportRequest{Root: root, Out: out, GoTool: filepath.Join(runtime.GOROOT(), "bin", "go"), Bins: []string{"app=."}}
}
func sourceAudit(t *testing.T, out string) project.SuppliedModules {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(out, "goxide-import.json"))
	if e != nil {
		t.Fatal(e)
	}
	var a project.SuppliedModules
	if e = json.Unmarshal(b, &a); e != nil {
		t.Fatal(e)
	}
	return a
}
func TestImportSourcesRemote(t *testing.T) {
	for _, fork := range []bool{false, true} {
		t.Run(map[bool]string{false: "module", true: "fork"}[fork], func(t *testing.T) {
			root, _, _ := sourceFixture(t, fork, true)
			before := sourceHash(t, root)
			out := filepath.Join(canonicalTempDir(t), "new")
			req := sourceRequest(root, out)
			req.Sources = []string{"example.org/d=remote"}
			var log bytes.Buffer
			if e := executeImportGo(req, &log, &log); e == nil || !strings.Contains(e.Error(), "--accept-source-diff") {
				t.Fatalf("missing confirmation %v %s", e, &log)
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("published without confirmation")
			}
			req.AcceptSourceDiff = []string{"example.org/d"}
			if e := executeImportGo(req, &log, &log); e != nil {
				t.Fatalf("remote import %v %s", e, &log)
			}
			a := sourceAudit(t, out)
			s := a.Sources["example.org/d"]
			if s.Status != "modified/unknown" || !s.Accepted || s.Sum == "" || len(s.Differences) != 1 {
				t.Fatalf("audit %+v", s)
			}
			remote := "example.org/d"
			if fork {
				remote = "example.org/fork"
			}
			if s.Module != remote {
				t.Fatal(s)
			}
			original, _ := os.ReadFile(filepath.Join(root, "vendor/example.org/d/d.go"))
			retained, _ := os.ReadFile(filepath.Join(out, a.Paths["example.org/d"], "d.go"))
			if !bytes.Equal(original, retained) || sourceHash(t, root) != before {
				t.Fatal("lost supplied bytes or mutated original")
			}
			if b, e := projectRun(t, out, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "remote" {
				t.Fatalf("remote not selected: %s %v", b, e)
			}
		})
	}
}
func TestImportSourcesVerifiedAndOffline(t *testing.T) {
	root, _, _ := sourceFixture(t, false, false)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.Error(w, "forbidden", 500) }))
	defer server.Close()
	t.Setenv("GOPROXY", server.URL)
	t.Setenv("GOSUMDB", "sum.golang.org "+server.URL)
	t.Setenv("GOTOOLCHAIN", "auto")
	run := func(out string, remote bool, plan bool) error {
		req := sourceRequest(root, out)
		req.Offline = true
		req.Plan = plan
		if remote {
			req.Sources = []string{"example.org/d=remote"}
		}
		return executeImportGo(req, &bytes.Buffer{}, &bytes.Buffer{})
	}
	if e := run(filepath.Join(canonicalTempDir(t), "warm"), true, false); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOMODCACHE", canonicalTempDir(t))
	if e := run("", true, true); e != nil {
		t.Fatal(e)
	}
	if e := run(filepath.Join(canonicalTempDir(t), "local"), false, false); e != nil {
		t.Fatal(e)
	}
	out := filepath.Join(canonicalTempDir(t), "missing")
	if e := run(out, true, false); e == nil || !strings.Contains(e.Error(), "offline=true") {
		t.Fatalf("missing cache %v", e)
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("published failed import")
	}

	req := sourceRequest(root, filepath.Join(canonicalTempDir(t), "tty-cold"))
	req.Offline = true
	req.Interactive = true
	req.Input = strings.NewReader("example.org/d=remote\n")
	if e := executeImportGo(req, &bytes.Buffer{}, &bytes.Buffer{}); e == nil || !strings.Contains(e.Error(), "offline=true") {
		t.Fatalf("TTY offline missing cache: %v", e)
	}
	req.Out = filepath.Join(canonicalTempDir(t), "tty-cancel")
	req.Input = strings.NewReader("cancel\n")
	if e := executeImportGo(req, &bytes.Buffer{}, &bytes.Buffer{}); e == nil || !strings.Contains(e.Error(), "cancelled") {
		t.Fatalf("TTY cancel: %v", e)
	}
	if hits.Load() != 0 {
		t.Fatalf("offline requests %d", hits.Load())
	}
}
func TestImportSourceSelectionInput(t *testing.T) {
	root, _, _ := sourceFixture(t, false, true)
	m, e := project.ImportMod("go", root)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := project.InspectVendor(root, m)
	if e != nil {
		t.Fatal(e)
	}
	a, e := project.MaterializeSupplied("go", root, "", m.GoVersion, snap, map[string]string{m.Module: root})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, input     string
		sources, accept []string
		tty, plan, ok   bool
	}{
		{name: "nonTTY default", ok: true},
		{name: "TTY local", tty: true, input: "\n", ok: true},
		{name: "TTY cancel", tty: true, input: "cancel\n"},
		{name: "TTY eof", tty: true},
		{name: "TTY missing newline", tty: true, input: "example.org/d=remote"},
		{name: "TTY confirm", tty: true, input: "example.org/d=remote\nremote example.org/d\n", ok: true},
		{name: "TTY decline", tty: true, input: "example.org/d=remote\nno\n"},
		{name: "TTY confirm eof", tty: true, input: "example.org/d=remote\n"},
		{name: "plan no input", tty: true, plan: true, ok: true},
		{name: "duplicate", sources: []string{"example.org/d=local", "example.org/d=remote"}},
		{name: "unknown", sources: []string{"example.org/no=remote"}},
		{name: "guessed Git", sources: []string{"example.org/d=git:https://example.org/d"}},
		{name: "accept local", sources: []string{"example.org/d=local"}, accept: []string{"example.org/d"}},
		{name: "explicit deterministic", tty: true, sources: []string{"example.org/d=local"}, ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ImportRequest{GoTool: "go", Sources: tc.sources, AcceptSourceDiff: tc.accept, Interactive: tc.tty, Plan: tc.plan, Input: strings.NewReader(tc.input)}
			var log bytes.Buffer
			e := chooseImportSources(req, root, a, &log)
			if (e == nil) != tc.ok {
				t.Fatalf("%v %s", e, &log)
			}
		})
	}
}
func TestImportSourceCacheIntegrity(t *testing.T) {
	for _, which := range []string{"zip", "expanded"} {
		t.Run(which, func(t *testing.T) {
			root, _, cache := sourceFixture(t, false, false)
			var target string
			if which == "zip" {
				target = filepath.Join(cache, "cache/download/example.org/d/@v/v1.2.0.zip")
			} else {
				target = filepath.Join(cache, "example.org/d@v1.2.0/d.go")
			}
			if e := os.Chmod(target, 0644); e != nil {
				t.Fatal(e)
			}
			if which == "zip" {
				// A valid new zip with different source retains the old cached ziphash.
				proxy := t.TempDir()
				publishVersion(t, proxy, "example.org/d", "v1.2.0", map[string]string{"go.mod": "module example.org/d\ngo 1.27\n", "d.go": "package d\nconst Value=\"tampered\"\n"})
				b, e := os.ReadFile(filepath.Join(proxy, "example.org/d/@v/v1.2.0.zip"))
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(target, b, 0644); e != nil {
					t.Fatal(e)
				}
			} else {
				if e := os.WriteFile(target, []byte("package d\nconst Value=\"tampered cache\"\n"), 0644); e != nil {
					t.Fatal(e)
				}
			}
			out := filepath.Join(canonicalTempDir(t), "new")
			req := sourceRequest(root, out)
			req.Sources = []string{"example.org/d=remote"}
			req.Offline = true
			var log bytes.Buffer
			e := executeImportGo(req, &log, &log)
			if e == nil {
				t.Fatalf("accepted %s tampering: %s", which, &log)
			}
			if which == "zip" && !strings.Contains(e.Error(), "checksum") {
				t.Fatal(e)
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatal("published corrupt cache")
			}
		})
	}
}

func TestImportSourcesMixed(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/e", "v1.0.0", map[string]string{"go.mod": "module example.org/e\ngo 1.27\n", "e.go": "package e\nconst Value=\"remote-e\"\n"})
	publishVersion(t, proxy, "example.org/d", "v1.2.0", map[string]string{"go.mod": "module example.org/d\ngo 1.27\nrequire example.org/e v1.0.0\n", "d.go": "package d\nimport \"example.org/e\"\nconst Value=\"remote-d \"+e.Value\n"})
	root := filepath.Join(canonicalTempDir(t), "old")
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\ngo 1.27\nrequire example.org/d v1.2.0\n", "main.go": "package main\nimport (\"fmt\";\"example.org/d\")\nfunc main(){fmt.Println(d.Value)}\n"})
	vendorGo(t, root, "mod", "tidy")
	vendorGo(t, root, "mod", "vendor")
	projectWrite(t, root, map[string]string{"vendor/example.org/e/e.go": "package e\nconst Value=\"local-e\"\n"})
	out := filepath.Join(canonicalTempDir(t), "new")
	req := sourceRequest(root, out)
	req.Sources = []string{"example.org/d=remote", "example.org/e=local"}
	req.Offline = true
	var log bytes.Buffer
	if e := executeImportGo(req, &log, &log); e != nil {
		t.Fatalf("%v %s", e, &log)
	}
	a := sourceAudit(t, out)
	if !a.Direct["example.org/d"] || a.Direct["example.org/e"] || a.Sources["example.org/d"].Status != "verified subset" || a.Sources["example.org/e"].Source != "local" {
		t.Fatalf("source roles %+v %+v", a.Direct, a.Sources)
	}
	moved := out + "-moved"
	if e := os.Rename(out, moved); e != nil {
		t.Fatal(e)
	}
	if e := os.RemoveAll(root); e != nil {
		t.Fatal(e)
	}
	if b, e := projectRun(t, moved, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "remote-d local-e" {
		t.Fatalf("mixed sources %v %s", e, b)
	}
}
