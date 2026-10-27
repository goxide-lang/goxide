package playground

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/types"
	"io"
	"regexp"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// MacroBundle is built alongside the browser compiler. Editable playground
// input cannot add libraries, change their roots, or select executable bytes.
type MacroBundle struct {
	PackagePath    string                   `json:"package,omitempty"`
	Features       config.Features          `json:"features"`
	Artifacts      []MacroArtifact          `json:"artifacts,omitempty"`
	Roots          paths.Roots              `json:"roots,omitempty"`
	RuntimeRoots   []analysis.RuntimeRoot   `json:"runtime_roots,omitempty"`
	SourcePackages []analysis.SourcePackage `json:"source_packages,omitempty"`
}

type MacroArtifact struct {
	SHA256  string                       `json:"sha256"`
	Library analysis.ProcLibrarySnapshot `json:"library"`
}

func ParseMacroBundle(data []byte) (MacroBundle, error) {
	var bundle MacroBundle
	if len(data) > 8<<20 {
		return bundle, fmt.Errorf("browser macro snapshot exceeds 8 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&bundle); err != nil {
		return bundle, fmt.Errorf("browser macro snapshot: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return bundle, fmt.Errorf("browser macro snapshot has trailing data")
	}
	if len(bundle.Artifacts) > 64 {
		return bundle, fmt.Errorf("browser macro snapshot exceeds 64 libraries")
	}
	for _, source := range bundle.SourcePackages {
		if source.EnvironmentDependent {
			return bundle, fmt.Errorf("browser source snapshot %s depends on environment variables", source.PackagePath)
		}
	}
	seen := map[string]bool{}
	digestPattern := regexp.MustCompile(`^[a-f0-9]{64}$`)
	for _, artifact := range bundle.Artifacts {
		if !digestPattern.MatchString(artifact.SHA256) {
			return bundle, fmt.Errorf("browser macro artifact has an invalid content digest")
		}
		name := artifact.Library.PackagePath
		if seen[name] {
			return bundle, fmt.Errorf("duplicate browser macro library %s", name)
		}
		seen[name] = true
	}
	return bundle, nil
}

// Libraries creates request-local bindings from the fixed build snapshot. The
// runner receives only a digest already present in this bundle.
func (bundle MacroBundle) Libraries(importer types.Importer, run func(string, pm.Request) ([]byte, error)) ([]*analysis.ProcLibrary, error) {
	var libraries []*analysis.ProcLibrary
	for _, artifact := range bundle.Artifacts {
		library, err := analysis.LoadProcLibrarySnapshot(artifact.Library, importer, func(request pm.Request) ([]byte, error) {
			return run(artifact.SHA256, request)
		})
		if err != nil {
			return nil, err
		}
		libraries = append(libraries, library)
	}
	return libraries, nil
}
