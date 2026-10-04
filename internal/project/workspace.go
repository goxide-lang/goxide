package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// WorkspaceOwner cannot collide with a legal Go module identity.
const WorkspaceOwner = "<workspace>"

// Workspace is an authoring boundary. Membership never supplies a dependency.
type Workspace struct {
	Root      string
	Config    WorkspaceConfig
	Members   map[string]string // module identity -> canonical directory
	Manifests map[string]Manifest
	Defaults  []string
}
type WorkspaceConfig struct {
	Go           *NativeWorkspaceConfig `toml:"go,omitempty"`
	Vendor       *VendorConfig          `toml:"vendor,omitempty"`
	Format       int                    `toml:"format"`
	Members      []string               `toml:"members"`
	Defaults     []string               `toml:"default-members,omitempty"`
	Exclude      []string               `toml:"exclude,omitempty"`
	Dependencies map[string]Dependency  `toml:"dependencies,omitempty"`
	Package      struct {
		Go        string `toml:"go,omitempty"`
		Toolchain string `toml:"toolchain,omitempty"`
	} `toml:"package,omitempty"`
}

func workspaceHeader(root string) (*WorkspaceConfig, error) {
	b, e := os.ReadFile(filepath.Join(root, ManifestName))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var doc map[string]any
	if e = toml.Unmarshal(b, &doc); e != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(root, ManifestName), e)
	}
	if e = validateNames(filepath.Join(root, ManifestName), b); e != nil {
		return nil, e
	}
	raw, ok := doc["workspace"]
	if !ok {
		return nil, nil
	}
	b, e = toml.Marshal(raw)
	if e != nil {
		return nil, e
	}
	var c WorkspaceConfig
	if e = toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&c); e != nil {
		return nil, fmt.Errorf("workspace: %w", e)
	}
	if c.Vendor != nil && (c.Vendor.Mode != "go-work" || c.Go == nil) {
		return nil, fmt.Errorf("workspace vendor requires native go-work mode")
	}
	if c.Format != 1 {
		return nil, fmt.Errorf("unsupported workspace.format %d", c.Format)
	}
	if e := validate(Manifest{Version: 2, Module: "goxide-workspace.invalid/templates", GoVersion: "1.27", Dependencies: c.Dependencies}); e != nil {
		return nil, fmt.Errorf("workspace templates: %w", e)
	}
	for name, d := range c.Dependencies {
		if d.Workspace || d.Optional {
			return nil, fmt.Errorf("workspace dependency %s: templates cannot inherit or be optional", name)
		}
	}
	if _, ok := doc["package"]; !ok && len(doc) != 1 {
		return nil, fmt.Errorf("virtual workspace accepts only [workspace]")
	}
	return &c, nil
}

