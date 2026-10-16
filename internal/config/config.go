// Package config holds one invocation's feature and environment inputs.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/build"
	"go/types"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

// A nil Declared set permits any valid feature name, as in a native Go module.
type Features struct {
	Enabled, Declared map[string]bool
}

// Features vary by source module; Environment and Reads belong to the invocation.
type Config struct {
	Features    Features
	Environment *Environment
	Reads       *Reads
}

func (c Config) Sizes() types.Sizes {
	if c.Environment == nil {
		return types.SizesFor("gc", runtime.GOARCH)
	}
	return c.Environment.Sizes()
}

type Target struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	Platform     string `json:"platform"`
	Endian       string `json:"endian"`
	PointerWidth int    `json:"pointer_width"`
}

func (t Target) String() string { return t.Arch + "-unknown-" + t.OS }

var combinations = map[string][]string{
	"aix":       {"ppc64"},
	"android":   {"386", "amd64", "arm", "arm64"},
	"darwin":    {"amd64", "arm64"},
	"dragonfly": {"amd64"},
	"freebsd":   {"386", "amd64", "arm", "arm64"},
	"illumos":   {"amd64"},
	"ios":       {"amd64", "arm64"},
	"js":        {"wasm"},
	"linux":     {"386", "amd64", "arm", "arm64", "loong64", "mips", "mips64", "mips64le", "mipsle", "ppc64", "ppc64le", "riscv64", "s390x"},
	"netbsd":    {"386", "amd64", "arm", "arm64"},
	"openbsd":   {"386", "amd64", "arm", "arm64", "ppc64", "riscv64"},
	"plan9":     {"386", "amd64", "arm"},
	"solaris":   {"amd64"},
	"wasip1":    {"wasm"},
	"windows":   {"386", "amd64", "arm64"},
}

func ParseTarget(text string) (Target, error) {
	var os, arch string
	if strings.Contains(text, "/") {
		parts := strings.Split(text, "/")
		if len(parts) == 2 {
			os, arch = parts[0], parts[1]
		}
	} else {
		parts := strings.Split(text, "-")
		if len(parts) == 3 && parts[1] == "unknown" {
			arch, os = parts[0], parts[2]
		}
	}
	if os == "" || arch == "" {
		return Target{}, fmt.Errorf("invalid target %q: expected GOARCH-unknown-GOOS or GOOS/GOARCH", text)
	}
	return target(os, arch)
}

func target(os, arch string) (Target, error) {
	for _, candidate := range combinations[os] {
		if candidate != arch {
			continue
		}
		t := Target{OS: os, Arch: arch, Platform: "unix", Endian: "little", PointerWidth: 64}
		switch os {
		case "windows", "plan9":
			t.Platform = os
		case "js", "wasip1":
			t.Platform = "wasm"
		}
		switch arch {
		case "386", "arm", "mips", "mipsle":
			t.PointerWidth = 32
		}
		switch arch {
		case "mips", "mips64", "ppc64", "s390x":
			t.Endian = "big"
		}
		return t, nil
	}
	return Target{}, fmt.Errorf("unsupported target %s/%s; use goxide env --list for supported values", os, arch)
}

func HostTarget() Target {
	t, _ := target(runtime.GOOS, runtime.GOARCH)
	return t
}

func Targets() []Target {
	var result []Target
	for os, arches := range combinations {
		for _, arch := range arches {
			t, _ := target(os, arch)
			result = append(result, t)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].OS != result[j].OS {
			return result[i].OS < result[j].OS
		}
		return result[i].Arch < result[j].Arch
	})
	return result
}

