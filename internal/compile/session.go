package compile

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
)

// Project owns prepared dependency views and macro resources. It does not
// retain publication leases or update the dependency lock during preparation.
type Project struct {
	ctx         context.Context
	cancel      context.CancelFunc
	plans       *project.BuildPlans
	graphs      []*project.Graph
	stages      map[*project.Graph]*stagedRole
	options     Options
	layoutRoot  string
	beforeLock  []byte
	directories []string
	inputMu     sync.Mutex
	inputLoaded bool
	input       analysis.Input
	inputErr    error
	sourceNames map[string]string
	sourceKeys  map[string]string

	mu       sync.Mutex
	closed   bool
	work     sync.WaitGroup
	close    sync.Once
	closeErr error
}

// Snapshot retains its project's resources until Close. Input is an immutable
// source selection; caller-specific analysis options may be set on its value
// before calling analysis.Analyze. Overlays use original absolute filenames.
type Snapshot struct {
	Input       analysis.Input
	SourceNames map[string]string // Canonical source key to original filename.
	Context     context.Context
	release     func()
	macros      map[*analysis.ProcLibrary][]byte
}

func (s *Snapshot) Close() { s.release() }

func (p *Project) loadInput(ctx context.Context) {
	stage := p.stages[p.plans.Target]
	if stage.input != nil {
		p.input = *stage.input
	} else {
		p.input, p.inputErr = analysis.LoadInput(ctx, analysis.Input{Modules: stage.modules})
	}
	if p.inputErr != nil {
		return
	}
	p.sourceNames, p.sourceKeys = map[string]string{}, map[string]string{}
	for key, disk := range p.input.DiskNames {
		name := stage.originals[disk]
		if name == "" {
			name = disk
		}
		p.sourceNames[key], p.sourceKeys[name] = name, key
	}
}

// Snapshot fixes the selected file set without re-reading unchanged sources.
// A nil overlay removes a file; an empty non-nil slice is an empty source file.
func (p *Project) Snapshot(ctx context.Context, overlays map[string][]byte) (*Snapshot, error) {
	ctx, release, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	p.inputMu.Lock()
	if !p.inputLoaded {
		p.loadInput(ctx)
		p.inputLoaded = p.inputErr == nil
	}
	input, inputErr := p.input, p.inputErr
	names := maps.Clone(p.sourceNames)
	p.inputMu.Unlock()
	if inputErr != nil {
		release()
		return nil, inputErr
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	input.Modules = append([]analysis.Module(nil), input.Modules...)
	reads := new(config.Reads)
	for i := range input.Modules {
		input.Modules[i].Context = ctx
		input.Modules[i].Config.Reads = reads
		if producer := input.Modules[i].Producer; producer != nil {
			copy := *producer
			input.Modules[i].Producer = &copy
		}
	}
	input.Overlays = make(map[string][]byte, len(overlays))
	for name, data := range overlays {
		key, err := p.sourceKey(name)
		if err != nil {
			if p.MacroSource(name) {
				continue
			}
			release()
			return nil, err
		}
		if key == "" {
			continue
		}
		input.Overlays[key] = bytes.Clone(data)
		names[key] = name
	}
	macros := map[*analysis.ProcLibrary][]byte{}
	for _, stage := range p.stages {
		if stage.library != nil {
			macros[stage.library] = stage.wasm
			packagePath, err := libraryPackage(stage.graph.Root)
			if err != nil {
				release()
				return nil, err
			}
			for staged, original := range stage.originals {
				relative, err := filepath.Rel(stage.generated[stage.graph.Root.Module], staged)
				if err != nil {
					continue
				}
				key := path.Join(stage.graph.Root.Module, filepath.ToSlash(relative))
				if path.Dir(key) == packagePath && filepath.Ext(staged) == ".gox" {
					names[path.Join(stage.library.PackagePath, filepath.Base(staged))] = original
				}
			}
		}
	}
	return &Snapshot{Input: input, SourceNames: names, Context: ctx, release: release, macros: macros}, nil
}

func (p *Project) sourceKey(name string) (string, error) {
	if !filepath.IsAbs(name) {
		return "", fmt.Errorf("overlay filename must be absolute: %s", name)
	}
	name = filepath.Clean(name)
	if extension := filepath.Ext(name); extension != ".gox" && extension != ".go" {
		return "", nil
	}
	if key := p.sourceKeys[name]; key != "" {
		return key, nil
	}
	stage := p.stages[p.plans.Target]
	module, destination := stage.sourceDestination(name)
	if destination == "" {
		return "", nil
	}
	relative, err := filepath.Rel(stage.generated[module], destination)
	if err != nil {
		return "", err
	}
	return path.Join(module, filepath.ToSlash(relative)), nil
}

// GoRoot is the private Go module view used by this prepared project.
func (p *Project) GoRoot() string {
	return p.stages[p.plans.Target].generated[p.plans.Target.Root.Module]
}

func (p *Project) begin(ctx context.Context) (context.Context, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, nil, fmt.Errorf("prepared project is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	p.work.Add(1)
	task, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	var once sync.Once
	return task, func() {
		once.Do(func() {
			stop()
			cancel()
			p.work.Done()
		})
	}, nil
}

// Close cancels work and waits for outstanding snapshot owners before deleting
// private files. It is safe to call repeatedly.
func (p *Project) Close() error {
	p.close.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cancel()
		p.mu.Unlock()
		p.work.Wait()
		for _, directory := range p.directories {
			if err := os.RemoveAll(directory); err != nil && p.closeErr == nil {
				p.closeErr = err
			}
		}
	})
	return p.closeErr
}

