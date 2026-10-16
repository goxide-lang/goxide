package project

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// SuppliedModules is an import-time audit, never a build or lock policy.
// Its native modules contain a pruned snapshot, not a complete upstream release.
type SuppliedModules struct {
	Direct             map[string]bool           `json:"direct_requirements,omitempty"`
	Sources            map[string]SuppliedSource `json:"source_choices,omitempty"`
	MainModules        []string                  `json:"main_modules"`
	Snapshot           *VendorSnapshot           `json:"input"`
	Selected           map[string]VendorModule   `json:"selected_modules"`
	Paths              map[string]string         `json:"local_modules"`
	Outputs            map[string][]string       `json:"files"`
	Metadata           map[string]string         `json:"generated_metadata"`
	PolicyFiles        map[string]string         `json:"original_metadata"`
	UpstreamDifference string                    `json:"upstream_difference"`
}

func InspectWorkVendor(root, version string, policy any) (*VendorSnapshot, error) {
	return inspectVendor(root, Manifest{GoVersion: version}, "go-work", Digest(policy))
}

// MaterializeSupplied validates every supplied import independently of host tags.
// A nil destination produces the same plan without publishing files.
func MaterializeSupplied(tool, root, destination, goVersion string, snapshot *VendorSnapshot, members map[string]string) (*SuppliedModules, error) {
	a := &SuppliedModules{MainModules: Keys(members), PolicyFiles: map[string]string{}, Snapshot: snapshot, Paths: map[string]string{}, Outputs: map[string][]string{}, Metadata: map[string]string{}, UpstreamDifference: "unknown (no upstream fetch)"}
	for _, name := range []string{"go.mod", "go.sum", "go.work", "go.work.sum", "vendor/modules.txt"} {
		b, e := os.ReadFile(filepath.Join(root, name))
		if e == nil {
			a.PolicyFiles[name] = string(b)
		} else if !os.IsNotExist(e) {
			return nil, e
		}
	}
	a.Direct = map[string]bool{}
	for _, module := range Keys(members) {
		policy, e := ReadMod(tool, members[module])
		if e != nil {
			return nil, e
		}
		for _, r := range policy.Require {
			if !r.Indirect {
				a.Direct[r.Path] = true
			}
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			b, e := os.ReadFile(filepath.Join(members[module], name))
			if e == nil {
				a.PolicyFiles["members/"+module+"/"+name] = string(b)
			} else if !os.IsNotExist(e) {
				return nil, e
			}
		}
	}
	modules := map[string]VendorModule{}
	headers := map[string][]VendorModule{}
	packages := map[string]string{}
	for _, m := range snapshot.Modules {
		if m.Version != "" && (m.Explicit || len(m.Packages) > 0) {
			headers[m.Path] = append(headers[m.Path], m)
		}
	}
	for _, module := range Keys(headers) {
		candidates := headers[module]
		chosen := candidates[0]
		if len(candidates) > 1 {
			var data []byte
			var e error
			if snapshot.Mode == "go-work" {
				data, e = NativeGo(tool, root, true, "list", "-mod=vendor", "-m", "-json", module)
			} else {
				data, e = goCommand(tool, root, true, nil, "list", "-mod=vendor", "-m", "-json", module)
			}
			if e != nil {
				return nil, e
			}
			var selected Selection
			if e = json.Unmarshal(data, &selected); e != nil {
				return nil, e
			}
			found := false
			for _, candidate := range candidates {
				if candidate.Version == selected.Version {
					chosen = candidate
					found = true
				} else if len(candidate.Packages) > 0 {
					return nil, fmt.Errorf("packages supplied for unselected module %s@%s", module, candidate.Version)
				}
			}
			if !found {
				return nil, fmt.Errorf("no supplied metadata for Go selected module %s@%s", module, selected.Version)
			}
		}
		modules[module] = chosen
		a.Paths[module] = "local-modules/" + Digest(module)[:24]
		for _, p := range chosen.Packages {
			packages[p] = module
		}
	}
	a.Selected = modules
	// Standard package identity must include non-host packages such as syscall/js.
	goroot, e := goCommand(tool, root, true, nil, "env", "GOROOT")
	if e != nil {
		return nil, e
	}
	stdRoot := filepath.Join(strings.TrimSpace(string(goroot)), "src")
	standard := map[string]bool{"C": true}
	e = filepath.WalkDir(stdRoot, func(file string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "cmd" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			rel, e := filepath.Rel(stdRoot, filepath.Dir(file))
			if e != nil {
				return e
			}
			standard[filepath.ToSlash(rel)] = true
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	outputs := map[string]string{}
	edges := map[string]map[string]bool{}
	contents := map[string][]byte{}
	modes := map[string]os.FileMode{}
	for _, rel := range Keys(snapshot.Files) {
		source := filepath.Join(root, "vendor", filepath.FromSlash(rel))
		data, e := os.ReadFile(source)
		if e != nil {
			return nil, e
		}
		info, e := os.Lstat(source)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() || Digest([]any{data, uint32(info.Mode().Perm())}) != snapshot.Files[rel] {
			return nil, fmt.Errorf("supplied source changed during migration: %s", rel)
		}
		if rel == "modules.txt" {
			continue
		}
		owner := ""
		longest := 0
		for m := range modules {
			if strings.HasPrefix(rel, m+"/") && len(m) > longest {
				owner = m
				longest = len(m)
			}
		}
		if declared := packages[path.Dir(rel)]; strings.HasSuffix(rel, ".go") && declared != "" && declared != owner {
			return nil, fmt.Errorf("supplied package ownership conflicts with nested module: %s", rel)
		}
		if owner != "" {
			a.Outputs[rel] = append(a.Outputs[rel], a.Paths[owner]+"/"+strings.TrimPrefix(rel, owner+"/"))
		}
		if vendorLicense(path.Base(rel)) {
			for _, m := range Keys(modules) {
				if strings.HasPrefix(m, path.Dir(rel)+"/") {
					a.Outputs[rel] = append(a.Outputs[rel], a.Paths[m]+"/supplied-licenses/"+Digest(rel)[:16]+"-"+path.Base(rel))
				}
			}
		}
		if len(a.Outputs[rel]) == 0 {
			return nil, fmt.Errorf("supplied file has no local module owner: %s", rel)
		}
		for _, output := range a.Outputs[rel] {
			if old, exists := outputs[output]; exists {
				return nil, fmt.Errorf("supplied output collision: %s and %s -> %s", old, rel, output)
			}
			outputs[output] = rel
		}
		if strings.HasSuffix(rel, ".go") {
			owner = packages[path.Dir(rel)]
			file, e := parser.ParseFile(token.NewFileSet(), rel, data, parser.ImportsOnly)
			if e != nil {
				return nil, e
			}
			if edges[owner] == nil {
				edges[owner] = map[string]bool{}
			}
			for _, imp := range file.Imports {
				p, e := strconv.Unquote(imp.Path.Value)
				if e != nil {
					return nil, e
				}
				if standard[p] {
					continue
				}
				child := packages[p]
				if child == "" {
					// Only explicitly listed original main modules may supply non-vendored packages.
					for m, dir := range members {
						if p == m || strings.HasPrefix(p, m+"/") {
							pkgDir := filepath.Join(dir, strings.TrimPrefix(strings.TrimPrefix(p, m), "/"))
							entries, err := os.ReadDir(pkgDir)
							if err == nil {
								for _, entry := range entries {
									if strings.HasSuffix(entry.Name(), ".go") {
										child = m
										break
									}
								}
							}
							if child != "" {
								break
							}
						}
					}
				}
				if child == "" {
					return nil, fmt.Errorf("supplied source %s imports missing package %s; provide local source explicitly (no download)", rel, p)
				}
				if child != owner {
					edges[owner][child] = true
				}
			}
		}
		contents[rel] = data
		modes[rel] = info.Mode().Perm()
	}
	for _, module := range Keys(modules) {
		mod := modules[module]
		v := mod.Go
		if v == "" {
			v = goVersion
		}
		m := Manifest{Module: module, GoVersion: v}
		for _, child := range Keys(edges[module]) {
			version := modules[child].Version
			if version == "" {
				version = Placeholder(child)
			}
			m.Native.Require = append(m.Native.Require, GoRequire{Module: child, Version: version})
		}
		data, e := ModuleText(m)
		if e != nil {
			return nil, e
		}
		a.Metadata[a.Paths[module]+"/go.mod"] = string(data)
	}
	if destination == "" {
		return a, nil
	}
	for _, name := range []string{"local-modules", "goxide-import.json"} {
		if _, e := os.Lstat(filepath.Join(destination, name)); !os.IsNotExist(e) {
			return nil, fmt.Errorf("migration output reserved path already exists: %s", name)
		}
	}
	for _, rel := range Keys(contents) {
		for _, output := range a.Outputs[rel] {
			target := filepath.Join(destination, filepath.FromSlash(output))
			if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
				return nil, e
			}
			if e := os.WriteFile(target, contents[rel], modes[rel]); e != nil {
				return nil, e
			}
			if e := os.Chmod(target, modes[rel]); e != nil {
				return nil, e
			}
		}
	}
	for _, rel := range Keys(a.Metadata) {
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return nil, e
		}
		if e := os.WriteFile(target, []byte(a.Metadata[rel]), 0644); e != nil {
			return nil, e
		}
	}
	data, e := json.MarshalIndent(a, "", "  ")
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(destination, "goxide-import.json"), append(data, '\n'), 0644); e != nil {
		return nil, e
	}
	return a, nil
}

