//go:build race

package lsp

import (
	"context"

	"github.com/goxide-lang/goxide/internal/procmacro"
)

func init() {
	// Keep real workspace and candidate macro sessions under race instrumentation;
	// production wall-clock budgets are checked without instrumentation.
	newWorkspaceWASIHost = func(ctx context.Context) (*procmacro.WASIHost, error) {
		return procmacro.NewWASIHostWithBudgets(ctx, procmacro.MaxInvocationTime*20, procmacro.MaxCompilationTime*20)
	}
}
