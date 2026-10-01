package frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageEnumCycleUsesDeclaringFile(t *testing.T) {
	_, err := preparePackage(map[string][]byte{"a.hgo": []byte("package p\npub enum A { V(B), }\n"), "b.hgo": []byte("package p\n" + strings.Repeat("// padding\n", 80) + "pub enum B { V(B), }\n")})
	if err == nil || !strings.Contains(err.Error(), "b.hgo:82:") || !strings.Contains(err.Error(), "recursive enum") {
		t.Fatalf("wrong error: %v", err)
	}
}
func TestModulePreservesResourcesAndQuotedReplacement(t *testing.T) {
	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	os.MkdirAll(filepath.Join(src, "testdata"), 0755)
	os.MkdirAll(filepath.Join(src, ".assets"), 0755)
	os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/app\n\ngo 1.27\nreplace example.org/dependency => \"../dependency with spaces\"\n"), 0600)
	os.WriteFile(filepath.Join(src, "main.hgo"), []byte("package main\nfunc main() {}\n"), 0600)
	os.WriteFile(filepath.Join(src, "testdata", "input.json"), []byte("resource"), 0600)
	os.WriteFile(filepath.Join(src, ".assets", "data.txt"), []byte("hidden"), 0600)
	dest := filepath.Join(parent, "out")
	_, err := CompileModule(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	mod, _ := os.ReadFile(filepath.Join(dest, "go.mod"))
	if !strings.Contains(string(mod), filepath.ToSlash(filepath.Join(parent, "dependency with spaces"))) {
		t.Fatalf("replacement changed: %s", mod)
	}
	for _, p := range []string{"testdata/input.json", ".assets/data.txt"} {
		if _, e := os.Stat(filepath.Join(dest, p)); e != nil {
			t.Fatal(e)
		}
	}
}
func TestModuleOutputAndSelectionBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, filename, source, want string }{
		{"ignored", "_ignored.hgo", "package p\n", "ignored Go-style"},
		{"tags", "input.hgo", "//go:build windows\n\npackage p\n", "build constraints"},
		{"platform", "input_linux.hgo", "package p\n", "platform suffix"},
		{"macro", "input.hgo", "package p\nfunc f(){dep.log!()}\n", "explicit import"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/p\ngo 1.27\n"), 0600)
			os.WriteFile(filepath.Join(src, tc.filename), []byte(tc.source), 0600)
			dest := filepath.Join(t.TempDir(), "out")
			_, err := CompileModule(src, dest)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			if _, err = os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("failed output was retained")
			}
		})
	}
	t.Run("symlink-parent", func(t *testing.T) {
		parent := t.TempDir()
		src := filepath.Join(parent, "src")
		os.Mkdir(src, 0755)
		os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.org/p\ngo 1.27\n"), 0600)
		link := filepath.Join(parent, "link")
		if err := os.Symlink(src, link); err != nil {
			t.Skip(err)
		}
		_, err := CompileModule(src, filepath.Join(link, "out"))
		if err == nil || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("%v", err)
		}
	})
}

func TestPackageGroupedPayloadRetainsEnumBoundary(t *testing.T) {
	_, err := preparePackage(map[string][]byte{
		"enum.hgo":  []byte("package p\npub enum Mode { Idle, }\n"),
		"group.hgo": []byte("package p\ntype ( Container struct { Modes []Mode } )\n"),
		"use.hgo":   []byte("package p\nfunc f(v Container) Result[Container,error] { return Result[Container,error]::Ok(v) }\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "enum-bearing pointer/container") {
		t.Fatalf("cross-file grouped payload escaped validation: %v", err)
	}
}
