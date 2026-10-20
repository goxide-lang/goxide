package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	goversion "go/version"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/naming"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/rootconfig"
)

// Package generation belongs to the nearest module. A nested native Go module
// must not be generated as part of an enclosing manifest project.
func generationProject(directory string) (string, error) {
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", err
	}
	for {
		found, err := project.Detect(directory)
		if err != nil {
			return "", err
		}
		if found {
			return directory, nil
		}
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return "", nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", nil
		}
		directory = parent
	}
}

func runPreparedProject(req CompileRequest, prepared *compile.Result, sourceRoot string, out, stderr io.Writer) error {
	if req.Mode == "expand" {
		return fmt.Errorf("expand is a single-file command; use transpile for packages")
	}
	sourceRoot, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return err
	}
	manifest, err := project.Detect(sourceRoot)
	if err != nil {
		return err
	}
	if req.Mode == "transpile" {
		if err := checkPreparedPackage(req, prepared, false); err != nil {
			return err
		}
		if req.Output == "" {
			_, err := fmt.Fprintln(out, prepared.Root)
			return err
		}
		destination, err := packageOutput(req.Output, sourceRoot, "output directory", prepared.SourceRoots...)
		if err != nil {
			return err
		}
		if err := os.CopyFS(destination, os.DirFS(filepath.Dir(prepared.Root))); err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, filepath.Join(destination, "module"))
		return err
	}

	tags := strings.FieldsFunc(req.Tags, func(r rune) bool { return r == ',' || r == ' ' })
	binary := manifest && prepared.Kind != "lib"
	if req.Mode == "build" && !manifest {
		listing := exec.Command(req.GoTool, "list", "-mod=readonly", "-tags="+strings.Join(tags, ","), "-f", "{{.Name}}", prepared.Entry)
		listing.Dir, listing.Env = prepared.Root, localEnv(prepared.Config.Environment.ExecEnv())
		name, err := listing.CombinedOutput()
		if err != nil {
			return fmt.Errorf("selected target package: %w: %s", err, bytes.TrimSpace(name))
		}
		binary = strings.TrimSpace(string(name)) == "main"
	}
	argv := []string{req.Mode, "-buildvcs=false", "-mod=readonly", "-tags=" + strings.Join(tags, ",")}
	managedBinary := req.Mode == "build" && binary && req.Output == "" && manifest
	var unit *project.Unit
	if managedBinary {
		layout, preparedUnit, err := preparedProjectUnit(prepared.Root, sourceRoot)
		if err != nil {
			return err
		}
		defer layout.Close()
		unit = preparedUnit
	}
	if req.Mode == "build" {
		if !binary && req.Output != "" {
			return fmt.Errorf("lib build checks the package; -o is reserved for binaries")
		}
		if binary {
			destination := req.Output
			if managedBinary {
				destination = filepath.Join(unit.Intermediate, "executable")
			} else {
				if destination == "" {
					destination = filepath.Base(req.Input)
				}
				destination, err = packageOutput(destination, sourceRoot, "binary output", prepared.SourceRoots...)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
					return err
				}
			}
			req.Output = destination
			argv = append(argv, "-o", destination)
		}
	}
	target := prepared.Entry
	if req.Mode == "test" {
		argv = append(argv, "-race")
		if !manifest {
			if target == "." {
				target = "./..."
			} else {
				target += "/..."
			}
		}
	}
	argv = append(argv, target)
	if req.Mode == "run" {
		argv = append(argv, req.ProgramArgs...)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command := exec.CommandContext(ctx, req.GoTool, argv...)
	command.Dir, command.Env = prepared.Root, localEnv(prepared.Config.Environment.ExecEnv())
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, out, stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("generated project %s: %w", req.Mode, err)
	}
	if managedBinary {
		destination, err := unit.PublishBinary(req.Output)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, destination)
		return err
	}
	return err
}

// Go export compilation checks function bodies without running package init or
// the selected program. Transpile must reject invalid Go before publishing it.
func checkPreparedPackage(req CompileRequest, prepared *compile.Result, descendants bool) error {
	target := prepared.Entry
	if descendants {
		if target == "." {
			target = "./..."
		} else {
			target += "/..."
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command := exec.CommandContext(ctx, req.GoTool, "list", "-export", "-mod=readonly", "-buildvcs=false", "-tags="+req.Tags, target)
	command.Dir, command.Env = prepared.Root, localEnv(prepared.Config.Environment.ExecEnv())
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("generated package check: %w: %s", err, bytes.TrimSpace(output))
	}
	return nil
}

func preparedProjectUnit(module, sourceRoot string) (*project.Layout, *project.Unit, error) {
	var record struct {
		Owner struct {
			Version                    int
			Project, Module, Directory string
		}
		Unit project.UnitSpec
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(module)), ".goxide-unit.json"))
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, nil, err
	}
	root, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return nil, nil, err
	}
	if record.Owner.Project != root {
		return nil, nil, fmt.Errorf("prepared module belongs to a different source project")
	}
	layout, err := project.OpenLayout(root, record.Owner.Module, record.Owner.Directory)
	if err != nil {
		return nil, nil, err
	}
	unit, err := layout.Begin(record.Unit)
	if err != nil {
		layout.Close()
		return nil, nil, err
	}
	if unit.Bundle != filepath.Dir(module) {
		layout.Close()
		return nil, nil, fmt.Errorf("prepared module does not match its target unit")
	}
	return layout, unit, nil
}

