package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/types"
	"io"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
)

type nativeSourceDependency struct {
	name       string
	namespace  paths.Namespace
	dependency project.Dependency
	macro      bool
}

type nativeDependencies struct {
	manifest  project.Manifest
	roots     paths.Roots
	macros    []string
	automatic map[string][]nativeSourceDependency
	required  map[string]string
}

type nativeModuleSelection struct {
	Path, Version, Dir string
	Replace            *nativeModuleSelection
	Error              *struct{ Err string }
}

type nativeRootDemand struct{ alias string }

func (e *nativeRootDemand) Error() string { return "source dependency required for root " + e.alias }

func (plan *nativeDependencies) generationRoots(owner *singleFileModule, importer types.Importer) (paths.Roots, func(string, paths.Namespace) error, error) {
	roots := maps.Clone(plan.roots)
	standard := paths.NewGoNamespaces(importer)
	for _, alias := range project.Keys(plan.automatic) {
		_, found, err := standard.Root(alias)
		if err != nil {
			return nil, nil, err
		}
		if found {
			continue // Automatic names never replace a real toolchain namespace.
		}
		roots[alias] = plan.automatic[alias][0].namespace
	}
	missing := map[string]string{}
	missingRequest := &missingGenerationDependencies{Root: owner.root, Config: owner.config}
	for _, alias := range []string{"std", "serde"} {
		module := "github.com/goxide-lang/" + alias
		if plan.required[module] != "" {
			continue
		}
		if _, exists := roots[alias]; exists {
			continue
		}
		explicit := false
		for _, namespace := range plan.roots {
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
			if !slices.Contains(missingRequest.Modules, module) {
				missingRequest.Modules = append(missingRequest.Modules, module)
				slices.Sort(missingRequest.Modules)
			}
			return missingRequest
		}
		candidates := plan.automatic[alias]
		if namespace.Source == "toolchain" {
			return nil
		}
		if len(candidates) == 0 {
			if _, source := plan.manifest.Dependencies[alias]; source {
				if owner.usedSourceRoots == nil {
					owner.usedSourceRoots = map[string]paths.Namespace{}
				}
				owner.usedSourceRoots[namespace.ModulePath] = plan.roots[alias]
			}
			return nil
		}
		if len(candidates) > 1 {
			var modules []string
			for _, candidate := range candidates {
				modules = append(modules, candidate.namespace.ModulePath)
			}
			return fmt.Errorf("ambiguous default dependency root %q: %s; choose an explicit alias in goxide.roots.toml", alias, strings.Join(modules, ", "))
		}
		if _, active := plan.manifest.Dependencies[alias]; !active {
			return &nativeRootDemand{alias: alias}
		}
		if owner.usedSourceRoots == nil {
			owner.usedSourceRoots = map[string]paths.Namespace{}
		}
		owner.usedSourceRoots[namespace.ModulePath] = candidates[0].namespace
		return nil
	}
	return roots, require, nil
}

// Native generation may acquire the selected module graph, but every command
// receives the consumer's private modfile. Go's captured proxy, private-module
// and checksum policy remains authoritative; execution/import helpers stay offline.
func nativeDependencyCommand(root string, cfg config.Config, args ...string) ([]byte, error) {
	command := exec.Command(cfg.Environment.GoTool, args...)
	command.Dir, command.Env = root, cfg.Environment.ExecEnv()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		var download struct{ Error string }
		if json.Unmarshal(output, &download) == nil && download.Error != "" {
			return nil, fmt.Errorf("native source module: %s", download.Error)
		}
		return nil, fmt.Errorf("native source module: %w: %s", err, stderr.String())
	}
	return output, nil
}

