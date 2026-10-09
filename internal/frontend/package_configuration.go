package frontend

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
)

const packageConfigurationFile = "goxide_project_config.go"

// packageConfigurationSources materializes the semantic build context before
// file ordering, macro hygiene and helper naming. Both module and in-memory
// package compilation must see this same compiler-owned source file.
// filename selects only its filesystem placement, not its contents or identity.
func packageConfigurationSources(sources map[string][]byte, roots paths.Roots, configuration, filename string) (map[string][]byte, string, error) {
	if len(roots) > 0 {
		data, err := json.Marshal(roots)
		if err != nil {
			return nil, "", fmt.Errorf("dependency roots: %w", err)
		}
		configuration += fmt.Sprintf(" roots=%x", sha256.Sum256(data))
	}
	if configuration == "" {
		return sources, "", nil
	}
	names := sortedSources(sources)
	if len(names) == 0 {
		return nil, "", fmt.Errorf("package has no source files for its configuration")
	}
	for _, name := range names {
		base := filepath.Base(name)
		if strings.HasSuffix(base, ".gox") {
			base = strings.TrimSuffix(base, ".gox") + ".go"
		}
		if base == packageConfigurationFile {
			return nil, "", fmt.Errorf("reserved project config filename conflicts: %s", name)
		}
	}
	first := names[0]
	parsed, err := parseSourceImports(first, sources[first])
	if err != nil {
		return nil, "", err
	}
	if filename == "" {
		filename = filepath.Join(filepath.Dir(first), packageConfigurationFile)
	}
	prepared := make(map[string][]byte, len(sources)+1)
	for name, source := range sources {
		prepared[name] = source
	}
	prepared[filename] = []byte(fmt.Sprintf("package %s\nconst __goxide_project_configuration = %q\n", parsed.Name.Name, configuration))
	return prepared, filename, nil
}
