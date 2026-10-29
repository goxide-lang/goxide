//go:build race

package main

import (
	"context"

	"github.com/goxide-lang/goxide/internal/procmacro"
)

func init() {
	// Keep in-process CLI and development macros under race instrumentation;
	// production wall-clock budgets are checked without instrumentation.
	newWASIHost = func(ctx context.Context) (*procmacro.WASIHost, error) {
		return procmacro.NewWASIHostWithBudgets(ctx, procmacro.MaxInvocationTime*20, procmacro.MaxCompilationTime*20)
	}
}