func Values(predicate string) []string {
	values := map[string]bool{}
	for _, t := range Targets() {
		var value string
		switch predicate {
		case "target_os":
			value = t.OS
		case "target_arch":
			value = t.Arch
		case "target_platform":
			value = t.Platform
		case "target_endian":
			value = t.Endian
		case "target_pointer_width":
			value = fmt.Sprint(t.PointerWidth)
		default:
			return nil
		}
		values[value] = true
	}
	var result []string
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// Environment is prepared once. Accessors return copies of its mutable data.
type Environment struct {
	Host, Target      Target
	GoTool, GoVersion string
	SupportedTargets  []Target
	process           map[string]string
	execution         []string
	values            map[string]string
	toolTags          []string
	releaseTags       []string
}

// NewEnvironment supplies an explicit context for in-memory compilation.
// Unlike Prepare it does not discover or execute a Go toolchain.
func NewEnvironment(selected Target, captured []string) (*Environment, error) {
	selected, err := target(selected.OS, selected.Arch)
	if err != nil {
		return nil, err
	}
	process, err := environmentMap(captured, runtime.GOOS == "windows")
	if err != nil {
		return nil, err
	}
	execution := cloneMap(process)
	execution["GOOS"], execution["GOARCH"] = selected.OS, selected.Arch
	execution["GOTOOLCHAIN"], execution["GOWORK"], execution["GOFLAGS"] = "local", "off", ""
	return &Environment{
		Host: HostTarget(), Target: selected, GoTool: filepath.Join(runtime.GOROOT(), "bin", "go"), GoVersion: runtime.Version(), process: process,
		execution: environmentSlice(execution), values: map[string]string{"GOOS": selected.OS, "GOARCH": selected.Arch, "GOVERSION": runtime.Version()},
		releaseTags: append([]string(nil), build.Default.ReleaseTags...),
	}, nil
}

func (e *Environment) LookupEnv(key string) (string, bool) {
	if e.Host.OS == "windows" {
		key = strings.ToUpper(key)
	}
	value, present := e.process[key]
	return value, present
}

func (e *Environment) EnvKey(key string) string {
	if e.Host.OS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func (e *Environment) ExecEnv() []string           { return append([]string(nil), e.execution...) }
func (e *Environment) GoValues() map[string]string { return cloneMap(e.values) }
func (e *Environment) Sizes() types.Sizes          { return types.SizesFor("gc", e.Target.Arch) }

func (e *Environment) Context(tags []string, race bool) build.Context {
	c := build.Default
	c.GOOS, c.GOARCH, c.Compiler = e.Target.OS, e.Target.Arch, "gc"
	c.CgoEnabled = e.values["CGO_ENABLED"] == "1"
	if root := e.values["GOROOT"]; root != "" {
		c.GOROOT = root
	}
	c.BuildTags = append([]string(nil), tags...)
	if race {
		c.BuildTags = append(c.BuildTags, "race")
	}
	c.ToolTags = append([]string(nil), e.toolTags...)
	c.ReleaseTags = append([]string(nil), e.releaseTags...)
	return c
}

// Value uses bytes so JSON hashing preserves invalid UTF-8 as well as empty values.
type EnvInput struct {
	Key     string `json:"key"`
	Present bool   `json:"present"`
	Value   []byte `json:"value"`
}

func (i EnvInput) MarshalJSON() ([]byte, error) {
	// Environment keys can contain invalid UTF-8 on Unix as well.
	return json.Marshal(struct {
		Key     []byte `json:"key"`
		Present bool   `json:"present"`
		Value   []byte `json:"value"`
	}{[]byte(i.Key), i.Present, i.Value})
}

type Reads struct {
	mu      sync.Mutex
	inputs  map[string]EnvInput
	targets map[*ast.File]bool
}

func (r *Reads) Record(key string, present bool, value string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inputs == nil {
		r.inputs = map[string]EnvInput{}
	}
	input := EnvInput{Key: key, Present: present}
	if present {
		input.Value = []byte(value)
	}
	r.inputs[key] = input
}

func (r *Reads) Inputs() []EnvInput {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []EnvInput
	for _, input := range r.inputs {
		if input.Present {
			input.Value = append([]byte{}, input.Value...)
		}
		result = append(result, input)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

func (r *Reads) Digest() string {
	encoded, _ := json.Marshal(r.Inputs())
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (r *Reads) RecordTarget(file *ast.File) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.targets == nil {
		r.targets = map[*ast.File]bool{}
	}
	r.targets[file] = true
}

func (r *Reads) TargetFiles() []*ast.File {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []*ast.File
	for file := range r.targets {
		result = append(result, file)
	}
	return result
}

func environmentMap(values []string, windows bool) (map[string]string, error) {
	result := map[string]string{}
	for _, entry := range values {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if windows {
			key = strings.ToUpper(key)
			if previous, ok := result[key]; ok && previous != value {
				return nil, fmt.Errorf("conflicting environment entries for %s", key)
			}
		}
		result[key] = value
	}
	return result, nil
}

func cloneMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func environmentSlice(values map[string]string) []string {
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
