package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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
type ModRequire struct {
	Path, Version string
	Indirect      bool
}
type ModFile struct {
	Godebug   []GoDebug
	Toolchain string
	Module    ModuleVersion
	Go        string
	Require   []ModRequire
	Replace   []ModReplace
	Exclude   []ModuleVersion
}

func goCommand(tool, dir string, offline bool, environment []string, args ...string) ([]byte, error) {
	c := exec.Command(tool, args...)
	c.Dir = dir
	c.Env = GoEnv(offline, environment)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("Go module planning %s: %w: %s", args[0], e, strings.TrimSpace(stderr.String()))
	}
	return b, nil
}
func ReadMod(tool, root string, environment ...[]string) (ModFile, error) {
	var m ModFile
	info, e := os.Lstat(filepath.Join(root, "go.mod"))
	if e != nil || !info.Mode().IsRegular() {
		return m, fmt.Errorf("module directory must contain its own regular go.mod: %s", root)
	}
	var base []string
	if len(environment) != 0 {
		base = environment[0]
	}
	b, e := goCommand(tool, root, true, base, "mod", "edit", "-json")
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if e == nil && m.Module.Path == "" {
		e = fmt.Errorf("go.mod has no module directive")
	}
	return m, e
}

const plannerPrefix = "goxide-planner.invalid/"

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
func (g *Graph) readNode(root string, published bool, checkout string) (*Node, error) {
	present, e := Detect(root)
	if e != nil {
		return nil, e
	}
	n := &Node{Root: root, Children: map[string]*Node{}, Sources: map[string]Dependency{}}
	n.setSource(pathSource(root))
	if present {
		n.HasManifest = true
		if override, ok := g.overrides[root]; ok {
			n.Manifest = override
		} else {
			n.Manifest, e = readManifest(root, checkout)
		}
		manifestErr := e
		if e != nil && n.Manifest.Module == "" {
			return nil, e
		}
		if !published {
			if e = CheckAuthoring(root, g.Environment); e != nil {
				return nil, e
			}
		}
		n.Module = n.Manifest.Module
		n.ModBytes, e = ModuleText(n.Manifest)
		if e != nil {
			return nil, e
		}
		dir, e := os.MkdirTemp(g.WorkRoot, "input-")
		if e != nil {
			return nil, e
		}
		defer os.RemoveAll(dir)
		if e = os.WriteFile(filepath.Join(dir, "go.mod"), n.ModBytes, 0644); e != nil {
			return nil, e
		}
		n.Mod, e = ReadMod(g.Tool, dir, g.Environment)
		if e != nil {
			return nil, e
		}
		if published {
			if _, e := ImportMod(g.Tool, root, g.Environment); e != nil {
				return nil, fmt.Errorf("published Go transport metadata: %w", e)
			}
			transport, e := ReadMod(g.Tool, root, g.Environment)
			if e != nil {
				return nil, e
			}
			if !sameTransport(n.Mod, transport) {
				return nil, fmt.Errorf("published Go transport metadata conflicts with goxide.toml: %s", n.Module)
			}
		}
		if manifestErr != nil {
			return n, manifestErr
		}
	} else {
		n.Mod, e = ReadMod(g.Tool, root, g.Environment)
		if e != nil {
			return nil, e
		}
		n.Module = n.Mod.Module.Path
		n.ModBytes, e = os.ReadFile(filepath.Join(root, "go.mod"))
		if e != nil {
			return nil, e
		}
	}
	if g.nativeStandalone {
		if record, known := g.Lock.NativeWorkspace.Members[n.Module]; known {
			if g.input(n) != record.Input {
				return nil, fmt.Errorf("copied native lock changed reachable member %s; materialize its explicit policy", n.Module)
			}
			pin := g.forced[n.Module]
			pin.Replace = &Selection{Path: n.Root}
			g.forced[n.Module] = pin
		}
	}
	return n, nil
}

