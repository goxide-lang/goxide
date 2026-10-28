package testutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

var formalWASIBudgetMultiplier time.Duration = 1

// FormalDependencies names the exact published libraries required by this
// compiler checkout. A neighbouring checkout and go.mod replace are irrelevant.
func FormalDependencies(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "../..")
	module, err := project.ReadMod(filepath.Join(runtime.GOROOT(), "bin", "go"), root)
	if err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{}
	for _, dependency := range module.Require {
		versions[dependency.Path] = dependency.Version
	}
	var result strings.Builder
	for _, name := range []string{"std", "serde"} {
		path := "github.com/goxide-lang/" + name
		if versions[path] == "" {
			t.Fatalf("compiler has no published %s pin", name)
		}
		fmt.Fprintf(&result, "[dependencies.%s]\ngo=%q\nversion=%q\n", name, path, versions[path])
	}
	return result.String()
}

// FormalManifest describes an authored library project with ordinary std and
// serde source dependencies. Tests write their source syntax without rewriting.
func FormalManifest(t *testing.T, module, name string, extraDependencies ...string) string {
	t.Helper()
	return fmt.Sprintf("[go_mod]\nmodule=%q\ngo=\"1.27\"\n[lib]\nname=%q\npath=\".\"\n", module, name) + FormalDependencies(t) + strings.Join(extraDependencies, "\n")
}

func PrepareFormal(t *testing.T, root string, overrides ...compile.Options) (*compile.Result, int, error) {
	return prepareFormal(t, root, true, overrides...)
}

func PrepareFormalBinary(t *testing.T, root string, overrides ...compile.Options) (*compile.Result, int, error) {
	return prepareFormal(t, root, false, overrides...)
}

func prepareFormal(t *testing.T, root string, library bool, overrides ...compile.Options) (*compile.Result, int, error) {
	t.Helper()
	started := time.Now()
	var macroHostElapsed time.Duration
	calls := 0
	defer func() {
		t.Logf("formal preparation: elapsed=%.3fs macro_host_elapsed=%.3fs macro_calls=%d (host includes WASM preparation, compilation, and execution)", time.Since(started).Seconds(), macroHostElapsed.Seconds(), calls)
	}()
	options := compile.Options{}
	if len(overrides) > 1 {
		t.Fatal("formal preparation accepts at most one option set")
	}
	if len(overrides) == 1 {
		options = overrides[0]
	}
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	_, here, _, _ := runtime.Caller(0)
	compilerRoot := filepath.Join(filepath.Dir(here), "../..")
	identity, err := procmacro.ReadSDKIdentity(context.Background(), tool, compilerRoot, project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	defer func() { procmacro.CompilerSDKIdentity = previous }()
	host, err := procmacro.NewWASIHostWithBudgets(context.Background(), procmacro.MaxInvocationTime*formalWASIBudgetMultiplier, procmacro.MaxCompilationTime*formalWASIBudgetMultiplier)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	options.Lib, options.Offline = library, true
	options.RunProcMacro = func(wasm []byte, request pm.Request) ([]byte, error) {
		calls++
		invocationStarted := time.Now()
		defer func() { macroHostElapsed += time.Since(invocationStarted) }()
		return host.Run(wasm, request)
	}
	result, err := compile.PrepareProject(root, options)
	return result, calls, err
}

type FormalResult struct {
	Project    *compile.Result
	Go         []byte
	MacroCalls int
}

func FormalSource(t *testing.T, filename string, source []byte, extraDependencies ...string) (FormalResult, error) {
	return formalSource(t, filename, source, true, extraDependencies...)
}

// FormalBinarySource selects an executable fixture while preserving package main.
func FormalBinarySource(t *testing.T, filename string, source []byte, extraDependencies ...string) (FormalResult, error) {
	return formalSource(t, filename, source, false, extraDependencies...)
}

func formalSource(t *testing.T, filename string, source []byte, library bool, extraDependencies ...string) (FormalResult, error) {
	t.Helper()
	root := t.TempDir()
	manifest := FormalManifest(t, "fixture", "fixture", extraDependencies...)
	if !library {
		manifest = strings.Replace(manifest, "[lib]", "[[bin]]", 1)
	}
	if err := os.WriteFile(filepath.Join(root, "goxide.toml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.Base(filename)), source, 0600); err != nil {
		t.Fatal(err)
	}
	result, calls, err := prepareFormal(t, root, library)
	if err != nil {
		return FormalResult{}, err
	}
	name := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)) + ".go"
	data, err := os.ReadFile(filepath.Join(result.Root, name))
	if err != nil {
		t.Fatal(err)
	}
	return FormalResult{Project: result, Go: data, MacroCalls: calls}, nil
}

func RunFormalGo(t *testing.T, result *compile.Result, args ...string) string {
	t.Helper()
	environment := result.Config.Environment
	command := exec.Command(environment.GoTool, args...)
	command.Dir, command.Env = result.Root, project.GoEnv(true, environment.ExecEnv())
	started := time.Now()
	output, err := command.CombinedOutput()
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("formal generated Go %s: elapsed=%.3fs: %v\n%s", strings.Join(args, " "), elapsed.Seconds(), err, output)
	}
	t.Logf("formal generated Go %s: elapsed=%.3fs\n%s", strings.Join(args, " "), elapsed.Seconds(), output)
	return string(output)
}
