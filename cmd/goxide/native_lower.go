package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/scripttrust"
)

func nativeFeatureRequests(features string, members []string) (map[string][]string, error) {
	result := map[string][]string{}
	for _, f := range strings.FieldsFunc(features, func(r rune) bool { return r == ',' || r == ' ' }) {
		module := ""
		if i := strings.LastIndex(f, "/"); i >= 0 {
			module, f = f[:i], f[i+1:]
		} else if len(members) == 1 {
			module = members[0]
		} else {
			return nil, fmt.Errorf("multiple packages require module/feature requests")
		}
		found := false
		for _, m := range members {
			found = found || m == module
		}
		if !found {
			return nil, fmt.Errorf("feature request must refer to selected member %s", module)
		}
		result[module] = append(result[module], f)
	}
	return result, nil
}

// Lower into the already-owned native view. Its unchanged work policy is used
// throughout import/type checking; only explicitly reachable sources exist.
func lowerNativeView(ctx context.Context, w *project.Workspace, g *project.Graph, dirs map[string]string, bundle string, selected project.Target, kind string, race bool, layout *project.Layout, log io.Writer) (string, error) {
	// Qualify every enhanced target before preparing or executing any script.
	for _, n := range g.Nodes {
		enhanced, e := project.NativeEnhanced(dirs[n.Module], n.Manifest)
		if e != nil {
			return "", e
		}
		if !enhanced {
			continue
		}
		target, targetKind := selected, kind
		if n != g.Root {
			if n.Manifest.Lib == nil {
				return "", fmt.Errorf("enhanced native dependency %s requires lib", n.Module)
			}
			target = *n.Manifest.Lib
			targetKind = "lib"
		}
		if _, _, _, e = projectLayout(n, target, targetKind); e != nil {
			return "", e
		}
	}
	plans, e := prepareScriptPlans(g, g.Tool, projectCompilerID(), nil, race, layout.Root)
	if e != nil {
		return "", e
	}
	defer plans.close()
	var policy *scripttrust.Store
	if len(plans.plans) > 0 {
		forbidden := []string{layout.Root}
		for _, n := range g.Nodes {
			forbidden = append(forbidden, n.Root)
		}
		policy, e = scripttrust.Open(forbidden...)
		if e != nil {
			return "", e
		}
		defer policy.Close()
		if e = policy.Authorize(plans.identity, plans.required()); e != nil {
			return "", e
		}
		platform := runtime.GOOS + "-" + runtime.GOARCH
		for n := range plans.plans {
			if _, e = layout.ScriptCache(platform, platform, n.Root, n.Module); e != nil {
				return "", e
			}
		}
	}
	targetPath := filepath.Join(dirs[g.Root.Module], selected.Path)
	for _, n := range g.Nodes {
		output := dirs[n.Module]
		lower, e := project.NativeEnhanced(output, n.Manifest)
		if e != nil {
			return "", e
		}
		if !lower {
			continue
		}
		target, targetKind := selected, kind
		if n != g.Root {
			targetKind = "lib"
			if n.Manifest.Lib == nil {
				return "", fmt.Errorf("enhanced native dependency %s requires lib", n.Module)
			}
			target = *n.Manifest.Lib
		}
		files, excluded, targetPackage, e := projectLayout(n, target, targetKind)
		if e != nil {
			return "", e
		}
		artifact, e := plans.execute(ctx, n, policy, log)
		if e != nil {
			return "", e
		}
		if n.Manifest.Build != nil {
			excluded[filepath.Clean(filepath.Dir(n.Manifest.Build.Entry))] = true
		}
		mod, e := os.ReadFile(filepath.Join(output, "go.mod"))
		if e != nil {
			return "", e
		}
		sums, e := os.ReadFile(filepath.Join(output, "go.sum"))
		if e != nil && !os.IsNotExist(e) {
			return "", e
		}
		if e = os.RemoveAll(output); e != nil {
			return "", e
		}
		features := map[string]bool{}
		for f := range n.Manifest.Features {
			features[f] = n.Features[f]
		}
		config := project.Digest([]any{"native-enhanced-v1", g.PlanID, n.Manifest, project.Keys(n.Features), target, targetKind, race, artifact.Digest, projectCompilerID()})
		options := frontend.ModuleOptions{WorkspaceFile: filepath.Join(bundle, "go.work"), ModuleMode: project.NativeMode(w), Context: ctx, Environment: project.NativeGoEnv(filepath.Join(bundle, "go.work"), true), SourceModuleRoot: n.Root, SourceModuleFile: mod, ManagedOutputRoot: layout.Root, Features: features, Race: race, OnlyTarget: true, EntryFiles: files, ExcludeDirs: excluded, TargetPackage: targetPackage, GeneratedFiles: artifact.Files, Configuration: config}
		options.Prepare = func(dir string) error {
			if e := os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0644); e != nil {
				return e
			}
			if len(sums) > 0 {
				return os.WriteFile(filepath.Join(dir, "go.sum"), sums, 0644)
			}
			return nil
		}
		result, e := frontend.CompileModuleWithOptions(n.Root, output, options)
		if e != nil {
			return "", e
		}
		if n == g.Root {
			targetPath = filepath.Join(result.Root, result.Target)
		}
	}
	if policy != nil {
		if e = policy.Authorize(plans.identity, plans.required()); e != nil {
			return "", e
		}
	}
	return targetPath, nil
}
