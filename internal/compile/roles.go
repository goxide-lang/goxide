package compile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	modules    []lower.Module
	generated  map[string]string
	library    *lower.ProcLibrary
	wasm       []byte
	sdk        string
	unit       *project.Unit
	sources    []lower.SourcePackage
}

func prepareBuildPlans(plans *project.BuildPlans, layout *project.Layout, target project.Target, kind string, configuration config.Config, options Options) (*Result, error) {
	graphs := append(append([]*project.Graph(nil), plans.Macros...), plans.Target)
	stages := make(map[*project.Graph]*stagedRole, len(graphs))
	for _, graph := range graphs {
		for _, node := range graph.Nodes {
			cached := node.CheckoutRoot != "" && within(graph.Cache, node.CheckoutRoot) && within(node.CheckoutRoot, node.Root)
			if within(layout.Root, node.Root) && !cached || node.Root != plans.Target.Root.Root && within(node.Root, layout.Root) {
				return nil, fmt.Errorf("target directory overlaps source module %s", node.Module)
			}
		}
		staged, err := os.MkdirTemp(layout.Root, ".generation-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(staged)
		selected, targetKind := target, kind
		cfg := configuration
		if graph.Role == "proc-macro" {
			selected, targetKind = *graph.Root.Manifest.Lib, "proc-macro"
			cfg.Environment, err = config.Prepare(graph.Tool, graph.Root.Root, "wasip1/wasm", false, graph.Environment)
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
		stages[graph] = stage
		if err := prepareModules(graph, stage.generated); err != nil {
			return nil, err
		}
		if graph.Role == "proc-macro" {
			stage.sdk, err = procmacro.ReadSDKIdentity(context.Background(), graph.Tool, stage.generated[graph.Root.Module], project.GoEnv(true, graph.Environment))
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
		seen := map[*lower.ProcLibrary]bool{}
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
			if err := stage.buildProcMacro(options.RunProcMacro); err != nil {
				return nil, err
			}
		} else {
			if options.PrepareNativePackage != nil {
				if err := options.PrepareNativePackage(stage.modules); err != nil {
					return nil, err
				}
			}
			if options.OnPreparedSources != nil {
				stage.modules[0].OnSourcePackages = func(sources []lower.SourcePackage) error { stage.sources = sources; return nil }
			}
			if err := lower.CompileModules(stage.modules); err != nil {
				return nil, err
			}
			if options.OnPreparedSources != nil {
				prepared := PreparedSources{SourcePackages: stage.sources, Roots: stage.modules[0].Roots, RuntimeRoots: stage.modules[0].RuntimeRoots}
				for _, dependency := range graphs {
					if producer := stages[dependency]; producer.library != nil {
						prepared.Macros = append(prepared.Macros, ProcArtifact{Library: producer.library, WASM: append([]byte(nil), producer.wasm...)})
					}
				}
				if err := options.OnPreparedSources(prepared); err != nil {
					return nil, err
				}
			}
			arguments := []string{"list", "-mod=readonly", "-tags=" + strings.Join(options.Tags, ","), "-f", "{{.Name}}"}
			if options.Race {
				arguments = append(arguments, "-race")
			}
			name, err := goCommand(graph, stage.generated[graph.Root.Module], append(arguments, stage.entry)...)
			if err != nil {
				return nil, err
			}
			if main := strings.TrimSpace(string(name)) == "main"; kind == "lib" && main || kind != "lib" && !main {
				if kind == "lib" {
					return nil, fmt.Errorf("lib target %s must be an importable package, not package main", target.Name)
				}
				return nil, fmt.Errorf("%s target %s must be package main", kind, target.Name)
			}
		}
		tags, race := options.Tags, options.Race
		if graph.Role == "proc-macro" {
			tags, race = nil, false
		}
		spec, err := unitSpec(graph, stage.target, stage.kind, stage.config, stage.features, stage.sugar, tags, race)
		if err != nil {
			return nil, err
		}
		if len(plans.Macros) > 0 || graph.Role == "proc-macro" {
			content, err := stagedContent(stage.dir)
			if err != nil {
				return nil, err
			}
			spec.Config = project.Digest([]string{spec.Config, stage.sdk, procmacro.ProfileID, content})
		}
		stage.unit, err = layout.Begin(spec)
		if err != nil {
			return nil, err
		}
	}
	if err := plans.Save(); err != nil {
		return nil, err
	}
	for _, graph := range graphs {
		stage := stages[graph]
		if err := stage.unit.PublishBundle(stage.dir); err != nil {
			return nil, err
		}
	}
	final := stages[plans.Target]
	final.config.Features = final.features[plans.Target.Root.SourceKey]
	final.config.GoSugar = final.modules[0].Config.GoSugar
	result := &Result{Root: filepath.Join(final.unit.Bundle, "module"), Entry: final.entry, Kind: final.kind, Config: final.config}
	if len(final.wasm) != 0 {
		result.ProcMacro = filepath.Join(final.unit.Bundle, "macro.wasm")
	}
	seen := map[string]bool{}
	for _, graph := range graphs {
		for _, node := range graph.Nodes {
			if !seen[node.Root] {
				result.SourceRoots = append(result.SourceRoots, node.Root)
				seen[node.Root] = true
			}
		}
	}
	return result, nil
}

func stageRole(graph *project.Graph, target project.Target, kind string, configuration config.Config, options Options, selectedSource, staged string) (*stagedRole, error) {
	stage := &stagedRole{graph: graph, dir: staged, target: target, kind: kind, config: configuration, features: map[string]config.Features{}, sugar: map[string]bool{}, generated: map[string]string{}}
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
		packagePath, err := stageSources(node, selected, targetKind, output)
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
		module := lower.Module{Path: node.Module, Dir: output, RootPackage: rootPackage, Roots: roots, Config: moduleConfig, Tags: options.Tags, Race: options.Race}
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

func runtimeRoots(graph *project.Graph, node *project.Node, roots paths.Roots, rootPackage string) []lower.RuntimeRoot {
	var result []lower.RuntimeRoot
	for _, alias := range project.Keys(roots) {
		root := roots[alias]
		if root.Source == "toolchain" || root.Unavailable != "" || root.ModulePath == "" {
			continue
		}
		if _, requested := node.Requests[alias]; !requested {
			if _, selected := graph.Plan[root.ModulePath]; !selected {
				continue
			}
		}
		if child := node.Children[alias]; child != nil && child.Manifest.Lib != nil && child.Manifest.Lib.ProcMacro {
			continue
		}
		result = append(result, lower.RuntimeRoot{LogicalModule: root.ModulePath, BindingID: project.Digest([]string{graph.PlanID, root.ModulePath}), Namespace: root})
	}
	if library := node.Manifest.Lib; library != nil && !library.ProcMacro && targetUnavailable(*library, node.Features) == "" {
		result = append(result, lower.RuntimeRoot{LogicalModule: node.Module, BindingID: project.Digest([]string{graph.PlanID, node.Module}), Namespace: paths.Namespace{ModulePath: node.Module, PackagePath: rootPackage, Source: "module"}})
	}
	return result
}

func stagedContent(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%d:%s%d:", len(filepath.ToSlash(relative)), filepath.ToSlash(relative), len(data))
		hash.Write(data)
		return nil
	})
	return hex.EncodeToString(hash.Sum(nil)), err
}

func (stage *stagedRole) buildProcMacro(run func([]byte, pm.Request) ([]byte, error)) error {
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
	if _, err := goCommand(stage.graph, root, "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", artifact, "./goxide_proc_macro"); err != nil {
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
