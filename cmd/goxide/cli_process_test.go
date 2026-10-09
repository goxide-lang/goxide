package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Exercise the actual OS argv/stream/exit boundary, not only run or a parser mock.
// One binary is built for the whole matrix; each case has an isolated working dir.
func TestCLIProcessContract(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	binary := filepath.Join(t.TempDir(), "goxide")
	build := exec.Command(goTool, "build", "-mod=readonly", "-o", binary, "./cmd/goxide")
	build.Dir = repo
	build.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	type result struct {
		stdout, stderr string
		code           int
	}
	execute := func(t *testing.T, dir string, args ...string) result {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOXIDE_TRUST_DIR="+filepath.Join(dir, "trust"), "GOXIDE_TARGET_DIR="+filepath.Join(dir, "target"), "PATH="+filepath.Join(dir, "fakebin")+string(os.PathListSeparator)+os.Getenv("PATH"), "GOXIDE_TEST_GO_MARKER="+filepath.Join(dir, "go-called"))
		var out, diagnostic bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		code := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				code = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if ctx.Err() != nil {
			t.Fatalf("CLI timed out: %q", args)
		}
		return result{out.String(), diagnostic.String(), code}
	}
	fixture := func(t *testing.T) string {
		t.Helper()
		d := t.TempDir()
		if runtime.GOOS != "windows" {
			if err := os.Mkdir(filepath.Join(d, "fakebin"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "fakebin", "go"), []byte("#!/bin/sh\nprintf called > \"$GOXIDE_TEST_GO_MARKER\"\nexit 91\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		for _, name := range []string{"input.gox", "-go", "-input.gox"} {
			if err := os.WriteFile(filepath.Join(d, name), []byte("package main\nfunc main(){}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	assertNoArtifacts := func(t *testing.T, d string) {
		t.Helper()
		for _, name := range []string{"trust", "target", "ffline", "output.go", "go-called"} {
			if _, err := os.Lstat(filepath.Join(d, name)); !os.IsNotExist(err) {
				t.Errorf("unexpected side effect %s: %v", name, err)
			}
		}
	}

	for _, name := range []string{"go", "offline", "features", "source-map", "expanded", "runtime", "tags", "no-default-features", "bin", "example", "lib", "target-dir", "locked", "action", "subject", "plan", "out", "help"} {
		for _, suffix := range []string{"", "=value"} {
			t.Run("migration/"+name+suffix, func(t *testing.T) {
				d := fixture(t)
				got := execute(t, d, "build", "-"+name+suffix, "input.gox")
				if got.code != 1 || got.stdout != "" || !strings.Contains(got.stderr, "-"+name+" is not supported; use --"+name) {
					t.Fatalf("migration result: %+v", got)
				}
				assertNoArtifacts(t, d)
			})
		}
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"empty", nil}, {"unknown-command", []string{"buid"}},
		{"repeat-short", []string{"build", "-hhh"}}, {"unknown-combination", []string{"build", "-aaa", "input.gox"}},
		{"help-does-not-hide-unknown", []string{"build", "-hz"}}, {"unknown-long", []string{"build", "--unknown", "input.gox"}},
		{"missing-value", []string{"build", "--go"}}, {"short-equals", []string{"build", "-o=output.go", "input.gox"}},
		{"boolean-short-equals", []string{"build", "-h=true"}}, {"value-terminator", []string{"build", "-o", "--", "input.gox"}},
		{"long-value-terminator", []string{"build", "--go", "--", "input.gox"}},
		{"flags-after-input", []string{"transpile", "input.gox", "--offline"}},
		{"bool-does-not-consume-false", []string{"build", "--offline", "false", "input.gox"}},
		{"run-unbounded-tail", []string{"run", "input.gox", "arg"}}, {"test-no-passthrough", []string{"test", ".", "--", "-run", "TestX"}},
		{"action-conflict", []string{"scripts", "allow", "--action", "trust-project", "."}},
		{"completion-unknown-shell", []string{"completion", "nosuchshell"}},
		{"no-implicit-trust-project", []string{"scripts", "trust-project"}},
		{"completion-rejects-old-flags", []string{"__complete", "run", "-offline", ""}},
	} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			d := fixture(t)
			got := execute(t, d, tc.args...)
			if got.code != 1 || got.stdout != "" || got.stderr == "" {
				t.Fatalf("expected stderr/exit1: %+v", got)
			}
			assertNoArtifacts(t, d)
		})
	}
	for _, args := range [][]string{
		{"--help"}, {"help", "scripts"}, {"build", "-h"}, {"build", "--help"},
		{"build", "-hooutput.go"}, {"build", "-ho", "output.go"},
		{"scripts", "allow", "--help"}, {"scripts", "trust-project", "--help"},
		{"import-go", "--help"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"},
	} {
		t.Run("help-or-completion/"+strings.Join(args, "_"), func(t *testing.T) {
			d := fixture(t)
			// Even an invalid manifest must not be inspected by UI-only calls.
			if err := os.WriteFile(filepath.Join(d, "goxide.toml"), []byte("invalid [ manifest"), 0600); err != nil {
				t.Fatal(err)
			}
			got := execute(t, d, args...)
			if got.code != 0 || got.stdout == "" || got.stderr != "" {
				t.Fatalf("expected stdout/exit0: %+v", got)
			}
			assertNoArtifacts(t, d)
		})
	}
	for _, args := range [][]string{{"__complete", "run", "--go", ""}, {"__complete", "scripts", "trust-project", ""}} {
		t.Run("dynamic-completion/"+strings.Join(args, "_"), func(t *testing.T) {
			d := fixture(t)
			got := execute(t, d, args...)
			if got.code != 0 || !strings.Contains(got.stdout, ":") {
				t.Fatalf("completion protocol: %+v", got)
			}
			assertNoArtifacts(t, d)
		})
	}

	for _, tc := range []struct {
		name   string
		flags  []string
		output string
	}{
		{"separate", []string{"-o", "output.go"}, "output.go"},
		{"attached", []string{"-ooutput.go"}, "output.go"},
		{"negative-value", []string{"-o", "-aaa"}, "-aaa"},
		{"numeric-negative", []string{"-o", "-7"}, "-7"},
		{"literal-terminator", []string{"--o=--"}, "--"},
		{"repeated-last-wins", []string{"-o", "first.go", "--o", "output.go"}, "output.go"},
	} {
		t.Run("output/"+tc.name, func(t *testing.T) {
			d := fixture(t)
			args := append([]string{"transpile"}, tc.flags...)
			got := execute(t, d, append(args, "input.gox")...)
			if got.code != 0 || got.stderr != "" {
				t.Fatalf("output flags: %+v", got)
			}
			content, err := os.ReadFile(filepath.Join(d, tc.output))
			if err != nil || !bytes.Contains(content, []byte("func main()")) {
				t.Fatalf("missing generated output: %s %v", content, err)
			}
			if tc.name == "repeated-last-wins" {
				if _, err := os.Stat(filepath.Join(d, "first.go")); !os.IsNotExist(err) {
					t.Fatal("earlier output was written")
				}
			}
		})
	}
	for _, args := range [][]string{
		{"transpile", "--go", "-offline", "input.gox"},
		{"transpile", "--offline=false", "--offline=false", "input.gox"},
		{"transpile", "--", "-go"},
	} {
		t.Run("accepted/"+strings.Join(args, "_"), func(t *testing.T) {
			got := execute(t, fixture(t), args...)
			if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "func main()") {
				t.Fatalf("accepted syntax: %+v", got)
			}
		})
	}
	t.Run("scripts-action-named-directories", func(t *testing.T) {
		// The bare legacy form has no --go option. Put the actual toolchain
		// ahead of any system command called go without creating a fake runner.
		t.Setenv("PATH", filepath.Dir(goTool)+string(os.PathListSeparator)+os.Getenv("PATH"))
		for _, name := range []string{"plan", "allow", "trust-project"} {
			d := t.TempDir()
			root := filepath.Join(d, name)
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			for file, content := range map[string]string{
				"goxide.toml": "[package]\nformat=2\nmodule='example.org/routefixture'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n",
				"main.gox":    "package main\nfunc main(){}\n",
			} {
				if err := os.WriteFile(filepath.Join(root, file), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var first string
			for _, args := range [][]string{{"scripts", name}, {"scripts", "--", name}} {
				got := execute(t, d, args...)
				if got.code != 0 || got.stderr != "" {
					t.Fatalf("same-name path %q: %+v", args, got)
				}
				var plan struct {
					Identity string `json:"root_identity"`
					Trusted  bool   `json:"root_trusted"`
				}
				if err := json.Unmarshal([]byte(got.stdout), &plan); err != nil {
					t.Fatalf("not a plan: %s: %v", got.stdout, err)
				}
				if plan.Identity == "" || plan.Trusted {
					t.Fatalf("same-name path unexpectedly trusted: %+v", plan)
				}
				if first != "" && first != got.stdout {
					t.Fatalf("delimiter changed default plan: %s / %s", first, got.stdout)
				}
				first = got.stdout
			}
		}
	})

	t.Run("scripts-canonical-legacy-identity", func(t *testing.T) {
		d := t.TempDir()
		projectRoot := filepath.Join(d, "project")
		if err := os.Mkdir(projectRoot, 0700); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{
			"goxide.toml": "[package]\nformat=2\nmodule='example.org/scriptcli'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n",
			"main.gox":    "package main\nfunc main(){}\n",
		} {
			if err := os.WriteFile(filepath.Join(projectRoot, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		call := func(action string, legacy bool) result {
			args := []string{"scripts"}
			if legacy {
				args = append(args, "--action", action)
			} else {
				args = append(args, action)
			}
			args = append(args, "--go", goTool, "--offline", "project")
			got := execute(t, d, args...)
			if got.code != 0 || got.stderr != "" {
				t.Fatalf("scripts %s legacy=%v: %+v", action, legacy, got)
			}
			return got
		}
		canonical, legacy := call("plan", false), call("plan", true)
		if canonical.stdout != legacy.stdout {
			t.Fatalf("canonical and legacy plan differ: %s / %s", canonical.stdout, legacy.stdout)
		}
		var initial struct {
			Identity string `json:"root_identity"`
			Trusted  bool   `json:"root_trusted"`
		}
		if err := json.Unmarshal([]byte(canonical.stdout), &initial); err != nil {
			t.Fatal(err)
		}
		if initial.Identity == "" || initial.Trusted {
			t.Fatalf("unexpected initial plan: %+v", initial)
		}
		for _, legacyGrant := range []bool{false, true} {
			call("trust-project", legacyGrant)
			var granted struct {
				Identity string `json:"root_identity"`
				Trusted  bool   `json:"root_trusted"`
			}
			if err := json.Unmarshal([]byte(call("plan", !legacyGrant).stdout), &granted); err != nil {
				t.Fatal(err)
			}
			if !granted.Trusted || granted.Identity != initial.Identity {
				t.Fatalf("grant identity mismatch: %+v", granted)
			}
			call("forget-project", !legacyGrant)
			if got := call("plan", legacyGrant); got.stdout != canonical.stdout {
				t.Fatalf("revocation did not restore same identity: %s", got.stdout)
			}
		}
	})

	for _, mode := range []string{"directory", "project"} {
		t.Run("native-run-"+mode, func(t *testing.T) {
			d := t.TempDir()
			files := map[string]string{
				"main.gox": "package main\nuse json;\nuse os;\nfunc main(){json::NewEncoder(os::Stdout).Encode(os::Args[1:])}\n",
			}
			if mode == "directory" {
				files["go.mod"] = "module example.org/cliargv\ngo 1.27\n"
				files["goxide.roots.toml"] = fixtureToolchainRoots
			} else {
				files["goxide.toml"] = fixtureNamespaceManifest(t, "[package]\nformat=2\nmodule='example.org/cliargv'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.gox'\n")
			}
			for name, content := range files {
				if err := os.WriteFile(filepath.Join(d, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"run", "--go", goTool}
			if mode == "project" {
				args = append(args, "--offline")
			}
			args = append(args, ".", "--", "--offline", "-aaa", "", "--")
			got := execute(t, d, args...)
			if got.code != 0 || got.stderr != "" || strings.TrimSpace(got.stdout) != `["--offline","-aaa","","--"]` {
				t.Fatalf("%s program argv: %+v", mode, got)
			}
		})
	}

	t.Run("native-run-verbatim-argv", func(t *testing.T) {
		d := fixture(t)
		fixtureRootsFile(t, d, "")
		source := []byte("package main\nuse json;\nuse os;\nfunc main(){json::NewEncoder(os::Stdout).Encode(os::Args[1:])}\n")
		for _, input := range []string{"input.gox", "-input.gox"} {
			if err := os.WriteFile(filepath.Join(d, input), source, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"run", "--go", goTool, "--runtime", repo}
			if strings.HasPrefix(input, "-") {
				args = append(args, "--")
			}
			args = append(args, input, "--", "--offline", "-aaa", "", "--", "a b", "$(do-not-execute)")
			got := execute(t, d, args...)
			if got.code != 0 || got.stderr != "" {
				t.Fatalf("native run: %+v", got)
			}
			var actual []string
			if err := json.Unmarshal([]byte(got.stdout), &actual); err != nil {
				t.Fatal(err)
			}
			want := []string{"--offline", "-aaa", "", "--", "a b", "$(do-not-execute)"}
			if len(actual) != len(want) {
				t.Fatalf("argv: %q", actual)
			}
			for i := range want {
				if actual[i] != want[i] {
					t.Fatalf("argv: %q", actual)
				}
			}
		}
	})
}
