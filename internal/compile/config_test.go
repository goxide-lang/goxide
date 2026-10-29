package compile

import (
	"bytes"
	"debug/elf"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestUnitDependencyIdentityTracksSelectionsNotSourceBodies(t *testing.T) {
	helper := &project.Graph{DependencyPlanID: "helper-selection", PlanID: "helper-body-1"}
	macro := &project.Graph{DependencyPlanID: "macro-selection", PlanID: "macro-body-1", ProcEdges: []project.ProcEdge{{Consumer: &project.Node{SourceKey: "macro"}, Alias: "helper", Plan: helper}}}
	graph := &project.Graph{DependencyPlanID: "target-selection", PlanID: "target-body-1", ProcEdges: []project.ProcEdge{{Consumer: &project.Node{SourceKey: "target"}, Alias: "macro", Plan: macro}}}
	first := unitDependencyID(graph)
	helper.PlanID, macro.PlanID, graph.PlanID = "helper-body-2", "macro-body-2", "target-body-2"
	if unitDependencyID(graph) != first {
		t.Fatal("ordinary source edits changed the output configuration")
	}
	helper.DependencyPlanID = "helper-new-selection"
	if unitDependencyID(graph) == first {
		t.Fatal("transitive macro dependency selection did not change the output configuration")
	}
}

func TestProjectEnvironmentReadsDetermineGenerationIdentity(t *testing.T) {
	root := smallProject(t)
	writeProjectFile(t, root, "app.gox", `package main
use fmt;
macro_rules! query { ($key:tt) => { cfg!(env($key)) }; }
#[cfg(any())]
func removed() bool { return cfg!(env("GOXIDE_TEST_REMOVED_ENV")) }
#[cfg_attr(any(), cfg(env("GOXIDE_TEST_UNSELECTED_ENV")))]
const Present = query!("GOXIDE_TEST_QUERY_ENV")
func main() { fmt::Println(Present) }
`)
	base := withoutConfigEnv(os.Environ(), "GOXIDE_TEST_QUERY_ENV", "GOXIDE_TEST_REMOVED_ENV", "GOXIDE_TEST_UNSELECTED_ENV", "GOXIDE_TEST_UNQUERIED_ENV", "GOOS", "GOARCH")
	type generation struct {
		root  string
		files map[string][]byte
	}
	seen := map[string]generation{}
	var lock []byte
	for _, test := range []struct {
		name, value, identity string
		present               bool
		extra                 []string
	}{
		{name: "unset", identity: "unset"},
		{name: "empty", identity: "empty", present: true},
		{name: "first value", value: "alpha", identity: "alpha", present: true},
		{name: "second value same predicate", value: "beta", identity: "beta", present: true},
		{name: "first value again", value: "alpha", identity: "alpha", present: true},
		{name: "unread values", value: "alpha", identity: "alpha", present: true, extra: []string{"GOXIDE_TEST_REMOVED_ENV=one", "GOXIDE_TEST_UNSELECTED_ENV=two", "GOXIDE_TEST_UNQUERIED_ENV=three", "GOOS=invalid-ambient-os", "GOARCH=invalid-ambient-arch"}},
		{name: "raw ff", value: string([]byte{0xff}), identity: "ff", present: true},
		{name: "raw fe", value: string([]byte{0xfe}), identity: "fe", present: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && (test.identity == "ff" || test.identity == "fe") {
				t.Skip("Windows environment values do not support arbitrary non-UTF-8 bytes")
			}
			captured := append(append([]string(nil), base...), test.extra...)
			if test.present {
				captured = append(captured, "GOXIDE_TEST_QUERY_ENV="+test.value)
			}
			environment, err := config.Prepare("", root, "", false, captured)
			if err != nil {
				t.Fatal(err)
			}
			result, err := PrepareProject(root, Options{Offline: true, Locked: lock != nil, Config: config.Config{Environment: environment}})
			if err != nil {
				t.Fatal(err)
			}
			inputs := result.Config.Reads.Inputs()
			if len(inputs) != 1 || inputs[0].Key != "GOXIDE_TEST_QUERY_ENV" || inputs[0].Present != test.present || !bytes.Equal(inputs[0].Value, []byte(test.value)) {
				t.Fatalf("actual env reads = %#v", inputs)
			}
			want := "false\n"
			if test.present {
				want = "true\n"
			}
			if got := configuredGo(t, result, "run", "-mod=readonly", result.Entry); got != want {
				t.Fatalf("generated env predicate = %q, want %q", got, want)
			}
			files := projectSnapshot(t, filepath.Dir(result.Root))
			if prior, exists := seen[test.identity]; exists {
				if prior.root != result.Root || !reflect.DeepEqual(prior.files, files) {
					t.Fatal("unchanged queried environment changed the artifact path or bytes")
				}
			} else {
				for identity, prior := range seen {
					if prior.root == result.Root {
						t.Fatalf("different raw env states %s and %s reused one artifact path", identity, test.identity)
					}
				}
				seen[test.identity] = generation{result.Root, files}
			}
			lock = unchangedConfigLock(t, root, lock)
		})
	}
}

