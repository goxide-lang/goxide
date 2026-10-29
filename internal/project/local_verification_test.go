package project

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDotlessLocalModulesKeepOfflineLockIdentity(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, module := range []string{"zlib", "fixture/facade"} {
		for _, format := range []string{"go", "goxide"} {
			t.Run(module+"/"+format, func(t *testing.T) {
				root := sourceTempDir(t)
				app, lib := filepath.Join(root, "app"), filepath.Join(root, "lib")
				wsWrite(t, app, ManifestName, sourceLibrary("fixture")+"[dependencies.dep]\npath='../lib'\n")
				if format == "go" {
					wsWrite(t, lib, "go.mod", "module "+module+"\ngo 1.27\n")
					wsWrite(t, lib, "lib.go", "package lib\nfunc Value()int{return 42}\n")
				} else {
					wsWrite(t, lib, ManifestName, sourceLibrary(module))
					wsWrite(t, lib, "lib.gox", "package lib\npub func Value()int{return 42}\n")
				}
				graph, err := Load(app, tool, nil, false, true, false)
				if err != nil {
					t.Fatal(err)
				}
				if graph.Root.Children["dep"].Module != module || effective(graph.Plan[module]).Dir != lib {
					t.Fatalf("local module became a cached remote source: %+v", graph.Plan)
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
				if err != nil || !bytes.Equal(before, after) || graph.PlanID != replay.PlanID {
					t.Fatalf("offline replay changed local dependency identity: %v", err)
				}
			})
		}
	}
}

func TestSelectedRemoteSourcesStillVerifyBothCacheForms(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, form := range []string{"directory", "zip"} {
		t.Run(form, func(t *testing.T) {
			root, proxy := sourceTempDir(t), roleProxyEnvironment(t)
			const module, version = "example.org/remote", "v1.0.0"
			roleProxyModule(t, proxy, module, version, "", "module "+module+"\ngo 1.27\n")
			wsWrite(t, root, ManifestName, sourceLibrary("fixture")+"[dependencies.remote]\ngo='"+module+"'\nversion='"+version+"'\n")
			graph, err := Load(root, tool, nil, false, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := graph.Save(); err != nil {
				t.Fatal(err)
			}
			cache := os.Getenv("GOMODCACHE")
			name := filepath.Join(cache, module+"@"+version, "lib.go")
			if form == "zip" {
				name = filepath.Join(cache, "cache", "download", module, "@v", version+".zip")
			}
			if err := os.Chmod(name, 0600); err != nil {
				t.Fatal(err)
			}
			if form == "directory" {
				if err := os.WriteFile(name, []byte("package library\nconst Changed=1\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				// A valid altered archive must fail its checksum, not just ZIP parsing.
				roleProxyModule(t, proxy, module, version, "", "module "+module+"\ngo 1.27\n// changed\n")
				altered, err := os.ReadFile(filepath.Join(proxy, module, "@v", version+".zip"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(name, altered, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load(root, tool, nil, false, true, true); err == nil || !strings.Contains(err.Error(), form+" has been modified") {
				t.Fatalf("modified remote %s was not rejected: %v", form, err)
			}
		})
	}
}
