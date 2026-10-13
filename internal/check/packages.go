package check

import (
	"go/importer"
	"path"
	"path/filepath"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
)

// Resolve's inputs carry directories; explicit use paths supply the module prefix.
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
	prefixes := map[string]bool{}
	standard := importer.Default()
	for _, p := range res.Packages {
		rel, err := filepath.Rel(root, p.Dir)
		if err != nil || rel == "." {
			continue
		}
		rel = filepath.ToSlash(rel)
		for _, f := range res.Files {
			for _, use := range f.Uses {
				for cut := len(use.Target); cut > 0; cut-- {
					imported := strings.Join(use.Target[:cut], "/")
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
