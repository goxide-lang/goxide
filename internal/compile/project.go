// Package compile connects author projects to the new compiler pipeline.
package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/config"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/project"
)

type Options struct {
	Bin, GoTool, TargetDir, Target string
	Offline, Locked                bool
	Features                       []string
	NoDefaultFeatures              bool
	NativeFeatures                 *config.Features // Native directory inputs keep an open root feature set.
	Roots                          paths.Roots      // Optional root-module namespace context for native directory inputs.
	Config                         config.Config
	Tags                           []string
	Race                           bool
}

// Result names a stable generated Go module and its selected binary package.
type Result struct {
	Root, Entry string
	SourceRoots []string // Original inputs, retained for caller output protection.
	Config      config.Config
}

// PrepareProject generates a single manifest project and its source dependencies.
// The caller can run Go build/run/test in Result.Root after this function returns.
func PrepareProject(directory string, options Options) (*Result, error) {
	root, err := project.DiscoverProject(directory)
	if err != nil {
		return nil, err
	}
	if root == "" {
		return nil, fmt.Errorf("no %s found from %s", project.ManifestName, directory)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if err := singleProject(root); err != nil {
		return nil, err
	}
	manifest, err := project.Read(root)
	if err != nil {
		return nil, err
	}
	if err := supportedManifest(manifest); err != nil {
		return nil, err
	}
	configuration := options.Config
	if configuration.Environment == nil {
		configuration.Environment, err = config.Prepare(options.GoTool, root, options.Target, false, os.Environ())
		if err != nil {
			return nil, err
		}
	}
	if configuration.Reads == nil {
		configuration.Reads = &config.Reads{}
	}
	environment := configuration.Environment
	tool := environment.GoTool
	if work, _ := environment.LookupEnv("GOWORK"); work != "" && work != "off" {
		return nil, fmt.Errorf("workspace migration unsupported: explicit GOWORK")
	}
	targetDir := options.TargetDir
	if targetDir == "" {
		targetDir, _ = environment.LookupEnv("GOXIDE_TARGET_DIR")
	}
	if targetDir == "" {
		targetDir = "target"
	}
	release, err := project.LockProject(root)
	if err != nil {
		return nil, err
	}
	defer release()
	graph, err := project.LoadWithTarget(root, tool, options.Features, !options.NoDefaultFeatures, options.Offline, options.Locked, targetDir, environment.ExecEnv())
	if err != nil {
		return nil, err
	}
	target, err := project.Select(manifest, options.Bin, "", graph.Root.Features)
	if err != nil {
		return nil, err
	}
	features := make(map[string]config.Features, len(graph.Nodes))
	for _, node := range graph.Nodes {
		selected := config.Features{Enabled: node.Features}
		if node.HasManifest {
			if err := singleProject(node.Root); err != nil {
				return nil, err
			}
			if err := supportedManifest(node.Manifest); err != nil {
				return nil, fmt.Errorf("%s: %w", node.Module, err)
			}
			selected.Declared = make(map[string]bool, len(node.Manifest.Features))
			for name := range node.Manifest.Features {
				selected.Declared[name] = true
			}
		}
		if node == graph.Root && options.NativeFeatures != nil {
			selected = *options.NativeFeatures
		}
		features[node.SourceKey] = selected
	}
	layout, err := project.OpenLayout(root, manifest.Module, targetDir)
	if err != nil {
		return nil, err
	}
	defer layout.Close()
	for _, node := range graph.Nodes {
		cached := node.CheckoutRoot != "" && within(graph.Cache, node.CheckoutRoot) && within(node.CheckoutRoot, node.Root)
		if within(layout.Root, node.Root) && !cached || node != graph.Root && within(node.Root, layout.Root) {
			return nil, fmt.Errorf("target directory overlaps source module %s", node.Module)
		}
	}
	staged, err := os.MkdirTemp(layout.Root, ".generation-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staged)
	modules := make([]lower.Module, 0, len(graph.Nodes))
	generated := make(map[string]string, len(graph.Nodes))
	ordered := append([]*project.Node{graph.Root}, graph.Nodes...)
	entry := ""
	for _, node := range ordered {
		if generated[node.Module] != "" {
			continue
		}
		output := filepath.Join(staged, "deps", project.Digest(node.Module)[:16])
		selected := project.Target{Name: "native", Path: "."}
		kind := "lib"
		if node == graph.Root {
			output = filepath.Join(staged, "module")
			selected, kind = target, "bin"
		} else if node.Manifest.Lib != nil {
			selected = *node.Manifest.Lib
		}
		packagePath, err := stageSources(node, selected, kind, output)
		if err != nil {
			return nil, err
		}
		if node == graph.Root {
			entry = "./" + filepath.ToSlash(packagePath)
			if packagePath == "." {
				entry = "."
			}
		}
		roots, err := moduleRoots(node)
		if err != nil {
			return nil, err
		}
		if node == graph.Root && options.Roots != nil {
			roots = options.Roots
		}
		generated[node.Module] = output
		moduleConfig := configuration
		moduleConfig.Features = features[node.SourceKey]
		modules = append(modules, lower.Module{Path: node.Module, Dir: output, Roots: roots, Config: moduleConfig, Tags: options.Tags, Race: options.Race})
	}
	if err := prepareModules(graph, generated); err != nil {
		return nil, err
	}
	if err := lower.CompileModules(modules); err != nil {
		return nil, err
	}
	arguments := []string{"list", "-mod=readonly", "-tags=" + strings.Join(options.Tags, ","), "-f", "{{.Name}}"}
	if options.Race {
		arguments = append(arguments, "-race")
	}
	name, err := goCommand(graph, generated[graph.Root.Module], append(arguments, entry)...)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(name)) != "main" {
		return nil, fmt.Errorf("bin target %s must be package main", target.Name)
	}
	spec, err := unitSpec(graph, target, configuration, features, options.Tags, options.Race)
	if err != nil {
		return nil, err
	}
	unit, err := layout.Begin(spec)
	if err != nil {
		return nil, err
	}
	if err := graph.Save(); err != nil {
		return nil, err
	}
	if err := unit.PublishBundle(staged); err != nil {
		return nil, err
	}
	configuration.Features = features[graph.Root.SourceKey]
	result := &Result{Root: filepath.Join(unit.Bundle, "module"), Entry: entry, Config: configuration}
	for _, node := range graph.Nodes {
		result.SourceRoots = append(result.SourceRoots, node.Root)
	}
	return result, nil
}

func singleProject(root string) error {
	workspace, err := project.DiscoverWorkspace(root)
	if err != nil {
		return err
	}
	if workspace != nil {
		return fmt.Errorf("%s: workspace support resumes after pre-MVP", root)
	}
	data, err := os.ReadFile(filepath.Join(root, "goxide.lock"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var lock project.Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if lock.Version == 3 || lock.Version == 4 || lock.Unified != nil || lock.NativeWorkspace != nil {
		return fmt.Errorf("%s: workspace locks require workspace support after pre-MVP", root)
	}
	return nil
}

func supportedManifest(manifest project.Manifest) error {
	if manifest.Build != nil {
		return fmt.Errorf("build scripts resume after pre-MVP")
	}
	if len(manifest.Examples) != 0 {
		return fmt.Errorf("example targets are not supported")
	}
	for _, alias := range project.Keys(manifest.Dependencies) {
		dependency := manifest.Dependencies[alias]
		if dependency.Workspace {
			return fmt.Errorf("dependency %s: workspace selection resumes after pre-MVP", alias)
		}
	}
	return nil
}

func unitSpec(graph *project.Graph, target project.Target, configuration config.Config, features map[string]config.Features, tags []string, race bool) (project.UnitSpec, error) {
	executable, err := os.Executable()
	if err != nil {
		return project.UnitSpec{}, err
	}
	compiler, err := os.ReadFile(executable)
	if err != nil {
		return project.UnitSpec{}, err
	}
	type source struct {
		Module, Root, Mod string
		Manifest          project.Manifest
		Features          []string
	}
	var sources []source
	for _, node := range graph.Nodes {
		var enabled []string
		for _, name := range project.Keys(features[node.SourceKey].Enabled) {
			if features[node.SourceKey].Enabled[name] {
				enabled = append(enabled, name)
			}
		}
		sources = append(sources, source{node.Module, node.Root, string(node.ModBytes), node.Manifest, enabled})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Module < sources[j].Module })
	buildTags := map[string]bool{}
	for _, tag := range tags {
		buildTags[tag] = true
	}
	environment := configuration.Environment
	values := map[string][]byte{}
	for key, value := range environment.GoValues() {
		values[key] = []byte(value)
	}
	identity := project.Digest(struct {
		Sources     []source
		Target      project.Target
		Platform    config.Target
		Environment map[string][]byte
		EnvReads    []config.EnvInput
		Tags        []string
		Race        bool
		GoTool      string
		Compiler    string
		Plan        string
	}{sources, target, environment.Target, values, configuration.Reads.Inputs(), project.Keys(buildTags), race, environment.GoTool, project.Digest(compiler), graph.PlanID})
	host := environment.Host.OS + "-" + environment.Host.Arch
	platform := environment.Target.OS + "-" + environment.Target.Arch
	return project.UnitSpec{Host: host, Target: platform, Kind: "bin", Name: target.Name, Config: identity}, nil
}

func within(root, name string) bool {
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
