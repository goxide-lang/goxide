package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PackageConfig struct {
	Format    int    `toml:"format"`
	Module    string `toml:"module"`
	Go        string `toml:"go"`
	Toolchain string `toml:"toolchain,omitempty"`
}
type GoRequire struct {
	Module   string `json:"module" toml:"module"`
	Version  string `json:"version" toml:"version"`
	Indirect bool   `json:"indirect,omitempty" toml:"indirect,omitempty"`
}
type GoReplace struct {
	Module      string `json:"module" toml:"module"`
	Version     string `json:"version,omitempty" toml:"version,omitempty"`
	Path        string `json:"path,omitempty" toml:"path,omitempty"`
	With        string `json:"with,omitempty" toml:"with,omitempty"`
	WithVersion string `json:"with_version,omitempty" toml:"with_version,omitempty"`
}
type GoExclude struct {
	Module  string `json:"module" toml:"module"`
	Version string `json:"version" toml:"version"`
}
type GoDebug struct {
	Key   string `json:"key" toml:"key"`
	Value string `json:"value" toml:"value"`
}
type GoConfig struct {
	Godebug []GoDebug   `json:"godebug,omitempty" toml:"godebug,omitempty"`
	Require []GoRequire `json:"require,omitempty" toml:"require,omitempty"`
	Replace []GoReplace `json:"replace,omitempty" toml:"replace,omitempty"`
	Exclude []GoExclude `json:"exclude,omitempty" toml:"exclude,omitempty"`
}

