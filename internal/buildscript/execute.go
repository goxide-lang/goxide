package buildscript

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hxsf/goxide/internal/lower"
)

const owner = "goxide-build-script-cache-v1\n"

func owned(root string, create bool) error {
	if e := checkComponents(root); e != nil {
		return e
	}
	if _, e := os.Lstat(root); os.IsNotExist(e) && create {
		if e = os.MkdirAll(root, 0700); e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(root, "owner"), []byte(owner), 0600)
	}
	i, e := os.Lstat(filepath.Join(root, "owner"))
	if e != nil {
		return fmt.Errorf("build cache lacks ownership marker: %w", e)
	}
	if e = regular(root, i); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(root, "owner"))
	if e != nil || string(b) != owner {
		return fmt.Errorf("build cache ownership mismatch: %s", root)
	}
	return nil
}

// Clean deletes only a verified owned cache. Optional root is reserved for caller-controlled artifact layout.
func Clean(projectRoot string, artifactRoot ...string) error {
	root := filepath.Join(projectRoot, ".goxide", "build-scripts")
	if len(artifactRoot) > 0 && artifactRoot[0] != "" {
		root = artifactRoot[0]
	}
	if _, e := os.Lstat(root); os.IsNotExist(e) {
		return nil
	}
	if e := owned(root, false); e != nil {
		return e
	}
	lock := filepath.Join(root, "lock")
	if e := os.Mkdir(lock, 0700); e != nil {
		return fmt.Errorf("build cache busy: %w", e)
	}
	defer os.Remove(lock)
	entries, e := os.ReadDir(root)
	if e != nil {
		return e
	}
	for _, d := range entries {
		if d.Name() == "owner" || d.Name() == "lock" {
			continue
		}
		if d.Name() != "entries" {
			return fmt.Errorf("unknown cache entry; refusing clean: %s", d.Name())
		}
		if e = checkComponents(filepath.Join(root, d.Name())); e != nil {
			return e
		}
	}
	return os.RemoveAll(filepath.Join(root, "entries"))
}
func writeFiles(root string, files map[string][]byte) error {
	for n, b := range files {
		if strings.HasSuffix(n, "/") {
			n = strings.TrimSuffix(n, "/")
			if e := safeRel(n); e != nil {
				return e
			}
			if e := os.MkdirAll(filepath.Join(root, n), 0700); e != nil {
				return e
			}
			continue
		}
		if e := safeRel(n); e != nil {
			return e
		}
		path := filepath.Join(root, n)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return e
		}
		if e := os.WriteFile(path, b, 0600); e != nil {
			return e
		}
	}
	return nil
}

type cappedLog struct {
	mu  sync.Mutex
	dst io.Writer
	n   int
}

