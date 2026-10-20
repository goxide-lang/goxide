package buildscript

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/project"
)

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func safeRel(s string) error {
	if s == "" || filepath.IsAbs(s) || strings.ContainsAny(s, "\\:\x00\r\n") || filepath.ToSlash(filepath.Clean(s)) != s || s == "." {
		return fmt.Errorf("invalid module-relative path %q", s)
	}
	for _, part := range strings.Split(s, "/") {
		if strings.TrimRight(part, ". ") != part {
			return fmt.Errorf("nonportable path %q", s)
		}
		if part == ".." || part == ".git" || part == ".goxide" || part == "vendor" {
			return fmt.Errorf("reserved or escaping path %q", s)
		}
	}
	return nil
}
func regular(path string, info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file (symlinks and special files forbidden): %s", path)
	}
	// FileInfo's platform structure exposes Nlink on Unix; avoid platform-specific compilation dependencies.
	v := reflect.ValueOf(info.Sys())
	if v.IsValid() && v.Kind() == reflect.Pointer {
		v = v.Elem()
		if v.Kind() == reflect.Struct {
			n := v.FieldByName("Nlink")
			if n.IsValid() && n.CanUint() && n.Uint() > 1 {
				return fmt.Errorf("hardlinked file forbidden: %s", path)
			}
		}
	}
	return nil
}
func checkComponents(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for at := path; ; at = filepath.Dir(at) {
		i, e := os.Lstat(at)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path forbidden: %s", at)
		}
		if filepath.Dir(at) == at {
			break
		}
	}
	return nil
}
func snapshotSource(root, moduleRoot, module string) (map[string][]byte, error) {
	if e := rejectManagedInput(root, moduleRoot, module); e != nil {
		return nil, e
	}
	return snapshotTree(root, false, moduleRoot, module)
}
func snapshotMode(root string, strict bool) (map[string][]byte, error) {
	return snapshotTree(root, strict, "", "")
}
func snapshotTree(root string, strict bool, moduleRoot, module string) (map[string][]byte, error) {
	if err := checkComponents(root); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			if err := naming.Path(path); err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(root, path)
		if !strict && (d.Name() == ".git" || d.Name() == ".goxide" || d.Name() == "vendor") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if !strict && moduleRoot != "" {
				managed, err := project.ManagedSourceDirectory(path, moduleRoot, module)
				if err != nil {
					return err
				}
				if managed {
					return filepath.SkipDir
				}
			}
			if !strict && rel != "." {
				if len(out) >= 10000 {
					return fmt.Errorf("snapshot entry limit exceeded: %s", path)
				}
				out[filepath.ToSlash(rel)+"/"] = nil
			}
			return nil
		}
		i, e := d.Info()
		if e != nil {
			return e
		}
		if e = regular(path, i); e != nil {
			return e
		}
		if len(out) >= 10000 || i.Size() > 32<<20 || total+i.Size() > 128<<20 {
			return fmt.Errorf("snapshot limit exceeded: %s", path)
		}
		b, e := readLimited(path, 32<<20)
		total += int64(len(b))
		if e != nil {
			return e
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	return out, err
}
func Prepare(o Options) (*Plan, error) {
	// The caller may reuse or mutate its manifest after preparation. Freeze configuration too.
	frozen, _ := json.Marshal(o)
	var copied Options
	if err := json.Unmarshal(frozen, &copied); err != nil {
		return nil, err
	}
	o = copied
	if o.ToolchainRoots == nil {
		o.ToolchainRoots = paths.Roots{}
	}
	for alias, root := range o.ToolchainRoots {
		if root.Source != "toolchain" || root.ModulePath != "" {
			return nil, fmt.Errorf("host root %s: only explicit toolchain roots are supported; target module mappings do not authorize host imports", alias)
		}
	}
	var err error
	o.ProjectRoot, err = filepath.Abs(o.ProjectRoot)
	if err != nil {
		return nil, err
	}
	o.PackageRoot, err = filepath.Abs(o.PackageRoot)
	if err != nil {
		return nil, err
	}
	if err = checkComponents(o.ProjectRoot); err != nil {
		return nil, err
	}
	if err = checkComponents(o.PackageRoot); err != nil {
		return nil, err
	}
	if o.ArtifactRoot == "" {
		o.ArtifactRoot = filepath.Join(o.ProjectRoot, ".goxide", "build-scripts")
	}
	o.ArtifactRoot, err = filepath.Abs(o.ArtifactRoot)
	if err != nil {
		return nil, err
	}
	if o.GoTool == "" {
		o.GoTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	o.GoTool, err = exec.LookPath(o.GoTool)
	if err != nil {
		return nil, err
	}
	o.GoTool, err = filepath.Abs(o.GoTool)
	if err != nil {
		return nil, err
	}
	p := &Plan{options: o, helpers: map[string]map[string][]byte{}, tools: map[string][]byte{}, inputs: map[string][]byte{}, env: map[string]string{}, targetOS: runtime.GOOS, targetArch: runtime.GOARCH}
	if s := os.Getenv("GOOS"); s != "" {
		p.targetOS = s
	}
	if s := os.Getenv("GOARCH"); s != "" {
		p.targetArch = s
	}
	if p.targetOS != runtime.GOOS || p.targetArch != runtime.GOARCH {
		return nil, fmt.Errorf("build script target %s/%s: frontend currently requires host %s/%s; cross compilation unsupported", p.targetOS, p.targetArch, runtime.GOOS, runtime.GOARCH)
	}
	if err = safeRel(o.Config.Entry); err != nil {
		return nil, err
	}
	ext := filepath.Ext(o.Config.Entry)
	if ext != ".go" && ext != ".gox" {
		return nil, fmt.Errorf("build entry must be .go or .gox (canonical build script: build.gox)")
	}
	if filepath.Dir(o.Config.Entry) == "." {
		return nil, fmt.Errorf("build entry must live in a dedicated subdirectory, not module root")
	}
	if err = checkComponents(filepath.Join(o.PackageRoot, o.Config.Entry)); err != nil {
		return nil, err
	}
	i, err := os.Lstat(filepath.Join(o.PackageRoot, o.Config.Entry))
	if err != nil {
		return nil, err
	}
	if err = regular(o.Config.Entry, i); err != nil {
		return nil, err
	}
	if len(o.Config.Inputs)+len(o.Config.Outputs)+len(o.Config.Dependencies)+len(o.Config.Tools) > 10000 {
		return nil, fmt.Errorf("build script declaration limit exceeded")
	}
	p.script, err = snapshotSource(filepath.Join(o.PackageRoot, filepath.Dir(o.Config.Entry)), o.PackageRoot, o.Module)
	if err != nil {
		return nil, err
	}
	for n := range p.script {
		if filepath.Base(n) == "go.mod" || filepath.Base(n) == "goxide.toml" {
			return nil, fmt.Errorf("build script directory must not contain module/manifest: %s", n)
		}
	}
	if len(o.Config.Outputs) == 0 {
		return nil, fmt.Errorf("build script requires explicit outputs")
	}
	seen := map[string]bool{}
	for _, n := range o.Config.Outputs {
		if err := naming.Path(n); err != nil {
			return nil, err
		}
		if err = safeRel(n); err != nil {
			return nil, err
		}
		fold := strings.ToLower(n)
		if seen[fold] {
			return nil, fmt.Errorf("duplicate output %s", n)
		}
		for prior := range seen {
			if strings.HasPrefix(fold, prior+"/") || strings.HasPrefix(prior, fold+"/") {
				return nil, fmt.Errorf("overlapping output paths %s", n)
			}
		}
		seen[fold] = true
		if strings.HasSuffix(strings.ToLower(n), ".go.map.json") || strings.Split(n, "/")[0] == "goxide_targets" || filepath.Base(n) == "goxide_project_config.go" || filepath.Base(n) == "go.mod" || filepath.Base(n) == "go.sum" || filepath.Base(n) == "goxide.toml" || filepath.Base(n) == "goxide.lock" {
			return nil, fmt.Errorf("reserved output %s", n)
		}
		path := filepath.Join(o.PackageRoot, n)
		if err = checkComponents(path); err != nil {
			return nil, err
		}
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			return nil, fmt.Errorf("output would overwrite source: %s", n)
		}
	}
	for _, n := range o.Config.Inputs {
		if err = safeRel(n); err != nil {
			return nil, err
		}
		path := filepath.Join(o.PackageRoot, n)
		if err = checkComponents(path); err != nil {
			return nil, err
		}
		if err = withinBoundary(o.Boundary, path); err != nil {
			return nil, err
		}
		i, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if e = rejectManagedInput(path, o.PackageRoot, o.Module); e != nil {
			return nil, e
		}
		if i.IsDir() {
			files, e := snapshotSource(path, o.PackageRoot, o.Module)
			if e != nil {
				return nil, e
			}
			p.inputDirs = append(p.inputDirs, n)
			for k, v := range files {
				p.inputs[n+"/"+k] = v
			}

		} else {
			if e = regular(path, i); e != nil {
				return nil, e
			}
			p.inputs[n], e = readLimited(path, 32<<20)
			if e != nil {
				return nil, e
			}
		}
		if err = p.checkSize(); err != nil {
			return nil, err
		}
	}
	if err = p.checkSize(); err != nil {
		return nil, err
	}
	for _, n := range o.Config.Env {
		if n == "" || strings.ContainsAny(n, "=\x00") || reservedEnv(n) {
			return nil, fmt.Errorf("invalid environment name %q", n)
		}
		if v, ok := os.LookupEnv(n); ok {
			p.env[n] = v
		}
	}
	for mod, path := range o.Config.Dependencies {
		if mod == "" || strings.ContainsAny(mod, " \t\r\n\"\\") || strings.HasPrefix(mod, ".") {
			return nil, fmt.Errorf("invalid helper module %q", mod)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(o.PackageRoot, path)
		}
		if e := withinBoundary(o.Boundary, path); e != nil {
			return nil, e
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		files, e := snapshotSource(path, path, mod)
		if e != nil {
			return nil, e
		}
		if _, e := project.Detect(path); e != nil {
			return nil, e
		}
		if b, ok := files[project.ManifestName]; ok {
			m, e := project.Parse(filepath.Join(path, project.ManifestName), b)
			if e != nil {
				return nil, e
			}
			if e = project.CheckAuthoring(path); e != nil {
				return nil, e
			}
			files["go.mod"], e = project.ModuleText(m)
			if e != nil {
				return nil, e
			}
			if m.Build != nil {
				return nil, fmt.Errorf("helper %s declares build script: bootstrap scripts unsupported", mod)
			}
		}
		if e = validateModule(mod, files, o.Config.Dependencies); e != nil {
			return nil, e
		}
		p.helpers[mod] = files
		if err = p.checkSize(); err != nil {
			return nil, err
		}
		p.Subjects = append(p.Subjects, Subject{digest(struct {
			Module         string
			Files          map[string][]byte
			ToolchainRoots paths.Roots
		}{mod, files, o.ToolchainRoots}), "host helper " + mod})
	}
	for alias, path := range o.Config.Tools {
		if safeRel(alias) != nil || strings.ContainsAny(alias, "/\\\x00") {
			return nil, fmt.Errorf("invalid tool alias %q", alias)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(o.PackageRoot, path)
		}
		if err = checkComponents(path); err != nil {
			return nil, err
		}
		if err = withinBoundary(o.Boundary, path); err != nil {
			return nil, err
		}
		i, e := os.Lstat(path)
		if e != nil {
			return nil, e
		}
		if e = regular(path, i); e != nil {
			return nil, e
		}
		b, e := readLimited(path, 32<<20)
		if e != nil {
			return nil, e
		}
		p.tools[alias] = b
		if err = p.checkSize(); err != nil {
			return nil, err
		}
		p.Subjects = append(p.Subjects, Subject{digest(struct {
			Name  string
			Bytes []byte
		}{alias, b}), "host tool " + alias})
	}
	p.options.Features = append([]string(nil), o.Features...)
	p.options.Tags = append([]string(nil), o.Tags...)
	sort.Strings(p.inputDirs)
	sort.Strings(p.options.Features)
	sort.Strings(p.options.Tags)
	tool, err := os.ReadFile(o.GoTool)
	if err != nil {
		return nil, err
	}
	p.RootIdentity = digest([]string{o.ProjectRoot, o.Module})
	p.Subject = digest(struct {
		Config         Config
		ToolchainRoots paths.Roots
		SourceID       string
		Script, Inputs map[string][]byte
		Helpers        map[string]map[string][]byte
		Tools          map[string][]byte
	}{o.Config, o.ToolchainRoots, o.SourceID, p.script, p.inputs, p.helpers, p.tools})
	if !o.Root {
		p.Subjects = append(p.Subjects, Subject{p.Subject, "dependency build script " + o.Module + " (execution closure)"})
	}
	sort.Slice(p.Subjects, func(i, j int) bool { return p.Subjects[i].ID < p.Subjects[j].ID })
	p.ID = digest(struct {
		InputDirs      []string
		GoRuntime      string
		Options        Options
		Script, Inputs map[string][]byte
		Helpers        map[string]map[string][]byte
		Tools          map[string][]byte
		Env            map[string]string
		GoTool         string
		Host, Target   string
	}{p.inputDirs, runtime.Version() + "/" + runtime.GOROOT(), p.options, p.script, p.inputs, p.helpers, p.tools, p.env, digest(tool), runtime.GOOS + "/" + runtime.GOARCH, p.targetOS + "/" + p.targetArch})
	return p, nil
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file exceeds snapshot limit: %s", path)
	}
	return b, e
}
func reservedEnv(n string) bool {
	n = strings.ToUpper(n)
	if strings.HasPrefix(n, "HGO_") || strings.HasPrefix(n, "GO") || strings.HasPrefix(n, "CGO_") {
		return true
	}
	switch n {
	case "PATH", "HOME", "USERPROFILE", "TMPDIR", "TEMP", "TMP", "HOST", "TARGET", "OUT_DIR", "SOURCE_DIR", "LD_PRELOAD", "LD_LIBRARY_PATH", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH":
		return true
	}
	return false
}
func withinBoundary(boundary, path string) error {
	if boundary == "" {
		return nil
	}
	b, e := filepath.Abs(boundary)
	if e != nil {
		return e
	}
	p, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(b, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("host input escapes dependency source boundary: %s", path)
	}
	return nil
}
func validateModule(mod string, files map[string][]byte, deps map[string]string) error {
	b, ok := files["go.mod"]
	if !ok {
		return fmt.Errorf("host helper %s requires go.mod", mod)
	}
	mode := ""
	found := false
	for _, line := range strings.Split(string(b), "\n") {
		line, _, _ = strings.Cut(line, "//")
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == ")" {
			mode = ""
			continue
		}
		kind := mode
		if kind == "" {
			kind = f[0]
			f = f[1:]
		}
		if len(f) == 1 && f[0] == "(" {
			mode = kind
			continue
		}
		switch kind {
		case "module":
			if len(f) != 1 || strings.Trim(f[0], "\"") != mod {
				return fmt.Errorf("helper module identity mismatch for %s", mod)
			}
			found = true
		case "go":
		case "require":
			if len(f) < 2 {
				return fmt.Errorf("invalid helper require in %s", mod)
			}
			if _, ok := deps[strings.Trim(f[0], "\"")]; !ok {
				return fmt.Errorf("helper %s needs undeclared host dependency %s", mod, f[0])
			}
		default:
			return fmt.Errorf("helper %s: go.mod directive %s unsupported; use explicit host dependencies", mod, kind)
		}
	}
	if !found {
		return fmt.Errorf("helper %s missing module identity", mod)
	}
	for n := range files {
		if filepath.Dir(n) != "." && (filepath.Base(n) == "go.mod" || filepath.Base(n) == project.ManifestName) {
			return fmt.Errorf("nested host modules unsupported: %s/%s", mod, n)
		}
	}
	return nil
}

func (p *Plan) checkSize() error {
	count, total := 0, 0
	add := func(m map[string][]byte) {
		for _, b := range m {
			count++
			total += len(b)
		}
	}
	add(p.script)
	add(p.inputs)
	add(p.tools)
	for _, m := range p.helpers {
		add(m)
	}
	if count > 10000 || total > 128<<20 {
		return fmt.Errorf("build script snapshot exceeds 10000 entries or 128 MiB")
	}
	return nil
}

// Explicit source selections must not name a managed layout or anything inside
// one. Descendant layouts encountered during a source walk are validated and
// skipped instead; this keeps build products out of input fingerprints.
func rejectManagedInput(path, moduleRoot, module string) error {
	if e := project.RejectManagedOutput(path); e != nil {
		return fmt.Errorf("managed artifacts cannot be build script inputs: %w", e)
	}
	info, e := os.Stat(path)
	if e != nil {
		return e
	}
	if !info.IsDir() {
		return nil
	}
	managed, e := project.ManagedSourceDirectory(path, moduleRoot, module)
	if e != nil {
		return e
	}
	if managed {
		return fmt.Errorf("managed artifacts cannot be build script inputs: %s", path)
	}
	return nil
}
