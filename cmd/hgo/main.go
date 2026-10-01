// hgo compiles the documented macro prototype subset to ordinary Go.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxsf/goxide/internal/frontend"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: hgo <transpile|expand|run|build> [flags] file.hgo")
	}
	mode := args[0]
	if mode != "transpile" && mode != "expand" && mode != "run" && mode != "build" {
		return fmt.Errorf("unknown command %q", mode)
	}
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	f.SetOutput(stderr)
	dest := f.String("o", "", "output Go source (transpile), expanded source (expand), or binary (build)")
	mapPath := f.String("source-map", "", "write source mapping JSON")
	expandedPath := f.String("expanded", "", "write expanded Go source for inspection")
	runtimeRoot := f.String("runtime", "", "local goxide module root for run/build")
	goTool := f.String("go", "go", "Go compiler executable (must be Go 1.27+)")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 1 {
		return errors.New("exactly one .hgo input is required; put flags before the input")
	}
	input, err := filepath.Abs(f.Arg(0))
	if err != nil {
		return err
	}
	src, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	// Validate all output paths before writing any artifacts.
	paths := []string{*mapPath, *expandedPath, *dest}
	if mode == "build" && *dest == "" {
		paths[2] = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	}
	for i, path := range paths {
		if path == "" {
			continue
		}
		if samePath(path, input) {
			return errors.New("refusing to overwrite input")
		}
		for _, prior := range paths[:i] {
			if prior != "" && samePath(path, prior) {
				return errors.New("output paths must be distinct")
			}
		}
	}
	result, err := frontend.Compile(input, src)
	if err != nil {
		return err
	}
	if *mapPath != "" {
		if err := writeArtifact(*mapPath, input, result.SourceMap); err != nil {
			return err
		}
	}
	if *expandedPath != "" {
		if err := writeArtifact(*expandedPath, input, result.Expanded); err != nil {
			return err
		}
	}
	if mode == "transpile" || mode == "expand" {
		data := result.Go
		if mode == "expand" {
			data = result.Expanded
		}
		if *dest == "" {
			_, err = out.Write(data)
			return err
		}
		return writeArtifact(*dest, input, data)
	}
	root := *runtimeRoot
	if root == "" {
		root = findModule(filepath.Dir(input))
		if root == "" {
			cwd, _ := os.Getwd()
			root = findModule(cwd)
		}
	}
	if root == "" {
		return errors.New("run/build requires -runtime pointing to the local goxide module")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return err
	}
	if !bytes.Contains(gomod, []byte("module github.com/hxsf/goxide")) {
		return errors.New("-runtime does not identify the goxide module")
	}
	// Select only a real Go executable; this environment may have a board game named go.
	tool, err := exec.LookPath(*goTool)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	check := exec.CommandContext(ctx, tool, "version")
	check.Env = localEnv()
	ver, err := check.Output()
	if err != nil {
		return fmt.Errorf("Go compiler check: %w", err)
	}
	parts := strings.Fields(string(ver))
	if len(parts) < 3 || parts[0] != "go" || parts[1] != "version" || !version.IsValid(parts[2]) || version.Compare(parts[2], "go1.27") < 0 {
		return fmt.Errorf("not a supported Go compiler: %s", ver)
	}
	tmp, err := os.MkdirTemp("", "hgo-run-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	// Quoting is go.mod string quoting, not shell interpolation.
	quoted, _ := json.Marshal(filepath.ToSlash(root))
	mod := "module hgo.local/generated\n\ngo 1.27\n\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => " + string(quoted) + "\n"
	if err = os.WriteFile(filepath.Join(tmp, "go.mod"), []byte(mod), 0600); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(tmp, "main.go"), result.Go, 0600); err != nil {
		return err
	}
	var argv []string
	if mode == "run" {
		argv = []string{"run", "-buildvcs=false", "."}
	} else {
		target := *dest
		if target == "" {
			target = strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		}
		target, err = filepath.Abs(target)
		if err != nil {
			return err
		}
		if samePath(target, input) {
			return errors.New("refusing to overwrite input")
		}
		argv = []string{"build", "-buildvcs=false", "-o", target, "."}
	}
	cmd := exec.Command(tool, argv...)
	cmd.Dir = tmp
	cmd.Env = localEnv()
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("generated Go %s: %w", mode, err)
	}
	return nil
}
func localEnv() []string {
	env := []string{}
	for _, v := range os.Environ() {
		name, _, _ := strings.Cut(v, "=")
		switch name {
		case "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB":
			continue
		}
		env = append(env, v)
	}
	return append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
}
func findModule(dir string) string {
	for {
		b, e := os.ReadFile(filepath.Join(dir, "go.mod"))
		if e == nil && bytes.Contains(b, []byte("module github.com/hxsf/goxide")) {
			return dir
		}
		p := filepath.Dir(dir)
		if p == dir {
			return ""
		}
		dir = p
	}
}
func writeArtifact(path, input string, data []byte) error {
	full, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if samePath(full, input) {
		return errors.New("refusing to overwrite input")
	}
	return os.WriteFile(full, data, 0644)
}

func samePath(a, b string) bool {
	x, e := filepath.Abs(a)
	if e != nil {
		return false
	}
	y, e := filepath.Abs(b)
	if e != nil {
		return false
	}
	if x == y {
		return true
	}
	sx, ex := os.Stat(x)
	sy, ey := os.Stat(y)
	if ex == nil && ey == nil && os.SameFile(sx, sy) {
		return true
	}
	// Resolve parent symlinks even when the final output does not exist yet.
	px, ex := filepath.EvalSymlinks(filepath.Dir(x))
	py, ey := filepath.EvalSymlinks(filepath.Dir(y))
	return ex == nil && ey == nil && filepath.Join(px, filepath.Base(x)) == filepath.Join(py, filepath.Base(y))
}
