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
			manifest := "[package]\nformat=2\nmodule='example.org/lib'\ngo='1.27'\n[[go.require]]\nmodule='example.org/a'\nversion='v1.0.0'\n[[go.require]]\nmodule='example.org/b'\nversion='v1.0.0'\nindirect=true\n"
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
			_, err := g.readNode(root, true)
			if kind == "permuted" && err != nil {
				t.Fatal(err)
			}
			if kind != "permuted" && err == nil {
				t.Fatal("accepted incompatible transport", kind)
			}
		})
	}
}