func TestProjectTargetSelectsSourcesAndBuildsRequestedMachine(t *testing.T) {
	root := featureProject(t, map[string]string{
		"goxide.toml": `[go_mod]
module = "example.org/target"
go = "1.27"
[[bin]]
name = "app"
path = "."
[dependencies]
fmt = { go = "fmt" }
`,
		"main.gox": `package main
use fmt;
#[cfg(target_os("linux"))]
func label() string { return "linux" }
#[cfg(target_os("windows"))]
func label() string { return "windows" }
#[cfg(target_pointer_width("64"))]
const Wide int = 1 << 40
func main() { fmt::Println(label(), nativeValue, cfg!(target_pointer_width("32"))) }
`,
		"native_linux.go":   "package main\nconst nativeValue = \"linux\"\n",
		"native_windows.go": "package main\nconst nativeValue = \"windows\"\n",
		"invalid_plan9.gox": "invalid syntax in an unselected source file",
	})
	var lock []byte
	var linuxRoot string
	var linuxFiles map[string][]byte
	for _, target := range []string{"linux/amd64", "amd64-unknown-linux", "windows/amd64", "linux/386"} {
		t.Run(target, func(t *testing.T) {
			result, err := PrepareProject(root, Options{Offline: true, Locked: lock != nil, Target: target})
			if err != nil {
				t.Fatal(err)
			}
			selected := result.Config.Environment.Target
			files := projectSnapshot(t, filepath.Dir(result.Root))
			if target == "linux/amd64" {
				linuxRoot, linuxFiles = result.Root, files
			} else if target == "amd64-unknown-linux" {
				if result.Root != linuxRoot || !reflect.DeepEqual(files, linuxFiles) {
					t.Fatal("target aliases changed generation identity or bytes")
				}
			} else if result.Root == linuxRoot {
				t.Fatal("different targets reused one artifact path")
			}
			if target != "amd64-unknown-linux" {
				binary := filepath.Join(t.TempDir(), "target.exe")
				configuredGo(t, result, "build", "-mod=readonly", "-o", binary, result.Entry)
				if selected.OS == "windows" {
					file, err := pe.Open(binary)
					if err != nil {
						t.Fatal(err)
					}
					defer file.Close()
					if file.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
						t.Fatalf("Windows binary machine = %#x", file.Machine)
					}
				} else {
					file, err := elf.Open(binary)
					if err != nil {
						t.Fatal(err)
					}
					defer file.Close()
					want := elf.EM_X86_64
					if selected.Arch == "386" {
						want = elf.EM_386
					}
					if file.Machine != want {
						t.Fatalf("Linux binary machine = %s, want %s", file.Machine, want)
					}
				}
			}
			lock = unchangedConfigLock(t, root, lock)
		})
	}
}

