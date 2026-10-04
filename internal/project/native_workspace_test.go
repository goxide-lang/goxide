package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeWorkspaceCopyUnknownMarker(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	os.MkdirAll(filepath.Join(src, "notes"), 0755)
	os.WriteFile(filepath.Join(src, "notes", LayoutMarker), []byte("not an owner marker"), 0644)
	os.WriteFile(filepath.Join(src, "notes", "important.txt"), []byte("keep"), 0644)
	err := CopyNativeSource(src, dst)
	if err != nil {
		return
	}
	if _, err = os.Stat(filepath.Join(dst, "notes", "important.txt")); err != nil {
		t.Fatal("successful copy silently discarded ordinary notes directory")
	}
}
func TestNativeWorkspaceArchitectureConfiguration(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	t.Setenv("GOAMD64", "v1")
	a, e := NativeConfiguration(filepath.Join(runtime.GOROOT(), "bin", "go"))
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOAMD64", "v3")
	b, e := NativeConfiguration(filepath.Join(runtime.GOROOT(), "bin", "go"))
	if e != nil {
		t.Fatal(e)
	}
	if Digest(a) == Digest(b) {
		t.Fatal("GOAMD64 implicit build tag changed but native configuration unchanged")
	}
}

func TestNativeWorkspaceCCompilerConfiguration(t *testing.T) {
	t.Setenv("CGO_CFLAGS", "-O1")
	a, err := NativeConfiguration(filepath.Join(runtime.GOROOT(), "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGO_CFLAGS", "-O2")
	b, err := NativeConfiguration(filepath.Join(runtime.GOROOT(), "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	if Digest(a) == Digest(b) {
		t.Fatal("C compiler configuration identity reused")
	}
}
