// goxide compiles the documented macro prototype subset to ordinary Go.
package main

import (
	"errors"
	"fmt"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/naming"
	sourcepaths "github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

// CompileRequest is the parser-independent input to compilation services.
// ProgramArgs is used only by run and never interpreted as Go tool flags.
type CompileRequest struct {
	Packages                                                        []string
	Workspace                                                       bool
	Mode, Input, Output, SourceMap, Expanded, Runtime, Tags, GoTool string
	RootsFile                                                       string
	Features, Bin, Example, TargetDir, Target                       string
	config                                                          config.Config
	NoDefaultFeatures, Lib, Offline, Locked                         bool
	FeaturesSet, TargetSet                                          bool
	ProgramArgs                                                     []string
}

type compiledFile struct {
	Go, Expanded []byte
}

func compileSourceFile(filename string, source []byte, packagePath string, goImports types.Importer, roots sourcepaths.Roots, cfg config.Config) (compiledFile, error) {
	if err := naming.Source(filename, source); err != nil {
		return compiledFile{}, err
	}
	displayName := filename
	filename = path.Join(packagePath, filepath.Base(filename))
	file, diagnostics := parser.ParseFile(filename, source)
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == parser.Error {
			return compiledFile{}, typecheck.ParserError(displayName, source, diagnostic)
		}
	}
	sources := map[string]typecheck.DiagnosticSource{filename: {Name: displayName, Text: source, AST: file}}
	if _, err := expand.Configure(file, cfg); err != nil {
		return compiledFile{}, typecheck.SourceError(err, sources, nil)
	}
	expanded, err := expand.Expand([]*syntax.File{file}, nil, map[*syntax.File]config.Config{file: cfg})
	if err != nil {
		return compiledFile{}, typecheck.SourceError(err, sources, expanded)
	}
	imports := typecheck.NewNamespaceImporter(goImports, roots)
	imports.Prepare(expanded)
	resolved := resolve.ResolveExpanded(map[string]*syntax.File{filename: file}, expanded, imports)
	if imports.Err() != nil {
		return compiledFile{}, imports.Err()
	}
	resolvedFile := resolved.Files[filename]
	if err := imports.CheckUses(resolvedFile); err != nil {
		return compiledFile{}, typecheck.SourceError(err, sources, expanded)
	}
	if len(resolved.Diagnostics) != 0 {
		return compiledFile{}, typecheck.SourceError(resolved.Diagnostics[0], sources, expanded)
	}
	files := map[string]*syntax.File{filename: resolvedFile.AST}
	checked, problems := check.CheckWithImporter(files, resolved, goImports, cfg.Sizes())
	if len(problems) != 0 {
		return compiledFile{}, typecheck.SourceError(problems[0], sources, expanded)
	}
	generated, err := lower.Lower(files, checked)
	if err != nil {
		return compiledFile{}, typecheck.SourceError(err, sources, expanded)
	}
	if err := typecheck.CheckPackage(packagePath, generated, goImports, cfg.Sizes()); err != nil {
		return compiledFile{}, err
	}
	if err := lower.ApplyTargetConstraints(generated, map[string]*syntax.File{filename: file}, cfg); err != nil {
		return compiledFile{}, err
	}
	return compiledFile{Go: generated[filename], Expanded: []byte(printer.Source(resolvedFile.AST))}, nil
}

