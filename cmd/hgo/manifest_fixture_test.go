package main

import (
	"encoding/json"
	"github.com/hxsf/goxide/internal/project"
	"github.com/pelletier/go-toml/v2"
	"os"
	"path/filepath"
	"runtime"
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
	if v, ok := m["version"]; ok {
		if _, ok := m["package"]; !ok {
			m["package"] = map[string]any{"format": int(v.(float64))}
		}
		delete(m, "version")
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
	if p, ok := (*m)["package"].(map[string]any); ok {
		(*m)["version"] = p["format"]
		// Keep module/go identity when editing this fixture.
	}
	for a, b := range map[string]string{"bin": "bins", "example": "examples"} {
		if v, ok := (*m)[a]; ok {
			(*m)[b] = v
			delete(*m, a)
		}
	}
	return nil
}

// Legacy fixture vocabulary is adapted at the test boundary; disk projects use
// format 2 exclusively. Native-only projects and version transport go.mod remain.
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
	mapdata["package"] = identity["package"]
	if v, ok := identity["go"]; ok {
		mapdata["go"] = v
	} else {
		delete(mapdata, "go")
	}
	b, e = toml.Marshal(mapdata)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
