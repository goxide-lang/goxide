package main

import (
	"bytes"
	"context"
	"errors"
	"go/types"
	"io"
	"os"
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
	if !automatic && len(plan.Manifest.Dependencies) == 0 {
		return nil, nil
	}
	selected := plan.Roots
	if automatic {
		owner.nativeRequirements = plan.Required
		selected, options.RequireRoot, err = owner.generationRoots(plan, importer())
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
		if len(plan.Manifest.Dependencies) == 0 {
			generated, err = lower.CompilePackage(files, owner.packagePath, importer(), selected, options)
		} else {
			generated, err = compilePreparedNativePackage(owner, plan, selected, cfg, tags, &diagnostics, files, importer, options, automatic)
		}
		var demand *compile.NativeRootDemand
		if !automatic || !errors.As(err, &demand) {
			if _, writeErr := io.Copy(stderr, &diagnostics); err == nil && writeErr != nil {
				err = writeErr
			}
			return generated, err
		}
		candidate := plan.Automatic[demand.Alias][0]
		plan.Manifest.Dependencies[demand.Alias] = candidate.Dependency
		if candidate.Macro {
			plan.Macros = append(plan.Macros, candidate.Namespace.ModulePath)
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

func compilePreparedNativePackage(owner *singleFileModule, plan *compile.NativeDependencies, selected paths.Roots, cfg config.Config, tags []string, stderr io.Writer, files map[string][]byte, importer func() types.Importer, options lower.PackageOptions, online bool) (map[string][]byte, error) {
	generated := map[string][]byte{}
	err := withProcHost(stderr, func(run func([]byte, pm.Request) ([]byte, error), diagnostic func(string, expand.ProcDiagnostic)) error {
		displays := map[string]string{}
		if owner.displayInput != "" {
			displays[owner.input] = owner.displayInput
		}
		prepared, err := compile.PrepareNative(context.Background(), owner.input, compile.NativeOptions{
			Options: compile.Options{Config: cfg, Roots: selected, Tags: tags, StableGo: options.StableGo, RunProcMacro: run, OnProcDiagnostic: diagnostic},
			Module:  owner.nativeModule(), Importer: importer, Sources: files, Automatic: online, DisplayNames: displays, Dependencies: plan, RequireRoot: options.RequireRoot,
			Generated: generated, Expanded: options.Expanded, LineDirectives: options.LineDirectives, RejectConfiguration: options.RejectConfiguration,
		})
		if err != nil {
			return err
		}
		defer prepared.Close()
		_, err = prepared.Build(context.Background())
		return err
	})
	return generated, err
}
