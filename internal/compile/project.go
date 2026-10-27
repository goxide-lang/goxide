// Package compile connects author projects to the new compiler pipeline.
package compile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	pm "github.com/goxide-lang/syntax/proc_macro"
)

type Options struct {
	Bin, Example, GoTool, TargetDir, Target string
	Lib                                     bool
	// StableGo requests a separately consumable library export. Selecting a
	// library target alone keeps the ordinary project compilation behavior.
	StableGo          bool
	Offline, Locked   bool
	Features          []string
	NoDefaultFeatures bool
	GoSugar           *bool            // Overrides only the selected entry module.
	NativeFeatures    *config.Features // Native directory inputs keep an open root feature set.
	NativeGeneration  bool             // Native gen keeps Go replacement import identities.
	Roots             paths.Roots      // Optional root-module namespace context for native directory inputs.
	Config            config.Config
	Tags              []string
	Race              bool
	// RunProcMacro executes a prepared WASM command. A nil callback leaves
	// actual expansion unavailable until a restricted host is supplied.
	RunProcMacro     func(wasm []byte, request pm.Request) ([]byte, error)
	OnProcDiagnostic func(filename string, diagnostic expand.ProcDiagnostic)
	// OnPreparedSources runs while the role-specific importers and staged Go
	// dependencies remain available, so callers can create fixed distributions.
	OnPreparedSources    func(PreparedSources) error
	PrepareNativePackage func([]lower.Module) error
}

// PreparedSources is valid only during OnPreparedSources. Its macro libraries
// retain their own role's live importers, so fixed-distribution snapshots must
// be taken here rather than after the staging directories have been removed.
type PreparedSources struct {
	SourcePackages []lower.SourcePackage
	Macros         []ProcArtifact
	Roots          paths.Roots
	RuntimeRoots   []lower.RuntimeRoot
}

// Result names a stable generated Go module and its selected target package.
type Result struct {
	Root, Entry string
	Kind        string
	SourceRoots []string // Original inputs, retained for caller output protection.
	Config      config.Config
	ProcMacro   string // Prepared WASI command for a proc-macro lib target.
}

// ProcArtifact is one checked macro program and its restricted WASI command.
// The library is available for a source snapshot; execution uses the supplied host.
type ProcArtifact struct {
	Library *lower.ProcLibrary
	WASM    []byte
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
	// Native inputs use a private staged project and always execute with GOWORK=off.
	if work, _ := environment.LookupEnv("GOWORK"); options.NativeFeatures == nil && work != "" && work != "off" {
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
	plans, err := project.LoadBuildPlans(root, tool, options.Features, !options.NoDefaultFeatures, options.Offline, options.Locked, targetDir, environment.Target.OS+"/"+environment.Target.Arch, options.Lib && manifest.Lib != nil && manifest.Lib.ProcMacro, options.NativeGeneration, environment.ExecEnv())
	if err != nil {
		return nil, err
	}
	graph := plans.Target
	target, err := project.Select(manifest, options.Bin, options.Example, options.Lib, graph.Root.Features)
	if err != nil {
		return nil, err
	}
	kind := "bin"
	if options.Lib {
		kind = "lib"
	} else if options.Example != "" {
		kind = "example"
	}
	layout, err := project.OpenLayout(root, manifest.Module, targetDir)
	if err != nil {
		return nil, err
	}
	defer layout.Close()
	return prepareBuildPlans(plans, layout, target, kind, configuration, options)
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
	for _, alias := range project.Keys(manifest.Dependencies) {
		dependency := manifest.Dependencies[alias]
		if dependency.Workspace {
			return fmt.Errorf("dependency %s: workspace selection resumes after pre-MVP", alias)
		}
	}
	return nil
}

func unitSpec(graph *project.Graph, target project.Target, kind string, configuration config.Config, features map[string]config.Features, sugar map[string]bool, tags []string, race, stableGo bool) (project.UnitSpec, error) {
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
		GoSugar           bool
	}
	var sources []source
	for _, node := range graph.Nodes {
		var enabled []string
		for _, name := range project.Keys(features[node.SourceKey].Enabled) {
			if features[node.SourceKey].Enabled[name] {
				enabled = append(enabled, name)
			}
		}
		sources = append(sources, source{node.Module, node.Root, string(node.ModBytes), node.Manifest, enabled, sugar[node.SourceKey]})
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
		Host        config.Target
		Platform    config.Target
		Environment map[string][]byte
		EnvReads    []config.EnvInput
		Tags        []string
		Race        bool
		StableGo    bool
		GoTool      string
		Compiler    string
		Plan        string
	}{sources, target, environment.Host, environment.Target, values, configuration.Reads.Inputs(), project.Keys(buildTags), race, stableGo, environment.GoTool, project.Digest(compiler), unitDependencyID(graph)})
	host := environment.Host.OS + "-" + environment.Host.Arch
	platform := environment.Target.OS + "-" + environment.Target.Arch
	return project.UnitSpec{Host: host, Target: platform, Kind: kind, Name: target.Name, Config: identity}, nil
}

// Unit paths identify selected configurations, not macro source revisions.
// Include the entire macro dependency closure: a nested selection can change
// without changing the consumer's own dependency plan.
func unitDependencyID(graph *project.Graph) string {
	dependencies := make([]project.RoleDependency, 0, len(graph.ProcEdges))
	for _, edge := range graph.ProcEdges {
		dependencies = append(dependencies, project.RoleDependency{Consumer: edge.Consumer.SourceKey, Alias: edge.Alias, Plan: unitDependencyID(edge.Plan)})
	}
	sort.Slice(dependencies, func(i, j int) bool {
		if dependencies[i].Consumer != dependencies[j].Consumer {
			return dependencies[i].Consumer < dependencies[j].Consumer
		}
		return dependencies[i].Alias < dependencies[j].Alias
	})
	return project.Digest(struct {
		Plan         string
		Dependencies []project.RoleDependency
	}{graph.DependencyPlanID, dependencies})
}

func within(root, name string) bool {
	relative, err := filepath.Rel(root, name)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
