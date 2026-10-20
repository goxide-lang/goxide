// Package typecheck supplies offline Go export data for semantic passes.
package typecheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hxsf/goxide/internal/config"
)

type ImporterOptions struct {
	Config  config.Config
	Context context.Context
	Tags    []string
	Race    bool
	// ModFile is an absolute, caller-owned copy of the source module and sums.
	// When supplied, graph updates are forbidden and sums stay beside that copy.
	ModFile string
}

type importerFailure struct{ err error }

func (i importerFailure) Import(string) (*types.Package, error) { return nil, i.err }

// NewImporter uses the selected target and source module without downloading
// dependencies or switching toolchains. Each importer owns its export cache.
func NewImporter(filename string, options ImporterOptions) types.Importer {
	if options.ModFile != "" && !filepath.IsAbs(options.ModFile) {
		return importerFailure{fmt.Errorf("importer module file must be an absolute path")}
	}
	if options.Config.Environment == nil {
		return importerFailure{fmt.Errorf("Go importer requires an explicit compilation environment")}
	}
	parent := options.Context
	if parent == nil {
		parent = context.Background()
	}
	if imp := platformImporter(); imp != nil {
		return imp
	}
	environment := importerEnvironment(options.Config.Environment.ExecEnv())
	tool := options.Config.Environment.GoTool
	tags := append([]string(nil), options.Tags...)
	sort.Strings(tags)
	build, err := json.Marshal(struct {
		Tool, Version string
		Values        map[string]string
		Tags          []string
		Race          bool
	}{tool, options.Config.Environment.GoVersion, options.Config.Environment.GoValues(), tags, options.Race})
	if err != nil {
		return importerFailure{err}
	}
	dir, _ := filepath.Abs(filepath.Dir(filename))
	if root := moduleRoot(dir); root != "" {
		dir = root
	} else if cwd, err := os.Getwd(); err == nil {
		dir = cwd
	}
	exports := map[string]string{}
	sources := map[string]packageFiles{}
	origins := map[string]packageOrigin{}
	lookup := func(path string) (io.ReadCloser, error) {
		if file := exports[path]; file != "" {
			return os.Open(file)
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		// Export loading is a deterministic type/source query, not binary stamping.
		args := []string{"list", "-buildvcs=false", "-export", "-deps", "-json", "-tags=" + strings.Join(tags, ",")}
		if options.Race {
			args = append(args, "-race")
		}
		if options.ModFile != "" {
			args = append(args, "-mod=readonly", "-modfile="+options.ModFile)
		}
		args = append(args, "--", path)
		cmd := exec.CommandContext(ctx, tool, args...)
		cmd.Dir = dir
		cmd.WaitDelay = time.Second
		cmd.Env = environment
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("offline export data for %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
		}
		d := json.NewDecoder(bytes.NewReader(data))
		for {
			var p struct {
				Standard                                                                                                                     bool
				Module                                                                                                                       *struct{ Path string }
				ImportPath, Export, Dir                                                                                                      string
				GoFiles, CgoFiles, CFiles, CXXFiles, MFiles, HFiles, FFiles, SFiles, SwigFiles, SwigCXXFiles, SysoFiles, EmbedFiles, Imports []string
			}
			err := d.Decode(&p)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			origin := packageOrigin{standard: p.Standard}
			if p.Module != nil {
				origin.module = p.Module.Path
			}
			origins[p.ImportPath] = origin
			if p.Export != "" {
				exports[p.ImportPath] = p.Export
				inputs := []string{}
				for _, group := range [][]string{p.GoFiles, p.CgoFiles, p.CFiles, p.CXXFiles, p.MFiles, p.HFiles, p.FFiles, p.SFiles, p.SwigFiles, p.SwigCXXFiles, p.SysoFiles, p.EmbedFiles} {
					inputs = append(inputs, group...)
				}
				sources[p.ImportPath] = packageFiles{dir: p.Dir, names: append(p.GoFiles, p.CgoFiles...), inputs: inputs, imports: p.Imports}
			}
		}
		file := exports[path]
		if file == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	}
	index := func(source, module string) ([]string, error) {
		pattern := module + "/..."
		if source == "toolchain" {
			pattern = "std"
		} else if source != "module" || module == "" {
			return nil, fmt.Errorf("cannot enumerate Go packages for source %q and module %q", source, module)
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		args := []string{"list", "-buildvcs=false", "-e", "-find", "-json", "-tags=" + strings.Join(tags, ",")}
		if options.Race {
			args = append(args, "-race")
		}
		if options.ModFile != "" {
			args = append(args, "-mod=readonly", "-modfile="+options.ModFile)
		}
		cmd := exec.CommandContext(ctx, tool, append(args, "--", pattern)...)
		cmd.Dir, cmd.Env, cmd.WaitDelay = dir, environment, time.Second
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("offline package index for %s: %w: %s", pattern, err, strings.TrimSpace(stderr.String()))
		}
		var packages []string
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var p struct {
				ImportPath, Name string
				Standard         bool
				Module           *struct{ Path string }
			}
			if err := decoder.Decode(&p); err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}
			selected := p.Standard && source == "toolchain"
			if source == "module" {
				selected = !p.Standard && p.Module != nil && p.Module.Path == module
			}
			if selected && p.Name != "" {
				packages = append(packages, p.ImportPath)
			}
		}
		sort.Strings(packages)
		return packages, nil
	}
	return &sourceImporter{Importer: importer.ForCompiler(token.NewFileSet(), "gc", lookup), sources: sources, origins: origins, fingerprints: map[string]string{}, build: fmt.Sprintf("%x", sha256.Sum256(build)), index: index, indices: map[string]packageIndex{}}
}

