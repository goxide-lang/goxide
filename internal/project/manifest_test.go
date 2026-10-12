package project

import (
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureGraphDiagnostics(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`[features]
a = ['b']
b = ['a']

[package]
format = 1
`, "cycle"},
		{`[features]
a = ['unknown']

[package]
format = 1
`, "unknown feature"},
		{`[features]
a = ['dep:missing']

[package]
format = 1
`, "optional dependency"},
		{`[features]
a = ['missing/x']

[package]
format = 1
`, "unknown dependency"},
		{`[[bin]]
name = 'same'
path = 'a.gox'

[[bin]]
name = 'same'
path = 'b.gox'

[package]
format = 1
`, "duplicate target"},
		{`[[bin]]
name = 'bad'
path = '../outside'

[package]
format = 1
`, "stay inside"},
		{`[[lib]]
name = 'a'
path = 'a'

[[lib]]
name = 'b'
path = 'b'

[package]
format = 1
`, "array table"},
		{`[dependencies]
[dependencies.x]
path = 'x'
go = 'example.org/x'
version = 'v1.0.0'

[package]
format = 1
`, "exactly one"},
		{`[dependencies]
[dependencies.x]
git = 'https://example.invalid/x'
branch = 'main'
subdir = '../x'
module = 'example.org/x'

[package]
format = 1
`, "subdir"},
		{`[[bin]]
name = 'app'
path = 'a'
required_features = ['unknown']

[features]
x = []

[package]
format = 1
`, "unknown required feature"},
	} {
		root := t.TempDir()
		if e := os.WriteFile(filepath.Join(root, ManifestName), []byte(strings.ReplaceAll(tc.src, "[package]\nformat = 1", "[package]\nformat = 2\nmodule = 'example.org/fixture'\ngo = '1.27'")), 0644); e != nil {
			t.Fatal(e)
		}
		_, e := Read(root)
		if e == nil || !strings.Contains(e.Error(), tc.want) {
			t.Fatalf("want %s got %v", tc.want, e)
		}
	}
}
func TestGitURLAndLockBoundaries(t *testing.T) {
	for _, raw := range []string{"https://user:password@example.invalid/x", "ext::sh -c whoami", "-uanything", "ssh://example.invalid/x", "https://example.invalid/x?token=secret"} {
		if validateGit(raw) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	root := t.TempDir()
	release, e := LockProject(root)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = LockProject(root); e == nil {
		t.Fatal("parallel writer accepted")
	}
	other := t.TempDir()
	if e = os.Symlink(root, filepath.Join(other, "goxide.lock.guard")); e != nil {
		t.Fatal(e)
	}
	if _, e = LockProject(other); e == nil {
		t.Fatal("symlink build guard accepted")
	}
}

func TestTOMLFormatAndDiagnostics(t *testing.T) {
	const valid = `# human comments are not configuration
[package]
format = 2
module = "example.org/fixture"
go = "1.27"
[features]
default = [
 "fast", # trailing comments and commas
]
fast = []
[dependencies."some_lib"]
go = 'example.org/lib/v2'
version = "v2.1.0"
default_features = false
features = []
[lib]
name = 'lib'
path = '''src/lib.gox'''
[[bin]]
name = 'cli'
path = 'cmd/cli'
required_features = ["fast"]
[[example]]
name = 'demo'
path = 'examples/demo'
[build]
entry = 'tools/build.go'
inputs = ['schema.txt']
outputs = ['gen/a.go']
env = []
timeout_seconds = 5
[build.dependencies]
"example.org/helper" = '../helper'
[build.tools]
formatter = '/tools/formatter'
`
	m, e := Parse("fixture.toml", []byte(valid))
	if e != nil {
		t.Fatal(e)
	}
	enabled, deps, e := Resolve(m, nil, true)
	if e != nil || !enabled["fast"] || deps["some_lib"].Defaults {
		t.Fatalf("%v %v %v", enabled, deps, e)
	}
	if m.Version != 2 || len(m.Bins) != 1 || len(m.Examples) != 1 || m.Lib.Path != "src/lib.gox" || m.Build.TimeoutSeconds != 5 || m.Dependencies["some_lib"].Version != "v2.1.0" {
		t.Fatal(m)
	}
	doc := manifestDocument{Build: m.Build, Features: m.Features, Dependencies: authorDependencies(m.Dependencies), Lib: m.Lib, Bins: m.Bins, Examples: m.Examples}
	doc.Package = PackageConfig{Format: m.Version, Module: m.Module, Go: m.GoVersion, Toolchain: m.Toolchain, Namespaces: m.PackageNamespaces}
	b, e := toml.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	again, e := Parse("roundtrip.toml", b)
	if e != nil || Digest(m) != Digest(again) {
		t.Fatalf("roundtrip %s %v", b, e)
	}
	for _, tc := range []struct{ name, src, want string }{
		{"case root", "[Package]\nformat=1", "case-sensitive"},
		{"case field", "[package]\nFormat=1", "case-sensitive"},
		{"case dependency", "[package]\nformat=1\n[dependencies.x]\nModule='example.org/x'\npath='.'", "case-sensitive"},
		{"case inline array", "bin=[{Name='x',path='.'}]\n[package]\nformat=1", "case-sensitive"},
		{"empty unknown table", "[wat]\n[package]\nformat=1", "unknown"},
		{"unknown", "[package]\nformat=1\nmagic=2\n", "fixture.toml:3:1: unknown"},
		{"unknown table", "[package]\nformat=1\n[wat]\nx=1", "fixture.toml:3:"},
		{"duplicate", "[package]\nformat=1\nformat=1", "fixture.toml:3:"},
		{"type", "[package]\nformat='1'", "fixture.toml:2:"},
		{"bad table name", "[package]\nformat=1\n[dependencies.'bad/name']\nmodule='example.org/x'\npath='.'", "fixture.toml:3:"},
		{"inline bad name", "[package]\nformat=1\n[features]\n'bad/name'=[]", "fixture.toml:4:"},
		{"malformed", "[package\nformat=1", "fixture.toml:1:"},
		{"renamed JSON", `{"version":1}`, "fixture.toml:1:"},
		{"wrong bins", "[package]\nformat=1\n[[bins]]\nname='x'\npath='.'", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Parse("fixture.toml", []byte(tc.src))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q got %v", tc.want, e)
			}
		})
	}
}

func TestManifestDetectionMigration(t *testing.T) {
	root := t.TempDir()
	if yes, e := Detect(root); yes || e != nil {
		t.Fatalf("%v %v", yes, e)
	}
	old := filepath.Join(root, LegacyManifestName)
	current := filepath.Join(root, ManifestName)
	os.WriteFile(old, []byte(`{"version":1}`), 0644)
	if _, e := Detect(root); e == nil || !strings.Contains(e.Error(), "legacy hgo.json") {
		t.Fatal(e)
	}
	os.WriteFile(current, []byte("[package]\nformat=1\n"), 0644)
	if _, e := Read(root); e == nil || !strings.Contains(e.Error(), "both") {
		t.Fatal(e)
	}
	os.Remove(old)
	if yes, e := Detect(root); !yes || e != nil {
		t.Fatalf("%v %v", yes, e)
	}
	os.Remove(current)
	os.Symlink(old, current)
	if _, e := Detect(root); e == nil || !strings.Contains(e.Error(), "regular file") {
		t.Fatal(e)
	}
}
