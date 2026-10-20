package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type UnifiedLock struct {
	GitPins   map[string]string    `json:"git_pins"`
	MinimumGo string               `json:"minimum_go"`
	Workspace string               `json:"workspace"`
	Sources   map[string]string    `json:"sources"`
	Policy    ModFile              `json:"policy"`
	Inputs    map[string]string    `json:"inputs"`
	Modules   map[string]Selection `json:"modules"`
}
type Lock struct {
	NativeWorkspace *NativeWorkspaceLock            `json:"native_workspace,omitempty"`
	Unified         *UnifiedLock                    `json:"unified,omitempty"`
	Vendor          *VendorSnapshot                 `json:"vendor,omitempty"`
	PlanningSums    string                          `json:"planning_sums,omitempty"`
	Provenance      string                          `json:"provenance,omitempty"`
	Version         int                             `json:"version"`
	Git             map[string]string               `json:"git"`
	Sums            map[string]string               `json:"sums"`
	Plans           map[string]map[string]Selection `json:"plans,omitempty"`
}

// UnmarshalJSON prevents pre-1.0 vendor artifacts from silently changing source policy.
func (l *Lock) UnmarshalJSON(data []byte) error {
	type plain Lock
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if _, ok := fields["vendor"]; ok {
		return fmt.Errorf("obsolete vendor lock: explicitly re-import original Go input as local modules")
	}
	if raw := fields["native_workspace"]; len(raw) > 0 {
		var native map[string]json.RawMessage
		if err := json.Unmarshal(raw, &native); err != nil {
			return err
		}
		if _, ok := native["vendor"]; ok {
			return fmt.Errorf("obsolete workspace vendor lock: explicitly re-import original Go input as local modules")
		}
	}
	return json.Unmarshal(data, (*plain)(l))
}

type Node struct {
	Mod             ModFile
	ModBytes        []byte
	Root, Module    string
	HasManifest     bool
	CheckoutRoot    string
	SourceKey       string
	SourceID        string
	source          sourceIdentity
	SelectedVersion string
	Manifest        Manifest
	Features        map[string]bool
	Requests        map[string]Request
	Children        map[string]*Node
	Sources         map[string]Dependency // Active module sources with resolved Module identities.
}
type Graph struct {
	nativeStandalone       bool
	featureOverrides       map[string]Request
	overrides              map[string]Manifest
	allFeatures            bool
	unifiedPlanning        bool
	forced                 map[string]Selection
	workspace              *Workspace
	ownerRoot, ownerModule string
	rootInputKey           string
	planningSeed           string
	planningVerified       map[string]bool
	WorkRoot               string
	Root                   *Node
	Nodes                  []*Node
	Lock                   Lock
	LockPath, Cache        string
	Offline, Locked        bool
	original               []byte
	Tool                   string
	Environment            []string
	Plan                   map[string]Selection
	PlanID                 string
	RootMod                ModFile
}