func (l *cappedLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(b)
	if l.n < 1<<20 {
		take := min(n, (1<<20)-l.n)
		if l.dst != nil {
			_, _ = l.dst.Write(b[:take])
		}
		l.n += take
	}
	return n, nil
}
func (p *Plan) compileEnv() []string {
	env := []string{"GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0", "GOPROXY=off", "GONOPROXY=none", "GOSUMDB=off", "GOVCS=*:off", "GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH, "GOENV=off"}
	for _, n := range []string{"HOME", "USERPROFILE", "TMPDIR", "TEMP", "PATH", "GOCACHE"} {
		if v := os.Getenv(n); v != "" {
			env = append(env, n+"="+v)
		}
	}
	return env
}
func (p *Plan) run(ctx context.Context, dir string, env []string, log io.Writer, exe string, args ...string) error {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.WaitDelay = time.Second
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("build script command %s failed: %w", filepath.Base(exe), e)
	}
	return nil
}
func (p *Plan) Execute(ctx context.Context, authorize func() error, log io.Writer) (Artifact, error) {
	fail := Artifact{}
	if authorize == nil {
		return fail, fmt.Errorf("build script authorization callback required")
	}
	if e := authorize(); e != nil {
		return fail, e
	}
	root := p.options.ArtifactRoot
	if e := owned(root, true); e != nil {
		return fail, e
	}
	lock := filepath.Join(root, "lock")
	if e := os.Mkdir(lock, 0700); e != nil {
		return fail, fmt.Errorf("build script cache busy (remove stale lock only after checking no build is running): %w", e)
	}
	defer os.Remove(lock)
	entries := filepath.Join(root, "entries")
	if e := checkComponents(entries); e != nil {
		return fail, e
	}
	if e := os.MkdirAll(entries, 0700); e != nil {
		return fail, e
	}
	cache := filepath.Join(entries, p.ID)
	if e := checkComponents(cache); e != nil {
		return fail, e
	}
	if e := authorize(); e != nil {
		return fail, e
	}
	if _, e := os.Lstat(cache); e == nil {
		entries, e := os.ReadDir(cache)
		if e != nil {
			return fail, e
		}
		if len(entries) != 1 || entries[0].Name() != "artifact.json" {
			return fail, fmt.Errorf("invalid build cache entry")
		}
		info, e := os.Lstat(filepath.Join(cache, "artifact.json"))
		if e != nil {
			return fail, e
		}
		if e = regular(cache, info); e != nil {
			return fail, e
		}
		raw, e := readLimited(filepath.Join(cache, "artifact.json"), 192<<20)
		if e != nil {
			return fail, e
		}

		var a Artifact
		if e = json.Unmarshal(raw, &a); e != nil {
			return fail, e
		}
		if e = p.validateFiles(a.Files); e != nil {
			return fail, e
		}
		if a.Digest != digest(a.Files) {
			return fail, fmt.Errorf("build cache digest mismatch")
		}
		if e = authorize(); e != nil {
			return fail, e
		}
		a.CacheHit = true
		return a, nil
	} else if !os.IsNotExist(e) {
		return fail, e
	}
	scratch, e := os.MkdirTemp("", "goxide-build-script-")
	if e != nil {
		return fail, e
	}
	defer os.RemoveAll(scratch)
	timeout := p.options.Config.TimeoutSeconds
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	logger := &cappedLog{dst: log}
	if e = authorize(); e != nil {
		return fail, e
	}
	host := filepath.Join(scratch, "host")
	if e = writeHostFiles(host, p.script); e != nil {
		return fail, e
	}
	mods := make([]string, 0, len(p.helpers))
	for mod := range p.helpers {
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	paths := map[string]string{}
	for i, mod := range mods {
		paths[mod] = filepath.Join(scratch, "helper-"+strconv.Itoa(i))
		if e = writeHostFiles(paths[mod], p.helpers[mod]); e != nil {
			return fail, e
		}
	}
	modFile := func(module string) []byte {
		var b strings.Builder
		fmt.Fprintf(&b, "module %s\n\ngo 1.27\n", module)
		for _, m := range mods {
			if m == module {
				continue
			}
			version := "v0.0.0"
			last := m[strings.LastIndex(m, "/")+1:]
			if len(last) > 1 && last[0] == 'v' {
				if n, er := strconv.Atoi(last[1:]); er == nil && n >= 2 {
					version = last + ".0.0"
				}
			}
			fmt.Fprintf(&b, "require %s %s\nreplace %s => %s\n", m, version, m, strconv.Quote(paths[m]))
		}
		return []byte(b.String())
	}
	// Lower helpers in import order; all module files are reconstructed from the explicit local closure.
	for _, mod := range mods {
		if e = os.WriteFile(filepath.Join(paths[mod], "go.mod"), modFile(mod), 0600); e != nil {
			return fail, e
		}
	}
	imports := map[string][]string{}
	for _, m := range mods {
		imports[m], e = hostImports(m, p.helpers[m], p.options.ToolchainRoots)
		if e != nil {
			return fail, fmt.Errorf("host helper %s imports: %w", m, e)
		}
	}
	pending := map[string]bool{}
	for _, m := range mods {
		pending[m] = true
	}
	for len(pending) > 0 {
		progress := false
		for _, m := range mods {
			if !pending[m] {
				continue
			}
			blocked := false
			for other := range pending {
				if m == other {
					continue
				}
				for _, imported := range imports[m] {
					if importedHelper(imported, mods) == other {
						blocked = true
					}
				}
			}
			if blocked {
				continue
			}
			if hasGoxide(p.helpers[m]) {
				if e = lower.CompileModules([]lower.Module{{Path: m, Dir: paths[m], GoTool: p.options.GoTool, Roots: p.options.ToolchainRoots, Context: ctx, Environment: p.compileEnv()}}); e != nil {
					return fail, fmt.Errorf("host helper %s: %w", m, e)
				}
			}
			delete(pending, m)
			progress = true
		}
		if !progress {
			return fail, fmt.Errorf("host helper bootstrap/import cycle")
		}
	}
	if e = os.WriteFile(filepath.Join(host, "go.mod"), modFile("goxide.buildscript.local/host"), 0600); e != nil {
		return fail, e
	}
	if hasGoxide(p.script) {
		if e = lower.CompileModules([]lower.Module{{Path: "goxide.buildscript.local/host", Dir: host, GoTool: p.options.GoTool, Roots: p.options.ToolchainRoots, Context: ctx, Environment: p.compileEnv()}}); e != nil {
			return fail, fmt.Errorf("host script lowering (only explicit toolchain roots are available): %w", e)
		}
	}
	exe := filepath.Join(scratch, "generator")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if e = p.run(ctx, host, p.compileEnv(), logger, p.options.GoTool, "build", "-mod=mod", "-buildvcs=false", "-o", exe, "."); e != nil {
		return fail, e
	}
	source, out := filepath.Join(scratch, "inputs"), filepath.Join(scratch, "out")
	if e = os.MkdirAll(source, 0700); e != nil {
		return fail, e
	}
	for _, d := range p.inputDirs {
		if e = os.MkdirAll(filepath.Join(source, d), 0700); e != nil {
			return fail, e
		}
	}
	if e = writeFiles(source, p.inputs); e != nil {
		return fail, e
	}
	if e = os.MkdirAll(out, 0700); e != nil {
		return fail, e
	}
	req := Request{Version: 1, SourceDir: source, OutDir: out, HostOS: runtime.GOOS, HostArch: runtime.GOARCH, TargetOS: p.targetOS, TargetArch: p.targetArch, Features: p.options.Features, Tools: map[string]string{}, Env: p.env}
	for name, b := range p.tools {
		path := filepath.Join(scratch, "tools", name)
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return fail, e
		}
		if e = os.WriteFile(path, b, 0700); e != nil {
			return fail, e
		}
		req.Tools[name] = path
	}
	b, _ := json.Marshal(req)
	requestPath := filepath.Join(scratch, "request.json")
	if e = os.WriteFile(requestPath, b, 0600); e != nil {
		return fail, e
	}
	env := []string{"HOST=" + runtime.GOOS + "/" + runtime.GOARCH, "TARGET=" + p.targetOS + "/" + p.targetArch, "OUT_DIR=" + out, "SOURCE_DIR=" + source}
	for n, v := range p.env {
		env = append(env, n+"="+v)
	}
	if e = authorize(); e != nil {
		return fail, e
	}
	if e = p.run(ctx, source, env, logger, exe, requestPath); e != nil {
		return fail, e
	}
	if e = ctx.Err(); e != nil {
		return fail, e
	}
	files, e := snapshotMode(out, true)
	if e != nil {
		return fail, e
	}
	if e = p.validateFiles(files); e != nil {
		return fail, e
	}
	a := Artifact{Files: files, Digest: digest(files)}
	if e = authorize(); e != nil {
		return fail, e
	}
	if e = owned(root, false); e != nil {
		return fail, e
	}
	if e = checkComponents(entries); e != nil {
		return fail, e
	}
	stage, e := os.MkdirTemp(entries, "stage-")
	if e != nil {
		return fail, e
	}
	defer os.RemoveAll(stage)
	b, _ = json.Marshal(a)
	if e = os.WriteFile(filepath.Join(stage, "artifact.json"), b, 0600); e != nil {
		return fail, e
	}
	if e = ctx.Err(); e != nil {
		return fail, e
	}
	if e = authorize(); e != nil {
		return fail, e
	}
	if e = os.Rename(stage, cache); e != nil {
		return fail, e
	}
	if e = authorize(); e != nil {
		return fail, e
	}
	return a, nil
}
func hasGoxide(files map[string][]byte) bool {
	for n := range files {
		if strings.HasSuffix(n, ".gox") {
			return true
		}
	}
	return false
}
func (p *Plan) validateFiles(files map[string][]byte) error {
	want := map[string]bool{}
	for _, n := range p.options.Config.Outputs {
		want[n] = true
		if _, ok := files[n]; !ok {
			return fmt.Errorf("build script missing declared output %s", n)
		}
	}
	for n := range files {
		if err := naming.Path(n); err != nil {
			return err
		}
		if !want[n] {
			return fmt.Errorf("build script emitted undeclared output %s", n)
		}
	}
	return nil
}
