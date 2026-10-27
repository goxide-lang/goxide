package compile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// BindProcMacros binds execution to this snapshot's own cancellation and budget.
// It replaces only callback-bearing library values, retaining immutable source
// data. The prepared project's libraries remain usable by other snapshots.
func (s *Snapshot) BindProcMacros(run func([]byte, pm.Request) ([]byte, error)) {
	bound := map[*analysis.ProcLibrary]*analysis.ProcLibrary{}
	programs := map[*analysis.ProcLibrary][]byte{}
	for index := range s.Input.Modules {
		libraries := append([]*analysis.ProcLibrary(nil), s.Input.Modules[index].ProcLibraries...)
		for j, library := range libraries {
			copy := bound[library]
			if copy == nil {
				value := *library
				copy = &value
				wasm := s.macros[library]
				copy.Run = func(request pm.Request) ([]byte, error) {
					if err := s.Context.Err(); err != nil {
						return nil, err
					}
					if library.Unavailable != nil {
						return nil, library.Unavailable.Err
					}
					if run == nil || wasm == nil {
						return nil, fmt.Errorf("snapshot macro execution is unavailable")
					}
					output, err := run(bytes.Clone(wasm), request)
					if canceled := s.Context.Err(); canceled != nil {
						return nil, canceled
					}
					return output, err
				}
				bound[library] = copy
				programs[copy] = wasm
			}
			libraries[j] = copy
		}
		s.Input.Modules[index].ProcLibraries = libraries
	}
	s.macros = programs
}

func (p *Project) OwnsSource(name string) bool {
	if within(p.layoutRoot, name) {
		return false
	}
	for _, graph := range p.graphs {
		if _, destination := p.stages[graph].sourceDestination(name); destination != "" {
			return true
		}
	}
	return false
}

// MacroSource identifies the complete selected macro dependency closure. Such
// edits require new programs before the consumer's next analysis.
func (p *Project) MacroSource(name string) bool {
	for _, graph := range p.graphs {
		if graph.Role != "proc-macro" || p.stages[graph].library == nil {
			continue
		}
		if _, destination := p.stages[graph].sourceDestination(name); destination != "" {
			return true
		}
	}
	return false
}

func (stage *stagedRole) applyOverlays(overlays map[string][]byte) error {
	if len(overlays) == 0 {
		return nil
	}
	for name, data := range overlays {
		if !filepath.IsAbs(name) {
			return fmt.Errorf("overlay filename must be absolute: %s", name)
		}
		name = filepath.Clean(name)
		if within(filepath.Dir(stage.dir), name) {
			continue
		}
		extension := filepath.Ext(name)
		if extension != ".gox" && extension != ".go" {
			continue
		}
		_, destination := stage.sourceDestination(name)
		if destination == "" {
			continue
		}
		if data == nil {
			if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
				return err
			}
			delete(stage.originals, destination)
		} else {
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(destination, data, 0644); err != nil {
				return err
			}
			stage.originals[destination] = name
		}
	}
	return nil
}

// sourceDestination uses the same target paths that staged the fixed inventory,
// including file-target relocation and exclusions for other executable targets.
func (stage *stagedRole) sourceDestination(name string) (string, string) {
	if !filepath.IsAbs(name) || within(filepath.Dir(stage.dir), name) {
		return "", ""
	}
	name = filepath.Clean(name)
	var owner *project.Node
	for _, node := range stage.graph.Nodes {
		if within(node.Root, name) && (owner == nil || len(node.Root) > len(owner.Root)) {
			owner = node
		}
	}
	if owner == nil || !sourcePathSelected(owner.Root, name) {
		return "", ""
	}
	relative, err := filepath.Rel(owner.Root, name)
	if err != nil {
		return "", ""
	}
	selection := stage.selections[owner.Module]
	if selection == nil {
		return "", ""
	}
	relative, selected := selection.destination(relative)
	if !selected {
		return "", ""
	}
	return owner.Module, filepath.Join(stage.generated[owner.Module], relative)
}
