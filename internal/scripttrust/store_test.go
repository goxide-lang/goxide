package scripttrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentTrustAndLease(t *testing.T) {
	t.Setenv("HGO_TRUST_DIR", filepath.Join(t.TempDir(), "policy"))
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
	dir := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(dir, "link")
	if e := os.Symlink(outside, link); e != nil {
		t.Skip(e)
	}
	t.Setenv("HGO_TRUST_DIR", link)
	if _, e := Open(); e == nil {
		t.Fatal("linked policy directory")
	}
	t.Setenv("HGO_TRUST_DIR", dir)
	if e := os.WriteFile(filepath.Join(dir, "grants.json"), []byte(`{"version":1,"projects":{},"subjects":{},"unexpected":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Open(); e == nil {
		t.Fatal("unknown trust field")
	}
}
