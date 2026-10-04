package project

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// NativeMemberRecord authenticates a materialized member independently of the
// workspace-wide input digest. Local member locations may be explicitly moved;
// their own records and the module path are checked when they become reachable.
type NativeMemberRecord struct {
	Input string `json:"input"`
}

func nativeMemberInput(m Manifest, root string, locations map[string]string) string {
	b, _ := json.Marshal(m)
	var normalized Manifest
	_ = json.Unmarshal(b, &normalized)
	direct := map[string]string{}
	for alias, d := range normalized.Dependencies {
		if d.Path == "" {
			continue
		}
		path := d.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		if locations[d.Module] == filepath.Clean(path) {
			direct[d.Module] = filepath.Clean(path)
			d.Path = "member:" + d.Module
			normalized.Dependencies[alias] = d
		}
	}
	normalized.Native.Replace = nil
	for _, r := range m.Native.Replace {
		path := r.Path
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		// An explicit local member edge already specifies this same source. The
		// standalone planner still requires the corresponding native replace when
		// a native require coexists; this only normalizes the relocation identity.
		if r.Path != "" && direct[r.Module] != "" && direct[r.Module] == filepath.Clean(path) {
			continue
		}
		if r.Path != "" && locations[r.Module] == filepath.Clean(path) {
			r.Path = "local:" + r.Module
		}
		normalized.Native.Replace = append(normalized.Native.Replace, r)
	}
	return Digest(normalized)
}
func CaptureNativeMembers(w *Workspace, lock *NativeWorkspaceLock) {
	lock.Members = map[string]NativeMemberRecord{}
	locations := map[string]string{}
	for module, root := range w.Members {
		locations[module] = root
	}
	for module, pin := range lock.Modules {
		if pin.Replace != nil && pin.Replace.Version == "" && contained(w.Root, pin.Replace.Path) {
			locations[module] = pin.Replace.Path
		}
	}
	for module, root := range w.Members {
		lock.Members[module] = NativeMemberRecord{Input: nativeMemberInput(w.Manifests[root], root, locations)}
	}
}
func (g *Graph) input(n *Node) string {
	if !g.nativeStandalone {
		return nodeInput(n)
	}
	locations := map[string]string{n.Module: n.Root}
	for _, d := range n.Manifest.Dependencies {
		if _, known := g.Lock.NativeWorkspace.Members[d.Module]; !known || d.Path == "" {
			continue
		}
		path := d.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(n.Root, path)
		}
		locations[d.Module] = filepath.Clean(path)
	}
	for module := range g.Lock.NativeWorkspace.LocalSources {
		pin := g.Lock.NativeWorkspace.Modules[module]
		var fallback, exact string
		for _, r := range n.Manifest.Native.Replace {
			if r.Module == module && r.Path != "" {
				if r.Version == "" {
					fallback = r.Path
				} else if r.Version == pin.Version {
					exact = r.Path
				}
			}
		}
		p := exact
		if p == "" {
			p = fallback
		}
		if p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(n.Root, p)
			}
			locations[module] = p
		}
	}
	return nativeMemberInput(n.Manifest, n.Root, locations)
}
func (g *Graph) prepareNativeStandalone() error {
	n := g.Lock.NativeWorkspace
	if n == nil || len(n.Members) == 0 {
		return fmt.Errorf("copied native lock lacks member records; refresh it in the original workspace")
	}
	if n.Vendor != nil {
		return fmt.Errorf("obsolete vendor lock: explicitly re-import original Go input as local modules")
	}
	g.nativeStandalone = true
	g.Lock.Unified = &UnifiedLock{Inputs: map[string]string{}, Sources: map[string]string{}, Modules: map[string]Selection{}, GitPins: map[string]string{}}
	for module, record := range n.Members {
		g.Lock.Unified.Inputs[module] = record.Input
		// This location is intentionally unusable until an explicit author path
		// resolves and verifies the corresponding member. Membership is not a path.
		g.Lock.Unified.Modules[module] = Selection{Path: module, Version: Placeholder(module), Replace: &Selection{Path: "unmaterialized-native-member:" + module}}
	}
	for module, pin := range n.Modules {
		g.Lock.Unified.Modules[module] = pin
	}
	g.forced = g.Lock.Unified.Modules
	return nil
}
