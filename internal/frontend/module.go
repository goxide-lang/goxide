package frontend

import (
	"bytes"
	"fmt"
	"go/build"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/typecheck"
)

// ModuleResult names the real Go module and the generated per-file artifacts.
type ModuleResult struct {
	Root, Target, ModulePath, SourceRoot string
	Files                                map[string]Result
}

// CompileModule copies an existing module to a fresh destination, lowers each
// package independently, then checks packages in import dependency order.
// Relative filesystem replacements are rebased to preserve their meaning.
func CompileModule(input, destination string) (ModuleResult, error) {
	input, err := filepath.Abs(input)
	if err != nil {
		return ModuleResult{}, err
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return ModuleResult{}, err
	}
	root := input
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return ModuleResult{}, fmt.Errorf("directory mode requires an existing go.mod")
		}
		root = parent
	}
	destination, err = canonicalOutput(destination)
	if err != nil {
		return ModuleResult{}, err
	}
	if rel, e := filepath.Rel(root, destination); e == nil && (rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		return ModuleResult{}, fmt.Errorf("output directory must be outside the input module")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return ModuleResult{}, fmt.Errorf("output directory must not exist: %s", destination)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ModuleResult{}, err
	}
	modulePath := ""
	lines := strings.Split(string(mod), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			modulePath = strings.Trim(fields[1], "\"")
		}
		if at := strings.Index(line, "=>"); at >= 0 {
			tail := strings.TrimSpace(line[at+2:])
			tokenText := strings.Fields(tail)
			if len(tokenText) > 0 {
				spelling := tokenText[0]
				if strings.HasPrefix(tail, "\"") {
					var err error
					spelling, err = strconv.QuotedPrefix(tail)
					if err != nil {
						return ModuleResult{}, fmt.Errorf("go.mod replacement: %w", err)
					}
				}
				path := spelling
				if strings.HasPrefix(spelling, "\"") {
					path, _ = strconv.Unquote(spelling)
				}
				if strings.HasPrefix(path, ".") {
					lines[i] = line[:at+2] + " " + strconv.Quote(filepath.ToSlash(filepath.Join(root, path))) + tail[len(spelling):]
				}
			}
		}
	}
	if modulePath == "" {
		return ModuleResult{}, fmt.Errorf("go.mod has no module directive")
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return ModuleResult{}, err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(destination)
		}
	}()
	packages := map[string]map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		// Worktrees can use a .git file rather than a directory. Neither belongs
		// in the independently buildable generated module.
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("module input symlinks are unsupported: %s", path)
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if _, e := os.Stat(filepath.Join(path, "go.mod")); e == nil {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0755)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if rel == "go.mod" {
			data = []byte(strings.Join(lines, "\n"))
		}
		ext := filepath.Ext(path)
		sourceDir := true
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if part != "." && (part == "testdata" || part == "vendor" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
				sourceDir = false
			}
		}
		hgoSource := ext == ".hgo" && sourceDir
		if hgoSource && (strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_")) {
			return fmt.Errorf("%s: ignored Go-style .hgo filename is unsupported", path)
		}
		if hgoSource {
			base := strings.TrimSuffix(entry.Name(), ext)
			if strings.HasSuffix(base, "_test") {
				return fmt.Errorf("%s: .hgo test files are not yet supported; use native Go tests", path)
			}
			if bytes.Contains(data, []byte("//go:build")) || bytes.Contains(data, []byte("// +build")) {
				return fmt.Errorf("%s: build constraints on .hgo files are not yet supported", path)
			}
			for _, suffix := range []string{"aix", "android", "darwin", "dragonfly", "freebsd", "illumos", "ios", "js", "linux", "netbsd", "openbsd", "plan9", "solaris", "wasip1", "windows", "386", "amd64", "arm", "arm64", "loong64", "mips", "mipsle", "mips64", "mips64le", "ppc64", "ppc64le", "riscv64", "s390x", "wasm"} {
				if strings.HasSuffix(base, "_"+suffix) {
					return fmt.Errorf("%s: platform suffix on .hgo files is unsupported", path)
				}
			}
			if _, e := os.Stat(strings.TrimSuffix(path, ".hgo") + ".go"); e == nil {
				return fmt.Errorf("%s: generated Go filename conflicts with native source", path)
			}
		}
		if hgoSource || sourceDir && ext == ".go" && !strings.HasSuffix(path, "_test.go") {
			selected := true
			if ext == ".go" {
				selected, e = build.Default.MatchFile(filepath.Dir(path), entry.Name())
				if e != nil {
					return e
				}
			}
			if selected {
				dir := filepath.Dir(rel)
				if packages[dir] == nil {
					packages[dir] = map[string][]byte{}
				}
				packages[dir][path] = data
			}
		}
		if hgoSource {
			return nil
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		return ModuleResult{}, err
	}
	result := ModuleResult{Root: destination, ModulePath: modulePath, SourceRoot: root, Files: map[string]Result{}}
	relTarget, _ := filepath.Rel(root, input)
	result.Target = "./" + filepath.ToSlash(relTarget)
	if relTarget == "." {
		result.Target = "."
	}
	metadata := map[string]PackageResult{}
	buildBudget := &macros.Budget{}
	state := map[string]int{}
	var compileDir func(string) error
	compileDir = func(dir string) error {
		if state[dir] == 2 {
			return nil
		}
		if state[dir] == 1 {
			return fmt.Errorf("module import cycle at %s", dir)
		}
		state[dir] = 1
		for name, source := range packages[dir] {
			f, e := parseSourceImports(name, source)
			if e != nil {
				return e
			}
			for _, spec := range f.Imports {
				imp, _ := strconv.Unquote(spec.Path.Value)
				dep := ""
				if imp == modulePath {
					dep = "."
				} else if strings.HasPrefix(imp, modulePath+"/") {
					dep = filepath.FromSlash(strings.TrimPrefix(imp, modulePath+"/"))
				} else {
					continue
				}
				if _, ok := packages[dep]; ok {
					if e := compileDir(dep); e != nil {
						return e
					}
				}
			}
		}
		pkgPath := modulePath
		if dir != "." {
			pkgPath += "/" + filepath.ToSlash(dir)
		}
		p, e := preparePackageContext(pkgPath, packages[dir], metadata, typecheck.NewImporter(filepath.Join(destination, dir, "package.go")), buildBudget)
		if e != nil {
			return e
		}
		checked, e := finishPackage(pkgPath, p, typecheck.NewImporter(filepath.Join(destination, dir, "package.go")))
		if e != nil {
			return e
		}
		for name, r := range checked.Files {
			if !strings.HasSuffix(name, ".hgo") {
				continue
			}
			rel, _ := filepath.Rel(root, name)
			goName := strings.TrimSuffix(rel, ".hgo") + ".go"
			if e = os.WriteFile(filepath.Join(destination, goName), r.Go, 0644); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(destination, goName+".map.json"), r.SourceMap, 0644); e != nil {
				return e
			}
			result.Files[goName] = r
		}
		metadata[pkgPath] = checked
		state[dir] = 2
		return nil
	}
	for _, dir := range sortedPackageDirs(packages) {
		if err = compileDir(dir); err != nil {
			return result, err
		}
	}
	success = true
	return result, nil
}
func sortedPackageDirs(packages map[string]map[string][]byte) []string {
	m := map[string][]byte{}
	for k := range packages {
		m[k] = nil
	}
	return sortedSources(m)
}

func canonicalOutput(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := path
	var suffix []string
	for {
		resolved, e := filepath.EvalSymlinks(parent)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", e
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
	}
}
