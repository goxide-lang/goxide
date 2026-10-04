package project

import (
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"go/version"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type VendorConfig struct {
	Mode string `json:"mode" toml:"mode"`
}
type VendorModule struct {
	Path           string   `json:"path"`
	Version        string   `json:"version,omitempty"`
	Replace        string   `json:"replace,omitempty"`
	ReplaceVersion string   `json:"replace_version,omitempty"`
	Go             string   `json:"go,omitempty"`
	Explicit       bool     `json:"explicit,omitempty"`
	Packages       []string `json:"packages,omitempty"`
}
type VendorSnapshot struct {
	Mode    string            `json:"mode"`
	Policy  string            `json:"policy"`
	Digest  string            `json:"digest"`
	Files   map[string]string `json:"files"`
	Modules []VendorModule    `json:"modules"`
}

func vendorPath(s string) bool {
	return s != "" && s != "." && !strings.HasPrefix(s, "/") && path.Clean(s) == s && !strings.ContainsAny(s, "\\:@\x00\r\n\t ") && !strings.HasPrefix(s, "../")
}
func vendorModules(data []byte) ([]VendorModule, error) { return vendorModulesMode(data, false) }
func vendorModulesMode(data []byte, workspace bool) ([]VendorModule, error) {
	if workspace && !strings.HasPrefix(string(data), "## workspace\n") {
		return nil, fmt.Errorf("workspace vendor requires ## workspace marker")
	}
	var modules []VendorModule
	current := -1
	headers := map[string]bool{}
	packages := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		fail := func() ([]VendorModule, error) {
			return nil, fmt.Errorf("vendor/modules.txt:%d: unsupported or malformed entry %q", i+1, line)
		}
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) != line {
			return fail()
		}
		if strings.HasPrefix(line, "## ") {
			if line == "## workspace" {
				if workspace && i == 0 {
					continue
				}
				return nil, fmt.Errorf("vendor/modules.txt:%d: workspace vendor is unsupported", i+1)
			}
			if current < 0 {
				return fail()
			}
			for _, value := range strings.Split(line[3:], ";") {
				value = strings.TrimSpace(value)
				switch {
				case value == "explicit":
					if modules[current].Explicit {
						return fail()
					}
					modules[current].Explicit = true
				case strings.HasPrefix(value, "go "):
					v := strings.TrimPrefix(value, "go ")
					if !version.IsValid("go"+v) || modules[current].Go != "" {
						return fail()
					}
					modules[current].Go = v
				default:
					return fail()
				}
			}
			continue
		}
		if strings.HasPrefix(line, "# ") {
			f := strings.Fields(line[2:])
			if len(f) < 2 || !vendorPath(f[0]) {
				return fail()
			}
			m := VendorModule{Path: f[0]}
			f = f[1:]
			if f[0] != "=>" {
				m.Version = f[0]
				if !strings.HasPrefix(m.Version, "v") {
					return fail()
				}
				f = f[1:]
			}
			if len(f) > 0 {
				if (len(f) != 2 && len(f) != 3) || f[0] != "=>" {
					return fail()
				}
				m.Replace = f[1]
				if len(f) == 3 {
					m.ReplaceVersion = f[2]
					if !vendorPath(m.Replace) || !strings.HasPrefix(m.ReplaceVersion, "v") {
						return fail()
					}
				}
			}
			key := m.Path + "@" + m.Version
			if headers[key] {
				return fail()
			}
			headers[key] = true
			modules = append(modules, m)
			current = len(modules) - 1
			continue
		}
		if current < 0 || !vendorPath(line) || packages[line] || !(line == modules[current].Path || strings.HasPrefix(line, modules[current].Path+"/")) || modules[current].Version == "" {
			return fail()
		}
		packages[line] = true
		modules[current].Packages = append(modules[current].Packages, line)
	}
	for _, m := range modules {
		if len(m.Packages) > 0 && m.Go == "" {
			return nil, fmt.Errorf("vendor/modules.txt: module %s lacks modern Go version metadata", m.Path)
		}
	}
	return modules, nil
}
func vendorLicense(name string) bool {
	for _, prefix := range []string{"AUTHORS", "CONTRIBUTORS", "COPYLEFT", "COPYING", "COPYRIGHT", "LEGAL", "LICENSE", "NOTICE", "PATENTS"} {
		if strings.HasPrefix(strings.ToUpper(name), prefix) {
			return true
		}
	}
	return false
}

