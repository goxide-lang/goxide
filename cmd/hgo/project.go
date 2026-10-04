package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/scripttrust"
)

type projectFlags struct {
	features, bin, example          string
	noDefault, offline, locked, lib bool
}

func runProject(mode, input, dest, tool string, tags []string, flags projectFlags, out, stderr io.Writer) error {
	if mode == "expand" {
		return fmt.Errorf("project expand is unsupported; use transpile")
	}
	if err := checkDirectoryCompiler(tool); err != nil {
		return err
	}
	release, err := project.LockProject(input)
	if err != nil {
		return err
	}
	defer release()
	g, err := project.Load(input, tool, strings.FieldsFunc(flags.features, func(r rune) bool { return r == ',' || r == ' ' }), !flags.noDefault, flags.offline, flags.locked)
	if err != nil {
		return err
	}
	var target project.Target
	if flags.lib || (flags.bin == "" && flags.example == "" && len(g.Root.Manifest.Bins) == 0 && g.Root.Manifest.Lib != nil) {
		if flags.bin != "" || flags.example != "" {
			return fmt.Errorf("--lib conflicts with --bin/--example")
		}
		if g.Root.Manifest.Lib == nil {
			return fmt.Errorf("project has no lib")
		}
		target = *g.Root.Manifest.Lib
		if mode == "run" {
			return fmt.Errorf("a lib cannot be run; select --bin or --example")
		}
		for _, f := range target.RequiredFeatures {
			if !g.Root.Features[f] {
				return fmt.Errorf("lib requires feature %s", f)
			}
		}
	} else {
		target, err = project.Select(g.Root.Manifest, flags.bin, flags.example, g.Root.Features)
		if err != nil {
			return err
		}
	}
	targetInput := g.Root.Root
	compilerID := projectCompilerID()
	plans, err := prepareScriptPlans(g, tool, compilerID, tags, mode == "test")
	if err != nil {
		return err
	}
	defer plans.close()
	var policy *scripttrust.Store
	if len(plans.plans) != 0 {
		policy, err = scripttrust.Open()
		if err != nil {
			return err
		}
		defer policy.Close()
		if err = policy.Authorize(plans.identity, plans.required()); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var bundle string
	if mode == "transpile" {
		if dest == "" {
			return fmt.Errorf("project transpile requires -o with a fresh bundle directory")
		}
		bundle, err = filepath.Abs(dest)
		if err != nil {
			return err
		}
		if _, e := os.Lstat(bundle); !os.IsNotExist(e) {
			return fmt.Errorf("output bundle must not exist")
		}
		for _, n := range g.Nodes {
			if within(n.Root, bundle) {
				return fmt.Errorf("output bundle must be outside every source module")
			}
		}
		if err = os.MkdirAll(bundle, 0755); err != nil {
			return err
		}
	} else {
		bundle, err = os.MkdirTemp("", "hgo-project-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(bundle)
	}
	success := false
	defer func() {
		if !success && mode == "transpile" {
			os.RemoveAll(bundle)
		}
	}()
	generated := map[string]string{}
	var result frontend.ModuleResult
	for _, n := range g.Nodes {
		inputPath := n.Root
		selected := target
		kind := "bin"
		if n != g.Root {
			if n.Manifest.Lib != nil {
				selected = *n.Manifest.Lib
				kind = "lib"
			} else if _, e := os.Stat(filepath.Join(n.Root, project.ManifestName)); e == nil {
				return fmt.Errorf("hgo dependency %s must declare lib", n.Module)
			} else {
				selected = project.Target{Name: "native", Path: "."}
				kind = "lib"
			}
		} else if flags.lib || (flags.bin == "" && flags.example == "" && len(n.Manifest.Bins) == 0 && n.Manifest.Lib != nil) {
			kind = "lib"
		} else if flags.example != "" {
			kind = "example"
		}
		for _, f := range selected.RequiredFeatures {
			if !n.Features[f] {
				return fmt.Errorf("target %s requires feature %s", selected.Name, f)
			}
		}
		entryFiles, excluded, targetPackage, e := projectLayout(n, selected, kind)
		if e != nil {
			return e
		}
		if n == g.Root {
			inputPath = targetInput
		}

		features := map[string]bool{}
		for f := range n.Manifest.Features {
			features[f] = n.Features[f]
		}
		output := filepath.Join(bundle, "deps", project.Digest(n.Module)[:16])
		if n == g.Root {
			output = filepath.Join(bundle, "module")
		}
		configuration := ""
		if n.HasManifest {
			configuration = project.Digest(struct {
				Manifest       project.Manifest
				Features       []string
				Target         project.Target
				Kind, Compiler string
			}{n.Manifest, project.Keys(n.Features), selected, kind, compilerID})
		} else {
			features = nil
		}
		artifact, e := plans.execute(ctx, n, policy, stderr)
		if e != nil {
			return e
		}
		if n.Manifest.Build != nil {
			excluded[filepath.Clean(filepath.Dir(n.Manifest.Build.Entry))] = true
			configuration = project.Digest([]string{configuration, artifact.Digest})
		}
		options := frontend.ModuleOptions{Context: ctx, Configuration: configuration, Tags: tags, Race: mode == "test", OnlyTarget: true, Features: features, EntryFiles: entryFiles, ExcludeDirs: excluded, TargetPackage: targetPackage, GeneratedFiles: artifact.Files}
		options.Prepare = func(root string) error { return prepareProjectModule(g, n, root, generated) }
		result, err = frontend.CompileModuleWithOptions(inputPath, output, options)
		if err != nil {
			return err
		}
		generated[n.Module] = result.Root
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if policy != nil {
		if err = policy.Authorize(plans.identity, plans.required()); err != nil {
			return err
		}
	}
	if err = validateProjectKind(tool, result, flags.lib || (flags.bin == "" && flags.example == "" && len(g.Root.Manifest.Bins) == 0 && g.Root.Manifest.Lib != nil), tags); err != nil {
		return err
	}
	if err = g.Save(); err != nil {
		return err
	}
	if mode == "transpile" {
		success = true
		_, err = fmt.Fprintln(out, result.Root)
		return err
	}
	argv := []string{mode, "-buildvcs=false", "-mod=readonly", "-tags=" + strings.Join(tags, ",")}
	if mode == "test" {
		argv = append(argv, "-race")
	}
	if mode == "build" && !(flags.lib || (len(g.Root.Manifest.Bins) == 0 && g.Root.Manifest.Lib != nil && flags.example == "")) {
		if dest == "" {
			return fmt.Errorf("project build requires -o for binary output")
		}
		dest, err = filepath.Abs(dest)
		if err != nil {
			return err
		}
		if _, e := os.Lstat(dest); !os.IsNotExist(e) {
			return fmt.Errorf("refusing to overwrite existing binary output")
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(dest))
		if e != nil {
			return e
		}
		dest = filepath.Join(parent, filepath.Base(dest))
		for _, n := range g.Nodes {
			if within(n.Root, dest) {
				return fmt.Errorf("binary output must be outside source modules")
			}
		}
		argv = append(argv, "-o", dest)
	} else if mode == "build" && dest != "" {
		return fmt.Errorf("lib build checks the package; -o is reserved for binaries")
	}
	argv = append(argv, result.Target)
	cmd := exec.CommandContext(ctx, tool, argv...)
	cmd.Dir = result.Root
	cmd.Env = project.GoEnv(true)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("generated project %s: %w", mode, err)
	}
	success = true
	return nil
}
func within(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}
func checkTarget(root, path string) error {
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return e
	}
	if !within(root, resolved) {
		return fmt.Errorf("target escapes module")
	}
	info, e := os.Stat(resolved)
	if e != nil {
		return e
	}
	if !info.IsDir() && filepath.Ext(path) != ".hgo" && filepath.Ext(path) != ".go" {
		return fmt.Errorf("target must be a package directory or .hgo/.go entry file")
	}
	if !info.IsDir() {
		resolved = filepath.Dir(resolved)
	}
	for p := resolved; p != root; p = filepath.Dir(p) {
		if _, e := os.Stat(filepath.Join(p, "go.mod")); e == nil {
			return fmt.Errorf("target crosses nested module boundary")
		}
	}
	return nil
}
func prepareProjectModule(g *project.Graph, n *project.Node, root string, generated map[string]string) error {
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(g.Tool, args...)
		cmd.Dir = root
		cmd.Env = project.GoEnv(g.Offline)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, e := cmd.Output()
		if e != nil {
			return nil, fmt.Errorf("Go module operation in %s: %w", n.Module, e)
		}
		return b, nil
	}
	raw, e := run("mod", "edit", "-json")
	if e != nil {
		return e
	}
	var mod struct {
		Require []struct{ Path, Version string }
		Replace []struct {
			Old, New struct{ Path, Version string }
		}
	}
	if e = json.Unmarshal(raw, &mod); e != nil {
		return e
	}
	if n.CheckoutRoot != "" {
		for _, r := range mod.Replace {
			if r.New.Version != "" {
				continue
			}
			resolved, err := filepath.EvalSymlinks(r.New.Path)
			if err != nil {
				return fmt.Errorf("git dependency local replace unavailable: %s", r.Old.Path)
			}
			rel, err := filepath.Rel(n.CheckoutRoot, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return fmt.Errorf("git dependency go.mod replace escapes checkout: %s", r.Old.Path)
			}
		}
	}
	requires := map[string]string{}
	for _, r := range mod.Require {
		requires[r.Path] = r.Version
	}
	applyReplacement := func(module, source, output string) error {
		for _, r := range mod.Replace {
			if r.Old.Path == module {
				if r.New.Version != "" || !samePath(r.New.Path, source) {
					return fmt.Errorf("manifest source conflicts with go.mod replace for %s", module)
				}
				old := r.Old.Path
				if r.Old.Version != "" {
					old += "@" + r.Old.Version
				}
				if _, e := run("mod", "edit", "-dropreplace="+old); e != nil {
					return e
				}
			}
		}
		_, e := run("mod", "edit", "-replace="+module+"="+output)
		return e
	}
	seen := map[string]string{}
	for _, alias := range project.Keys(n.Sources) {
		d := n.Sources[alias]
		version := d.Version
		replacement := ""
		if child := n.Children[alias]; child != nil {
			replacement = generated[child.Module]
			version = requires[d.Module]
			if version == "" {
				version = "v0.0.0"
				parts := strings.Split(d.Module, "/")
				last := parts[len(parts)-1]
				if len(last) > 1 && last[0] == 'v' {
					if major, e := strconv.Atoi(last[1:]); e == nil && major >= 2 {
						version = fmt.Sprintf("v%d.0.0", major)
					}
				}
			}
		}
		identity := version + "|" + replacement
		if prior, ok := seen[d.Module]; ok && prior != identity {
			return fmt.Errorf("conflicting dependency aliases for %s", d.Module)
		}
		seen[d.Module] = identity
		if v := requires[d.Module]; v != "" && v != version {
			return fmt.Errorf("manifest version conflicts with go.mod for %s", d.Module)
		}
		if replacement != "" {
			if e = applyReplacement(d.Module, n.Children[alias].Root, replacement); e != nil {
				return e
			}
		}
		if _, e = run("mod", "edit", "-require="+d.Module+"@"+version); e != nil {
			return e
		}
	}
	// Go ignores replace directives of dependencies. Apply the resolved local
	// source graph in each generated main module, including transitive modules.
	reachable := map[string]*project.Node{}
	var collect func(*project.Node)
	collect = func(node *project.Node) {
		for _, child := range node.Children {
			if reachable[child.Module] == nil {
				reachable[child.Module] = child
				collect(child)
			}
		}
	}
	collect(n)
	for _, module := range project.Keys(reachable) {
		if generated[module] == "" {
			return fmt.Errorf("dependency not generated: %s", module)
		}
		if e = applyReplacement(module, reachable[module].Root, generated[module]); e != nil {
			return e
		}
		if requires[module] == "" {
			version := "v0.0.0"
			parts := strings.Split(module, "/")
			last := parts[len(parts)-1]
			if len(last) > 1 && last[0] == 'v' {
				if major, e := strconv.Atoi(last[1:]); e == nil && major >= 2 {
					version = fmt.Sprintf("v%d.0.0", major)
				}
			}
			if _, e = run("mod", "edit", "-require="+module+"@"+version); e != nil {
				return e
			}
		}
	}
	// Lock sums are scoped to exact source manifests and go.mod, not cwd or
	// temporary generated paths. Retain entries for inactive configurations.
	sourceMod, e := os.ReadFile(filepath.Join(n.Root, "go.mod"))
	if e != nil {
		return e
	}
	key := project.Digest(struct {
		Module   string
		Manifest project.Manifest
		Mod      string
		Sources  map[string]project.Dependency
	}{n.Module, n.Manifest, string(sourceMod), n.Sources})
	if sums, ok := g.Lock.Sums[key]; ok {
		if e = os.WriteFile(filepath.Join(root, "go.sum"), []byte(sums), 0644); e != nil {
			return e
		}
	} else if g.Locked {
		return fmt.Errorf("--locked: module configuration absent from lock: %s", n.Module)
	}
	if _, e = run("mod", "download", "all"); e != nil {
		return e
	}
	// Use Go's selected build list, never a second version resolver. Until a
	// shared planning pass exists, reject inconsistent native graphs before
	// enhanced metadata can cross differently selected dependencies.
	buildList, e := run("list", "-m", "-json", "all")
	if e != nil {
		return e
	}
	if g.NativeSelections == nil {
		g.NativeSelections = map[string]string{}
	}
	local := map[string]bool{}
	for _, node := range g.Nodes {
		local[node.Module] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(buildList))
	for decoder.More() {
		var m struct {
			Path, Version string
			Main          bool
			Replace       *struct{ Path, Version string }
		}
		if e = decoder.Decode(&m); e != nil {
			return e
		}
		if m.Main || local[m.Path] {
			continue
		}
		identity := project.Digest(struct {
			Version string
			Replace any
		}{m.Version, m.Replace})
		if prior, ok := g.NativeSelections[m.Path]; ok && prior != identity {
			return fmt.Errorf("native Go build lists disagree for %s; align versions/replacements across hgo modules (v1)", m.Path)
		}
		g.NativeSelections[m.Path] = identity
	}
	sums, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if g.Locked && g.Lock.Sums[key] != string(sums) {
		return fmt.Errorf("--locked: checksums would change for %s", n.Module)
	}
	g.Lock.Sums[key] = string(sums)
	return nil
}

