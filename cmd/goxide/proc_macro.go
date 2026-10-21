package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/procmacro"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

func prepareProject(directory string, options compile.Options, stderr io.Writer) (*compile.Result, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var host *procmacro.WASIHost
	defer func() {
		if host != nil {
			_ = host.Close()
		}
	}()
	options.RunProcMacro = func(wasm []byte, request pm.Request) ([]byte, error) {
		// Only intercept interrupts while a cancellable guest is active. Other
		// preparation steps retain the CLI's normal interrupt behavior.
		callCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		detach := context.AfterFunc(callCtx, cancel)
		defer func() {
			detach()
			stop()
		}()
		if host == nil {
			var err error
			host, err = procmacro.NewWASIHost(ctx)
			if err != nil {
				return nil, err
			}
		}
		return host.Run(wasm, request)
	}
	reportedErrors := false
	options.OnProcDiagnostic = func(filename string, diagnostic expand.ProcDiagnostic) {
		if diagnostic.Severity == pm.Error {
			reportedErrors = true
		}
		location := diagnostic.Location
		if location.File != nil {
			line, column := 1, int(location.Span.Start)+1
			for index, offset := range location.File.LineStarts {
				if offset > location.Span.Start {
					break
				}
				line, column = index+1, int(location.Span.Start-offset)+1
			}
			filename = fmt.Sprintf("%s:%d:%d", filename, line, column)
		}
		fmt.Fprintf(stderr, "%s: %s: %s\n", filename, diagnostic.Severity, diagnostic.Message)
	}
	result, err := compile.PrepareProject(directory, options)
	if reportedErrors {
		err = lower.SummarizeProcDiagnostics(err)
	}
	return result, err
}
