package compile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

type stagedRole struct {
	graph      *project.Graph
	dir, entry string
	target     project.Target
	kind       string
	config     config.Config
	features   map[string]config.Features
	sugar      map[string]bool
	modules    []analysis.Module
	generated  map[string]string
	originals  map[string]string
	library    *analysis.ProcLibrary
	wasm       []byte
	sdk        string
	unit       *project.Unit
	sources    []analysis.SourcePackage
}

func prepareBuildPlans(ctx context.Context, plans *project.BuildPlans, layoutRoot string, target project.Target, kind string, configuration config.Config, options Options) (_ *Project, err error) {
	graphs := append(append([]*project.Graph(nil), plans.Macros...), plans.Target)
	stages := make(map[*project.Graph]*stagedRole, len(graphs))
	prepared := &Project{plans: plans, graphs: graphs, stages: stages, layoutRoot: layoutRoot, options: options}
	prepared.ctx, prepared.cancel = context.WithCancel(context.Background())
	stopCancel := context.AfterFunc(ctx, prepared.cancel)
	defer stopCancel()
	defer func() {
		if err != nil {
			prepared.Close()
		}
	}()
	for _, graph := range graphs {
		for _, node := range graph.Nodes {
			cached := node.CheckoutRoot != "" && within(graph.Cache, node.CheckoutRoot) && within(node.CheckoutRoot, node.Root)
			if within(layoutRoot, node.Root) && !cached || node.Root != plans.Target.Root.Root && within(node.Root, layoutRoot) {
				return nil, fmt.Errorf("target directory overlaps source module %s", node.Module)
			}
		}
		staged, err := os.MkdirTemp(layoutRoot, ".generation-")
		if err != nil {
			return nil, err
		}
		prepared.directories = append(prepared.directories, staged)
		selected, targetKind := target, kind
		cfg := configuration
		if graph.Role == "proc-macro" {
			selected, targetKind = *graph.Root.Manifest.Lib, "proc-macro"
			cfg.Environment, err = config.PrepareContext(ctx, graph.Tool, graph.Root.Root, "wasip1/wasm", false, graph.Environment)
			if err != nil {
				return nil, err
			}
			cfg.Reads = new(config.Reads)
		}
		stage, err := stageRole(graph, selected, targetKind, cfg, options, plans.Target.Root.SourceKey, staged)
		if err != nil {
			return nil, err
		}
		stage.modules[0].OnProcDiagnostic = options.OnProcDiagnostic
		for index := range stage.modules {
			stage.modules[index].Context = prepared.ctx
		}
		stages[graph] = stage
		if err := prepareModules(ctx, graph, stage.generated); err != nil {
			return nil, err
		}
		if graph.Role == "proc-macro" {
			stage.sdk, err = procmacro.ReadSDKIdentity(ctx, graph.Tool, stage.generated[graph.Root.Module], project.GoEnv(true, graph.Environment))
			if err != nil {
				return nil, err
			}
			if err := procmacro.ValidateSDKIdentity(stage.sdk); err != nil {
				return nil, fmt.Errorf("procedural macro %s: %w", graph.Root.Module, err)
			}
		}
	}
	// All plans and their Go sources are prepared before the first macro runs.
	for _, graph := range graphs {
		stage := stages[graph]
		seen := map[*analysis.ProcLibrary]bool{}
		var attach func(*project.Graph) error
		attach = func(plan *project.Graph) error {
			library := stages[plan].library
			if library == nil {
				return fmt.Errorf("procedural macro build order is incomplete for %s", plan.Root.Module)
			}
			if seen[library] {
				return nil
			}
			stage.modules[0].ProcLibraries = append(stage.modules[0].ProcLibraries, library)
			seen[library] = true
			for _, edge := range plan.ProcEdges {
				if err := attach(edge.Plan); err != nil {
					return err
				}
			}
			return nil
		}
		for _, edge := range graph.ProcEdges {
			if err := attach(edge.Plan); err != nil {
				return nil, err
			}
			for index := range stage.modules {
				module := &stage.modules[index]
				if module.Path == edge.Consumer.Module {
					root := module.Roots[edge.Alias]
					root.PackagePath = stages[edge.Plan].library.PackagePath
					module.Roots[edge.Alias] = root
				}
			}
		}
		if graph.Role == "proc-macro" {
			if err := stage.buildProcMacro(ctx, options.RunProcMacro); err != nil {
				return nil, err
			}
		} else {
			stage.modules[0].StableGo = options.StableGo
			if options.PrepareNativePackage != nil {
				if err := options.PrepareNativePackage(stage.modules); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return prepared, nil
}

func stageRole(graph *project.Graph, target project.Target, kind string, configuration config.Config, options Options, selectedSource, staged string) (*stagedRole, error) {
	stage := &stagedRole{graph: graph, dir: staged, target: target, kind: kind, config: configuration, features: map[string]config.Features{}, sugar: map[string]bool{}, generated: map[string]string{}, originals: map[string]string{}}
	ordered := append([]*project.Node{graph.Root}, graph.Nodes...)
	for _, node := range ordered {
		if stage.generated[node.Module] != "" {
			continue
		}
		features := config.Features{Enabled: node.Features}
		if node.HasManifest {
			if err := singleProject(node.Root); err != nil {
				return nil, err
			}
			if err := supportedManifest(node.Manifest); err != nil {
				return nil, fmt.Errorf("%s: %w", node.Module, err)
			}
			features.Declared = map[string]bool{}
			for name := range node.Manifest.Features {
				features.Declared[name] = true
			}
		}
		if node == graph.Root && graph.Role != "proc-macro" && options.NativeFeatures != nil {
			features = *options.NativeFeatures
		}
		stage.features[node.SourceKey] = features
		output := filepath.Join(staged, "deps", project.Digest(node.Module)[:16])
		selected, targetKind := project.Target{Name: "native", Path: "."}, "lib"
		if node == graph.Root {
			output, selected, targetKind = filepath.Join(staged, "module"), target, kind
			if targetKind == "proc-macro" {
				targetKind = "lib"
			}
		} else if node.Manifest.Lib != nil {
			selected = *node.Manifest.Lib
		}
		packagePath, err := stageSources(node, selected, targetKind, output, node == graph.Root || node.HasManifest, stage.originals)
		if err != nil {
			return nil, err
		}
		if node == graph.Root {
			stage.entry = "./" + filepath.ToSlash(packagePath)
			if packagePath == "." {
				stage.entry = "."
			}
		}
		roots, err := moduleRoots(node)
		if err != nil {
			return nil, err
		}
		if node == graph.Root && graph.Role != "proc-macro" && options.Roots != nil {
			roots = options.Roots
		}
		stage.generated[node.Module] = output
		rootPackage := ""
		if node.Manifest.Lib != nil && (!node.Manifest.Lib.ProcMacro || graph.Role == "proc-macro") {
			rootPackage, err = libraryPackage(node)
			if err != nil {
				return nil, err
			}
		}
		moduleConfig := configuration
		moduleConfig.Features = features
		moduleConfig.GoSugar = node.Manifest.Codegen.GoSugar
		if !node.HasManifest {
			codegen, err := project.ReadCodegen(node.Root)
			if err != nil {
				return nil, err
			}
			moduleConfig.GoSugar = codegen.GoSugar
		}
		if node.SourceKey == selectedSource && options.GoSugar != nil {
			moduleConfig.GoSugar = options.GoSugar
		}
		stage.sugar[node.SourceKey] = moduleConfig.GoSugarEnabled()
		module := analysis.Module{Path: node.Module, Dir: output, RootPackage: rootPackage, Roots: roots, Config: moduleConfig, Tags: options.Tags, Race: options.Race}
		if graph.Role == "proc-macro" {
			module.Tags, module.Race = nil, false
		}
		if node.HasManifest && !(node == graph.Root && options.NativeFeatures != nil) {
			module.Crates, module.Targets, err = targetCrates(node, selected, targetKind)
			if err != nil {
				return nil, err
			}
		}
		module.RuntimeRoots = runtimeRoots(graph, node, roots, rootPackage)
		stage.modules = append(stage.modules, module)
	}
	return stage, nil
}

func runtimeRoots(graph *project.Graph, node *project.Node, roots paths.Roots, rootPackage string) []analysis.RuntimeRoot {
	var result []analysis.RuntimeRoot
	for _, alias := range project.Keys(roots) {
		root := roots[alias]
		if root.Source == "toolchain" || root.Unavailable != "" || root.ModulePath == "" {
			continue
		}
		if _, requested := node.Requests[alias]; !requested {
			if _, selected := graph.Plan[root.ModulePath]; !selected {
				continue
			}
			// A native package alias does not expose its entire module as a
			// macro capability, or compete with the module's declared library.
			namespace := root.PackagePath
			if root.GoPrefix != "" {
				namespace = root.GoPrefix
			}
			if namespace != root.ModulePath {
				continue
			}
		}
		if child := node.Children[alias]; child != nil && child.Manifest.Lib != nil && child.Manifest.Lib.ProcMacro {
			continue
		}
		result = append(result, analysis.RuntimeRoot{LogicalModule: root.ModulePath, BindingID: project.Digest([]string{graph.PlanID, root.ModulePath}), Namespace: root})
	}
	if library := node.Manifest.Lib; library != nil && !library.ProcMacro && targetUnavailable(*library, node.Features) == "" {
		result = append(result, analysis.RuntimeRoot{LogicalModule: node.Module, BindingID: project.Digest([]string{graph.PlanID, node.Module}), Namespace: paths.Namespace{ModulePath: node.Module, PackagePath: rootPackage, Source: "module"}})
	}
	return result
}

func (stage *stagedRole) buildProcMacro(ctx context.Context, run func([]byte, pm.Request) ([]byte, error)) error {
	root := stage.generated[stage.graph.Root.Module]
	packagePath, err := libraryPackage(stage.graph.Root)
	if err != nil {
		return err
	}
	library, err := lower.CompileProcModules(stage.modules, packagePath)
	if err != nil {
		return err
	}
	launcher := filepath.Join(root, "goxide_proc_macro")
	if err := os.Mkdir(launcher, 0o755); err != nil {
		return fmt.Errorf("generated macro launcher directory: %w", err)
	}
	data, err := procLauncher(library)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(launcher, "main.go"), data, 0o644); err != nil {
		return err
	}
	artifact := filepath.Join(stage.dir, "macro.wasm")
	if _, err := goCommand(ctx, stage.graph, root, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -buildid=", "-o", artifact, "./goxide_proc_macro"); err != nil {
		return err
	}
	stage.wasm, err = os.ReadFile(artifact)
	if err != nil {
		return err
	}
	stage.wasm, err = procmacro.PrepareWASM(stage.wasm)
	if err != nil {
		return err
	}
	if err := os.WriteFile(artifact, stage.wasm, 0o644); err != nil {
		return err
	}
	library.Run = func(request pm.Request) ([]byte, error) {
		if run == nil {
			return nil, fmt.Errorf("procedural macro execution requires a restricted WASI host, which is not configured")
		}
		return run(append([]byte(nil), stage.wasm...), request)
	}
	// A source projection belongs to one build plan, even when another version
	// or feature plan of the same canonical crate is compiled in this pass.
	library.PackagePath += "/goxide_proc_" + stage.graph.PlanID[:16]
	stage.library = library
	return nil
}
