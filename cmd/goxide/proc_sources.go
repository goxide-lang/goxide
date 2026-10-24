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
	"strings"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/naming"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func nativeModuleCommand(root string, cfg config.Config, args ...string) ([]byte, error) {
	command := exec.Command(cfg.Environment.GoTool, args...)
	command.Dir, command.Env = root, singleFileEnvironment(cfg.Environment.ExecEnv())
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("native source module: %w: %s", err, stderr.String())
	}
	return output, nil
}

// nativeSourceDependencies gives explicit source roots their selected Go
// module's authored dependency graph. It does not infer aliases or fetch sources.
// modFile must be private: completing Go's selected graph may update it and its sum.
func nativeSourceDependencies(root, modFile string, roots paths.Roots, cfg config.Config) (project.Manifest, paths.Roots, []string, error) {
	data, err := nativeModuleCommand(root, cfg, "mod", "edit", "-json", modFile)
	if err != nil {
		return project.Manifest{}, nil, nil, err
	}
	var mod project.ModFile
	if err := json.Unmarshal(data, &mod); err != nil {
		return project.Manifest{}, nil, nil, err
	}
	manifest := project.Manifest{Module: mod.Module.Path, GoVersion: mod.Go, Toolchain: mod.Toolchain, Codegen: project.CodegenConfig{GoSugar: cfg.GoSugar}, Dependencies: map[string]project.Dependency{}}
	manifest.Native.Godebug = mod.Godebug
	required := map[string]string{}
	for _, item := range mod.Require {
		manifest.Native.Require = append(manifest.Native.Require, project.GoRequire{Module: item.Path, Version: item.Version, Indirect: item.Indirect})
		required[item.Path] = item.Version
	}
	for _, item := range mod.Exclude {
		manifest.Native.Exclude = append(manifest.Native.Exclude, project.GoExclude{Module: item.Path, Version: item.Version})
	}
	for _, item := range mod.Replace {
		replacement := project.GoReplace{Module: item.Old.Path, Version: item.Old.Version, With: item.New.Path, WithVersion: item.New.Version}
		if item.New.Version == "" {
			replacement.With = ""
			replacement.Path = item.New.Path
			if !filepath.IsAbs(replacement.Path) {
				replacement.Path = filepath.Join(root, replacement.Path)
			}
		}
		manifest.Native.Replace = append(manifest.Native.Replace, replacement)
	}
	updated := maps.Clone(roots)
	var macros []string
	type moduleSelection struct {
		Path, Version, Dir string
		Replace            *struct{ Path, Version, Dir string }
	}
	var selections map[string]moduleSelection
	for _, alias := range project.Keys(roots) {
		namespace := roots[alias]
		if namespace.Source == "toolchain" || namespace.ModulePath == "" || namespace.ModulePath == mod.Module.Path {
			continue
		}
		if selections == nil {
			selections = map[string]moduleSelection{}
			// An authored replacement may legitimately have no Go manifest yet.
			// Retain Go's selected versions for the other explicit roots; source
			// planning below validates the authored replacement independently.
			selected, queryErr := nativeModuleCommand(root, cfg, "list", "-e", "-mod=mod", "-modfile="+modFile, "-m", "-json", "all")
			if queryErr == nil {
				decoder := json.NewDecoder(bytes.NewReader(selected))
				for {
					var selection moduleSelection
					if err := decoder.Decode(&selection); err == io.EOF {
						break
					} else if err != nil {
						return manifest, nil, nil, err
					}
					selections[selection.Path] = selection
				}
			}
		}
		selection := selections[namespace.ModulePath]
		directory, version := selection.Dir, selection.Version
		if selection.Replace != nil && selection.Replace.Dir != "" {
			directory = selection.Replace.Dir
		}
		if version == "" {
			version = required[namespace.ModulePath]
		}
		// Authored Goxide replacements have no go.mod for Go to read yet.
		// Their source graph is resolved by the normal project planner below.
		if directory == "" && version != "" {
			for _, replacement := range manifest.Native.Replace {
				if replacement.Module == namespace.ModulePath && replacement.Path != "" && (replacement.Version == "" || replacement.Version == version) {
					directory = replacement.Path
					if replacement.Version == version {
						break
					}
				}
			}
		}
		if directory == "" {
			continue
		}
		found, err := project.Detect(directory)
		if err != nil {
			return manifest, nil, nil, err
		}
		if !found {
			continue
		}
		dependency, err := project.Read(directory)
		if err != nil {
			return manifest, nil, nil, err
		}
		if dependency.Lib == nil {
			continue
		}
		libraryPath := dependency.Lib.Path
		info, err := os.Stat(filepath.Join(directory, libraryPath))
		if err != nil {
			return manifest, nil, nil, err
		}
		if !info.IsDir() {
			libraryPath = "."
		}
		canonical := path.Join(namespace.ModulePath, filepath.ToSlash(libraryPath))
		// A root configured for a particular package must not silently acquire a
		// different library. Module-only roots select the declared library entry.
		if namespace.PackagePath != "" && namespace.PackagePath != canonical {
			continue
		}
		request := project.Dependency{Go: namespace.ModulePath, Version: version}
		manifest.Dependencies[alias] = request
		namespace.PackagePath, namespace.GoPrefix = canonical, ""
		updated[alias] = namespace
		if dependency.Lib.ProcMacro {
			macros = append(macros, namespace.ModulePath)
		}
	}
	return manifest, updated, macros, nil
}

