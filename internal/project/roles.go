package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BuildPlans separates the selected target from the macro programs needed to
// compile it. Macros are ordered with their prerequisites first.
type BuildPlans struct {
	Target *Graph
	Macros []*Graph
	owner  *Graph
}

// ProcEdge retains the source dependency even though it is absent from the
// consumer's Go build list. Consumer.Children[Alias] is Plan.Root.
type ProcEdge struct {
	Consumer *Node
	Alias    string
	Plan     *Graph
}

type RoleSource struct {
	Key, Module, Input string
	Features           []string
}

type RoleDependency struct {
	Consumer, Alias, Plan string
}

// RoleLock records configuration and selected sources. Local source edits
// invalidate PlanID without changing the dependency lock.
type RoleLock struct {
	Role, Platform, Root string
	Crate                *Target
	RootSelection        *Selection `json:",omitempty"`
	Sources              []RoleSource
	Modules              map[string]Selection
	ProcSources          map[string]Selection `json:",omitempty"`
	Dependencies         []RoleDependency     `json:",omitempty"`
}

type rolePlanner struct {
	owner      *Graph
	layoutRoot string
	plans      map[string]*Graph
	active     map[string]bool
	stack      []*Graph
	order      []*Graph
	selections map[string]Selection
	contents   map[string]string
}

