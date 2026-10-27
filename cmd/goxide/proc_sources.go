package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
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

// compileNativePackage preserves the selected files and compiles them together
// with only the ordinary dependencies selected by the frontend's root lookup.
// A nil output without an error leaves source-less compilation to its caller.
func compileNativePackage(owner *singleFileModule, roots paths.Roots, cfg config.Config, tags []string, stderr io.Writer, files map[string][]byte, importer func() types.Importer, options lower.PackageOptions, automatic bool) (map[string][]byte, error) {
	for _, name := range project.Keys(files) {
		if err := naming.Source(name, files[name]); err != nil {
			return nil, err
		}
	}
	if owner == nil {
		return nil, nil
	}
	plan, err := nativeSourceDependencies(owner.root, owner.modFile, roots, cfg, automatic)
	if err != nil {
		return nil, err
	}
	if !automatic && len(plan.manifest.Dependencies) == 0 {
		return nil, nil
	}
	selected := plan.roots
	if automatic {
		owner.nativeRequirements = plan.required
		selected, options.RequireRoot, err = plan.generationRoots(owner, importer())
		if err != nil {
			return nil, err
		}
	}
	configuration, err := os.ReadFile(owner.modFile)
	if err != nil {
		return nil, err
	}
	sumFile := strings.TrimSuffix(owner.modFile, ".mod") + ".sum"
	sums, err := os.ReadFile(sumFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	hadSums := err == nil
	for {
		var generated map[string][]byte
		var diagnostics bytes.Buffer
		if len(plan.manifest.Dependencies) == 0 {
			generated, err = lower.CompilePackage(files, owner.packagePath, importer(), selected, options)
		} else {
			generated, err = compilePreparedNativePackage(owner, plan.manifest, selected, plan.macros, cfg, tags, &diagnostics, files, importer, options, automatic)
		}
		var demand *nativeRootDemand
		if !automatic || !errors.As(err, &demand) {
			if _, writeErr := io.Copy(stderr, &diagnostics); err == nil && writeErr != nil {
				err = writeErr
			}
			return generated, err
		}
		candidate := plan.automatic[demand.alias][0]
		plan.manifest.Dependencies[demand.alias] = candidate.dependency
		if candidate.macro {
			plan.macros = append(plan.macros, candidate.namespace.ModulePath)
		}
		// A failed preparation may have installed generated replacements in the
		// private module. Each new root starts from the same selected Go graph.
		if err := os.WriteFile(owner.modFile, configuration, 0600); err != nil {
			return nil, err
		}
		if hadSums {
			if err := os.WriteFile(sumFile, sums, 0600); err != nil {
				return nil, err
			}
		} else if err := os.Remove(sumFile); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		clear(options.Expanded)
	}
}

func compilePreparedNativePackage(owner *singleFileModule, manifest project.Manifest, selected paths.Roots, macros []string, cfg config.Config, tags []string, stderr io.Writer, files map[string][]byte, importer func() types.Importer, options lower.PackageOptions, online bool) (map[string][]byte, error) {
	directory, err := os.MkdirTemp(owner.temporary, "sources-")
	if err != nil {
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
		prepared, err := compile.PrepareProject(directory, compile.Options{GoTool: cfg.Environment.GoTool, TargetDir: directory + "-target", Offline: !online, Roots: selected, NativeFeatures: &cfg.Features, NativeGeneration: online, Config: cfg, Tags: tags, RunProcMacro: run, OnProcDiagnostic: diagnostic,
			PrepareNativePackage: func(modules []analysis.Module) error {
				if err := owner.usePreparedModules(modules[0].Dir, macros); err != nil {
					return err
				}
				modules[0].NativePackage = &analysis.NativePackage{Importer: importer(), DisplayNames: displayNames, Generated: generated, Expanded: options.Expanded, LineDirectives: options.LineDirectives, RejectConfiguration: options.RejectConfiguration, RequireRoot: options.RequireRoot, StableGo: options.StableGo}
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
