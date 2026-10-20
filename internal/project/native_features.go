package project

import (
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"os"
	"path/filepath"
	"strings"
)

// NativeSelectionIdentity excludes checksums completed during package loading.
// Those integrity records do not change the selected source configuration.
func NativeSelectionIdentity(selections map[string]Selection) map[string]Selection {
	out := map[string]Selection{}
	for path, selected := range selections {
		identity := Selection{Path: selected.Path, Version: selected.Version}
		if selected.Replace != nil {
			identity.Replace = &Selection{Path: selected.Replace.Path, Version: selected.Replace.Version}
		}
		out[path] = identity
	}
	return out
}

// NativeFeatureGraphs uses the same additive Resolve semantics as the author
// graph, while leaving all Go version/replacement policy in the native view.
func NativeFeatureGraphs(w *Workspace, roots []string, tool string, requested map[string][]string, defaults bool, lock Lock) (map[string]*Graph, error) {
	modules, err := w.GoModules()
	if err != nil {
		return nil, err
	}
	for module := range modules {
		if _, e := NativeClosure(w, module); e != nil {
			return nil, e
		}
	}
	requests := map[string]Request{}
	for _, module := range roots {
		if _, ok := modules[module]; !ok {
			return nil, fmt.Errorf("unknown native workspace member %s", module)
		}
		requests[pathSource(modules[module]).key()] = Request{Features: requested[module], Defaults: defaults}
	}
	nodes := map[string]*Node{}
	for {
		changed := false
		for _, key := range keys(requests) {
			r := requests[key]
			root := w.Members[key]
			m := w.Manifests[root]
			features, deps, e := Resolve(m, r.Features, r.Defaults)
			if e != nil {
				return nil, fmt.Errorf("member %s: %w", root, e)
			}
			n := nodes[key]
			if n == nil {
				n = &Node{Root: root, Module: m.Module, Manifest: m, HasManifest: true, SourceID: Digest(m)}
				n.setSource(pathSource(root))
				nodes[key] = n
			}
			n.Features, n.Requests, n.Children, n.Sources = features, deps, map[string]*Node{}, map[string]Dependency{}
			n.ModBytes, e = ModuleText(m)
			if e != nil {
				return nil, e
			}
			for alias, r := range deps {
				d, e := w.memberDependency(n.Root, alias, m.Dependencies[alias])
				if e != nil {
					return nil, e
				}
				if d.IsToolchain() {
					continue
				}
				if _, member := modules[d.Module]; !member {
					if _, selected := lock.NativeWorkspace.Modules[d.Module]; !selected {
						return nil, fmt.Errorf("native workspace dependency %s: selected native module %s is missing", alias, d.Module)
					}
					continue // Requests exposes the root; Go owns its fixed artifact.
				}
				n.Sources[alias] = d
				childKey := pathSource(modules[d.Module]).key()
				old, exists := requests[childKey]
				merged := map[string]bool{}
				for _, f := range old.Features {
					merged[f] = true
				}
				for _, f := range r.Features {
					merged[f] = true
				}
				next := Request{Features: keys(merged), Defaults: old.Defaults || r.Defaults}
				if !exists || Digest(next) != Digest(old) {
					requests[childKey] = next
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	for _, n := range nodes {
		for alias, dependency := range n.Sources {
			n.Children[alias] = nodes[pathSource(modules[dependency.Module]).key()]
		}
	}
	graphs := map[string]*Graph{}
	for _, module := range roots {
		g := &Graph{Root: nodes[pathSource(modules[module]).key()], Tool: tool, Lock: lock, LockPath: filepath.Join(w.Root, "goxide.lock"), ownerRoot: w.Root, ownerModule: WorkspaceOwner, Plan: lock.NativeWorkspace.Modules, PlanID: Digest([]any{lock.NativeWorkspace.Input, NativeSelectionIdentity(lock.NativeWorkspace.Modules)}), workspace: w}
		seen := map[string]bool{}
		var visit func(*Node)
		visit = func(n *Node) {
			if seen[n.SourceKey] {
				return
			}
			seen[n.SourceKey] = true
			for _, alias := range keys(n.Children) {
				visit(n.Children[alias])
			}
			g.Nodes = append(g.Nodes, n)
		}
		visit(g.Root)
		for _, n := range g.Nodes {
			if n == g.Root {
				continue
			}
			enhanced, e := NativeEnhanced(n.Root, n.Manifest)
			if e != nil {
				return nil, e
			}
			if enhanced && n.Manifest.Lib == nil {
				return nil, fmt.Errorf("enhanced native dependency %s requires lib", n.Module)
			}
			if n.Manifest.Lib != nil {
				for _, f := range n.Manifest.Lib.RequiredFeatures {
					if !n.Features[f] {
						return nil, fmt.Errorf("dependency %s lib requires feature %s", n.Module, f)
					}
				}
			}
		}
		graphs[module] = g
	}
	return graphs, nil
}

// LoadNativeGraph prepares the same script subjects as a native workspace build.
// It neither executes scripts nor writes a lock while planning permissions.
func LoadNativeGraph(w *Workspace, root, tool string, requested []string, defaults, offline, locked bool, targetDir string) (*Graph, error) {
	module := w.Manifests[root].Module
	if module == "" {
		return nil, fmt.Errorf("native workspace requires member selection")
	}
	b, e := os.ReadFile(filepath.Join(w.Root, "goxide.lock"))
	if e != nil {
		return nil, e
	}
	var lock Lock
	if e = json.Unmarshal(b, &lock); e != nil {
		return nil, e
	}
	if lock.Version != 4 || lock.NativeWorkspace == nil {
		return nil, fmt.Errorf("native workspace requires schema 4 lock")
	}
	config, e := NativeConfiguration(tool)
	if e != nil {
		return nil, e
	}
	if Digest(config) != Digest(w.Config.Go.Configuration) {
		return nil, fmt.Errorf("native workspace configuration differs; re-analyze migration")
	}
	input, e := NativeInput(w)
	if e != nil {
		return nil, e
	}
	if locked && input != lock.NativeWorkspace.Input {
		return nil, fmt.Errorf("--locked: native workspace inputs changed")
	}
	lock.NativeWorkspace.Input = input
	layout, e := OpenLayout(w.Root, WorkspaceOwner, targetDir)
	if e != nil {
		return nil, e
	}
	defer layout.Close()
	view, e := os.MkdirTemp(layout.Root, ".native-scripts-plan-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(view)
	before := Digest(lock.NativeWorkspace)
	if _, e = NativeView(w, view, map[string]bool{}, lock.NativeWorkspace); e != nil {
		return nil, e
	}
	if e = ReadNativeSelections(w, tool, view, lock.NativeWorkspace, offline); e != nil {
		return nil, e
	}
	if locked && Digest(lock.NativeWorkspace) != before {
		return nil, fmt.Errorf("--locked: native workspace selection changed")
	}
	graphs, e := NativeFeatureGraphs(w, []string{module}, tool, map[string][]string{module: requested}, defaults, lock)
	if e != nil {
		return nil, e
	}
	return graphs[module], nil
}

// NativeEnhanced detects enhancement without parsing or executing source.
func NativeEnhanced(root string, m Manifest) (bool, error) {
	found := len(m.Features) > 0 || m.Build != nil
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := naming.Path(path); err != nil {
				return err
			}
		}
		if d.IsDir() && path != root {
			if d.Name() == ".git" || d.Name() == ".goxide" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			for _, marker := range []string{ManifestName, "go.mod", LayoutMarker} {
				if _, e := os.Lstat(filepath.Join(path, marker)); e == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(e) {
					return e
				}
			}
		}
		if !d.IsDir() && strings.HasSuffix(path, ".gox") {
			found = true
		}
		return nil
	})
	return found, e
}
