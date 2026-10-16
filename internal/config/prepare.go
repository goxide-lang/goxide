package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

var semanticGoKeys = []string{
	"GOVERSION", "GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT", "GOFIPS140",
	"GOAMD64", "GO386", "GOARM", "GOARM64", "GOMIPS", "GOMIPS64", "GOPPC64", "GORISCV64", "GOWASM",
	"CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS", "PKG_CONFIG",
}

// Prepare resolves Go defaults after applying this workflow's target policy.
// captured is the invocation's original environment, including when it is empty.
func Prepare(tool, dir, targetText string, native bool, captured []string) (*Environment, error) {
	selected := HostTarget()
	if native && targetText != "" {
		return nil, fmt.Errorf("--target is not accepted by native Go generation; use GOOS and GOARCH")
	}
	if targetText != "" {
		var err error
		selected, err = ParseTarget(targetText)
		if err != nil {
			return nil, err
		}
	}
	e, err := NewEnvironment(selected, captured)
	if err != nil {
		return nil, err
	}
	if tool == "" {
		tool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	tool, err = findTool(tool, e.process["PATH"])
	if err != nil {
		return nil, err
	}
	e.GoTool = tool
	execution := cloneMap(e.process)
	execution["GOTOOLCHAIN"], execution["GOWORK"], execution["GOFLAGS"] = "local", "off", ""
	if !native {
		execution["GOOS"], execution["GOARCH"] = selected.OS, selected.Arch
	}
	output, err := goOutput(tool, dir, environmentSlice(execution), "env", "-json")
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal(output, &values); err != nil {
		return nil, fmt.Errorf("read Go environment: %w", err)
	}
	if values["GOVERSION"] != runtime.Version() {
		return nil, fmt.Errorf("selected Go %s differs from compiler Go %s", values["GOVERSION"], runtime.Version())
	}
	e.GoVersion = values["GOVERSION"]
	e.Target, err = target(values["GOOS"], values["GOARCH"])
	if err != nil {
		return nil, err
	}
	// Pin Go's resolved defaults before disabling further reads of its env file.
	for key, value := range values {
		execution[key] = value
	}
	execution["GOENV"] = "off"
	execution["GOTOOLCHAIN"], execution["GOWORK"], execution["GOFLAGS"] = "local", "off", ""
	e.execution = environmentSlice(execution)
	e.values = map[string]string{}
	for _, key := range semanticGoKeys {
		if value, ok := values[key]; ok {
			e.values[key] = value
		}
	}
	output, err = goOutput(tool, dir, e.execution, "tool", "dist", "list", "-json")
	if err != nil {
		return nil, err
	}
	var supported []struct{ GOOS, GOARCH string }
	if err := json.Unmarshal(output, &supported); err != nil {
		return nil, fmt.Errorf("read Go target list: %w", err)
	}
	found := false
	for _, pair := range supported {
		t, err := target(pair.GOOS, pair.GOARCH)
		if err != nil {
			return nil, fmt.Errorf("selected Go has a target outside Goxide's known values: %w", err)
		}
		e.SupportedTargets = append(e.SupportedTargets, t)
		found = found || t.OS == e.Target.OS && t.Arch == e.Target.Arch
	}
	if !found {
		return nil, fmt.Errorf("selected Go does not support target %s", e.Target)
	}
	sort.Slice(e.SupportedTargets, func(i, j int) bool {
		return e.SupportedTargets[i].String() < e.SupportedTargets[j].String()
	})
	// Read Go's own experiment, architecture and release tags instead of
	// duplicating their version-specific derivation.
	output, err = goOutput(tool, dir, e.execution, "list", "-f", `{{join context.ToolTags ","}};{{join context.ReleaseTags ","}}`, "unsafe")
	if err != nil {
		return nil, err
	}
	toolTags, releaseTags, ok := strings.Cut(strings.TrimSpace(string(output)), ";")
	if !ok {
		return nil, fmt.Errorf("invalid Go build context")
	}
	if toolTags != "" {
		e.toolTags = strings.Split(toolTags, ",")
	}
	if releaseTags != "" {
		e.releaseTags = strings.Split(releaseTags, ",")
	}
	return e, nil
}

func findTool(tool, pathValue string) (string, error) {
	if strings.ContainsAny(tool, `/\`) {
		return filepath.Abs(tool)
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, tool)
		if runtime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			candidate += ".exe"
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0111 != 0) {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("Go tool %q is not available in the captured PATH", tool)
}

func goOutput(tool, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, tool, args...)
	c.Dir, c.Env = dir, env
	c.WaitDelay = time.Second
	var stderr bytes.Buffer
	c.Stderr = &stderr
	output, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("Go %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return output, nil
}
