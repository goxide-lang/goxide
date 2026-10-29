package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/buildscript"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/scripttrust"
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
func scriptSourceRoots(root string, manifest project.Manifest) []string {
	roots := []string{root}
	if manifest.Build != nil {
		for _, helper := range manifest.Build.Dependencies {
			if !filepath.IsAbs(helper) {
				helper = filepath.Join(root, helper)
			}
			roots = append(roots, helper)
		}
	}
	return roots
}

func prepareScriptPlans(g *project.Graph, tool, compiler string, tags []string, race bool, layoutRoot string) (*scriptPlans, error) {
	p := &scriptPlans{plans: map[*project.Node]*buildscript.Plan{}}
	var e error
	hasScripts := false
	for _, n := range g.Nodes {
		hasScripts = hasScripts || n.Manifest.Build != nil
	}
	if hasScripts {
		for _, n := range g.Nodes {
			if err := scripttrust.CheckLocation(scriptSourceRoots(n.Root, n.Manifest)...); err != nil {
				return nil, err
			}
		}
	}
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
					return nil, fmt.Errorf("build script host input escapes acquired source boundary: %s", v)
				}
			}
		}
		// Git checkout directories are keyed by the locked source selector; pin
		// the actual commit as well, including revisions with identical script bytes.
		commit := ""
		if n.CheckoutRoot != "" {
			commit = g.Lock.Git[filepath.Base(n.CheckoutRoot)]
		}
		roots, e := projectHostRoots(n)
		if e != nil {
			return nil, e
		}
		plan, e := buildscript.Prepare(buildscript.Options{ProjectRoot: g.Root.Root, PackageRoot: n.Root, Module: n.Module, SourceID: project.Digest([]string{n.Module, n.Root, n.CheckoutRoot, commit, n.SourceID, g.PlanID}), GoTool: tool, CompilerID: compiler, Root: n == g.Root, Boundary: n.CheckoutRoot, ArtifactRoot: project.ScriptCachePath(layoutRoot, runtime.GOOS+"-"+runtime.GOARCH, runtime.GOOS+"-"+runtime.GOARCH, n.Root, n.Module), Config: *n.Manifest.Build, ToolchainRoots: roots, Features: project.Keys(n.Features), Tags: tags, Race: race})
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

// executeScripts performs only the explicitly selected action. In particular,
// zero-value requests cannot grant trust or default to a mutating action.
func executeScripts(req ScriptsRequest, out, stderr io.Writer) error {
	if req.Root == "" {
		return fmt.Errorf("usage: goxide scripts [ACTION] [flags] PROJECT")
	}
	valid := map[string]bool{"plan": true, "trust-project": true, "allow": true, "revoke": true, "forget-project": true, "clean": true}
	if !valid[req.Action] {
		return fmt.Errorf("unknown scripts action %q", req.Action)
	}
	if req.Subject != "" && req.Action != "allow" && req.Action != "revoke" {
		return fmt.Errorf("--subject requires allow or revoke")
	}
	root, e := filepath.Abs(req.Root)
	if e != nil {
		return e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return e
	}
	if exists, err := project.Detect(root); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("scripts requires goxide.toml")
	}
	release, e := project.LockProject(root)
	if e != nil {
		return e
	}
	defer release()
	ownerRoot, ownerModule := root, ""
	if w, err := project.DiscoverWorkspace(root); err != nil {
		return err
	} else if w != nil {
		ownerRoot, ownerModule = w.Root, project.WorkspaceOwner
	}
	layoutPath, e := project.ResolveTargetDir(ownerRoot, req.TargetDir)
	if e != nil {
		return e
	}
	if e = scripttrust.CheckLocation(root, layoutPath); e != nil {
		return e
	}
	if e = checkDirectoryCompiler(req.GoTool); e != nil {
		return e
	}
	module, e := project.ModulePath(req.GoTool, root)
	if e != nil {
		return e
	}
	if req.Action == "clean" {
		if _, e := os.Lstat(layoutPath); os.IsNotExist(e) {
			fmt.Fprintln(out, "no script cache; local trust unchanged")
			return nil
		}
		if ownerModule == "" {
			ownerModule = module
		}
		layout, e := project.OpenLayout(ownerRoot, ownerModule, req.TargetDir)
		if e != nil {
			return e
		}
		defer layout.Close()
		if e = layout.CleanScriptCaches(func(path string) error { return buildscript.Clean(root, path) }); e != nil {
			return e
		}
		fmt.Fprintln(out, "cleaned owned build-script cache; local trust and retained target outputs unchanged")
		return nil
	}
	identity, e := projectTrustIdentity(root, module)
	if e != nil {
		return e
	}
	forbidden := []string{root, layoutPath}
	// Revocation must remain available even if the current manifest is broken.
	if req.Action != "revoke" && req.Action != "forget-project" {
		manifest, err := project.Read(root)
		if err != nil {
			return err
		}
		forbidden = append(forbidden, scriptSourceRoots(root, manifest)...)
	}
	store, e := scripttrust.Open(forbidden...)
	if e != nil {
		return e
	}
	defer store.Close()
	switch req.Action {
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
		if !scripttrust.ValidSubject(req.Subject) {
			return fmt.Errorf("--subject must be an exact SHA-256 from plan")
		}
		delete(store.Subjects, req.Subject)
		if e = store.Save(); e == nil {
			fmt.Fprintln(out, "host execution permission revoked")
		}
		return e
	}
	g, e := project.LoadWithTarget(root, req.GoTool, strings.FieldsFunc(req.Features, func(r rune) bool { return r == ',' || r == ' ' }), !req.NoDefaultFeatures, req.Offline, req.Locked, req.TargetDir)
	if e != nil {
		return e
	}
	plans, e := prepareScriptPlans(g, req.GoTool, projectCompilerID(), nil, false, layoutPath)
	if e != nil {
		return e
	}
	defer plans.close()
	if req.Action == "allow" {
		found := false
		for _, s := range plans.subjects {
			if s.ID == req.Subject {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("subject does not match the current frozen host execution closure; inspect scripts plan again")
		}
		store.Subjects[req.Subject] = true
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
	}{identity, store.Projects[identity], shown, "unconfined; declared-input incremental cache"})
}