func CheckAuthoring(root string) error {
	for _, name := range []string{"go.mod", "go.sum", "hgo.lock"} {
		if _, e := os.Lstat(filepath.Join(root, name)); e == nil {
			return fmt.Errorf("%s: goxide project cannot contain %s; use import-go staging migration and retain originals outside the migrated root", root, name)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	return CheckWorkspace(root)
}
func CheckWorkspace(root string) error {
	if v := os.Getenv("GOWORK"); v != "" && v != "off" {
		return fmt.Errorf("workspace migration unsupported: explicit GOWORK")
	}
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, e := os.Lstat(filepath.Join(dir, "go.work")); e == nil {
			return fmt.Errorf("workspace migration unsupported: go.work at %s", dir)
		} else if !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return nil
}

// ModuleText serializes only the accepted mapping; Go validates the result.
func ModuleText(m Manifest) ([]byte, error) {
	if m.Module == "" || m.GoVersion == "" {
		return nil, fmt.Errorf("package.module and package.go are required")
	}
	safe := func(s string) bool { return !strings.ContainsAny(s, "\x00\r\n") }
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n", strconv.Quote(m.Module), m.GoVersion)
	if m.Toolchain != "" {
		fmt.Fprintf(&b, "toolchain %s\n", m.Toolchain)
	}
	if !safe(m.GoVersion) || !safe(m.Toolchain) {
		return nil, fmt.Errorf("invalid Go version/toolchain")
	}
	for _, debug := range m.Native.Godebug {
		if debug.Key == "" || strings.ContainsAny(debug.Key+debug.Value, " \t\r\n\x00=()\"/\\#;") {
			return nil, fmt.Errorf("invalid godebug setting")
		}
		fmt.Fprintf(&b, "godebug %s=%s\n", debug.Key, debug.Value)
	}
	seen := map[string]bool{}
	for _, r := range m.Native.Require {
		if r.Module == "" || r.Version == "" || !safe(r.Version) || seen[r.Module] {
			return nil, fmt.Errorf("invalid/duplicate go.require %s", r.Module)
		}
		seen[r.Module] = true
		fmt.Fprintf(&b, "require %s %s", strconv.Quote(r.Module), r.Version)
		if r.Indirect {
			b.WriteString(" // indirect")
		}
		b.WriteByte('\n')
	}
	seen = map[string]bool{}
	for _, r := range m.Native.Replace {
		key := r.Module + "@" + r.Version
		if r.Module == "" || !safe(r.Version) || seen[key] || (r.Path == "") == (r.With == "") || (r.With != "" && r.WithVersion == "") || (r.Path != "" && r.WithVersion != "") || !safe(r.WithVersion) {
			return nil, fmt.Errorf("invalid/duplicate go.replace %s", r.Module)
		}
		seen[key] = true
		fmt.Fprintf(&b, "replace %s", strconv.Quote(r.Module))
		if r.Version != "" {
			fmt.Fprintf(&b, " %s", r.Version)
		}
		if r.Path != "" {
			if !filepath.IsAbs(r.Path) && !strings.HasPrefix(r.Path, ".") {
				return nil, fmt.Errorf("go.replace path must be explicit relative or absolute")
			}
			fmt.Fprintf(&b, " => %s\n", strconv.Quote(r.Path))
		} else {
			fmt.Fprintf(&b, " => %s %s\n", strconv.Quote(r.With), r.WithVersion)
		}
	}
	seen = map[string]bool{}
	for _, x := range m.Native.Exclude {
		key := x.Module + "@" + x.Version
		if x.Module == "" || x.Version == "" || !safe(x.Version) || seen[key] {
			return nil, fmt.Errorf("invalid/duplicate go.exclude %s", x.Module)
		}
		seen[key] = true
		fmt.Fprintf(&b, "exclude %s %s\n", strconv.Quote(x.Module), x.Version)
	}
	return []byte(b.String()), nil
}

// ImportMod uses Go's parser. Unsupported directives are never silently lost.
func ImportMod(tool, root string) (Manifest, error)          { return importMod(tool, root, false) }
func ImportWorkspaceMod(tool, root string) (Manifest, error) { return importMod(tool, root, true) }
func importMod(tool, root string, debug bool) (Manifest, error) {
	var m Manifest
	info, err := os.Lstat(filepath.Join(root, "go.mod"))
	if err != nil || !info.Mode().IsRegular() {
		return m, fmt.Errorf("import-go root must contain its own regular go.mod")
	}
	b, e := goCommand(tool, root, true, "mod", "edit", "-json")
	if e != nil {
		return m, e
	}
	var raw map[string]json.RawMessage
	if e = json.Unmarshal(b, &raw); e != nil {
		return m, e
	}
	allowed := map[string]bool{"Module": true, "Go": true, "Toolchain": true, "Require": true, "Replace": true, "Exclude": true}
	if debug {
		allowed["GoDebug"] = true
	}
	for k, v := range raw {
		if !allowed[k] && string(v) != "null" && string(v) != "[]" && string(v) != "\"\"" {
			return m, fmt.Errorf("go.mod directive %s unsupported by import-go; original files unchanged", k)
		}
	}
	var mod struct {
		Module        struct{ Path, Version, Deprecated string }
		Go, Toolchain string
		Require       []struct {
			Path, Version string
			Indirect      bool
		}
		Godebug []GoDebug
		Replace []ModReplace
		Exclude []ModuleVersion
	}
	if e = json.Unmarshal(b, &mod); e != nil {
		return m, e
	}
	if mod.Module.Deprecated != "" {
		return m, fmt.Errorf("go.mod module deprecation unsupported by import-go; original files unchanged")
	}
	m.Version = 2
	m.Module = mod.Module.Path
	m.GoVersion = mod.Go
	m.Toolchain = mod.Toolchain
	m.Native.Godebug = mod.Godebug
	for _, r := range mod.Require {
		m.Native.Require = append(m.Native.Require, GoRequire{r.Path, r.Version, r.Indirect})
	}
	for _, r := range mod.Replace {
		x := GoReplace{Module: r.Old.Path, Version: r.Old.Version}
		if r.New.Version == "" {
			x.Path = r.New.Path
		} else {
			x.With = r.New.Path
			x.WithVersion = r.New.Version
		}
		m.Native.Replace = append(m.Native.Replace, x)
	}
	for _, x := range mod.Exclude {
		m.Native.Exclude = append(m.Native.Exclude, GoExclude{x.Path, x.Version})
	}
	_, e = ModuleText(m)
	return m, e
}

func sameTransport(a, b ModFile) bool {
	// Go's JSON parser preserves directive order; compare normalized copies.
	normalize := func(m ModFile) ModFile {
		m.Require = append([]ModRequire(nil), m.Require...)
		m.Replace = append([]ModReplace(nil), m.Replace...)
		m.Exclude = append([]ModuleVersion(nil), m.Exclude...)
		sort.Slice(m.Require, func(i, j int) bool { return Digest(m.Require[i]) < Digest(m.Require[j]) })
		sort.Slice(m.Replace, func(i, j int) bool { return Digest(m.Replace[i]) < Digest(m.Replace[j]) })
		sort.Slice(m.Exclude, func(i, j int) bool { return Digest(m.Exclude[i]) < Digest(m.Exclude[j]) })
		return m
	}
	x, _ := json.Marshal(normalize(a))
	y, _ := json.Marshal(normalize(b))
	return bytes.Equal(x, y)
}
