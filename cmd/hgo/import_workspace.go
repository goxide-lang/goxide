package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/project"
	"github.com/pelletier/go-toml/v2"
)

type nativePackage struct {
	ImportPath, Name, Dir, ForTest     string
	Imports, TestImports, XTestImports []string
	Module                             *struct {
		Path, Dir string
		Main      bool
	}
	Error      *struct{ Err string }
	DepsErrors []struct{ Err string }
}

func nativePackages(tool, view string, patterns []string, vendor, offline, race bool) ([]nativePackage, error) {
	mode := "-mod=readonly"
	if vendor {
		mode = "-mod=vendor"
	}
	args := []string{"list", "-buildvcs=false", mode, "-deps", "-test", "-json"}
	if race {
		args = append(args, "-race")
	}
	args = append(args, patterns...)
	b, e := project.NativeGo(tool, view, offline, args...)
	if e != nil {
		return nil, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var packages []nativePackage
	for {
		var p nativePackage
		e = d.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if p.Error != nil {
			return nil, fmt.Errorf("Go package %s: %s", p.ImportPath, p.Error.Err)
		}
		if len(p.DepsErrors) > 0 {
			return nil, fmt.Errorf("Go package %s: %s", p.ImportPath, p.DepsErrors[0].Err)
		}
		packages = append(packages, p)
	}
	return packages, nil
}
func writeWorkspaceCandidate(root string, c project.WorkspaceConfig, manifests map[string]project.Manifest, edges map[string]map[string]bool) error {
	c.Dependencies = map[string]project.Dependency{}
	alias := map[string]string{}
	for i, module := range project.Keys(manifests) {
		alias[module] = fmt.Sprintf("member%d", i)
		c.Dependencies[alias[module]] = project.Dependency{Module: module, Path: c.Members[i]}
	}
	b, e := toml.Marshal(map[string]any{"workspace": c})
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(root, project.ManifestName), b, 0644); e != nil {
		return e
	}
	for i, module := range project.Keys(manifests) {
		b, e := project.Marshal(manifests[module])
		if e != nil {
			return e
		}
		var raw map[string]any
		if e = toml.Unmarshal(b, &raw); e != nil {
			return e
		}
		deps := map[string]any{}
		for child := range edges[module] {
			deps[alias[child]] = map[string]any{"workspace": true}
		}
		if len(deps) > 0 {
			raw["dependencies"] = deps
		}
		b, e = toml.Marshal(raw)
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(root, c.Members[i], project.ManifestName), b, 0644); e != nil {
			return e
		}
	}
	return nil
}
func rejectImportConfig(root string) error {
	for _, name := range []string{project.ManifestName, project.LegacyManifestName, "goxide.lock", "hgo.lock"} {
		if _, e := os.Lstat(filepath.Join(root, name)); e == nil {
			return fmt.Errorf("workspace import refuses existing %s at %s", name, root)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
func executeImportWorkspace(req ImportRequest, out, stderr io.Writer) error {
	if req.Root == "" || req.Lib != "" || len(req.Bins) > 0 {
		return fmt.Errorf("workspace import requires ROOT and uses --member-lib/--member-bin")
	}
	if !req.Plan && req.Out == "" {
		req.Plan = true
	}
	if req.Plan && req.Out != "" {
		return fmt.Errorf("choose --plan or --out")
	}
	root, e := filepath.Abs(req.Root)
	if e != nil {
		return e
	}
	if e = project.NoSymlinks(root); e != nil {
		return e
	}
	if e = rejectImportConfig(root); e != nil {
		return e
	}
	if e = checkDirectoryCompiler(req.GoTool); e != nil {
		return e
	}
	work, e := project.ReadGoWork(req.GoTool, root)
	if e != nil {
		return e
	}
	config, e := project.NativeConfiguration(req.GoTool)
	if e != nil {
		return e
	}
	policy := project.WorkPolicy(work)
	policy.Configuration = config
	vendor := false
	if _, e = os.Lstat(filepath.Join(root, "vendor")); e == nil {
		vendor = true
	} else if !os.IsNotExist(e) {
		return e
	}
	source := map[string]string{}
	manifests := map[string]project.Manifest{}
	for _, use := range work.Use {
		dir := use.DiskPath
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		dir = filepath.Clean(dir)
		if e = project.NoSymlinks(dir); e != nil {
			return e
		}
		if e = rejectImportConfig(dir); e != nil {
			return e
		}
		m, e := project.ImportWorkspaceMod(req.GoTool, dir)
		if e != nil {
			return e
		}
		if _, ok := source[m.Module]; ok {
			return fmt.Errorf("duplicate workspace module %s", m.Module)
		}
		source[m.Module] = dir
		manifests[m.Module] = m
	}
	// Stage is always outside the source trees. Analysis may write sums here.
	stageParent := ""
	final := ""
	if !req.Plan {
		final, e = filepath.Abs(req.Out)
		if e != nil {
			return e
		}
		if e = project.NoSymlinks(final); e != nil {
			return e
		}
		if e = project.RejectManagedOutput(final); e != nil {
			return e
		}
		if within(root, final) {
			return fmt.Errorf("workspace destination must be outside original root")
		}
		for _, dir := range source {
			if within(dir, final) {
				return fmt.Errorf("workspace destination overlaps source member")
			}
		}
		if _, e = os.Lstat(final); !os.IsNotExist(e) {
			return fmt.Errorf("workspace destination must not exist")
		}
		stageParent = filepath.Dir(final)
	}
	stage, e := os.MkdirTemp(stageParent, ".goxide-work-import-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	c := project.WorkspaceConfig{Format: 1, Go: policy}
	if vendor {
		c.Vendor = &project.VendorConfig{Mode: "go-work"}
	}
	seed := &project.NativeWorkspaceLock{Sums: map[string]string{}}
	readOptional := func(path string) (string, error) {
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			return "", nil
		}
		return string(b), e
	}
	seed.WorkSum, e = readOptional(filepath.Join(root, "go.work.sum"))
	if e != nil {
		return e
	}
	for i, module := range project.Keys(manifests) {
		rel := fmt.Sprintf("members/m%d", i)
		c.Members = append(c.Members, rel)
		dir := source[module]
		m := manifests[module]
		if e = project.CopyNativeSource(dir, filepath.Join(stage, rel), root, m.Module); e != nil {
			return e
		}
		for j := range m.Native.Replace {
			r := &m.Native.Replace[j]
			if r.Path == "" {
				continue
			}
			absolute := r.Path
			if !filepath.IsAbs(absolute) {
				absolute = filepath.Join(dir, absolute)
			}
			if vendor && !filepath.IsAbs(r.Path) {
				coordinate, err := filepath.Rel(root, absolute)
				if err != nil {
					return err
				}
				r.Path, err = filepath.Rel(rel, coordinate)
				if err != nil {
					return err
				}
				if !strings.HasPrefix(r.Path, ".") {
					r.Path = "./" + r.Path
				}
			} else if !vendor {
				r.Path = absolute
			}
		}
		manifests[module] = m
		seed.Sums[module], e = readOptional(filepath.Join(dir, "go.sum"))
		if e != nil {
			return e
		}
	}
	if !vendor {
		for i := range c.Go.Replace {
			r := &c.Go.Replace[i]
			if r.Path != "" && !filepath.IsAbs(r.Path) {
				r.Path = filepath.Join(root, r.Path)
			}
		}
	}
	edges := map[string]map[string]bool{}
	if e = writeWorkspaceCandidate(stage, c, manifests, edges); e != nil {
		return e
	}
	// Vendor is copied verbatim by a dedicated file walker; it is not regenerated.
	if vendor {
		if e = copyImportVendor(filepath.Join(root, "vendor"), filepath.Join(stage, "vendor")); e != nil {
			return e
		}
	}
	w, e := project.DiscoverWorkspace(stage)
	if e != nil {
		return e
	}
	if vendor {
		seed.Vendor, e = project.InspectWorkspaceVendor(w)
		if e != nil {
			return e
		}
	}
	view, e := os.MkdirTemp("", "goxide-native-analysis-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(view)
	all := map[string]bool{}
	for module := range manifests {
		all[module] = true
	}
	dirs, e := project.NativeView(w, view, all, seed)
	if e != nil {
		return e
	}
	patterns := []string{}
	for _, rel := range c.Members {
		patterns = append(patterns, "./"+filepath.ToSlash(rel)+"/...")
	}
	packages, e := nativePackages(req.GoTool, view, patterns, vendor, req.Offline, false)
	if e != nil {
		return e
	}
	if config["CGO_ENABLED"] == "1" {
		racePackages, err := nativePackages(req.GoTool, view, patterns, vendor, req.Offline, true)
		if err != nil {
			return err
		}
		packages = append(packages, racePackages...)
	}
	owners := map[string]map[string]bool{}
	for _, p := range packages {
		if p.Module != nil {
			if owners[p.ImportPath] == nil {
				owners[p.ImportPath] = map[string]bool{}
			}
			owners[p.ImportPath][p.Module.Path] = true
		}
	}
	targetSeen := map[string]bool{}
	for _, p := range packages {
		if p.Module == nil || !all[p.Module.Path] || p.ForTest != "" {
			continue
		}
		module := p.Module.Path
		rel, err := filepath.Rel(dirs[module], p.Dir)
		if err != nil {
			return err
		}
		expectedImport := module
		if rel != "." {
			expectedImport += "/" + filepath.ToSlash(rel)
		}
		// Exclude synthesized test packages by identity, not by a suffix that
		// a real module or package may legally use.
		if p.ImportPath != expectedImport {
			continue
		}
		if edges[module] == nil {
			edges[module] = map[string]bool{}
		}
		imports := append(append(append([]string{}, p.Imports...), p.TestImports...), p.XTestImports...)
		for _, imp := range imports {
			for child := range owners[imp] {
				if child != module && all[child] {
					edges[module][child] = true
				}
			}
		}
		m := manifests[module]
		if targetSeen[module+"/"+rel] {
			continue
		}
		targetSeen[module+"/"+rel] = true
		if p.Name == "main" {
			name := filepath.Base(rel)
			if rel == "." {
				name = filepath.Base(module)
			}
			m.Bins = append(m.Bins, project.Target{Name: name, Path: filepath.ToSlash(rel)})
		} else if rel == "." {
			m.Lib = &project.Target{Name: "lib", Path: "."}
		}
		manifests[module] = m
	}
	for _, raw := range req.MemberLibs {
		module, path, ok := strings.Cut(raw, "=")
		m, exists := manifests[module]
		if !ok || !exists {
			return fmt.Errorf("--member-lib requires MODULE=PATH for a use member")
		}
		m.Lib = &project.Target{Name: "lib", Path: path}
		manifests[module] = m
	}
	overridden := map[string]bool{}
	for _, raw := range req.MemberBins {
		left, path, ok := strings.Cut(raw, "=")
		module, name, qualified := strings.Cut(left, "::")
		m, exists := manifests[module]
		if !ok || !qualified || !exists {
			return fmt.Errorf("--member-bin requires MODULE::NAME=PATH")
		}
		if !overridden[module] {
			m.Bins = nil
			overridden[module] = true
		}
		m.Bins = append(m.Bins, project.Target{Name: name, Path: path})
		manifests[module] = m
	}
	for module, m := range manifests {
		if m.Lib == nil && len(m.Bins) == 0 {
			return fmt.Errorf("member %s has no inferred target; use --member-lib or --member-bin", module)
		}
		sort.Slice(m.Bins, func(i, j int) bool { return m.Bins[i].Name < m.Bins[j].Name })
		manifests[module] = m
	}
	if e = writeWorkspaceCandidate(stage, c, manifests, edges); e != nil {
		return e
	}
	w, e = project.DiscoverWorkspace(stage)
	if e != nil {
		return e
	}
	for module := range manifests {
		if _, e = project.NativeClosure(w, module); e != nil {
			return e
		}
	}
	seed.Input, e = project.NativeInput(w)
	if e != nil {
		return e
	}
	if vendor {
		seed.Vendor, e = project.InspectWorkspaceVendor(w)
		if e != nil {
			return e
		}
	}
	if e = project.CaptureNativeSums(w, view, seed); e != nil {
		return e
	}
	if req.Plan {
		fmt.Fprintln(out, "# Plan only: original files unchanged; native workspace, bounded Go configuration; no install scripts.")
		for _, module := range project.Keys(manifests) {
			fmt.Fprintf(out, "# member %s: %s -> %s\n", module, source[module], strings.TrimPrefix(w.Members[module], stage+string(filepath.Separator)))
			for _, child := range project.Keys(edges[module]) {
				fmt.Fprintf(out, "# explicit dependency %s -> %s (workspace=true)\n", module, child)
			}
		}
		if seed.Vendor != nil {
			fmt.Fprintln(out, "# workspace vendor: preserve supplied bytes/modes/licenses; upstream differences unknown; content hashes are not proxy h1")
			for _, line := range project.VendorInventory(seed.Vendor) {
				fmt.Fprintln(out, line)
			}
		}
		files := []string{project.ManifestName}
		for _, rel := range c.Members {
			files = append(files, filepath.Join(rel, project.ManifestName))
		}
		for _, rel := range files {
			b, e := os.ReadFile(filepath.Join(stage, rel))
			if e != nil {
				return e
			}
			fmt.Fprintf(out, "\n# %s\n%s", filepath.ToSlash(rel), b)
		}
		return nil
	}
	lock := project.Lock{Version: 4, NativeWorkspace: seed}
	b, e := json.MarshalIndent(lock, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "goxide.lock"), b, 0644); e != nil {
		return e
	}
	var discard bytes.Buffer
	if e = runNativeWorkspace(CompileRequest{Mode: "build", Workspace: true, GoTool: req.GoTool, Offline: req.Offline}, stage, w, &discard, stderr); e != nil {
		return fmt.Errorf("workspace migration validation failed; original unchanged: %w", e)
	}
	for _, name := range []string{"target", ".hgo"} {
		if e = os.RemoveAll(filepath.Join(stage, name)); e != nil {
			return e
		}
	}
	if _, e = os.Lstat(final); !os.IsNotExist(e) {
		return fmt.Errorf("workspace destination appeared; not overwritten")
	}
	if e = os.Rename(stage, final); e != nil {
		return e
	}
	_, e = fmt.Fprintf(out, "migrated workspace: %s\noriginal workspace retained: %s\n", final, root)
	return e
}
func copyImportVendor(src, dst string) error {
	if e := project.NoSymlinks(src); e != nil {
		return e
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, path)
		to := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("vendor symlink forbidden: %s", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("vendor special file: %s", rel)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = os.WriteFile(to, b, info.Mode().Perm()); e != nil {
			return e
		}
		return os.Chmod(to, info.Mode().Perm())
	})
}
