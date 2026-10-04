package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/hxsf/goxide/internal/project"
)

func runNativeWorkspace(req CompileRequest, input string, w *project.Workspace, out, stderr io.Writer) error {
	if req.Mode == "expand" || req.Features != "" || req.NoDefaultFeatures || req.Tags != "" {
		return fmt.Errorf("native workspace supports only its recorded configuration; features/tags/expand require a new analysis")
	}
	if req.Output != "" {
		return fmt.Errorf("native workspace uses managed outputs; -o is not supported")
	}
	members, e := w.Select(input, req.Packages, req.Workspace)
	if e != nil {
		return e
	}
	if len(members) > 1 && (req.Mode == "run" || req.Bin != "" || req.Example != "" || req.Lib) {
		return fmt.Errorf("native workspace run/target selection requires one package")
	}
	if e = checkDirectoryCompiler(req.GoTool); e != nil {
		return e
	}
	config, e := project.NativeConfiguration(req.GoTool)
	if e != nil {
		return e
	}
	if project.Digest(config) != project.Digest(w.Config.Go.Configuration) {
		return fmt.Errorf("native workspace configuration differs (platform/architecture features/Go toolchain); re-analyze migration")
	}
	for module := range w.Members {
		if _, e = project.NativeClosure(w, module); e != nil {
			return e
		}
	}
	release, e := project.LockProject(w.Root)
	if e != nil {
		return e
	}
	defer release()
	targetRoot, e := project.ResolveTargetDir(w.Root, req.TargetDir)
	if e != nil {
		return e
	}
	for _, dir := range w.Members {
		if within(targetRoot, dir) || dir != w.Root && within(dir, targetRoot) {
			return fmt.Errorf("native workspace target overlaps member")
		}
	}
	layout, e := project.OpenLayout(w.Root, project.WorkspaceOwner, req.TargetDir)
	if e != nil {
		return e
	}
	defer layout.Close()
	lockPath := filepath.Join(w.Root, "goxide.lock")
	var lock project.Lock
	original, e := os.ReadFile(lockPath)
	if e == nil {
		if e = json.Unmarshal(original, &lock); e != nil {
			return e
		}
		if lock.Version != 4 || lock.NativeWorkspace == nil {
			return fmt.Errorf("native workspace requires schema 4 lock; explicit migration required")
		}
	} else if !os.IsNotExist(e) {
		return e
	} else if req.Locked {
		return fmt.Errorf("--locked requires native workspace lock")
	} else {
		lock = project.Lock{Version: 4, NativeWorkspace: &project.NativeWorkspaceLock{Sums: map[string]string{}}}
	}
	prior := project.Digest(lock.NativeWorkspace)
	inputID, e := project.NativeInput(w)
	if e != nil {
		return e
	}
	if req.Locked && lock.NativeWorkspace.Input != inputID {
		return fmt.Errorf("--locked: native workspace inputs changed")
	}
	lock.NativeWorkspace.Input = inputID
	if w.Config.Vendor != nil {
		snapshot, err := project.InspectWorkspaceVendor(w)
		if err != nil {
			return err
		}
		if req.Locked && (lock.NativeWorkspace.Vendor == nil || lock.NativeWorkspace.Vendor.Digest != snapshot.Digest) {
			return fmt.Errorf("--locked: workspace vendor content/policy changed")
		}
		lock.NativeWorkspace.Vendor = snapshot
	} else if lock.NativeWorkspace.Vendor != nil {
		return fmt.Errorf("native workspace vendor mode cannot change implicitly")
	}
	planning, e := os.MkdirTemp(layout.Root, ".native-plan-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(planning)
	if _, e = project.NativeView(w, planning, map[string]bool{}, lock.NativeWorkspace); e != nil {
		return e
	}
	if e = project.ReadNativeSelections(w, req.GoTool, planning, lock.NativeWorkspace, req.Offline); e != nil {
		return e
	}
	if req.Locked && project.Digest(lock.NativeWorkspace) != prior {
		return fmt.Errorf("--locked: native workspace versions/checksums changed")
	}
	save := func() error { g := &project.Graph{Lock: lock, LockPath: lockPath}; return g.Save() }
	if !req.Locked {
		if e = save(); e != nil {
			return e
		}
	}
	for _, module := range members {
		m := w.Manifests[w.Members[module]]
		type selectedTarget struct {
			target project.Target
			kind   string
		}
		targets := []selectedTarget{}
		if req.Lib {
			if m.Lib == nil {
				return fmt.Errorf("member has no lib")
			}
			if req.Bin != "" || req.Example != "" {
				return fmt.Errorf("--lib conflicts with --bin/--example")
			}
			targets = append(targets, selectedTarget{*m.Lib, "lib"})
		} else if req.Bin != "" || req.Example != "" || req.Mode == "run" || len(members) == 1 && !req.Workspace {
			if req.Bin == "" && req.Example == "" && len(m.Bins) == 0 && m.Lib != nil {
				targets = append(targets, selectedTarget{*m.Lib, "lib"})
			} else {
				t, err := project.Select(m, req.Bin, req.Example, map[string]bool{})
				if err != nil {
					return err
				}
				kind := "bin"
				if req.Example != "" {
					kind = "example"
				}
				targets = append(targets, selectedTarget{t, kind})
			}
		} else {
			if m.Lib != nil {
				targets = append(targets, selectedTarget{*m.Lib, "lib"})
			}
			for _, t := range m.Bins {
				targets = append(targets, selectedTarget{t, "bin"})
			}
		}
		if len(targets) == 0 {
			return fmt.Errorf("native member %s has no selected targets", module)
		}
		allowed, err := project.NativeClosure(w, module)
		if err != nil {
			return err
		}
		for _, selected := range targets {
			if req.Mode == "run" && selected.kind == "lib" {
				return fmt.Errorf("cannot run a library")
			}
			platform := config["GOOS"] + "-" + config["GOARCH"]
			spec := project.UnitSpec{Host: runtime.GOOS + "-" + runtime.GOARCH, Target: platform, Kind: selected.kind, Name: selected.target.Name, Config: project.Digest([]any{"native-workspace-v1", lock.NativeWorkspace, module, selected, req.Mode, projectCompilerID()})}
			unit, err := layout.Begin(spec)
			if err != nil {
				return err
			}
			dirs, err := project.NativeView(w, unit.Bundle, allowed, lock.NativeWorkspace)
			if err != nil {
				return err
			}
			path := filepath.Join(dirs[module], selected.target.Path)
			if e = checkTarget(dirs[module], path); e != nil {
				return e
			}
			rel, err := filepath.Rel(unit.Bundle, path)
			if err != nil {
				return err
			}
			pattern := "./" + filepath.ToSlash(rel)
			// Every Go read and build sees this same owned work file. Metadata-only
			// members cannot satisfy an undeclared member import, even in batch mode.
			packages, err := nativePackages(req.GoTool, unit.Bundle, []string{pattern}, w.Config.Vendor != nil, req.Offline, req.Mode == "test")
			if err != nil {
				return fmt.Errorf("explicit native graph for %s: %w", module, err)
			}
			for _, p := range packages {
				if p.Module != nil {
					if _, member := w.Members[p.Module.Path]; member && !allowed[p.Module.Path] {
						return fmt.Errorf("undeclared native member import %s", p.ImportPath)
					}
				}
			}
			preflight := *lock.NativeWorkspace
			if e = project.CaptureNativeSums(w, unit.Bundle, &preflight); e != nil {
				return e
			}
			if req.Locked && project.Digest(preflight) != project.Digest(lock.NativeWorkspace) {
				return fmt.Errorf("--locked: native workspace sums changed during preflight")
			}
			lock.NativeWorkspace = &preflight
			if req.Mode == "transpile" {
				fmt.Fprintln(out, unit.Bundle)
				continue
			}
			args := []string{req.Mode, "-buildvcs=false", project.NativeMode(w)}
			if req.Mode == "test" {
				args = append(args, "-race")
			}
			binary := filepath.Join(unit.Intermediate, "executable")
			if req.Mode == "build" && selected.kind != "lib" {
				args = append(args, "-o", binary)
			}
			args = append(args, pattern)
			if req.Mode == "run" {
				args = append(args, req.ProgramArgs...)
			}
			cmd := exec.Command(req.GoTool, args...)
			cmd.Dir = unit.Bundle
			cmd.Env = project.NativeGoEnv(filepath.Join(unit.Bundle, "go.work"), true)
			cmd.Stdout = out
			cmd.Stderr = stderr
			if err = cmd.Run(); err != nil {
				return fmt.Errorf("native workspace %s: %w", req.Mode, err)
			}
			if req.Mode == "build" && selected.kind != "lib" {
				published, err := unit.PublishBinary(binary)
				if err != nil {
					return err
				}
				fmt.Fprintln(out, published)
			}
			// Readonly workspace operations may add sum metadata, but may not alter
			// policy. Do not hide such changes when the caller requested --locked.
			candidate := *lock.NativeWorkspace
			if e = project.CaptureNativeSums(w, unit.Bundle, &candidate); e != nil {
				return e
			}
			if req.Locked && project.Digest(candidate) != project.Digest(lock.NativeWorkspace) {
				return fmt.Errorf("--locked: native workspace sums changed during build")
			}
			lock.NativeWorkspace = &candidate
		}
	}
	if !req.Locked {
		return save()
	}
	return nil
}
