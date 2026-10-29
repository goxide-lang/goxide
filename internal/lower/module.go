package lower

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

// CompileModules emits and materializes one analyzed staged source graph.
func CompileModules(modules []analysis.Module) (err error) {
	ctx := context.Background()
	if len(modules) != 0 && modules[0].Context != nil {
		ctx = modules[0].Context
	}
	result, err := analysis.Analyze(ctx, analysis.Input{Modules: modules})
	if result != nil {
		defer func() { err = analysis.FormatSourceDiagnostic(err, result.Files, result.DisplayNames, result.Expanded) }()
	}
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return fmt.Errorf("at least one staging module is required")
	}
	native := modules[0].NativePackage
	stable := modules[0].StableGo || native != nil && native.StableGo
	if modules[0].OnSourcePackages != nil {
		if err := modules[0].OnSourcePackages(result.SourcePackages); err != nil {
			return err
		}
	}
	if result.Checked == nil {
		return nil
	}
	output, err := Generate(ctx, result, GenerationOptions{LineDirectives: native != nil && native.LineDirectives, StableGo: stable})
	if err != nil {
		return err
	}
	if native != nil {
		for name, display := range result.DisplayNames {
			if native.Generated != nil {
				native.Generated[display] = output[name]
			}
			if native.Expanded != nil && result.Resolved.Files[name] != nil {
				native.Expanded[display] = []byte(printer.Source(result.Resolved.Files[name].AST))
			}
		}
	}
	return Materialize(ctx, result, output, MaterializeOptions{GeneratedHeader: modules[0].StableGo, RemoveSources: true})
}

// PrepareModule lowers all .gox files beneath sourceDirs into a temporary Go
// module. Their paths are relative to the directories' common ancestor. The
// caller owns the returned directory and must remove it after use.
func PrepareModule(sourceDirs []string, modulePath string, cfg config.Config) (string, error) {
	if len(sourceDirs) == 0 || modulePath == "" {
		return "", fmt.Errorf("source directories and module path are required")
	}
	var root string
	names := map[string]bool{}
	for _, source := range sourceDirs {
		dir, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s: source directory required", dir)
		}
		if root == "" {
			root = dir
		} else {
			root = commonDirectory(root, dir)
		}
		files, err := analysis.ModuleSources(dir)
		if err != nil {
			return "", err
		}
		for _, name := range files {
			names[name] = true
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no .gox source files found")
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	dir, err := os.MkdirTemp("", "goxide-lower-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(dir)
		}
	}()
	module := []byte("module " + strconv.Quote(modulePath) + "\n\ngo 1.27\n")
	if source, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		lines := strings.Split(string(source), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "module ") {
				lines[i] = "module " + strconv.Quote(modulePath)
				break
			}
		}
		module = []byte(strings.Join(lines, "\n"))
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if roots, err := os.ReadFile(filepath.Join(root, rootconfig.Filename)); err == nil {
		if err := os.WriteFile(filepath.Join(dir, rootconfig.Filename), roots, 0o644); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), module, 0o644); err != nil {
		return "", err
	}
	if sum, err := os.ReadFile(filepath.Join(root, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, name := range ordered {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return "", err
		}
	}
	if err := CompileModules([]analysis.Module{{Path: modulePath, Dir: dir, Config: cfg}}); err != nil {
		return "", err
	}
	complete = true
	return dir, nil
}