// InspectVendor records actual supplied bytes, never proxy/checksum provenance.
// It does not fetch modules or reconstruct missing package/module metadata.
func InspectVendor(root string, m Manifest) (*VendorSnapshot, error) {
	return inspectVendor(root, m, "go-mod", Digest([]any{m.Module, m.GoVersion, m.Toolchain, m.Native}))
}
func inspectVendor(root string, m Manifest, mode, policy string) (*VendorSnapshot, error) {
	if version.Compare("go"+m.GoVersion, "go1.17") < 0 {
		return nil, fmt.Errorf("vendor migration requires go >= 1.17 with modern modules.txt metadata")
	}
	dir := filepath.Join(root, "vendor")
	if e := NoSymlinks(dir); e != nil {
		return nil, e
	}
	info, err := os.Lstat(filepath.Join(dir, "modules.txt"))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("vendor/modules.txt must be a regular file (no symlink)")
	}
	data, e := os.ReadFile(filepath.Join(dir, "modules.txt"))
	if e != nil {
		return nil, e
	}
	modules, e := vendorModulesMode(data, mode == "go-work")
	if e != nil {
		return nil, e
	}
	pkgs := map[string]bool{}
	modulePaths := map[string]bool{}
	for _, mod := range modules {
		modulePaths[mod.Path] = true
		for _, pkg := range mod.Packages {
			pkgs[pkg] = true
		}
	}
	snap := &VendorSnapshot{Mode: mode, Policy: policy, Files: map[string]string{}, Modules: modules}
	sources := map[string]bool{}
	e = filepath.WalkDir(dir, func(file string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := naming.Path(file); err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(dir, file)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == "." || strings.HasPrefix(rel, "/") || path.Clean(rel) != rel || strings.ContainsAny(rel, "\\:\x00\r\n") || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("vendor: invalid path %s", rel)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("vendor: symlink forbidden: %s", rel)
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == ".goxide" || d.Name() == ".goxide-migration" {
				return fmt.Errorf("vendor: reserved directory %s", rel)
			}
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("vendor: special file forbidden: %s", rel)
		}
		name := path.Base(rel)
		parent := path.Dir(rel)
		if name == "go.mod" || name == "go.sum" || name == ManifestName || name == LegacyManifestName || name == "goxide.lock" || strings.HasSuffix(name, ".gox") {
			return fmt.Errorf("vendor: nested module/enhanced source unsupported: %s", rel)
		}
		owned := rel == "modules.txt"
		for pkg := range pkgs {
			if rel == pkg || strings.HasPrefix(rel, pkg+"/") {
				owned = true
			}
		}
		if vendorLicense(name) {
			for mod := range modulePaths {
				if parent == mod || strings.HasPrefix(mod, parent+"/") || strings.HasPrefix(parent, mod+"/") {
					owned = true
				}
			}
		}
		if !owned {
			return fmt.Errorf("vendor: unowned file %s", rel)
		}
		if strings.HasSuffix(name, ".go") {
			if !pkgs[parent] {
				return fmt.Errorf("vendor: undeclared Go package file %s", rel)
			}
			sources[parent] = true
		}
		b, e := os.ReadFile(file)
		if e != nil {
			return e
		}
		snap.Files[rel] = Digest([]any{b, uint32(info.Mode().Perm())})
		return nil
	})
	if e != nil {
		return nil, e
	}
	for pkg := range pkgs {
		if !sources[pkg] {
			return nil, fmt.Errorf("vendor: listed package %s has no supplied Go source", pkg)
		}
	}
	snap.Digest = Digest([]any{snap.Mode, snap.Policy, snap.Files, snap.Modules})
	return snap, nil
}

func VendorInventory(s *VendorSnapshot) []string {
	var lines []string
	for _, m := range s.Modules {
		lines = append(lines, fmt.Sprintf("# vendor module %s@%s replace=%s@%s explicit=%t go=%s packages=%s", m.Path, m.Version, m.Replace, m.ReplaceVersion, m.Explicit, m.Go, strings.Join(m.Packages, ",")))
	}
	for name, hash := range s.Files {
		kind := "file"
		if vendorLicense(path.Base(name)) {
			kind = "license"
		}
		lines = append(lines, fmt.Sprintf("# vendor %s %s %s", kind, name, hash))
	}
	sort.Strings(lines)
	return lines
}
