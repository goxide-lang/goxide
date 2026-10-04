package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

// Selection describes Go's selected logical module and effective source. Cache
// paths are operational details, never persisted in configuration/lock identity.
type Selection struct {
	Path     string
	Version  string
	Sum      string     `json:",omitempty"`
	GoModSum string     `json:",omitempty"`
	Replace  *Selection `json:",omitempty"`
	Dir      string     `json:"-"`
	GoMod    string     `json:"-"`
}

func (s *Selection) UnmarshalJSON(data []byte) error {
	type wire Selection
	v := struct {
		*wire
		Dir, GoMod string
	}{wire: (*wire)(s)}
	if e := json.Unmarshal(data, &v); e != nil {
		return e
	}
	s.Dir = v.Dir
	s.GoMod = v.GoMod
	return nil
}

type ModuleVersion struct{ Path, Version string }
type ModReplace struct{ Old, New ModuleVersion }
type ModFile struct {
	Module  ModuleVersion
	Go      string
	Require []ModuleVersion
	Replace []ModReplace
	Exclude []ModuleVersion
}

func goCommand(tool, dir string, offline bool, args ...string) ([]byte, error) {
	c := exec.Command(tool, args...)
	c.Dir = dir
	c.Env = GoEnv(offline)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("Go module planning %s: %w: %s", args[0], e, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}
func ReadMod(tool, root string) (ModFile, error) {
	var m ModFile
	info, e := os.Lstat(filepath.Join(root, "go.mod"))
	if e != nil || !info.Mode().IsRegular() {
		return m, fmt.Errorf("module directory must contain its own regular go.mod: %s", root)
	}
	b, e := goCommand(tool, root, true, "mod", "edit", "-json")
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if e == nil && m.Module.Path == "" {
		e = fmt.Errorf("go.mod has no module directive")
	}
	return m, e
}

const plannerPrefix = "hgo-planner.invalid/"

func Placeholder(module string) string {
	last := module[strings.LastIndex(module, "/")+1:]
	if strings.HasPrefix(module, "gopkg.in/") {
		if i := strings.LastIndex(last, ".v"); i >= 0 {
			major := strings.TrimSuffix(last[i+2:], "-unstable")
			if n, e := strconv.Atoi(major); e == nil && n >= 0 {
				return fmt.Sprintf("v%d.0.0", n)
			}
		}
	}
	if len(last) > 1 && last[0] == 'v' {
		if n, e := strconv.Atoi(last[1:]); e == nil && n >= 2 {
			return fmt.Sprintf("v%d.0.0", n)
		}
	}
	return "v0.0.0"
}
func normalizedReplaces(root string, m ModFile) ([]ModReplace, error) {
	out := append([]ModReplace(nil), m.Replace...)
	for i := range out {
		if out[i].New.Version == "" {
			p := out[i].New.Path
			if !filepath.IsAbs(p) {
				p = filepath.Join(root, p)
			}
			if e := NoSymlinks(p); e != nil {
				return nil, e
			}
			p, e := filepath.EvalSymlinks(p)
			if e != nil {
				return nil, e
			}
			out[i].New.Path = p
		}
	}
	return out, nil
}
func (g *Graph) readNode(root string) (*Node, error) {
	m, e := ReadMod(g.Tool, root)
	if e != nil {
		return nil, e
	}
	n := &Node{Root: root, Module: m.Module.Path, Manifest: Manifest{Version: 1}, Children: map[string]*Node{}, Sources: map[string]Dependency{}}
	info, e := os.Lstat(filepath.Join(root, ManifestName))
	if os.IsNotExist(e) {
		return n, nil
	}
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("hgo manifest must be a regular file")
	}
	n.HasManifest = true
	n.Manifest, e = Read(root)
	return n, e
}

