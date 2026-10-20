package project

import (
	"encoding/json"
	"fmt"
	"go/version"
	"os"
	"path/filepath"
	"strings"
)

// Input fingerprints have no owner/target/cache path; a materialized Git-only
// member may use a copied root lock without reading unrelated lock records.
func nodeInput(n *Node) string { return Digest([]any{n.Manifest, string(n.ModBytes)}) }
func (g *Graph) validateUnified() error {
	u := g.Lock.Unified
	for _, n := range g.Nodes {
		if !g.nativeStandalone && n != g.Root && u.Sources[n.Module] != n.SourceID {
			return fmt.Errorf("unified lock source identity changed for %s", n.Module)
		}
		if want, ok := u.Inputs[n.Module]; !ok || want != g.input(n) {
			return fmt.Errorf("unified lock missing or changed reachable input %s; explicitly update lock", n.Module)
		}
	}
	for module, actual := range g.Plan {
		pin, ok := u.Modules[module]
		if !ok {
			return fmt.Errorf("unified lock missing reachable module %s", module)
		}
		a, b := effective(actual), effective(pin)
		if a.Version == "" && a.Path != b.Path {
			found := false
			for _, n := range g.Nodes {
				if n.Module == module {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("unified lock local source changed for %s", module)
			}
		}
		if actual.Path != pin.Path || actual.Version != pin.Version || a.Version != b.Version || a.Sum != b.Sum || a.GoModSum != b.GoModSum || (a.Version != "" && a.Path != b.Path) {
			return fmt.Errorf("unified lock identity/checksum changed for %s", module)
		}
	}
	return nil
}

func loadWorkspaceGraph(w *Workspace, root, tool string, requested []string, defaults, offline, locked bool, targetDir string, featureOverrides map[string]Request) (*Graph, error) {
	output, e := ResolveTargetDir(w.Root, targetDir)
	if e != nil {
		return nil, e
	}
	for _, dir := range w.Members {
		if contained(output, dir) || dir != w.Root && contained(dir, output) {
			return nil, fmt.Errorf("workspace target overlaps member source: %s", dir)
		}
	}
	layout, e := OpenLayout(w.Root, WorkspaceOwner, targetDir)
	if e != nil {
		return nil, e
	}
	defer layout.Close()
	work, e := os.MkdirTemp(layout.Root, ".workspace-plan-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(work)
	g := &Graph{WorkRoot: work, Tool: tool, Offline: offline, Locked: locked, Cache: filepath.Join(layout.Root, "cache", "git"), LockPath: filepath.Join(w.Root, "goxide.lock"), Lock: Lock{Version: 3, Git: map[string]string{}, Sums: map[string]string{}}, workspace: w, ownerRoot: w.Root, ownerModule: WorkspaceOwner, overrides: w.Manifests}
	b, e := os.ReadFile(g.LockPath)
	if e == nil {
		if e = json.Unmarshal(b, &g.Lock); e != nil {
			return nil, e
		}
		if g.Lock.Version != 3 || g.Lock.Unified == nil {
			return nil, fmt.Errorf("workspace requires version 3 unified root lock; explicit migration required")
		}
		g.original = b
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if locked {
		return nil, fmt.Errorf("--locked requires root goxide.lock")
	}
	if g.Lock.Git == nil {
		g.Lock.Git = map[string]string{}
	}
	if g.Lock.Sums == nil {
		g.Lock.Sums = map[string]string{}
	}
	workspaceID := Digest([]any{w.Config, w.Members})
	toolVersion, e := goCommand(tool, work, true, nil, "env", "GOVERSION")
	if e != nil {
		return nil, e
	}
	for _, m := range w.Manifests {
		if version.Compare(strings.TrimSpace(string(toolVersion)), "go"+m.GoVersion) < 0 {
			return nil, fmt.Errorf("workspace member %s requires Go >= %s", m.Module, m.GoVersion)
		}
	}
	fresh := g.Lock.Unified == nil
	if !fresh && g.Lock.Unified.Workspace != workspaceID {
		if locked {
			return nil, fmt.Errorf("--locked: workspace membership/policy changed")
		}
		fresh = true
	}
	if !fresh {
		for _, dir := range keys(w.Manifests) {
			n, e := g.readNode(dir, false, "")
			if e != nil {
				return nil, e
			}
			if g.Lock.Unified.Inputs[n.Module] != nodeInput(n) {
				if locked {
					return nil, fmt.Errorf("--locked: workspace member policy changed: %s", n.Module)
				}
				fresh = true
			}
		}
	}
	if !fresh && version.Compare(strings.TrimSpace(string(toolVersion)), "go"+g.Lock.Unified.MinimumGo) < 0 {
		return nil, fmt.Errorf("workspace lock requires Go >= %s", g.Lock.Unified.MinimumGo)
	}
	synthetic := Manifest{Module: "goxide-workspace.invalid/root", GoVersion: "1.27", Dependencies: map[string]Dependency{}}
	seenReplace := map[string]GoReplace{}
	seenExclude := map[string]bool{}
	for i, module := range keys(w.Members) {
		dir := w.Members[module]
		m := w.Manifests[dir]
		if version.Compare("go"+m.GoVersion, "go"+synthetic.GoVersion) > 0 {
			synthetic.GoVersion = m.GoVersion
		}
		synthetic.Dependencies[fmt.Sprintf("member%d", i)] = Dependency{Module: module, Path: dir, Features: keys(m.Features)}
		for _, rep := range m.Native.Replace {
			if rep.Path != "" && !filepath.IsAbs(rep.Path) {
				rep.Path = filepath.Join(dir, rep.Path)
			}
			key := rep.Module + "@" + rep.Version
			if prior, ok := seenReplace[key]; ok && prior != rep {
				return nil, fmt.Errorf("conflicting workspace replacement for %s", key)
			}
			seenReplace[key] = rep
		}
		for _, x := range m.Native.Exclude {
			key := x.Module + "@" + x.Version
			if !seenExclude[key] {
				synthetic.Native.Exclude = append(synthetic.Native.Exclude, x)
				seenExclude[key] = true
			}
		}
	}
	for _, key := range keys(seenReplace) {
		synthetic.Native.Replace = append(synthetic.Native.Replace, seenReplace[key])
	}
	policyBytes, e := ModuleText(synthetic)
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(work, "go.mod"), policyBytes, 0644); e != nil {
		return nil, e
	}
	expectedPolicy, e := ReadMod(tool, work)
	if e != nil {
		return nil, e
	}
	if !fresh && Digest(expectedPolicy) != Digest(g.Lock.Unified.Policy) {
		return nil, fmt.Errorf("unified lock root policy differs from workspace manifests")
	}
	if fresh {
		// A private in-memory root aggregates explicit member policies for Go MVS.
		// It never supplies imports to generated code and is never compiled.
		synthRoot := filepath.Join(work, "root")
		if e = os.Mkdir(synthRoot, 0755); e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(synthRoot, ManifestName), []byte("[go_mod]\nmodule=\"goxide-workspace.invalid/root\"\ngo=\"1.27\"\n"), 0644); e != nil {
			return nil, e
		}
		g.overrides[synthRoot] = synthetic
		g.allFeatures = true
		g.unifiedPlanning = true
		if _, e = g.plan(synthRoot, nil, false); e != nil {
			return nil, e
		}
		nodes := map[string]*Node{}
		for _, n := range g.Nodes {
			nodes[n.Module] = n
		}
		u := &UnifiedLock{GitPins: map[string]string{}, MinimumGo: g.RootMod.Go, Workspace: workspaceID, Sources: map[string]string{}, Policy: g.RootMod, Inputs: map[string]string{}, Modules: g.portablePlan(w.Root, g.Plan, nodes)}
		for _, n := range g.Nodes {
			for _, d := range n.Sources {
				if d.Git != "" {
					key := d.GitKey()
					u.GitPins[key] = g.Lock.Git[key]
				}
			}
			if n.Root != synthRoot {
				u.Inputs[n.Module] = nodeInput(n)
				u.Sources[n.Module] = n.SourceID
			}
		}
		for _, selection := range g.Plan {
			dir := effective(selection).Dir
			var mod ModFile
			found := false
			for _, n := range g.Nodes {
				if n.Module == selection.Path {
					mod = n.Mod
					found = true
					break
				}
			}
			if !found {
				var err error
				if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); os.IsNotExist(statErr) && effective(selection).GoMod != "" {
					metadata := effective(selection).GoMod
					if err = NoSymlinks(metadata); err != nil {
						return nil, err
					}
					b, err := os.ReadFile(metadata)
					if err != nil {
						return nil, err
					}
					dir, err = os.MkdirTemp(work, "native-mod-")
					if err != nil {
						return nil, err
					}
					if err = os.WriteFile(filepath.Join(dir, "go.mod"), b, 0644); err != nil {
						return nil, err
					}
				}
				mod, err = ReadMod(tool, dir)
				if err != nil {
					return nil, err
				}
			}
			if version.Compare("go"+mod.Go, "go"+u.MinimumGo) > 0 {
				u.MinimumGo = mod.Go
			}
		}
		g.Lock.Unified = u
		delete(g.overrides, synthRoot)
	}
	g.featureOverrides = featureOverrides
	g.forced = g.Lock.Unified.Modules
	g.allFeatures = false
	g.unifiedPlanning = false
	g.Nodes = nil
	g.Root = nil
	return g.plan(root, requested, defaults)
}

// LoadWorkspaceGraphs computes a feature union only across selected build roots.
// The lock's whole-workspace version graph remains independent of these requests.
// Callers must hold LockProject until all returned graphs have been consumed.
func LoadWorkspaceGraphs(w *Workspace, members []string, tool string, requested map[string][]string, defaults, offline, locked bool, targetDir string) ([]*Graph, error) {
	overrides := map[string]Request{}
	for iteration := 0; iteration < 64; iteration++ {
		next := map[string]Request{}
		graphs := []*Graph{}
		for _, module := range members {
			g, e := loadWorkspaceGraph(w, w.Members[module], tool, requested[module], defaults, offline, locked, targetDir, overrides)
			if e != nil {
				return nil, e
			}
			if e = g.Save(); e != nil {
				return nil, e
			}
			graphs = append(graphs, g)
			for _, n := range g.Nodes {
				next[n.Module] = unionRequest(next[n.Module], Request{Features: keys(n.Features)})
			}
		}
		if Digest(next) == Digest(overrides) {
			return graphs, nil
		}
		overrides = next
	}
	return nil, fmt.Errorf("workspace build feature graph did not converge")
}