// nativeSourceDependencies records authored require candidates before Go can
// complete the private graph. Automatic candidates are only metadata: callers
// activate their dependency edges when the frontend actually selects a root.
func nativeSourceDependencies(root, modFile string, roots paths.Roots, cfg config.Config, automatic bool) (*nativeDependencies, error) {
	data, err := nativeModuleCommand(root, cfg, "mod", "edit", "-json", modFile)
	if err != nil {
		return nil, err
	}
	var mod project.ModFile
	if err := json.Unmarshal(data, &mod); err != nil {
		return nil, err
	}
	plan := &nativeDependencies{
		manifest: project.Manifest{Module: mod.Module.Path, GoVersion: mod.Go, Toolchain: mod.Toolchain, Codegen: project.CodegenConfig{GoSugar: cfg.GoSugar}, Dependencies: map[string]project.Dependency{}},
		roots:    maps.Clone(roots), automatic: map[string][]nativeSourceDependency{}, required: map[string]string{},
	}
	if plan.roots == nil {
		plan.roots = paths.Roots{}
	}
	plan.manifest.Native.Godebug = mod.Godebug
	for _, item := range mod.Require {
		plan.manifest.Native.Require = append(plan.manifest.Native.Require, project.GoRequire{Module: item.Path, Version: item.Version, Indirect: item.Indirect})
		plan.required[item.Path] = item.Version
	}
	for _, item := range mod.Exclude {
		plan.manifest.Native.Exclude = append(plan.manifest.Native.Exclude, project.GoExclude{Module: item.Path, Version: item.Version})
	}
	for _, item := range mod.Replace {
		replacement := project.GoReplace{Module: item.Old.Path, Version: item.Old.Version, With: item.New.Path, WithVersion: item.New.Version}
		if item.New.Version == "" {
			replacement.With, replacement.Path = "", item.New.Path
			if !filepath.IsAbs(replacement.Path) {
				replacement.Path = filepath.Join(root, replacement.Path)
			}
		}
		plan.manifest.Native.Replace = append(plan.manifest.Native.Replace, replacement)
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
		for module := range plan.required {
			if module != mod.Module.Path {
				modules[module] = true
			}
		}
	}
	if len(modules) == 0 {
		return plan, nil
	}
	selections, err := nativeSourceOnlySelections(plan, modFile, cfg, automatic)
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
		selected, queryErr := query(root, cfg, arguments...)
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
	sources := map[string]*nativeSourceDependency{}
	for _, module := range project.Keys(modules) {
		selection := selections[module]
		directory, version := selection.Dir, selection.Version
		if version == "" {
			version = plan.required[module]
		}
		if selection.Replace != nil && selection.Replace.Dir != "" {
			directory = selection.Replace.Dir
		}
		// An authored local Goxide replacement need not have a Go manifest yet.
		// Specific-version replaces take precedence over wildcard replaces.
		local := ""
		for _, replacement := range plan.manifest.Native.Replace {
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
				if plan.required[module] == "" {
					continue // Unused explicit namespaces need not belong to this module's graph.
				}
				return nil, fmt.Errorf("native source module %s is not in the selected Go module graph", module)
			}
			if directory == "" {
				actual, actualVersion := module, version
				if selection.Replace != nil {
					actual, actualVersion = selection.Replace.Path, selection.Replace.Version
				}
				download, err := nativeDependencyCommand(root, cfg, "mod", "download", "-json", "-modfile="+modFile, actual+"@"+actualVersion)
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
		if namespace.PackagePath != "" && namespace.PackagePath != source.namespace.PackagePath {
			continue
		}
		plan.manifest.Dependencies[alias] = source.dependency
		namespace.PackagePath, namespace.GoPrefix = source.namespace.PackagePath, ""
		plan.roots[alias] = namespace
		if source.macro {
			plan.macros = append(plan.macros, namespace.ModulePath)
		}
	}
	if automatic {
		for _, module := range project.Keys(plan.required) {
			source := sources[module]
			if source == nil || explicit[module] {
				continue
			}
			if _, exists := roots[source.name]; !exists {
				plan.automatic[source.name] = append(plan.automatic[source.name], *source)
			}
		}
	}
	return plan, nil
}

// Source-only local replacements need module projections before strict Go
// selection. Reuse the project planner so their TOML dependencies participate
// in MVS, without compiling sources or changing the authored module graph.
func nativeSourceOnlySelections(plan *nativeDependencies, modFile string, cfg config.Config, automatic bool) (map[string]nativeModuleSelection, error) {
	if !automatic {
		return nil, nil
	}
	manifest := plan.manifest
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
			if plan.required[replacement.Module] != "" {
				sourceModules[replacement.Module] = true
			}
		}
	}
	if !projection {
		return nil, nil
	}
	for index, module := range project.Keys(sourceModules) {
		manifest.Dependencies[fmt.Sprintf("native_dependency_%d", index)] = project.Dependency{Go: module, Version: plan.required[module]}
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
	plans, err := project.LoadBuildPlans(root, cfg.Environment.GoTool, nil, true, false, false, filepath.Join(root, "target"), cfg.Environment.Target.OS+"/"+cfg.Environment.Target.Arch, false, true, cfg.Environment.ExecEnv())
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

func readNativeSourceDependency(directory, module, version string) (*nativeSourceDependency, error) {
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
	return &nativeSourceDependency{
		name:       dependency.Lib.Name,
		namespace:  paths.Namespace{ModulePath: module, PackagePath: path.Join(module, filepath.ToSlash(libraryPath)), Source: "module"},
		dependency: project.Dependency{Go: module, Version: version}, macro: dependency.Lib.ProcMacro,
	}, nil
}
