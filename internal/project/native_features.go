package project

import (
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"os"
	"path/filepath"
	"strings"
)

// NativeFeatureGraphs uses the same additive Resolve semantics as the author
// graph, while leaving all Go version/replacement policy in the native view.
func NativeFeatureGraphs(w *Workspace, roots []string, tool string, requested map[string][]string, defaults bool, lock Lock) (map[string]*Graph, error) {
	for module := range w.Members {
		if _, e := NativeClosure(w, module); e != nil {
			return nil, e
		}
	}
	requests := map[string]Request{}
	for _, module := range roots {
		requests[module] = Request{Features: requested[module], Defaults: defaults}
	}
	nodes := map[string]*Node{}
	for {
		changed := false
		for _, module := range keys(requests) {
			r := requests[module]
			m := w.Manifests[w.Members[module]]
			features, deps, e := Resolve(m, r.Features, r.Defaults)
			if e != nil {
				return nil, fmt.Errorf("member %s: %w", module, e)
			}
			n := nodes[module]
			if n == nil {
				n = &Node{Root: w.Members[module], Module: module, Manifest: m, HasManifest: true, SourceID: Digest(m)}
				nodes[module] = n
			}
			n.Features, n.Requests, n.Children, n.Sources = features, deps, map[string]*Node{}, map[string]Dependency{}
			n.ModBytes, e = ModuleText(m)
			if e != nil {
				return nil, e
			}
			for alias, r := range deps {
				d := m.Dependencies[alias]
				n.Sources[alias] = d
				old, exists := requests[d.Module]
				merged := map[string]bool{}
				for _, f := range old.Features {
					merged[f] = true
				}
				for _, f := range r.Features {
					merged[f] = true
				}
				next := Request{Features: keys(merged), Defaults: old.Defaults || r.Defaults}
				if !exists || Digest(next) != Digest(old) {
					requests[d.Module] = next
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	for _, n := range nodes {
		for alias := range n.Requests {
			n.Children[alias] = nodes[n.Manifest.Dependencies[alias].Module]
		}
	}
	graphs := map[string]*Graph{}
	for _, module := range roots {
		g := &Graph{Root: nodes[module], Tool: tool, Lock: lock, LockPath: filepath.Join(w.Root, "goxide.lock"), ownerRoot: w.Root, ownerModule: WorkspaceOwner, Plan: lock.NativeWorkspace.Modules, PlanID: Digest([]any{lock.NativeWorkspace.Input, lock.NativeWorkspace.Modules}), workspace: w}
		seen := map[string]bool{}
		var visit func(*Node)
		visit = func(n *Node) {
			if seen[n.Module] {
				return
			}
			seen[n.Module] = true
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
