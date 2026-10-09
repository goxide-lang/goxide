package rootconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitRootNamespaces(t *testing.T) {
	roots, err := Parse("roots.toml", []byte(`
[roots.wire]
module = "example.org/codec"
package = "example.org/codec/core"
[roots.wire.namespaces]
json = "example.org/codec/json"
"formats::binary" = "example.org/codec/binary"
[roots.standard]
module = "github.com/goxide-lang/std"
[roots.standard.namespaces]
sum = "github.com/goxide-lang/std/sum"
[roots.text]
source = "toolchain"
package = "fmt"
`))
	if err != nil {
		t.Fatal(err)
	}
	wire := roots["wire"]
	if wire.PackagePath != "example.org/codec/core" || wire.ModulePath != "example.org/codec" || wire.Source != "module" || wire.Children["json"].PackagePath != "example.org/codec/json" {
		t.Fatalf("exact mapping lost: %#v", wire)
	}
	formats := wire.Children["formats"]
	if formats.PackagePath != "" || formats.ModulePath != wire.ModulePath || formats.Children["binary"].PackagePath != "example.org/codec/binary" {
		t.Fatalf("namespace grouping lost: %#v", formats)
	}
	if roots["standard"].PackagePath != "" || roots["text"].ModulePath != "" || roots["text"].Source != "toolchain" {
		t.Fatalf("group root or toolchain owner inferred incorrectly: %#v", roots)
	}
}

func TestRootConfigRejectsImplicitSources(t *testing.T) {
	for name, input := range map[string]string{
		"missing-module":         "[roots.dep]\npackage='example.org/dep/pkg'",
		"toolchain-empty-module": "[roots.fmt]\nsource='toolchain'\nmodule=''\npackage='fmt'",
		"upper-table":            "[Roots.fmt]\nsource='toolchain'\npackage='fmt'",
		"upper-field":            "[roots.fmt]\nSource='toolchain'\npackage='fmt'",
		"toolchain-module":       "[roots.fmt]\nsource='toolchain'\nmodule='fmt'\npackage='fmt'",
		"root-not-identifier":    "[roots.bad-name]\nmodule='example.org/dep'",
		"empty-segment":          "[roots.dep]\nmodule='example.org/dep'\n[roots.dep.namespaces]\n'a::::b'='example.org/dep/a/b'",
		"go-selector-key":        "[roots.dep]\nmodule='example.org/dep'\n[roots.dep.namespaces]\n'a.b'='example.org/dep/a/b'",
		"empty-package":          "[roots.dep]\nmodule='example.org/dep'\n[roots.dep.namespaces]\na=''",
		"version-selection":      "[roots.dep]\nmodule='example.org/dep'\nversion='v1.2.3'",
		"unknown-source":         "[roots.dep]\nsource='directory'\nmodule='example.org/dep'",
		"relative-package":       "[roots.dep]\nmodule='example.org/dep'\npackage='./dep'",
		"reserved-root":          "[roots.use]\nmodule='example.org/dep'",
		"reserved-namespace":     "[roots.dep]\nmodule='example.org/dep'\n[roots.dep.namespaces]\nmatch='example.org/dep/match'",
		"invalid-package":        "[roots.dep]\nmodule='example.org/dep'\npackage='example.org/dep/☃'",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse("explicit.toml", []byte(input))
			if err == nil || !strings.Contains(err.Error(), "explicit.toml") {
				t.Fatalf("expected located mapping rejection, got %v", err)
			}
		})
	}
}

func TestRootDiscoveryStopsAtModuleBoundary(t *testing.T) {
	parent := t.TempDir()
	module := filepath.Join(parent, "module")
	nested := filepath.Join(module, "pkg")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, Filename), []byte("[roots.text]\nsource='toolchain'\npackage='fmt'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if roots, err := Discover(nested); err != nil || roots["text"].PackagePath != "fmt" {
		t.Fatalf("module-free discovery: %#v %v", roots, err)
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.org/app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if roots, err := Discover(nested); err != nil || roots != nil {
		t.Fatalf("inherited a mapping across module boundary: %#v %v", roots, err)
	}
	if err := os.WriteFile(filepath.Join(module, Filename), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if roots, err := Load(module); err != nil || roots == nil || len(roots) != 0 {
		t.Fatalf("explicit empty context lost: %#v %v", roots, err)
	}
}