// compileNativePackage preserves the selected files and compiles them together
// with their ordinary dependencies exactly once after all macro roles are built.
// A nil output without an error leaves source-less compilation to its caller.
func compileNativePackage(owner *singleFileModule, roots paths.Roots, cfg config.Config, tags []string, stderr io.Writer, files map[string][]byte, importer func() types.Importer, options lower.PackageOptions) (map[string][]byte, error) {
	for _, name := range project.Keys(files) {
		if err := naming.Source(name, files[name]); err != nil {
			return nil, err
		}
	}
	if owner == nil {
		return nil, nil
	}
	manifest, selected, macros, err := nativeSourceDependencies(owner.root, owner.modFile, roots, cfg)
	if err != nil {
		return nil, err
	}
	if len(manifest.Dependencies) == 0 {
		return nil, nil
	}
	directory := filepath.Join(owner.temporary, "sources")
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, err
	}
	displayNames := map[string]string{}
	for _, name := range project.Keys(files) {
		relative, err := filepath.Rel(owner.root, name)
		if err != nil {
			return nil, err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("native source is outside its owning module: %s", name)
		}
		if extension := filepath.Ext(name); extension != ".go" && extension != ".gox" {
			relative += ".gox"
		}
		target := filepath.Join(directory, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, files[name], 0600); err != nil {
			return nil, err
		}
		if filepath.Ext(name) != ".go" {
			display := name
			if name == owner.input && owner.displayInput != "" {
				display = owner.displayInput
			}
			displayNames[path.Join(manifest.Module, filepath.ToSlash(relative))] = display
		}
	}
	helper, err := os.MkdirTemp(directory, "goxide_cli_entry_")
	if err != nil {
		return nil, err
	}
	manifest.Bins = []project.Target{{Name: "native", Path: filepath.Base(helper)}}
	data, err := project.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(directory, project.ManifestName), data, 0600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(helper, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		return nil, err
	}
	generated := map[string][]byte{}
	err = withProcHost(stderr, func(run func([]byte, pm.Request) ([]byte, error), diagnostic func(string, expand.ProcDiagnostic)) error {
		prepared, err := compile.PrepareProject(directory, compile.Options{GoTool: cfg.Environment.GoTool, TargetDir: filepath.Join(owner.temporary, "target"), Offline: true, Roots: selected, NativeFeatures: &cfg.Features, Config: cfg, Tags: tags, RunProcMacro: run, OnProcDiagnostic: diagnostic,
			PrepareNativePackage: func(modules []lower.Module) error {
				if err := owner.usePreparedModules(modules[0].Dir, macros); err != nil {
					return err
				}
				modules[0].NativePackage = &lower.NativePackage{Importer: importer(), DisplayNames: displayNames, Generated: generated, Expanded: options.Expanded, LineDirectives: options.LineDirectives, RejectConfiguration: options.RejectConfiguration}
				return nil
			}})
		if err != nil {
			return err
		}
		return owner.usePreparedModules(prepared.Root, macros)
	})
	return generated, err
}

// Only the private modfile changes; authored bytes and unrelated directives are
// retained. Relative generated replacements belong to the prepared module.
func (owner *singleFileModule) usePreparedModules(root string, macros []string) error {
	data, err := nativeModuleCommand(root, owner.config, "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var mod project.ModFile
	if err := json.Unmarshal(data, &mod); err != nil {
		return err
	}
	args := []string{"mod", "edit", "-modfile=" + owner.modFile}
	for _, module := range macros {
		args = append(args, "-droprequire="+module)
	}
	for _, requirement := range mod.Require {
		args = append(args, "-require="+requirement.Path+"@"+requirement.Version)
	}
	for _, replacement := range mod.Replace {
		old, value := replacement.Old.Path, replacement.New.Path
		if replacement.Old.Version != "" {
			old += "@" + replacement.Old.Version
		}
		if replacement.New.Version != "" {
			value += "@" + replacement.New.Version
		} else if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		args = append(args, "-replace="+old+"="+value)
	}
	_, err = nativeModuleCommand(owner.root, owner.config, args...)
	return err
}
