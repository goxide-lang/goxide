package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func publicSupportModule(path string) bool {
	return path == "github.com/goxide-lang/std" || path == "github.com/goxide-lang/serde"
}

// writeStandaloneSupportModule selects the public support graph from the
// requested manifest. The compiler's own module and CLI dependencies are never
// dependencies of generated programs. Configured source modules use their own
// graph instead and do not call this standalone-only helper.
func writeStandaloneSupportModule(tool, root, destination string) error {
	inspect := exec.Command(tool, "mod", "edit", "-json")
	inspect.Dir, inspect.Env = root, localEnv()
	raw, err := inspect.Output()
	if err != nil {
		return fmt.Errorf("public support dependency manifest: %w", err)
	}
	type moduleVersion struct{ Path, Version string }
	var declarations struct {
		Go      string
		Require []moduleVersion
		Replace []struct{ Old, New moduleVersion }
		Exclude []moduleVersion
	}
	if err = json.Unmarshal(raw, &declarations); err != nil {
		return err
	}
	selected := map[string]bool{}
	edits := []string{"mod", "edit", "-module=goxide.local/generated", "-go=" + declarations.Go}
	for _, required := range declarations.Require {
		if publicSupportModule(required.Path) {
			selected[required.Path] = true
			edits = append(edits, "-require="+required.Path+"@"+required.Version)
		}
	}
	if len(selected) != 2 {
		return fmt.Errorf("public support dependency manifest must explicitly require pinned github.com/goxide-lang/std and github.com/goxide-lang/serde")
	}
	for _, replace := range declarations.Replace {
		old, replacement := replace.Old.Path, replace.New.Path
		if replace.Old.Version != "" {
			old += "@" + replace.Old.Version
		}
		if replace.New.Version != "" {
			replacement += "@" + replace.New.Version
		} else if !filepath.IsAbs(replacement) {
			replacement = filepath.Join(root, replacement)
		}
		edits = append(edits, "-replace="+old+"="+filepath.ToSlash(replacement))
	}
	for _, exclude := range declarations.Exclude {
		edits = append(edits, "-exclude="+exclude.Path+"@"+exclude.Version)
	}
	if err := os.WriteFile(filepath.Join(destination, "go.mod"), []byte("module goxide.local/generated\n"), 0600); err != nil {
		return err
	}
	editModule := func(args []string) error {
		edit := exec.Command(tool, args...)
		edit.Dir, edit.Env = destination, localEnv()
		if output, err := edit.CombinedOutput(); err != nil {
			return fmt.Errorf("generated public support module: %w: %s", err, output)
		}
		return nil
	}
	if err := editModule(edits); err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(sums) != 0 {
		if err := os.WriteFile(filepath.Join(destination, "go.sum"), sums, 0600); err != nil {
			return err
		}
	}
	// Start from only the public support roots. All provider replacements and
	// exclusions participate while resolving their real closure, including
	// local forks; unrelated CLI requirements never enter this graph.
	type graphModule struct {
		Path, Version string
		Main          bool
		Error         *struct{ Err string }
		Replace       *moduleVersion
	}
	var graph []graphModule
	for {
		list := exec.Command(tool, "list", "-mod=readonly", "-m", "-e", "-json", "all")
		list.Dir, list.Env = destination, localEnv()
		var stderr bytes.Buffer
		list.Stderr = &stderr
		raw, err := list.Output()
		if err != nil {
			return fmt.Errorf("public support dependency graph: %w: %s", err, &stderr)
		}
		graph = nil
		decoder := json.NewDecoder(bytes.NewReader(raw))
		reachable := map[string]bool{}
		for {
			var module graphModule
			if err := decoder.Decode(&module); err == io.EOF {
				break
			} else if err != nil {
				return err
			}
			if module.Main {
				continue
			}
			reachable[module.Path] = true
			graph = append(graph, module)
		}
		// Preserve a provider's higher minimum only when that module belongs
		// to the support closure. MVS may then expose further reachable nodes.
		extra := []string{"mod", "edit"}
		for _, required := range declarations.Require {
			if reachable[required.Path] && !selected[required.Path] {
				selected[required.Path] = true
				extra = append(extra, "-require="+required.Path+"@"+required.Version)
			}
		}
		if len(extra) == 2 {
			// Only the stable selected graph is authoritative. An intermediate
			// version may be unavailable or carry dependencies removed by a
			// provider's higher minimum; -e exposes its path for that override.
			for _, module := range graph {
				if compilerModule(module.Path) || module.Replace != nil && compilerModule(module.Replace.Path) {
					return fmt.Errorf("public support dependencies must not depend on the compiler module: %s", module.Path)
				}
				if module.Error != nil {
					return fmt.Errorf("public support dependency graph: %s: %s", module.Path, module.Error.Err)
				}
			}
			break
		}
		if err := editModule(extra); err != nil {
			return err
		}
	}
	selected = map[string]bool{}
	checksumModules := map[string]bool{}
	edits = []string{"mod", "edit"}
	for _, module := range graph {
		selected[module.Path] = true
		checksumModules[module.Path] = true
		edits = append(edits, "-require="+module.Path+"@"+module.Version)
		if module.Replace != nil && module.Replace.Version != "" {
			checksumModules[module.Replace.Path] = true
		}
	}
	for _, replace := range declarations.Replace {
		if !selected[replace.Old.Path] {
			old := replace.Old.Path
			if replace.Old.Version != "" {
				old += "@" + replace.Old.Version
			}
			edits = append(edits, "-dropreplace="+old)
		}
	}
	for _, exclude := range declarations.Exclude {
		if !selected[exclude.Path] {
			edits = append(edits, "-dropexclude="+exclude.Path+"@"+exclude.Version)
		}
	}
	if err := editModule(edits); err != nil {
		return err
	}
	var selectedSums strings.Builder
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && checksumModules[fields[0]] {
			selectedSums.WriteString(line + "\n")
		}
	}
	return os.WriteFile(filepath.Join(destination, "go.sum"), []byte(selectedSums.String()), 0600)
}

func compilerModule(path string) bool {
	return path == "github.com/hxsf/goxide" || path == "github.com/goxide-lang/goxide"
}
