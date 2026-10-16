package compile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hxsf/goxide/internal/project"
)

// Each generated module gets the same Go-selected graph. Relative replacements
// work both in the temporary tree and after PublishBundle moves its contents.
func prepareModules(graph *project.Graph, generated map[string]string) error {
	for _, node := range graph.Nodes {
		root := generated[node.Module]
		edits := []string{"mod", "edit"}
		for _, replacement := range node.Mod.Replace {
			old := replacement.Old.Path
			if replacement.Old.Version != "" {
				old += "@" + replacement.Old.Version
			}
			edits = append(edits, "-dropreplace="+old)
		}
		for _, excluded := range node.Mod.Exclude {
			edits = append(edits, "-dropexclude="+excluded.Path+"@"+excluded.Version)
		}
		for _, excluded := range graph.RootMod.Exclude {
			edits = append(edits, "-exclude="+excluded.Path+"@"+excluded.Version)
		}
		for _, module := range project.Keys(graph.Plan) {
			if module == node.Module {
				continue
			}
			selection := graph.Plan[module]
			edits = append(edits, "-require="+module+"@"+selection.Version)
			if generated[module] == "" && selection.Replace != nil {
				replacement := selection.Replace.Path
				if selection.Replace.Version != "" {
					replacement += "@" + selection.Replace.Version
				}
				edits = append(edits, "-replace="+module+"="+replacement)
			}
		}
		for _, module := range project.Keys(generated) {
			if module == node.Module {
				continue
			}
			relative, err := filepath.Rel(root, generated[module])
			if err != nil {
				return err
			}
			edits = append(edits, "-replace="+module+"="+filepath.ToSlash(relative))
		}
		if len(edits) > 2 {
			if _, err := goCommand(graph, root, edits...); err != nil {
				return err
			}
		}
	}
	for _, node := range graph.Nodes {
		root := generated[node.Module]
		key := project.Digest(struct {
			Module   string
			Manifest project.Manifest
			Mod      string
			Sources  map[string]project.Dependency
			Plan     string
		}{node.Module, node.Manifest, string(node.ModBytes), node.Sources, graph.PlanID})
		sumPath := filepath.Join(root, "go.sum")
		if _, err := os.Stat(sumPath); err == nil {
			if err := os.Chmod(sumPath, 0o644); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		if sums, exists := graph.Lock.Sums[key]; exists {
			if sums == "" {
				if err := os.Remove(sumPath); err != nil && !os.IsNotExist(err) {
					return err
				}
			} else if err := os.WriteFile(sumPath, []byte(sums), 0o644); err != nil {
				return err
			}
		} else if graph.Locked {
			return fmt.Errorf("--locked: module configuration absent from lock: %s", node.Module)
		}
		if _, err := goCommand(graph, root, "mod", "download", "all"); err != nil {
			return err
		}
		data, err := goCommand(graph, root, "list", "-mod=readonly", "-m", "-json", "all")
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var selected struct {
				Path, Version string
				Main          bool
				Replace       *project.Selection
			}
			err := decoder.Decode(&selected)
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if selected.Main {
				continue
			}
			wanted, exists := graph.Plan[selected.Path]
			if selected.Path != graph.Root.Module && (!exists || selected.Version != wanted.Version) {
				return fmt.Errorf("generated Go build list disagrees with stable plan for %s", selected.Path)
			}
			replacement, version := "", ""
			if wanted.Replace != nil {
				replacement, version = wanted.Replace.Path, wanted.Replace.Version
			}
			if output := generated[selected.Path]; output != "" {
				relative, err := filepath.Rel(root, output)
				if err != nil {
					return err
				}
				replacement, version = filepath.ToSlash(relative), ""
			}
			if replacement == "" {
				if selected.Replace != nil {
					return fmt.Errorf("unexpected replacement outside stable plan: %s", selected.Path)
				}
			} else if selected.Replace == nil || selected.Replace.Path != replacement || selected.Replace.Version != version {
				return fmt.Errorf("generated replacement disagrees with stable plan: %s", selected.Path)
			}
		}
		sums, err := os.ReadFile(sumPath)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if graph.Locked && graph.Lock.Sums[key] != string(sums) {
			return fmt.Errorf("--locked: checksums would change for %s", node.Module)
		}
		graph.Lock.Sums[key] = string(sums)
	}
	return nil
}

func goCommand(graph *project.Graph, root string, args ...string) ([]byte, error) {
	command := exec.Command(graph.Tool, args...)
	command.Dir = root
	command.Env = project.GoEnv(graph.Offline, graph.Environment)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("Go %s in %s: %w: %s", args[0], root, err, stderr.String())
	}
	return output, nil
}
