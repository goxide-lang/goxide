// Package rootconfig decodes explicit source-language dependency roots. It
// does not resolve module versions, inspect directories, or load Go packages.
package rootconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

const Filename = "goxide.roots.toml"

// Mapping declares exact packages belonging to one selected module or the Go
// toolchain. A missing Package is a grouping namespace, never a module-path
// shorthand. Namespace keys are explicit ::-separated identifier segments.
type Mapping struct {
	Module     string            `toml:"module,omitempty" json:"module,omitempty"`
	Package    string            `toml:"package,omitempty" json:"package,omitempty"`
	Source     string            `toml:"source,omitempty" json:"source,omitempty"`
	Namespaces map[string]string `toml:"namespaces,omitempty" json:"namespaces,omitempty"`
}

func Parse(filename string, data []byte) (paths.Roots, error) {
	var document struct {
		Roots map[string]Mapping `toml:"roots"`
	}
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&document); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	// Struct decoding accepts case-insensitive field names. The mapping format
	// does not: spelling and source-kind restrictions are part of its contract.
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	for key := range raw {
		if key != "roots" {
			return nil, fmt.Errorf("%s: unknown field %q (keys are case-sensitive)", filename, key)
		}
	}
	declarations, _ := raw["roots"].(map[string]any)
	for _, alias := range sortedKeys(declarations) {
		fields, _ := declarations[alias].(map[string]any)
		for _, key := range sortedKeys(fields) {
			switch key {
			case "module", "package", "source", "namespaces":
			default:
				return nil, fmt.Errorf("%s: root %s: unknown field %q (keys are case-sensitive)", filename, alias, key)
			}
			if fields["source"] == "toolchain" && key == "module" {
				return nil, fmt.Errorf("%s: root %s: toolchain source must not declare a module", filename, alias)
			}
		}
	}
	roots, err := Build(document.Roots)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	return roots, nil
}

func Build(mappings map[string]Mapping) (paths.Roots, error) {
	roots := paths.Roots{}
	for _, name := range sortedKeys(mappings) {
		mapping := mappings[name]
		if !identifier(name) {
			return nil, fmt.Errorf("invalid dependency root %q", name)
		}
		source := mapping.Source
		if source == "" {
			source = "module"
		}
		switch source {
		case "module":
			if !packagePath(mapping.Module) {
				return nil, fmt.Errorf("root %s: module source requires an explicit module path", name)
			}
		case "toolchain":
			if mapping.Module != "" {
				return nil, fmt.Errorf("root %s: toolchain source must not declare a module", name)
			}
		default:
			return nil, fmt.Errorf("root %s: unsupported source %q", name, source)
		}
		if mapping.Package != "" && !packagePath(mapping.Package) {
			return nil, fmt.Errorf("root %s: invalid exact package path %q", name, mapping.Package)
		}
		namespace := paths.Namespace{PackagePath: mapping.Package, ModulePath: mapping.Module, Source: source, Children: map[string]paths.Namespace{}}
		for _, key := range sortedKeys(mapping.Namespaces) {
			segments := strings.Split(key, "::")
			for _, segment := range segments {
				if !identifier(segment) {
					return nil, fmt.Errorf("root %s: invalid namespace path %q", name, key)
				}
			}
			if !packagePath(mapping.Namespaces[key]) {
				return nil, fmt.Errorf("root %s: namespace %s requires an exact package path", name, key)
			}
			namespace = insert(namespace, segments, mapping.Namespaces[key])
		}
		roots[name] = namespace
	}
	return roots, nil
}

func insert(parent paths.Namespace, segments []string, packageName string) paths.Namespace {
	if len(segments) == 0 {
		parent.PackagePath = packageName
		return parent
	}
	if parent.Children == nil {
		parent.Children = map[string]paths.Namespace{}
	}
	child, ok := parent.Children[segments[0]]
	if !ok {
		child = paths.Namespace{ModulePath: parent.ModulePath, Source: parent.Source}
	}
	parent.Children[segments[0]] = insert(child, segments[1:], packageName)
	return parent
}

func identifier(name string) bool { return paths.ValidIdentifier(name) }

func packagePath(path string) bool {
	if path == "" {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~+", c) {
				continue
			}
			return false
		}
	}
	return true
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Read reads one explicitly selected mapping file. Missing files are errors.
func Read(filename string) (paths.Roots, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Parse(filename, data)
}

// Load reads only the mapping file at a known module root. A missing file
// returns nil; an explicitly empty file returns an empty, non-nil context.
func Load(directory string) (paths.Roots, error) {
	filename := filepath.Join(directory, Filename)
	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return Parse(filename, data)
}

// Discover finds the nearest mapping for a module-free source. It never
// inherits a parent module's mapping across a go.mod boundary.
func Discover(directory string) (paths.Roots, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	for {
		roots, err := Load(directory)
		if err != nil || roots != nil {
			return roots, err
		}
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return nil, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return nil, nil
		}
		directory = parent
	}
}
