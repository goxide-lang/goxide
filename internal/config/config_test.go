package config

import (
	"bytes"
	"encoding/json"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestTargetFormatsAndLayout(t *testing.T) {
	for _, test := range []struct {
		text, platform, endian string
		width                  int
	}{
		{"386-unknown-linux", "unix", "little", 32},
		{"linux/mips", "unix", "big", 32},
		{"aix/ppc64", "unix", "big", 64},
		{"js/wasm", "wasm", "little", 64},
		{"wasip1/wasm", "wasm", "little", 64},
		{"plan9/arm", "plan9", "little", 32},
		{"windows/amd64", "windows", "little", 64},
	} {
		target, err := ParseTarget(test.text)
		if err != nil {
			t.Fatal(err)
		}
		if target.Platform != test.platform || target.Endian != test.endian || target.PointerWidth != test.width {
			t.Errorf("%s has wrong derived values: %+v", test.text, target)
		}
		alias, err := ParseTarget(target.String())
		if err != nil || alias != target {
			t.Errorf("canonical target does not round trip: %v %v", alias, err)
		}
	}
	if targets := Targets(); len(targets) != 47 {
		t.Fatalf("target baseline has %d combinations, want 47", len(targets))
	}
	for _, target := range Targets() {
		size := types.SizesFor("gc", target.Arch).Sizeof(types.NewPointer(types.Typ[types.Int]))
		if int(size)*8 != target.PointerWidth {
			t.Errorf("%s pointer width=%d, Go types width=%d", target, target.PointerWidth, size*8)
		}
	}
	for _, text := range []string{"linux", "amd64-linux", "x86_64-unknown-linux", "amd64-pc-linux", "amd64-unknown-linux-gnu", "js/amd64", "windows/arm", "Linux/amd64", "linux/amd64/gnu"} {
		if _, err := ParseTarget(text); err == nil {
			t.Errorf("accepted invalid target %q", text)
		}
	}
}

func TestEnvironmentSnapshotAndReadIdentity(t *testing.T) {
	captured := []string{"GOOS=darwin", "EDITION=A", "UNUSED=one", "EMPTY=", "BAD=\xff"}
	e, err := NewEnvironment(Target{OS: "linux", Arch: "386"}, captured)
	if err != nil {
		t.Fatal(err)
	}
	captured[1] = "EDITION=B"
	if value, present := e.LookupEnv("EDITION"); !present || value != "A" {
		t.Fatalf("snapshot changed with source slice: %q %v", value, present)
	}
	if value, _ := e.LookupEnv("GOOS"); value != "darwin" || e.Target.OS != "linux" {
		t.Fatalf("raw environment was replaced by target: %q %+v", value, e.Target)
	}
	execution := e.ExecEnv()
	execution[0] = "EDITION=changed"
	values := e.GoValues()
	values["GOOS"] = "changed"
	if e.GoValues()["GOOS"] != "linux" || reflect.DeepEqual(execution, e.ExecEnv()) {
		t.Fatal("environment accessors exposed mutable storage")
	}
	var reads Reads
	for _, key := range []string{"MISSING", "EMPTY", "EDITION", "BAD"} {
		value, present := e.LookupEnv(key)
		reads.Record(key, present, value)
	}
	inputs := reads.Inputs()
	if got := []string{inputs[0].Key, inputs[1].Key, inputs[2].Key, inputs[3].Key}; !reflect.DeepEqual(got, []string{"BAD", "EDITION", "EMPTY", "MISSING"}) {
		t.Fatalf("inputs not sorted or unused environment leaked: %v", got)
	}
	if !inputs[2].Present || inputs[2].Value == nil || inputs[3].Present || inputs[3].Value != nil {
		t.Fatalf("unset and empty were merged: %+v", inputs)
	}
	first := reads.Digest()
	reads.Record("EDITION", true, "B")
	if reads.Digest() == first {
		t.Fatal("value change did not change identity")
	}
	reads.Record("EDITION", true, "A")
	if reads.Digest() != first {
		t.Fatal("A to B to A did not restore identity")
	}
	inputs[0].Value[0] = 'x'
	if reads.Digest() != first {
		t.Fatal("Inputs exposed mutable read storage")
	}
}

func TestReadIdentityPreservesAllEnvironmentBytes(t *testing.T) {
	seen := map[string]bool{}
	for _, test := range []struct {
		key, value string
		present    bool
	}{
		{"KEY", "", false}, {"KEY", "", true}, {"KEY", "\xff", true}, {"KEY", "\xfe", true},
		{"\xff", "same", true}, {"\xfe", "same", true},
	} {
		var reads Reads
		reads.Record(test.key, test.present, test.value)
		digest := reads.Digest()
		if seen[digest] {
			t.Fatalf("environment identities collided: %+v", test)
		}
		seen[digest] = true
		encoded, err := json.Marshal(reads.Inputs())
		if err != nil {
			t.Fatal(err)
		}
		var decoded []struct {
			Key, Value []byte
			Present    bool
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if len(decoded) != 1 || string(decoded[0].Key) != test.key || string(decoded[0].Value) != test.value || decoded[0].Present != test.present {
			t.Fatalf("JSON lost environment bytes: %s", encoded)
		}
	}
}

func TestWindowsEnvironmentNames(t *testing.T) {
	values, err := environmentMap([]string{"Edition=one", "EDITION=one"}, true)
	if err != nil {
		t.Fatal(err)
	}
	e := &Environment{Host: Target{OS: "windows"}, process: values}
	if value, present := e.LookupEnv("edition"); !present || value != "one" || len(values) != 1 {
		t.Fatalf("case aliases not normalized: %v %q %v", values, value, present)
	}
	if _, err := environmentMap([]string{"Edition=one", "EDITION=two"}, true); err == nil {
		t.Fatal("conflicting Windows entries accepted")
	}
}

func TestPrepareWorkflowTargetsAndFrozenGoDefaults(t *testing.T) {
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	goEnvFile := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(goEnvFile, []byte("GOOS=windows\nGOARCH=amd64\nCGO_ENABLED=0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	captured := testEnvironment(os.Environ(), map[string]string{"GOENV": goEnvFile}, "GOOS", "GOARCH", "CGO_ENABLED")
	native, err := Prepare(goTool, "", "", true, captured)
	if err != nil {
		t.Fatal(err)
	}
	if native.Target.OS != "windows" || native.Target.Arch != "amd64" {
		t.Fatalf("native generation ignored saved Go target: %+v", native.Target)
	}
	if _, present := native.LookupEnv("GOOS"); present {
		t.Fatal("saved Go default appeared in raw process environment")
	}
	host, err := Prepare(goTool, "", "", false, captured)
	if err != nil {
		t.Fatal(err)
	}
	if host.Target != HostTarget() {
		t.Fatalf("toolchain default followed saved Go target: %+v", host.Target)
	}
	captured = testEnvironment(captured, map[string]string{"GOOS": "darwin", "GOARCH": "arm64", "GOAMD64": "v3"})
	selected, err := Prepare(goTool, "", "amd64-unknown-linux", false, captured)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := Prepare(goTool, "", "linux/amd64", false, captured)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Target != alias.Target || !reflect.DeepEqual(selected.GoValues(), alias.GoValues()) {
		t.Fatal("target aliases produce different effective configuration")
	}
	if raw, _ := selected.LookupEnv("GOOS"); raw != "darwin" || selected.Target.OS != "linux" {
		t.Fatalf("target override mutated raw environment: %q %+v", raw, selected.Target)
	}
	context := selected.Context([]string{"custom"}, true)
	if context.GOOS != "linux" || context.GOARCH != "amd64" || context.CgoEnabled || !slices.Contains(context.ToolTags, "amd64.v3") || !slices.Contains(context.BuildTags, "race") {
		t.Fatalf("Go build context differs from resolved settings: %+v", context)
	}
	if err := os.WriteFile(goEnvFile, []byte("GOOS=plan9\nGOARCH=386\nCGO_ENABLED=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(goTool, "env", "GOOS", "GOARCH", "CGO_ENABLED", "GOAMD64")
	command.Env = selected.ExecEnv()
	actual, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, []byte("linux\namd64\n0\nv3\n")) {
		t.Fatalf("child Go reread defaults or lost target context: %q", actual)
	}
	if _, err := Prepare(goTool, "", "linux/amd64", true, captured); err == nil {
		t.Fatal("native generation accepted --target")
	}
}

func TestPrepareUsesSelectedDirectory(t *testing.T) {
	input, unrelated := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(input, "go.mod"), []byte("module selected.example/project\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "go.mod"), []byte("invalid module syntax\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(unrelated)
	captured := testEnvironment(os.Environ(), map[string]string{"GOENV": "off", "CGO_ENABLED": "0", "GOAMD64": "v3"})
	environment, err := Prepare("", input, "linux/amd64", false, captured)
	if err != nil {
		t.Fatal(err)
	}
	if context := environment.Context(nil, false); !slices.Contains(context.ToolTags, "amd64.v3") {
		t.Fatalf("selected directory lost resolved architecture tags: %v", context.ToolTags)
	}
}

func testEnvironment(source []string, overrides map[string]string, removed ...string) []string {
	values, _ := environmentMap(source, runtime.GOOS == "windows")
	for _, key := range removed {
		delete(values, key)
	}
	for key, value := range overrides {
		values[key] = value
	}
	// The test's Go configuration remains independent of the invoking shell.
	delete(values, "GOFLAGS")
	for key := range values {
		if strings.HasPrefix(key, "CGO_") && key != "CGO_ENABLED" {
			delete(values, key)
		}
	}
	return environmentSlice(values)
}
