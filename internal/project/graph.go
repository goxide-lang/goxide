package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

type Lock struct {
	Version int               `json:"version"`
	Git     map[string]string `json:"git"`
	Sums    map[string]string `json:"sums"`
}
type Node struct {
	Root, Module string
	HasManifest  bool
	CheckoutRoot string
	Manifest     Manifest
	Features     map[string]bool
	Requests     map[string]Request
	Children     map[string]*Node
	Sources      map[string]Dependency
}
type Graph struct {
	Root             *Node
	Nodes            []*Node
	Lock             Lock
	LockPath, Cache  string
	Offline, Locked  bool
	original         []byte
	Tool             string
	NativeSelections map[string]string
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Load(root, tool string, requested []string, defaults, offline, locked bool) (*Graph, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	g := &Graph{Tool: tool, Offline: offline, Locked: locked, Cache: filepath.Join(root, ".hgo", "git"), LockPath: filepath.Join(root, "hgo.lock"), Lock: Lock{Version: 1, Git: map[string]string{}, Sums: map[string]string{}}}
	b, e := os.ReadFile(g.LockPath)
	if e == nil {
		if e = json.Unmarshal(b, &g.Lock); e != nil {
			return nil, e
		}
		if g.Lock.Version != 1 {
			return nil, fmt.Errorf("unsupported hgo.lock version")
		}
		g.original = b
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if locked {
		return nil, fmt.Errorf("--locked requires hgo.lock")
	}
	if g.Lock.Git == nil {
		g.Lock.Git = map[string]string{}
	}
	if g.Lock.Sums == nil {
		g.Lock.Sums = map[string]string{}
	}
	read := func(root string) (*Node, error) {
		_, manifestErr := os.Stat(filepath.Join(root, ManifestName))
		m, e := Read(root)
		if os.IsNotExist(e) {
			m = Manifest{Version: 1}
			e = nil
		}
		if e != nil {
			return nil, e
		}
		module, e := ModulePath(tool, root)
		if e != nil {
			return nil, e
		}
		return &Node{Root: root, Module: module, HasManifest: manifestErr == nil, Manifest: m, Children: map[string]*Node{}, Sources: map[string]Dependency{}}, nil
	}
	g.Root, e = read(root)
	if e != nil {
		return nil, e
	}
	nodes := map[string]*Node{g.Root.Module: g.Root}
	origins := map[string]string{g.Root.Module: root}
	requests := map[string]Request{g.Root.Module: {Features: requested, Defaults: defaults}}
	for changed := true; changed; {
		changed = false
		for _, module := range keys(nodes) {
			n := nodes[module]
			req := requests[module]
			features, deps, e := Resolve(n.Manifest, req.Features, req.Defaults)
			if e != nil {
				return nil, fmt.Errorf("%s: %w", module, e)
			}
			n.Features = features
			n.Requests = deps
			for _, alias := range keys(deps) {
				d := n.Manifest.Dependencies[alias]
				n.Sources[alias] = d
				if d.Version != "" {
					if prior, ok := origins[d.Module]; ok && prior != "go-module" {
						return nil, fmt.Errorf("conflicting sources for module %s", d.Module)
					}
					origins[d.Module] = "go-module"
					if len(deps[alias].Features) > 0 {
						return nil, fmt.Errorf("native version dependency %s cannot receive hgo features in v1; use path/git", alias)
					}
					continue
				}
				dir := d.Path
				boundary := n.CheckoutRoot
				if d.Git != "" {
					dir, e = g.gitSource(d)
					boundary = filepath.Join(g.Cache, Digest([]string{d.Git, d.Rev, d.Subdir, d.Module}))
					if e != nil {
						return nil, fmt.Errorf("dependency %s: %w", alias, e)
					}
				} else {
					if !filepath.IsAbs(dir) {
						dir = filepath.Join(n.Root, dir)
					}
					dir, e = filepath.EvalSymlinks(dir)
					if e != nil {
						return nil, e
					}
				}
				if boundary != "" {
					rel, err := filepath.Rel(boundary, dir)
					if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
						return nil, fmt.Errorf("git dependency path escapes checkout: %s", alias)
					}
				}
				sourceKey := dir
				if prior, ok := origins[d.Module]; ok && prior != sourceKey {
					return nil, fmt.Errorf("conflicting sources for module %s", d.Module)
				}
				child, ok := nodes[d.Module]
				if !ok {
					child, e = read(dir)
					if e != nil {
						return nil, e
					}
					if child.Module != d.Module {
						return nil, fmt.Errorf("dependency %s module path mismatch: want %s, got %s", alias, d.Module, child.Module)
					}
					child.CheckoutRoot = boundary
					nodes[d.Module] = child
					origins[d.Module] = sourceKey
					changed = true
				}
				n.Children[alias] = child
				r := requests[d.Module]
				old := Digest(r)
				r.Defaults = r.Defaults || deps[alias].Defaults
				set := map[string]bool{}
				for _, f := range append(r.Features, deps[alias].Features...) {
					set[f] = true
				}
				r.Features = keys(set)
				requests[d.Module] = r
				if Digest(r) != old {
					changed = true
				}
			}
		}
	}
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
	return g, nil
}
func ModulePath(tool, root string) (string, error) {
	cmd := exec.Command(tool, "mod", "edit", "-json")
	cmd.Dir = root
	cmd.Env = GoEnv(true)
	b, e := cmd.Output()
	if e != nil {
		return "", fmt.Errorf("read go.mod in %s: %w", root, e)
	}
	var v struct{ Module struct{ Path string } }
	if e = json.Unmarshal(b, &v); e != nil {
		return "", e
	}
	if v.Module.Path == "" {
		return "", fmt.Errorf("go.mod has no module directive")
	}
	return v.Module.Path, nil
}
func GoEnv(offline bool) []string {
	env := []string{}
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if k == "GOTOOLCHAIN" || k == "GOWORK" || k == "GOFLAGS" || (offline && (k == "GOPROXY" || k == "GOSUMDB" || k == "GONOPROXY" || k == "GOVCS")) {
			continue
		}
		env = append(env, v)
	}
	env = append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=")
	if offline {
		env = append(env, "GOPROXY=off", "GOSUMDB=off", "GONOPROXY=none", "GOVCS=*:off")
	}
	return env
}
func (g *Graph) Save() error {
	b, e := json.MarshalIndent(g.Lock, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if g.Locked {
		var before Lock
		if e = json.Unmarshal(g.original, &before); e != nil {
			return e
		}
		if !reflect.DeepEqual(before, g.Lock) {
			return fmt.Errorf("--locked: dependency lock would change")
		}
		return nil
	}
	tmp, e := os.CreateTemp(filepath.Dir(g.LockPath), ".hgo-lock-*")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, e = tmp.Write(b); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	return os.Rename(name, g.LockPath)
}
func validateGit(raw string) error {
	if filepath.IsAbs(raw) {
		return nil
	}
	u, e := url.Parse(raw)
	if e != nil {
		return fmt.Errorf("invalid git URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("git URL must not contain credentials, query, or fragment")
	}
	if u.Scheme == "file" && u.Host == "" && filepath.IsAbs(u.Path) {
		return nil
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("git source requires HTTPS URL or absolute local fixture path")
	}
	return nil
}
func git(dir string, args ...string) (string, error) {
	argv := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ext.allow=never", "-c", "protocol.file.allow=always", "-c", "credential.helper="}
	argv = append(argv, args...)
	cmd := exec.Command("git", argv...)
	cmd.Dir = dir
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if strings.HasPrefix(k, "GIT_") || k == "SSH_ASKPASS" || k == "SSH_ASKPASS_REQUIRE" {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")
	b, e := cmd.Output()
	if e != nil {
		return "", fmt.Errorf("git operation failed: %w", e)
	}
	return strings.TrimSpace(string(b)), nil
}
func (g *Graph) gitSource(d Dependency) (string, error) {
	if e := validateGit(d.Git); e != nil {
		return "", e
	}
	if strings.HasPrefix(d.Rev, "-") || strings.ContainsAny(d.Rev, "\x00\n\r") {
		return "", fmt.Errorf("invalid git revision")
	}
	key := Digest([]string{d.Git, d.Rev, d.Subdir, d.Module})
	commit := g.Lock.Git[key]
	if commit == "" && g.Locked {
		return "", fmt.Errorf("--locked: git source absent from lock")
	}
	for _, p := range []string{g.Cache, filepath.Join(g.Cache, key)} {
		if info, e := os.Lstat(p); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("git cache must not contain symlink directories")
		}
	}
	dir := filepath.Join(g.Cache, key)
	if _, e := os.Stat(filepath.Join(dir, ".git")); e != nil {
		if g.Offline {
			return "", fmt.Errorf("offline: git source is not cached")
		}
		if e = os.MkdirAll(g.Cache, 0755); e != nil {
			return "", e
		}
		tmp, e := os.MkdirTemp(g.Cache, "clone-")
		if e != nil {
			return "", e
		}
		defer os.RemoveAll(tmp)
		if _, e = git("", "clone", "--no-checkout", "--", d.Git, tmp); e != nil {
			return "", e
		}
		ref := commit
		if ref == "" {
			ref = d.Rev
		}
		c, e := git(tmp, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if e != nil && commit == "" {
			c, e = git(tmp, "rev-parse", "--verify", "--end-of-options", "refs/remotes/origin/"+ref+"^{commit}")
		}
		if e != nil {
			return "", e
		}
		if !isCommit(c) {
			return "", fmt.Errorf("invalid resolved commit")
		}
		if _, e = git(tmp, "checkout", "--detach", c); e != nil {
			return "", e
		}
		if e = os.Rename(tmp, dir); e != nil {
			return "", e
		}
		commit = c
	} else {
		actual, e := git(dir, "rev-parse", "HEAD")
		if e != nil {
			return "", e
		}
		if commit != "" && actual != commit {
			return "", fmt.Errorf("cached git checkout does not match locked commit")
		}
		if commit == "" {
			want, err := git(dir, "rev-parse", "--verify", "--end-of-options", d.Rev+"^{commit}")
			if err != nil {
				want, err = git(dir, "rev-parse", "--verify", "--end-of-options", "refs/remotes/origin/"+d.Rev+"^{commit}")
			}
			if err != nil || want != actual {
				return "", fmt.Errorf("unlocked cached checkout does not match requested revision")
			}
		}
		commit = actual
		// Cached sources are immutable, including untracked files.
		dirty, e := git(dir, "status", "--porcelain", "--untracked-files=all", "--ignored=matching")
		if e != nil {
			return "", e
		}
		if dirty != "" {
			return "", fmt.Errorf("cached git checkout has local modifications")
		}
	}
	g.Lock.Git[key] = commit
	dir = filepath.Join(dir, filepath.FromSlash(d.Subdir))
	resolved, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(filepath.Join(g.Cache, key), resolved)
	if e != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("git subdir escapes repository")
	}
	return resolved, nil
}
func isCommit(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}

// LockProject serializes lockfile and cache mutations. It does not claim to
// sandbox user code; contention fails instead of stealing a possibly live lock.
func LockProject(root string) (func(), error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	cache := filepath.Join(root, ".hgo")
	if info, e := os.Lstat(cache); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf(".hgo must not be a symlink")
	}
	if e = os.MkdirAll(cache, 0755); e != nil {
		return nil, e
	}
	lock := filepath.Join(cache, "build.lock")
	if e = os.Mkdir(lock, 0700); e != nil {
		return nil, fmt.Errorf("project build lock unavailable (another build or stale lock): %w", e)
	}
	return func() { os.Remove(lock) }, nil
}
