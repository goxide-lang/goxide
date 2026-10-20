// Package project implements the goxide.toml project format. Go remains
// responsible for module versions, checksums, and native dependency resolution.
package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/buildconfig"
	"github.com/pelletier/go-toml/v2"
)

const ManifestName = "goxide.toml"

type Manifest struct {
	Module       string                `json:"module"`
	GoVersion    string                `json:"go"`
	Toolchain    string                `json:"toolchain,omitempty"`
	Native       GoConfig              `json:"native,omitempty"`
	Build        *buildconfig.Config   `json:"build,omitempty" toml:"build,omitempty"`
	Features     map[string][]string   `json:"features,omitempty" toml:"features,omitempty"`
	Dependencies map[string]Dependency `json:"dependencies,omitempty" toml:"dependencies,omitempty"`
	Lib          *Target               `json:"lib,omitempty" toml:"lib,omitempty"`
	Bins         []Target              `json:"bins,omitempty" toml:"bin,omitempty"`
	Examples     []Target              `json:"examples,omitempty" toml:"example,omitempty"`
}
type Dependency struct {
	Workspace       bool     `json:"workspace,omitempty" toml:"workspace,omitempty"`
	Go              string   `json:"go,omitempty" toml:"go,omitempty"`
	Registry        string   `json:"registry,omitempty" toml:"registry,omitempty"`
	Module          string   `json:"module,omitempty" toml:"module,omitempty"`
	Version         string   `json:"version,omitempty" toml:"version,omitempty"`
	Path            string   `json:"path,omitempty" toml:"path,omitempty"`
	Git             string   `json:"git,omitempty" toml:"git,omitempty"`
	Rev             string   `json:"rev,omitempty" toml:"rev,omitempty"`
	Tag             string   `json:"tag,omitempty" toml:"tag,omitempty"`
	Branch          string   `json:"branch,omitempty" toml:"branch,omitempty"`
	Subdir          string   `json:"subdir,omitempty" toml:"subdir,omitempty"`
	Optional        bool     `json:"optional,omitempty" toml:"optional,omitempty"`
	Features        []string `json:"features,omitempty" toml:"features,omitempty"`
	DefaultFeatures *bool    `json:"default_features,omitempty" toml:"default_features,omitempty"`
}

// IsToolchain identifies roots supplied by Go itself, outside the module graph.
func (d Dependency) IsToolchain() bool { return d.Go != "" && d.Version == "" }

type Target struct {
	Name             string   `json:"name" toml:"name"`
	Path             string   `json:"path" toml:"path"`
	RequiredFeatures []string `json:"required_features,omitempty" toml:"required_features,omitempty"`
}

