package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDependencySourceModuleIdentity(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, source := range []string{"path", "git"} {
		for _, format := range []string{"go", "goxide"} {
			t.Run(source+"/"+format, func(t *testing.T) {
				root := t.TempDir()
				app, repo := filepath.Join(root, "app"), filepath.Join(root, "dep")
				dep := repo
				if source == "git" {
					dep = filepath.Join(repo, "packages", "lib")
				}
				if format == "go" {
					wsWrite(t, dep, "go.mod", "module example.org/lib\ngo 1.27\n")
				} else {
					wsWrite(t, dep, ManifestName, "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[lib]\nname='lib'\npath='.'\n")
				}
				wsWrite(t, dep, "lib.go", "package lib\nfunc Value() int { return 42 }\n")
				wsWrite(t, dep, "hidden/hidden.go", "package hidden\n")
				declaration := "path='../dep'"
				if source == "git" {
					for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}} {
						if _, err := git(repo, args...); err != nil {
							t.Fatal(err)
						}
					}
					declaration = fmt.Sprintf("git=%q\nsubdir='packages/lib'", repo)
				}
				manifest := mappingHeader + "[dependencies.lib]\n" + declaration + "\n"
				wsWrite(t, app, ManifestName, manifest)
				graph, err := Load(app, tool, nil, false, source != "git", false)
				if err != nil {
					t.Fatal(err)
				}
				if graph.Root.Manifest.Dependencies["lib"].Module != "" || graph.Root.Sources["lib"].Module != "example.org/lib" || graph.Root.Children["lib"].Module != "example.org/lib" {
					t.Fatalf("source identity did not stay separate from author input: %+v", graph.Root)
				}
				if err := graph.Save(); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
				if err != nil {
					t.Fatal(err)
				}
				replay, err := Load(app, tool, nil, false, true, true)
				if err != nil {
					t.Fatal(err)
				}
				if err := replay.Save(); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
				if err != nil || string(before) != string(after) {
					t.Fatalf("locked replay changed lock: %v", err)
				}
				// Adding an identity assertion must reuse the same acquired source.
				wsWrite(t, app, ManifestName, manifest+"module='example.org/lib'\n")
				if _, err := Load(app, tool, nil, false, true, false); err != nil {
					t.Fatal(err)
				}
				wsWrite(t, app, ManifestName, manifest+"module='example.org/wrong'\n")
				if _, err := Load(app, tool, nil, false, true, false); err == nil || !strings.Contains(err.Error(), "module path mismatch") {
					t.Fatalf("identity assertion was not checked: %v", err)
				}
				if source == "git" {
					manifest = strings.Replace(manifest, "packages/lib'", "packages/lib/hidden'", 1)
				} else {
					manifest = strings.Replace(manifest, "../dep'", "../dep/hidden'", 1)
				}
				wsWrite(t, app, ManifestName, manifest)
				if _, err := Load(app, tool, nil, false, source != "git", false); err == nil || !strings.Contains(err.Error(), "must contain its own regular go.mod") {
					t.Fatalf("ordinary subpackage became a module root: %v", err)
				}
			})
		}
	}
}

func TestWorkspaceInferredDependencyIdentity(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, ManifestName, "[workspace]\nmembers=['app', 'lib']\n[workspace.dependencies.lib]\npath='lib'\n")
	wsWrite(t, root, "app/"+ManifestName, mappingHeader+"[dependencies.lib]\nworkspace=true\n")
	wsWrite(t, root, "lib/"+ManifestName, "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[lib]\nname='lib'\npath='.'\n")
	wsWrite(t, root, "lib/lib.go", "package lib\n")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, locked := range []bool{false, true} {
		graph, err := Load(filepath.Join(root, "app"), tool, nil, false, true, locked)
		if err != nil {
			t.Fatal(err)
		}
		if graph.Root.Manifest.Dependencies["lib"].Module != "" || graph.Root.Sources["lib"].Module != "example.org/lib" || graph.Root.Children["lib"].Root != filepath.Join(root, "lib") {
			t.Fatalf("inherited dependency identity: %+v", graph.Root)
		}
		if err := graph.Save(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCopiedNativeLockInferredDependency(t *testing.T) {
	root := t.TempDir()
	app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
	manifest := mappingHeader + "[dependencies.lib]\npath='../lib'\n"
	library := "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[lib]\nname='lib'\npath='.'\n"
	wsWrite(t, app, ManifestName, manifest)
	wsWrite(t, lib, ManifestName, library)
	wsWrite(t, lib, "lib.go", "package lib\n")
	m, err := Read(app)
	if err != nil {
		t.Fatal(err)
	}
	lm, err := Read(lib)
	if err != nil {
		t.Fatal(err)
	}
	w := &Workspace{Root: root, Members: map[string]string{pathSource(app).key(): app, pathSource(lib).key(): lib}, Manifests: map[string]Manifest{app: m, lib: lm}}
	lock := Lock{Version: 4, NativeWorkspace: &NativeWorkspaceLock{Modules: map[string]Selection{}}}
	if err := CaptureNativeMembers(w, lock.NativeWorkspace); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	copyRoot := t.TempDir()
	wsWrite(t, copyRoot, "consumer/"+ManifestName, strings.Replace(manifest, "../lib", "../relocated", 1))
	wsWrite(t, copyRoot, "consumer/goxide.lock", string(data))
	wsWrite(t, copyRoot, "relocated/"+ManifestName, library)
	wsWrite(t, copyRoot, "relocated/lib.go", "package lib\n")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	graph, err := Load(filepath.Join(copyRoot, "consumer"), tool, nil, false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if graph.Root.Sources["lib"].Module != lm.Module || graph.Root.Children["lib"].Root != filepath.Join(copyRoot, "relocated") {
		t.Fatalf("copied lock lost inferred member: %+v", graph.Root)
	}
	wsWrite(t, copyRoot, "relocated/"+ManifestName, strings.Replace(library, "example.org/lib", "example.org/other", 1))
	if _, err := Load(filepath.Join(copyRoot, "consumer"), tool, nil, false, true, true); err == nil || !strings.Contains(err.Error(), "dependency lib module changed") {
		t.Fatalf("copied lock accepted another module at inferred path: %v", err)
	}
}
