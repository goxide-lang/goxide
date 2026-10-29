package analysis

import (
	"context"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/source"
)

// Module is a writable staging directory prepared by the caller. Path is its
// canonical Go module path; Roots overrides its goxide.roots.toml when non-nil.
// Config supplies the invocation's explicit environment and each module's
// features. The first module supplies the Go import context and build flags.
type Module struct {
	Path, Dir string
	// StableGo emits definition-owned entries usable by separately compiled Go consumers.
	// Only the first module's value is used for the complete source graph.
	StableGo bool
	// RootPackage is the canonical library entry declared by a project manifest.
	// An empty value leaves native Go package imports unrestricted.
	RootPackage   string
	Crates        []Crate
	Targets       []TargetBoundary
	Roots         paths.Roots
	Config        config.Config
	Context       context.Context
	Tags          []string
	Race          bool
	ProcLibraries []*ProcLibrary
	RuntimeRoots  []RuntimeRoot
	// Only the first module's callback is used. It receives the complete source
	// graph's procedural diagnostics in expansion order, including errors, after
	// module discovery finishes; intermediate discovery passes are not reported.
	// Filename is the canonical source key, including the owning package path.
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
	// OnSourcePackages receives the final selected ordinary dependency sources,
	// excluding the first module, before they are replaced by generated Go.
	OnSourcePackages func([]SourcePackage) error
	NativePackage    *NativePackage
	Producer         *ProcLibrary
}

// NativePackage retains one native invocation's importer, source diagnostics,
// and output selection while it joins the complete dependency source graph.
type NativePackage struct {
	Importer                            types.Importer
	DisplayNames                        map[string]string
	Generated, Expanded                 map[string][]byte
	LineDirectives, RejectConfiguration bool
	StableGo                            bool
	RequireRoot                         func(alias string, namespace paths.Namespace) error
}

// Crate describes an authored target without changing its Go package identity.
// SourceDir retains the canonical source package path before file projection.
type Crate struct {
	Name, Root, SourceDir string
	Library               bool
	Unavailable           string
}

// TargetBoundary records authored entries, including targets not being built.
type TargetBoundary struct {
	Path string
	File bool
}

func moduleSources(root string) ([]string, error) {
	return moduleFiles(root, ".gox", false)
}

func moduleFiles(root, extension string, managed bool) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name == root {
				return nil
			}
			base := entry.Name()
			if base == "testdata" || base == "vendor" || strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
				return filepath.SkipDir
			}
			for _, marker := range []string{"go.mod", "goxide.toml"} {
				if _, err := os.Stat(filepath.Join(name, marker)); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		if filepath.Ext(name) == extension && !strings.HasPrefix(entry.Name(), ".") && !strings.HasPrefix(entry.Name(), "_") {
			if extension == ".go" && managed {
				owned, err := ownedGeneratedModuleFile(name)
				if err != nil {
					return err
				}
				if owned {
					return nil
				}
			}
			names = append(names, name)
		}
		return nil
	})
	return names, err
}

// The caller has already selected an author-managed source scope.
func ownedGeneratedModuleFile(name string) (bool, error) {
	if !strings.HasSuffix(name, "_gox.go") {
		return false, nil
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return false, err
	}
	return source.IsGenerated(name, data), nil
}

func ModuleSources(root string) ([]string, error) { return moduleSources(root) }
