package frontend

import (
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoxideBuildSelection(t *testing.T) {
	for _, target := range []string{"linux", "windows", "darwin"} {
		for _, tagged := range []bool{false, true} {
			ctx := build.Default
			ctx.GOOS = target
			ctx.GOARCH = "amd64"
			if tagged {
				ctx.BuildTags = []string{"feature"}
			}
			for _, tc := range []struct {
				name, body string
				want       bool
			}{
				{"plain.gox", "package p\n", true},
				{"_ignored.gox", "invalid source", false},
				{".ignored.gox", "invalid source", false},
				{"host_linux.gox", "package p\n", target == "linux"},
				{"host_windows_amd64.gox", "package p\n", target == "windows"},
				{"host_arm64.gox", "package p\n", false},
				{"feature.gox", "//go:build feature\n\npackage p\n", tagged},
				{"legacy.gox", "// +build feature\n\npackage p\n", tagged},
				{"literal.gox", "package p\nvar text=`//go:build false`\n", true},
			} {
				got, err := matchSourceFile(ctx, filepath.Join(t.TempDir(), tc.name), []byte(tc.body))
				if err != nil || got != tc.want {
					t.Fatalf("%s feature=%v %s: selected=%v err=%v", target, tagged, tc.name, got, err)
				}
			}
		}
	}
}
func TestModuleTaggedImportsAndExcludedSyntax(t *testing.T) {
	for _, tagged := range []bool{false, true} {
		src := t.TempDir()
		os.Mkdir(filepath.Join(src, "dep"), 0700)
		files := map[string]string{
			"go.mod":         "module example.org/p\ngo 1.27\n",
			"main.gox":       "package main\nimport d \"example.org/p/dep\"\nfunc main(){_=d.Value()}\n",
			"excluded.gox":   "//go:build never_selected\n\ninvalid excluded language syntax\n",
			"_ignored.gox":   "invalid ignored language syntax\n",
			"dep/base.gox":   "//go:build !feature\n\npackage dep\nfunc Value()int{return 1}\n",
			"dep/tagged.gox": "//go:build feature\n\npackage dep\nfunc Value()int{return native()}\n",
			"dep/native.go":  "//go:build feature\n\npackage dep\nfunc native()int{return 2}\n",
		}
		for n, s := range files {
			if err := os.WriteFile(filepath.Join(src, n), []byte(s), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var tags []string
		if tagged {
			tags = []string{"feature"}
		}
		out := filepath.Join(t.TempDir(), "out")
		_, err := CompileModuleWithTags(src, out, tags)
		if err != nil {
			t.Fatal(err)
		}
		selected := "base.go"
		if tagged {
			selected = "tagged.go"
		}
		if _, err := os.Stat(filepath.Join(out, "dep", selected)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, "excluded.go")); !os.IsNotExist(err) {
			t.Fatal("excluded source emitted")
		}
	}
}
func TestModuleRejectsCrossTargetBeforeWriting(t *testing.T) {
	t.Setenv("GOARCH", "unsupported-target")
	out := filepath.Join(t.TempDir(), "out")
	_, err := CompileModule(t.TempDir(), out)
	if err == nil || !strings.Contains(err.Error(), "cross-target type sizes") {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("cross-target output created")
	}
}