func TestProjectRejectsExplicitWorkspace(t *testing.T) {
	root := smallProject(t)
	t.Setenv("GOWORK", filepath.Join(t.TempDir(), "go.work"))
	environment, err := config.Prepare("", root, "", false, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	result, err := PrepareProject(root, Options{Offline: true, Config: config.Config{Environment: environment}})
	if result != nil || err == nil || err.Error() != "workspace migration unsupported: explicit GOWORK" {
		t.Fatalf("authored project accepted an explicit workspace: %v, %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("rejected workspace published a lock: %v", err)
	}
}

func TestProjectUsesCapturedEnvironmentForEveryGoCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Go command observation uses a POSIX shell wrapper")
	}
	root, err := filepath.EvalSymlinks(smallProject(t))
	if err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, root, "app.gox", "package main\nuse fmt;\nfunc main(){fmt::Println(cfg!(env(\"GOXIDE_TEST_FROZEN_MARKER\", \"frozen\")))}\n")
	work := t.TempDir()
	tool, log := filepath.Join(work, "go"), filepath.Join(work, "commands.log")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '%s|%s|%s\\n' \"$GOXIDE_TEST_FROZEN_MARKER\" \"$GOOS\" \"$GOARCH\" >> " + quote(log) + "\nexec " + quote(filepath.Join(runtime.GOROOT(), "bin", "go")) + " \"$@\"\n"
	if err := os.WriteFile(tool, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOXIDE_TEST_FROZEN_MARKER", "frozen")
	t.Setenv("GOXIDE_TARGET_DIR", "captured-target")
	t.Setenv("GOWORK", "off")
	environment, err := config.Prepare(tool, root, "", false, os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOXIDE_TEST_FROZEN_MARKER", "changed")
	t.Setenv("GOOS", "invalid-ambient-os")
	t.Setenv("GOARCH", "invalid-ambient-arch")
	t.Setenv("GOXIDE_TARGET_DIR", "changed-target")
	t.Setenv("GOWORK", "invalid-ambient-workspace")
	result, err := PrepareProject(root, Options{Offline: true, Config: config.Config{Environment: environment}})
	if err != nil {
		t.Fatal(err)
	}
	if !within(filepath.Join(root, "captured-target"), result.Root) {
		t.Fatalf("generation ignored captured GOXIDE_TARGET_DIR: %s", result.Root)
	}
	if _, err := os.Stat(filepath.Join(root, "changed-target")); !os.IsNotExist(err) {
		t.Fatalf("generation consulted changed GOXIDE_TARGET_DIR: %v", err)
	}
	if got := configuredGo(t, result, "run", "-mod=readonly", result.Entry); got != "true\n" {
		t.Fatalf("captured environment predicate = %q", got)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 8 {
		t.Fatalf("too few Go subprocesses were observed: %q", data)
	}
	want := "frozen|" + environment.Target.OS + "|" + environment.Target.Arch
	for _, line := range lines {
		if line != want {
			t.Fatalf("Go subprocess environment = %q, want %q", line, want)
		}
	}
}

func configuredGo(t *testing.T, result *Result, args ...string) string {
	t.Helper()
	environment := result.Config.Environment
	command := exec.Command(environment.GoTool, args...)
	command.Dir, command.Env = result.Root, project.GoEnv(true, environment.ExecEnv())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func unchangedConfigLock(t *testing.T, root string, before []byte) []byte {
	t.Helper()
	current, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if before != nil && !bytes.Equal(before, current) {
		t.Fatal("configuration-only change modified dependency lock bytes")
	}
	return current
}

func withoutConfigEnv(environment []string, keys ...string) []string {
	var result []string
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		keep := true
		for _, removed := range keys {
			if key == removed || runtime.GOOS == "windows" && strings.EqualFold(key, removed) {
				keep = false
				break
			}
		}
		if keep {
			result = append(result, entry)
		}
	}
	return result
}
