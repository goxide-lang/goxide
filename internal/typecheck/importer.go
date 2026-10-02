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
	"runtime"
	"sort"
	"strings"
	"time"
)

// NewImporter uses the source module when present, otherwise the working module.
// It never downloads modules or switches toolchains. Each importer owns its cache.
func NewImporter(filename string) types.Importer { return NewImporterWithTags(filename, nil) }

// NewImporterWithTags uses the same explicit build tags as the module frontend.
func NewImporterWithTags(filename string, tags []string) types.Importer {
	return NewImporterWithBuild(filename, tags, false)
}

// NewImporterWithBuild keeps file selection and export instrumentation aligned.
func NewImporterWithBuild(filename string, tags []string, race bool) types.Importer {
	dir, _ := filepath.Abs(filepath.Dir(filename))
	if root := moduleRoot(dir); root != "" {
		dir = root
	} else if cwd, err := os.Getwd(); err == nil {
		dir = cwd
	}
	exports := map[string]string{}
	sources := map[string]packageFiles{}
	lookup := func(path string) (io.ReadCloser, error) {
		if file := exports[path]; file != "" {
			return os.Open(file)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		args := []string{"list", "-export", "-deps", "-json", "-tags=" + strings.Join(tags, ",")}
		if race {
			args = append(args, "-race")
		}
		args = append(args, "--", path)
		cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), args...)
		cmd.Dir = dir
		for _, v := range os.Environ() {
			n, _, _ := strings.Cut(v, "=")
			switch n {
			case "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB":
				continue
			}
			cmd.Env = append(cmd.Env, v)
		}
		cmd.Env = append(cmd.Env, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("offline export data for %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
		}
		d := json.NewDecoder(bytes.NewReader(data))
		for {
			var p struct {
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
	return &sourceImporter{Importer: importer.ForCompiler(token.NewFileSet(), "gc", lookup), sources: sources, fingerprints: map[string]string{}, race: race, tags: append([]string(nil), tags...)}
}

// PackageFiles exposes the exact source selection associated with offline export
// data. Consumers must not guess a module-cache or replace directory themselves.
type packageFiles struct {
	dir             string
	names           []string
	inputs, imports []string
}
type sourceImporter struct {
	types.Importer
	sources      map[string]packageFiles
	fingerprints map[string]string
	build        string
	race         bool
	tags         []string
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
// ordinary Go packages whose named types can change without an hgo marker change.
// Paths are import-relative, so moving an identical local replacement is stable.
func (i *sourceImporter) PackageFingerprint(path string) (string, error) {
	if path == "unsafe" {
		return runtime.Version() + ":unsafe", nil
	}
	if v, ok := i.fingerprints[path]; ok {
		return v, nil
	}
	if _, _, e := i.PackageFiles(path); e != nil {
		return "", e
	}
	p := i.sources[path]
	if i.build == "" {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM")
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off")
		b, e := cmd.Output()
		if e != nil {
			return "", e
		}
		tags := append([]string(nil), i.tags...)
		sort.Strings(tags)
		i.build = fmt.Sprintf("%s race=%t tags=%q", b, i.race, tags)
	}
	h := sha256.New()
	fmt.Fprintf(h, "%q %q %q\n", runtime.Version(), path, i.build)
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
func Check(filename string, source []byte, imp types.Importer) error {
	return CheckPackage("hgo/generated", map[string][]byte{filename: source}, imp)
}

// CheckPackage checks independently parsed files in their real Go package.
// File scopes (especially import names) remain separate. Callers select files
// using their build configuration before passing them here.
func CheckPackage(path string, sources map[string][]byte, imp types.Importer) error {
	if path == "" {
		return fmt.Errorf("package import path is required")
	}
	if len(sources) == 0 {
		return fmt.Errorf("package %s has no source files", path)
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
			return err
		}
		files = append(files, file)
	}
	conf := types.Config{Importer: imp}
	_, err := conf.Check(path, fs, files, nil)
	return err
}
