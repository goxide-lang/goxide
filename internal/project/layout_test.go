package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func layoutSpec() UnitSpec {
	return UnitSpec{Host: "linux-amd64", Target: "linux-amd64", Kind: "bin", Name: "app", Config: "abc123"}
}
func TestLayoutOwnershipAndPublishing(t *testing.T) {
	root := t.TempDir()
	l, e := OpenLayout(root, "example.org/app", "")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if _, e := OpenLayout(root, "example.org/app", ""); e == nil {
		t.Fatal("concurrent lease accepted")
	}
	if _, e := OpenLayout(t.TempDir(), "example.org/other", l.Root); e == nil {
		t.Fatal("shared foreign directory accepted")
	}
	u, e := l.Begin(layoutSpec())
	if e != nil {
		t.Fatal(e)
	}
	src := filepath.Join(u.Intermediate, "program")
	if e = os.WriteFile(src, []byte("one"), 0755); e != nil {
		t.Fatal(e)
	}
	dest, e := u.PublishBinary(src)
	if e != nil {
		t.Fatal(e)
	}
	u2, e := l.Begin(layoutSpec())
	if e != nil {
		t.Fatal(e)
	}
	if u.Bundle != u2.Bundle || u.Directory != u2.Directory {
		t.Fatal("stable unit identity mismatch")
	}
	src2 := filepath.Join(u2.Intermediate, "program")
	os.WriteFile(src2, []byte("two"), 0755)
	if out, e := u2.PublishBinary(src2); e != nil || out != dest {
		t.Fatalf("%s %v", out, e)
	}
	unknown := filepath.Join(l.Root, "user-note")
	os.WriteFile(unknown, []byte("keep"), 0644)
	os.WriteFile(dest, []byte("user edited"), 0755)
	if _, e := u.PublishBinary(src); e == nil || !strings.Contains(e.Error(), "modified") {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(dest); string(b) != "user edited" {
		t.Fatal("modified output overwritten")
	}
	if e = RejectManagedOutput(filepath.Join(u2.Intermediate, "other")); e == nil {
		t.Fatal("explicit output bypassed managed parent")
	}
	if e = l.Close(); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(unknown); string(b) != "keep" {
		t.Fatal("unknown data removed")
	}
	if _, e = os.Stat(u.Directory); e != nil {
		t.Fatal("stable unit removed")
	}
}
func TestLayoutUnknownAndSymlinkBoundaries(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "target")
	os.Mkdir(existing, 0755)
	if _, e := OpenLayout(root, "example.org/app", ""); e == nil {
		t.Fatal("claimed pre-existing empty target")
	}
	os.WriteFile(filepath.Join(existing, "keep"), []byte("safe"), 0644)
	if _, e := OpenLayout(root, "example.org/app", ""); e == nil {
		t.Fatal("claimed user target")
	}
	external := t.TempDir()
	os.Symlink(external, filepath.Join(root, "alias"))
	for _, path := range []string{"alias/new", ".", "vendor/output", ".goxide/output", ".git/output"} {
		if _, e := OpenLayout(root, "example.org/app", path); e == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	l, e := OpenLayout(root, "example.org/app", "artifacts")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	if ok, e := ManagedSourceDirectory(l.Root, root, "example.org/app"); e != nil || !ok {
		t.Fatal(ok, e)
	}
	if _, e := ManagedSourceDirectory(l.Root, root, "example.org/wrong"); e == nil {
		t.Fatal("foreign marker accepted")
	}
	os.Symlink(external, filepath.Join(l.Root, "units"))
	if _, e := l.Begin(layoutSpec()); e == nil {
		t.Fatal("symlink unit parent accepted")
	}
	if b, _ := os.ReadFile(filepath.Join(existing, "keep")); string(b) != "safe" {
		t.Fatal("unknown target changed")
	}
}
func TestLayoutUnknownUnitAndBinary(t *testing.T) {
	root := t.TempDir()
	l, e := OpenLayout(root, "example.org/app", "")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	s := layoutSpec()
	dir := filepath.Join(l.Root, "units", s.Host, s.Target, s.Kind, s.Name, s.Config)
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "unknown"), []byte("keep"), 0644)
	if _, e = l.Begin(s); e == nil {
		t.Fatal("claimed unknown unit")
	}
	s.Config = "other"
	u, e := l.Begin(s)
	if e != nil {
		t.Fatal(e)
	}
	if e = l.ownedDirectory(u.BinaryDirectory, s); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(u.BinaryDirectory, s.Name)
	os.WriteFile(dest, []byte("user file"), 0755)
	src := filepath.Join(u.Intermediate, "built")
	os.WriteFile(src, []byte("tool file"), 0755)
	if _, e = u.PublishBinary(src); e == nil || !strings.Contains(e.Error(), "unrecorded") {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(dest); string(b) != "user file" {
		t.Fatal("unrecorded binary changed")
	}
}

func TestLayoutRejectsNestedOwnership(t *testing.T) {
	root := t.TempDir()
	outer, err := OpenLayout(root, "example.org/outer", "")
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	for _, source := range []string{root, t.TempDir()} {
		nested := filepath.Join(outer.Root, "nested")
		if got, err := OpenLayout(source, "example.org/nested", nested); err == nil {
			got.Close()
			t.Fatal("nested layout bypassed parent's lease")
		}
		if _, err := os.Lstat(nested); !os.IsNotExist(err) {
			t.Fatal("nested directory created", err)
		}
	}
}
