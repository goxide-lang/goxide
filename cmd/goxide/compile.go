// goxide compiles the documented macro prototype subset to ordinary Go.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/typecheck"
)

// CompileRequest is the parser-independent input to compilation services.
// ProgramArgs is used only by run and never interpreted as Go tool flags.
type CompileRequest struct {
	Packages                                                        []string
	Workspace                                                       bool
	Mode, Input, Output, SourceMap, Expanded, Runtime, Tags, GoTool string
	RootsFile                                                       string
	Features, Bin, Example, TargetDir                               string
	NoDefaultFeatures, Lib, Offline, Locked                         bool
	ProgramArgs                                                     []string
}

func executeCompile(req CompileRequest, out, stderr io.Writer) error {
	switch req.Mode {
	case "build", "run", "test", "transpile", "expand":
	default:
		return fmt.Errorf("unknown command %q", req.Mode)
	}
	if req.Input == "" {
		return errors.New("input is required")
	}
	if req.Mode != "run" && len(req.ProgramArgs) != 0 {
		return fmt.Errorf("program arguments require run")
	}

	if err := naming.Path(req.Input); err != nil {
		return err
	}
	mode := req.Mode
	dest, mapPath, expandedPath := &req.Output, &req.SourceMap, &req.Expanded
	runtimeRoot, tags, goTool := &req.Runtime, &req.Tags, &req.GoTool
	features, noDefault, bin, example := &req.Features, &req.NoDefaultFeatures, &req.Bin, &req.Example
	lib, targetDir, offline, locked := &req.Lib, &req.TargetDir, &req.Offline, &req.Locked
	input, err := filepath.Abs(req.Input)
	if err != nil {
		return err
	}
	if info, statErr := os.Stat(input); statErr == nil && info.IsDir() {
		if *mapPath != "" || *expandedPath != "" {
			return errors.New("directory mode writes per-file .go.map.json automatically; --source-map/--expanded are single-file flags")
		}
		if w, e := project.DiscoverWorkspace(input); e != nil {
			return e
		} else if w != nil {
			if req.RootsFile != "" {
				return fmt.Errorf("--roots cannot override a goxide workspace; declare roots in member dependencies")
			}
			return runWorkspace(req, input, w, out, stderr)
		}
		if req.Workspace || len(req.Packages) > 0 {
			return fmt.Errorf("workspace selection requires a workspace manifest")
		}
		if exists, e := project.Detect(input); e != nil {
			return e
		} else if exists {
			if req.RootsFile != "" {
				return fmt.Errorf("--roots cannot override goxide.toml dependency mappings")
			}
			return runProject(mode, input, *dest, *goTool, strings.FieldsFunc(*tags, func(r rune) bool { return r == ',' || r == ' ' }), projectFlags{targetDir: *targetDir, features: *features, bin: *bin, example: *example, noDefault: *noDefault, offline: *offline, locked: *locked, lib: *lib, programArgs: req.ProgramArgs}, out, stderr)
		}
		if *targetDir != "" || *features != "" || *noDefault || *bin != "" || *example != "" || *lib || *offline || *locked {
			return errors.New("project flags require goxide.toml in the input directory")
		}
		return runDirectoryArgs(mode, input, *dest, *goTool, strings.FieldsFunc(*tags, func(r rune) bool { return r == ',' || r == ' ' }), req.ProgramArgs, req.RootsFile, out, stderr)
	}
	if req.Workspace || len(req.Packages) > 0 {
		return errors.New("workspace selection requires a directory")
	}
	if *targetDir != "" || *features != "" || *noDefault || *bin != "" || *example != "" || *lib || *offline || *locked {
		return errors.New("project flags require a directory with goxide.toml")
	}
	if *tags != "" {
		return errors.New("--tags requires directory mode")
	}
	if mode == "test" {
		return errors.New("test requires a package directory")
	}
	src, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	// Validate all output paths before writing any artifacts.
	paths := []string{*mapPath, *expandedPath, *dest}
	if mode == "build" && *dest == "" {
		paths[2] = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	}
	for i, path := range paths {
		if path == "" {
			continue
		}
		if samePath(path, input) {
			return errors.New("refusing to overwrite input")
		}
		for _, prior := range paths[:i] {
			if prior != "" && samePath(path, prior) {
				return errors.New("output paths must be distinct")
			}
		}
	}
	owner, err := prepareSingleFileModule(input)
	if err != nil {
		return err
	}
	var result frontend.Result
	var configuredTool string
	moduleRoot := ""
	if owner != nil {
		moduleRoot = owner.root
	}
	rootInput, err := filepath.EvalSymlinks(input)
	if err != nil {
		if owner != nil {
			owner.close()
		}
		return err
	}
	roots, err := sourceRoots(moduleRoot, filepath.Dir(rootInput), req.RootsFile)
	if err != nil {
		if owner != nil {
			owner.close()
		}
		return err
	}
	if owner != nil {
		defer owner.close()
		input = owner.input
		if err = owner.validateOutputs(&req); err != nil {
			return err
		}
		if mode == "run" || mode == "build" {
			configuredTool, err = exec.LookPath(*goTool)
			if err != nil {
				return err
			}
			configuredTool, err = filepath.Abs(configuredTool)
			if err != nil {
				return err
			}
			if err = checkDirectoryCompiler(configuredTool); err != nil {
				return fmt.Errorf("configured single-file compiler: %w", err)
			}
		}
		imp := typecheck.NewImporterWithModFile(context.Background(), input, nil, false, singleFileEnvironment(), owner.modFile)
		result, err = frontend.CompileWithOptions(input, src, frontend.FileOptions{PackagePath: owner.packagePath, Importer: imp, Roots: roots})
	} else {
		result, err = frontend.CompileWithOptions(input, src, frontend.FileOptions{Roots: roots})
	}
	if err != nil {
		return err
	}
	if *mapPath != "" {
		if err := writeArtifact(*mapPath, input, result.SourceMap); err != nil {
			return err
		}
	}
	if *expandedPath != "" {
		if err := writeArtifact(*expandedPath, input, result.Expanded); err != nil {
			return err
		}
	}
	if mode == "transpile" || mode == "expand" {
		data := result.Go
		if mode == "expand" {
			data = result.Expanded
		}
		if *dest == "" {
			_, err = out.Write(data)
			return err
		}
		return writeArtifact(*dest, input, data)
	}
	if owner != nil {
		return owner.run(req, result, configuredTool, out, stderr)
	}
	root := *runtimeRoot
	if root == "" {
		root = findModule(filepath.Dir(input))
		if root == "" {
			cwd, _ := os.Getwd()
			root = findModule(cwd)
		}
	}
	if root == "" {
		return errors.New("standalone run/build requires --runtime pointing to a Go module manifest with pinned std/serde dependencies")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	// Select only a real Go executable; this environment may have a board game named go.
	tool, err := exec.LookPath(*goTool)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, tool, "version")
	check.Env = localEnv()
	ver, err := check.Output()
	if err != nil {
		return fmt.Errorf("Go compiler check: %w", err)
	}
	parts := strings.Fields(string(ver))
	if len(parts) < 3 || parts[0] != "go" || parts[1] != "version" || !version.IsValid(parts[2]) || version.Compare(parts[2], "go1.27") < 0 {
		return fmt.Errorf("not a supported Go compiler: %s", ver)
	}
	tmp, err := os.MkdirTemp("", "goxide-run-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err = writeStandaloneSupportModule(tool, root, tmp); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(tmp, "main.go"), result.Go, 0600); err != nil {
		return err
	}
	var argv []string
	if mode == "run" {
		argv = append([]string{"run", "-buildvcs=false", "."}, req.ProgramArgs...)
	} else {
		target := *dest
		if target == "" {
			target = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		}
		target, err = filepath.Abs(target)
		if err != nil {
			return err
		}
		if samePath(target, input) {
			return errors.New("refusing to overwrite input")
		}
		argv = []string{"build", "-buildvcs=false", "-o", target, "."}
	}
	cmd := exec.Command(tool, argv...)
	cmd.Dir = tmp
	cmd.Env = localEnv()
	cmd.Stdout = out
	var compilerErrors bytes.Buffer
	cmd.Stderr = io.MultiWriter(stderr, &compilerErrors)
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		if result.Diagnostics != nil {
			fmt.Fprint(stderr, result.Diagnostics.Diagnostics(result.Go, compilerErrors.String()))
		}
		return fmt.Errorf("generated Go %s: %w", mode, err)
	}
	return nil
}
func localEnv() []string {
	env := []string{}
	for _, v := range os.Environ() {
		name, _, _ := strings.Cut(v, "=")
		switch name {
		case "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB":
			continue
		}
		env = append(env, v)
	}
	return append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
}
func findModule(dir string) string {
	for {
		_, e := os.Stat(filepath.Join(dir, "go.mod"))
		if e == nil {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			return ""
		}
		dir = p
	}
}
func writeArtifact(path, input string, data []byte) error {
	full, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if samePath(full, input) {
		return errors.New("refusing to overwrite input")
	}
	return os.WriteFile(full, data, 0644)
}

func samePath(a, b string) bool {
	x, e := filepath.Abs(a)
	if e != nil {
		return false
	}
	y, e := filepath.Abs(b)
	if e != nil {
		return false
	}
	if x == y {
		return true
	}
	sx, ex := os.Stat(x)
	sy, ey := os.Stat(y)
	if ex == nil && ey == nil && os.SameFile(sx, sy) {
		return true
	}
	// Resolve parent symlinks even when the final output does not exist yet.
	px, ex := filepath.EvalSymlinks(filepath.Dir(x))
	py, ey := filepath.EvalSymlinks(filepath.Dir(y))
	return ex == nil && ey == nil && filepath.Join(px, filepath.Base(x)) == filepath.Join(py, filepath.Base(y))
}
