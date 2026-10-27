package lower

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/source"
)

// MaterializeOptions controls the caller-owned Go projection directory.
type MaterializeOptions struct {
	GeneratedHeader bool
	RemoveSources   bool // Staging modules consume their source files after generation.
}

// Materialize writes a previously generated projection. Source analysis and
// generation remain usable without calling this filesystem operation.
func Materialize(ctx context.Context, result *analysis.Result, output map[string][]byte, options MaterializeOptions) error {
	if result == nil || result.Err != nil {
		return fmt.Errorf("materialization requires a successful analysis")
	}
	if result.ProjectionOnly {
		return fmt.Errorf("editor-only projection cannot be materialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var names []string
	for name := range output {
		names = append(names, name)
	}
	sort.Strings(names)
	// Check every destination before changing any source or generated file.
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		target := result.DiskOutputs[name]
		if target == "" {
			return fmt.Errorf("%s: generated Go has no output path", name)
		}
		data, err := os.ReadFile(target)
		if err == nil && !source.IsGenerated(target, data) {
			return fmt.Errorf("%s: generated Go would overwrite an existing file", name)
		}
		if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		data := output[name]
		if options.GeneratedHeader {
			data = append([]byte(source.GeneratedHeader), data...)
		}
		target := result.DiskOutputs[name]
		if previous, err := os.ReadFile(target); err == nil && bytes.Equal(previous, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return err
		}
	}
	if options.RemoveSources {
		for _, name := range result.RemovedFiles {
			if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		for _, name := range names {
			if err := os.Remove(result.DiskNames[name]); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
