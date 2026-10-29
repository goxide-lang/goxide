package project

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLocalPublishedMacroDependency(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, kind := range []string{"path", "git"} {
		for _, conflict := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/conflict=%t", kind, conflict), func(t *testing.T) {
				base := sourceTempDir(t)
				library, app := filepath.Join(base, "library"), filepath.Join(base, "app")
				wsWrite(t, library, ManifestName, roleManifest("example.org/macros", true))
				mod := "module example.org/macros\ngo 1.27\n"
				if conflict {
					mod = "module example.org/other\ngo 1.27\n"
				}
				wsWrite(t, library, "go.mod", mod)
				dependency := fmt.Sprintf("path=%q", library)
				if kind == "git" {
					for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}} {
						if _, err := git(library, args...); err != nil {
							t.Fatal(err)
						}
					}
					dependency = fmt.Sprintf("git=%q", library)
				}
				wsWrite(t, app, ManifestName, roleManifest("example.org/app", false)+"[dependencies.macros]\n"+dependency+"\n")
				plans, err := LoadBuildPlans(app, tool, nil, false, false, false, "", "linux/amd64", false, false)
				if conflict {
					if err == nil || !strings.Contains(err.Error(), "transport metadata conflicts with goxide.toml") {
						t.Fatalf("accepted conflicting dependency metadata: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(plans.Macros) != 1 || plans.Macros[0].Root.Module != "example.org/macros" {
					t.Fatalf("macro role lost the published dependency: %#v", plans.Macros)
				}
				if _, err := LoadBuildPlans(library, tool, nil, false, false, false, "", "linux/amd64", false, false); err == nil || !strings.Contains(err.Error(), "goxide project cannot contain go.mod") {
					t.Fatalf("author root accepted transport metadata: %v", err)
				}
			})
		}
	}
}

func TestPublishedTransportPolicy(t *testing.T) {
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, kind := range []string{"permuted", "conflict", "unsupported"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			manifest := "[go_mod]\nmodule='example.org/lib'\ngo='1.27'\n[[go.require]]\nmodule='example.org/a'\nversion='v1.0.0'\n[[go.require]]\nmodule='example.org/b'\nversion='v1.0.0'\nindirect=true\n"
			mod := "module example.org/lib\ngo 1.27\nrequire example.org/b v1.0.0 // indirect\nrequire example.org/a v1.0.0\n"
			if kind == "conflict" {
				mod = strings.Replace(mod, "example.org/a v1.0.0", "example.org/a v1.1.0", 1)
			}
			if kind == "unsupported" {
				mod += "godebug default=go1.26\n"
			}
			for name, data := range map[string]string{ManifestName: manifest, "go.mod": mod} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0644); err != nil {
					t.Fatal(err)
				}
			}
			g := Graph{Tool: tool, WorkRoot: t.TempDir()}
			_, err := g.readNode(root, true, "")
			if kind == "permuted" && err != nil {
				t.Fatal(err)
			}
			if kind != "permuted" && err == nil {
				t.Fatal("accepted incompatible transport", kind)
			}
		})
	}
}

func TestPublishedEmbeddedRoleRoot(t *testing.T) {
	checkout := t.TempDir()
	root := filepath.Join(checkout, "macros")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := "[go_mod]\nmodule='example.org/lib/macros'\ngo='1.27'\n[lib]\nname='macros'\npath='.'\nproc_macro=true\n"
	if err := os.WriteFile(filepath.Join(root, ManifestName), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	source := &Node{Root: root, CheckoutRoot: checkout}
	source.setSource(sourceIdentity{Kind: "go", Module: "example.org/lib", Version: "v1.0.0", Subdir: "macros"})
	g := Graph{Tool: filepath.Join(runtime.GOROOT(), "bin", "go"), WorkRoot: t.TempDir(), roleRoot: source}
	node, err := g.readRoot(root)
	if err != nil {
		t.Fatalf("embedded macro source requires no nested Go module: %v", err)
	}
	if node.Module != "example.org/lib/macros" || node.CheckoutRoot != checkout || node.source != source.source {
		t.Fatalf("role changed acquired source identity: %#v", node)
	}
	// The acquired module itself still requires its transport metadata.
	g.roleRoot.CheckoutRoot = root
	if _, err := g.readRoot(root); err == nil || !strings.Contains(err.Error(), "published Go transport metadata") {
		t.Fatalf("accepted a transport root without go.mod: %v", err)
	}
}
