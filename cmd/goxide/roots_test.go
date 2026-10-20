package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/rootconfig"
)

func writeRootFixture(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

const rootContextProgram = `package main
use output;
use dependency::implementation as api;
func main(){output::Println(api::Value())}
`

const rootContextMapping = `[roots.output]
source = "toolchain"
package = "fmt"
[roots.dependency]
module = "example.org/dependency"
package = "example.org/dependency"
[roots.unused]
module = "example.org/not-selected"
package = "example.org/not-selected/missing"
`

func TestCLIRejectsReservedCrateRoot(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		name := "discovered"
		if explicit {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mapping := rootconfig.Filename
			if explicit {
				mapping = "selected.toml"
			}
			writeRootFixture(t, root, map[string]string{
				"go.mod":   "module example.org/app\ngo 1.27\n",
				"main.gox": "package main\nuse crate as output;\nfunc main(){output::Println(42)}\n",
				mapping:    "[roots.crate]\nsource='toolchain'\npackage='fmt'\n",
			})
			args := []string{"transpile"}
			if explicit {
				args = append(args, "--roots", filepath.Join(root, mapping))
			}
			args = append(args, filepath.Join(root, "main.gox"))
			var output bytes.Buffer
			if err := run(args, &output, &output); err == nil || !strings.Contains(err.Error(), `invalid dependency root "crate"`) {
				t.Fatalf("reserved root: %v: %s", err, &output)
			}
			if _, err := os.Stat(filepath.Join(root, "main_gox.go")); !os.IsNotExist(err) {
				t.Fatalf("invalid roots produced output: %v", err)
			}
		})
	}
}

func TestCLIExplicitRootContexts(t *testing.T) {
	root := t.TempDir()
	app, dependency := filepath.Join(root, "app"), filepath.Join(root, "dep")
	writeRootFixture(t, dependency, map[string]string{
		"go.mod":                  "module example.org/dependency\ngo 1.27\n",
		"root.go":                 "package dependency\n",
		"implementation/value.go": "package declaredname\nfunc Value()int{return 42}\n",
	})
	writeRootFixture(t, app, map[string]string{
		"go.mod":            "module example.org/app\ngo 1.27\nrequire example.org/dependency v0.0.0\nreplace example.org/dependency => ../dep\n",
		"main.gox":          rootContextProgram,
		rootconfig.Filename: rootContextMapping,
	})
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	link := filepath.Join(root, "app-link")
	if err := os.Symlink(app, link); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{app, filepath.Join(app, "main.gox"), link} {
		var output bytes.Buffer
		if err := run([]string{"run", "--go", tool, input}, &output, &output); err != nil || output.String() != "42\n" {
			t.Fatalf("root context for %s: %v: %s", input, err, &output)
		}
	}
	for name, mapping := range map[string]string{
		"wrong-module-owner": strings.ReplaceAll(rootContextMapping, `module = "example.org/dependency"`, `module = "example.org/imposter"`),
		"toolchain-spoof":    strings.ReplaceAll(rootContextMapping, `module = "example.org/dependency"`, `source = "toolchain"`),
	} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(root, name+".toml")
			if err := os.WriteFile(file, []byte(mapping), 0600); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := run([]string{"transpile", "-o", "-", "--roots", file, filepath.Join(app, "main.gox")}, &output, &output); err == nil {
				t.Fatal("accepted a false package owner")
			}
		})
	}
	// An explicit file is the only mapping source when selected, even if the
	// source module has its own mapping with the same aliases.
	override := filepath.Join(root, "override.toml")
	if err := os.WriteFile(override, []byte(strings.ReplaceAll(rootContextMapping, `package = "example.org/dependency"`, `package = "example.org/dependency/missing"`)), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"run", "--roots", override, "--go", tool, filepath.Join(app, "main.gox")}, &output, &output); err == nil {
		t.Fatal("explicit missing package mapping fell back to module-local context")
	}
	if err := os.Remove(filepath.Join(app, rootconfig.Filename)); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"transpile", "-o", "-", filepath.Join(app, "main.gox")}, &output, &output); err == nil {
		t.Fatal("unknown roots were inferred from module requirements")
	}
}

