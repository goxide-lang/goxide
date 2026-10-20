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
	"sort"
	"strconv"
	"strings"
)

// NativeWorkspaceConfig preserves Go's work policy independently of member
// language versions and of the explicit authoring dependency graph.
type NativeWorkspaceConfig struct {
	Version       string            `toml:"go"`
	Toolchain     string            `toml:"toolchain,omitempty"`
	Godebug       []GoDebug         `toml:"godebug,omitempty"`
	Replace       []GoReplace       `toml:"replace,omitempty"`
	Configuration map[string]string `toml:"configuration"`
}
type NativeWorkspaceLock struct {
	LocalSources map[string]NativeMemberRecord `json:"local_sources,omitempty"`
	Members      map[string]NativeMemberRecord `json:"members,omitempty"`
	Input        string                        `json:"input"`
	Modules      map[string]Selection          `json:"modules"`
	Sums         map[string]string             `json:"sums"`
	WorkSum      string                        `json:"work_sum"`
	Vendor       *VendorSnapshot               `json:"vendor,omitempty"`
}
type GoWorkFile struct {
	Go, Toolchain string
	Godebug       []GoDebug
	Use           []struct{ DiskPath, ModulePath string }
	Replace       []ModReplace
}

func NativeGoEnv(work string, offline bool) []string {
	env := GoEnv(offline)
	for i, v := range env {
		if strings.HasPrefix(v, "GOWORK=") {
			env[i] = "GOWORK=" + work
		}
	}
	return env
}
func NativeGo(tool, dir string, offline bool, args ...string) ([]byte, error) {
	c := exec.Command(tool, args...)
	c.Dir = dir
	c.Env = NativeGoEnv(filepath.Join(dir, "go.work"), offline)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return nil, fmt.Errorf("native workspace Go %s: %w: %s", strings.Join(args, " "), e, stderr.String())
	}
	return b, nil
}
func NativeConfiguration(tool string) (map[string]string, error) {
	c := exec.Command(tool, "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOVERSION", "GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM", "GOEXPERIMENT", "GOFIPS140", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS", "PKG_CONFIG")
	c.Env = GoEnv(true)
	b, e := c.Output()
	if e != nil {
		return nil, e
	}
	var config map[string]string
	e = json.Unmarshal(b, &config)
	return config, e
}
func ReadGoWork(tool, root string) (GoWorkFile, error) {
	var w GoWorkFile
	file := filepath.Join(root, "go.work")
	if e := NoSymlinks(file); e != nil {
		return w, e
	}
	c := exec.Command(tool, "work", "edit", "-json", file)
	c.Dir = root
	c.Env = GoEnv(true)
	b, e := c.CombinedOutput()
	if e != nil {
		return w, fmt.Errorf("read go.work: %w: %s", e, b)
	}
	var raw map[string]json.RawMessage
	if e = json.Unmarshal(b, &raw); e != nil {
		return w, e
	}
	allowed := map[string]bool{"Go": true, "Toolchain": true, "Godebug": true, "Use": true, "Replace": true}
	for key, value := range raw {
		if !allowed[key] && string(value) != "null" && string(value) != "[]" && string(value) != "\"\"" {
			return w, fmt.Errorf("unsupported go.work directive %s", key)
		}
	}
	e = json.Unmarshal(b, &w)
	if e == nil && (w.Go == "" || len(w.Use) == 0) {
		e = fmt.Errorf("go.work requires explicit go and use members")
	}
	return w, e
}
func WorkPolicy(w GoWorkFile) *NativeWorkspaceConfig {
	p := &NativeWorkspaceConfig{Version: w.Go, Toolchain: w.Toolchain, Godebug: w.Godebug}
	for _, r := range w.Replace {
		x := GoReplace{Module: r.Old.Path, Version: r.Old.Version}
		if r.New.Version == "" {
			x.Path = r.New.Path
		} else {
			x.With = r.New.Path
			x.WithVersion = r.New.Version
		}
		p.Replace = append(p.Replace, x)
	}
	return p
}
func NativeInput(w *Workspace) (string, error) {
	root, e := os.ReadFile(filepath.Join(w.Root, ManifestName))
	if e != nil {
		return "", e
	}
	members := map[string]string{}
	for module, dir := range w.Members {
		b, e := os.ReadFile(filepath.Join(dir, ManifestName))
		if e != nil {
			return "", e
		}
		members[module] = string(b)
	}
	return Digest([]any{root, members}), nil
}
func NativeClosure(w *Workspace, module string) (map[string]bool, error) {
	allowed := map[string]bool{}
	state := map[string]int{}
	var visit func(string) error
	visit = func(module string) error {
		if state[module] == 1 {
			return fmt.Errorf("native workspace module dependency cycle at %s", module)
		}
		if state[module] == 2 {
			return nil
		}
		state[module] = 1
		dir, ok := w.Members[module]
		if !ok {
			return fmt.Errorf("unknown native workspace member %s", module)
		}
		m := w.Manifests[dir]
		for alias, d := range m.Dependencies {
			if d.IsToolchain() {
				continue
			}
			if _, member := w.Members[d.Module]; !member {
				if e := validateNativeExternalDependency(m, alias, d); e != nil {
					return e
				}
				continue
			}
			if d.Git != "" || d.Version != "" || d.Path == "" {
				return fmt.Errorf("native workspace dependency %s must be an explicit member path", alias)
			}
			path := d.Path
			if !filepath.IsAbs(path) {
				path = filepath.Join(dir, path)
			}
			if w.Members[d.Module] != filepath.Clean(path) {
				return fmt.Errorf("native workspace dependency %s must name its explicit member path", alias)
			}
			if e := visit(d.Module); e != nil {
				return e
			}
		}
		// Validate dormant forwarding too. External mappings name fixed native
		// artifacts; they never join the member feature-resolution graph.
		_, requests, e := Resolve(m, keys(m.Features), false)
		if e != nil {
			return e
		}
		for alias, request := range requests {
			d := m.Dependencies[alias]
			if _, member := w.Members[d.Module]; !member && !d.IsToolchain() && len(request.Features) != 0 {
				return fmt.Errorf("native workspace dependency %s: external Go artifacts cannot receive feature requests", alias)
			}
		}
		allowed[module] = true
		state[module] = 2
		return nil
	}
	e := visit(module)
	return allowed, e
}

// An external namespace only exposes an existing native requirement. Go's
// policy remains authoritative for versions and replacement/source selection;
// this mapping must not add a second version or an independent source policy.
func validateNativeExternalDependency(m Manifest, alias string, d Dependency) error {
	if d.Version == "" || d.Path != "" || d.Git != "" || d.Rev != "" || d.Subdir != "" {
		return fmt.Errorf("native workspace dependency %s: external roots require a version matching go.require; configure sources with go.replace", alias)
	}
	if len(d.Features) != 0 {
		return fmt.Errorf("native workspace dependency %s: external Go artifacts cannot receive feature requests", alias)
	}
	for _, requirement := range m.Native.Require {
		if requirement.Module == d.Module && requirement.Version == d.Version {
			return nil
		}
	}
	return fmt.Errorf("native workspace dependency %s: module/version must match this member's go.require for %s@%s", alias, d.Module, d.Version)
}

// CopyNativeSource never follows symlinks or enters nested module/managed trees.
func CopyNativeSource(src, dst string, workRoot ...string) error {
	if e := NoSymlinks(src); e != nil {
		return e
	}
	w, err := DiscoverWorkspace(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := naming.Path(path); err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return os.MkdirAll(dst, 0755)
		}
		if d.Name() == ".git" || d.Name() == ".goxide" || d.Name() == ".goxide-migration" || d.Name() == "goxide.lock.guard" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("native migration symlink unsupported: %s", path)
		}
		if d.IsDir() {
			if _, err := os.Lstat(filepath.Join(path, LayoutMarker)); err == nil {
				if len(workRoot) < 2 {
					return fmt.Errorf("unverified managed output marker: %s", path)
				}
				owned, err := ManagedSourceDirectory(path, src, workRoot[1])
				if err != nil {
					return err
				}
				if !owned {
					return fmt.Errorf("unverified managed output: %s", path)
				}
				return filepath.SkipDir
			} else if !os.IsNotExist(err) {
				return err
			}
			if _, err := os.Lstat(filepath.Join(path, ManifestName)); err == nil {
				if w != nil {
					if _, member := w.Manifests[path]; member {
						return filepath.SkipDir
					}
				}
				return fmt.Errorf("native migration refuses nested enhanced manifest: %s", path)
			} else if !os.IsNotExist(err) {
				return err
			}
			if _, err := os.Lstat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			} else if !os.IsNotExist(err) {
				return err
			}
			if d.Name() == "vendor" {
				if rel == "vendor" && len(workRoot) > 0 && src == workRoot[0] {
					return filepath.SkipDir
				}
				return fmt.Errorf("member vendor cannot be mixed with workspace vendor: %s", path)
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		switch rel {
		case "go.mod", "go.sum", "go.work", "go.work.sum", ManifestName, "goxide.lock":
			return nil
		}
		if w == nil && strings.HasSuffix(d.Name(), ".gox") {
			return fmt.Errorf("native workspace does not support enhanced source: %s", path)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("native migration special file: %s", path)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		to := filepath.Join(dst, rel)
		if e = os.MkdirAll(filepath.Dir(to), 0755); e != nil {
			return e
		}
		if e = os.WriteFile(to, b, info.Mode().Perm()); e != nil {
			return e
		}
		return os.Chmod(to, info.Mode().Perm())
	})
}
func NativeMode(w *Workspace) string { return "-mod=readonly" }
func WorkText(p *NativeWorkspaceConfig, members []string, root string) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "go %s\n", p.Version)
	if p.Toolchain != "" {
		fmt.Fprintf(&b, "toolchain %s\n", p.Toolchain)
	}
	// Reuse Go's accepted directive serializer, then omit its module/go header.
	m := Manifest{Module: "goxide-work-policy.invalid", GoVersion: p.Version, Toolchain: p.Toolchain, Native: GoConfig{Replace: append([]GoReplace(nil), p.Replace...), Godebug: p.Godebug}}
	{
		for i := range m.Native.Replace {
			r := &m.Native.Replace[i]
			if r.Path != "" && !filepath.IsAbs(r.Path) {
				r.Path = filepath.Join(root, r.Path)
			}
			if r.Path != "" {
				if e := NoSymlinks(r.Path); e != nil {
					return nil, e
				}
			}
		}
	}
	data, e := ModuleText(m)
	if e != nil {
		return nil, e
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "replace ") || strings.HasPrefix(line, "godebug ") {
			b.WriteString(line + "\n")
		}
	}
	sort.Strings(members)
	for _, dir := range members {
		fmt.Fprintf(&b, "use %s\n", strconv.Quote("./"+filepath.ToSlash(dir)))
	}
	return []byte(b.String()), nil
}

