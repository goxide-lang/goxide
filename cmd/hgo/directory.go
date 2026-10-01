package main

import (
	"bytes"
	"context"
	"fmt"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/hxsf/goxide/internal/frontend"
)

func runDirectory(mode, input, dest, goTool string, out, stderr io.Writer) error {
	return runDirectoryTags(mode, input, dest, goTool, nil, out, stderr)
}
func runDirectoryTags(mode, input, dest, goTool string, tags []string, out, stderr io.Writer) error {
	options := frontend.ModuleOptions{Tags: tags, Race: mode == "test"}
	if mode == "expand" {
		return fmt.Errorf("expand is a single-file command; use transpile for packages")
	}
	if mode == "transpile" {
		if dest == "" {
			return fmt.Errorf("directory transpile requires -o with a fresh output directory")
		}
		_, err := frontend.CompileModuleWithOptions(input, dest, options)
		return err
	}
	if err := checkDirectoryCompiler(goTool); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "hgo-module-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	result, err := frontend.CompileModuleWithOptions(input, filepath.Join(tmp, "module"), options)
	if err != nil {
		return err
	}
	argv := []string{mode, "-buildvcs=false", "-tags=" + strings.Join(tags, ",")}
	if mode == "build" {
		if dest == "" {
			dest = filepath.Base(input)
		}
		dest, err = filepath.Abs(dest)
		if err != nil {
			return err
		}
		if _, e := os.Lstat(dest); !os.IsNotExist(e) {
			return fmt.Errorf("refusing to overwrite existing binary output %s", dest)
		}
		parent, e := filepath.EvalSymlinks(filepath.Dir(dest))
		if e != nil {
			return e
		}
		dest = filepath.Join(parent, filepath.Base(dest))
		if rel, e := filepath.Rel(result.SourceRoot, dest); e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return fmt.Errorf("binary output must be outside the source module")
		}

		argv = append(argv, "-o", dest)
	}
	target := result.Target
	if mode == "test" {
		if target == "." {
			target = "./..."
		} else {
			target += "/..."
		}
		argv = append(argv, "-race")
	}
	argv = append(argv, target)
	cmd := exec.Command(goTool, argv...)
	cmd.Dir = result.Root
	cmd.Env = localEnv()
	cmd.Stdout = out
	var compilerErrors bytes.Buffer
	cmd.Stderr = io.MultiWriter(stderr, &compilerErrors)
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		for name, r := range result.Files {
			if r.Diagnostics != nil {
				fmt.Fprint(stderr, r.Diagnostics.DiagnosticsFile(r.Go, compilerErrors.String(), name))
				fmt.Fprint(stderr, r.Diagnostics.DiagnosticsFile(r.Go, compilerErrors.String(), filepath.Join(result.Root, name)))
			}
		}
		return fmt.Errorf("generated module %s: %w", mode, err)
	}
	return nil
}

func checkDirectoryCompiler(tool string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "version")
	cmd.Env = localEnv()
	data, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Go compiler check: %w", err)
	}
	parts := strings.Fields(string(data))
	if len(parts) < 3 || parts[0] != "go" || parts[1] != "version" || !version.IsValid(parts[2]) || version.Compare(parts[2], "go1.27") < 0 {
		return fmt.Errorf("not a supported Go compiler: %s", data)
	}
	if parts[2] != runtime.Version() {
		return fmt.Errorf("directory -go compiler version %s must match the frontend build toolchain %s", parts[2], runtime.Version())
	}
	return nil
}
