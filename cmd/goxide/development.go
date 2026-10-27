package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/spf13/cobra"
)

type developmentRequest struct {
	Input, Package, Run, Delve string
	Test                       bool
	Compiler                   compile.Options
}

func devCommand(out, stderr io.Writer) *cobra.Command {
	command := cliCommand("dev", "Run editor testing and debugging services")
	command.RunE = func(command *cobra.Command, args []string) error {
		if len(args) == 0 {
			return usageError(command, fmt.Errorf("choose targets, tests, test, or debug"))
		}
		child := childCommand(command, args[0])
		if child == nil {
			return usageError(command, fmt.Errorf("unknown development action %q", args[0]))
		}
		return executeCLI(child, args[1:])
	}
	for _, action := range []string{"targets", "tests", "test", "debug"} {
		req := developmentRequest{}
		var features, tags string
		child := cliCommand(action+" [flags] [INPUT]", "Provide editor "+action+" for the selected input")
		flags := child.Flags()
		flags.StringVar(&req.Compiler.GoTool, "go", "go", "Go compiler executable")
		flags.StringVar(&req.Compiler.Target, "target", "", "selected compilation target")
		flags.StringVar(&req.Compiler.Bin, "bin", "", "select project binary")
		flags.StringVar(&req.Compiler.Example, "example", "", "select project example")
		flags.BoolVar(&req.Compiler.Lib, "lib", false, "select project library")
		flags.StringVar(&features, "features", "", "enabled features (comma/space separated)")
		flags.StringVar(&tags, "tags", "", "Go build tags (comma/space separated)")
		flags.BoolVar(&req.Compiler.NoDefaultFeatures, "no-default-features", false, "disable project default features")
		flags.BoolVar(&req.Compiler.Offline, "offline", false, "use cached dependencies")
		flags.BoolVar(&req.Compiler.Locked, "locked", false, "require unchanged dependency lock")
		if action == "test" || action == "debug" {
			flags.StringVar(&req.Package, "package", "", "one package from dev tests")
			flags.StringVar(&req.Run, "run", "", "Go test name pattern")
		}
		if action == "debug" {
			flags.StringVar(&req.Delve, "dlv", "dlv", "original Delve DAP executable")
			flags.BoolVar(&req.Test, "test", false, "debug the selected test package")
		}
		child.RunE = func(child *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError(child, fmt.Errorf("at most one INPUT is allowed"))
			}
			req.Input = "."
			if len(args) == 1 {
				req.Input = args[0]
			}
			req.Compiler.Features, req.Compiler.Tags = featureNames(features), featureNames(tags)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if action == "targets" {
				return developmentTargets(req.Input, out)
			}
			if action == "debug" {
				return debugDevelopment(ctx, req, os.Stdin, out, stderr)
			}
			view, err := prepareDevelopment(ctx, req, stderr)
			if err != nil {
				return err
			}
			defer view.Close()
			if action == "tests" {
				tests, err := view.Tests(ctx)
				if err != nil {
					return err
				}
				return json.NewEncoder(out).Encode(tests)
			}
			return view.Test(ctx, req.Package, req.Run, out, stderr)
		}
		command.AddCommand(child)
	}
	return command
}

type developmentTarget struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	Path             string   `json:"path"`
	RequiredFeatures []string `json:"requiredFeatures,omitempty"`
}

func developmentTargets(input string, out io.Writer) error {
	input, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	info, err := os.Stat(input)
	if err != nil {
		return err
	}
	if !info.IsDir() && filepath.Base(input) != project.ManifestName {
		return json.NewEncoder(out).Encode([]developmentTarget{{Name: filepath.Base(input), Kind: "file", Path: input}})
	}
	if !info.IsDir() {
		input = filepath.Dir(input)
	}
	root, err := project.DiscoverProject(input)
	if err != nil {
		return err
	}
	if root == "" {
		return json.NewEncoder(out).Encode([]developmentTarget{{Name: filepath.Base(input), Kind: "directory", Path: input}})
	}
	manifest, err := project.Read(root)
	if err != nil {
		return err
	}
	targets := []developmentTarget{}
	add := func(kind string, target project.Target) {
		if !target.ProcMacro {
			targets = append(targets, developmentTarget{Name: target.Name, Kind: kind, Path: filepath.Join(root, target.Path), RequiredFeatures: target.RequiredFeatures})
		}
	}
	for _, target := range manifest.Bins {
		add("bin", target)
	}
	for _, target := range manifest.Examples {
		add("example", target)
	}
	if manifest.Lib != nil {
		add("lib", *manifest.Lib)
	}
	return json.NewEncoder(out).Encode(targets)
}

func developmentFlags(tags []string) []string {
	return []string{"-buildvcs=false", "-mod=readonly", "-tags=" + strings.Join(tags, ",")}
}