// DiscoverWorkspace stops at the nearest workspace, including an unlisted package.
func DiscoverWorkspace(root string) (*Workspace, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		c, e := workspaceHeader(dir)
		if e != nil {
			return nil, e
		}
		if c != nil {
			return loadWorkspace(dir, *c)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return nil, nil
}

func loadWorkspace(root string, c WorkspaceConfig) (*Workspace, error) {
	if e := NoSymlinks(root); e != nil {
		return nil, e
	}
	if e := CheckAuthoring(root); e != nil {
		return nil, e
	}
	w := &Workspace{Root: root, Config: c, Members: map[string]string{}, Manifests: map[string]Manifest{}}
	dirs := map[string]bool{}
	expand := func(pattern string) ([]string, error) {
		if pattern == "" || filepath.IsAbs(pattern) || strings.Contains(pattern, "**") {
			return nil, fmt.Errorf("workspace path must be explicit relative path or segment glob: %q", pattern)
		}
		if strings.ContainsAny(pattern, "*?[") && strings.Contains(pattern, "..") {
			return nil, fmt.Errorf("workspace external members require explicit paths")
		}
		parts := strings.Split(filepath.ToSlash(filepath.Clean(pattern)), "/")
		for _, part := range parts {
			if part == "target" || part == ".git" || part == ".hgo" {
				return nil, fmt.Errorf("workspace member cannot traverse reserved directory %s", part)
			}
		}
		matches := []string{root}
		for _, part := range parts {
			next := []string{}
			for _, parent := range matches {
				children, e := filepath.Glob(filepath.Join(parent, part))
				if e != nil {
					return nil, e
				}
				for _, dir := range children {
					base := filepath.Base(dir)
					if base == "target" || base == ".git" || base == ".hgo" || base == "vendor" {
						return nil, fmt.Errorf("workspace pattern traverses reserved directory %s", dir)
					}
					if e := NoSymlinks(dir); e != nil {
						return nil, e
					}
					if _, e := os.Lstat(filepath.Join(dir, LayoutMarker)); e == nil {
						return nil, fmt.Errorf("workspace pattern traverses managed output %s", dir)
					} else if !os.IsNotExist(e) {
						return nil, e
					}
					next = append(next, dir)
				}
			}
			matches = next
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("workspace member pattern has no matches: %s", pattern)
		}
		return matches, nil
	}
	for _, p := range c.Members {
		matches, e := expand(p)
		if e != nil {
			return nil, e
		}
		for _, d := range matches {
			dirs[d] = true
		}
	}
	for _, p := range c.Exclude {
		matches, e := expand(p)
		if e != nil {
			return nil, e
		}
		for _, d := range matches {
			delete(dirs, d)
		}
	}
	b, e := os.ReadFile(filepath.Join(root, ManifestName))
	if e != nil {
		return nil, e
	}
	var raw map[string]any
	if e = toml.Unmarshal(b, &raw); e != nil {
		return nil, e
	}
	_, rootPackage := raw["package"]
	if rootPackage {
		dirs[root] = true
	}
	for _, dir := range keys(dirs) {
		if dir != root {
			if e := RejectManagedOutput(dir); e != nil {
				return nil, e
			}
			for parent := dir; parent != root; parent = filepath.Dir(parent) {
				if c, e := workspaceHeader(parent); e != nil {
					return nil, e
				} else if c != nil {
					return nil, fmt.Errorf("member belongs to another workspace: %s", parent)
				}
				if filepath.Dir(parent) == parent {
					break
				}
			}
			if _, e := os.Lstat(filepath.Join(dir, "goxide.lock")); e == nil {
				return nil, fmt.Errorf("WS-MEMBER-LOCK: member must use root lock: %s", dir)
			} else if !os.IsNotExist(e) {
				return nil, e
			}
		}
		if e := CheckAuthoring(dir); e != nil {
			return nil, e
		}
		b, e := os.ReadFile(filepath.Join(dir, ManifestName))
		if e != nil {
			return nil, e
		}
		m, e := parseWorkspaceManifest(filepath.Join(dir, ManifestName), b, w)
		if e != nil {
			return nil, e
		}
		if m.Vendor != nil {
			return nil, fmt.Errorf("workspace vendor is not supported; use standalone vendor project")
		}
		if old, ok := w.Members[m.Module]; ok {
			return nil, fmt.Errorf("duplicate workspace module %s: %s and %s", m.Module, old, dir)
		}
		w.Members[m.Module] = dir
		w.Manifests[dir] = m
	}
	if len(w.Members) == 0 {
		return nil, fmt.Errorf("workspace requires at least one member")
	}
	for _, p := range c.Defaults {
		dir := filepath.Clean(filepath.Join(root, p))
		found := ""
		for mod, d := range w.Members {
			if d == dir {
				found = mod
			}
		}
		if found == "" {
			return nil, fmt.Errorf("default member is not a member: %s", p)
		}
		w.Defaults = append(w.Defaults, found)
	}
	if len(c.Defaults) == 0 {
		if rootPackage {
			w.Defaults = []string{w.Manifests[root].Module}
		} else {
			w.Defaults = keys(w.Members)
		}
	}
	sort.Strings(w.Defaults)
	return w, nil
}

func parseWorkspaceManifest(filename string, b []byte, w *Workspace) (Manifest, error) {
	if e := validateNames(filename, b); e != nil {
		return Manifest{}, e
	}
	var raw map[string]any
	if e := toml.Unmarshal(b, &raw); e != nil {
		return Manifest{}, e
	}
	delete(raw, "workspace")
	if pkg, ok := raw["package"].(map[string]any); ok {
		for _, key := range []string{"go", "toolchain"} {
			if v, ok := pkg[key].(map[string]any); ok {
				if len(v) != 1 || v["workspace"] != true {
					return Manifest{}, fmt.Errorf("%s: invalid package.%s inheritance", filename, key)
				}
				value := w.Config.Package.Go
				if key == "toolchain" {
					value = w.Config.Package.Toolchain
				}
				if value == "" {
					return Manifest{}, fmt.Errorf("workspace.package.%s is not defined", key)
				}
				pkg[key] = value
			}
		}
	}
	if deps, ok := raw["dependencies"].(map[string]any); ok {
		for alias, v := range deps {
			d, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if d["workspace"] != true {
				continue
			}
			base, ok := w.Config.Dependencies[alias]
			if !ok {
				return Manifest{}, fmt.Errorf("workspace dependency template %s not found", alias)
			}
			for k := range d {
				if k != "workspace" && k != "features" && k != "default_features" && k != "optional" {
					return Manifest{}, fmt.Errorf("dependency %s: workspace inheritance cannot override %s", alias, k)
				}
			}
			if base.Path != "" && !filepath.IsAbs(base.Path) {
				base.Path = filepath.Join(w.Root, base.Path)
			}
			b, e := toml.Marshal(base)
			if e != nil {
				return Manifest{}, e
			}
			var merged map[string]any
			if e = toml.Unmarshal(b, &merged); e != nil {
				return Manifest{}, e
			}
			if _, present := d["features"]; present {
				if _, ok := d["features"].([]any); !ok {
					return Manifest{}, fmt.Errorf("dependency %s: features must be an array", alias)
				}
			}
			if f, ok := d["features"].([]any); ok {
				prior, _ := merged["features"].([]any)
				merged["features"] = append(prior, f...)
			}
			for _, k := range []string{"default_features", "optional"} {
				if v, ok := d[k]; ok {
					merged[k] = v
				}
			}
			deps[alias] = merged
		}
	}
	b, e := toml.Marshal(raw)
	if e != nil {
		return Manifest{}, e
	}
	return Parse(filename, b)
}

func (w *Workspace) Select(input string, packages []string, all bool) ([]string, error) {
	input, e := filepath.Abs(input)
	if e != nil {
		return nil, e
	}
	if _, ok := w.Manifests[input]; !ok && input != w.Root {
		return nil, fmt.Errorf("WS-NOT-MEMBER: input is not a workspace member: %s", input)
	}
	if all && len(packages) > 0 {
		return nil, fmt.Errorf("--workspace conflicts with --package")
	}
	if all {
		return keys(w.Members), nil
	}
	if len(packages) == 0 {
		if m, ok := w.Manifests[input]; ok && input != w.Root {
			packages = []string{m.Module}
		} else {
			packages = w.Defaults
		}
	}
	seen := map[string]bool{}
	for _, p := range packages {
		if _, ok := w.Members[p]; !ok {
			return nil, fmt.Errorf("unknown workspace package %s", p)
		}
		seen[p] = true
	}
	return keys(seen), nil
}
