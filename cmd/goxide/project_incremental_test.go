package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectDefaultDiscoveryAndStaleOutputs(t *testing.T) {
	root := t.TempDir()
	projectWrite(t, root, map[string]string{
		"goxide.toml":     "[go_mod]\nmodule='example.org/incremental'\ngo='1.27'\n[[bin]]\nname='app'\npath='src'\n[dependencies]\nfmt={go='fmt'}\n",
		"src/main.gox":    "package main\nuse fmt;\nfunc main(){fmt::Println(\"first\")}\n",
		"src/obsolete.go": "package main\nfunc obsolete(){}\n",
		"native/go.mod":   "module example.org/nested-native\ngo 1.27\n",
		"src/deep/note":   "cwd below a project source directory finds the project",
	})
	t.Chdir(filepath.Join(root, "src", "deep"))
	transpile := func(wantError string) string {
		t.Helper()
		var output, diagnostic bytes.Buffer
		destination := filepath.Join(t.TempDir(), "export")
		err := run([]string{"transpile", "-o", destination, "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), "--offline"}, &output, &diagnostic)
		if wantError != "" {
			if err == nil || !strings.Contains(err.Error(), wantError) {
				t.Fatalf("want %q: %v", wantError, err)
			}
			return ""
		} else if err != nil {
			t.Fatalf("%s: %v", &diagnostic, err)
		}
		if got, want := strings.TrimSpace(output.String()), filepath.Join(destination, "module"); got != want {
			t.Fatalf("export path: got %q, want %q", got, want)
		}
		// Explicit export retains the managed project cache. Observe that cache,
		// rather than each fresh export, when checking incremental generation.
		generated, err := filepath.Glob(filepath.Join(root, "target", "units", "*", "*", "*", "*", "*", "bundle", "module"))
		if err != nil || len(generated) != 1 {
			t.Fatalf("managed module: %v %v", generated, err)
		}
		return generated[0]
	}
	generated := transpile("")
	file := filepath.Join(generated, "src", "main.go")
	before, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if again := transpile(""); again != generated {
		t.Fatalf("unstable module path: %s / %s", generated, again)
	}
	after, err := os.Stat(file)
	if err != nil || before.ModTime() != after.ModTime() {
		t.Fatalf("unchanged source rewritten: %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, ".goxide")); !os.IsNotExist(err) {
		t.Fatalf("project cache leaked: %v", err)
	}
	if err = os.Remove(filepath.Join(root, "src", "obsolete.go")); err != nil {
		t.Fatal(err)
	}
	projectWrite(t, root, map[string]string{"src/main.gox": "package main\nuse fmt;\nfunc main(){fmt::Println(\"second\")}\n"})
	if transpile("") != generated {
		t.Fatal("source edit changed output path")
	}
	if _, err = os.Stat(filepath.Join(generated, "src", "obsolete.go")); !os.IsNotExist(err) {
		t.Fatalf("obsolete Go file retained: %v", err)
	}
	if err = os.WriteFile(file, []byte("user edit"), 0644); err != nil {
		t.Fatal(err)
	}
	transpile("modified or unrecorded generated output")
	if data, err := os.ReadFile(file); err != nil || string(data) != "user edit" {
		t.Fatalf("user edit overwritten: %s %v", data, err)
	}
}

func TestProjectGitWorkspaceCacheIdentity(t *testing.T) {
	t.Skip("MVP-DEFERRED: MVP CLI workspace dependency selection deferred by P6")
	t.Setenv("GIT_AUTHOR_NAME", "Fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.invalid")
	repo, root := filepath.Join(t.TempDir(), "repo"), t.TempDir()
	projectWrite(t, repo, map[string]string{
		"goxide.toml":        "[workspace]\nmembers=['lib','helper']\n[workspace.dependencies.helper]\npath='helper'\nmodule='example.org/helper'\n",
		"lib/goxide.toml":    "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[lib]\nname='lib'\npath='.'\n[dependencies.helper]\nworkspace=true\n",
		"lib/lib.gox":        "package lib\nuse helper;\nfunc Value()int{return helper::Value()}\n",
		"helper/goxide.toml": "[go_mod]\nmodule='example.org/helper'\ngo='1.27'\n[lib]\nname='helper'\npath='.'\n",
		"helper/helper.go":   "package helper\nfunc Value()int{return 42}\n",
	})
	fixtureGit(t, repo, "init", "-b", "main")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "workspace")
	projectWrite(t, root, map[string]string{
		"goxide.toml": fmt.Sprintf("[go_mod]\nmodule='example.org/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n[dependencies]\nfmt={go='fmt'}\nlib={git=%q,subdir='lib',module='example.org/lib'}\n", repo),
		"main.gox":    "package main\nuse fmt;\nuse lib;\nfunc main(){fmt::Println(lib::Value())}\n",
	})
	if out, e := projectRun(t, root, "run"); e != nil || strings.TrimSpace(out) != "42" {
		t.Fatalf("source workspace: %s %v", out, e)
	}
	lock, e := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(lock, []byte(filepath.Join(root, "target"))) {
		t.Fatal("cache path leaked into lock")
	}
	if out, e := projectRun(t, root, "run", "--locked", "--target-dir", "second-target"); e != nil || strings.TrimSpace(out) != "42" {
		t.Fatalf("cache relocation: %s %v", out, e)
	}
	if after, e := os.ReadFile(filepath.Join(root, "goxide.lock")); e != nil || !bytes.Equal(after, lock) {
		t.Fatal("cache relocation changed lock", e)
	}
}
