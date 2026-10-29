package main

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/pe"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
)

func targetCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	args = append([]string{args[0], "--go", filepath.Join(runtime.GOROOT(), "bin", "go")}, args[1:]...)
	err := run(args, &output, &output)
	return output.String(), err
}

func TestTargetWorkflowSingleFile(t *testing.T) {
	root := t.TempDir()
	singleFileWrite(t, root, "go.mod", "module example.org/targetfile\ngo 1.27\n")
	singleFileWrite(t, root, "main.gox", `package main
const IsLinux = cfg!(target_os("linux"))
const Is32 = cfg!(target_pointer_width("32"))
const OriginalLinux = cfg!(env("GOOS", "linux"))
func main() {}
`)
	input := filepath.Join(root, "main.gox")
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "386")
	t.Setenv("CGO_ENABLED", "0")
	for _, dest := range []string{"-", filepath.Join(t.TempDir(), "out.go")} {
		output, err := targetCLI(t, "gen", "-o", dest, input)
		if err != nil {
			t.Fatalf("native generation: %v: %s", err, output)
		}
		if dest != "-" {
			data, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			output = string(data)
		}
		for _, want := range []string{"IsLinux = true", "Is32 = true", "OriginalLinux = true"} {
			if !strings.Contains(output, want) {
				t.Fatalf("native Go target missing %q:\n%s", want, output)
			}
		}
	}
	output, err := targetCLI(t, "expand", "--target=amd64-unknown-windows", input)
	if err != nil || !strings.Contains(output, "IsLinux = false") || !strings.Contains(output, "Is32 = false") || !strings.Contains(output, "OriginalLinux = true") {
		t.Fatalf("toolchain target and original environment diverged: %v\n%s", err, output)
	}
	for _, dest := range []string{"-", filepath.Join(t.TempDir(), "rejected.go")} {
		if _, err := targetCLI(t, "gen", "--target=linux/386", "-o", dest, input); err == nil || !strings.Contains(err.Error(), "--target is not accepted") {
			t.Fatalf("native --target accepted: %v", err)
		}
	}
	if _, err := targetCLI(t, "gen", "--target=", "-o", "-", input); err == nil || !strings.Contains(err.Error(), "--target is not accepted") {
		t.Fatalf("empty explicit --target accepted in native workflow: %v", err)
	}
	// The saved Go environment is used by native generation after process values
	// are removed, while toolchain actions still default to the native host.
	os.Unsetenv("GOOS")
	os.Unsetenv("GOARCH")
	singleFileWrite(t, root, "go-env", "GOOS=linux\nGOARCH=386\n")
	t.Setenv("GOENV", filepath.Join(root, "go-env"))
	output, err = targetCLI(t, "gen", "-o", "-", input)
	if err != nil || !strings.Contains(output, "Is32 = true") || !strings.Contains(output, "OriginalLinux = false") {
		t.Fatalf("saved Go target: %v\n%s", err, output)
	}
	output, err = targetCLI(t, "expand", input)
	if err != nil {
		t.Fatal(err)
	}
	want32 := "Is32 = false"
	if config.HostTarget().PointerWidth == 32 {
		want32 = "Is32 = true"
	}
	if !strings.Contains(output, want32) {
		t.Fatalf("toolchain default inherited GOENV: %s", output)
	}
}

func TestTargetNativeNestedModuleOutput(t *testing.T) {
	outer := t.TempDir()
	singleFileWrite(t, outer, "goxide.toml", "this outer manifest must not be parsed")
	root := filepath.Join(outer, "native")
	singleFileWrite(t, root, "go.mod", "module example.org/nestedtarget\ngo 1.27\n")
	singleFileWrite(t, root, "main.gox", "//go:build target_case\n\npackage main\nconst Selected = cfg!(target_os(\"linux\"))\nfunc main(){ value() }\n")
	singleFileWrite(t, root, "value_linux_386.gox", "package main\nfunc value(){}\n")
	singleFileWrite(t, root, "value_windows.gox", "package main\nfunc value(){ unresolved() }\n")
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "386")
	t.Setenv("CGO_ENABLED", "0")
	dest := filepath.Join(t.TempDir(), "generated")
	if output, err := targetCLI(t, "gen", "--tags=target_case", "-o", dest, root); err != nil {
		t.Fatalf("native -o inherited outer manifest: %v\n%s", err, output)
	}
	for _, name := range []string{"main.go", "value_linux_386.go"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "//go:build") || !strings.Contains(string(data), "!android") || !strings.Contains(string(data), "386") {
			t.Fatalf("whole-package exact target constraint missing in %s:\n%s", name, data)
		}
	}
	data, err := os.ReadFile(filepath.Join(dest, "main.go"))
	if err != nil || !strings.Contains(string(data), "target_case") {
		t.Fatalf("native directory output lost original constraint: %v\n%s", err, data)
	}
	if _, err := os.Stat(filepath.Join(dest, "value_windows.go")); !os.IsNotExist(err) {
		t.Fatalf("unselected file reached output: %v", err)
	}
}

