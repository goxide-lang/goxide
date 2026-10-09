package frontend

import (
	"bytes"
	"go/build"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/testdeps"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestPackageGoFilesPreservesBuildSelectionAndOriginal(t *testing.T) {
	const name = "model_linux_amd64.gox"
	imp := typecheck.NewImporter("playground.gox")
	result, err := CompilePackage("playground", map[string][]byte{name: []byte(`//go:build feature

// Package p contains a selected model.
package p
#[derive(Clone)]
type Item struct{ N int }
`)}, imp)
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), result.Files[name].Go...)
	originalMap := append([]byte(nil), result.Files[name].SourceMap...)
	files, err := PackageGoFiles("playground", result, imp)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("missing helper: %v", files)
	}
	if !bytes.Equal(original, result.Files[name].Go) || !bytes.Equal(originalMap, result.Files[name].SourceMap) {
		t.Fatal("emission changed frontend source or map")
	}
	dir := t.TempDir()
	for path, r := range files {
		if !strings.Contains(string(r.Go), "//go:build feature") || !strings.Contains(string(r.Go), "// Package p contains a selected model.") {
			t.Fatalf("lost leading comments in %s:\n%s", path, r.Go)
		}
		path = strings.TrimSuffix(path, ".gox")
		if !strings.HasSuffix(path, ".go") {
			path += ".go"
		}
		if !strings.HasSuffix(path, "_linux_amd64.go") {
			t.Fatalf("lost platform suffix: %s", path)
		}
		if err := os.WriteFile(filepath.Join(dir, path), r.Go, 0600); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			os, arch string
			tags     []string
			want     bool
		}{
			{"linux", "amd64", []string{"feature"}, true},
			{"linux", "amd64", nil, false},
			{"windows", "amd64", []string{"feature"}, false},
			{"linux", "arm64", []string{"feature"}, false},
		} {
			ctx := build.Default
			ctx.GOOS, ctx.GOARCH, ctx.BuildTags = tc.os, tc.arch, tc.tags
			got, err := ctx.MatchFile(dir, path)
			if err != nil || got != tc.want {
				t.Fatalf("%s %+v: %v, %v", path, tc, got, err)
			}
		}
	}
}

func TestPackageGoFilesImportBindingsAndCollision(t *testing.T) {
	imp := typecheck.NewImporter("playground.gox")
	// Two imports of the same path have distinct file bindings. A helper split
	// must not preserve the unused alias simply because the path is still used.
	r := PackageResult{Files: map[string]Result{"main.gox": {
		Go: []byte(`package p
import first "strings"
import second "strings"
func Main()string{return first.ToUpper("main")}
func CloneItem()string{return second.ToLower("HELPER")}
`), generatedHelpers: map[string]bool{"CloneItem": true},
	}}}
	files, err := PackageGoFiles("playground", r, imp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["main.gox"].Go), "import second") || strings.Contains(string(files["goxide_helpers_main.go"].Go), "import first") {
		t.Fatal("file import bindings were merged")
	}
	r.Files["goxide_helpers_main.go"] = Result{Go: []byte("package p\n")}
	if _, err := PackageGoFiles("playground", r, imp); err == nil || !strings.Contains(err.Error(), "filename conflicts") {
		t.Fatalf("helper collision: %v", err)
	}
}

func TestPackageHelperRejectsUnselectedNativeCollision(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"go.mod":                  "module playground\ngo 1.27\n",
		"model.gox":               "package p\n#[derive(Clone)]\ntype Item struct{N int}\n",
		"goxide_helpers_model.go": "//go:build never_selected\n\npackage p\nconst NativeSentinel = 42\n",
	} {
		if err := testdeps.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	dest := filepath.Join(t.TempDir(), "out")
	if _, err := CompileModule(dir, dest); err == nil || !strings.Contains(err.Error(), "generated helper filename conflicts with existing output") {
		t.Fatalf("unselected native source overwritten: %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failed output was retained")
	}
	data, err := os.ReadFile(filepath.Join(dir, "goxide_helpers_model.go"))
	if err != nil || !strings.Contains(string(data), "NativeSentinel") {
		t.Fatalf("source changed: %v", err)
	}
}