// BindSupplied makes all selected snapshot identities explicit local sources.
// Root requirements retain direct/indirect roles at the supplied selected version;
// missing transitive labels are
// added for Go's pruned graph, without claiming an upstream go.mod reconstruction.
func BindSupplied(m *Manifest, a *SuppliedModules, relativeRoot string) {
	requires := map[string]bool{}
	selected := map[string]string{}
	for module, mod := range a.Selected {
		selected[module] = mod.Version
	}
	for i := range m.Native.Require {
		r := &m.Native.Require[i]
		if v := selected[r.Module]; v != "" {
			r.Version = v
		}
	}
	for _, r := range m.Native.Require {
		requires[r.Module] = true
	}
	// Old replacements for supplied modules are provenance, not author paths.
	// Unselected replacement-only tails likewise authorize no new local source.
	// Preserve workspace main-module policies for normal member materialization.
	mainModules := map[string]bool{}
	if a.Snapshot.Mode == "go-work" {
		for _, module := range a.MainModules {
			mainModules[module] = true
		}
	}
	replacements := m.Native.Replace[:0]
	for _, r := range m.Native.Replace {
		if a.Paths[r.Module] == "" && mainModules[r.Module] {
			replacements = append(replacements, r)
		}
	}
	m.Native.Replace = replacements
	for _, module := range Keys(a.Selected) {
		mod := a.Selected[module]
		if mod.Version == "" || a.Paths[mod.Path] == "" {
			continue
		}
		if !requires[mod.Path] {
			m.Native.Require = append(m.Native.Require, GoRequire{Module: mod.Path, Version: mod.Version, Indirect: true})
			requires[mod.Path] = true
		}
	}
	for _, module := range Keys(a.Paths) {
		if choice, ok := a.Sources[module]; ok && choice.Source == "remote" {
			m.Native.Replace = append(m.Native.Replace, GoReplace{Module: module, With: choice.Module, WithVersion: choice.Version})
			continue
		}
		m.Native.Replace = append(m.Native.Replace, GoReplace{Module: module, Path: filepath.ToSlash(filepath.Join(relativeRoot, a.Paths[module]))})
		r := &m.Native.Replace[len(m.Native.Replace)-1]
		if !strings.HasPrefix(r.Path, ".") {
			r.Path = "./" + r.Path
		}
	}
}

// SuppliedSourceChoices displays only evidence available in the original files.
// A module path is never guessed to be a Git URL. No remote request is made.
func SuppliedSourceChoices(a *SuppliedModules, offline bool) []string {
	var lines []string
	for _, module := range Keys(a.Selected) {
		m := a.Selected[module]
		remote, version := module, m.Version
		if m.Replace != "" {
			remote = m.Replace
			version = m.ReplaceVersion
		}
		status := "unknown (not remotely verified)"
		if offline {
			status = "unknown (--offline: not remotely verified)"
		}
		if version == "" {
			remote = "unconfirmed (original replacement is local)"
		}
		role := "indirect"
		if a.Direct[module] {
			role = "direct"
		}
		lines = append(lines, fmt.Sprintf("# source %s: role=%s; local=%s (default, supplied bytes); remote module=%s version=%s status=%s", module, role, a.Paths[module], remote, version, status))
	}
	return lines
}
