// Package compile connects author projects to the new compiler pipeline.
package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/project"
)

type Options struct {
	Bin, GoTool, TargetDir string
	Offline, Locked        bool
	Features               []string
	NoDefaultFeatures      bool
	NativeFeatures         *expand.Features // Native directory inputs keep an open root feature set.
	Roots                  paths.Roots      // Optional root-module namespace context for native directory inputs.
}

// Result names a stable generated Go module and its selected binary package.
type Result struct {
	Root, Entry string
	SourceRoots []string // Original inputs, retained for caller output protection.
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
	tool := options.GoTool
	if tool == "" {
		tool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	environment, err := goEnvironment(tool)
	if err != nil {
		return nil, err
	}
	release, err := project.LockProject(root)
	if err != nil {
		return nil, err
	}
	defer release()
	graph, err := project.LoadWithTarget(root, tool, options.Features, !options.NoDefaultFeatures, options.Offline, options.Locked, options.TargetDir)
	if err != nil {
		return nil, err
	}
	target, err := project.Select(manifest, options.Bin, "", graph.Root.Features)
	if err != nil {
		return nil, err
	}
	features := make(map[string]expand.Features, len(graph.Nodes))
	for _, node := range graph.Nodes {
		configuration := expand.Features{Enabled: node.Features}
		if node.HasManifest {
			if err := singleProject(node.Root); err != nil {
				return nil, err
			}
			if err := supportedManifest(node.Manifest); err != nil {
				return nil, fmt.Errorf("%s: %w", node.Module, err)
			}
			configuration.Declared = make(map[string]bool, len(node.Manifest.Features))
			for name := range node.Manifest.Features {
				configuration.Declared[name] = true
			}
		}
		if node == graph.Root && options.NativeFeatures != nil {
			configuration = *options.NativeFeatures
		}
		features[node.Module] = configuration
	}
	layout, err := project.OpenLayout(root, manifest.Module, options.TargetDir)
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
	spec, err := unitSpec(graph, target, environment, features)
	if err != nil {
		return nil, err
	}
	unit, err := layout.Begin(spec)
	if err != nil {
		return nil, err
	}
	staged, err := os.MkdirTemp(unit.Intermediate, ".generation-")
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
		modules = append(modules, lower.Module{Path: node.Module, Dir: output, GoTool: tool, Roots: roots, Features: features[node.Module]})
	}
	if err := prepareModules(graph, generated); err != nil {
		return nil, err
	}
	if err := lower.CompileModules(modules); err != nil {
		return nil, err
	}
	name, err := goCommand(graph, generated[graph.Root.Module], "list", "-mod=readonly", "-f", "{{.Name}}", entry)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(name)) != "main" {
		return nil, fmt.Errorf("bin target %s must be package main", target.Name)
	}
	if err := graph.Save(); err != nil {
		return nil, err
	}
	if err := unit.PublishBundle(staged); err != nil {
		return nil, err
	}
	result := &Result{Root: filepath.Join(unit.Bundle, "module"), Entry: entry}
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
	if manifest.Vendor != nil {
		return fmt.Errorf("vendor support resumes after pre-MVP")
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

func goEnvironment(tool string) (map[string]string, error) {
	command := exec.Command(tool, "env", "-json", "GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS", "PKG_CONFIG")
	command.Env = project.GoEnv(true)
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read Go environment: %w", err)
	}
	var environment map[string]string
	if err := json.Unmarshal(data, &environment); err != nil {
		return nil, err
	}
	if environment["GOVERSION"] != runtime.Version() {
		return nil, fmt.Errorf("Go tool %s differs from compiler importer %s", environment["GOVERSION"], runtime.Version())
	}
	if environment["GOOS"] != runtime.GOOS || environment["GOARCH"] != runtime.GOARCH {
		return nil, fmt.Errorf("project compilation requires host GOOS/GOARCH")
	}
	return environment, nil
}

func unitSpec(graph *project.Graph, target project.Target, environment map[string]string, features map[string]expand.Features) (project.UnitSpec, error) {
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
		for _, name := range project.Keys(features[node.Module].Enabled) {
			if features[node.Module].Enabled[name] {
				enabled = append(enabled, name)
			}
		}
		sources = append(sources, source{node.Module, node.Root, string(node.ModBytes), node.Manifest, enabled})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Module < sources[j].Module })
	config := project.Digest(struct {
		Sources     []source
		Target      project.Target
		Environment map[string]string
		Compiler    string
		Plan        string
	}{sources, target, environment, project.Digest(compiler), graph.PlanID})
	platform := runtime.GOOS + "-" + runtime.GOARCH
	return project.UnitSpec{Host: platform, Target: platform, Kind: "bin", Name: target.Name, Config: config}, nil
}

func within(root, name string) bool {
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
