package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/hxsf/goxide/internal/project"
)

func runWorkspace(req CompileRequest, input string, w *project.Workspace, out, stderr io.Writer) error {
	if w.Config.Go != nil {
		return runNativeWorkspace(req, input, w, out, stderr)
	}
	members, e := w.Select(input, req.Packages, req.Workspace)
	if e != nil {
		return e
	}
	if req.Mode == "expand" {
		return fmt.Errorf("project expand is unsupported; use transpile")
	}
	if len(members) > 1 && (req.Mode == "run" || req.Output != "" || req.Bin != "" || req.Example != "" || req.Lib) {
		return fmt.Errorf("run/output/target selection requires exactly one workspace package")
	}
	if e = checkDirectoryCompiler(req.GoTool); e != nil {
		return e
	}
	requested := map[string][]string{}
	for _, f := range strings.FieldsFunc(req.Features, func(r rune) bool { return r == ',' || r == ' ' }) {
		module := ""
		if i := strings.LastIndex(f, "/"); i >= 0 {
			module = f[:i]
			f = f[i+1:]
		} else if len(members) == 1 {
			module = members[0]
		} else {
			return fmt.Errorf("multiple packages require module/feature requests")
		}
		found := false
		for _, m := range members {
			found = found || m == module
		}
		if !found {
			return fmt.Errorf("feature request must refer to selected member %s", module)
		}
		requested[module] = append(requested[module], f)
	}
	release, e := project.LockProject(w.Root)
	if e != nil {
		return e
	}
	defer release()
	graphs, e := project.LoadWorkspaceGraphs(w, members, req.GoTool, requested, !req.NoDefaultFeatures, req.Offline, req.Locked, req.TargetDir)
	if e != nil {
		return e
	}
	tags := strings.FieldsFunc(req.Tags, func(r rune) bool { return r == ',' || r == ' ' })
	for _, g := range graphs {
		flags := projectFlags{graph: g, bin: req.Bin, example: req.Example, targetDir: req.TargetDir, noDefault: req.NoDefaultFeatures, offline: req.Offline, locked: req.Locked, lib: req.Lib, programArgs: req.ProgramArgs}
		if len(members) == 1 && (!req.Workspace || req.Mode == "run" || req.Output != "" || req.Bin != "" || req.Example != "" || req.Lib) {
			if e = runProject(req.Mode, g.Root.Root, req.Output, req.GoTool, tags, flags, out, stderr); e != nil {
				return e
			}
			continue
		}
		m := g.Root.Manifest
		if m.Lib == nil && len(m.Bins) == 0 {
			return fmt.Errorf("workspace member %s has no library or binary target", m.Module)
		}
		available := func(t project.Target) bool {
			for _, f := range t.RequiredFeatures {
				if !g.Root.Features[f] {
					fmt.Fprintf(stderr, "skip %s/%s: requires feature %s\n", g.Root.Module, t.Name, f)
					return false
				}
			}
			return true
		}
		if m.Lib != nil && available(*m.Lib) {
			flags.lib = true
			if e = runProject(req.Mode, g.Root.Root, "", req.GoTool, tags, flags, out, stderr); e != nil {
				return e
			}
			flags.lib = false
		}
		for _, target := range m.Bins {
			if !available(target) {
				continue
			}
			flags.bin = target.Name
			if e = runProject(req.Mode, g.Root.Root, "", req.GoTool, tags, flags, out, stderr); e != nil {
				return e
			}
		}
	}
	return nil
}