func packageOutput(name, sourceRoot, kind string, sourceRoots ...string) (string, error) {
	name, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		return "", fmt.Errorf("refusing to overwrite existing %s %s", kind, name)
	}
	parent := filepath.Dir(name)
	tail := filepath.Base(name)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			name = filepath.Join(resolved, tail)
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(parent) == parent {
			return "", err
		}
		tail = filepath.Join(filepath.Base(parent), tail)
		parent = filepath.Dir(parent)
	}
	if within(sourceRoot, name) {
		return "", fmt.Errorf("%s must be outside the source module", kind)
	}
	for _, root := range sourceRoots {
		if within(root, name) {
			return "", fmt.Errorf("%s must be outside source dependency modules", kind)
		}
	}
	if err := project.RejectManagedOutput(name); err != nil {
		return "", err
	}
	return name, nil
}

func runDirectoryArgs(mode, input, dest, goTool string, tags, programArgs []string, rootsFile string, cfg config.Config, out, stderr io.Writer) error {
	if mode == "expand" {
		return fmt.Errorf("expand is a single-file command; use transpile for packages")
	}
	if mode == "transpile" && dest == "" {
		return fmt.Errorf("directory transpile requires -o with a fresh output directory")
	}
	input, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return err
	}
	root := findModule(input)
	if root == "" {
		return fmt.Errorf("directory mode requires an existing Go module")
	}
	if dest != "" {
		if _, err := packageOutput(dest, root, "output"); err != nil {
			return err
		}
	}
	manifest, err := project.ImportMod(goTool, root, cfg.Environment.ExecEnv())
	if err != nil {
		return err
	}
	for i := range manifest.Native.Replace {
		replacement := &manifest.Native.Replace[i]
		if replacement.Path != "" && !filepath.IsAbs(replacement.Path) {
			replacement.Path = filepath.Join(root, replacement.Path)
		}
	}
	roots, err := sourceRoots(root, input, rootsFile)
	if err != nil {
		return err
	}
	temporaryRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return err
	}
	if within(root, temporaryRoot) {
		return fmt.Errorf("directory temporary directory must be outside the source module; set TMPDIR to an external directory")
	}
	temporary, err := os.MkdirTemp(temporaryRoot, "goxide-directory-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	stage := filepath.Join(temporary, "source")
	if err := os.Mkdir(stage, 0700); err != nil {
		return err
	}
	if err := copyDirectorySources(root, input, stage, manifest.Module, roots, tags, mode == "test", cfg); err != nil {
		return err
	}
	for _, name := range []string{"go.mod", "go.sum", rootconfig.Filename} {
		if err := os.Remove(filepath.Join(stage, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	helper, err := os.MkdirTemp(stage, "goxide_cli_entry_")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(helper, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		return err
	}
	manifest.Bins = []project.Target{{Name: "directory", Path: filepath.Base(helper)}}
	data, err := project.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, project.ManifestName), data, 0600); err != nil {
		return err
	}
	prepared, err := compile.PrepareProject(stage, compile.Options{GoTool: goTool, TargetDir: filepath.Join(temporary, "target"), Offline: true, Roots: roots, NativeFeatures: &cfg.Features, Config: cfg, Tags: tags, Race: mode == "test"})
	if err != nil {
		return err
	}
	bundle := filepath.Dir(prepared.Root)
	if err := os.RemoveAll(filepath.Join(prepared.Root, filepath.Base(helper))); err != nil {
		return err
	}
	relative, err := filepath.Rel(root, input)
	if err != nil {
		return err
	}
	prepared.Entry = "."
	if relative != "." {
		prepared.Entry = "./" + filepath.ToSlash(relative)
	}
	req := CompileRequest{Mode: mode, Input: input, Output: dest, GoTool: goTool, Tags: strings.Join(tags, ","), ProgramArgs: programArgs}
	if mode == "transpile" {
		if err := checkPreparedPackage(req, prepared, true); err != nil {
			return err
		}
		destination, err := packageOutput(dest, root, "output directory", prepared.SourceRoots...)
		if err != nil {
			return err
		}
		configuration := rootsFile
		if configuration == "" {
			configuration = filepath.Join(root, rootconfig.Filename)
		}
		rootData, err := os.ReadFile(configuration)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := exportDirectoryModule(goTool, prepared.Root, bundle, destination, cfg.Environment.ExecEnv()); err != nil {
			return err
		}
		if rootData != nil {
			return os.WriteFile(filepath.Join(destination, rootconfig.Filename), rootData, 0644)
		}
		return nil
	}
	return runPreparedProject(req, prepared, root, out, stderr)
}

func copyDirectorySources(root, input, stage, module string, roots paths.Roots, tags []string, race bool, cfg config.Config) error {
	context := cfg.Environment.Context(tags, race)
	selected := []string{input}
	for _, namespace := range roots {
		if namespace.ModulePath == module && namespace.PackagePath != "" {
			selected = append(selected, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(namespace.PackagePath, module), "/"))))
		}
	}
	return filepath.WalkDir(root, func(name string, item fs.DirEntry, err error) error {
		if err != nil || name == root {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		if item.Name() == ".git" || item.Name() == ".goxide" || item.Name() == "vendor" || item.Name() == "node_modules" || item.Name() == "goxide.lock.guard" {
			if item.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if item.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source module contains symlink: %s", name)
		}
		if item.IsDir() {
			managed, err := project.ManagedSourceDirectory(name, root, module)
			if err != nil {
				return err
			}
			if managed {
				return filepath.SkipDir
			}
			for _, boundary := range []string{"go.mod", project.ManifestName} {
				if _, err := os.Stat(filepath.Join(name, boundary)); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return os.MkdirAll(filepath.Join(stage, relative), 0755)
		}
		if relative == "go.work" || relative == "go.work.sum" || relative == "goxide.lock" {
			return nil
		}
		if err := naming.Path(name); err != nil {
			return err
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source must be a regular file: %s", name)
		}
		extension := filepath.Ext(name)
		resource := strings.HasPrefix(filepath.ToSlash(relative), "testdata/") || strings.Contains(filepath.ToSlash(relative), "/testdata/")
		if extension == ".gox" {
			keep := resource
			for _, directory := range selected {
				keep = keep || within(directory, name)
			}
			if !keep || !resource && !race && strings.HasSuffix(name, "_test.gox") {
				return nil
			}
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		if !resource && (extension == ".go" || extension == ".gox") {
			context.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
			matches, err := context.MatchFile("", strings.TrimSuffix(item.Name(), extension)+".go")
			if err != nil {
				return err
			}
			if !matches {
				return nil
			}

		}
		return os.WriteFile(filepath.Join(stage, relative), data, info.Mode().Perm())
	})
}

func exportDirectoryModule(tool, source, bundle, destination string, base []string) error {
	if err := os.CopyFS(destination, os.DirFS(source)); err != nil {
		return err
	}
	dependencies := filepath.Join(bundle, "deps")
	modules := map[string]string{source: destination}
	entries, err := os.ReadDir(dependencies)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(dependencies, entry.Name()), filepath.Join(destination, ".goxide-deps", entry.Name())
		if from == source {
			continue
		}
		if err := os.CopyFS(to, os.DirFS(from)); err != nil {
			return err
		}
		modules[from] = to
	}
	for _, from := range project.Keys(modules) {
		to := modules[from]
		mod, err := project.ReadMod(tool, to, base)
		if err != nil {
			return err
		}
		arguments := []string{"mod", "edit"}
		for _, replacement := range mod.Replace {
			if replacement.New.Version != "" {
				continue
			}
			target := replacement.New.Path
			if !filepath.IsAbs(target) {
				target = filepath.Join(from, target)
			}
			old := replacement.Old.Path
			if replacement.Old.Version != "" {
				old += "@" + replacement.Old.Version
			}
			if relocated := modules[target]; relocated != "" {
				relative, err := filepath.Rel(to, relocated)
				if err != nil {
					return err
				}
				if !strings.HasPrefix(relative, ".") {
					relative = "./" + relative
				}
				arguments = append(arguments, "-replace="+old+"="+filepath.ToSlash(relative))
			}
		}
		if len(arguments) != 2 {
			command := exec.Command(tool, arguments...)
			command.Dir, command.Env = to, localEnv(base)
			if output, err := command.CombinedOutput(); err != nil {
				return fmt.Errorf("export Go module: %w: %s", err, output)
			}
		}
	}
	return nil
}

func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func projectCompilerID() string {
	executable, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		return "unknown"
	}
	return project.Digest(data)
}

func checkDirectoryCompiler(tool string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, tool, "version")
	command.Env = localEnv()
	data, err := command.Output()
	if err != nil {
		return fmt.Errorf("Go compiler check: %w", err)
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 || parts[0] != "go" || parts[1] != "version" || !goversion.IsValid(parts[2]) || goversion.Compare(parts[2], "go1.27") < 0 {
		return fmt.Errorf("not a supported Go compiler: %s", data)
	}
	if parts[2] != runtime.Version() {
		return fmt.Errorf("directory -go compiler version %s must match the frontend build toolchain %s", parts[2], runtime.Version())
	}
	return nil
}
