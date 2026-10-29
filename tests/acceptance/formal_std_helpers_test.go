package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

const stdFixtureImports = "[dependencies.fmt]\ngo=\"fmt\"\n[dependencies.errors]\ngo=\"errors\"\n[dependencies.reflect]\ngo=\"reflect\"\n[dependencies.math]\ngo=\"math\"\n"

func formalDependencyDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writeStdFormalManifest(t *testing.T, directory, module, name string, dependencies map[string]string) {
	t.Helper()
	manifest := testdeps.FormalManifest(t, module, name, stdFixtureImports)
	names := make([]string, 0, len(dependencies))
	for dependency := range dependencies {
		names = append(names, dependency)
	}
	sort.Strings(names)
	for _, dependency := range names {
		manifest += fmt.Sprintf("[dependencies.%s]\npath=%q\n", dependency, filepath.ToSlash(dependencies[dependency]))
	}
	packagesWrite(t, directory, "goxide.toml", manifest)
}

func compileStdFormalGraph(t *testing.T, success bool, source, destination string) string {
	t.Helper()
	result, _, err := testdeps.PrepareFormal(t, source)
	if success != (err == nil) {
		t.Fatalf("formal source graph expected success=%v: %v", success, err)
	}
	if err != nil {
		return err.Error()
	}
	if err := os.MkdirAll(destination, 0755); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(result.Root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(result.Root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	}); err != nil {
		t.Fatal(err)
	}
	// Relocate the copied module's Go graph without changing authored source.
	manifest, err := project.ImportMod(result.Config.Environment.GoTool, result.Root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Native.Replace {
		replacement := &manifest.Native.Replace[i]
		if replacement.Path != "" && !filepath.IsAbs(replacement.Path) {
			replacement.Path = filepath.Join(result.Root, replacement.Path)
		}
	}
	data, err := project.ModuleText(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, "go.mod"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return ""
}

func writeStdNativeConsumerManifest(t *testing.T, directory, module, generated string) {
	t.Helper()
	manifest, err := project.ImportMod(filepath.Join(runtime.GOROOT(), "bin", "go"), generated)
	if err != nil {
		t.Fatal(err)
	}
	dependency := manifest.Module
	manifest.Module = module
	for i := range manifest.Native.Replace {
		replacement := &manifest.Native.Replace[i]
		if replacement.Path != "" && !filepath.IsAbs(replacement.Path) {
			replacement.Path = filepath.Join(generated, replacement.Path)
		}
	}
	manifest.Native.Require = append(manifest.Native.Require, project.GoRequire{Module: dependency, Version: "v0.0.0"})
	manifest.Native.Replace = append(manifest.Native.Replace, project.GoReplace{Module: dependency, Path: generated})
	data, err := project.ModuleText(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), data, 0600); err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(generated, "go.sum"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err == nil {
		if err := os.WriteFile(filepath.Join(directory, "go.sum"), sum, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func enumFormalStdModule(t *testing.T, source, assertions string, wantFailure bool, verifyVet ...bool) {
	t.Helper()
	result, err := testdeps.FormalSource(t, "enum.gox", []byte(source), stdFixtureImports)
	if err != nil {
		if wantFailure {
			return
		}
		t.Fatal(err)
	}
	if wantFailure {
		t.Fatal("invalid formal source accepted")
	}
	packagesWrite(t, result.Project.Root, "business_test.go", assertions)
	testdeps.RunFormalGo(t, result.Project, "test", "-mod=readonly", "-race", "-count=1", "./...")
	if len(verifyVet) > 0 && verifyVet[0] {
		testdeps.RunFormalGo(t, result.Project, "vet", "-mod=readonly", "./...")
	}
}

// Native input modes keep explicit roots and the selected public Go transport.
func stdFixtureGoRequirements(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	mod, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), filepath.Join(filepath.Dir(here), "../.."))
	if err != nil {
		t.Fatal(err)
	}
	result := ""
	for _, dependency := range mod.Require {
		if dependency.Path == "github.com/goxide-lang/std" || dependency.Path == "github.com/goxide-lang/serde" {
			result += fmt.Sprintf("require %s %s\n", dependency.Path, dependency.Version)
		}
	}
	return result
}

func writeStdNativeRoots(t *testing.T, directory string) {
	t.Helper()
	packagesWrite(t, directory, "goxide.roots.toml", `[roots.std]
module="github.com/goxide-lang/std"
[roots.serde]
module="github.com/goxide-lang/serde"
[roots.fmt]
source="toolchain"
package="fmt"
[roots.errors]
source="toolchain"
package="errors"
`)
}

func writeStdFormalBinaryManifest(t *testing.T, directory, module, name string) {
	t.Helper()
	manifest := strings.Replace(testdeps.FormalManifest(t, module, name, stdFixtureImports), "[lib]", "[[bin]]", 1)
	packagesWrite(t, directory, "goxide.toml", manifest)
}

func enumFormalStdBinary(t *testing.T, source, assertions string) {
	t.Helper()
	result, err := testdeps.FormalBinarySource(t, "main.gox", []byte(source), stdFixtureImports)
	if err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, result.Project.Root, "business_test.go", assertions)
	testdeps.RunFormalGo(t, result.Project, "test", "-mod=readonly", "-race", "-count=1", "./...")
	testdeps.RunFormalGo(t, result.Project, "vet", "-mod=readonly", "./...")
}