func TestTargetSingleFileCrossBuildAndEarlyExecutionRejection(t *testing.T) {
	root := t.TempDir()
	singleFileWrite(t, root, "go.mod", "module example.org/crossfile\ngo 1.27\n")
	singleFileWrite(t, root, "main.gox", `package main
#[cfg(env("GOOS", "darwin"))]
func main() {}
#[cfg(not(env("GOOS", "darwin")))]
func main(){ unresolved() }
`)
	input := filepath.Join(root, "main.gox")
	t.Setenv("GOOS", "darwin")
	t.Setenv("GOARCH", "arm64")
	t.Setenv("CGO_ENABLED", "0")
	for _, target := range []string{"linux/386", "amd64-unknown-windows", "linux/arm64"} {
		t.Run(strings.ReplaceAll(target, "/", "-"), func(t *testing.T) {
			outputFile := filepath.Join(t.TempDir(), "program")
			if output, err := targetCLI(t, "build", "--target", target, "-o", outputFile, input); err != nil {
				t.Fatalf("cross build: %v\n%s", err, output)
			}
			selected, err := config.ParseTarget(target)
			if err != nil {
				t.Fatal(err)
			}
			info, err := buildinfo.ReadFile(outputFile)
			if err != nil {
				t.Fatal(err)
			}
			settings := map[string]string{}
			for _, item := range info.Settings {
				settings[item.Key] = item.Value
			}
			if settings["GOOS"] != selected.OS || settings["GOARCH"] != selected.Arch {
				t.Fatalf("wrong actual Go target: %v", settings)
			}
			if selected.OS == "linux" {
				binary, err := elf.Open(outputFile)
				if err != nil {
					t.Fatal(err)
				}
				defer binary.Close()
				if (binary.Class == elf.ELFCLASS32) != (selected.Arch == "386") {
					t.Fatal("ELF pointer class differs from target")
				}
			} else {
				binary, err := pe.Open(outputFile)
				if err != nil {
					t.Fatal(err)
				}
				defer binary.Close()
				if binary.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
					t.Fatal("PE machine differs from target")
				}
			}
		})
	}
	// Invalid source deliberately proves rejection precedes parsing/generation.
	singleFileWrite(t, root, "main.gox", "this is not a source file")
	before := singleFileSnapshot(t, root)
	other := "linux/386"
	if runtime.GOOS == "linux" && runtime.GOARCH == "386" {
		other = "windows/amd64"
	}
	for _, mode := range []string{"run", "test"} {
		selectedInput := input
		if mode == "test" {
			selectedInput = root
		}
		if _, err := targetCLI(t, mode, "--target", other, selectedInput); err == nil || !strings.Contains(err.Error(), "requires the host target") {
			t.Fatalf("%s was not rejected before generation: %v", mode, err)
		}
	}
	if !reflect.DeepEqual(before, singleFileSnapshot(t, root)) {
		t.Fatal("rejected execution changed sources")
	}
}

func TestEnvironmentCommandTargetsAndRawValues(t *testing.T) {
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "386")
	t.Setenv("CGO_ENABLED", "0")
	output, err := targetCLI(t, "env", "--json", "--target=amd64-unknown-windows")
	if err != nil {
		t.Fatal(err)
	}
	var targets struct {
		Host          config.Target
		GoEnvironment config.Target `json:"go_environment"`
		Target        config.Target
	}
	if err := json.Unmarshal([]byte(output), &targets); err != nil {
		t.Fatal(err)
	}
	if targets.Host != config.HostTarget() || targets.GoEnvironment.OS != "linux" || targets.GoEnvironment.Arch != "386" || targets.Target.OS != "windows" {
		t.Fatalf("targets: %+v", targets)
	}
	if strings.Contains(output, "digest") {
		t.Fatal("environment query invented a source configuration digest")
	}
	output, err = targetCLI(t, "env", "--list", "--json")
	if err != nil || !strings.Contains(output, "target_pointer_width") || !strings.Contains(output, "wasm-unknown-js") {
		t.Fatalf("target directory: %v\n%s", err, output)
	}
	t.Setenv("GOXIDE_ENV_EMPTY", "")
	t.Setenv("GOXIDE_ENV_BYTES", string([]byte{0xfe, 0xff}))
	t.Setenv("GOXIDE_ENV_MISSING", "placeholder")
	os.Unsetenv("GOXIDE_ENV_MISSING")
	output, err = targetCLI(t, "env", "--json", "GOOS", "GOXIDE_ENV_EMPTY", "GOXIDE_ENV_MISSING", "GOXIDE_ENV_BYTES")
	if err != nil {
		t.Fatal(err)
	}
	var values []struct {
		Key     string
		Present bool
		Value   *string
		Base64  string `json:"value_base64"`
	}
	if err := json.Unmarshal([]byte(output), &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 4 || values[0].Value == nil || *values[0].Value != "linux" || values[1].Value == nil || *values[1].Value != "" || values[2].Present || values[2].Value != nil || values[3].Base64 != "/v8=" {
		t.Fatalf("original env values: %+v", values)
	}
}

func TestNativeGenerationUsesEffectiveCgoFileSelection(t *testing.T) {
	root := t.TempDir()
	singleFileWrite(t, root, "go.mod", "module example.org/cgoselection\ngo 1.27\n")
	singleFileWrite(t, root, "value.gox", "package p\nfunc Value() int { return 42 }\n")
	singleFileWrite(t, root, "native.go", "package p\nimport \"C\"\nfunc COnly() { unresolved() }\n")
	t.Setenv("CGO_ENABLED", "0")
	if output, err := targetCLI(t, "gen", root); err != nil {
		t.Fatalf("disabled cgo file entered frontend: %v\n%s", err, output)
	}
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", ".")
	command.Dir, command.Env = root, localEnv()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Go selection differs from generation: %v\n%s", err, output)
	}
}
