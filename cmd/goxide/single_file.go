package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
)

// singleFileModule owns only configuration copies and one generated file. Its
// source directory remains Go's module owner; no sibling source is selected.
type singleFileModule struct {
	input, root, packagePath string
	displayInput             string
	temporary, modFile       string
	config                   config.Config
	usedSourceRoots          map[string]paths.Namespace
	nativeRequirements       map[string]string
	authoredMod              []byte
}

// Keep the execution environment aligned with the offline export importer,
// including private-module settings that can otherwise bypass GOPROXY=off.
func singleFileEnvironment(base ...[]string) []string {
	env := os.Environ()
	if len(base) != 0 {
		env = base[0]
	}
	return compile.NativeEnvironment(env)
}

func prepareSingleFileModule(input string, cfg config.Config) (*singleFileModule, error) {
	module, err := compile.OpenNativeModule(context.Background(), input, cfg)
	if err != nil || module == nil {
		return nil, err
	}
	return &singleFileModule{input: module.Input, root: module.Root, packagePath: module.PackagePath, temporary: module.Temporary, modFile: module.ModFile, config: module.Config, authoredMod: module.AuthoredMod}, nil
}

func (m *singleFileModule) nativeModule() *compile.NativeModule {
	return &compile.NativeModule{Input: m.input, Root: m.root, PackagePath: m.packagePath, Temporary: m.temporary, ModFile: m.modFile, Config: m.config, AuthoredMod: m.authoredMod}
}

func (m *singleFileModule) close() { os.RemoveAll(m.temporary) }

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (m *singleFileModule) validateOutputs(req *CompileRequest) error {
	if req.Mode == "build" && req.Output == "" {
		req.Output = strings.TrimSuffix(filepath.Base(m.input), filepath.Ext(m.input))
	}
	for _, output := range []struct {
		path *string
		kind string
	}{{&req.SourceMap, "source map"}, {&req.Expanded, "expanded source"}, {&req.Output, "output"}} {
		if *output.path == "" {
			continue
		}
		kind := output.kind
		if output.path == &req.Output && req.Mode == "build" {
			kind = "binary output"
		}
		path, err := m.outputPath(*output.path, kind)
		if err != nil {
			return err
		}
		*output.path = path
	}
	return nil
}

func (m *singleFileModule) outputPath(path, kind string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	path = filepath.Join(parent, filepath.Base(path))
	if pathWithin(m.root, path) {
		return "", fmt.Errorf("%s must be outside the source module; choose an external output path", kind)
	}
	// Existing outputs can themselves be symlinks/hardlinks to source or
	// dependency files. A configured invocation only owns fresh artifacts.
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("refusing to overwrite existing %s %s", kind, path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func (m *singleFileModule) run(req CompileRequest, result compiledFile, tool string, out, stderr io.Writer) error {
	generated := filepath.Join(m.temporary, "selected.go")
	if err := os.WriteFile(generated, result.Go, 0600); err != nil {
		return err
	}
	// Go ignores filenames beginning with '_' even in a named-file command.
	// Use a fresh ordinary filename, and never overlay a real source file.
	stem := "goxide_single_" + strings.TrimPrefix(filepath.Base(m.temporary), "goxide-single-")
	var virtual string
	for n := 0; ; n++ {
		virtual = filepath.Join(filepath.Dir(m.input), fmt.Sprintf("%s_%d.go", stem, n))
		if _, err := os.Lstat(virtual); os.IsNotExist(err) {
			break
		} else if err != nil {
			return err
		}
	}
	overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{virtual: generated}})
	if err != nil {
		return err
	}
	overlayPath := filepath.Join(m.temporary, "overlay.json")
	if err = os.WriteFile(overlayPath, overlay, 0600); err != nil {
		return err
	}
	argv := []string{req.Mode, "-buildvcs=false", "-mod=readonly", "-modfile=" + m.modFile, "-overlay=" + overlayPath}
	if req.Mode == "build" {
		target, err := m.outputPath(req.Output, "binary output")
		if err != nil {
			return err
		}
		argv = append(argv, "-o", target)
	}
	argv = append(argv, virtual)
	if req.Mode == "run" {
		argv = append(argv, req.ProgramArgs...)
	}
	cmd := exec.Command(tool, argv...)
	cmd.Dir, cmd.Env = filepath.Dir(m.input), singleFileEnvironment(m.config.Environment.ExecEnv())
	cmd.Stdout, cmd.Stdin = out, os.Stdin
	cmd.Stderr = stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("generated Go %s: %w", req.Mode, err)
	}
	return nil
}
