package project

import (
	"fmt"
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
func vendorModules(data []byte) ([]VendorModule, error) {
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
	modules, e := vendorModules(data)
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
	snap := &VendorSnapshot{Mode: "go-mod", Policy: Digest([]any{m.Module, m.GoVersion, m.Toolchain, m.Native}), Files: map[string]string{}, Modules: modules}
	sources := map[string]bool{}
	e = filepath.WalkDir(dir, func(file string, d os.DirEntry, e error) error {
		if e != nil {
			return e
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
			if d.Name() == ".git" || d.Name() == ".hgo" || d.Name() == ".goxide-migration" {
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
		if name == "go.mod" || name == "go.sum" || name == ManifestName || name == LegacyManifestName || name == "goxide.lock" || strings.HasSuffix(name, ".hgo") {
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

// PrepareVendor copies the verified supplied view into an owned generated module.
// Go, not the modules.txt reader, checks require/explicit/replace consistency.
func PrepareVendor(tool, source, destination string, m Manifest, expected *VendorSnapshot) error {
	actual, e := InspectVendor(source, m)
	if e != nil {
		return e
	}
	if expected == nil || actual.Digest != expected.Digest {
		return fmt.Errorf("vendor source changed during build")
	}
	for _, rel := range Keys(actual.Files) {
		src := filepath.Join(source, "vendor", filepath.FromSlash(rel))
		dst := filepath.Join(destination, "vendor", filepath.FromSlash(rel))
		b, e := os.ReadFile(src)
		if e != nil {
			return e
		}
		info, e := os.Stat(src)
		if e != nil {
			return e
		}
		if Digest([]any{b, uint32(info.Mode().Perm())}) != actual.Files[rel] {
			return fmt.Errorf("vendor source changed: %s", rel)
		}
		if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
			return e
		}
		if _, err := os.Lstat(dst); err == nil {
			if e = os.Chmod(dst, info.Mode().Perm()|0200); e != nil {
				return e
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if e = os.WriteFile(dst, b, info.Mode().Perm()); e != nil {
			return e
		}
		if e = os.Chmod(dst, info.Mode().Perm()); e != nil {
			return e
		}
	}
	_, e = goCommand(tool, destination, true, "list", "-mod=vendor", "-m", "-json")
	return e
}
func (g *Graph) planVendor(n *Node, requested []string, defaults bool) (*Graph, error) {
	if len(n.Manifest.Dependencies) > 0 {
		return nil, fmt.Errorf("vendor mode cannot mix enhanced dependencies; supplied vendor is a native Go package view")
	}
	features, _, e := Resolve(n.Manifest, requested, defaults)
	if e != nil {
		return nil, e
	}
	n.Features = features
	snap, e := InspectVendor(n.Root, n.Manifest)
	if e != nil {
		return nil, e
	}
	if g.Locked && (g.Lock.Vendor == nil || g.Lock.Vendor.Digest != snap.Digest) {
		return nil, fmt.Errorf("--locked: vendor contents or policy changed")
	}
	dir, e := os.MkdirTemp(g.WorkRoot, "vendor-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(dir)
	if e = os.WriteFile(filepath.Join(dir, "go.mod"), n.ModBytes, 0644); e != nil {
		return nil, e
	}
	if e = PrepareVendor(g.Tool, n.Root, dir, n.Manifest, snap); e != nil {
		return nil, e
	}
	g.Lock.Vendor = snap
	g.Lock.PlanningSums = ""
	g.Root = n
	g.Nodes = []*Node{n}
	g.RootMod = n.Mod
	g.Plan = map[string]Selection{}
	// These versions describe the supplied package view, not a newly solved MVS graph.
	for _, mod := range snap.Modules {
		if mod.Version != "" && (mod.Explicit || len(mod.Packages) > 0) {
			s := Selection{Path: mod.Path, Version: mod.Version}
			if mod.Replace != "" {
				s.Replace = &Selection{Path: mod.Replace, Version: mod.ReplaceVersion}
			}
			g.Plan[mod.Path] = s
		}
	}
	g.PlanID = Digest([]any{"go-mod-vendor-v1", snap.Digest})
	return g, nil
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