func executeCompile(req CompileRequest, out, stderr io.Writer) error {
	switch req.Mode {
	case "build", "run", "test", "transpile", "expand":
	default:
		return fmt.Errorf("unknown command %q", req.Mode)
	}
	if req.SourceMap != "" {
		return errors.New("--source-map is not supported by the new compiler pipeline")
	}
	if req.Workspace || len(req.Packages) > 0 {
		return errors.New("workspace selection is deferred until MVP")
	}
	if req.Input == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		if req.Mode == "transpile" {
			req.Input = cwd
		} else {
			req.Input, err = project.DiscoverProject(cwd)
			if err != nil {
				return err
			}
			if req.Input == "" {
				return errors.New("no goxide.toml found from the current directory upward; specify a source file or Go module directory")
			}
		}
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
	info, err := os.Stat(input)
	if err != nil {
		return err
	}
	manifestRoot := ""
	if info.IsDir() {
		manifestRoot, err = generationProject(input)
		if err != nil {
			return err
		}
	}
	nativeGeneration := mode == "transpile" && manifestRoot == ""
	if nativeGeneration && (req.TargetSet || req.Target != "") {
		return errors.New("--target is not accepted by native gen/transpile; select GOOS/GOARCH in the Go environment")
	}
	environmentDir := input
	if !info.IsDir() {
		environmentDir = filepath.Dir(input)
	}
	environment, err := config.Prepare(req.GoTool, environmentDir, req.Target, nativeGeneration, os.Environ())
	if err != nil {
		return err
	}
	req.config = config.Config{Features: nativeFeatures(req.Features), Environment: environment, Reads: &config.Reads{}}
	req.GoTool = environment.GoTool
	if (mode == "run" || mode == "test") && environment.Target != environment.Host {
		return fmt.Errorf("%s requires the host target %s; selected %s", mode, environment.Host, environment.Target)
	}
	if nativeGeneration && (*dest == "" || *dest == "-") && (info.IsDir() || filepath.Ext(input) == ".gox") {
		return generatePackage(req, out, stderr)
	}
	if (mode == "transpile" || mode == "expand") && *dest == "-" {
		*dest = ""
	}
	nativeGo := filepath.Ext(input) == ".go"
	if info, statErr := os.Stat(input); statErr == nil && info.IsDir() {
		if manifestRoot != "" {
			input = manifestRoot
		}
		if *mapPath != "" || *expandedPath != "" {
			return errors.New("--expanded is a single-file flag")
		}
		if exists, e := project.Detect(input); e != nil {
			return e
		} else if exists {
			if req.RootsFile != "" {
				return fmt.Errorf("--roots cannot override goxide.toml dependency declarations")
			}
			if *lib && mode == "run" {
				return errors.New("library targets cannot be run")
			}
			if *tags != "" {
				return errors.New("--tags is not supported for manifest projects by the new compiler pipeline")
			}
			prepared, err := prepareProject(input, compile.Options{Bin: *bin, Example: *example, Lib: *lib, GoTool: *goTool, TargetDir: *targetDir, Offline: *offline, Locked: *locked, Features: featureNames(*features), NoDefaultFeatures: *noDefault, Config: req.config, Race: mode == "test"}, stderr)
			if err != nil {
				return err
			}
			return runPreparedProject(req, prepared, input, out, stderr)
		}
		if *targetDir != "" || (*features != "" && mode != "transpile") || *noDefault || *bin != "" || *example != "" || *lib || *offline || *locked {
			return errors.New("project flags require goxide.toml in the input directory")
		}
		return runDirectoryArgs(mode, input, *dest, *goTool, strings.FieldsFunc(*tags, func(r rune) bool { return r == ',' || r == ' ' }), req.ProgramArgs, req.RootsFile, req.config, out, stderr)
	}
	if req.Workspace || len(req.Packages) > 0 {
		return errors.New("workspace selection requires a directory")
	}
	if *noDefault {
		return errors.New("--no-default-features requires goxide.toml")
	}
	if *targetDir != "" || *bin != "" || *example != "" || *lib || *offline || *locked {
		return errors.New("project flags require a directory with goxide.toml")
	}
	if nativeGo && (req.FeaturesSet || *features != "") {
		return errors.New("--features applies to Goxide source; native .go uses Go build selection")
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
	owner, err := prepareSingleFileModule(input, req.config)
	if err != nil {
		return err
	}
	var result compiledFile
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
	var roots sourcepaths.Roots
	if nativeGo {
		if req.RootsFile != "" {
			if owner != nil {
				owner.close()
			}
			return errors.New("--roots applies to Goxide source; native .go uses Go imports")
		}
	} else {
		roots, err = sourceRoots(moduleRoot, filepath.Dir(rootInput), req.RootsFile)
		if err != nil {
			if owner != nil {
				owner.close()
			}
			return err
		}
	}
	if owner != nil {
		defer owner.close()
		input = owner.input
		if err = owner.validateOutputs(&req); err != nil {
			return err
		}
		configuredTool = req.GoTool

		if nativeGo {
			result, err = compileNativeGoFile(input, src)
		} else {
			expanded := map[string][]byte{}
			newImporter := func() types.Importer {
				return typecheck.NewImporter(input, typecheck.ImporterOptions{Config: req.config, ModFile: owner.modFile})
			}
			var generated map[string][]byte
			generated, err = compileNativePackage(owner, roots, req.config, nil, stderr, map[string][]byte{input: src}, newImporter, lower.PackageOptions{Config: req.config, Expanded: expanded})
			if err == nil && generated == nil {
				result, err = compileSourceFile(input, src, owner.packagePath, newImporter(), roots, req.config)
			} else {
				if err == nil {
					err = typecheck.CheckPackage(owner.packagePath, generated, newImporter(), req.config.Sizes())
				}
				result = compiledFile{Go: generated[input], Expanded: expanded[input]}
			}
		}
	} else if nativeGo {
		result, err = compileNativeGoFile(input, src)
	} else {
		importContext := input
		if *runtimeRoot != "" {
			importContext = filepath.Join(*runtimeRoot, "go.mod")
		}
		result, err = compileSourceFile(input, src, "goxide/generated", typecheck.NewImporter(importContext, typecheck.ImporterOptions{Config: req.config}), roots, req.config)
	}
	if err != nil {
		return err
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
	tool := req.GoTool
	tmp, err := os.MkdirTemp("", "goxide-run-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err = writeStandaloneSupportModule(tool, root, tmp, environment.ExecEnv()); err != nil {
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
	cmd.Env = localEnv(environment.ExecEnv())
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("generated Go %s: %w", mode, err)
	}
	return nil
}
func localEnv(base ...[]string) []string {
	var original []string
	if len(base) != 0 {
		original = base[0]
	} else {
		original = os.Environ()
	}
	env := []string{}
	for _, v := range original {
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
