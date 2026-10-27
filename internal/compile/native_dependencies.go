package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
)

type NativeSourceDependency struct {
	Name       string
	Namespace  paths.Namespace
	Dependency project.Dependency
	Macro      bool
}

type NativeDependencies struct {
	Manifest  project.Manifest
	Roots     paths.Roots
	Macros    []string
	Automatic map[string][]NativeSourceDependency
	Required  map[string]string
}

type nativeModuleSelection struct {
	Path, Version, Dir string
	Replace            *nativeModuleSelection
	Error              *struct{ Err string }
}

type NativeRootDemand struct{ Alias string }

func (e *NativeRootDemand) Error() string { return "source dependency required for root " + e.Alias }

func (plan *NativeDependencies) GenerationRoots(root string, cfg config.Config, importer types.Importer, used func(paths.Namespace)) (paths.Roots, func(string, paths.Namespace) error, error) {
	roots := maps.Clone(plan.Roots)
	standard := paths.NewGoNamespaces(importer)
	for _, alias := range project.Keys(plan.Automatic) {
		_, found, err := standard.Root(alias)
		if err != nil {
			return nil, nil, err
		}
		if found {
			continue // Automatic names never replace a real toolchain namespace.
		}
		roots[alias] = plan.Automatic[alias][0].Namespace
		if candidates := plan.Automatic[alias]; len(candidates) == 1 && candidates[0].Namespace.ModulePath == "github.com/goxide-lang/std" {
			// Prelude lookup needs the selected standard source graph before any
			// explicit use can demand a dependency root.
			plan.Manifest.Dependencies[alias] = candidates[0].Dependency
		}
	}
	missing := map[string]string{}
	for _, alias := range []string{"std", "serde"} {
		module := "github.com/goxide-lang/" + alias
		if plan.Required[module] != "" {
			continue
		}
		if _, exists := roots[alias]; exists {
			continue
		}
		explicit := false
		for _, namespace := range plan.Roots {
			explicit = explicit || namespace.ModulePath == module
		}
		if explicit {
			continue
		}
		missing[alias] = module
		roots[alias] = paths.Namespace{ModulePath: module, PackagePath: module, Source: "module"}
	}
	require := func(alias string, namespace paths.Namespace) error {
		if module := missing[alias]; module != "" && namespace.ModulePath == module {
			return &MissingNativeDependencies{Root: root, Config: cfg, Modules: []string{module}}
		}
		candidates := plan.Automatic[alias]
		if namespace.Source == "toolchain" {
			return nil
		}
		if len(candidates) == 0 {
			if _, source := plan.Manifest.Dependencies[alias]; source {
				if used != nil {
					used(plan.Roots[alias])
				}
			}
			return nil
		}
		if len(candidates) > 1 {
			var modules []string
			for _, candidate := range candidates {
				modules = append(modules, candidate.Namespace.ModulePath)
			}
			return fmt.Errorf("ambiguous default dependency root %q: %s; choose an explicit alias in goxide.Roots.toml", alias, strings.Join(modules, ", "))
		}
		if _, active := plan.Manifest.Dependencies[alias]; !active {
			return &NativeRootDemand{Alias: alias}
		}
		if used != nil {
			used(candidates[0].Namespace)
		}
		return nil
	}
	return roots, require, nil
}

// Native generation may acquire the selected module graph, but every command
// receives the consumer's private modfile. Go's captured proxy, private-module
// and checksum policy remains authoritative; execution/import helpers stay offline.
func nativeDependencyCommand(ctx context.Context, root string, cfg config.Config, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, cfg.Environment.GoTool, args...)
	command.WaitDelay = time.Second
	command.Dir, command.Env = root, cfg.Environment.ExecEnv()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		var download struct{ Error string }
		if json.Unmarshal(output, &download) == nil && download.Error != "" {
			return nil, fmt.Errorf("native source module: %s", download.Error)
		}
		return nil, fmt.Errorf("native source module: %w: %s", err, stderr.String())
	}
	return output, nil
}