// Published source projects must not silently incorporate a previous hgo view.
func validatePublished(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("published module contains symlink: %s", path)
		}
		if d.IsDir() {
			if path != root && (d.Name() == ".git" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if name == "hgo_project_config.go" || name == ".hgo.refs.json" || name == ".hgo.enums.json" || name == ".hgo.uses.json" || strings.HasSuffix(name, ".go.map.json") {
			return fmt.Errorf("published hgo source mixes generated artifacts: %s", path)
		}
		if strings.HasSuffix(name, ".hgo") {
			if _, e := os.Lstat(strings.TrimSuffix(path, ".hgo") + ".go"); e == nil {
				return fmt.Errorf("published hgo source has duplicate .hgo/.go ownership: %s", path)
			} else if !os.IsNotExist(e) {
				return e
			}
		}
		return nil
	})
}

type requirement struct{ Module, Version string }
type planningRound struct {
	root         *Node
	nodes        map[string]*Node
	requirements []requirement
	local        map[string]string
	errors       []error
}

func unionRequest(a, b Request) Request {
	set := map[string]bool{}
	for _, f := range a.Features {
		set[f] = true
	}
	for _, f := range b.Features {
		set[f] = true
	}
	return Request{Features: keys(set), Defaults: a.Defaults || b.Defaults}
}
func effective(s Selection) Selection {
	if s.Replace != nil {
		return *s.Replace
	}
	return s
}
func (g *Graph) round(root string, requested []string, defaults bool, selected map[string]Selection) (*planningRound, error) {
	r := &planningRound{nodes: map[string]*Node{}, local: map[string]string{}}
	var e error
	r.root, e = g.readNode(root)
	if e != nil {
		return nil, e
	}
	r.nodes[r.root.Module] = r.root
	reqs := map[string]Request{r.root.Module: {Features: requested, Defaults: defaults}}
	deferred := map[string]error{}
	provenance := map[string]map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, module := range keys(r.nodes) {
			n := r.nodes[module]
			req := reqs[module]
			features, deps, e := Resolve(n.Manifest, req.Features, req.Defaults)
			if e != nil {
				if n == r.root {
					return nil, e
				}
				deferred[module] = e
				known := []string{}
				for _, f := range req.Features {
					if _, ok := n.Manifest.Features[f]; ok {
						known = append(known, f)
					}
				}
				features, deps, e = Resolve(n.Manifest, known, req.Defaults)
				if e != nil {
					features, deps, _ = Resolve(Manifest{Dependencies: n.Manifest.Dependencies}, nil, false)
				}
			}
			n.Features = features
			n.Requests = deps
			for _, alias := range keys(deps) {
				d := n.Manifest.Dependencies[alias]
				if strings.HasPrefix(d.Module, plannerPrefix) {
					return nil, fmt.Errorf("reserved planner module namespace: %s", d.Module)
				}
				n.Sources[alias] = d
				dir, boundary, sourceID, version := "", n.CheckoutRoot, "", ""
				published := false
				if d.Version != "" {
					s, ok := selected[d.Module]
					if !ok {
						continue
					}
					eff := effective(s)
					dir = eff.Dir
					version = s.Version
					sourceID = Digest(s)
					if dir == "" {
						return nil, fmt.Errorf("selected source directory missing: %s", d.Module)
					}
					if eff.Version != "" {
						boundary = dir
						published = true
					} else {
						boundary = ""
					}
				} else if d.Git != "" {
					dir, e = g.gitSource(d)
					if e != nil {
						return nil, e
					}
					boundary = filepath.Join(g.Cache, Digest([]string{d.Git, d.Rev, d.Subdir, d.Module}))
					sourceID = Digest([]string{d.Git, d.Rev, d.Subdir, g.Lock.Git[filepath.Base(boundary)]})
				} else {
					dir = d.Path
					if !filepath.IsAbs(dir) {
						dir = filepath.Join(n.Root, dir)
					}
					if e = NoSymlinks(dir); e != nil {
						return nil, e
					}
					dir, e = filepath.EvalSymlinks(dir)
					if e != nil {
						return nil, e
					}
				}
				if boundary != "" && !contained(boundary, dir) {
					return nil, fmt.Errorf("dependency path escapes checkout/acquired source boundary: %s", alias)
				}
				if prior, ok := r.nodes[d.Module]; ok && prior.Root != dir {
					return nil, fmt.Errorf("conflicting sources for module %s", d.Module)
				}
				if d.Version != "" {
					if _, err := os.Lstat(filepath.Join(dir, ManifestName)); os.IsNotExist(err) {
						if len(deps[alias].Features) > 0 {
							deferred[d.Module] = fmt.Errorf("version dependency %s has no hgo source manifest; fixed Go artifacts cannot receive hgo features", alias)
						}
						continue
					} else if err != nil {
						return nil, err
					}
				}
				child, ok := r.nodes[d.Module]
				if !ok {
					child, e = g.readNode(dir)
					if e != nil {
						if child == nil {
							return nil, e
						}
						deferred[d.Module] = e
					}
					if d.Version != "" && !child.HasManifest {
						if len(deps[alias].Features) > 0 {
							deferred[d.Module] = fmt.Errorf("version dependency %s has no hgo source manifest; fixed Go artifacts cannot receive hgo features", alias)
						}
						continue
					}
					if child.Module != d.Module {
						return nil, fmt.Errorf("dependency %s module path mismatch: want %s, got %s", alias, d.Module, child.Module)
					}
					if published {
						if e = validatePublished(dir); e != nil {
							deferred[d.Module] = e
						}
					}
					child.CheckoutRoot = boundary
					child.SourceID = sourceID
					child.SelectedVersion = version
					if boundary != "" {
						mod, err := ReadMod(g.Tool, dir)
						if err != nil {
							return nil, err
						}
						for _, rep := range mod.Replace {
							if rep.New.Version == "" {
								p := rep.New.Path
								if !filepath.IsAbs(p) {
									p = filepath.Join(dir, p)
								}
								p, err = filepath.EvalSymlinks(p)
								if err != nil || !contained(boundary, p) {
									return nil, fmt.Errorf("acquired dependency go.mod replace escapes checkout/source boundary: %s", rep.Old.Path)
								}
							}
						}
					}
					r.nodes[d.Module] = child
					changed = true
				}
				if version != "" {
					child.SelectedVersion = version
				}
				if boundary != "" && boundary != child.CheckoutRoot && (child.CheckoutRoot == "" || contained(child.CheckoutRoot, boundary)) {
					child.CheckoutRoot = boundary
					changed = true // Revisit children already traversed via a weaker source edge.
				}
				if sourceID == "" {
					sourceID = Digest([]string{child.Module, child.Root})
				}
				if provenance[child.Module] == nil {
					provenance[child.Module] = map[string]bool{}
				}
				provenance[child.Module][sourceID] = true
				child.SourceID = Digest(keys(provenance[child.Module]))
				n.Children[alias] = child
				prior := reqs[d.Module]
				next := unionRequest(prior, deps[alias])
				if !reflect.DeepEqual(prior, next) {
					reqs[d.Module] = next
					changed = true
				}
			}
		}
	}
	for _, module := range keys(r.nodes) {
		n := r.nodes[module]
		for _, alias := range keys(n.Sources) {
			d := n.Sources[alias]
			v := d.Version
			if v == "" {
				v = Placeholder(d.Module)
				child := n.Children[alias]
				if child != nil {
					if old, ok := r.local[d.Module]; ok && old != child.Root {
						return nil, fmt.Errorf("conflicting sources for module %s", d.Module)
					}
					r.local[d.Module] = child.Root
				}
			}
			r.requirements = append(r.requirements, requirement{d.Module, v})
		}
	}
	for _, module := range keys(deferred) {
		r.errors = append(r.errors, fmt.Errorf("%s: %w", module, deferred[module]))
	}
	return r, nil
}

