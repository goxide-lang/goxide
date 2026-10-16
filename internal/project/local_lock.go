package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// portableSelections normalizes local paths inside the author root, never
// external paths. The runtime plan retains absolute paths for Go invocations.
func portableSelections(root string, selections map[string]Selection) map[string]Selection {
	out := map[string]Selection{}
	for module, s := range selections {
		if s.Replace != nil && s.Replace.Version == "" {
			r := *s.Replace
			if filepath.IsAbs(r.Path) {
				if rel, e := filepath.Rel(root, r.Path); e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					r.Path = "./" + filepath.ToSlash(rel)
				}
			}
			s.Replace = &r
		}
		out[module] = s
	}
	return out
}

func captureNativeLocals(w *Workspace, lock *NativeWorkspaceLock) error {
	lock.LocalSources = map[string]NativeMemberRecord{}
	for module, s := range lock.Modules {
		if s.Replace == nil || s.Replace.Version != "" {
			continue
		}
		source := s.Replace.Path
		if !contained(w.Root, source) {
			continue
		}
		data, e := os.ReadFile(filepath.Join(source, "go.mod"))
		if e != nil {
			return e
		}
		lock.LocalSources[module] = NativeMemberRecord{Input: Digest(data)}
		r := *s.Replace
		r.Path = "local:" + module
		s.Replace = &r
		lock.Modules[module] = s
	}
	return nil
}

// bindNativeLocal reads only a currently declared local replacement. A copied
// lock authenticates module metadata but cannot authorize a historical path.
func (g *Graph) bindNativeLocal(module string, pin Selection, replacements []ModReplace) (Selection, error) {
	record, ok := g.Lock.NativeWorkspace.LocalSources[module]
	if !ok {
		return pin, nil
	}
	var fallback, exact string
	for _, r := range replacements {
		if r.Old.Path == module && r.New.Version == "" {
			if r.Old.Version == "" {
				fallback = r.New.Path
			} else if r.Old.Version == pin.Version {
				exact = r.New.Path
			}
		}
	}
	location := exact
	if location == "" {
		location = fallback
	}
	if location == "" {
		return pin, fmt.Errorf("copied lock needs explicit local path for %s", module)
	}
	if err := NoSymlinks(location); err != nil {
		return pin, err
	}
	data, e := os.ReadFile(filepath.Join(location, "go.mod"))
	if e != nil {
		return pin, e
	}
	if Digest(data) != record.Input {
		return pin, fmt.Errorf("copied lock local module metadata changed for %s", module)
	}
	m, e := ReadMod(g.Tool, location, g.Environment)
	if e != nil {
		return pin, e
	}
	if m.Module.Path != module {
		return pin, fmt.Errorf("local module identity mismatch: want %s, got %s", module, m.Module.Path)
	}
	pin.Replace = &Selection{Path: location}
	g.forced[module] = pin
	return pin, nil
}

// Acquired sources are identified by their module and pinned Git/source record,
// not the target directory chosen to materialize their checkout.
func (g *Graph) portablePlan(root string, selections map[string]Selection, nodes map[string]*Node) map[string]Selection {
	out := portableSelections(root, selections)
	for module, n := range nodes {
		if n.CheckoutRoot == "" || !contained(g.Cache, n.CheckoutRoot) {
			continue
		}
		s, ok := out[module]
		if !ok || s.Replace == nil || s.Replace.Version != "" {
			continue
		}
		replacement := *s.Replace
		replacement.Path = module
		s.Replace = &replacement
		out[module] = s
	}
	return out
}
