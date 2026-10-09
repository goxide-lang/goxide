package frontend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModuleReexportNamespaceSelectedDependency(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		name := "selected child"
		if cycle {
			name = "cycle"
		}
		t.Run(name, func(t *testing.T) {
			src := t.TempDir()
			dst := filepath.Join(t.TempDir(), "out")
			child := "package child\nfunc Value()int{return 42}\n"
			if cycle {
				child = "package child\nuse application;func Value()int{return application::Value()}\n"
			}
			useWrite(t, src, map[string]string{
				"go.mod": "module example.org/staticgraph\ngo 1.27\n",
				"goxide.roots.toml": `[roots.application]
module="example.org/staticgraph"
package="example.org/staticgraph"
[roots.api]
module="example.org/staticgraph"
package="example.org/staticgraph/api"
[roots.tree]
module="example.org/staticgraph"
[roots.tree.namespaces]
child="example.org/staticgraph/child"
unused="example.org/staticgraph/unused"
`,
				"main.gox":          "package root\nuse api;func Value()int{return api::group::child::Value()}\n",
				"api/api.gox":       "package api\npub use tree as group;\n",
				"child/child.gox":   child,
				"unused/unused.gox": "package unused\nfunc Broken(){this is not valid}\n",
			})
			_, err := CompileModuleWithOptions(src, dst, ModuleOptions{OnlyTarget: true})
			if cycle {
				if err == nil || !strings.Contains(err.Error(), "module import cycle") {
					t.Fatalf("cycle: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dst, "child", "child.go")); err != nil {
				t.Fatal("selected namespace leaf not compiled:", err)
			}
			if _, err := os.Stat(filepath.Join(dst, "unused", "unused.go")); !os.IsNotExist(err) {
				t.Fatal("unused namespace leaf compiled:", err)
			}
			if err := os.WriteFile(filepath.Join(dst, "main_test.go"), []byte("package root\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=42{t.Fatal(Value())}}\n"), 0644); err != nil {
				t.Fatal(err)
			}
			useGo(t, dst, "test", "-race", ".")
		})
	}
}