// NativeSourceDependencies records authored require candidates before Go can
// complete the private graph. Automatic candidates are only metadata: callers
// activate their dependency edges when the frontend actually selects a root.
func NativeSourceDependencies(ctx context.Context, root, modFile string, roots paths.Roots, cfg config.Config, automatic bool) (*NativeDependencies, error) {
	data, err := nativeModuleCommand(ctx, root, cfg, "mod", "edit", "-json", modFile)
	if err != nil {
		return nil, err
	}
	var mod project.ModFile
	if err := json.Unmarshal(data, &mod); err != nil {
		return nil, err
	}
	plan := &NativeDependencies{
		Manifest: project.Manifest{Module: mod.Module.Path, GoVersion: mod.Go, Toolchain: mod.Toolchain, Codegen: project.CodegenConfig{GoSugar: cfg.GoSugar}, Dependencies: map[string]project.Dependency{}},
		Roots:    maps.Clone(roots), Automatic: map[string][]NativeSourceDependency{}, Required: map[string]string{},
	}
	if plan.Roots == nil {
		plan.Roots = paths.Roots{}
	}
	plan.Manifest.Native.Godebug = mod.Godebug
	for _, item := range mod.Require {
		plan.Manifest.Native.Require = append(plan.Manifest.Native.Require, project.GoRequire{Module: item.Path, Version: item.Version, Indirect: item.Indirect})
		plan.Required[item.Path] = item.Version
	}
	for _, item := range mod.Exclude {
		plan.Manifest.Native.Exclude = append(plan.Manifest.Native.Exclude, project.GoExclude{Module: item.Path, Version: item.Version})
	}
	for _, item := range mod.Replace {
		replacement := project.GoReplace{Module: item.Old.Path, Version: item.Old.Version, With: item.New.Path, WithVersion: item.New.Version}
		if item.New.Version == "" {
			replacement.With, replacement.Path = "", item.New.Path
			if !filepath.IsAbs(replacement.Path) {
				replacement.Path = filepath.Join(root, replacement.Path)
			}
		}
		plan.Manifest.Native.Replace = append(plan.Manifest.Native.Replace, replacement)
	}
	modules, explicit := map[string]bool{}, map[string]bool{}
	for _, namespace := range roots {
		if namespace.Source != "toolchain" && namespace.ModulePath != "" {
			explicit[namespace.ModulePath] = true
			if namespace.ModulePath != mod.Module.Path {
				modules[namespace.ModulePath] = true
			}
		}
	}
	if automatic {
		for module := range plan.Required {
			if module != mod.Module.Path {
				modules[module] = true
			}
		}
	}
	if len(modules) == 0 {
		return plan, nil
	}
	selections, err := nativeSourceOnlySelections(ctx, plan, modFile, cfg, automatic)
	if err != nil {
		return nil, err
	}
	if selections == nil {
		query := nativeModuleCommand
		arguments := []string{"list"}
		if automatic {
			query = nativeDependencyCommand
		} else {
			arguments = append(arguments, "-e")
		}
		arguments = append(arguments, "-mod=mod", "-modfile="+modFile, "-m", "-json", "all")
		selected, queryErr := query(ctx, root, cfg, arguments...)
		if queryErr != nil && automatic {
			return nil, queryErr
		}
		selections = map[string]nativeModuleSelection{}
		if queryErr == nil {
			decoder := json.NewDecoder(bytes.NewReader(selected))
			for {
				var selection nativeModuleSelection
				if err := decoder.Decode(&selection); err == io.EOF {
					break
				} else if err != nil {
					return nil, err
				}
				selections[selection.Path] = selection
			}
		}
	}
	sources := map[string]*NativeSourceDependency{}
	for _, module := range project.Keys(modules) {
		selection := selections[module]
		directory, version := selection.Dir, selection.Version
		if version == "" {
			version = plan.Required[module]
		}
		if selection.Replace != nil && selection.Replace.Dir != "" {
			directory = selection.Replace.Dir
		}
		// An authored local Goxide replacement need not have a Go manifest yet.
		// Specific-version replaces take precedence over wildcard replaces.
		local := ""
		for _, replacement := range plan.Manifest.Native.Replace {
			if replacement.Module == module && replacement.Path != "" && (replacement.Version == "" || replacement.Version == version) {
				local = replacement.Path
				if replacement.Version == version {
					break
				}
			}
		}
		if local != "" {
			directory = local
		} else if automatic {
			if selection.Error != nil {
				return nil, fmt.Errorf("native source module %s: %s", module, selection.Error.Err)
			}
			if selection.Path == "" || version == "" {
				if plan.Required[module] == "" {
					continue // Unused explicit namespaces need not belong to this module's graph.
				}
				return nil, fmt.Errorf("native source module %s is not in the selected Go module graph", module)
			}
			if directory == "" {
				actual, actualVersion := module, version
				if selection.Replace != nil {
					actual, actualVersion = selection.Replace.Path, selection.Replace.Version
				}
				download, err := nativeDependencyCommand(ctx, root, cfg, "mod", "download", "-json", "-modfile="+modFile, actual+"@"+actualVersion)
				if err != nil {
					return nil, err
				}
				var acquired struct{ Path, Version, Dir, Error string }
				if err := json.Unmarshal(download, &acquired); err != nil {
					return nil, err
				}
				if acquired.Error != "" {
					return nil, fmt.Errorf("native source module %s: %s", module, acquired.Error)
				}
				if acquired.Path != actual || acquired.Version != actualVersion || acquired.Dir == "" {
					return nil, fmt.Errorf("native source module %s: download did not provide selected source %s@%s", module, actual, actualVersion)
				}
				directory = acquired.Dir
			}
		}
		if directory == "" {
			continue
		}
		source, err := readNativeSourceDependency(directory, module, version)
		if err != nil {
			return nil, err
		}
		sources[module] = source
	}
	for _, alias := range project.Keys(roots) {
		namespace := roots[alias]
		source := sources[namespace.ModulePath]
		if namespace.Source == "toolchain" || source == nil {
			continue
		}
		// Exact-package roots never acquire another library entry implicitly.
		if namespace.PackagePath != "" && namespace.PackagePath != source.Namespace.PackagePath {
			continue
		}
		plan.Manifest.Dependencies[alias] = source.Dependency
		namespace.PackagePath, namespace.GoPrefix = source.Namespace.PackagePath, ""
		plan.Roots[alias] = namespace
		if source.Macro {
			plan.Macros = append(plan.Macros, namespace.ModulePath)
		}
	}
	if automatic {
		for _, module := range project.Keys(plan.Required) {
			source := sources[module]
			if source == nil || explicit[module] {
				continue
			}
			if _, exists := roots[source.Name]; !exists {
				plan.Automatic[source.Name] = append(plan.Automatic[source.Name], *source)
			}
		}
	}
	return plan, nil
}

