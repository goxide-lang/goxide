package compile

import (
	"bytes"
	"context"
	"fmt"
	"go/types"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"github.com/goxide-lang/goxide/internal/typecheck"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

// NativeOptions selects the same private native module view for CLI and editors.
// Sources, when provided, is an already selected package. Otherwise a file
// selects only itself, and a directory selects the native module source tree.
type NativeOptions struct {
	// Dependencies and RequireRoot reuse a CLI's existing lazy-root selection.
	Dependencies        *NativeDependencies
	Importer            func() types.Importer
	RequireRoot         func(string, paths.Namespace) error
	Generated, Expanded map[string][]byte
	Options             Options
	Module              *NativeModule
	Sources             map[string][]byte
	RootsFile           string
	Automatic           bool
	DisplayNames        map[string]string
	RejectConfiguration bool
	LineDirectives      bool
	// Private stages directory inputs before callers materialize executable Go.
	Private bool
}

// NativeProject owns the private Go configuration and prepared source dependencies.
// Activate is called only after releasing the snapshot that returned a root demand.
type NativeProject struct {
	owner              *NativeModule
	ownModule          bool
	authoredModule     bool
	staged             bool
	options            NativeOptions
	plan               *NativeDependencies
	roots              paths.Roots
	requireRoot        func(string, paths.Namespace) error
	project            *Project
	directory          string
	originals          map[string]string
	sourceFiles        map[string]bool
	modBytes, sumBytes []byte
	hadSum             bool
	close              sync.Once
	closeErr           error
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.Mutex
	closed             bool
	work               sync.WaitGroup
	input              *analysis.Input
	helper             string
}

func PrepareNative(ctx context.Context, input string, options NativeOptions) (_ *NativeProject, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	input, err = filepath.Abs(input)
	if err != nil {
		return nil, err
	}
	canonical, resolveErr := filepath.EvalSymlinks(input)
	if resolveErr != nil {
		if _, selected := options.Sources[input]; !selected || options.Module == nil || !os.IsNotExist(resolveErr) {
			return nil, resolveErr
		}
		parent, parentErr := filepath.EvalSymlinks(filepath.Dir(input))
		if parentErr != nil {
			return nil, parentErr
		}
		canonical = filepath.Join(parent, filepath.Base(input))
	}
	input = canonical
	info, statErr := os.Stat(input)
	if statErr != nil && (!os.IsNotExist(statErr) || options.Module == nil) {
		return nil, statErr
	}
	isDirectory := info != nil && info.IsDir()
	if options.Sources == nil && !isDirectory {
		data, err := os.ReadFile(input)
		if err != nil {
			return nil, err
		}
		options.Sources = map[string][]byte{input: data}
	}
	sourceDirectory := filepath.Dir(input)
	if isDirectory {
		sourceDirectory = input
	}
	cfg := options.Options.Config
	if cfg.Environment == nil {
		cfg.Environment, err = config.PrepareContext(ctx, options.Options.GoTool, sourceDirectory, options.Options.Target, false, os.Environ())
		if err != nil {
			return nil, err
		}
	}
	codegen, err := project.ModuleCodegen(sourceDirectory)
	if err != nil {
		return nil, err
	}
	if cfg.GoSugar == nil {
		cfg.GoSugar = codegen.GoSugar
	}
	if options.Options.GoSugar != nil {
		cfg.GoSugar = options.Options.GoSugar
	}
	if options.Options.NativeFeatures != nil {
		cfg.Features = *options.Options.NativeFeatures
	} else if options.Options.Features != nil {
		cfg.Features.Enabled = map[string]bool{}
		for _, feature := range options.Options.Features {
			cfg.Features.Enabled[feature] = true
		}
	}
	options.Options.Config = cfg
	owner := options.Module
	own := owner == nil
	if owner == nil {
		owner, err = OpenNativeModule(ctx, input, cfg)
		if err != nil {
			return nil, err
		}
	}
	moduleRoot := ""
	if owner != nil {
		moduleRoot = owner.Root
	}
	if owner == nil {
		if isDirectory {
			return nil, fmt.Errorf("directory language service requires a Go module or goxide.toml")
		}
		temporary, e := os.MkdirTemp("", "goxide-native-")
		if e != nil {
			return nil, e
		}
		owner = &NativeModule{Input: input, Root: sourceDirectory, PackagePath: "goxide/standalone", Temporary: temporary, ModFile: filepath.Join(temporary, "consumer.mod"), Config: cfg, AuthoredMod: []byte("module goxide/standalone\n\ngo 1.27\n")}
		if e := os.WriteFile(owner.ModFile, owner.AuthoredMod, 0600); e != nil {
			owner.Close()
			return nil, e
		}
	}
	owner.Config = cfg
	options.Options.SourceOverlays = cloneSourceBytes(options.Options.SourceOverlays)
	options.Sources = cloneSourceBytes(options.Sources)
	prepared := &NativeProject{owner: owner, ownModule: own, options: options, authoredModule: moduleRoot != "" && len(owner.AuthoredMod) != 0}
	prepared.ctx, prepared.cancel = context.WithCancel(context.Background())
	defer func() {
		if err != nil {
			prepared.Close()
		}
	}()
	roots := options.Options.Roots
	if roots == nil {
		roots, err = SourceRoots(moduleRoot, sourceDirectory, options.RootsFile)
		if err != nil {
			return nil, err
		}
	}
	prepared.plan = options.Dependencies
	if prepared.plan == nil {
		prepared.plan, err = NativeSourceDependencies(ctx, owner.Root, owner.ModFile, roots, cfg, options.Automatic)
	}
	if err != nil {
		return nil, err
	}
	prepared.roots = prepared.plan.Roots
	if options.Dependencies != nil {
		prepared.roots = roots
		prepared.requireRoot = options.RequireRoot
	} else if options.Automatic {
		importer := typecheck.NewImporter(filepath.Join(sourceDirectory, "input.gox"), typecheck.ImporterOptions{Config: cfg, Tags: options.Options.Tags, ModFile: owner.ModFile})
		prepared.roots, prepared.requireRoot, err = prepared.plan.GenerationRoots(owner.Root, cfg, importer, nil)
		if err != nil {
			return nil, err
		}
	}
	prepared.modBytes, err = os.ReadFile(owner.ModFile)
	if err != nil {
		return nil, err
	}
	prepared.sumBytes, err = os.ReadFile(strings.TrimSuffix(owner.ModFile, ".mod") + ".sum")
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	prepared.hadSum = err == nil
	err = nil
	prepared.directory, err = os.MkdirTemp(owner.Temporary, "source-")
	if err != nil {
		return nil, err
	}
	prepared.directory = filepath.Join(prepared.directory, "module")
	if err = os.MkdirAll(prepared.directory, 0700); err != nil {
		return nil, err
	}
	prepared.originals = map[string]string{}
	prepared.sourceFiles = map[string]bool{}
	if len(prepared.plan.Manifest.Dependencies) != 0 || !prepared.authoredModule || options.Sources != nil || options.Private {
		if err = prepared.stageInput(); err != nil {
			return nil, err
		}
	}
	if err = prepared.prepare(ctx); err != nil {
		return nil, err
	}
	return prepared, nil
}

func (p *NativeProject) writeSource(name string, data []byte) error {
	if !filepath.IsAbs(name) || !within(p.owner.Root, name) {
		return fmt.Errorf("native source is outside its owning module: %s", name)
	}
	relative, err := filepath.Rel(p.owner.Root, name)
	if err != nil {
		return err
	}
	if extension := filepath.Ext(name); extension != ".gox" && extension != ".go" {
		relative += ".gox"
	}
	target := filepath.Join(p.directory, relative)
	if data == nil {
		delete(p.sourceFiles, relative)
		delete(p.originals, target)
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(target, data, 0600); err != nil {
		return err
	}
	p.originals[target] = name
	p.sourceFiles[relative] = true
	return nil
}

func (p *NativeProject) prepare(ctx context.Context) error {
	manifest := p.plan.Manifest
	if len(p.plan.Manifest.Dependencies) == 0 {
		return p.preparePlain(ctx)
	}
	// The native view owns these projected files. A source-dependency view
	// replaces them with goxide.toml; remove them before staging can copy sums.
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := os.Remove(filepath.Join(p.directory, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if !p.staged {
		if err := p.stageInput(); err != nil {
			return err
		}
	}
	if p.helper == "" {
		var err error
		p.helper, err = os.MkdirTemp(p.directory, "goxide_native_entry_")
		if err != nil {
			return err
		}
	}
	helper := p.helper
	if err := os.WriteFile(filepath.Join(helper, "main.go"), []byte("package main\nfunc main() {}\n"), 0600); err != nil {
		return err
	}
	p.sourceFiles[filepath.Join(filepath.Base(helper), "main.go")] = true
	manifest.Bins = []project.Target{{Name: "native", Path: filepath.Base(helper)}}
	data, err := project.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(p.directory, project.ManifestName), data, 0600); err != nil {
		return err
	}
	options := p.options.Options
	options.Config = p.owner.Config
	options.Roots = p.roots
	options.GoTool = p.owner.Config.Environment.GoTool
	options.TargetDir = filepath.Join(filepath.Dir(p.directory), "target")
	options.NativeFeatures = &p.owner.Config.Features
	options.NativeGeneration = p.options.Automatic
	options.NativeFiles = project.Keys(p.sourceFiles)
	options.Offline = !p.options.Automatic
	options.SourceOverlays = maps.Clone(p.options.Options.SourceOverlays)
	// Target sources were already overlaid in the authored-to-private copy;
	// only dependency overlays still use their original absolute filenames.
	for name := range options.SourceOverlays {
		if p.targetSource(name) {
			delete(options.SourceOverlays, name)
		}
	}
	options.PrepareNativePackage = func(modules []analysis.Module) error {
		if err := p.owner.UsePreparedModules(ctx, modules[0].Dir, p.plan.Macros); err != nil {
			return err
		}
		displays := p.displayNames()
		importer := typecheck.NewImporter(filepath.Join(p.owner.Root, "input.gox"), typecheck.ImporterOptions{Config: p.owner.Config, Context: p.ctx, Tags: options.Tags, ModFile: p.owner.ModFile})
		if p.options.Importer != nil {
			importer = p.options.Importer()
		}
		modules[0].StableGo = false
		modules[0].NativePackage = &analysis.NativePackage{Importer: importer, DisplayNames: displays, RequireRoot: p.requireRoot, RejectConfiguration: p.options.RejectConfiguration, LineDirectives: p.options.LineDirectives, StableGo: options.StableGo, Generated: p.options.Generated, Expanded: p.options.Expanded}
		return nil
	}
	p.project, err = Prepare(ctx, p.directory, options)
	return err
}

// OwnsTargetSource excludes prepared dependency sources.
func (p *NativeProject) OwnsTargetSource(name string) bool { return p.targetSource(name) }

// NativeRoot identifies a selected native module directory. Explicit files use
// a private Go view even when their imports belong to an authored module.
func (p *NativeProject) NativeRoot() string {
	if p.authoredModule && p.options.Sources == nil {
		return p.owner.Root
	}
	return ""
}

// SingleFile identifies an explicitly selected file and its authored import
// context. Package selections return empty values; standalone files have no module.
func (p *NativeProject) SingleFile() (name, module, packagePath string) {
	if len(p.options.Sources) != 1 {
		return "", "", ""
	}
	if _, selected := p.options.Sources[p.owner.Input]; !selected {
		return "", "", ""
	}
	if !p.authoredModule {
		return p.owner.Input, "", p.owner.PackagePath
	}
	return p.owner.Input, p.plan.Manifest.Module, p.owner.PackagePath
}

func (p *NativeProject) GoModFile() string { return p.owner.ModFile }

func (p *NativeProject) Root() string { return p.owner.Root }
func (p *NativeProject) GoRoot() string {
	if p.project == nil {
		return p.directory
	}
	return p.project.GoRoot()
}
func (p *NativeProject) Config() config.Config { return p.owner.Config }
func (p *NativeProject) OwnsSource(name string) bool {
	return p.targetSource(name) || p.project != nil && p.project.OwnsSource(name)
}
func (p *NativeProject) MacroSource(name string) bool {
	return p.project != nil && p.project.MacroSource(name)
}

func (p *NativeProject) Snapshot(ctx context.Context, overlays map[string][]byte) (*Snapshot, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("prepared native project is closed")
	}
	if err := ctx.Err(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.work.Add(1)
	p.mu.Unlock()
	task, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	var once sync.Once
	release := func() { once.Do(func() { stop(); cancel(); p.work.Done() }) }
	translated := map[string][]byte{}
	for name, data := range overlays {
		if !p.OwnsSource(name) {
			continue
		}
		target := name
		if p.targetSource(name) {
			relative, err := filepath.Rel(p.owner.Root, name)
			if err != nil {
				release()
				return nil, err
			}
			if extension := filepath.Ext(name); extension != ".gox" && extension != ".go" {
				relative += ".gox"
			}
			target = filepath.Join(p.directory, relative)
		}
		translated[target] = data
	}
	var snapshot *Snapshot
	if p.project != nil {
		var err error
		snapshot, err = p.project.Snapshot(task, translated)
		if err != nil {
			release()
			return nil, err
		}
		inner := snapshot.release
		snapshot.release = func() { inner(); release() }
	} else {
		input := *p.input
		input.Modules = append([]analysis.Module(nil), input.Modules...)
		input.Modules[0].Context = task
		input.Modules[0].Config.Reads = new(config.Reads)
		input.Overlays = map[string][]byte{}
		names := maps.Clone(input.DiskNames)
		for name, data := range translated {
			if !within(p.directory, name) {
				release()
				return nil, fmt.Errorf("overlay is outside selected native module: %s", name)
			}
			relative, err := filepath.Rel(p.directory, name)
			if err != nil {
				release()
				return nil, err
			}
			key := path.Join(input.Modules[0].Path, filepath.ToSlash(relative))
			input.Overlays[key] = bytes.Clone(data)
			names[key] = name
		}
		snapshot = &Snapshot{Input: input, SourceNames: names, Context: task, release: release}
	}
	if len(snapshot.Input.Modules) != 0 && snapshot.Input.Modules[0].NativePackage != nil {
		native := *snapshot.Input.Modules[0].NativePackage
		native.Importer = typecheck.NewImporter(filepath.Join(p.owner.Root, "input.gox"), typecheck.ImporterOptions{Config: p.owner.Config, Context: task, Tags: p.options.Options.Tags, ModFile: p.owner.ModFile})
		snapshot.Input.Modules[0].NativePackage = &native
	}
	for key, name := range snapshot.SourceNames {
		if original := p.originals[name]; original != "" {
			snapshot.SourceNames[key] = original
			continue
		}
		if within(p.directory, name) {
			relative, err := filepath.Rel(p.directory, name)
			if err != nil {
				snapshot.Close()
				return nil, err
			}
			snapshot.SourceNames[key] = filepath.Join(p.owner.Root, relative)
		}
	}
	return snapshot, nil
}

func (p *NativeProject) preparePlain(ctx context.Context) error {
	if err := os.WriteFile(filepath.Join(p.directory, "go.mod"), p.modBytes, 0600); err != nil {
		return err
	}
	if p.hadSum {
		if err := os.WriteFile(filepath.Join(p.directory, "go.sum"), p.sumBytes, 0600); err != nil {
			return err
		}
	}
	args := []string{"mod", "edit"}
	for _, replacement := range p.plan.Manifest.Native.Replace {
		if replacement.Path != "" {
			old := replacement.Module
			if replacement.Version != "" {
				old += "@" + replacement.Version
			}
			args = append(args, "-replace="+old+"="+replacement.Path)
		}
	}
	if len(args) > 2 {
		if _, err := nativeModuleCommand(ctx, p.directory, p.owner.Config, args...); err != nil {
			return err
		}
	}
	native := &analysis.NativePackage{RequireRoot: p.requireRoot, DisplayNames: p.displayNames(), RejectConfiguration: p.options.RejectConfiguration}
	directory := p.directory
	if p.authoredModule && p.options.Sources == nil && !p.staged {
		directory = p.owner.Root
	}
	overlays := map[string][]byte{}
	for name, data := range p.options.Options.SourceOverlays {
		if !p.targetSource(name) {
			continue
		}
		relative, err := filepath.Rel(p.owner.Root, name)
		if err != nil {
			return err
		}
		overlays[path.Join(p.plan.Manifest.Module, filepath.ToSlash(relative))] = data
	}
	input, err := analysis.LoadInput(ctx, analysis.Input{Overlays: overlays, Modules: []analysis.Module{{Path: p.plan.Manifest.Module, Dir: directory, Config: p.owner.Config, Roots: p.roots, Context: ctx, Tags: p.options.Options.Tags, NativePackage: native}}})
	if err != nil {
		return err
	}
	p.input = &input
	return nil
}

// Build publishes only within the owner's private temporary layout. The target
// is analyzed once by the same pipeline used by snapshots.
func (p *NativeProject) Build(ctx context.Context) (*Result, error) {
	if p.project == nil {
		return nil, fmt.Errorf("native build requires activated source dependencies")
	}
	result, err := p.project.buildAndPublish(ctx)
	if err != nil {
		return nil, err
	}
	if err := p.owner.UsePreparedModules(ctx, result.Root, p.plan.Macros); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *NativeProject) Activate(ctx context.Context, cause error, run func([]byte, pm.Request) ([]byte, error)) (bool, error) {
	if !p.options.Automatic || !p.plan.Activate(cause) {
		return false, nil
	}
	p.options.Options.RunProcMacro = run
	if p.project != nil {
		if err := p.project.Close(); err != nil {
			return false, err
		}
	}
	p.work.Wait()
	p.project = nil
	p.input = nil
	if err := os.WriteFile(p.owner.ModFile, p.modBytes, 0600); err != nil {
		return false, err
	}
	sumFile := strings.TrimSuffix(p.owner.ModFile, ".mod") + ".sum"
	if p.hadSum {
		if err := os.WriteFile(sumFile, p.sumBytes, 0600); err != nil {
			return false, err
		}
	} else if err := os.Remove(sumFile); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := p.prepare(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (p *NativeProject) Close() error {
	p.close.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.cancel()
		p.mu.Unlock()
		if p.project != nil {
			p.closeErr = p.project.Close()
		}
		p.work.Wait()
		if p.ownModule {
			if err := p.owner.Close(); p.closeErr == nil {
				p.closeErr = err
			}
		} else if p.directory != "" {
			if err := os.RemoveAll(p.directory); p.closeErr == nil {
				p.closeErr = err
			}
		}
	})
	return p.closeErr
}

func (p *NativeProject) targetSource(name string) bool {
	if !filepath.IsAbs(name) || !within(p.owner.Root, name) {
		return false
	}
	if p.options.Sources != nil {
		_, selected := p.options.Sources[name]
		return selected
	}
	return (filepath.Ext(name) == ".gox" || filepath.Ext(name) == ".go") && sourcePathSelected(p.owner.Root, name)
}

func (p *NativeProject) displayNames() map[string]string {
	displays := map[string]string{}
	for name := range p.options.Sources {
		if filepath.Ext(name) == ".go" {
			continue
		}
		relative, err := filepath.Rel(p.owner.Root, name)
		if err != nil {
			continue
		}
		if filepath.Ext(name) != ".gox" {
			relative += ".gox"
		}
		display := name
		if override := p.options.DisplayNames[name]; override != "" {
			display = override
		}
		displays[path.Join(p.plan.Manifest.Module, filepath.ToSlash(relative))] = display
	}
	return displays
}

func (p *NativeProject) stageInput() error {
	if p.options.Sources == nil {
		node := &project.Node{Root: p.owner.Root, Module: p.plan.Manifest.Module, ModBytes: p.modBytes}
		_, files, err := stageSources(node, project.Target{Name: "native", Path: "."}, "lib", p.directory, true, p.originals)
		if err != nil {
			return err
		}
		for _, name := range files {
			p.sourceFiles[name] = true
		}
	} else {
		for name, data := range p.options.Sources {
			if err := p.writeSource(name, data); err != nil {
				return err
			}
		}
	}
	for _, name := range []string{"go.mod", "go.sum", rootconfig.Filename} {
		delete(p.sourceFiles, name)
		if err := os.Remove(filepath.Join(p.directory, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for name, data := range p.options.Options.SourceOverlays {
		if p.targetSource(name) {
			if err := p.writeSource(name, data); err != nil {
				return err
			}
		}
	}
	p.staged = true
	return nil
}

func cloneSourceBytes(input map[string][]byte) map[string][]byte {
	if input == nil {
		return nil
	}
	output := make(map[string][]byte, len(input))
	for key, data := range input {
		output[key] = bytes.Clone(data)
	}
	return output
}
