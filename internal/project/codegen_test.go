package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodegenManifestRoundTripAndDependencyIdentity(t *testing.T) {
	const base = "[go_mod]\nmodule='example.org/codegen'\ngo='1.27'\n"
	plain, err := Parse(ManifestName, []byte(base))
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		text := base + "[codegen]\ngo_sugar=false\n"
		if enabled {
			text = strings.Replace(text, "go_sugar=false", "go_sugar=true", 1)
		}
		manifest, err := Parse(ManifestName, []byte(text))
		if err != nil || manifest.Codegen.GoSugar == nil || *manifest.Codegen.GoSugar != enabled {
			t.Fatalf("codegen setting lost: %+v, %v", manifest.Codegen, err)
		}
		data, err := Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := Parse(ManifestName, data)
		if err != nil || restored.Codegen.GoSugar == nil || *restored.Codegen.GoSugar != enabled {
			t.Fatalf("codegen round trip: %s, %v", data, err)
		}
		if Digest(manifest) != Digest(plain) {
			t.Fatal("codegen policy changed dependency identity")
		}
	}
}

func TestCodegenManifestDiagnostics(t *testing.T) {
	for _, body := range []string{
		"[codegen]\ngo_sugar='false'\n",
		"[codegen]\ngo_sugar=0\n",
		"[codegen]\nGo_Sugar=false\n",
		"[Codegen]\ngo_sugar=false\n",
		"[codegen]\nsugar=false\n",
	} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/native\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		filename := filepath.Join(root, ManifestName)
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadCodegen(root); err == nil || !strings.Contains(err.Error(), filename+":") {
			t.Fatalf("invalid native config accepted or unlocated: %s: %v", body, err)
		}
	}
}

func TestCodegenNativeModuleBoundary(t *testing.T) {
	root := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		filename := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("goxide.toml", "[go_mod]\nmodule='example.org/outer'\ngo='1.27'\n[codegen]\ngo_sugar=false\n")
	write("native/go.mod", "module example.org/native\ngo 1.27\n")
	write("native/pkg/source.gox", "package pkg\n")
	native := filepath.Join(root, "native")
	settings, err := ModuleCodegen(filepath.Join(native, "pkg"))
	if err != nil || settings.GoSugar != nil {
		t.Fatalf("native module inherited outer policy: %+v, %v", settings, err)
	}
	write("native/goxide.toml", "[codegen]\ngo_sugar=false\n")
	settings, err = ModuleCodegen(filepath.Join(native, "pkg"))
	if err != nil || settings.GoSugar == nil || *settings.GoSugar {
		t.Fatalf("native module policy missing: %+v, %v", settings, err)
	}
	if author, err := Detect(native); err != nil || author {
		t.Fatalf("native codegen configuration became an author project: %v, %v", author, err)
	}
	write("native/nested/go.mod", "module example.org/nested\ngo 1.27\n")
	settings, err = ModuleCodegen(filepath.Join(native, "nested"))
	if err != nil || settings.GoSugar != nil {
		t.Fatalf("nested module inherited native parent policy: %+v, %v", settings, err)
	}
}
