// Package naming rejects obsolete pre-1.0 inputs instead of silently treating
// them as native Go, ordinary comments or reusable generated artifacts.
package naming

import (
	"fmt"
	"go/scanner"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
)

func Environment() error {
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "HGO_") {
			return fmt.Errorf("%s is removed; use GOXIDE_%s and unset the old variable (also when both are set)", key, strings.TrimPrefix(key, "HGO_"))
		}
	}
	return nil
}

func Path(path string) error {
	name := filepath.Base(path)
	if strings.HasSuffix(strings.ToLower(name), ".hgo") {
		return fmt.Errorf("%s: legacy .hgo source is unsupported; rename to .gox (remove same-name conflicts) before generating", path)
	}
	if strings.HasPrefix(name, ".hgo.") || strings.HasPrefix(name, ".hgo-") || name == "hgo_project_config.go" {
		return fmt.Errorf("%s: legacy generated artifact; regenerate all producers and consumers with Goxide", path)
	}
	return nil
}

func Source(filename string, src []byte) error {
	if err := Path(filename); err != nil {
		return err
	}
	fs := token.NewFileSet()
	var s scanner.Scanner
	s.Init(fs.AddFile(filename, -1, len(src)), src, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT && strings.HasPrefix(lit, "//hgo:") {
			return fmt.Errorf("%s: legacy hgo directive; migrate to //goxide: before compiling", fs.Position(pos))
		}
	}
	return nil
}

func Package(pkg *types.Package) error {
	for _, marker := range []string{"HgoEnumMetadata", "HgoReferenceMetadata", "HgoUseMetadata", "HgoSumABI"} {
		if pkg.Scope().Lookup(marker) != nil {
			return fmt.Errorf("%s: legacy %s artifact; regenerate all producers and consumers with Goxide", pkg.Path(), marker)
		}
	}
	return nil
}