func (p *Project) buildAndPublish(ctx context.Context) (*Result, error) {
	if p.options.Editor {
		return nil, fmt.Errorf("editor preparation cannot publish build artifacts")
	}
	ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	stage := p.stages[p.plans.Target]
	if stage.graph.Role != "proc-macro" {
		if p.options.OnPreparedSources != nil {
			stage.modules[0].OnSourcePackages = func(sources []analysis.SourcePackage) error { stage.sources = sources; return nil }
		}
		modules := append([]analysis.Module(nil), stage.modules...)
		for i := range modules {
			modules[i].Context = ctx
		}
		if err := lower.CompileModules(modules); err != nil {
			return nil, err
		}
		if err := p.preparedSources(stage); err != nil {
			return nil, err
		}
		arguments := []string{"list", "-mod=readonly", "-tags=" + strings.Join(p.options.Tags, ","), "-f", "{{.Name}}"}
		if p.options.Race {
			arguments = append(arguments, "-race")
		}
		name, err := goCommand(ctx, stage.graph, stage.generated[stage.graph.Root.Module], append(arguments, stage.entry)...)
		if err != nil {
			return nil, err
		}
		if main := strings.TrimSpace(string(name)) == "main"; stage.kind == "lib" && main || stage.kind != "lib" && !main {
			if stage.kind == "lib" {
				return nil, fmt.Errorf("lib target %s must be an importable package, not package main", stage.target.Name)
			}
			return nil, fmt.Errorf("%s target %s must be package main", stage.kind, stage.target.Name)
		}
	}
	return p.publish(ctx)
}

func (p *Project) preparedSources(stage *stagedRole) error {
	if p.options.OnPreparedSources == nil {
		return nil
	}
	prepared := PreparedSources{SourcePackages: stage.sources, Roots: stage.modules[0].Roots, RuntimeRoots: stage.modules[0].RuntimeRoots}
	for _, graph := range p.graphs {
		if producer := p.stages[graph]; producer.library != nil {
			prepared.Macros = append(prepared.Macros, ProcArtifact{Library: producer.library, WASM: append([]byte(nil), producer.wasm...)})
		}
	}
	return p.options.OnPreparedSources(prepared)
}

func (p *Project) publish(ctx context.Context) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := p.plans.Target.Root
	release, err := project.LockProject(root.Root)
	if err != nil {
		return nil, err
	}
	defer release()
	current, err := os.ReadFile(filepath.Join(root.Root, "goxide.lock"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if !bytes.Equal(current, p.beforeLock) {
		return nil, fmt.Errorf("dependency lock changed during preparation; retry with the current project")
	}
	layout, err := project.OpenLayout(root.Root, root.Module, p.layoutRoot)
	if err != nil {
		return nil, err
	}
	defer layout.Close()
	for _, graph := range p.graphs {
		stage := p.stages[graph]
		tags, race := p.options.Tags, p.options.Race
		if graph.Role == "proc-macro" {
			tags, race = nil, false
		}
		var spec project.UnitSpec
		if stage.spec != nil {
			spec = *stage.spec
		} else {
			var err error
			spec, err = unitSpec(graph, stage.target, stage.kind, stage.config, stage.features, stage.sugar, tags, race, stage.modules[0].StableGo)
			if err != nil {
				return nil, err
			}
			if len(p.plans.Macros) > 0 || graph.Role == "proc-macro" {
				spec.Config = project.Digest([]string{spec.Config, stage.sdk, procmacro.ProfileID})
			}
		}
		stage.unit, err = layout.Begin(spec)
		if err != nil {
			return nil, err
		}
	}
	if err := p.plans.Save(); err != nil {
		return nil, err
	}
	for _, graph := range p.graphs {
		stage := p.stages[graph]
		if err := stage.unit.PublishBundle(stage.dir); err != nil {
			return nil, err
		}
	}
	final := p.stages[p.plans.Target]
	final.config.Features = final.features[p.plans.Target.Root.SourceKey]
	final.config.GoSugar = final.modules[0].Config.GoSugar
	result := &Result{Root: filepath.Join(final.unit.Bundle, "module"), Entry: final.entry, Kind: final.kind, Config: final.config}
	if len(final.wasm) != 0 {
		result.ProcMacro = filepath.Join(final.unit.Bundle, "macro.wasm")
	}
	seen := map[string]bool{}
	for _, graph := range p.graphs {
		for _, node := range graph.Nodes {
			if !seen[node.Root] {
				result.SourceRoots = append(result.SourceRoots, node.Root)
				seen[node.Root] = true
			}
		}
	}
	return result, nil
}