func projectLayout(n *project.Node, selected project.Target, kind string) (map[string]string, map[string]bool, string, error) {
	files := map[string]string{}
	excluded := map[string]bool{}
	selectedDir := ""
	targets := append(append([]project.Target{}, n.Manifest.Bins...), n.Manifest.Examples...)
	for _, t := range targets {
		p := filepath.Join(n.Root, t.Path)
		if err := checkTarget(n.Root, p); err != nil {
			return nil, nil, "", fmt.Errorf("target %s: %w", t.Name, err)
		}
		info, _ := os.Stat(p)
		rel := filepath.Clean(t.Path)
		if info.IsDir() {
			if rel != "." {
				excluded[rel] = true
			}
		} else {
			files[rel] = ""
		}
	}
	if n.Manifest.Lib != nil {
		lib := n.Manifest.Lib
		p := filepath.Join(n.Root, lib.Path)
		if e := checkTarget(n.Root, p); e != nil {
			return nil, nil, "", e
		}
		info, _ := os.Stat(p)
		if !info.IsDir() {
			files[filepath.Clean(lib.Path)] = filepath.Base(lib.Path)
		}
	}
	path := filepath.Join(n.Root, selected.Path)
	if e := checkTarget(n.Root, path); e != nil {
		return nil, nil, "", e
	}
	info, _ := os.Stat(path)
	if info.IsDir() {
		selectedDir = filepath.Clean(selected.Path)
		delete(excluded, selectedDir)
	} else {
		selectedDir = filepath.Join("hgo_targets", kind, selected.Name)
		if kind == "lib" {
			selectedDir = "."
		}
		files[filepath.Clean(selected.Path)] = filepath.Join(selectedDir, filepath.Base(selected.Path))
	}
	if _, e := os.Stat(filepath.Join(n.Root, "hgo_targets")); e == nil {
		return nil, nil, "", fmt.Errorf("hgo_targets is reserved for generated project entries")
	}
	return files, excluded, selectedDir, nil
}
func projectCompilerID() string {
	exe, e := os.Executable()
	if e != nil {
		return "unknown"
	}
	data, e := os.ReadFile(exe)
	if e != nil {
		return "unknown"
	}
	return project.Digest(data)
}
func validateProjectKind(tool string, result frontend.ModuleResult, lib bool, tags []string) error {
	cmd := exec.Command(tool, "list", "-buildvcs=false", "-mod=readonly", "-tags="+strings.Join(tags, ","), "-f", "{{.Name}}", result.Target)
	cmd.Dir = result.Root
	cmd.Env = project.GoEnv(true)
	b, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("selected target package: %w: %s", e, b)
	}
	if lib && strings.TrimSpace(string(b)) == "main" {
		return fmt.Errorf("lib target must not be package main")
	}
	if !lib && strings.TrimSpace(string(b)) != "main" {
		return fmt.Errorf("bin/example target must be package main")
	}
	return nil
}