var ident = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// DiscoverProject selects the nearest author manifest, including a virtual
// workspace root. Native Go modules do not hide an outer workspace.
func DiscoverProject(dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		found, err := Detect(dir)
		if err != nil {
			return "", err
		}
		if found {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// Detect reports a regular author manifest before native Go fallback.
func Detect(root string) (bool, error) {
	info, err := os.Lstat(filepath.Join(root, ManifestName))
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if info == nil {
		return false, nil
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s: manifest must be a regular file", filepath.Join(root, ManifestName))
	}
	return true, nil
}

// manifestDocument deliberately separates the human format from internal identities.
type manifestDocument struct {
	Workspace    *WorkspaceConfig      `toml:"workspace,omitempty"`
	GoMod        GoModConfig           `toml:"go_mod"`
	Native       GoConfig              `toml:"go,omitempty"`
	Build        *buildconfig.Config   `toml:"build,omitempty"`
	Features     map[string][]string   `toml:"features,omitempty"`
	Dependencies map[string]Dependency `toml:"dependencies,omitempty"`
	Lib          *Target               `toml:"lib,omitempty"`
	Bins         []Target              `toml:"bin,omitempty"`
	Examples     []Target              `toml:"example,omitempty"`
}

func Read(root string) (Manifest, error) { return readManifest(root, "") }

func readManifest(root, checkout string) (Manifest, error) {
	if _, err := Detect(root); err != nil {
		return Manifest{}, err
	}
	b, err := os.ReadFile(filepath.Join(root, ManifestName))
	if err != nil {
		return Manifest{}, err
	}
	w, err := discoverWorkspace(root, checkout)
	if err != nil {
		return Manifest{}, err
	}
	if w != nil {
		if m, ok := w.Manifests[root]; ok {
			return m, nil
		}
	}
	return Parse(filepath.Join(root, ManifestName), b)
}

// Parse reads the strict, current TOML schema. filename is diagnostic context.
func Parse(filename string, b []byte) (Manifest, error) {
	var d manifestDocument
	if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&d); err != nil {
		return Manifest{}, manifestDecodeError(filename, err)
	}
	if d.Workspace != nil {
		return Manifest{}, fmt.Errorf("workspace manifest requires workspace-aware loading")
	}
	if err := validateNames(filename, b); err != nil {
		return Manifest{}, err
	}
	deps, err := resolveAuthorDependencies(d.Dependencies)
	if err != nil {
		return Manifest{}, fmt.Errorf("%s:1:1: %w", filename, err)
	}
	m := Manifest{Module: d.GoMod.Module, GoVersion: d.GoMod.Go, Toolchain: d.GoMod.Toolchain, Native: d.Native, Build: d.Build, Features: d.Features, Dependencies: deps, Lib: d.Lib, Bins: d.Bins, Examples: d.Examples}
	if err := validate(m); err != nil {
		return m, fmt.Errorf("%s:1:1: %w", filename, err)
	}
	return m, nil
}

func validate(m Manifest) error {
	if _, err := ModuleText(m); err != nil {
		return err
	}
	for k, d := range m.Dependencies {
		if d.Workspace {
			return fmt.Errorf("dependency %s: workspace inheritance requires membership in a workspace", k)
		}
		if !dependencyIdentifier(k) {
			return fmt.Errorf("invalid dependency %q", k)
		}
		if err := validateDependency(k, d); err != nil {
			return err
		}
		if d.IsToolchain() {
			continue
		}
		n := 0
		for _, v := range []string{d.Version, d.Path, d.Git} {
			if v != "" {
				n++
			}
		}
		if n != 1 {
			return fmt.Errorf("dependency %s requires exactly one of version, path, git", k)
		}
		if d.Git == "" && (d.Rev != "" || d.Subdir != "") {
			return fmt.Errorf("dependency %s: rev/subdir require git", k)
		}
		if d.Subdir != "" && !safeRelative(d.Subdir) {
			return fmt.Errorf("dependency %s: subdir must stay inside repository", k)
		}
	}
	for k := range m.Features {
		if !ident.MatchString(k) {
			return fmt.Errorf("invalid feature %q", k)
		}
	}
	targets := [][]Target{m.Bins, m.Examples}
	if m.Lib != nil {
		if !dependencyIdentifier(m.Lib.Name) {
			return fmt.Errorf("invalid library namespace %q", m.Lib.Name)
		}
		if _, exists := m.Dependencies[m.Lib.Name]; exists {
			return fmt.Errorf("library namespace %q conflicts with dependency alias", m.Lib.Name)
		}
		targets = append(targets, []Target{*m.Lib})
	}
	for group, ts := range targets {
		seen := map[string]bool{}
		for _, t := range ts {
			if group < 2 && !ident.MatchString(t.Name) || seen[t.Name] {
				return fmt.Errorf("invalid or duplicate target name %q", t.Name)
			}
			seen[t.Name] = true
			if !safeRelative(t.Path) {
				return fmt.Errorf("target %s path must stay inside module", t.Name)
			}
			for _, f := range t.RequiredFeatures {
				if _, ok := m.Features[f]; !ok {
					return fmt.Errorf("target %s: unknown required feature %s", t.Name, f)
				}
			}
		}
	}
	// Validate dormant features as well as enabled ones.
	_, _, e := Resolve(m, keys(m.Features), false)
	return e
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
		if d.IsToolchain() && (f != "" || d.Features != nil || d.DefaultFeatures != nil) {
			return fmt.Errorf("dependency %s: toolchain source cannot receive feature requests", n)
		}
		r, exists := deps[n]
		if !exists {
			r.Features = append([]string(nil), d.Features...)
			r.Defaults = !d.IsToolchain() && (d.DefaultFeatures == nil || *d.DefaultFeatures)
		}
		if f != "" {
			r.Features = append(r.Features, f)
		}
		deps[n] = r
		return nil
	}
	for n, d := range m.Dependencies {
		if !d.Optional {
			if err := activate(n, ""); err != nil {
				return nil, nil, err
			}
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
func Select(m Manifest, bin, example string, lib bool, features map[string]bool) (Target, error) {
	if bin != "" && example != "" || lib && (bin != "" || example != "") {
		return Target{}, fmt.Errorf("--bin, --example and --lib are mutually exclusive")
	}
	ts, name := m.Bins, bin
	if lib {
		if m.Lib == nil {
			return Target{}, fmt.Errorf("project has no lib")
		}
		ts, name = []Target{*m.Lib}, m.Lib.Name
	} else if example != "" {
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

// Marshal emits the authoring schema, not the internal JSON identity representation.
func Marshal(m Manifest) ([]byte, error) {
	d := manifestDocument{GoMod: GoModConfig{Module: m.Module, Go: m.GoVersion, Toolchain: m.Toolchain}, Native: m.Native, Build: m.Build, Features: m.Features, Dependencies: authorDependencies(m.Dependencies), Lib: m.Lib, Bins: m.Bins, Examples: m.Examples}
	return toml.Marshal(d)
}
