package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureGraphDiagnostics(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`{"version":1,"features":{"a":["b"],"b":["a"]}}`, "cycle"},
		{`{"version":1,"features":{"a":["unknown"]}}`, "unknown feature"},
		{`{"version":1,"features":{"a":["dep:missing"]}}`, "optional dependency"},
		{`{"version":1,"features":{"a":["missing/x"]}}`, "unknown dependency"},
		{`{"version":1,"bins":[{"name":"same","path":"a.hgo"},{"name":"same","path":"b.hgo"}]}`, "duplicate target"},
		{`{"version":1,"bins":[{"name":"bad","path":"../outside"}]}`, "stay inside"},
		{`{"version":1,"lib":[{"name":"a","path":"a"},{"name":"b","path":"b"}]}`, "cannot unmarshal"},
		{`{"version":1,"dependencies":{"x":{"module":"example.org/x","path":"x","version":"v1.0.0"}}}`, "exactly one"},
		{`{"version":1,"dependencies":{"x":{"module":"example.org/x","git":"https://example.invalid/x","rev":"main","subdir":"../x"}}}`, "subdir"},
		{`{"version":1,"features":{"x":[]},"bins":[{"name":"app","path":"a","required_features":["unknown"]}]}`, "unknown required feature"},
	} {
		root := t.TempDir()
		if e := os.WriteFile(filepath.Join(root, ManifestName), []byte(tc.src), 0644); e != nil {
			t.Fatal(e)
		}
		_, e := Read(root)
		if e == nil || !strings.Contains(e.Error(), tc.want) {
			t.Fatalf("want %s got %v", tc.want, e)
		}
	}
}
func TestGitURLAndLockBoundaries(t *testing.T) {
	for _, raw := range []string{"https://user:password@example.invalid/x", "ext::sh -c whoami", "-uanything", "ssh://example.invalid/x", "https://example.invalid/x?token=secret"} {
		if validateGit(raw) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	root := t.TempDir()
	release, e := LockProject(root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = LockProject(root); e == nil {
		t.Fatal("parallel writer accepted")
	}
	other := t.TempDir()
	if e = os.Symlink(root, filepath.Join(other, ".hgo")); e != nil {
		t.Fatal(e)
	}
	if _, e = LockProject(other); e == nil {
		t.Fatal("symlink cache accepted")
	}
}
