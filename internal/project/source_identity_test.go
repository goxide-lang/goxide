package project

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func sourceTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func sourceLibrary(module string) string {
	return fmt.Sprintf("[go_mod]\nmodule=%q\ngo='1.27'\n[lib]\nname='lib'\npath='.'\n[features]\nleft=[]\nright=[]\n", module)
}

func sourceRound(t *testing.T, root string, selected map[string]Selection) (*Graph, *planningRound) {
	t.Helper()
	work := sourceTempDir(t)
	g := &Graph{Tool: filepath.Join(runtime.GOROOT(), "bin", "go"), WorkRoot: work, Cache: filepath.Join(work, "cache"), Lock: Lock{Git: map[string]string{}}}
	r, err := g.round(root, nil, false, selected)
	if err != nil {
		t.Fatal(err)
	}
	return g, r
}

func TestSourcePathAliasFeatureUnion(t *testing.T) {
	for _, replacement := range []string{"path", "wildcard", "version-specific"} {
		t.Run(replacement, func(t *testing.T) {
			root := sourceTempDir(t)
			second := "path='lib/.'"
			policy := ""
			if replacement != "path" {
				second = "go='example.org/lib', version='v1.2.0'"
				policy = "[[go.replace]]\nmodule='example.org/lib'\npath='./lib'\n"
				if replacement == "version-specific" {
					policy += "version='v1.2.0'\n"
				}
			}
			manifest := mappingHeader + "[dependencies]\na={path='lib',features=['left']}\nb={" + second + ",features=['right']}\n" + policy
			wsWrite(t, root, ManifestName, manifest)
			wsWrite(t, root, "lib/"+ManifestName, sourceLibrary("example.org/lib"))
			tool := filepath.Join(runtime.GOROOT(), "bin", "go")
			for _, locked := range []bool{false, true} {
				g, err := Load(root, tool, nil, false, true, locked)
				if err != nil {
					t.Fatal(err)
				}
				a, b := g.Root.Children["a"], g.Root.Children["b"]
				if a != b || len(g.Nodes) != 2 || !a.Features["left"] || !a.Features["right"] {
					t.Fatalf("same source did not merge feature requests: %+v %+v", a, b)
				}
				if err := g.Save(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestSourceGitCommitAliasFeatureUnion(t *testing.T) {
	root := sourceTempDir(t)
	repo, app := filepath.Join(root, "repo"), filepath.Join(root, "app")
	wsWrite(t, repo, "a/"+ManifestName, sourceLibrary("example.org/a")+"[dependencies.b]\npath='../b'\nfeatures=['left']\n")
	wsWrite(t, repo, "b/"+ManifestName, sourceLibrary("example.org/b"))
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}, {"tag", "v1"}} {
		if _, err := git(repo, args...); err != nil {
			t.Fatal(err)
		}
	}
	manifest := mappingHeader + fmt.Sprintf("[dependencies]\na={git=%q,branch='main',subdir='a',features=['left']}\nb={git=%q,tag='v1',subdir='a',features=['right']}\nc={git=%q,tag='v1',subdir='b',features=['right']}\n", repo, repo, repo)
	wsWrite(t, app, ManifestName, manifest)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	var before []byte
	var sourceIDs string
	for _, target := range []string{"", filepath.Join(root, "another-target")} {
		g, err := LoadWithTarget(app, tool, nil, false, false, before != nil, target)
		if err != nil {
			t.Fatal(err)
		}
		a, b, c := g.Root.Children["a"], g.Root.Children["b"], g.Root.Children["c"]
		if a != b || a.Children["b"] != c || len(g.Nodes) != 3 || !a.Features["left"] || !a.Features["right"] || !c.Features["left"] || !c.Features["right"] {
			t.Fatalf("Git aliases or internal path retained distinct sources: %+v", g.Nodes)
		}
		ids := a.SourceID + c.SourceID
		if sourceIDs != "" && sourceIDs != ids {
			t.Fatal("source identity depends on acquisition directory")
		}
		sourceIDs = ids
		if err := g.Save(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(app, "goxide.lock"))
		if err != nil {
			t.Fatal(err)
		}
		if before != nil && string(data) != string(before) {
			t.Fatal("changing Git acquisition directory changed lock")
		}
		before = data
	}
}

func TestSourceModuleConflictIsProjectionOnly(t *testing.T) {
	for _, kind := range []string{"path", "git"} {
		t.Run(kind, func(t *testing.T) {
			root := sourceTempDir(t)
			declarations := "a={path='a',features=['left']}\nb={path='b',features=['right']}\n"
			var repository, first, second string
			if kind == "path" {
				wsWrite(t, root, "a/"+ManifestName, sourceLibrary("example.org/shared"))
				wsWrite(t, root, "b/"+ManifestName, sourceLibrary("example.org/shared"))
			} else {
				repository = sourceTempDir(t)
				wsWrite(t, repository, "lib/"+ManifestName, sourceLibrary("example.org/shared"))
				for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "first"}, {"tag", "first"}} {
					if _, err := git(repository, args...); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				first, err = git(repository, "rev-parse", "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				wsWrite(t, repository, "change.txt", "second commit\n")
				for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "second"}} {
					if _, err := git(repository, args...); err != nil {
						t.Fatal(err)
					}
				}
				second, err = git(repository, "rev-parse", "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				declarations = fmt.Sprintf("a={git=%q,tag='first',subdir='lib',features=['left']}\nb={git=%q,branch='main',subdir='lib',features=['right']}\n", repository, repository)
			}
			wsWrite(t, root, ManifestName, mappingHeader+"[dependencies]\n"+declarations)
			g, r := sourceRound(t, root, nil)
			a, b := r.root.Children["a"], r.root.Children["b"]
			if a == b || a.SourceKey == b.SourceKey || len(r.nodes) != 3 || !a.Features["left"] || a.Features["right"] || !b.Features["right"] || b.Features["left"] {
				t.Fatalf("source graph merged distinct roots: %+v", r.nodes)
			}
			if len(r.requirements) != 0 || len(r.local) != 0 {
				t.Fatal("source collection constructed a Go projection")
			}
			g.Tool = filepath.Join(root, "missing-go")
			_, err := g.solve(root, r)
			if err == nil || !strings.Contains(err.Error(), "Go module projection has conflicting sources for module example.org/shared") {
				t.Fatalf("Go projection did not diagnose distinct sources before invoking Go: %v", err)
			}
			if kind == "git" {
				for _, want := range []string{repository, first, second, "subdir lib"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("Git source conflict omitted %s: %v", want, err)
					}
				}
			}
		})
	}
}

func TestSourceGoVersionIdentity(t *testing.T) {
	root := sourceTempDir(t)
	app := filepath.Join(root, "app")
	wsWrite(t, app, ManifestName, mappingHeader+"[dependencies.lib]\ngo='example.org/lib'\nversion='v1.0.0'\nfeatures=['left']\n")
	var before *Node
	for i, version := range []string{"v1.0.0", "v1.1.0"} {
		dir := filepath.Join(root, version)
		wsWrite(t, dir, ManifestName, sourceLibrary("example.org/lib"))
		wsWrite(t, dir, "go.mod", "module example.org/lib\ngo 1.27\n")
		_, r := sourceRound(t, app, map[string]Selection{"example.org/lib": {Path: "example.org/lib", Version: version, Dir: dir}})
		n := r.root.Children["lib"]
		if !n.Features["left"] {
			t.Fatal("Go-selected source lost requested feature")
		}
		if i != 0 && (before.SourceKey != n.SourceKey || before.SourceID == n.SourceID) {
			t.Fatal("Go selection did not retain logical source while changing content identity")
		}
		before = n
	}
}

func TestSourceVersionArtifactConflict(t *testing.T) {
	proxy := sourceTempDir(t)
	module, version := "example.org/native", "v1.2.0"
	metadata := "module " + module + "\ngo 1.27\n"
	base := filepath.Join(proxy, module, "@v")
	wsWrite(t, base, version+".mod", metadata)
	wsWrite(t, base, version+".info", fmt.Sprintf("{\"Version\":%q,\"Time\":\"2026-01-01T00:00:00Z\"}", version))
	file, err := os.Create(filepath.Join(base, version+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, body := range map[string]string{"go.mod": metadata, "lib.go": "package lib\n"} {
		entry, err := archive.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOPROXY", "file://"+proxy)
	t.Setenv("GOSUMDB", "off")
	cache := sourceTempDir(t)
	t.Setenv("GOMODCACHE", cache)
	t.Cleanup(func() {
		if err := filepath.WalkDir(cache, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0755)
			}
			return err
		}); err != nil {
			t.Error(err)
		}
	})
	for _, request := range []string{"dependency", "native-require"} {
		for _, replacement := range []string{"none", "unmatched", "effective"} {
			t.Run(request+"/"+replacement, func(t *testing.T) {
				root := sourceTempDir(t)
				manifest := mappingHeader + "[dependencies.local]\npath='lib'\n"
				if request == "dependency" {
					manifest += fmt.Sprintf("[dependencies.versioned]\ngo=%q\nversion=%q\n", module, version)
				} else {
					manifest += fmt.Sprintf("[[go.require]]\nmodule=%q\nversion=%q\n", module, version)
				}
				if replacement != "none" {
					replacedVersion := version
					if replacement == "unmatched" {
						replacedVersion = "v1.0.0"
					}
					manifest += fmt.Sprintf("[[go.replace]]\nmodule=%q\nversion=%q\npath='./lib'\n", module, replacedVersion)
				}
				wsWrite(t, root, ManifestName, manifest)
				wsWrite(t, root, "lib/go.mod", metadata)
				wsWrite(t, root, "lib/lib.go", "package lib\n")
				g, err := Load(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false)
				if replacement == "effective" {
					if err != nil {
						t.Fatal(err)
					}
					if effective(g.Plan[module]).Dir != g.Root.Children["local"].Root {
						t.Fatal("effective native replacement did not select the path source")
					}
				} else if err == nil || !strings.Contains(err.Error(), "Go module projection has conflicting sources") || !strings.Contains(err.Error(), module+"@"+version) {
					t.Fatalf("fixed Go artifact and path source were not distinguished: %v", err)
				}
			})
		}
	}
}

func TestSourceCopiedUnifiedLockReachableOnly(t *testing.T) {
	root := sourceTempDir(t)
	repos := sourceTempDir(t)
	for _, name := range []string{"used", "unused"} {
		repo := filepath.Join(repos, name)
		wsWrite(t, repo, "go.mod", "module example.org/"+name+"\ngo 1.27\n")
		for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}} {
			if _, err := git(repo, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	wsWrite(t, root, ManifestName, "[workspace]\nmembers=['app','other']\n")
	appManifest := mappingHeader + fmt.Sprintf("[dependencies.used]\ngit=%q\nbranch='main'\n", filepath.Join(repos, "used"))
	wsWrite(t, root, "app/"+ManifestName, appManifest)
	wsWrite(t, root, "other/"+ManifestName, strings.Replace(mappingHeader, "example.org/app", "example.org/other", 1)+fmt.Sprintf("[dependencies.unused]\ngit=%q\nbranch='main'\n", filepath.Join(repos, "unused")))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	g, err := Load(filepath.Join(root, "app"), tool, nil, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	commit := g.Root.Children["used"].source.Commit
	wsWrite(t, repos, "used/new.txt", "another commit\n")
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "new"}} {
		if _, err := git(filepath.Join(repos, "used"), args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(filepath.Join(repos, "unused"), filepath.Join(repos, "unavailable")); err != nil {
		t.Fatal(err)
	}
	split := sourceTempDir(t)
	wsWrite(t, split, ManifestName, appManifest)
	wsWrite(t, split, "goxide.lock", string(before))
	for _, offline := range []bool{false, true} {
		replay, err := Load(split, tool, nil, false, offline, true)
		if err != nil {
			t.Fatal(err)
		}
		if child := replay.Root.Children["used"]; child.source.Commit != commit || len(replay.Nodes) != 2 {
			t.Fatal("copied lock changed its pinned source or loaded an unreachable member")
		}
		if err := replay.Save(); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(split, "goxide.lock"))
		if err != nil || string(before) != string(after) {
			t.Fatalf("copied unified lock changed: %v", err)
		}
		g = replay
	}
	delete(g.Lock.Unified.Sources, g.Root.Children["used"].lockSourceKey(split))
	tampered, err := json.Marshal(g.Lock)
	if err != nil {
		t.Fatal(err)
	}
	wsWrite(t, split, "goxide.lock", string(tampered))
	if _, err := Load(split, tool, nil, false, true, true); err == nil || !strings.Contains(err.Error(), "source identity changed") {
		t.Fatalf("root input rebinding bypassed child source verification: %v", err)
	}
}

func TestSourceCycle(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, mappingHeader+"[dependencies.a]\npath='a'\n")
	wsWrite(t, root, "a/"+ManifestName, sourceLibrary("example.org/a")+"[dependencies.root]\npath='..'\n")
	_, err := Load(root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, true, false)
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("source cycle was not diagnosed: %v", err)
	}
}

func TestSourceLockRelocation(t *testing.T) {
	root := sourceTempDir(t)
	wsWrite(t, root, ManifestName, mappingHeader+"[dependencies.a]\npath='a'\n")
	wsWrite(t, root, "a/"+ManifestName, sourceLibrary("example.org/a"))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	g, err := Load(root, tool, nil, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if err != nil {
		t.Fatal(err)
	}
	moved := sourceTempDir(t)
	for _, name := range []string{ManifestName, "a/" + ManifestName, "goxide.lock"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		wsWrite(t, moved, name, string(data))
	}
	replay, err := Load(moved, tool, nil, false, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Save(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(moved, "goxide.lock"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("relocating author root changed source lock: %v", err)
	}
}
