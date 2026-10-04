// Package project implements the additive hgo.json project format. Go remains
// responsible for module versions, checksums, and native dependency resolution.
package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ManifestName = "hgo.json"

type Manifest struct {
	Version      int                   `json:"version"`
	Features     map[string][]string   `json:"features,omitempty"`
	Dependencies map[string]Dependency `json:"dependencies,omitempty"`
	Lib          *Target               `json:"lib,omitempty"`
	Bins         []Target              `json:"bins,omitempty"`
	Examples     []Target              `json:"examples,omitempty"`
}
type Dependency struct {
	Module          string   `json:"module"`
	Version         string   `json:"version,omitempty"`
	Path            string   `json:"path,omitempty"`
	Git             string   `json:"git,omitempty"`
	Rev             string   `json:"rev,omitempty"`
	Subdir          string   `json:"subdir,omitempty"`
	Optional        bool     `json:"optional,omitempty"`
	Features        []string `json:"features,omitempty"`
	DefaultFeatures *bool    `json:"default_features,omitempty"`
}
type Target struct {
	Name             string   `json:"name"`
	Path             string   `json:"path"`
	RequiredFeatures []string `json:"required_features,omitempty"`
}

var ident = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func Read(root string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(filepath.Join(root, ManifestName))
	if e != nil {
		return m, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&m); e != nil {
		return m, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return m, fmt.Errorf("manifest must contain one JSON object")
	}
	if m.Version != 1 {
		return m, fmt.Errorf("unsupported hgo.json version %d", m.Version)
	}
	for k, d := range m.Dependencies {
		if !ident.MatchString(k) || d.Module == "" {
			return m, fmt.Errorf("invalid dependency %q", k)
		}
		n := 0
		for _, v := range []string{d.Version, d.Path, d.Git} {
			if v != "" {
				n++
			}
		}
		if n != 1 {
			return m, fmt.Errorf("dependency %s requires exactly one of version, path, git", k)
		}
		if d.Git == "" && (d.Rev != "" || d.Subdir != "") {
			return m, fmt.Errorf("dependency %s: rev/subdir require git", k)
		}
		if d.Git != "" && d.Rev == "" {
			return m, fmt.Errorf("dependency %s: git requires explicit rev", k)
		}
		if d.Subdir != "" && !safeRelative(d.Subdir) {
			return m, fmt.Errorf("dependency %s: subdir must stay inside repository", k)
		}
	}
	for k := range m.Features {
		if !ident.MatchString(k) {
			return m, fmt.Errorf("invalid feature %q", k)
		}
	}
	targets := [][]Target{m.Bins, m.Examples}
	if m.Lib != nil {
		targets = append(targets, []Target{*m.Lib})
	}
	for _, ts := range targets {
		seen := map[string]bool{}
		for _, t := range ts {
			if !ident.MatchString(t.Name) || seen[t.Name] {
				return m, fmt.Errorf("invalid or duplicate target name %q", t.Name)
			}
			seen[t.Name] = true
			if !safeRelative(t.Path) {
				return m, fmt.Errorf("target %s path must stay inside module", t.Name)
			}
			for _, f := range t.RequiredFeatures {
				if _, ok := m.Features[f]; !ok {
					return m, fmt.Errorf("target %s: unknown required feature %s", t.Name, f)
				}
			}
		}
	}
	// Validate dormant features as well as enabled ones.
	_, _, e = Resolve(m, keys(m.Features), false)
	return m, e
}
func safeRelative(p string) bool {
	return p != "" && !filepath.IsAbs(p) && p != ".." && !strings.HasPrefix(filepath.Clean(p), ".."+string(filepath.Separator))
}
func keys[V any](m map[string]V) []string {
	r := []string{}
	for k := range m {
		r = append(r, k)
	}
	sort.Strings(r)
	return r
}
func Keys[V any](m map[string]V) []string { return keys(m) }

type Request struct {
	Features []string
	Defaults bool
}

// Resolve computes a monotone feature union. dep:name activates optional deps;
// name/feature activates the dependency and forwards a feature request.
func Resolve(m Manifest, requested []string, defaults bool) (map[string]bool, map[string]Request, error) {
	enabled := map[string]bool{}
	deps := map[string]Request{}
	state := map[string]int{}
	activate := func(n string, f string) error {
		d, ok := m.Dependencies[n]
		if !ok {
			return fmt.Errorf("unknown dependency %q", n)
		}
		r, exists := deps[n]
		if !exists {
			r.Features = append([]string(nil), d.Features...)
			r.Defaults = d.DefaultFeatures == nil || *d.DefaultFeatures
		}
		if f != "" {
			r.Features = append(r.Features, f)
		}
		deps[n] = r
		return nil
	}
	for n, d := range m.Dependencies {
		if !d.Optional {
			_ = activate(n, "")
		}
	}
	var visit func(string) error
	visit = func(n string) error {
		if state[n] == 1 {
			return fmt.Errorf("feature cycle at %q", n)
		}
		if state[n] == 2 {
			return nil
		}
		edges, ok := m.Features[n]
		if !ok {
			return fmt.Errorf("unknown feature %q", n)
		}
		state[n] = 1
		enabled[n] = true
		for _, edge := range edges {
			if strings.HasPrefix(edge, "dep:") {
				dep := strings.TrimPrefix(edge, "dep:")
				d, ok := m.Dependencies[dep]
				if !ok || !d.Optional {
					return fmt.Errorf("feature %s: dep:%s must name an optional dependency", n, dep)
				}
				if e := activate(dep, ""); e != nil {
					return e
				}
			} else if dep, f, ok := strings.Cut(edge, "/"); ok {
				if f == "" {
					return fmt.Errorf("empty forwarded feature in %s", edge)
				}
				if e := activate(dep, f); e != nil {
					return e
				}
			} else if e := visit(edge); e != nil {
				return e
			}
		}
		state[n] = 2
		return nil
	}
	if defaults {
		if _, ok := m.Features["default"]; ok {
			requested = append(append([]string(nil), requested...), "default")
		}
	}
	for _, n := range requested {
		if e := visit(n); e != nil {
			return nil, nil, e
		}
	}
	return enabled, deps, nil
}
func Select(m Manifest, bin, example string, features map[string]bool) (Target, error) {
	if bin != "" && example != "" {
		return Target{}, fmt.Errorf("--bin and --example are mutually exclusive")
	}
	ts, name := m.Bins, bin
	if example != "" {
		ts, name = m.Examples, example
	}
	if name == "" {
		if len(ts) == 0 {
			return Target{}, fmt.Errorf("project has no bin; select --lib or --example")
		}
		if len(ts) != 1 {
			return Target{}, fmt.Errorf("multiple bins: select one with --bin")
		}
		name = ts[0].Name
	}
	for _, t := range ts {
		if t.Name == name {
			for _, f := range t.RequiredFeatures {
				if !features[f] {
					return Target{}, fmt.Errorf("target %s requires feature %s", name, f)
				}
			}
			return t, nil
		}
	}
	return Target{}, fmt.Errorf("unknown target %q", name)
}
