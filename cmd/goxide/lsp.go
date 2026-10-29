package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/goxide-lang/goxide/internal/lsp"
	"github.com/spf13/cobra"
)

func lspCommand(out, stderr io.Writer) *cobra.Command {
	options := lsp.ServerOptions{Log: stderr, Version: version}
	var features, tags string
	command := cliCommand("lsp [flags] [INPUT]", "Serve Goxide language features over standard input/output")
	flags := command.Flags()
	flags.StringVar(&options.Gopls, "gopls", "gopls", "original gopls executable (Go services are optional)")
	flags.StringVar(&options.Compiler.GoTool, "go", "go", "Go compiler executable")
	flags.StringVar(&options.Compiler.Target, "target", "", "selected compilation target")
	flags.StringVar(&options.Compiler.TargetDir, "target-dir", "", "managed project artifacts directory")
	flags.StringVar(&options.Compiler.Bin, "bin", "", "select project binary")
	flags.StringVar(&options.Compiler.Example, "example", "", "select project example")
	flags.BoolVar(&options.Compiler.Lib, "lib", false, "select project library")
	flags.BoolVar(&options.Compiler.Offline, "offline", false, "use only cached dependencies")
	flags.BoolVar(&options.Compiler.Locked, "locked", false, "require unchanged dependency lock")
	flags.StringVar(&features, "features", "", "enabled features (comma/space separated)")
	flags.StringVar(&tags, "tags", "", "Go build tags (comma/space separated)")
	flags.BoolVar(&options.Compiler.NoDefaultFeatures, "no-default-features", false, "disable project default features")
	command.RunE = func(command *cobra.Command, args []string) error {
		if len(args) > 1 {
			return usageError(command, fmt.Errorf("at most one INPUT is allowed"))
		}
		if len(args) == 1 {
			options.Directory = args[0]
		}
		split := func(value string) []string {
			return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' })
		}
		options.Compiler.Features, options.Compiler.Tags = split(features), split(tags)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return lsp.Serve(ctx, os.Stdin, out, options)
	}
	return command
}
