package lower

import (
	"context"
	"fmt"
	"path"

	"github.com/goxide-lang/goxide/internal/analysis"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// GenerationOptions changes Go output without repeating source analysis.
type GenerationOptions struct {
	StableGo       bool
	LineDirectives bool
}

// Generate emits Go from an already successful analysis. It never reads or
// writes source files and retains the analysis for subsequent queries.
func Generate(ctx context.Context, result *analysis.Result, options GenerationOptions) (map[string][]byte, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if result == nil || result.Err != nil || result.Checked == nil {
		return nil, fmt.Errorf("generation requires a successful source analysis")
	}
	output, err := lower(result.EmitFiles, result.Checked, emissionOptions{lineDirectives: options.LineDirectives, originals: result.Files, stableGo: options.StableGo})
	if err != nil {
		return nil, err
	}
	if len(result.Modules) == 0 {
		if err := ApplyTargetConstraints(output, result.Files, result.Config); err != nil {
			return nil, err
		}
	} else {
		for _, module := range result.Modules {
			files := map[string]*syntax.File{}
			for name, file := range result.Files {
				if result.ModuleOwner(path.Dir(name)) == module.Path {
					files[name] = file
				}
			}
			if err := ApplyTargetConstraints(output, files, module.Config); err != nil {
				return nil, err
			}
		}
		if err := result.CheckImportCycles(output); err != nil {
			return nil, err
		}
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return output, nil
}
