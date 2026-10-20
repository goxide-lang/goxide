package check

import (
	"fmt"
	"go/importer"
	"path"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (c *checker) sourceModules() {
	packages := make(map[string]*resolve.Package)
	for _, pkg := range c.out.Resolve.Packages {
		if !pkg.IsExternalTest() {
			packages[filepath.ToSlash(pkg.Dir)] = pkg
		}
	}
	for _, pkg := range c.out.Resolve.Packages {
		for _, file := range pkg.Files {
			ast.Walk(file.AST, func(node ast.Node) bool {
				decl, ok := node.(*ast.ModDecl)
				if !ok || decl.Inline {
					return true
				}
				if symbol := file.Definitions[decl.ID()]; symbol != nil {
					if child := packages[strings.Join(symbol.Target, "/")]; child != nil {
						c.out.Modules[decl] = child
					}
				}
				return false
			})
		}
	}
}

func (c *checker) checkImportedVisibility() {
	for _, pkg := range c.out.Resolve.Packages {
		for _, file := range pkg.Files {
			c.file = file
			for _, use := range file.Uses {
				symbol := c.localSymbol(use.Target)
				// A public namespace route may share a canonical path with a
				// private item. That item does not replace the selected namespace.
				if use.Resolved != nil && use.Resolved.Kind == resolve.Module && use.Resolved.Visibility == resolve.Public {
					continue
				}
				if symbol == nil || symbol.Visibility != resolve.Private || symbol.Decl.File == "" || c.sameSourcePackage(symbol.Decl.File, file.Name) {
					continue
				}
				for _, item := range file.AST.Items {
					decl, ok := item.(*ast.UseDecl)
					if ok && decl.ID() == use.Decl.ID {
						c.diag(decl, SourceVisibility, fmt.Sprintf("private source item %q is not visible from this package", symbol.Name.Name))
					}
				}
			}
		}
	}
}

func (c *checker) sameSourcePackage(first, second string) bool {
	a, b := c.out.Resolve.Files[first], c.out.Resolve.Files[second]
	return a != nil && b != nil && a.Scope.Parent == b.Scope.Parent
}

// Runtime references carry canonical package targets. Namespace bindings are
// routing identities and must not be used as generated Go import prefixes.
func sourcePackagePaths(res *resolve.Result) map[string]string {
	out := make(map[string]string)
	if len(res.Packages) == 0 {
		return out
	}
	root := res.Packages[0].Dir
	for _, p := range res.Packages[1:] {
		for {
			rel, err := filepath.Rel(root, p.Dir)
			if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			root = filepath.Dir(root)
		}
	}
	targets := map[string][]string{}
	for _, file := range res.Files {
		if filepath.IsAbs(root) {
			// Direct source-only callers use filesystem keys and may need an
			// imported package's identity even for an unused public reexport.
			for _, use := range file.Uses {
				targets[strings.Join(use.Target, "/")] = use.Target
			}
			continue
		}
		for _, reference := range file.References {
			target := reference.Path
			if symbol := reference.Symbol; symbol != nil {
				if symbol.Kind == resolve.Module || symbol.Kind == resolve.Import || len(symbol.Target) == 0 {
					continue
				}
				target = symbol.Target
			}
			if len(target) > 1 {
				targets[strings.Join(target, "/")] = target
			}
		}
	}
	prefixes := map[string]bool{}
	standard := importer.Default()
	for _, p := range res.Packages {
		rel, err := filepath.Rel(root, p.Dir)
		if err != nil || rel == "." {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, target := range targets {
			for cut := len(target); cut > 0; cut-- {
				imported := strings.Join(target[:cut], "/")
				if imported != rel && !strings.HasSuffix(imported, "/"+rel) {
					continue
				}
				if _, err := standard.Import(imported); err == nil {
					continue
				}
				prefixes[strings.TrimSuffix(strings.TrimSuffix(imported, rel), "/")] = true
			}
		}
	}
	if len(prefixes) != 1 {
		return out
	}
	for prefix := range prefixes {
		for _, p := range res.Packages {
			rel, err := filepath.Rel(root, p.Dir)
			if err == nil {
				out[filepath.ToSlash(p.Dir)] = path.Join(prefix, filepath.ToSlash(rel))
			}
		}
	}
	return out
}