// Source-only local replacements need module projections before strict Go
// selection. Reuse the project planner so their TOML dependencies participate
// in MVS, without compiling sources or changing the authored module graph.
func nativeSourceOnlySelections(ctx context.Context, plan *NativeDependencies, modFile string, cfg config.Config, automatic bool) (map[string]nativeModuleSelection, error) {
	if !automatic {
		return nil, nil
	}
	manifest := plan.Manifest
	manifest.Dependencies = map[string]project.Dependency{}
	projection := false
	sourceModules := map[string]bool{}
	for _, replacement := range manifest.Native.Replace {
		if replacement.Path == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(replacement.Path, "go.mod")); !os.IsNotExist(err) {
			if err != nil {
				return nil, err
			}
			continue
		}
		found, err := project.Detect(replacement.Path)
		if err != nil {
			return nil, err
		}
		if found {
			projection = true
			if plan.Required[replacement.Module] != "" {
				sourceModules[replacement.Module] = true
			}
		}
	}
	if !projection {
		return nil, nil
	}
	for index, module := range project.Keys(sourceModules) {
		manifest.Dependencies[fmt.Sprintf("native_dependency_%d", index)] = project.Dependency{Go: module, Version: plan.Required[module]}
	}
	root, err := os.MkdirTemp(filepath.Dir(modFile), "selection-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	data, err := project.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, project.ManifestName), data, 0600); err != nil {
		return nil, err
	}
	sumFile := strings.TrimSuffix(modFile, ".mod") + ".sum"
	sums, err := os.ReadFile(sumFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(sums) != 0 {
		lock, err := json.Marshal(project.Lock{Version: 2, PlanningSums: string(sums)})
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(root, "goxide.lock"), lock, 0600); err != nil {
			return nil, err
		}
	}
	plans, err := project.LoadBuildPlansContext(ctx, root, cfg.Environment.GoTool, nil, true, false, false, filepath.Join(root, "target"), cfg.Environment.Target.OS+"/"+cfg.Environment.Target.Arch, false, true, cfg.Environment.ExecEnv())
	if err != nil {
		return nil, err
	}
	verified := map[string]bool{}
	checksumGroups := []string{string(sums), plans.Target.Lock.PlanningSums}
	for _, macro := range plans.Macros {
		checksumGroups = append(checksumGroups, macro.Lock.PlanningSums)
	}
	for _, data := range checksumGroups {
		for _, line := range strings.Split(data, "\n") {
			if line != "" {
				verified[line] = true
			}
		}
	}
	if len(verified) != 0 {
		if err := os.WriteFile(sumFile, []byte(strings.Join(project.Keys(verified), "\n")+"\n"), 0600); err != nil {
			return nil, err
		}
	}
	selections := map[string]nativeModuleSelection{}
	for module, selected := range plans.Target.Plan {
		selection := nativeModuleSelection{Path: selected.Path, Version: selected.Version, Dir: selected.Dir}
		if selected.Replace != nil {
			selection.Replace = &nativeModuleSelection{Path: selected.Replace.Path, Version: selected.Replace.Version, Dir: selected.Replace.Dir}
		}
		selections[module] = selection
	}
	for _, child := range plans.Target.Root.Children {
		if _, exists := selections[child.Module]; !exists {
			selections[child.Module] = nativeModuleSelection{Path: child.Module, Version: child.SelectedVersion, Dir: child.Root}
		}
	}
	return selections, nil
}

