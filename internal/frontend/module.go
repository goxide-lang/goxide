package frontend

import (
	"bytes"
	"fmt"
	"go/build"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/refs"
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
	return CompileModuleWithTags(input, destination, nil)
}

// CompileModuleWithTags selects hgo and native files using the same host Go build
// context. The generated module is a view of this one explicit configuration.
func CompileModuleWithTags(input, destination string, tags []string) (ModuleResult, error) {
	return CompileModuleWithOptions(input, destination, ModuleOptions{Tags: tags})
}

type ModuleOptions struct {
	Tags []string
	Race bool
}

func CompileModuleWithOptions(input, destination string, options ModuleOptions) (ModuleResult, error) {
	ctx := build.Default
	ctx.BuildTags = append([]string(nil), options.Tags...)
	if options.Race {
		ctx.BuildTags = append(ctx.BuildTags, "race")
	}
	for key, dst := range map[string]*string{"GOOS": &ctx.GOOS, "GOARCH": &ctx.GOARCH} {
		if v := os.Getenv(key); v != "" {
			*dst = v
		}
	}
	if ctx.GOOS != runtime.GOOS || ctx.GOARCH != runtime.GOARCH {
		return ModuleResult{}, fmt.Errorf("directory mode currently requires host GOOS/GOARCH (%s/%s); cross-target type sizes are unsupported", runtime.GOOS, runtime.GOARCH)
	}
	if v := os.Getenv("CGO_ENABLED"); v != "" {
		ctx.CgoEnabled = v == "1"
	}

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
		if hgoSource && strings.HasSuffix(strings.TrimSuffix(entry.Name(), ext), "_test") {
			return fmt.Errorf("%s: .hgo test files are not yet supported; use native Go tests", path)
		}
		if hgoSource || sourceDir && ext == ".go" && !strings.HasSuffix(path, "_test.go") {
			selected, err := matchSourceFile(ctx, path, data)
			if err != nil {
				return err
			}
			if selected {
				if hgoSource {
					if _, err := os.Stat(strings.TrimSuffix(path, ".hgo") + ".go"); err == nil {
						return fmt.Errorf("%s: generated Go filename conflicts with native source", path)
					}
				}
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
		for _, name := range sortedSources(packages[dir]) {
			source := packages[dir][name]
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
		packageImporter := typecheck.NewImporterWithBuild(filepath.Join(destination, dir, "package.go"), options.Tags, options.Race)
		p, e := preparePackageContext(pkgPath, packages[dir], metadata, packageImporter, buildBudget)
		if e != nil {
			return e
		}
		checked, e := finishPackage(pkgPath, p, packageImporter)
		if e != nil {
			return e
		}
		for name, r := range checked.Files {
			// Native Go files only change when carrying a transparent-alias marker.
			if !strings.HasSuffix(name, ".hgo") && checked.enumMarkerFile != name {
				continue
			}
			rel, _ := filepath.Rel(root, name)
			goName := rel
			if strings.HasSuffix(rel, ".hgo") {
				goName = strings.TrimSuffix(rel, ".hgo") + ".go"
			}
			if e = os.WriteFile(filepath.Join(destination, goName), r.Go, 0644); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(destination, goName+".map.json"), r.SourceMap, 0644); e != nil {
				return e
			}
			result.Files[goName] = r
		}

		if len(checked.references.Functions) > 0 || len(checked.enumMetadata.Enums) > 0 {
			sourceFiles := map[string][]byte{}
			for name := range packages[dir] {
				base := filepath.Base(name)
				if strings.HasSuffix(base, ".hgo") {
					base = strings.TrimSuffix(base, ".hgo") + ".go"
				}
				data, e := os.ReadFile(filepath.Join(destination, dir, base))
				if e != nil {
					return e
				}
				sourceFiles[base] = data
			}
			if len(checked.references.Functions) > 0 {
				data, e := refs.MarshalMetadataArtifact(checked.references, sourceFiles, packageImporter)
				if e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(destination, dir, refs.MetadataFilename), data, 0644); e != nil {
					return e
				}
			}
			if len(checked.enumMetadata.Enums) > 0 {
				provider, ok := packageImporter.(refs.ArtifactImporter)
				if !ok {
					return fmt.Errorf("enum artifact source importer unavailable")
				}
				data, e := marshalEnumArtifact(checked.enumMetadata, sourceFiles, provider)
				if e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(destination, dir, enumMetadataFilename), data, 0644); e != nil {
					return e
				}
			}
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

// MatchFile understands only Go extensions. Present hgo bytes through a virtual
// .go filename, preserving suffix selection and the standard constraint parser.
func matchSourceFile(ctx build.Context, path string, source []byte) (bool, error) {
	name := filepath.Base(path)
	if strings.HasSuffix(name, ".hgo") {
		name = strings.TrimSuffix(name, ".hgo") + ".go"
	}
	virtual := filepath.Join(filepath.Dir(path), name)
	ctx.OpenFile = func(name string) (io.ReadCloser, error) {
		if name == virtual {
			return io.NopCloser(bytes.NewReader(source)), nil
		}
		return os.Open(name)
	}
	return ctx.MatchFile(filepath.Dir(path), name)
}
