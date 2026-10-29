package check

import (
	"go/types"
	"path/filepath"
	"strings"
)

// CanonicalPackagePath uses module identity when available. Source-only inputs
// have no module path; their package paths are relative to the shared source
// root so a checkout's absolute location does not change generated identities.
func (i *Info) CanonicalPackagePath(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	path := pkg.Path()
	if i != nil && i.PackagePaths[path] != "" {
		return i.PackagePaths[path]
	}
	if !filepath.IsAbs(path) {
		return path
	}
	root := path
	if i != nil && i.Resolve != nil {
		for _, source := range i.Resolve.Packages {
			for {
				relative, err := filepath.Rel(root, source.Dir)
				if err != nil || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
					break
				}
				parent := filepath.Dir(root)
				if parent == root {
					break
				}
				root = parent
			}
		}
	}
	relative, _ := filepath.Rel(root, path)
	return filepath.ToSlash(relative) + "/" + pkg.Name()
}
