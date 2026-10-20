package lower

import (
	"fmt"
	"go/build"
	"go/build/constraint"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// Goxide sources obey the same filename and header selection as their Go output.
func matchesModuleSource(filename string, context build.Context) (bool, error) {
	context.OpenFile = func(string) (io.ReadCloser, error) { return os.Open(filename) }
	base := strings.TrimSuffix(filepath.Base(filename), ".gox") + ".go"
	return context.MatchFile(filepath.Dir(filename), base)
}

// ApplyTargetConstraints marks every generated file in each package whose
// configuration queried the target. files must retain the original AST pointers,
// including files left empty after Configure removed their declarations.
func ApplyTargetConstraints(output map[string][]byte, files map[string]*syntax.File, cfg config.Config) error {
	targetFiles := map[*syntax.File]bool{}
	for _, file := range cfg.Reads.TargetFiles() {
		targetFiles[file] = true
	}
	type packageID struct{ directory, name string }
	packages := map[packageID]bool{}
	for name, file := range files {
		if targetFiles[file] {
			packages[packageID{filepath.Dir(name), file.Package.Name}] = true
		}
	}
	if len(packages) == 0 {
		return nil
	}
	if cfg.Environment == nil {
		return fmt.Errorf("target-dependent generation requires an explicit compilation environment")
	}
	target := cfg.Environment.Target
	var platform constraint.Expr = &constraint.TagExpr{Tag: target.OS}
	// Go intentionally aliases these tags. Exact target cfg predicates do not.
	alias := map[string]string{"linux": "android", "darwin": "ios", "solaris": "illumos"}[target.OS]
	if alias != "" {
		platform = &constraint.AndExpr{X: platform, Y: &constraint.NotExpr{X: &constraint.TagExpr{Tag: alias}}}
	}
	platform = &constraint.AndExpr{X: platform, Y: &constraint.TagExpr{Tag: target.Arch}}
	for name, file := range files {
		data, ok := output[name]
		if !ok || !packages[packageID{filepath.Dir(name), file.Package.Name}] {
			continue
		}
		constrained, err := andBuildConstraint(data, platform)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		output[name] = constrained
	}
	return nil
}

func andBuildConstraint(data []byte, platform constraint.Expr) ([]byte, error) {
	var current, legacy constraint.Expr
	var kept []string
	header := true
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "package ") {
			header = false
		}
		if header && (constraint.IsGoBuild(line) || constraint.IsPlusBuild(line)) {
			parsed, err := constraint.Parse(line)
			if err != nil {
				return nil, err
			}
			if constraint.IsGoBuild(line) {
				current = parsed
			} else if legacy == nil {
				legacy = parsed
			} else {
				legacy = &constraint.AndExpr{X: legacy, Y: parsed}
			}
			continue
		}
		kept = append(kept, line)
	}
	if current == nil {
		current = legacy
	}
	if current != nil {
		platform = &constraint.AndExpr{X: platform, Y: current}
	}
	return []byte("//go:build " + platform.String() + "\n\n" + strings.TrimLeft(strings.Join(kept, "\n"), "\n")), nil
}