func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Load(root, tool string, requested []string, defaults, offline, locked bool) (*Graph, error) {
	return LoadWithTarget(root, tool, requested, defaults, offline, locked, "")
}
func LoadWithTarget(root, tool string, requested []string, defaults, offline, locked bool, targetDir string, environment ...[]string) (*Graph, error) {
	var captured []string
	if len(environment) != 0 {
		captured = append([]string{}, environment[0]...)
	} else {
		captured = os.Environ()
	}
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	if e = CheckAuthoring(root, captured); e != nil {
		return nil, e
	}
	w, e := DiscoverWorkspace(root)
	if e != nil {
		return nil, e
	}
	if w != nil {
		if _, ok := w.Manifests[root]; !ok {
			return nil, fmt.Errorf("workspace root requires explicit package selection")
		}
		if w.Config.Go != nil {
			return LoadNativeGraph(w, root, tool, requested, defaults, offline, locked, targetDir)
		}
		return loadWorkspaceGraph(w, root, tool, requested, defaults, offline, locked, targetDir, nil)
	}
	m, e := Read(root)
	if e != nil {
		return nil, e
	}
	layout, e := OpenLayout(root, m.Module, targetDir)
	if e != nil {
		return nil, e
	}
	work, e := os.MkdirTemp(layout.Root, ".plan-")
	if e != nil {
		layout.Close()
		return nil, e
	}
	defer os.RemoveAll(work)
	defer layout.Close()
	g := &Graph{WorkRoot: work, Tool: tool, Environment: captured, Offline: offline, Locked: locked, Cache: filepath.Join(layout.Root, "cache", "git"), LockPath: filepath.Join(root, "goxide.lock"), Lock: Lock{Version: 2, Git: map[string]string{}, Sums: map[string]string{}}}
	b, e := os.ReadFile(g.LockPath)
	if e == nil {
		if e = json.Unmarshal(b, &g.Lock); e != nil {
			return nil, e
		}
		if g.Lock.Version != 2 && g.Lock.Version != 3 && g.Lock.Version != 4 {
			return nil, fmt.Errorf("unsupported goxide.lock version")
		}
		g.original = b
	} else if !os.IsNotExist(e) {
		return nil, e
	} else if locked {
		return nil, fmt.Errorf("--locked requires goxide.lock")
	}
	if g.Lock.Git == nil {
		g.Lock.Git = map[string]string{}
	}
	if g.Lock.Sums == nil {
		g.Lock.Sums = map[string]string{}
	}
	if g.Lock.Version == 4 {
		if e = g.prepareNativeStandalone(); e != nil {
			return nil, e
		}
	}
	if g.Lock.Version == 3 {
		if g.Lock.Unified == nil {
			return nil, fmt.Errorf("version 3 lock missing unified records")
		}
		g.forced = g.Lock.Unified.Modules
	}
	return g.plan(root, requested, defaults)
}

func ModulePath(tool, root string) (string, error) {
	if yes, e := Detect(root); e != nil {
		return "", e
	} else if yes {
		m, e := Read(root)
		return m.Module, e
	}
	m, e := ReadMod(tool, root)
	return m.Module.Path, e
}
func GoEnv(offline bool, environment ...[]string) []string {
	var base []string
	if len(environment) != 0 {
		base = environment[0]
	}
	if base == nil {
		base = os.Environ()
	}
	env := []string{}
	for _, v := range base {
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
	if g.nativeStandalone {
		return nil
	} // Retain the complete copied root lock; reachable records were validated by plan.
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
		if Digest(before) != Digest(g.Lock) {
			return fmt.Errorf("--locked: dependency lock would change")
		}
		return nil
	}
	if current, err := os.ReadFile(g.LockPath); err == nil {
		if bytes.Equal(b, current) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, e := os.CreateTemp(filepath.Dir(g.LockPath), ".goxide-lock-*")
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
	if d.Tag != "" || d.Branch != "" {
		ref := d.gitRef()
		if _, e := git("", "check-ref-format", ref); e != nil {
			return "", fmt.Errorf("invalid git tag/branch: %w", e)
		}
	}
	key := d.GitKey()
	commit := g.Lock.Git[key]
	if g.Lock.Unified != nil {
		if expected, exists := g.Lock.Unified.GitPins[key]; exists && expected != commit {
			return "", fmt.Errorf("unified lock missing or changed git pin")
		}
	}
	if commit == "" && (g.Locked || g.Lock.Unified != nil && !g.unifiedPlanning) {
		return "", fmt.Errorf("--locked: git source absent from lock")
	}
	dir := filepath.Join(g.Cache, key)
	if e := NoSymlinks(dir); e != nil {
		return "", e
	}
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
			ref = d.gitRef()
		}
		c, e := git(tmp, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if e != nil && (commit != "" || d.Rev != "") {
			if _, e = git(tmp, "fetch", "--", "origin", ref); e == nil {
				c, e = git(tmp, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
			}
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
			want, err := git(dir, "rev-parse", "--verify", "--end-of-options", d.gitRef()+"^{commit}")
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
	if w, err := DiscoverWorkspace(root); err != nil {
		return nil, err
	} else if w != nil {
		root = w.Root
	}
	lock := filepath.Join(root, "goxide.lock.guard")
	if e = os.Mkdir(lock, 0700); e != nil {
		return nil, fmt.Errorf("project build lock unavailable (another build or stale lock): %w", e)
	}
	return func() { os.Remove(lock) }, nil
}

func (g *Graph) OwnerRoot() string {
	if g.ownerRoot != "" {
		return g.ownerRoot
	}
	return g.Root.Root
}
func (g *Graph) OwnerModule() string {
	if g.ownerModule != "" {
		return g.ownerModule
	}
	return g.Root.Module
}