// solve delegates the complete version ordering and module graph to Go. Private
// local go.mod-only bridges preserve multiple constraints for one module path.
func (g *Graph) solve(root string, r *planningRound) (map[string]Selection, error) {
	work, e := os.MkdirTemp("", "hgo-module-plan-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(work)
	raw, e := os.ReadFile(filepath.Join(root, "go.mod"))
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(work, "go.mod"), raw, 0644); e != nil {
		return nil, e
	}
	if sums, e := os.ReadFile(filepath.Join(root, "go.sum")); e == nil {
		if e = os.WriteFile(filepath.Join(work, "go.sum"), sums, 0644); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	run := func(args ...string) ([]byte, error) { return goCommand(g.Tool, work, g.Offline, args...) }
	for _, req := range g.RootMod.Require {
		if strings.HasPrefix(req.Path, plannerPrefix) {
			return nil, fmt.Errorf("reserved planner module namespace: %s", req.Path)
		}
	}
	for _, rep := range g.RootMod.Replace {
		if strings.HasPrefix(rep.Old.Path, plannerPrefix) || strings.HasPrefix(rep.New.Path, plannerPrefix) {
			return nil, fmt.Errorf("reserved planner module namespace in root replacement")
		}
	}
	if strings.HasPrefix(g.RootMod.Module.Path, plannerPrefix) {
		return nil, fmt.Errorf("reserved planner module namespace")
	}
	replacements, e := normalizedReplaces(root, g.RootMod)
	if e != nil {
		return nil, e
	}
	args := []string{"mod", "edit"}
	for _, rep := range replacements {
		old := rep.Old.Path
		if rep.Old.Version != "" {
			old += "@" + rep.Old.Version
		}
		new := rep.New.Path
		if rep.New.Version != "" {
			new += "@" + rep.New.Version
		}
		args = append(args, "-replace="+old+"="+new)
	}
	for _, module := range keys(r.local) {
		dir := r.local[module]
		hasVersion := false
		for _, node := range r.nodes {
			for _, dep := range node.Sources {
				if dep.Module == module && dep.Version != "" {
					hasVersion = true
				}
			}
		}
		if hasVersion {
			matched := false
			for _, rep := range replacements {
				if rep.Old.Path == module && rep.Old.Version == "" && rep.New.Version == "" && rep.New.Path == dir {
					matched = true
				}
			}
			if !matched {
				return nil, fmt.Errorf("conflicting version/path sources for module %s; use an unversioned root local replace to unify", module)
			}
		}
		for _, rep := range replacements {
			if rep.Old.Path == module && (rep.New.Version != "" || rep.New.Path != dir) {
				return nil, fmt.Errorf("manifest source conflicts with go.mod replace for %s", module)
			}
		}
		args = append(args, "-replace="+module+"="+dir)
	}
	if len(args) > 2 {
		if _, e = run(args...); e != nil {
			return nil, e
		}
	}
	// All active hgo module constraints are main-module roots. A pruned Go
	// graph cannot rely on artificial bridge modules to declare a complete
	// indirect-require closure. Bridges retain additional minimum constraints;
	// Go still selects their maximum, never this first-root insertion order.
	rooted := map[string]bool{r.root.Module: true}
	for _, req := range g.RootMod.Require {
		rooted[req.Path] = true
	}
	roots := []string{"mod", "edit"}
	for _, req := range r.requirements {
		if !rooted[req.Module] {
			roots = append(roots, "-require="+req.Module+"@"+req.Version)
			rooted[req.Module] = true
		}
	}
	if len(roots) > 2 {
		if _, e = run(roots...); e != nil {
			return nil, e
		}
	}
	bridgeGo := g.RootMod.Go
	if bridgeGo == "" {
		bridgeGo = "1.16"
	}
	bridges := map[string]bool{}
	for i, req := range r.requirements {
		path := fmt.Sprintf("hgo-planner.invalid/requirements/r%d", i)
		bridges[path] = true
		if req.Module == path || r.root.Module == path {
			return nil, fmt.Errorf("reserved planner module identity")
		}
		dir := filepath.Join(work, fmt.Sprintf("requirement-%d", i))
		if e = os.Mkdir(dir, 0755); e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+path+"\n\ngo "+bridgeGo+"\n"), 0644); e != nil {
			return nil, e
		}
		if _, e = goCommand(g.Tool, dir, g.Offline, "mod", "edit", "-require="+req.Module+"@"+req.Version); e != nil {
			return nil, e
		}
		if _, e = run("mod", "edit", "-require="+path+"@v0.0.0", "-replace="+path+"="+dir); e != nil {
			return nil, e
		}
	}
	data, e := run("list", "-mod=mod", "-m", "-json", "all")
	if e != nil {
		return nil, e
	}
	selected := map[string]Selection{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var s Selection
		e = decoder.Decode(&s)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if s.Path == r.root.Module || bridges[s.Path] {
			continue
		}
		selected[s.Path] = s
	}
	graph, e := run("mod", "graph")
	if e != nil {
		return nil, e
	}
	for _, line := range strings.Split(string(graph), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		parent, _, _ := strings.Cut(fields[0], "@")
		child, _, _ := strings.Cut(fields[1], "@")
		if strings.HasPrefix(child, plannerPrefix) && parent != r.root.Module && !bridges[parent] {
			return nil, fmt.Errorf("native requirement collides with reserved planner namespace: %s", line)
		}
	}
	// Download by selected identity, then ask Go to verify cached zip AND expanded
	// directory. A cached download's Sum by itself cannot detect a modified Dir.
	data, e = run("mod", "download", "-json", "all")
	if e != nil {
		return nil, e
	}
	downloads := map[string]Selection{}
	decoder = json.NewDecoder(bytes.NewReader(data))
	for {
		var s Selection
		e = decoder.Decode(&s)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		downloads[s.Path+"@"+s.Version] = s
	}
	if _, e = run("mod", "verify"); e != nil {
		return nil, fmt.Errorf("module source verification failed before manifest loading: %w", e)
	}
	for _, path := range keys(selected) {
		s := selected[path]
		eff := effective(s)
		if eff.Version != "" {
			d, ok := downloads[eff.Path+"@"+eff.Version]
			if !ok || d.Dir == "" || d.Sum == "" || d.GoModSum == "" {
				return nil, fmt.Errorf("download lacks verified source identity: %s@%s", eff.Path, eff.Version)
			}
			if s.Replace != nil {
				s.Replace = &d
			} else {
				s = d
			}
		} else {
			if eff.Dir == "" {
				eff.Dir = eff.Path
			}
			if !filepath.IsAbs(eff.Dir) {
				eff.Dir = filepath.Join(work, eff.Dir)
			}
			eff.Dir, e = filepath.Abs(eff.Dir)
			if e != nil {
				return nil, e
			}
			if s.Replace != nil {
				s.Replace = &eff
			} else {
				s = eff
			}
		}
		eff = effective(s)
		if eff.Version != "" {
			if _, err := os.Lstat(filepath.Join(eff.Dir, ManifestName)); err == nil {
				if err = NoSymlinks(eff.GoMod); err != nil {
					return nil, err
				}
				cached, err := os.ReadFile(eff.GoMod)
				if err != nil {
					return nil, err
				}
				source, err := os.ReadFile(filepath.Join(eff.Dir, "go.mod"))
				if err != nil {
					return nil, err
				}
				if !bytes.Equal(cached, source) {
					return nil, fmt.Errorf("hgo module go.mod differs between selected metadata and verified source: %s", path)
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		if e = NoSymlinks(effective(s).Dir); e != nil {
			return nil, e
		}
		selected[path] = s
	}
	return selected, nil
}
func (g *Graph) plan(root string, requested []string, defaults bool) (*Graph, error) {
	var e error
	g.RootMod, e = ReadMod(g.Tool, root)
	if e != nil {
		return nil, e
	}
	selected := map[string]Selection{}
	states := map[string]bool{}
	previous := ""
	for round := 0; round < 64; round++ {
		r, e := g.round(root, requested, defaults, selected)
		if e != nil {
			return nil, e
		}
		next, e := g.solve(root, r)
		if e != nil {
			return nil, e
		}
		// Identity excludes cache/temporary directories; nodes/edges/features include
		// only this round's root-reachable manifest closure, never accumulated history.
		type stateNode struct {
			Module, Source string
			Features       []string
			Sources        map[string]Dependency
		}
		view := []stateNode{}
		for _, module := range keys(r.nodes) {
			n := r.nodes[module]
			view = append(view, stateNode{module, n.SourceID, keys(n.Features), n.Sources})
		}
		state := Digest(struct {
			Modules map[string]Selection
			Nodes   []stateNode
		}{next, view})
		if state == previous && Digest(next) == Digest(selected) {
			if len(r.errors) > 0 {
				return nil, r.errors[0]
			}
			g.Root = r.root
			g.Plan = next
			g.PlanID = Digest(next)
			state := map[string]int{}
			var visit func(*Node) error
			visit = func(n *Node) error {
				if state[n.Module] == 1 {
					return fmt.Errorf("dependency cycle at %s", n.Module)
				}
				if state[n.Module] == 2 {
					return nil
				}
				state[n.Module] = 1
				for _, alias := range keys(n.Children) {
					if e := visit(n.Children[alias]); e != nil {
						return e
					}
				}
				state[n.Module] = 2
				g.Nodes = append(g.Nodes, n)
				return nil
			}
			if e = visit(g.Root); e != nil {
				return nil, e
			}
			// Dependency target qualification is part of the stable graph, not
			// a late per-node error after an earlier script may have run.
			for _, n := range g.Nodes {
				if n == g.Root || !n.HasManifest {
					continue
				}
				if n.Manifest.Lib == nil {
					return nil, fmt.Errorf("hgo dependency %s must declare lib", n.Module)
				}
				for _, f := range n.Manifest.Lib.RequiredFeatures {
					if !n.Features[f] {
						return nil, fmt.Errorf("target %s requires feature %s", n.Manifest.Lib.Name, f)
					}
				}
			}
			// This key captures source declarations and active dependency constraints.
			// Feature-only cfg changes with an identical module plan may reuse the lock.
			type lockNode struct {
				Module, Root string
				Manifest     Manifest
				Mod          string
				Sources      map[string]Dependency
			}
			inputs := []lockNode{}
			for _, module := range keys(r.nodes) {
				n := r.nodes[module]
				raw, e := os.ReadFile(filepath.Join(n.Root, "go.mod"))
				if e != nil {
					return nil, e
				}
				idRoot := n.Root
				if n.SelectedVersion != "" {
					idRoot = n.Module
				}
				inputs = append(inputs, lockNode{n.Module, idRoot, n.Manifest, string(raw), n.Sources})
			}
			key := Digest(inputs)
			if g.Lock.Plans == nil {
				g.Lock.Plans = map[string]map[string]Selection{}
			}
			if old, ok := g.Lock.Plans[key]; g.Locked && (!ok || Digest(old) != Digest(next)) {
				return nil, fmt.Errorf("--locked: stable module plan missing or changed")
			}
			g.Lock.Plans[key] = next
			return g, nil
		}
		if states[state] {
			return nil, fmt.Errorf("hgo version/feature graph oscillates; no stable module plan")
		}
		states[state] = true
		previous = state
		selected = next
	}
	return nil, fmt.Errorf("hgo version/feature graph did not converge within 64 rounds")
}