func TestCLIProjectRootContexts(t *testing.T) {
	root := t.TempDir()
	app, dependency := filepath.Join(root, "app"), filepath.Join(root, "dep")
	writeRootFixture(t, dependency, map[string]string{
		"go.mod":                  "module example.org/dependency\ngo 1.27\n",
		"root.go":                 "package dependency\n",
		"implementation/value.go": "package declaredname\nfunc Value()int{return 42}\n",
	})
	writeRootFixture(t, app, map[string]string{
		"main.gox": rootContextProgram,
		"goxide.toml": `[go_mod]
module = "example.org/app"
go = "1.27"
[dependencies.output]
go = "fmt"
[dependencies.dependency]
module = "example.org/dependency"
path = "../dep"
[[bin]]
name = "app"
path = "."
`,
	})
	var output bytes.Buffer
	if err := run([]string{"run", "--offline", "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), app}, &output, &output); err != nil || output.String() != "42\n" {
		t.Fatalf("project root context: %v: %s", err, &output)
	}
}

func TestCLIStandaloneRootContext(t *testing.T) {
	root := t.TempDir()
	writeRootFixture(t, root, map[string]string{
		"main.gox":          "package main\nuse output;\nfunc main(){output::Println(42)}\n",
		rootconfig.Filename: "[roots.output]\nsource='toolchain'\npackage='fmt'\n",
	})
	var output bytes.Buffer
	if err := run([]string{"transpile", "-o", "-", filepath.Join(root, "main.gox")}, &output, &output); err != nil || !strings.Contains(output.String(), `"fmt"`) {
		t.Fatalf("standalone discovery: %v: %s", err, &output)
	}
	link := filepath.Join(t.TempDir(), "standalone-link.gox")
	if err := os.Symlink(filepath.Join(root, "main.gox"), link); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"transpile", "-o", "-", link}, &output, &output); err != nil {
		t.Fatalf("standalone context followed link directory instead of source: %v", err)
	}
	mapping := filepath.Join(root, "selected.toml")
	if err := os.Rename(filepath.Join(root, rootconfig.Filename), mapping); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"transpile", "-o", "-", "--roots", mapping, filepath.Join(root, "main.gox")}, &output, &output); err != nil {
		t.Fatalf("standalone explicit context: %v: %s", err, &output)
	}
	output.Reset()
	if err := run([]string{"transpile", "-o", "-", filepath.Join(root, "main.gox")}, &output, &output); err == nil {
		t.Fatal("standalone accepted an undeclared root")
	}
	if err := os.WriteFile(filepath.Join(root, "main.gox"), []byte("package main\nfunc main(){output::Println(42)}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{"transpile", "-o", "-", "--roots", mapping, filepath.Join(root, "main.gox")}, &output, &output); err == nil {
		t.Fatal("declaring a root implicitly bound it in source")
	}
}

func TestCLICurrentModulePackageContext(t *testing.T) {
	root := t.TempDir()
	writeRootFixture(t, root, map[string]string{
		"src/lib.gox":  "package library\nfunc Value()int{return 43}\n",
		"src/main.gox": "package main\nuse output;\nuse library;\nfunc main(){output::Println(library::Value())}\n",
		"goxide.toml": `[go_mod]
module="example.org/app"
go="1.27"
[dependencies.output]
go = "fmt"
[lib]
name="library"
path="src/lib.gox"
[[bin]]
name="app"
path="src/main.gox"
`,
	})
	var output bytes.Buffer
	if err := run([]string{"run", "--offline", "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), root}, &output, &output); err != nil || output.String() != "43\n" {
		t.Fatalf("same-module package mapping: %v: %s", err, &output)
	}
	m, err := project.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	node := &project.Node{Root: root, Module: m.Module, HasManifest: true, Manifest: m, Requests: map[string]project.Request{"output": {}}}
	host, err := projectHostRoots(node)
	if err != nil || len(host) != 1 || host["output"].Source != "toolchain" {
		t.Fatalf("host inherited target roots: %v %v", host, err)
	}
}
