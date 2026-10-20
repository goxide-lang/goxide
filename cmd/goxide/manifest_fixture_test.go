package main

import (
	"encoding/json"
	"github.com/hxsf/goxide/internal/project"
	"github.com/pelletier/go-toml/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Builders accept the internal Manifest field vocabulary; every stored fixture is TOML.
func projectManifest(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return manifestJSON(string(b))
}
func manifestJSON(s string) string {
	var m map[string]any
	if e := json.Unmarshal([]byte(s), &m); e != nil {
		panic(e)
	}
	for a, b := range map[string]string{"bins": "bin", "examples": "example"} {
		if v, ok := m[a]; ok {
			m[b] = v
			delete(m, a)
		}
	}
	if build, ok := m["build"].(map[string]any); ok {
		if v, ok := build["timeout_seconds"].(float64); ok {
			build["timeout_seconds"] = int(v)
		}
	}
	b, e := toml.Marshal(m)
	if e != nil {
		panic(e)
	}
	return string(b)
}
func decodeManifestFixture(b []byte, m *map[string]any) error {
	if e := toml.Unmarshal(b, m); e != nil {
		return e
	}
	for a, b := range map[string]string{"bin": "bins", "example": "examples"} {
		if v, ok := (*m)[a]; ok {
			(*m)[b] = v
			delete(*m, a)
		}
	}
	return nil
}

// Fixture builders combine native module identity with the project fields.
// Native-only projects and published Go transport metadata keep their go.mod.
func fixtureAuthoring(t *testing.T, root string, files map[string]string) {
	t.Helper()
	text, has := files["goxide.toml"]
	if !has {
		if _, goChange := files["go.mod"]; !goChange {
			return
		}
		b, e := os.ReadFile(filepath.Join(root, "goxide.toml"))
		if e != nil {
			return
		}
		text = string(b)
	}
	mod, ok := files["go.mod"]
	if !ok {
		b, e := os.ReadFile(filepath.Join(root, "go.mod"))
		if e == nil {
			mod = string(b)
		} else {
			old, e := project.Read(root)
			if e == nil || old.Module != "" {
				b, _ := project.ModuleText(old)
				mod = string(b)
			}
		}
	}
	if mod == "" {
		if has {
			files["goxide.toml"] = fixtureDependencyManifest(t, text)
		}
		return
	}
	files["goxide.toml"] = fixtureManifest(t, text, mod)
	delete(files, "go.mod")
	delete(files, "go.sum")
	os.Remove(filepath.Join(root, "go.mod"))
	os.Remove(filepath.Join(root, "go.sum"))
}
func fixtureManifest(t *testing.T, text, mod string) string {
	t.Helper()
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0644); e != nil {
		t.Fatal(e)
	}
	m, e := project.ImportMod(filepath.Join(runtime.GOROOT(), "bin", "go"), dir)
	if e != nil {
		t.Fatal(e)
	}
	b, e := project.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	var identity, mapdata map[string]any
	if e = toml.Unmarshal(b, &identity); e != nil {
		t.Fatal(e)
	}
	if e = toml.Unmarshal([]byte(text), &mapdata); e != nil {
		t.Fatal(e)
	}
	mapdata["go_mod"] = identity["go_mod"]
	if v, ok := identity["go"]; ok {
		mapdata["go"] = v
	} else {
		delete(mapdata, "go")
	}
	b, e = toml.Marshal(mapdata)
	if e != nil {
		t.Fatal(e)
	}
	return fixtureDependencyManifest(t, string(b))
}

// Keep explicit short aliases used by the CLI fixtures.
func fixtureDependencyManifest(t *testing.T, text string) string {
	t.Helper()
	var doc map[string]any
	if err := toml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	goMod, _ := doc["go_mod"].(map[string]any)
	if goMod == nil {
		return text
	}
	module, _ := goMod["module"].(string)
	if module == "" {
		return text
	}
	deps, _ := doc["dependencies"].(map[string]any)
	if deps == nil {
		deps = map[string]any{}
		doc["dependencies"] = deps
	}
	for alias, path := range map[string]string{"fmt": "fmt", "json": "encoding/json", "os": "os", "filepath": "path/filepath", "strings": "strings"} {
		if _, exists := deps[alias]; !exists {
			deps[alias] = map[string]any{"go": path}
		}
	}
	data, err := toml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const fixtureToolchainRoots = `[roots.fmt]
source="toolchain"
package="fmt"
[roots.json]
source="toolchain"
package="encoding/json"
[roots.os]
source="toolchain"
package="os"
`
const fixtureSerdeRoots = `[roots.serde]
module="github.com/goxide-lang/serde"
package="github.com/goxide-lang/serde"
`

func fixtureRootsFile(t *testing.T, root, extra string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "goxide.roots.toml"), []byte(fixtureToolchainRoots+extra), 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git: %s %v", b, e)
	}
	return strings.TrimSpace(string(b))
}