// LoadBuildPlans uses one owner lock and acquisition cache. target is GOOS/GOARCH;
// procRoot selects the project's lib as a macro program instead of a normal
// bin/example. A normal target can depend on its own separate proc-macro lib.
func LoadBuildPlans(root, tool string, requested []string, defaults, offline, locked bool, targetDir, target string, procRoot, nativeGeneration bool, environment ...[]string) (*BuildPlans, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	base := os.Environ()
	if len(environment) != 0 {
		base = append([]string{}, environment[0]...)
	}
	if err := CheckAuthoring(root, base); err != nil {
		return nil, err
	}
	if workspace, err := DiscoverWorkspace(root); err != nil {
		return nil, err
	} else if workspace != nil {
		return nil, fmt.Errorf("role planning requires a standalone project; workspace support resumes after pre-MVP")
	}
	manifest, err := Read(root)
	if err != nil {
		return nil, err
	}
	if procRoot && (manifest.Lib == nil || !manifest.Lib.ProcMacro) {
		return nil, fmt.Errorf("proc-macro root requires a proc_macro lib")
	}
	if _, err := roleEnvironment(base, target); err != nil {
		return nil, err
	}
	layout, err := OpenLayout(root, manifest.Module, targetDir)
	if err != nil {
		return nil, err
	}
	defer layout.Close()
	work, err := os.MkdirTemp(layout.Root, ".role-plan-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	owner := &Graph{WorkRoot: work, Tool: tool, Environment: base, Offline: offline, Locked: locked, Cache: filepath.Join(layout.Root, "cache", "git"), LockPath: filepath.Join(root, "goxide.lock"), ownerRoot: root, ownerModule: manifest.Module,
		Lock: Lock{Version: 2, Git: map[string]string{}, Sums: map[string]string{}, Roles: map[string]RoleLock{}}, nativeGeneration: nativeGeneration}
	data, err := os.ReadFile(owner.LockPath)
	if err == nil {
		if err := json.Unmarshal(data, &owner.Lock); err != nil {
			return nil, err
		}
		if owner.Lock.Version != 2 || owner.Lock.Unified != nil || owner.Lock.NativeWorkspace != nil {
			return nil, fmt.Errorf("role planning requires a standalone project lock")
		}
		owner.original = data
	} else if !os.IsNotExist(err) {
		return nil, err
	} else if locked {
		return nil, fmt.Errorf("--locked requires goxide.lock")
	}
	if owner.Lock.Git == nil {
		owner.Lock.Git = map[string]string{}
	}
	if owner.Lock.Sums == nil {
		owner.Lock.Sums = map[string]string{}
	}
	if owner.Lock.Roles == nil {
		owner.Lock.Roles = map[string]RoleLock{}
	}
	planner := &rolePlanner{owner: owner, layoutRoot: layout.Root, plans: map[string]*Graph{}, active: map[string]bool{}, selections: map[string]Selection{}, contents: map[string]string{}}
	role := "target"
	if procRoot {
		role, target = "proc-macro", "wasip1/wasm"
	}
	graph, err := planner.load(root, nil, requested, defaults, role, target)
	if err != nil {
		return nil, err
	}
	result := &BuildPlans{Target: graph, owner: owner}
	for _, macro := range planner.order {
		if macro != graph {
			result.Macros = append(result.Macros, macro)
		}
	}
	// All lock changes must be known before executing any prepared macro.
	if locked {
		if err := owner.Save(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (plans *BuildPlans) Save() error { return plans.owner.Save() }

func roleEnvironment(base []string, platform string) ([]string, error) {
	goos, goarch, ok := strings.Cut(platform, "/")
	if !ok || goos == "" || goarch == "" || strings.Contains(goarch, "/") {
		return nil, fmt.Errorf("role target must be GOOS/GOARCH: %q", platform)
	}
	env := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if key == "GOOS" || key == "GOARCH" || platform == "wasip1/wasm" && key == "CGO_ENABLED" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "GOOS="+goos, "GOARCH="+goarch)
	if platform == "wasip1/wasm" {
		env = append(env, "CGO_ENABLED=0")
	}
	return env, nil
}

func (p *rolePlanner) load(root string, source *Node, requested []string, defaults bool, role, platform string) (*Graph, error) {
	environment, err := roleEnvironment(p.owner.Environment, platform)
	if err != nil {
		return nil, err
	}
	g := &Graph{Role: role, Platform: platform, roles: p, roleRoot: source, WorkRoot: p.owner.WorkRoot, Tool: p.owner.Tool, Environment: environment, Offline: p.owner.Offline, Locked: p.owner.Locked,
		Cache: p.owner.Cache, LockPath: p.owner.LockPath, Lock: p.owner.Lock, original: p.owner.original, ownerRoot: p.owner.ownerRoot, ownerModule: p.owner.ownerModule, nativeGeneration: p.owner.nativeGeneration}
	if _, err := g.plan(root, requested, defaults); err != nil {
		return nil, err
	}
	if role == "proc-macro" {
		for _, feature := range g.Root.Manifest.Lib.RequiredFeatures {
			if !g.Root.Features[feature] {
				return nil, fmt.Errorf("target %s requires feature %s", g.Root.Manifest.Lib.Name, feature)
			}
		}
	}
	p.mergeSums(g.Lock.PlanningSums)
	record, key, unit, err := p.record(g)
	if err != nil {
		return nil, err
	}
	g.DependencyPlanID = key
	if previous := p.plans[unit]; previous != nil {
		if p.active[unit] {
			var chain []string
			for _, frame := range p.stack {
				chain = append(chain, frame.Root.Module+" ("+frame.Role+")")
			}
			chain = append(chain, g.Root.Module+" ("+g.Role+")")
			return nil, fmt.Errorf("procedural macro build cycle: %s", strings.Join(chain, " -> "))
		}
		return previous, nil
	}
	p.plans[unit], p.active[unit] = g, true
	p.stack = append(p.stack, g)
	defer func() { p.stack = p.stack[:len(p.stack)-1]; delete(p.active, unit) }()
	for _, consumer := range g.Nodes {
		for _, alias := range keys(consumer.Children) {
			child := consumer.Children[alias]
			if !procBoundary(child, g.Root) {
				continue
			}
			macro, err := p.load(child.Root, child, keys(child.Features), false, "proc-macro", "wasip1/wasm")
			if err != nil {
				return nil, err
			}
			consumer.Children[alias] = macro.Root
			g.ProcEdges = append(g.ProcEdges, ProcEdge{consumer, alias, macro})
		}
	}
	if role == "target" && g.Root.Manifest.Lib != nil && g.Root.Manifest.Lib.ProcMacro {
		macro, err := p.load(g.Root.Root, g.Root, keys(g.Root.Features), false, "proc-macro", "wasip1/wasm")
		if err != nil {
			return nil, err
		}
		alias := g.Root.Manifest.Lib.Name
		g.Root.Children[alias] = macro.Root
		g.ProcEdges = append(g.ProcEdges, ProcEdge{g.Root, alias, macro})
	}
	var macroIDs []string
	for _, edge := range g.ProcEdges {
		record.Dependencies = append(record.Dependencies, RoleDependency{edge.Consumer.lockSourceKey(p.owner.ownerRoot), edge.Alias, edge.Plan.DependencyPlanID})
		macroIDs = append(macroIDs, edge.Plan.PlanID)
	}
	sort.Slice(record.Dependencies, func(i, j int) bool {
		a, b := record.Dependencies[i], record.Dependencies[j]
		return a.Consumer+"\x00"+a.Alias < b.Consumer+"\x00"+b.Alias
	})
	sort.Strings(macroIDs)
	g.PlanID = Digest([]any{unit, macroIDs})
	if role == "target" && len(g.ProcEdges) == 0 {
		nodes := map[string]*Node{}
		for _, node := range g.Nodes {
			nodes[node.SourceKey] = node
		}
		if err := g.lockModulePlan(root, g.Plan, nodes); err != nil {
			return nil, err
		}
		p.owner.Lock.Plans = g.Lock.Plans
		g.DependencyPlanID = Digest(g.portablePlan(root, g.Plan, nodes))
		g.PlanID = g.DependencyPlanID
		return g, nil
	}
	if old, exists := p.owner.Lock.Roles[key]; g.Locked && (!exists || Digest(old) != Digest(record)) {
		return nil, fmt.Errorf("--locked: %s role plan missing or changed for %s", role, g.Root.Module)
	}
	p.owner.Lock.Roles[key] = record
	if role == "proc-macro" {
		p.order = append(p.order, g)
	}
	return g, nil
}

func procBoundary(node, root *Node) bool {
	return node != root && node.Manifest.Lib != nil && node.Manifest.Lib.ProcMacro
}

func (g *Graph) projectedMod(node, root *Node) []byte {
	if g.roles != nil && procBoundary(node, root) {
		return []byte(fmt.Sprintf("module %q\n\ngo %s\n", node.Module, node.Mod.Go))
	}
	return node.ModBytes
}

func (g *Graph) readRoot(root string) (*Node, error) {
	if g.roleRoot == nil {
		return g.readNode(root, false, "")
	}
	source := g.roleRoot
	// Only the acquired Go module root owns transport metadata. An embedded
	// source crate retains the archive boundary and provenance, but cannot own
	// a nested go.mod: Go module archives exclude nested modules.
	transportRoot := source.source.Kind == "go" && source.CheckoutRoot != "" && filepath.Clean(root) == filepath.Clean(source.CheckoutRoot)
	if !transportRoot && source.SelectedVersion != "" && source.source.Kind == "path" {
		var err error
		transportRoot, err = localModuleTransport(root)
		if err != nil {
			return nil, err
		}
	}
	node, err := g.readNode(root, transportRoot, source.CheckoutRoot)
	if node != nil {
		if err == nil && source.Module != "" && node.Module != source.Module {
			if err = g.bindSelectedModule(node, source.Module, source.sourceSelection); err != nil {
				return nil, err
			}
		}
		node.setSource(source.source)
		node.CheckoutRoot, node.SelectedVersion = source.CheckoutRoot, source.SelectedVersion
		node.sourceSelection = source.sourceSelection
	}
	return node, err
}

func (p *rolePlanner) record(g *Graph) (RoleLock, string, string, error) {
	nodes := map[string]*Node{}
	for _, node := range g.Nodes {
		nodes[node.SourceKey] = node
		for _, child := range node.Children {
			if procBoundary(child, g.Root) {
				nodes[child.SourceKey] = child
			}
		}
	}
	record := RoleLock{Role: g.Role, Platform: g.Platform, Root: g.Root.lockSourceKey(p.owner.ownerRoot), Crate: g.Root.Manifest.Lib, Modules: g.portablePlan(p.owner.ownerRoot, g.Plan, nodes), ProcSources: g.portablePlan(p.owner.ownerRoot, g.roleSelections, nodes)}
	if g.roleRoot != nil && g.roleRoot.SelectedVersion != "" {
		selection := g.roleRoot.sourceSelection
		portable := portableSelections(p.owner.ownerRoot, map[string]Selection{g.Root.Module: selection})[g.Root.Module]
		record.RootSelection = &portable
	}
	var contents []string
	for _, key := range keys(nodes) {
		node := nodes[key]
		record.Sources = append(record.Sources, RoleSource{node.lockSourceKey(p.owner.ownerRoot), node.Module, nodeInput(node), keys(node.Features)})
		content, err := p.sourceContent(node)
		if err != nil {
			return RoleLock{}, "", "", err
		}
		contents = append(contents, node.lockSourceKey(p.owner.ownerRoot)+":"+content)
	}
	sort.Slice(record.Sources, func(i, j int) bool { return record.Sources[i].Key < record.Sources[j].Key })
	sort.Strings(contents)
	key := Digest(record)
	return record, key, Digest([]any{record, contents}), nil
}

func (p *rolePlanner) mergeSums(sums string) {
	set := map[string]bool{}
	for _, data := range []string{p.owner.Lock.PlanningSums, sums} {
		for _, line := range strings.Split(data, "\n") {
			if line != "" {
				set[line] = true
			}
		}
	}
	p.owner.Lock.PlanningSums = ""
	if len(set) != 0 {
		p.owner.Lock.PlanningSums = strings.Join(keys(set), "\n") + "\n"
	}
}
