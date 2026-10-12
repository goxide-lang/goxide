package project

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const mappingHeader = "[package]\nformat=2\nmodule='example.org/app'\ngo='1.27'\n"

func TestDependencyMappingSchema(t *testing.T) {
	text := mappingHeader + `
[features]
fast-mode = []
[dependencies.serde]
go = "github.com/goxide-lang/serde"
version = "v1.0.0"
package = "github.com/goxide-lang/serde"
[dependencies.serde.namespaces]
json = "github.com/goxide-lang/serde/json"
"formats::custom" = "example.org/explicit/format"
[dependencies.std]
module = "github.com/goxide-lang/std"
path = "../std"
[dependencies.std.namespaces]
sum = "github.com/goxide-lang/std/sum"
[dependencies._json]
go = "encoding/json"
`
	m, err := Parse("roots.toml", []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies["std"].Package != "" {
		t.Fatal("module identity became an implicit package mapping")
	}
	if d := m.Dependencies["serde"]; d.Package != "github.com/goxide-lang/serde" || d.Namespaces["formats::custom"] != "example.org/explicit/format" {
		t.Fatalf("lost exact mappings: %+v", d)
	}
	if d := m.Dependencies["_json"]; !d.IsToolchain() || d.Module != "" || d.Package != "encoding/json" {
		t.Fatalf("toolchain became a module: %+v", d)
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse("roundtrip.toml", data)
	if err != nil || !reflect.DeepEqual(m, again) || Digest(m) != Digest(again) {
		t.Fatalf("mapping roundtrip: %s\n%+v, %v", data, again, err)
	}
	before := Digest(m)
	d := again.Dependencies["serde"]
	d.Namespaces["json"] = "example.org/changed"
	if Digest(again) == before {
		t.Fatal("namespace mapping missing from manifest identity")
	}
	d = m.Dependencies["std"]
	d.Path = "../different-std"
	m.Dependencies["std"] = d
	if Digest(m) == before {
		t.Fatal("source declaration missing from manifest identity")
	}
}

func TestDependencyMappingDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"root hyphen", "[dependencies.bad-name]\nmodule='example.org/a'\npath='a'", "invalid dependencies name"},
		{"root digit", "[dependencies.1name]\nmodule='example.org/a'\npath='a'", "invalid dependencies name"},
		{"root blank", "[dependencies._]\nmodule='example.org/a'\npath='a'", "invalid dependencies name"},
		{"root keyword", "[dependencies.type]\nmodule='example.org/a'\npath='a'", "invalid dependencies name"},
		{"root source keyword", "[dependencies.use]\nmodule='example.org/a'\npath='a'", "invalid dependencies name"},
		{"namespace hyphen", "[dependencies.a]\nmodule='example.org/a'\npath='a'\nnamespaces={'bad-name'='example.org/a/b'}", "invalid namespace path"},
		{"namespace dot", "[dependencies.a]\nmodule='example.org/a'\npath='a'\nnamespaces={'a.b'='example.org/a/b'}", "invalid namespace path"},
		{"namespace empty segment", "[dependencies.a]\nmodule='example.org/a'\npath='a'\nnamespaces={'a::::b'='example.org/a/b'}", "invalid namespace path"},
		{"namespace blank", "[dependencies.a]\nmodule='example.org/a'\npath='a'\nnamespaces={'a::_'='example.org/a/b'}", "invalid namespace path"},
		{"package url", "[dependencies.a]\ngo = 'https://example.org/a' ", "path without protocol"},
		{"package relative", "[dependencies.a]\ngo = '../fmt' ", "path without protocol"},
		{"package normalized", "[dependencies.a]\ngo = 'encoding/../fmt' ", "path without protocol"},
		{"package trailing slash", "[dependencies.a]\ngo = 'fmt/' ", "path without protocol"},
		{"package empty segment", "[dependencies.a]\ngo = 'encoding//json' ", "path without protocol"},
		{"namespace package empty", "[dependencies.a]\nnamespaces={b=''}\ngo = 'encoding/json'\n", "invalid exact package path"},
		{"old source selector", "[dependencies.a]\nsource='toolchain'\npackage='fmt'", "unknown field"},
		{"conflicting sources", "[dependencies.a]\nmodule='example.org/a'\ngit='https://example.org/a'\npath='../a'", "exactly one"},
		{"conflicting refs", "[dependencies.a]\nmodule='example.org/a'\ngit='https://example.org/a'\nrev='deadbeef'\nbranch='main'", "at most one"},
		{"registry unavailable", "[dependencies.a]\nversion='1.0'", "registry source is not implemented"},
		{"missing module", "[dependencies.a]\ngo='example.org/a'", "requires version"},
		{"feature forward", "[dependencies.a]\ngo = 'fmt' \n[features]\nunused=['a/format']", "cannot receive feature requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("roots.toml", []byte(mappingHeader+tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
	for _, field := range []string{"module=''", "version=''", "path=''", "git=''", "rev=''", "subdir=''", "features=[]", "default_features=false", "workspace=false"} {
		t.Run("toolchain "+field, func(t *testing.T) {
			_, err := Parse("roots.toml", []byte(mappingHeader+"[dependencies.a]\ngo = 'fmt' \n"+field))
			if err == nil {
				t.Fatalf("forbidden toolchain field %s: %v", field, err)
			}
		})
	}
}

func TestDependencyMappingOptionalActivation(t *testing.T) {
	m, err := Parse("roots.toml", []byte(mappingHeader+`
[dependencies.fmt]
go = "fmt"
[dependencies.json]
go = "encoding/json"
optional = true
[dependencies.missing]
module = "example.org/missing"
path = "does-not-exist"
package = "example.org/missing"
namespaces = { nested = "example.org/missing/nested" }
optional = true
[features]
json = ["dep:json"]
missing = ["dep:missing"]
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, requested := range [][]string{nil, {"json"}} {
		_, deps, err := Resolve(m, requested, false)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]Request{"fmt": {}}
		if len(requested) != 0 {
			want["json"] = Request{}
		}
		if !reflect.DeepEqual(deps, want) {
			t.Fatalf("mapping implicitly activated dependencies or toolchain defaults: %+v, want %+v", deps, want)
		}
	}
	m.Features["bad"] = []string{"json/feature"}
	if _, _, err := Resolve(m, []string{"bad"}, false); err == nil || !strings.Contains(err.Error(), "cannot receive feature requests") {
		t.Fatalf("toolchain accepted forwarded feature: %v", err)
	}
}

func TestDependencyMappingWorkspaceInheritance(t *testing.T) {
	for _, override := range []string{"", "package='other'", "namespaces={other='other'}", "go = 'fmt'"} {
		t.Run(override, func(t *testing.T) {
			root := t.TempDir()
			wsWrite(t, root, ManifestName, `[workspace]
format=1
members=["app"]
[workspace.dependencies.serde]
go = "example.org/serde"
version = "v1.0.0"
package="example.org/serde"
namespaces={json="example.org/serde/json", "formats::custom"="example.org/serde/custom"}
[workspace.dependencies.json]
go = "encoding/json"
`)
			wsWrite(t, root, "app/"+ManifestName, mappingHeader+"[dependencies.serde]\nworkspace=true\n"+override+"\n[dependencies.json]\nworkspace=true\noptional=true\n[features]\njson=['dep:json']\n")
			m, err := Read(filepath.Join(root, "app"))
			if override != "" {
				if err == nil || !strings.Contains(err.Error(), "workspace inheritance cannot override") {
					t.Fatalf("accepted mapping override %q: %v", override, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d := m.Dependencies["serde"]; d.Package != "example.org/serde" || d.Namespaces["formats::custom"] != "example.org/serde/custom" {
				t.Fatalf("workspace lost mappings: %+v", d)
			}
			if d := m.Dependencies["json"]; !d.IsToolchain() || !d.Optional || d.Package != "encoding/json" {
				t.Fatalf("workspace lost toolchain/optional: %+v", d)
			}
			_, deps, err := Resolve(m, nil, false)
			if _, found := deps["json"]; err != nil || found {
				t.Fatalf("inherited optional mapping activated: %+v, %v", deps, err)
			}
		})
	}
}

func TestToolchainModulePlan(t *testing.T) {
	root := t.TempDir()
	manifest := mappingHeader + `
[dependencies.fmt]
go = "fmt"
[dependencies.encoding]
namespaces={json="encoding/json"}
optional=true
go = 'encoding/json'
[dependencies.missing]
module="example.org/missing"
path="does-not-exist"
package="example.org/missing"
optional=true
[features]
json=["dep:encoding"]
`
	wsWrite(t, root, ManifestName, manifest)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	g, err := Load(root, tool, []string{"json"}, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 1 || len(g.Plan) != 0 || len(g.Root.Children) != 0 || len(g.Root.Sources) != 0 || len(g.Root.Requests) != 2 {
		t.Fatalf("toolchain became a module edge: nodes=%d, plan=%v, children=%v, sources=%v, requests=%v", len(g.Nodes), g.Plan, g.Root.Children, g.Root.Sources, g.Root.Requests)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, tool, []string{"json"}, false, true, true); err != nil {
		t.Fatalf("identical locked mappings: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(manifest, "package=\"fmt\"", "package=\"bytes\"", 1),
		strings.Replace(manifest, "json=\"encoding/json\"", "json=\"encoding/xml\"", 1),
	} {
		wsWrite(t, root, ManifestName, changed)
		if _, err := Load(root, tool, []string{"json"}, false, true, true); err == nil || !strings.Contains(err.Error(), "--locked") {
			t.Fatalf("changed mapping reused lock: %v", err)
		}
	}
}

func TestToolchainNativeFeatureGraph(t *testing.T) {
	root := t.TempDir()
	m, err := Parse("roots.toml", []byte(mappingHeader+`
[dependencies.fmt]
go = "fmt"
[dependencies.json]
go = "encoding/json"
optional=true
[features]
json=["dep:json"]
`))
	if err != nil {
		t.Fatal(err)
	}
	w := &Workspace{Root: root, Members: map[string]string{m.Module: root}, Manifests: map[string]Manifest{root: m}}
	closure, err := NativeClosure(w, m.Module)
	if err != nil || !reflect.DeepEqual(closure, map[string]bool{m.Module: true}) {
		t.Fatalf("toolchain entered native member closure: %+v, %v", closure, err)
	}
	lock := Lock{NativeWorkspace: &NativeWorkspaceLock{Modules: map[string]Selection{}}}
	for _, features := range [][]string{nil, {"json"}} {
		graphs, err := NativeFeatureGraphs(w, []string{m.Module}, "unused-go-tool", map[string][]string{m.Module: features}, false, lock)
		if err != nil {
			t.Fatal(err)
		}
		g := graphs[m.Module]
		want := 1 + len(features)
		if len(g.Nodes) != 1 || len(g.Root.Requests) != want || len(g.Root.Children) != 0 || len(g.Root.Sources) != 0 {
			t.Fatalf("native toolchain graph: nodes=%d, requests=%v, children=%v, sources=%v", len(g.Nodes), g.Root.Requests, g.Root.Children, g.Root.Sources)
		}
	}
}

func TestToolchainWorkspacePlan(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, ManifestName, `[workspace]
format=1
members=["app"]
[workspace.dependencies.fmt]
go = "fmt"
[workspace.dependencies.encoding]
namespaces={json="encoding/json"}
go = 'encoding/json'
`)
	wsWrite(t, root, "app/"+ManifestName, mappingHeader+`
[dependencies.fmt]
workspace=true
[dependencies.encoding]
workspace=true
optional=true
[features]
json=["dep:encoding"]
`)
	app := filepath.Join(root, "app")
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, requested := range [][]string{nil, {"json"}} {
		g, err := Load(app, tool, requested, false, true, len(requested) != 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(g.Nodes) != 1 || len(g.Root.Requests) != 1+len(requested) || len(g.Root.Children) != 0 || len(g.Root.Sources) != 0 {
			t.Fatalf("workspace selected inactive toolchain roots or module edges: nodes=%d, requests=%v, children=%v, sources=%v", len(g.Nodes), g.Root.Requests, g.Root.Children, g.Root.Sources)
		}
		if g.Lock.Unified == nil || len(g.Lock.Unified.Modules) != 1 || g.Lock.Unified.Modules[g.Root.Module].Path != g.Root.Module {
			t.Fatalf("toolchain entered unified module lock: %+v", g.Lock.Unified)
		}
		if err := g.Save(); err != nil {
			t.Fatal(err)
		}
	}
}
