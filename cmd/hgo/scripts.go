package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/buildscript"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/scripttrust"
)

type scriptPlans struct {
	identity string
	plans    map[*project.Node]*buildscript.Plan
	subjects []buildscript.Subject
}

func projectTrustIdentity(root, module string) (string, error) {
	root, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", e
	}
	root, e = filepath.Abs(root)
	if e != nil {
		return "", e
	}
	// Repository identity excludes HEAD: project trust deliberately survives edits
	// and branch updates. Configuration/remotes and worktree identity remain bound.
	repositoryRoot := root
	for {
		if _, e := os.Lstat(filepath.Join(repositoryRoot, ".git")); e == nil {
			break
		} else if !os.IsNotExist(e) {
			return "", e
		}
		parent := filepath.Dir(repositoryRoot)
		if parent == repositoryRoot {
			repositoryRoot = ""
			break
		}
		repositoryRoot = parent
	}
	gitdir := filepath.Join(repositoryRoot, ".git")
	var repository []byte
	if info, e := os.Lstat(gitdir); repositoryRoot != "" && e == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("script trust refuses symlink .git")
		}
		if !info.IsDir() {
			data, e := os.ReadFile(gitdir)
			if e != nil {
				return "", e
			}
			repository = append(repository, data...)
			if strings.HasPrefix(string(data), "gitdir:") {
				gitdir = strings.TrimSpace(strings.TrimPrefix(string(data), "gitdir:"))
				if !filepath.IsAbs(gitdir) {
					gitdir = filepath.Join(repositoryRoot, gitdir)
				}
				if common, e := os.ReadFile(filepath.Join(gitdir, "commondir")); e == nil {
					gitdir = filepath.Join(gitdir, strings.TrimSpace(string(common)))
				}
			}
		}
		if config, e := os.ReadFile(filepath.Join(gitdir, "config")); e == nil {
			repository = append(repository, config...)
		}
	}
	return project.Digest(struct {
		Root, Module string
		Repository   []byte
	}{root, module, repository}), nil
}
func prepareScriptPlans(g *project.Graph, tool, compiler string, tags []string, race bool) (*scriptPlans, error) {
	p := &scriptPlans{plans: map[*project.Node]*buildscript.Plan{}}
	var e error
	ids := map[string]buildscript.Subject{}
	for _, n := range g.Nodes {
		if n.Manifest.Build == nil {
			continue
		}
		if p.identity == "" {
			p.identity, e = projectTrustIdentity(g.Root.Root, g.Root.Module)
			if e != nil {
				return nil, e
			}
		}
		// A Git source cannot use the host dependency/tool escape hatch to reach
		// outside its verified checkout. Local roots/path packages require explicit grants.
		if n.CheckoutRoot != "" {
			paths := []string{}
			for _, v := range n.Manifest.Build.Dependencies {
				paths = append(paths, v)
			}
			for _, v := range n.Manifest.Build.Tools {
				paths = append(paths, v)
			}
			for _, v := range paths {
				if !filepath.IsAbs(v) {
					v = filepath.Join(n.Root, v)
				}
				v, e = filepath.EvalSymlinks(v)
				if e != nil {
					return nil, e
				}
				if !within(n.CheckoutRoot, v) {
					return nil, fmt.Errorf("build script host input escapes Git checkout: %s", v)
				}
			}
		}
		// Git checkout directories are keyed by the locked source selector; pin
		// the actual commit as well, including revisions with identical script bytes.
		commit := ""
		if n.CheckoutRoot != "" {
			commit = g.Lock.Git[filepath.Base(n.CheckoutRoot)]
		}
		plan, e := buildscript.Prepare(buildscript.Options{ProjectRoot: g.Root.Root, PackageRoot: n.Root, Module: n.Module, SourceID: project.Digest([]string{n.Module, n.Root, n.CheckoutRoot, commit}), GoTool: tool, CompilerID: compiler, Root: n == g.Root, Boundary: n.CheckoutRoot, Config: *n.Manifest.Build, Features: project.Keys(n.Features), Tags: tags, Race: race})
		if e != nil {
			return nil, fmt.Errorf("build script %s: %w", n.Module, e)
		}
		p.plans[n] = plan
		for _, subject := range plan.Subjects {
			ids[subject.ID] = subject
		}
	}
	for _, id := range project.Keys(ids) {
		p.subjects = append(p.subjects, ids[id])
	}
	return p, nil
}
func (p *scriptPlans) required() []string {
	ids := make([]string, 0, len(p.subjects))
	for _, s := range p.subjects {
		ids = append(ids, s.ID)
	}
	return ids
}
func (p *scriptPlans) close() {
	for _, plan := range p.plans {
		_ = plan.Close()
	}
}
func (p *scriptPlans) execute(ctx context.Context, n *project.Node, store *scripttrust.Store, log io.Writer) (buildscript.Artifact, error) {
	plan := p.plans[n]
	if plan == nil {
		return buildscript.Artifact{}, nil
	}
	authorize := func() error { return store.Authorize(p.identity, p.required()) }
	if e := authorize(); e != nil {
		return buildscript.Artifact{}, e
	}
	fmt.Fprintln(log, "build-script unconfined: declared-input cache; offline controls dependency fetching, not script network; cancellation does not guarantee descendant termination")
	a, e := plan.Execute(ctx, authorize, log)
	if e != nil {
		return a, e
	}
	state := "run"
	if a.CacheHit {
		state = "cache hit"
	}
	fmt.Fprintf(log, "build-script %s %s\n", state, n.Module)
	return a, nil
}
func runScripts(args []string, out, stderr io.Writer) error {
	f := flag.NewFlagSet("scripts", flag.ContinueOnError)
	f.SetOutput(stderr)
	action := f.String("action", "plan", "plan|trust-project|allow|revoke|forget-project|clean")
	subject := f.String("subject", "", "exact host execution subject from plan")
	tool := f.String("go", "go", "Go 1.27+ compiler")
	features := f.String("features", "", "project features")
	defaults := f.Bool("no-default-features", false, "disable root default features")
	offline := f.Bool("offline", false, "resolve only cached dependencies; does not sandbox scripts")
	locked := f.Bool("locked", false, "require existing unchanged dependency lock")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 1 {
		return fmt.Errorf("usage: hgo scripts [flags] PROJECT")
	}
	valid := map[string]bool{"plan": true, "trust-project": true, "allow": true, "revoke": true, "forget-project": true, "clean": true}
	if !valid[*action] {
		return fmt.Errorf("unknown scripts action %q", *action)
	}
	if *subject != "" && *action != "allow" && *action != "revoke" {
		return fmt.Errorf("--subject requires allow or revoke")
	}
	root, e := filepath.Abs(f.Arg(0))
	if e != nil {
		return e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(root, project.ManifestName)); e != nil {
		return fmt.Errorf("scripts requires hgo.json: %w", e)
	}
	release, e := project.LockProject(root)
	if e != nil {
		return e
	}
	defer release()
	if *action == "clean" {
		if e = buildscript.Clean(root); e != nil {
			return e
		}
		fmt.Fprintln(out, "cleaned owned build-script cache; local trust unchanged")
		return nil
	}
	if e = checkDirectoryCompiler(*tool); e != nil {
		return e
	}
	module, e := project.ModulePath(*tool, root)
	if e != nil {
		return e
	}
	identity, e := projectTrustIdentity(root, module)
	if e != nil {
		return e
	}
	store, e := scripttrust.Open()
	if e != nil {
		return e
	}
	defer store.Close()
	switch *action {
	case "trust-project":
		store.Projects[identity] = true
		if e = store.Save(); e == nil {
			fmt.Fprintln(out, "trusted root project (unconfined); third-party host execution closures still require exact permission")
		}
		return e
	case "forget-project":
		delete(store.Projects, identity)
		if e = store.Save(); e == nil {
			fmt.Fprintln(out, "project trust revoked; cached script artifacts require authorization before reuse")
		}
		return e
	case "revoke":
		if !scripttrust.ValidSubject(*subject) {
			return fmt.Errorf("--subject must be an exact SHA-256 from plan")
		}
		delete(store.Subjects, *subject)
		if e = store.Save(); e == nil {
			fmt.Fprintln(out, "host execution permission revoked")
		}
		return e
	}
	g, e := project.Load(root, *tool, strings.FieldsFunc(*features, func(r rune) bool { return r == ',' || r == ' ' }), !*defaults, *offline, *locked)
	if e != nil {
		return e
	}
	plans, e := prepareScriptPlans(g, *tool, projectCompilerID(), nil, false)
	if e != nil {
		return e
	}
	defer plans.close()
	if *action == "allow" {
		found := false
		for _, s := range plans.subjects {
			if s.ID == *subject {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("subject does not match the current frozen host execution closure; inspect scripts plan again")
		}
		store.Subjects[*subject] = true
		if e = store.Save(); e == nil {
			fmt.Fprintln(out, "permitted exact host execution closure (unconfined)")
		}
		return e
	}
	type shownSubject struct {
		ID          string `json:"id"`
		Description string `json:"description"`
		Trusted     bool   `json:"trusted"`
	}
	shown := []shownSubject{}
	for _, s := range plans.subjects {
		shown = append(shown, shownSubject{s.ID, s.Description, store.Subjects[s.ID]})
	}
	sort.Slice(shown, func(i, j int) bool { return shown[i].ID < shown[j].ID })
	return json.NewEncoder(out).Encode(struct {
		RootIdentity string         `json:"root_identity"`
		RootTrusted  bool           `json:"root_trusted"`
		Subjects     []shownSubject `json:"subjects"`
		Mode         string         `json:"mode"`
	}{plans.identity, store.Projects[plans.identity], shown, "unconfined; declared-input incremental cache"})
}
