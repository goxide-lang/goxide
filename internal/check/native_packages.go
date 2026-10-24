package check

import (
	"fmt"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
)

// Native packages whose dependency closure contains no selected Goxide source
// already have complete Go export data. Reuse that graph so a native value has
// the same identity whether reached directly or through another Go package.
// Mixed packages and native relays over Goxide declarations remain source
// checked together; their export data may be absent or describe older output.
func (c *checker) prepareNativePackages(requireImports bool) []*resolve.Package {
	c.out.NativePackages = map[string]*types.Package{}
	selected := map[string]*resolve.Package{}
	candidates := map[string]bool{}
	for _, pkg := range c.out.Resolve.Packages {
		if pkg.IsExternalTest() {
			continue
		}
		name := filepath.ToSlash(pkg.Dir)
		selected[name] = pkg
		native := true
		for _, file := range pkg.Files {
			native = native && file.AST.NativeGo
		}
		candidates[name] = native
	}
	for changed := true; changed; {
		changed = false
		for name, pkg := range selected {
			if !candidates[name] {
				continue
			}
			for _, file := range pkg.Files {
				for _, use := range file.Uses {
					target := strings.Join(use.Target, "/")
					if selected[target] != nil && !candidates[target] {
						candidates[name] = false
						changed = true
					}
				}
			}
		}
	}
	var source []*resolve.Package
	for _, pkg := range c.out.Resolve.Packages {
		name := filepath.ToSlash(pkg.Dir)
		if pkg.IsExternalTest() || !candidates[name] {
			source = append(source, pkg)
			continue
		}
		imported, err := c.importer.Import(name)
		if err != nil {
			if requireImports {
				c.file = pkg.Files[0]
				c.diag(c.file.AST.Package, "native-package", fmt.Sprintf("load selected native Go package %s: %v", name, err))
			} else {
				source = append(source, pkg)
			}
			continue
		}
		c.packages[pkg] = imported
		c.imported[name], c.out.NativePackages[name] = imported, imported
		for _, symbol := range pkg.Scope.Bindings {
			if object := imported.Scope().Lookup(symbol.GoName); object != nil {
				c.out.Symbols[symbol] = object.Type()
				if value, ok := object.(*types.Const); ok {
					c.constants[symbol] = value.Val()
				}
			}
		}
	}
	return source
}
