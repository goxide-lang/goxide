package project

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
