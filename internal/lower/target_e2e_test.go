package lower_test

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/typecheck"
)

func targetConfig(t *testing.T, target string) config.Config {
	t.Helper()
	selected, err := config.ParseTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := config.NewEnvironment(selected, append(os.Environ(), "CGO_ENABLED=0"))
	if err != nil {
		t.Fatal(err)
	}
	return config.Config{Environment: environment, Reads: new(config.Reads)}
}

func TestTargetConfigurationGeneratesBuildableGo(t *testing.T) {
	for _, target := range []string{"linux/386", "windows/amd64", "linux/s390x", "wasip1/wasm"} {
		t.Run(target, func(t *testing.T) {
			cfg := targetConfig(t, target)
			selected := cfg.Environment.Target
			predicate := fmt.Sprintf("all(target_os(%q), target_arch(%q), target_platform(%q), target_endian(%q), target_pointer_width(%q))", selected.OS, selected.Arch, selected.Platform, selected.Endian, fmt.Sprint(selected.PointerWidth))
			source := fmt.Sprintf(`//go:build cfgintegration

package targetfixture
#[cfg(%s)]
func Selected() int { return 1 }
#[cfg(not(%s))]
func Selected() Missing { return missing }
#[cfg(target_pointer_width("32"))]
const WordBytes = 4
#[cfg(target_pointer_width("64"))]
const WordBytes = 8
`, predicate, predicate)
			native := []byte(`package targetfixture
import "unsafe"
var _ [unsafe.Sizeof(uintptr(0)) - WordBytes]byte
var _ [WordBytes - unsafe.Sizeof(uintptr(0))]byte
func check() int { return Selected() + Plain() }
`)
			sources := map[string][]byte{
				"selected.gox": []byte(source),
				"gone.gox":     []byte("package targetfixture\n#[cfg(target_os(\"plan9\"))]\nfunc Removed() Missing { return missing }\n"),
				"plain.gox":    []byte("package targetfixture\nfunc Plain() int { return 2 }\n"),
				"native.go":    native,
			}
			generated, err := lower.CompilePackage(sources, "targetfixture", nil, nil, lower.PackageOptions{Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			again, err := lower.CompilePackage(sources, "targetfixture", nil, nil, lower.PackageOptions{Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			checked := map[string][]byte{"native.go": native}
			for name, data := range generated {
				if !bytes.Equal(data, again[name]) {
					t.Fatalf("generation changed for %s", name)
				}
				if !bytes.HasPrefix(data, []byte("//go:build ")) || !bytes.Contains(bytes.SplitN(data, []byte("\n"), 2)[0], []byte(selected.Arch)) {
					t.Fatalf("package file %s omitted target constraint:\n%s", name, data)
				}
				checked[strings.TrimSuffix(name, ".gox")+".go"] = data
			}
			if !bytes.Contains(generated["selected.gox"], []byte("&& cfgintegration")) {
				t.Fatalf("source build constraint was lost:\n%s", generated["selected.gox"])
			}
			if !bytes.Contains(generated["gone.gox"], []byte("package targetfixture")) || bytes.Contains(generated["gone.gox"], []byte("Removed")) {
				t.Fatalf("disabled source did not keep its constrained package shell:\n%s", generated["gone.gox"])
			}
			imp := typecheck.NewImporter(filepath.Join(directory, "selected.gox"), typecheck.ImporterOptions{Config: cfg})
			if err := typecheck.CheckPackage("targetfixture", checked, imp, cfg.Sizes()); err != nil {
				t.Fatalf("target Go check: %v", err)
			}
			writeDiagnosticModuleFile(t, directory, "go.mod", []byte("module targetfixture\n\ngo 1.27\n"))
			for name, data := range checked {
				writeDiagnosticModuleFile(t, directory, name, data)
			}
			command := exec.Command(cfg.Environment.GoTool, "build", "-tags=cfgintegration", "./...")
			command.Dir, command.Env = directory, cfg.Environment.ExecEnv()
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("target Go build: %v\n%s", err, output)
			}
		})
	}
}

func TestTargetModulesSelectGoxideAndNativeFiles(t *testing.T) {
	for _, target := range []string{"linux/386", "windows/amd64"} {
		t.Run(target, func(t *testing.T) {
			cfg := targetConfig(t, target)
			directory := t.TempDir()
			files := map[string]string{
				"go.mod":            "module selection\n\ngo 1.27\n",
				"main.gox":          "package selection\nfunc Combined() int { return Platform() + Native() + Tagged() }\n",
				"value_linux.gox":   "package selection\nfunc Platform() int { return 1 }\n",
				"value_windows.gox": "package selection\nfunc Platform() int { return 2 }\n",
				"native_linux.go":   "package selection\nfunc Native() int { return 3 }\n",
				"native_windows.go": "package selection\nfunc Native() int { return 4 }\n",
				"tagged.gox":        "//go:build selected\n\npackage selection\nfunc Tagged() int { return 5 }\n",
				"ignored.gox":       "//go:build !selected\n\nthis is not valid Goxide\n",
				"ignored_native.go": "//go:build !selected\n\nthis is not valid Go\n",
				"cgo.go":            "package selection\nimport (\"C\"; \"selection\")\nfunc COnly(v C.int) { selection.Combined() }\n",
			}
			for name, source := range files {
				writeDiagnosticModuleFile(t, directory, name, []byte(source))
			}
			if err := lower.CompileModules([]lower.Module{{Path: "selection", Dir: directory, Config: cfg, Tags: []string{"selected"}}}); err != nil {
				t.Fatal(err)
			}
			chosen := "value_" + cfg.Environment.Target.OS + ".go"
			if _, err := os.Stat(filepath.Join(directory, chosen)); err != nil {
				t.Fatal(err)
			}
			other := "windows"
			if cfg.Environment.Target.OS == "windows" {
				other = "linux"
			}
			if _, err := os.Stat(filepath.Join(directory, "value_"+other+".go")); !os.IsNotExist(err) {
				t.Fatalf("excluded Goxide file was generated: %v", err)
			}
			command := exec.Command(cfg.Environment.GoTool, "build", "-tags=selected", "./...")
			command.Dir, command.Env = directory, cfg.Environment.ExecEnv()
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("target Go build: %v\n%s", err, output)
			}
		})
	}
}

func TestTargetMacroConstrainsCallPackage(t *testing.T) {
	cfg := targetConfig(t, "linux/386")
	root := t.TempDir()
	app, kit := filepath.Join(root, "app"), filepath.Join(root, "kit")
	writeDiagnosticModuleFile(t, app, "go.mod", []byte("module targetapp\n\ngo 1.27\nrequire targetkit v0.0.0\nreplace targetkit => ../kit\n"))
	writeDiagnosticModuleFile(t, kit, "go.mod", []byte("module targetkit\n\ngo 1.27\n"))
	writeDiagnosticModuleFile(t, kit, "macros.gox", []byte("package targetkit\npub macro_rules! marker { () => { #[cfg(target_os(\"plan9\"))] func Gone() {} }; }\n"))
	writeDiagnosticModuleFile(t, app, "main.gox", []byte("package targetapp\nuse targetkit::marker;\nmarker!()\n"))
	writeDiagnosticModuleFile(t, app, "other.gox", []byte("package targetapp\nfunc Plain() {}\n"))
	if err := lower.CompileModules([]lower.Module{{Path: "targetapp", Dir: app, Config: cfg}, {Path: "targetkit", Dir: kit, Config: cfg}}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "other.go"} {
		data, err := os.ReadFile(filepath.Join(app, name))
		if err != nil || !bytes.HasPrefix(data, []byte("//go:build linux && !android && 386\n")) {
			t.Fatalf("macro call package %s lacks target constraint: %v\n%s", name, err, data)
		}
	}
	data, err := os.ReadFile(filepath.Join(kit, "macros.go"))
	if err != nil || bytes.Contains(data, []byte("//go:build")) {
		t.Fatalf("unexpanded definition package acquired a target constraint: %v\n%s", err, data)
	}
}

func TestTargetConstraintsDoNotAcceptGoOSAliases(t *testing.T) {
	for _, test := range []struct{ target, alias string }{
		{"linux/amd64", "android"},
		{"darwin/arm64", "ios"},
		{"solaris/amd64", "illumos"},
	} {
		t.Run(test.target, func(t *testing.T) {
			cfg := targetConfig(t, test.target)
			source := []byte("// +build !blocked\n\npackage fixture\nconst Selected = cfg!(target_os(\"linux\"))\n")
			generated, err := lower.CompilePackage(map[string][]byte{"source.gox": source}, "fixture", nil, nil, lower.PackageOptions{Config: cfg})
			if err != nil {
				t.Fatal(err)
			}
			context := build.Default
			context.GOARCH = cfg.Environment.Target.Arch
			context.OpenFile = func(string) (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(generated["source.gox"])), nil
			}
			for _, os := range []string{cfg.Environment.Target.OS, test.alias} {
				context.GOOS = os
				matches, err := context.MatchFile("", "source.go")
				if err != nil || matches != (os == cfg.Environment.Target.OS) {
					t.Fatalf("selected %s, evaluated %s: matches=%t, error=%v\n%s", test.target, os, matches, err, generated["source.gox"])
				}
			}
			context.GOOS = cfg.Environment.Target.OS
			context.BuildTags = []string{"blocked"}
			if matches, err := context.MatchFile("", "source.go"); err != nil || matches {
				t.Fatalf("legacy source constraint was lost: matches=%t, error=%v\n%s", matches, err, generated["source.gox"])
			}
		})
	}
	generated, err := lower.CompilePackage(map[string][]byte{"source.gox": []byte("package fixture\nconst Selected = cfg!(all())\n")}, "fixture", nil, nil, lower.PackageOptions{Config: targetConfig(t, "linux/386")})
	if err != nil || bytes.Contains(generated["source.gox"], []byte("//go:build")) {
		t.Fatalf("target-independent configuration acquired a target constraint: %v\n%s", err, generated["source.gox"])
	}
}
