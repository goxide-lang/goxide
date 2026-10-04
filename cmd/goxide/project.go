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
	"runtime"
	"strings"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/scripttrust"
)

type projectFlags struct {
	graph                             *project.Graph
	programArgs                       []string
	features, bin, example, targetDir string
	noDefault, offline, locked, lib   bool
}

func runProject(mode, input, dest, tool string, tags []string, flags projectFlags, out, stderr io.Writer) error {
	if mode == "expand" {
		return fmt.Errorf("project expand is unsupported; use transpile")
	}
	if err := checkDirectoryCompiler(tool); err != nil {
		return err
	}
	var err error
	g := flags.graph
	if g == nil {
		release, e := project.LockProject(input)
		if e != nil {
			return e
		}
		defer release()
		g, err = project.LoadWithTarget(input, tool, strings.FieldsFunc(flags.features, func(r rune) bool { return r == ',' || r == ' ' }), !flags.noDefault, flags.offline, flags.locked, flags.targetDir)
		if err != nil {
			return err
		}
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
	if err = preflightProjectModules(g, flags.targetDir); err != nil {
		return err
	}
	compilerID := projectCompilerID()
	layoutPath, err := project.ResolveTargetDir(g.OwnerRoot(), flags.targetDir)
	if err != nil {
		return err
	}
	for _, node := range g.Nodes {
		if within(layoutPath, node.Root) || node != g.Root && within(node.Root, layoutPath) {
			return fmt.Errorf("target directory overlaps a dependency/source module")
		}
	}
	plans, err := prepareScriptPlans(g, tool, compilerID, tags, mode == "test", layoutPath)
	if err != nil {
		return err
	}
	defer plans.close()
	var policy *scripttrust.Store
	if len(plans.plans) != 0 {
		forbidden := []string{layoutPath}
		for _, node := range g.Nodes {
			forbidden = append(forbidden, node.Root)
		}
		policy, err = scripttrust.Open(forbidden...)
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
	layout, err := project.OpenLayout(g.OwnerRoot(), g.OwnerModule(), flags.targetDir)
	if err != nil {
		return err
	}
	defer layout.Close()
	for node := range plans.plans {
		platform := runtime.GOOS + "-" + runtime.GOARCH
		if _, err = layout.ScriptCache(platform, platform, node.Root, node.Module); err != nil {
			return err
		}
	}
	kind := "bin"
	if flags.lib || (flags.bin == "" && flags.example == "" && len(g.Root.Manifest.Bins) == 0 && g.Root.Manifest.Lib != nil) {
		kind = "lib"
	} else if flags.example != "" {
		kind = "example"
	}
	spec, err := projectUnitSpec(g, target, kind, mode, tags, compilerID)
	if err != nil {
		return err
	}
	unit, err := layout.Begin(spec)
	if err != nil {
		return err
	}
	bundle := unit.Bundle
	if mode == "transpile" && dest != "" {
		bundle, err = filepath.Abs(dest)
		if err != nil {
			return err
		}
		if err = project.NoSymlinks(bundle); err != nil {
			return err
		}
		if err = project.RejectManagedOutput(bundle); err != nil {
			return err
		}
		if _, e := os.Lstat(bundle); !os.IsNotExist(e) {
			return fmt.Errorf("output bundle must not exist")
		}
		for _, n := range g.Nodes {
			if within(n.Root, bundle) {
				return fmt.Errorf("explicit output bundle must be outside source modules")
			}
		}
	}
	if err = os.MkdirAll(filepath.Dir(bundle), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(bundle, 0755); err != nil {
		return err
	}
	// Generation directories are retained; the frontend may remove its own new
	// incomplete module on failure. No generated-source cache hit is claimed.

	generated, err := projectModuleAnchors(g, filepath.Join(bundle, "policy-anchors"))
	if err != nil {
		return err
	}
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
				return fmt.Errorf("goxide dependency %s must declare lib", n.Module)
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
			configuration = project.ManifestProtocol + ":" + project.Digest(struct {
				Manifest             project.Manifest
				Features             []string
				Target               project.Target
				Kind, Compiler, Plan string
			}{n.Manifest, project.Keys(n.Features), selected, kind, compilerID, g.PlanID})
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
		options := frontend.ModuleOptions{VendorMode: n.Manifest.Vendor != nil, ManagedOutputRoot: layout.Root, SourceModuleRoot: n.Root, SourceModuleFile: n.ModBytes, Context: ctx, Configuration: configuration, Tags: tags, Race: mode == "test", OnlyTarget: true, Features: features, EntryFiles: entryFiles, ExcludeDirs: excluded, TargetPackage: targetPackage, GeneratedFiles: artifact.Files}
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
	if err = validateProjectKind(tool, result, flags.lib || (flags.bin == "" && flags.example == "" && len(g.Root.Manifest.Bins) == 0 && g.Root.Manifest.Lib != nil), tags, g.Root.Manifest.Vendor != nil); err != nil {
		return err
	}
	if err = g.Save(); err != nil {
		return err
	}
	if mode == "transpile" {
		_, err = fmt.Fprintln(out, result.Root)
		return err
	}
	moduleMode := "-mod=readonly"
	if g.Root.Manifest.Vendor != nil {
		moduleMode = "-mod=vendor"
	}
	argv := []string{mode, "-buildvcs=false", moduleMode, "-tags=" + strings.Join(tags, ",")}
	if mode == "test" {
		argv = append(argv, "-race")
	}
	managedBinary := mode == "build" && kind != "lib" && dest == ""
	if mode == "build" && kind != "lib" {
		if managedBinary {
			dest = filepath.Join(unit.Intermediate, "executable")
		} else {
			dest, err = filepath.Abs(dest)
			if err != nil {
				return err
			}
			if err = project.NoSymlinks(dest); err != nil {
				return err
			}
			if err = project.RejectManagedOutput(dest); err != nil {
				return err
			}
			if _, e := os.Lstat(dest); !os.IsNotExist(e) {
				return fmt.Errorf("refusing to overwrite existing binary output")
			}
			for _, n := range g.Nodes {
				if within(n.Root, dest) {
					return fmt.Errorf("binary output must be outside source modules")
				}
			}
		}
		argv = append(argv, "-o", dest)
	} else if mode == "build" && dest != "" {
		return fmt.Errorf("lib build checks the package; -o is reserved for binaries")
	}

	argv = append(argv, result.Target)
	if mode == "run" {
		argv = append(argv, flags.programArgs...)
	}
	cmd := exec.CommandContext(ctx, tool, argv...)
	cmd.Dir = result.Root
	cmd.Env = project.GoEnv(true)
	for i := len(cmd.Env) - 1; i >= 0; i-- {
		if strings.HasPrefix(cmd.Env[i], "GOTMPDIR=") {
			cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
		}
	}
	cmd.Env = append(cmd.Env, "GOTMPDIR="+unit.Intermediate)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("generated project %s: %w", mode, err)
	}
	if managedBinary {
		path, e := unit.PublishBinary(dest)
		if e != nil {
			return e
		}
		_, err = fmt.Fprintln(out, path)
		return err
	}
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
	if !info.IsDir() && filepath.Ext(path) != ".gox" && filepath.Ext(path) != ".go" {
		return fmt.Errorf("target must be a package directory or .goxide/.go entry file")
	}
	if !info.IsDir() {
		resolved = filepath.Dir(resolved)
	}
	for p := resolved; p != root; p = filepath.Dir(p) {
		for _, name := range []string{"go.mod", project.ManifestName, project.LegacyManifestName} {
			if _, e := os.Lstat(filepath.Join(p, name)); e == nil {
				return fmt.Errorf("target crosses nested module boundary")
			} else if !os.IsNotExist(e) {
				return e
			}
		}
	}
	return nil
}

// Validate every generated module policy before any generator or frontend runs.
// These shadow modules contain no source code and never become published output.
func preflightProjectModules(g *project.Graph, targetDir string) error {
	layout, e := project.OpenLayout(g.OwnerRoot(), g.OwnerModule(), targetDir)
	if e != nil {
		return e
	}
	defer layout.Close()
	work, e := os.MkdirTemp(layout.Root, ".preflight-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(work)
	_, e = projectModuleAnchors(g, work)
	return e
}

// Policy anchors keep every graph node available to Go before its source is generated.
func projectModuleAnchors(g *project.Graph, work string) (map[string]string, error) {
	if e := os.MkdirAll(work, 0755); e != nil {
		return nil, e
	}
	var e error
	generated := map[string]string{}
	for _, n := range g.Nodes {
		dir := filepath.Join(work, project.Digest(n.Module))
		if e = os.Mkdir(dir, 0755); e != nil {
			return nil, e
		}
		for _, name := range []string{"go.mod", "go.sum"} {
			b, err := os.ReadFile(filepath.Join(n.Root, name))
			if name == "go.mod" {
				b = n.ModBytes
				err = nil
			}
			if name == "go.sum" && n.HasManifest {
				continue
			}
			if os.IsNotExist(err) && name == "go.sum" {
				continue
			}
			if err != nil {
				return nil, err
			}
			if err = os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
				return nil, err
			}
		}
		// Frontend normally normalizes relative replaces while copying. Do the same
		// for this module-only preflight, without touching the original go.mod.
		m, err := n.Mod, error(nil)
		if err != nil {
			return nil, err
		}
		args := []string{"mod", "edit"}
		for _, rep := range m.Replace {
			if n.Manifest.Vendor == nil && rep.New.Version == "" && !filepath.IsAbs(rep.New.Path) {
				old := rep.Old.Path
				if rep.Old.Version != "" {
					old += "@" + rep.Old.Version
				}
				args = append(args, "-replace="+old+"="+filepath.Join(n.Root, rep.New.Path))
			}
		}
		if len(args) > 2 {
			c := exec.Command(g.Tool, args...)
			c.Dir = dir
			c.Env = project.GoEnv(true)
			if b, e := c.CombinedOutput(); e != nil {
				return nil, fmt.Errorf("module preflight: %w: %s", e, b)
			}
		}
		generated[n.Module] = dir
	}
	for _, n := range g.Nodes {
		if e = prepareProjectModule(g, n, generated[n.Module], generated); e != nil {
			return nil, e
		}
	}
	return generated, nil
}

func prepareProjectModule(g *project.Graph, n *project.Node, root string, generated map[string]string) error {
	if n.Manifest.Vendor != nil {
		return project.PrepareVendor(g.Tool, n.Root, root, n.Manifest, g.Lock.Vendor)
	}
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(g.Tool, args...)
		cmd.Dir = root
		cmd.Env = project.GoEnv(g.Offline)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		b, e := cmd.Output()
		if e != nil {
			return nil, fmt.Errorf("Go module operation in %s: %w: %s", n.Module, e, stderr.String())
		}
		return b, nil
	}
	// Go module cache sources are read-only; only the new generated copies may
	// be made writable for Go's module operations.
	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(root, name)
		if _, e := os.Lstat(path); e == nil {
			if e = os.Chmod(path, 0644); e != nil {
				return e
			}
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	mod, e := project.ReadMod(g.Tool, root)
	if e != nil {
		return e
	}
	// Discard dependency-local policy; root replace/exclude is authoritative. Keep
	// the acquired-source boundary check even for directives Go would ignore.
	edits := []string{"mod", "edit"}
	for _, r := range mod.Replace {
		if n.CheckoutRoot != "" && r.New.Version == "" {
			resolved, err := filepath.EvalSymlinks(r.New.Path)
			if err != nil || !within(n.CheckoutRoot, resolved) {
				return fmt.Errorf("acquired dependency go.mod replace escapes checkout/source boundary: %s", r.Old.Path)
			}
		}
		old := r.Old.Path
		if r.Old.Version != "" {
			old += "@" + r.Old.Version
		}
		edits = append(edits, "-dropreplace="+old)
	}
	for _, x := range mod.Exclude {
		edits = append(edits, "-dropexclude="+x.Path+"@"+x.Version)
	}
	for _, x := range g.RootMod.Exclude {
		edits = append(edits, "-exclude="+x.Path+"@"+x.Version)
	}
	if n != g.Root {
		anchor := generated[g.Root.Module]
		if anchor == "" {
			return fmt.Errorf("missing main-module policy anchor")
		}
		edits = append(edits, "-replace="+g.Root.Module+"="+anchor)
	}
	for _, module := range project.Keys(g.Plan) {
		if module == n.Module {
			continue
		}
		s := g.Plan[module]
		edits = append(edits, "-require="+module+"@"+s.Version)
		if output := generated[module]; output != "" {
			edits = append(edits, "-replace="+module+"="+output)
		} else if s.Replace != nil {
			replacement := s.Replace.Path
			if s.Replace.Version != "" {
				replacement += "@" + s.Replace.Version
			}
			edits = append(edits, "-replace="+module+"="+replacement)
		}
	}
	if len(edits) > 2 {
		if _, e = run(edits...); e != nil {
			return e
		}
	}
	sourceMod, e := n.ModBytes, error(nil)
	if e != nil {
		return e
	}
	key := project.Digest(struct {
		Module   string
		Manifest project.Manifest
		Mod      string
		Sources  map[string]project.Dependency
		Plan     string
	}{n.Module, n.Manifest, string(sourceMod), n.Sources, g.PlanID})
	if g.Lock.Unified != nil {
		if e = os.WriteFile(filepath.Join(root, "go.sum"), []byte(g.Lock.PlanningSums), 0644); e != nil {
			return e
		}
	} else if sums, ok := g.Lock.Sums[key]; ok {
		if e = os.WriteFile(filepath.Join(root, "go.sum"), []byte(sums), 0644); e != nil {
			return e
		}
	} else if g.Locked {
		return fmt.Errorf("--locked: module configuration absent from lock: %s", n.Module)
	}
	if _, e = run("mod", "download", "all"); e != nil {
		return e
	}
	list, e := run("list", "-m", "-json", "all")
	if e != nil {
		return e
	}
	decoder := json.NewDecoder(bytes.NewReader(list))
	for decoder.More() {
		var m struct {
			Path, Version string
			Main          bool
			Replace       *struct{ Path, Version string }
		}
		if e = decoder.Decode(&m); e != nil {
			return e
		}
		if m.Main {
			continue
		}
		if m.Path == g.Root.Module && n != g.Root {
			if m.Replace == nil || m.Replace.Path != generated[g.Root.Module] || m.Replace.Version != "" {
				return fmt.Errorf("main-module policy anchor mismatch")
			}
			continue
		}
		want, ok := g.Plan[m.Path]
		if !ok || m.Version != want.Version {
			return fmt.Errorf("generated Go build list disagrees with stable plan for %s", m.Path)
		}
		replacement, version := "", ""
		if want.Replace != nil {
			replacement = want.Replace.Path
			version = want.Replace.Version
		}
		if output := generated[m.Path]; output != "" {
			replacement = output
			version = ""
		}
		if replacement == "" {
			if m.Replace != nil {
				return fmt.Errorf("unexpected replacement outside stable plan: %s", m.Path)
			}
		} else if m.Replace == nil || m.Replace.Path != replacement || m.Replace.Version != version {
			return fmt.Errorf("generated replacement disagrees with stable plan: %s", m.Path)
		}
	}
	sums, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	if g.Lock.Unified != nil {
		allowed := map[string]bool{}
		for _, line := range strings.Split(g.Lock.PlanningSums, "\n") {
			allowed[line] = true
		}
		for _, line := range strings.Split(string(sums), "\n") {
			if !allowed[line] {
				return fmt.Errorf("unified lock checksum missing for %s", n.Module)
			}
		}
		return nil
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
		selectedDir = filepath.Join("goxide_targets", kind, selected.Name)
		if kind == "lib" {
			selectedDir = "."
		}
		files[filepath.Clean(selected.Path)] = filepath.Join(selectedDir, filepath.Base(selected.Path))
	}
	if _, e := os.Stat(filepath.Join(n.Root, "goxide_targets")); e == nil {
		return nil, nil, "", fmt.Errorf("goxide_targets is reserved for generated project entries")
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
func validateProjectKind(tool string, result frontend.ModuleResult, lib bool, tags []string, vendor bool) error {
	moduleMode := "-mod=readonly"
	if vendor {
		moduleMode = "-mod=vendor"
	}
	cmd := exec.Command(tool, "list", "-buildvcs=false", moduleMode, "-tags="+strings.Join(tags, ","), "-f", "{{.Name}}", result.Target)
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

// projectUnitSpec is configuration identity, not a source-content cache key.
// Every invocation gets a fresh generation beneath the stable unit directory.
func projectUnitSpec(g *project.Graph, target project.Target, kind, mode string, tags []string, compiler string) (project.UnitSpec, error) {
	cmd := exec.Command(g.Tool, "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM", "CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS", "PKG_CONFIG")
	cmd.Env = project.GoEnv(true)
	data, e := cmd.Output()
	if e != nil {
		return project.UnitSpec{}, fmt.Errorf("read target Go environment: %w", e)
	}
	environment := map[string]string{}
	if e = json.Unmarshal(data, &environment); e != nil {
		return project.UnitSpec{}, e
	}
	// Keep the frontend's existing host-only semantic guarantee. A separate
	// compiler change is required before cross-target sizes can be trusted.
	if environment["GOOS"] != runtime.GOOS || environment["GOARCH"] != runtime.GOARCH {
		return project.UnitSpec{}, fmt.Errorf("project compilation requires host GOOS/GOARCH; cross-target type sizes are unsupported")
	}
	type nodeConfig struct {
		Module, Root, GoMod string
		Manifest            project.Manifest
		Features            []string
	}
	nodes := []nodeConfig{}
	commits := map[string]string{}
	for _, n := range g.Nodes {
		mod, e := n.ModBytes, error(nil)
		if e != nil {
			return project.UnitSpec{}, e
		}
		nodes = append(nodes, nodeConfig{n.Module, n.Root, string(mod), n.Manifest, project.Keys(n.Features)})
		for _, d := range n.Sources {
			if d.Git != "" {
				key := project.Digest([]string{d.Git, d.Rev, d.Subdir, d.Module})
				commits[key] = g.Lock.Git[key]
			}
		}
	}
	config := project.Digest(struct {
		Nodes                []nodeConfig
		Commits, Environment map[string]string
		Tags                 []string
		Target               project.Target
		Kind, Compiler, Plan string
		Race                 bool
	}{nodes, commits, environment, tags, target, kind, compiler, g.PlanID, mode == "test"})
	return project.UnitSpec{Host: runtime.GOOS + "-" + runtime.GOARCH, Target: environment["GOOS"] + "-" + environment["GOARCH"], Kind: kind, Name: target.Name, Config: config}, nil
}