func importerEnvironment(environment []string) []string {
	var out []string
	for _, v := range environment {
		n, _, _ := strings.Cut(v, "=")
		switch n {
		case "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB", "GONOPROXY", "GOVCS":
			continue
		}
		out = append(out, v)
	}
	return append(out, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GONOPROXY=none", "GOVCS=*:off")
}

// PackageFiles exposes the exact source selection associated with offline export
// data. Consumers must not guess a module-cache or replace directory themselves.
type packageFiles struct {
	dir             string
	names           []string
	inputs, imports []string
}
type packageOrigin struct {
	module   string
	standard bool
}

type sourceImporter struct {
	types.Importer
	sources      map[string]packageFiles
	origins      map[string]packageOrigin
	fingerprints map[string]string
	build        string
	index        func(source, module string) ([]string, error)
	indices      map[string]packageIndex
}

type packageIndex struct {
	paths []string
	err   error
}

// PackageIndex contains only packages selected from this exact module or Go
// distribution. Nested modules and unrelated transitive dependencies are absent.
func (i *sourceImporter) PackageIndex(source, module string) ([]string, error) {
	key := source + "\x00" + module
	result, ok := i.indices[key]
	if !ok {
		result.paths, result.err = i.index(source, module)
		i.indices[key] = result
	}
	return append([]string(nil), result.paths...), result.err
}

// PackageOrigin reports the Go resolver's actual selected module. Namespace
// bindings use this proof instead of inferring ownership from a path prefix.
func (i *sourceImporter) PackageOrigin(path string) (module string, standard bool, err error) {
	if path == "unsafe" {
		return "", true, nil
	}
	if _, err := i.Import(path); err != nil {
		return "", false, err
	}
	origin, ok := i.origins[path]
	if !ok {
		return "", false, fmt.Errorf("package origin unavailable for %s", path)
	}
	return origin.module, origin.standard, nil
}

func (i *sourceImporter) ImportFrom(path, dir string, mode types.ImportMode) (*types.Package, error) {
	if imp, ok := i.Importer.(types.ImporterFrom); ok {
		return imp.ImportFrom(path, dir, mode)
	}
	return i.Import(path)
}

func (i *sourceImporter) PackageFiles(path string) (string, []string, error) {
	if _, err := i.Import(path); err != nil {
		return "", nil, err
	}
	p, ok := i.sources[path]
	if !ok || p.dir == "" {
		return "", nil, fmt.Errorf("source files unavailable for %s", path)
	}
	return p.dir, append([]string(nil), p.names...), nil
}

// PackageFingerprint pins the source closure used by permission analysis, including
// ordinary Go packages whose named types can change without an goxide marker change.
// Paths are import-relative, so moving an identical local replacement is stable.
func (i *sourceImporter) PackageFingerprint(path string) (string, error) {
	if path == "unsafe" {
		return i.build + ":unsafe", nil
	}
	if v, ok := i.fingerprints[path]; ok {
		return v, nil
	}
	if _, _, e := i.PackageFiles(path); e != nil {
		return "", e
	}
	p := i.sources[path]
	h := sha256.New()
	fmt.Fprintf(h, "%q %q\n", path, i.build)
	names := append([]string(nil), p.inputs...)
	sort.Strings(names)
	for _, name := range names {
		b, e := os.ReadFile(filepath.Join(p.dir, name))
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%q %x\n", name, sha256.Sum256(b))
	}
	deps := append([]string(nil), p.imports...)
	sort.Strings(deps)
	for _, dep := range deps {
		if dep == "C" {
			continue
		}
		v, e := i.PackageFingerprint(dep)
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%q %q\n", dep, v)
	}
	v := fmt.Sprintf("%x", h.Sum(nil))
	i.fingerprints[path] = v
	return v, nil
}

func moduleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Check verifies complete generated Go after semantic lowering, without ignoring
// errors used temporarily by an earlier pass for type inference.
func Check(filename string, source []byte, imp types.Importer, sizes types.Sizes) error {
	return CheckPackage("goxide/generated", map[string][]byte{filename: source}, imp, sizes)
}

// CheckPackage checks independently parsed files in their real Go package.
// File scopes (especially import names) remain separate. Callers select files
// using their build configuration before passing them here.
func CheckPackage(path string, sources map[string][]byte, imp types.Importer, sizes types.Sizes) error {
	_, err := CheckPackageTypes(path, sources, imp, sizes)
	return err
}

// CheckPackageTypes returns the checked identities for metadata export.
func CheckPackageTypes(path string, sources map[string][]byte, imp types.Importer, sizes types.Sizes) (*types.Package, error) {
	if path == "" {
		return nil, fmt.Errorf("package import path is required")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("package %s has no source files", path)
	}
	if sizes == nil {
		return nil, fmt.Errorf("Go type checking requires target sizes")
	}
	fs := token.NewFileSet()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	files := make([]*ast.File, 0, len(names))
	for _, name := range names {
		file, err := parser.ParseFile(fs, name, sources[name], parser.AllErrors)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	conf := types.Config{Importer: imp, Sizes: sizes}
	return conf.Check(path, fs, files, nil)
}
