package main

import (
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
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
		m["package"] = map[string]any{"format": int(v.(float64))}
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
		delete(*m, "package")
	}
	for a, b := range map[string]string{"bin": "bins", "example": "examples"} {
		if v, ok := (*m)[a]; ok {
			(*m)[b] = v
			delete(*m, a)
		}
	}
	return nil
}
