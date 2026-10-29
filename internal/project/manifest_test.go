package project

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestFeatureGraphDiagnostics(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`[features]
a = ['b']
b = ['a']

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "cycle"},
		{`[features]
a = ['unknown']

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "unknown feature"},
		{`[features]
a = ['dep:missing']

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "optional dependency"},
		{`[features]
a = ['missing/x']

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "unknown dependency"},
		{`[[bin]]
name = 'same'
path = 'a.gox'

[[bin]]
name = 'same'
path = 'b.gox'

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "duplicate target"},
		{`[[bin]]
name = 'bad'
path = '../outside'

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "stay inside"},
		{`[[lib]]
name = 'a'
path = 'a'

[[lib]]
name = 'b'
path = 'b'

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "array table"},
		{`[dependencies]
[dependencies.x]
path = 'x'
go = 'example.org/x'
version = 'v1.0.0'

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "exactly one"},
		{`[dependencies]
[dependencies.x]
git = 'https://example.invalid/x'
branch = 'main'
subdir = '../x'
module = 'example.org/x'

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "subdir"},
		{`[[bin]]
name = 'app'
path = 'a'
required_features = ['unknown']

[features]
x = []

[go_mod]
module = 'example.org/fixture'
go = '1.27'
`, "unknown required feature"},
	} {
		root := t.TempDir()
		if e := os.WriteFile(filepath.Join(root, ManifestName), []byte(tc.src), 0644); e != nil {
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

func TestManifestTOMLAndDiagnostics(t *testing.T) {
	const valid = `# human comments are not configuration
[go_mod]
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
	if len(m.Bins) != 1 || len(m.Examples) != 1 || m.Lib.Path != "src/lib.gox" || m.Build.TimeoutSeconds != 5 || m.Dependencies["some_lib"].Version != "v2.1.0" {
		t.Fatal(m)
	}
	doc := manifestDocument{Build: m.Build, Features: m.Features, Dependencies: authorDependencies(m.Dependencies), Lib: m.Lib, Bins: m.Bins, Examples: m.Examples}
	doc.GoMod = GoModConfig{Module: m.Module, Go: m.GoVersion, Toolchain: m.Toolchain}
	b, e := toml.Marshal(doc)
	if e != nil {
		t.Fatal(e)
	}
	again, e := Parse("roundtrip.toml", b)
	if e != nil || Digest(m) != Digest(again) {
		t.Fatalf("roundtrip %s %v", b, e)
	}
	for _, tc := range []struct{ name, src, want string }{
		{"case root", "[Go_Mod]\nmodule='example.org/x'", "case-sensitive"},
		{"case field", "[go_mod]\nModule='example.org/x'", "case-sensitive"},
		{"case dependency", "[go_mod]\n[dependencies.x]\nModule='example.org/x'\npath='.'", "case-sensitive"},
		{"case inline array", "bin=[{Name='x',path='.'}]\n[go_mod]", "case-sensitive"},
		{"empty unknown table", "[wat]\n[go_mod]", "unknown"},
		{"unknown", "[go_mod]\nmodule='example.org/x'\nmagic=2\n", "fixture.toml:3:1: unknown"},
		{"unknown table", "[go_mod]\nmodule='example.org/x'\n[wat]\nx=1", "fixture.toml:3:"},
		{"duplicate", "[go_mod]\nmodule='example.org/x'\nmodule='example.org/y'", "fixture.toml:3:"},
		{"type", "[go_mod]\nmodule=1", "fixture.toml:2:"},
		{"bad table name", "[go_mod]\nmodule='example.org/x'\n[dependencies.'bad/name']\npath='.'", "fixture.toml:3:"},
		{"inline bad name", "[go_mod]\nmodule='example.org/x'\n[features]\n'bad/name'=[]", "fixture.toml:4:"},
		{"malformed", "[go_mod\nmodule='example.org/x'", "fixture.toml:1:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, e := Parse("fixture.toml", []byte(tc.src))
			if e == nil || !strings.Contains(e.Error(), tc.want) {
				t.Fatalf("want %q got %v", tc.want, e)
			}
		})
	}
}

func TestManifestDetection(t *testing.T) {
	root := t.TempDir()
	if yes, e := Detect(root); yes || e != nil {
		t.Fatalf("%v %v", yes, e)
	}
	current := filepath.Join(root, ManifestName)
	if e := os.WriteFile(current, []byte("[go_mod]\nmodule='example.org/app'\ngo='1.27'\n"), 0644); e != nil {
		t.Fatal(e)
	}
	if yes, e := Detect(root); !yes || e != nil {
		t.Fatalf("%v %v", yes, e)
	}
	if e := os.Remove(current); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(root, "missing.toml"), current); e != nil {
		t.Fatal(e)
	}
	if _, e := Detect(root); e == nil || !strings.Contains(e.Error(), "regular file") {
		t.Fatal(e)
	}
}

func TestGitFailureRetainsDiagnostic(t *testing.T) {
	_, err := git(t.TempDir(), "rev-parse", "HEAD")
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("git failure lost exit error: %v", err)
	}
	detail := strings.TrimSpace(string(exit.Stderr))
	if detail == "" || !strings.Contains(err.Error(), detail) {
		t.Fatalf("git failure hid stderr %q: %v", detail, err)
	}
}