func readNativeSourceDependency(directory, module, version string) (*NativeSourceDependency, error) {
	found, err := project.Detect(directory)
	if err != nil || !found {
		return nil, err
	}
	dependency, err := project.Read(directory)
	if err != nil || dependency.Lib == nil {
		return nil, err
	}
	libraryPath := dependency.Lib.Path
	info, err := os.Stat(filepath.Join(directory, libraryPath))
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		libraryPath = "."
	}
	return &NativeSourceDependency{
		Name:       dependency.Lib.Name,
		Namespace:  paths.Namespace{ModulePath: module, PackagePath: path.Join(module, filepath.ToSlash(libraryPath)), Source: "module"},
		Dependency: project.Dependency{Go: module, Version: version}, Macro: dependency.Lib.ProcMacro,
	}, nil
}

// Activate adds only the dependency requested by frontend root discovery. A
// root demand is returned before the target reaches semantic checking.
func (plan *NativeDependencies) Activate(err error) bool {
	var demand *NativeRootDemand
	if !errors.As(err, &demand) {
		return false
	}
	candidates := plan.Automatic[demand.Alias]
	if len(candidates) != 1 {
		return false
	}
	if _, active := plan.Manifest.Dependencies[demand.Alias]; active {
		return false
	}
	candidate := candidates[0]
	plan.Manifest.Dependencies[demand.Alias] = candidate.Dependency
	if candidate.Macro {
		plan.Macros = append(plan.Macros, candidate.Namespace.ModulePath)
	}
	return true
}

// MissingNativeDependencies is actionable by an interactive CLI; preparation
// itself never modifies the author's Go module.
type MissingNativeDependencies struct {
	Root    string
	Modules []string
	Config  config.Config
	Cause   error
}

func (e *MissingNativeDependencies) Error() string {
	message := "generation needs dependencies absent from go.mod: " + strings.Join(e.Modules, ", ")
	if e.Cause != nil {
		message += "\n" + e.Cause.Error()
	}
	return message
}
func (e *MissingNativeDependencies) Unwrap() error { return e.Cause }
