package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/project"
)

func TestUnifiedLocalReplaceAndPureGit(t *testing.T) {
	for _, kind := range []string{"replace", "git", "path"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			lib := filepath.Join(base, "lib")
			root := filepath.Join(base, "app")
			projectWrite(t, lib, map[string]string{"goxide.toml": "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[features]\ndefault=['on']\non=[]\n[lib]\nname='lib'\npath='.'\n", "lib.gox": "package lib\nfunc Value()bool{return cfg!(feature(\"on\"))}\n"})
			dependency := "module='example.org/lib'\npath='../lib'\n"
			extra := ""
			if kind == "replace" {
				dependency = "go = 'example.org/lib'\nversion = 'v1.0.0'\n"
				extra = "[[go.replace]]\nmodule='example.org/lib'\npath='../lib'\n"
			}
			if kind == "git" {
				fixtureGit(t, lib, "init")
				fixtureGit(t, lib, "add", ".")
				fixtureGit(t, lib, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "pure")
				dependency = "module='example.org/lib'\ngit = '" + lib + "'\nrev = '" + fixtureGit(t, lib, "rev-parse", "HEAD") + "'\n"
			}
			projectWrite(t, root, map[string]string{"goxide.toml": "[go_mod]\nmodule='example.org/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.go'\n[dependencies.lib]\n" + dependency + extra, "main.go": "package main\nimport (\"fmt\";\"example.org/lib\")\nfunc main(){fmt.Println(lib.Value())}\n"})
			before := sourceHash(t, lib)
			flags := []string{"run"}
			if kind != "git" {
				flags = append(flags, "--offline")
			}
			if b, e := projectRun(t, root, flags...); e != nil || strings.TrimSpace(b) != "true" {
				t.Fatalf("%s %v", b, e)
			}
			if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "true" {
				t.Fatalf("%s %v", b, e)
			}
			if sourceHash(t, lib) != before {
				t.Fatal("dependency source modified")
			}
		})
	}
}

func TestUnifiedTargetModuleBoundary(t *testing.T) {
	for _, marker := range []string{"go.mod", "goxide.toml"} {
		t.Run(marker, func(t *testing.T) {
			root := t.TempDir()
			nested := filepath.Join(root, "nested")
			if err := os.Mkdir(nested, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(nested, marker), []byte("boundary"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(nested, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
				t.Fatal(err)
			}
			for _, entry := range []string{"nested", "nested/main.go"} {
				data, err := project.Marshal(project.Manifest{Module: "example.org/target", GoVersion: "1.27", Bins: []project.Target{{Name: "app", Path: entry}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, project.ManifestName), data, 0644); err != nil {
					t.Fatal(err)
				}
				_, err = compile.PrepareProject(root, compile.Options{GoTool: filepath.Join(runtime.GOROOT(), "bin", "go"), Offline: true})
				if err == nil || !strings.Contains(err.Error(), "nested module") {
					t.Fatalf("%s: %v", entry, err)
				}
			}
		})
	}
}