// NativeView preserves every member policy, but includes source only for the
// explicit build closure. Go use must never supply code from an unselected edge.
func NativeView(w *Workspace, dst string, source map[string]bool, lock *NativeWorkspaceLock) (map[string]string, error) {
	if e := os.MkdirAll(dst, 0755); e != nil {
		return nil, e
	}
	dirs := map[string]string{}
	uses := []string{}
	for _, module := range keys(w.Members) {
		src := w.Members[module]
		rel, e := filepath.Rel(w.Root, src)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("native workspace author members must be inside root")
		}
		dir := filepath.Join(dst, rel)
		if source[module] {
			if e = CopyNativeSource(src, dir, w.Root, w.Manifests[src].Module); e != nil {
				return nil, e
			}
		} else if e = os.MkdirAll(dir, 0755); e != nil {
			return nil, e
		}
		data, e := nativeModuleText(w, src, dst)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(dir, "go.mod"), data, 0644); e != nil {
			return nil, e
		}
		if sums := lock.Sums[module]; sums != "" {
			if e = os.WriteFile(filepath.Join(dir, "go.sum"), []byte(sums), 0644); e != nil {
				return nil, e
			}
		}
		dirs[module] = dir
		uses = append(uses, rel)
	}
	data, e := nativeWorkText(w, uses, dst)
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(dst, "go.work"), data, 0644); e != nil {
		return nil, e
	}
	if lock.WorkSum != "" {
		if e = os.WriteFile(filepath.Join(dst, "go.work.sum"), []byte(lock.WorkSum), 0644); e != nil {
			return nil, e
		}
	}
	return dirs, nil
}
func ReadNativeSelections(w *Workspace, tool, view string, lock *NativeWorkspaceLock, offline bool) error {
	lock.Modules = map[string]Selection{}
	{
		b, e := NativeGo(tool, view, offline, "list", "-mod=readonly", "-m", "-json", "all")
		if e != nil {
			return e
		}
		d := json.NewDecoder(bytes.NewReader(b))
		for {
			var s Selection
			e = d.Decode(&s)
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if _, member := w.Members[s.Path]; !member {
				lock.Modules[s.Path] = s
			}
		}
		if _, e = NativeGo(tool, view, offline, "mod", "verify"); e != nil {
			return e
		}
	}
	CaptureNativeMembers(w, lock)
	if e := captureNativeLocals(w, lock); e != nil {
		return e
	}
	return CaptureNativeSums(w, view, lock)
}
func CaptureNativeSums(w *Workspace, view string, lock *NativeWorkspaceLock) error {
	if e := CheckNativePolicies(w, view); e != nil {
		return e
	}
	lock.Sums = map[string]string{}
	for module, dir := range w.Members {
		rel, _ := filepath.Rel(w.Root, dir)
		b, e := os.ReadFile(filepath.Join(view, rel, "go.sum"))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		lock.Sums[module] = string(b)
	}
	b, e := os.ReadFile(filepath.Join(view, "go.work.sum"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	lock.WorkSum = string(b)
	return nil
}

func nativeModuleText(w *Workspace, src, view string) ([]byte, error) {
	m := w.Manifests[src]
	m.Native.Replace = append([]GoReplace(nil), m.Native.Replace...)
	{
		for i := range m.Native.Replace {
			r := &m.Native.Replace[i]
			if r.Path != "" && !filepath.IsAbs(r.Path) {
				r.Path = filepath.Join(src, r.Path)
			}
			if w.Members[r.Module] == filepath.Clean(r.Path) {
				rel, e := filepath.Rel(w.Root, r.Path)
				if e != nil {
					return nil, e
				}
				r.Path = filepath.Join(view, rel)
			}
			if r.Path != "" {
				if e := NoSymlinks(r.Path); e != nil {
					return nil, e
				}
			}
		}
	}
	return ModuleText(m)
}

// A Go analysis operation may add sums, but may never rewrite author policy.
func CheckNativePolicies(w *Workspace, view string) error {
	uses := []string{}
	check := func(path string, want []byte) error {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if !bytes.Equal(b, want) {
			return fmt.Errorf("Go operation changed native workspace policy: %s", path)
		}
		return nil
	}
	for _, module := range keys(w.Members) {
		src := w.Members[module]
		rel, e := filepath.Rel(w.Root, src)
		if e != nil {
			return e
		}
		uses = append(uses, rel)
		data, e := nativeModuleText(w, src, view)
		if e != nil {
			return e
		}
		if e = check(filepath.Join(view, rel, "go.mod"), data); e != nil {
			return e
		}
	}
	data, e := nativeWorkText(w, uses, view)
	if e != nil {
		return e
	}
	return check(filepath.Join(view, "go.work"), data)
}

// A local replacement of a use member follows that member into the generated
// view. Nonmember replacements retain their explicit external author path.
func nativeWorkText(w *Workspace, uses []string, view string) ([]byte, error) {
	policy := *w.Config.Go
	policy.Replace = append([]GoReplace(nil), policy.Replace...)
	{
		for i := range policy.Replace {
			r := &policy.Replace[i]
			if r.Path == "" {
				continue
			}
			absolute := r.Path
			if !filepath.IsAbs(absolute) {
				absolute = filepath.Join(w.Root, absolute)
			}
			if w.Members[r.Module] == filepath.Clean(absolute) {
				rel, e := filepath.Rel(w.Root, absolute)
				if e != nil {
					return nil, e
				}
				r.Path = filepath.Join(view, rel)
			}
		}
	}
	return WorkText(&policy, uses, w.Root)
}
