package typecheck

import (
	"go/constant"
	"go/importer"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
)

func TestGoChecksUseExplicitTargetSizes(t *testing.T) {
	for _, arch := range []string{"386", "amd64", "s390x", "wasm"} {
		t.Run(arch, func(t *testing.T) {
			sizes := types.SizesFor("gc", arch)
			files := map[string][]byte{"word.go": []byte("package p\nimport \"unsafe\"\nconst Bytes = unsafe.Sizeof(uintptr(0))\n")}
			pkg, err := CheckPackageTypes("target.example/p", files, importer.Default(), sizes)
			if err != nil {
				t.Fatal(err)
			}
			value, ok := constant.Int64Val(pkg.Scope().Lookup("Bytes").(*types.Const).Val())
			if !ok || value != sizes.Sizeof(types.Typ[types.Uintptr]) {
				t.Fatalf("unsafe.Sizeof = %d, expected %d", value, sizes.Sizeof(types.Typ[types.Uintptr]))
			}
			err = Check("overflow.go", []byte("package p\nvar Value int = 1 << 32\n"), nil, sizes)
			if arch == "386" {
				if err == nil || !strings.Contains(err.Error(), "overflows") {
					t.Fatalf("32-bit int overflow was accepted: %v", err)
				}
			} else if err != nil {
				t.Fatalf("64-bit int value was rejected: %v", err)
			}
		})
	}
	if err := Check("missing.go", []byte("package p\nvar Value int\n"), nil, nil); err == nil || !strings.Contains(err.Error(), "target sizes") {
		t.Fatalf("missing target sizes were accepted: %v", err)
	}
}

func TestImporterUsesCapturedTargetSelection(t *testing.T) {
	directory := t.TempDir()
	files := map[string]string{
		"go.mod":        "module target.example/p\n\ngo 1.27\n",
		"word_386.go":   "package p\nconst Word = 4\n",
		"word_amd64.go": "package p\nconst Word = 8\n",
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var fingerprints []string
	for _, arch := range []string{"386", "amd64"} {
		selected, err := config.ParseTarget("linux/" + arch)
		if err != nil {
			t.Fatal(err)
		}
		environment, err := config.NewEnvironment(selected, append(os.Environ(), "CGO_ENABLED=0"))
		if err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{Environment: environment}
		imp := NewImporter(filepath.Join(directory, "input.gox"), ImporterOptions{Config: cfg})
		t.Setenv("GOOS", "windows")
		t.Setenv("GOARCH", "arm64")
		pkg, err := imp.Import("target.example/p")
		if err != nil {
			t.Fatal(err)
		}
		word, _ := constant.Int64Val(pkg.Scope().Lookup("Word").(*types.Const).Val())
		if word != int64(selected.PointerWidth/8) {
			t.Fatalf("%s selected Word=%d", arch, word)
		}
		provider := imp.(interface {
			PackageFiles(string) (string, []string, error)
			PackageFingerprint(string) (string, error)
		})
		_, names, err := provider.PackageFiles("target.example/p")
		if err != nil || len(names) != 1 || names[0] != "word_"+arch+".go" {
			t.Fatalf("%s selected files %v: %v", arch, names, err)
		}
		fingerprint, err := provider.PackageFingerprint("target.example/p")
		if err != nil {
			t.Fatal(err)
		}
		fresh := NewImporter(filepath.Join(directory, "input.gox"), ImporterOptions{Config: cfg}).(interface{ PackageFingerprint(string) (string, error) })
		if repeated, err := fresh.PackageFingerprint("target.example/p"); err != nil || repeated != fingerprint {
			t.Fatalf("captured target fingerprint changed: %q, %q, %v", fingerprint, repeated, err)
		}
		fingerprints = append(fingerprints, fingerprint)
	}
	if fingerprints[0] == fingerprints[1] {
		t.Fatal("distinct target source closures share a fingerprint")
	}
}
