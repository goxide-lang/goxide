package scripttrust

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func trustTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPersistentTrustAndLease(t *testing.T) {
	t.Setenv("GOXIDE_TRUST_DIR", filepath.Join(trustTempDir(t), "policy"))
	s, e := Open()
	if e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("a", 64)
	if e = s.Authorize("root", nil); e == nil {
		t.Fatal("untrusted root")
	}
	s.Projects["root"] = true
	s.Subjects[id] = true
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(); e == nil {
		t.Fatal("active policy lease bypassed")
	}
	s.Close()
	s, e = Open()
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Authorize("root", []string{id}); e != nil {
		t.Fatal(e)
	}
	delete(s.Subjects, id)
	if e = s.Save(); e != nil {
		t.Fatal(e)
	}
	if e = s.Authorize("root", []string{id}); e == nil {
		t.Fatal("revoked closure allowed")
	}
}
func TestTrustRejectsLinksAndBadJSON(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		dir := trustTempDir(t)
		outside := trustTempDir(t)
		link := filepath.Join(dir, "link")
		if e := os.Symlink(outside, link); e != nil {
			if runtime.GOOS == "windows" {
				t.Skip(e)
			}
			t.Fatal(e)
		}
		t.Setenv("GOXIDE_TRUST_DIR", link)
		if _, e := Open(); e == nil || !strings.Contains(e.Error(), "only real directories: "+link) {
			t.Fatalf("linked policy directory: %v", e)
		}
	})
	t.Run("unknown-json-field", func(t *testing.T) {
		dir := trustTempDir(t)
		t.Setenv("GOXIDE_TRUST_DIR", dir)
		if e := os.WriteFile(filepath.Join(dir, "grants.json"), []byte(`{"version":1,"projects":{},"subjects":{},"unexpected":true}`), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := Open(); e == nil || !strings.Contains(e.Error(), `unknown field "unexpected"`) {
			t.Fatalf("unknown trust field: %v", e)
		}
	})
}

func TestTrustCannotLiveInSourceOrManagedArtifacts(t *testing.T) {
	root := trustTempDir(t)
	t.Setenv("GOXIDE_TRUST_DIR", filepath.Join(root, "trust"))
	if _, err := Open(root); err == nil {
		t.Fatal("trust allowed in source")
	}
	if _, err := os.Stat(filepath.Join(root, "trust")); !os.IsNotExist(err) {
		t.Fatal("forbidden trust directory was created")
	}
	if err := os.WriteFile(filepath.Join(root, ".goxide-layout.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(); err == nil {
		t.Fatal("trust allowed inside an existing layout")
	}
}