// Published source projects must not silently incorporate a previous goxide view.
func validatePublished(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := naming.Path(path); err != nil {
				return err
			}
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
		if name == "goxide_project_config.go" || name == ".goxide.refs.json" || name == ".goxide.enums.json" || name == ".goxide.uses.json" || name == ".goxide.protocols.json" || strings.HasSuffix(name, ".go.map.json") {
			return fmt.Errorf("published goxide source mixes generated artifacts: %s", path)
		}
		if strings.HasSuffix(name, ".gox") {
			if _, e := os.Lstat(strings.TrimSuffix(path, ".gox") + ".go"); e == nil {
				return fmt.Errorf("published goxide source has duplicate .goxide/.go ownership: %s", path)
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
	selected     map[string]Selection
	requirements []requirement
	local        map[string]string
	versioned    map[string]bool
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
	r := &planningRound{nodes: map[string]*Node{}, selected: selected}
	var e error
	r.root, e = g.readNode(root, false, "")
	if e != nil {
		return nil, e
	}
	owner := root
	if g.ownerRoot != "" {
		owner = g.ownerRoot
	}
	g.rootInputKey = r.root.lockSourceKey(owner)
	if g.Lock.Unified != nil && g.workspace == nil && !g.nativeStandalone {
		input := g.input(r.root)
		if g.Lock.Unified.Inputs[g.rootInputKey] != input {
			found := ""
			for key, value := range g.Lock.Unified.Inputs {
				if value != input {
					continue
				}
				if found != "" {
					return nil, fmt.Errorf("copied unified lock has ambiguous root input for %s", r.root.Module)
				}
				found = key
			}
			if found != "" {
				g.rootInputKey = found
			}
		}
	}
	r.nodes[r.root.SourceKey] = r.root
	reqs := map[string]Request{r.root.SourceKey: {Features: requested, Defaults: defaults}}
	deferred := map[string]error{}
	recordSource := func(n *Node) {
		n.SourceID = Digest([]string{n.lockSourceKey(owner), n.source.Content})
	}
	recordSource(r.root)
	for changed := true; changed; {
		changed = false
		for _, key := range keys(r.nodes) {
			n := r.nodes[key]
			req := unionRequest(reqs[key], g.featureOverrides[key])
			if g.allFeatures {
				req.Features = keys(n.Manifest.Features)
				req.Defaults = true
			}
			features, deps, e := Resolve(n.Manifest, req.Features, req.Defaults)
			if e != nil {
				if n == r.root {
					return nil, e
				}
				deferred[key] = e
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
			if g.Lock.Unified != nil && !g.unifiedPlanning {
				inputKey := n.lockSourceKey(owner)
				if n == r.root {
					inputKey = g.rootInputKey
				}
				if g.nativeStandalone {
					inputKey = n.Module
				}
				if g.Lock.Unified.Inputs[inputKey] != g.input(n) {
					return nil, fmt.Errorf("unified lock missing or changed reachable input %s; explicitly update lock", n.Module)
				}
			}
			for _, alias := range keys(deps) {
				d := n.Manifest.Dependencies[alias]
				if d.IsToolchain() {
					continue
				}
				dir, boundary, version := "", n.CheckoutRoot, ""
				source := sourceIdentity{}
				published := false
				if d.Version != "" {
					n.Sources[alias] = d
					s, ok := selected[d.Module]
					if !ok {
						continue
					}
					eff := effective(s)
					dir = eff.Dir
					version = s.Version
					if dir == "" {
						return nil, fmt.Errorf("selected source directory missing: %s", d.Module)
					}
					source = selectedSource(d.Module, s)
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
					boundary = filepath.Join(g.Cache, d.GitKey())
					source, e = gitIdentity(d.Git, g.Lock.Git[d.GitKey()], d.Subdir)
					if e != nil {
						return nil, e
					}
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
					source = pathSource(dir)
					if boundary != "" {
						relative, err := filepath.Rel(boundary, dir)
						if err != nil {
							return nil, err
						}
						source = n.source
						source.Subdir = filepath.ToSlash(relative)
						if source.Subdir == "." {
							source.Subdir = ""
						}
					}
				}
				if boundary != "" && !contained(boundary, dir) {
					return nil, fmt.Errorf("dependency path escapes checkout/acquired source boundary: %s", alias)
				}
				key := source.key()
				if d.Version != "" {
					if present, err := Detect(dir); err != nil {
						return nil, err
					} else if !present {
						if len(deps[alias].Features) > 0 {
							deferred[key] = fmt.Errorf("version dependency %s has no goxide source manifest; fixed Go artifacts cannot receive goxide features", alias)
						}
						continue
					}
				}
				child := r.nodes[key]
				if child == nil {
					child, e = g.readNode(dir, published, boundary)
					if e != nil {
						if child == nil {
							return nil, e
						}
						deferred[key] = e
					}
					child.setSource(source)
					child.CheckoutRoot = boundary
					child.SelectedVersion = version
					if published {
						if e = validatePublished(dir); e != nil {
							deferred[key] = e
						}
					}
					if boundary != "" {
						for _, rep := range child.Mod.Replace {
							if rep.New.Version == "" {
								p := rep.New.Path
								if !filepath.IsAbs(p) {
									p = filepath.Join(dir, p)
								}
								resolved, err := filepath.EvalSymlinks(p)
								if err != nil || !contained(boundary, resolved) {
									return nil, fmt.Errorf("acquired dependency go.mod replace escapes checkout/source boundary: %s", rep.Old.Path)
								}
							}
						}
					}
					r.nodes[key] = child
					changed = true
				}
				if d.Module != "" && child.Module != d.Module {
					return nil, fmt.Errorf("dependency %s module path mismatch: want %s, got %s", alias, d.Module, child.Module)
				}
				if g.nativeStandalone {
					if expected := g.Lock.NativeWorkspace.Members[n.Module].Dependencies[alias]; expected != "" && expected != child.Module {
						return nil, fmt.Errorf("copied native lock dependency %s module changed: want %s, got %s", alias, expected, child.Module)
					}
				}
				d.Module = child.Module
				n.Sources[alias] = d
				if version != "" {
					child.SelectedVersion = version
				}
				recordSource(child)
				n.Children[alias] = child
				prior := reqs[key]
				next := unionRequest(prior, deps[alias])
				if !reflect.DeepEqual(prior, next) {
					reqs[key] = next
					changed = true
				}
			}
		}
	}
	for _, key := range keys(deferred) {
		label := key
		if node := r.nodes[key]; node != nil {
			label = fmt.Sprintf("Go module %s (source %s)", node.Module, node.source.description())
		} else {
			for _, module := range keys(selected) {
				source := selectedSource(module, selected[module])
				if source.key() == key {
					label = fmt.Sprintf("Go module %s (source %s)", module, source.description())
					break
				}
			}
		}
		r.errors = append(r.errors, fmt.Errorf("%s: %w", label, deferred[key]))
	}
	return r, nil
}

// Go metadata becomes a unique module index only when constructing its projection.
func (g *Graph) projectRound(r *planningRound) (map[string]*Node, error) {
	modules := map[string]*Node{}
	r.local = map[string]string{}
	r.versioned = map[string]bool{}
	r.requirements = nil
	localRequirements := []requirement{}
	checkModule := func(module string) error {
		if strings.HasPrefix(module, plannerPrefix) {
			return fmt.Errorf("reserved planner module namespace: %s", module)
		}
		if g.Lock.Unified != nil && !g.unifiedPlanning && module != r.root.Module {
			if _, ok := g.Lock.Unified.Modules[module]; !ok {
				return fmt.Errorf("unified lock missing reachable module %s", module)
			}
		}
		return nil
	}
	for _, key := range keys(r.nodes) {
		n := r.nodes[key]
		if err := checkModule(n.Module); err != nil {
			return nil, err
		}
		if prior := modules[n.Module]; prior != nil && prior.SourceKey != n.SourceKey {
			return nil, fmt.Errorf("Go module projection has conflicting sources for module %s: %s and %s", n.Module, prior.source.description(), n.source.description())
		}
		modules[n.Module] = n
		for _, req := range n.Mod.Require {
			r.versioned[req.Path] = true
			if err := checkModule(req.Path); err != nil {
				return nil, err
			}
		}
		for _, alias := range keys(n.Sources) {
			d := n.Sources[alias]
			if err := checkModule(d.Module); err != nil {
				return nil, err
			}
			v := d.Version
			if v != "" {
				r.versioned[d.Module] = true
			}
			if v == "" {
				v = Placeholder(d.Module)
				if child := n.Children[alias]; child != nil {
					r.local[d.Module] = child.Root
				}
				localRequirements = append(localRequirements, requirement{d.Module, v})
			} else {
				r.requirements = append(r.requirements, requirement{d.Module, v})
			}
		}
	}
	for _, req := range localRequirements {
		if !r.versioned[req.Module] {
			r.requirements = append(r.requirements, req)
		}
	}
	for _, module := range keys(r.versioned) {
		selected, ok := r.selected[module]
		if !ok {
			continue
		}
		source := selectedSource(module, selected)
		if node := modules[module]; node != nil && node.SourceKey != source.key() {
			return nil, fmt.Errorf("Go module projection has conflicting sources for module %s: %s and %s", module, node.source.description(), source.description())
		}
	}
	return modules, nil
}

// solve delegates the complete version ordering and module graph to Go. Private
// local go.mod-only bridges preserve multiple constraints for one module path.
func (g *Graph) solve(root string, r *planningRound) (map[string]Selection, error) {
	modules, e := g.projectRound(r)
	if e != nil {
		return nil, e
	}
	work, e := os.MkdirTemp(g.WorkRoot, "solve-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(work)
	raw := r.root.ModBytes
	if e = os.WriteFile(filepath.Join(work, "go.mod"), raw, 0644); e != nil {
		return nil, e
	}
	if sums := []byte(g.planningSeed + g.Lock.PlanningSums); len(sums) > 0 {
		if e = os.WriteFile(filepath.Join(work, "go.sum"), sums, 0644); e != nil {
			return nil, e
		}
	}
	run := func(args ...string) ([]byte, error) {
		return goCommand(g.Tool, work, g.Offline, g.Environment, args...)
	}
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
	for _, req := range g.RootMod.Require {
		if pin, ok := g.forced[req.Path]; ok {
			args = append(args, "-require="+req.Path+"@"+pin.Version)
		}
	}
	for i, req := range r.requirements {
		if pin, ok := g.forced[req.Module]; ok {
			r.requirements[i].Version = pin.Version
		}
	}
	// Follow only native requirement edges reachable from this feature round.
	// Pre-pin the whole reachable native closure before Go can read older .mod
	// files. Unrelated workspace lock entries must never become Go roots.
	if g.Lock.Unified != nil && !g.unifiedPlanning {
		queue := []string{}
		for _, req := range g.RootMod.Require {
			queue = append(queue, req.Path)
		}
		for _, req := range r.requirements {
			queue = append(queue, req.Module)
		}
		for _, node := range r.nodes {
			for _, req := range node.Mod.Require {
				queue = append(queue, req.Path)
			}
		}
		seen := map[string]bool{}
		for len(queue) > 0 {
			module := queue[0]
			queue = queue[1:]
			if seen[module] || module == r.root.Module {
				continue
			}
			seen[module] = true
			pin, ok := g.forced[module]
			if !ok {
				return nil, fmt.Errorf("unified lock missing reachable module %s", module)
			}
			if g.nativeStandalone {
				var err error
				pin, err = g.bindNativeLocal(module, pin, replacements)
				if err != nil {
					return nil, err
				}
			}
			var mod ModFile
			if node := modules[module]; node != nil {
				mod = node.Mod
			} else {
				eff := effective(pin)
				if eff.Version == "" {
					node, err := g.readNode(eff.Path, false, "")
					if err != nil {
						return nil, err
					}
					owner := root
					if g.ownerRoot != "" {
						owner = g.ownerRoot
					}
					inputKey := node.lockSourceKey(owner)
					if g.nativeStandalone {
						inputKey = module
					}
					if want, exists := g.Lock.Unified.Inputs[inputKey]; exists && want != g.input(node) {
						return nil, fmt.Errorf("unified lock changed reachable input %s", module)
					}
					mod = node.Mod
				} else {
					// Explicit download verifies cached/fetched .mod against go.sum;
					// compare its identity to the root lock before following any edge.
					data, err := run("mod", "download", "-json", eff.Path+"@"+eff.Version)
					if err != nil {
						return nil, err
					}
					var verified Selection
					if err = json.Unmarshal(data, &verified); err != nil {
						return nil, err
					}
					if verified.Path != eff.Path || verified.Version != eff.Version || verified.GoModSum != eff.GoModSum || verified.Sum != eff.Sum || verified.GoMod == "" {
						return nil, fmt.Errorf("unified lock identity/checksum changed for %s", module)
					}
					if err = NoSymlinks(verified.GoMod); err != nil {
						return nil, err
					}
					body, err := os.ReadFile(verified.GoMod)
					if err != nil {
						return nil, err
					}
					metadata, err := os.MkdirTemp(work, "locked-mod-")
					if err != nil {
						return nil, err
					}
					if err = os.WriteFile(filepath.Join(metadata, "go.mod"), body, 0644); err != nil {
						return nil, err
					}
					mod, err = ReadMod(g.Tool, metadata, g.Environment)
					if err != nil {
						return nil, err
					}
				}
			}
			for _, child := range mod.Require {
				queue = append(queue, child.Path)
			}
			if pin.Version != "" {
				args = append(args, "-require="+module+"@"+pin.Version)
			}
		}
	}
	for _, x := range g.RootMod.Exclude {
		args = append(args, "-exclude="+x.Path+"@"+x.Version)
	}
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
		if r.versioned[module] {
			// Native selection determines whether an explicit local replacement is
			// effective. Do not synthesize a wildcard that could override its version.
			continue
		}
		for _, rep := range replacements {
			if rep.Old.Path == module && (rep.New.Version != "" || rep.New.Path != dir) {
				return nil, fmt.Errorf("manifest source conflicts with go.mod replace for %s", module)
			}
		}
		args = append(args, "-replace="+module+"="+dir)
	}
	// Go sees generated module-only projections of TOML authoring sources.
	shadows := map[string]string{}

	for _, rep := range replacements {
		if rep.New.Version != "" {
			continue
		}
		yes, err := Detect(rep.New.Path)
		if err != nil {
			return nil, err
		}
		if !yes {
			continue
		}
		n, err := g.readNode(rep.New.Path, false, "")
		if err != nil {
			return nil, err
		}
		shadow := filepath.Join(work, "replace-"+Digest([]string{rep.Old.Path, rep.Old.Version}))
		if e = os.Mkdir(shadow, 0755); e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(shadow, "go.mod"), n.ModBytes, 0644); e != nil {
			return nil, e
		}
		shadows[shadow] = rep.New.Path
		old := rep.Old.Path
		if rep.Old.Version != "" {
			old += "@" + rep.Old.Version
		}
		args = append(args, "-replace="+old+"="+shadow)
	}
	for module, dir := range r.local {
		if r.versioned[module] {
			continue
		}
		n := modules[module]
		if n == nil || !n.HasManifest {
			continue
		}
		shadow := filepath.Join(work, "source-"+Digest(module))
		if e = os.Mkdir(shadow, 0755); e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(shadow, "go.mod"), n.ModBytes, 0644); e != nil {
			return nil, e
		}
		shadows[shadow] = dir
		args = append(args, "-replace="+module+"="+shadow)
	}
	if len(args) > 2 {
		if _, e = run(args...); e != nil {
			return nil, e
		}
	}
	// All active goxide module constraints are main-module roots. A pruned Go
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
		path := fmt.Sprintf("goxide-planner.invalid/requirements/r%d", i)
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
		if _, e = goCommand(g.Tool, dir, g.Offline, g.Environment, "mod", "edit", "-require="+req.Module+"@"+req.Version); e != nil {
			return nil, e
		}
		if _, e = run("mod", "edit", "-require="+path+"@v0.0.0", "-replace="+path+"="+dir); e != nil {
			return nil, e
		}
	}
	// Generated modules promote every selected module to a main-module require.
	// With Go graph pruning, that promotion can expose additional dependencies
	// (notably through older go directives). Solve the same roots here before
	// locking/verifying the plan, rather than permitting unplanned modules later.
	// This closure starts fresh in every feature/source round; inactive historical
	// selections must never become roots of a subsequent round.
	var data []byte
	var decoder *json.Decoder
	selected := map[string]Selection{}
	previousNative := ""
	for closureRound := 0; ; closureRound++ {
		if closureRound == 64 {
			return nil, fmt.Errorf("promoted Go module graph did not converge within 64 rounds")
		}
		data, e = run("list", "-mod=mod", "-m", "-json", "all")
		if e != nil {
			return nil, e
		}
		selected = map[string]Selection{}
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
			if s.Path == r.root.Module || bridges[s.Path] {
				continue
			}
			selected[s.Path] = s
		}
		pinArgs := []string{"mod", "edit"}
		for _, path := range keys(selected) {
			candidate := selected[path]
			if pin, ok := g.forced[path]; ok && pin.Version != "" && candidate.Version != pin.Version {
				pinArgs = append(pinArgs, "-require="+path+"@"+pin.Version)
			}
		}
		if len(pinArgs) > 2 {
			if _, e = run(pinArgs...); e != nil {
				return nil, e
			}
			continue
		}
		nativeState := Digest(selected)
		if nativeState == previousNative {
			break
		}
		previousNative = nativeState
		roots := []string{"mod", "edit"}
		for _, path := range keys(selected) {
			roots = append(roots, "-require="+path+"@"+selected[path].Version)
		}
		if len(roots) > 2 {
			if _, e = run(roots...); e != nil {
				return nil, e
			}
		}
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
		if g.unifiedPlanning && parent != r.root.Module && !bridges[parent] {
			if dir, local := r.local[child]; local {
				selection, exists := selected[child]
				eff := effective(selection)
				location := eff.Dir
				if original, ok := shadows[location]; ok {
					location = original
				}
				if !exists || eff.Version != "" || location != dir {
					return nil, fmt.Errorf("native module %s requires %s by version; workspace membership cannot replace it; declare an effective root local replace", parent, child)
				}
			}
		}
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
					return nil, fmt.Errorf("goxide module go.mod differs between selected metadata and verified source: %s", path)
				}
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
		if e = NoSymlinks(effective(s).Dir); e != nil {
			return nil, e
		}
		if original, ok := shadows[effective(s).Dir]; ok {
			eff := effective(s)
			eff.Dir = original
			eff.Path = original
			eff.GoMod = ""
			if s.Replace != nil {
				s.Replace = &eff
			} else {
				s = eff
			}
		}
		selected[path] = s
	}
	if sums, err := os.ReadFile(filepath.Join(work, "go.sum")); err == nil {
		needed := map[string]bool{}
		for _, line := range strings.Split(string(graph), "\n") {
			for _, item := range strings.Fields(line) {
				if path, v, ok := strings.Cut(item, "@"); ok {
					needed[path+" "+v+"/go.mod"] = true
				}
			}
		}
		for _, s := range selected {
			eff := effective(s)
			if eff.Version != "" {
				needed[eff.Path+" "+eff.Version] = true
				needed[eff.Path+" "+eff.Version+"/go.mod"] = true
			}
		}
		var kept []string
		for _, line := range strings.Split(string(sums), "\n") {
			f := strings.Fields(line)
			if len(f) == 3 && needed[f[0]+" "+f[1]] {
				g.planningVerified[line] = true
			}
		}
		for line := range g.planningVerified {
			kept = append(kept, line)
		}
		sort.Strings(kept)
		g.Lock.PlanningSums = ""
		if len(kept) > 0 {
			g.Lock.PlanningSums = strings.Join(kept, "\n") + "\n"
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return selected, nil
}
func (g *Graph) plan(root string, requested []string, defaults bool) (*Graph, error) {
	g.planningSeed = g.Lock.PlanningSums
	g.planningVerified = map[string]bool{}
	if len(g.Lock.Plans) > 0 || g.Lock.Unified != nil {
		for _, line := range strings.Split(g.Lock.PlanningSums, "\n") {
			if line != "" {
				g.planningVerified[line] = true
			}
		}
	}

	var e error
	initial, err := g.readNode(root, false, "")
	if err != nil {
		return nil, err
	}
	g.RootMod = initial.Mod
	if g.workspace != nil && g.Lock.Unified != nil && !g.unifiedPlanning {
		g.RootMod.Replace = g.Lock.Unified.Policy.Replace
		g.RootMod.Exclude = g.Lock.Unified.Policy.Exclude
	}
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
			SourceKey, Module, Source string
			Features                  []string
			Sources                   map[string]Dependency
		}
		view := []stateNode{}
		for _, key := range keys(r.nodes) {
			n := r.nodes[key]
			view = append(view, stateNode{n.SourceKey, n.Module, n.SourceID, keys(n.Features), n.Sources})
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
			g.PlanID = Digest(g.portablePlan(root, next, r.nodes))
			state := map[string]int{}
			var visit func(*Node) error
			visit = func(n *Node) error {
				if state[n.SourceKey] == 1 {
					return fmt.Errorf("dependency cycle at %s", n.Module)
				}
				if state[n.SourceKey] == 2 {
					return nil
				}
				state[n.SourceKey] = 1
				for _, alias := range keys(n.Children) {
					if e := visit(n.Children[alias]); e != nil {
						return e
					}
				}
				state[n.SourceKey] = 2
				g.Nodes = append(g.Nodes, n)
				return nil
			}
			if e = visit(g.Root); e != nil {
				return nil, e
			}
			// Dependency target qualification is part of the stable graph, not
			// a late per-node error after an earlier script may have run.
			for _, n := range g.Nodes {
				if g.unifiedPlanning || n == g.Root || !n.HasManifest {
					continue
				}
				if n.Manifest.Lib == nil {
					return nil, fmt.Errorf("goxide dependency %s must declare lib", n.Module)
				}
				for _, f := range n.Manifest.Lib.RequiredFeatures {
					if !n.Features[f] {
						return nil, fmt.Errorf("target %s requires feature %s", n.Manifest.Lib.Name, f)
					}
				}
			}
			if g.unifiedPlanning {
				return g, nil
			}
			if g.Lock.Unified != nil {
				if e := g.validateUnified(); e != nil {
					return nil, e
				}
				return g, nil
			}
			// This key captures source declarations and active dependency constraints.
			// Feature-only cfg changes with an identical module plan may reuse the lock.
			type lockNode struct {
				SourceKey, Module string
				Manifest          Manifest
				Mod               string
				Sources           map[string]Dependency
			}
			owner := root
			if g.ownerRoot != "" {
				owner = g.ownerRoot
			}
			inputs := []lockNode{}
			for _, n := range r.nodes {
				inputs = append(inputs, lockNode{n.lockSourceKey(owner), n.Module, n.Manifest, string(n.ModBytes), n.Sources})
			}
			sort.Slice(inputs, func(i, j int) bool { return inputs[i].SourceKey < inputs[j].SourceKey })
			key := Digest(inputs)
			if g.Lock.Plans == nil {
				g.Lock.Plans = map[string]map[string]Selection{}
			}
			if old, ok := g.Lock.Plans[key]; g.Locked && (!ok || Digest(old) != Digest(g.portablePlan(root, next, r.nodes))) {
				return nil, fmt.Errorf("--locked: stable module plan missing or changed")
			}
			g.Lock.Plans[key] = g.portablePlan(root, next, r.nodes)
			return g, nil
		}
		if states[state] {
			return nil, fmt.Errorf("goxide version/feature graph oscillates; no stable module plan")
		}
		states[state] = true
		previous = state
		selected = next
	}
	return nil, fmt.Errorf("goxide version/feature graph did not converge within 64 rounds")
}
