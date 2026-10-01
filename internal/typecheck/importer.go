// Package typecheck supplies offline Go export data for semantic passes.
package typecheck

import (
	"bytes"
	"context"
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
func NewImporter(filename string) types.Importer {
	dir, _ := filepath.Abs(filepath.Dir(filename))
	if root := moduleRoot(dir); root != "" {
		dir = root
	} else if cwd, err := os.Getwd(); err == nil {
		dir = cwd
	}
	exports := map[string]string{}
	lookup := func(path string) (io.ReadCloser, error) {
		if file := exports[path]; file != "" {
			return os.Open(file)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, filepath.Join(runtime.GOROOT(), "bin", "go"), "list", "-export", "-deps", "-json", "--", path)
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
			var p struct{ ImportPath, Export string }
			err := d.Decode(&p)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if p.Export != "" {
				exports[p.ImportPath] = p.Export
			}
		}
		file := exports[path]
		if file == "" {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	}
	return importer.ForCompiler(token.NewFileSet(), "gc", lookup)
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
