package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// NativeModule owns a private copy of a native Go module's configuration.
type NativeModule struct {
	Input, Root, PackagePath, Temporary, ModFile string
	Config                                       config.Config
	AuthoredMod                                  []byte
}

func OpenNativeModule(ctx context.Context, input string, cfg config.Config) (_ *NativeModule, err error) {
	input, err = filepath.Abs(input)
	if err != nil {
		return nil, err
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return nil, err
	}
	sourceDirectory := filepath.Dir(input)
	if info, statErr := os.Stat(input); statErr != nil {
		return nil, statErr
	} else if info.IsDir() {
		sourceDirectory = input
	}
	root := sourceDirectory
	for {
		_, err = os.Lstat(filepath.Join(root, "go.mod"))
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, nil // Standalone keeps its existing runtime fallback.
		}
		root = parent
	}
	temporaryRoot, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil, err
	}
	temporaryRoot, err = filepath.EvalSymlinks(temporaryRoot)
	if err != nil {
		return nil, err
	}
	if within(root, temporaryRoot) {
		return nil, fmt.Errorf("configured single-file temporary directory must be outside the source module; set TMPDIR to an external directory")
	}
	dir, err := os.MkdirTemp(temporaryRoot, "goxide-single-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	m := &NativeModule{Input: input, Root: root, Temporary: dir, ModFile: filepath.Join(dir, "consumer.mod"), Config: cfg}
	for _, item := range []struct{ source, destination string }{{"go.mod", "consumer.mod"}, {"go.sum", "consumer.sum"}} {
		data, e := os.ReadFile(filepath.Join(root, item.source))
		if item.source == "go.sum" && os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if item.source == "go.mod" {
			m.AuthoredMod = data
		}
		if e = os.WriteFile(filepath.Join(dir, item.destination), data, 0600); e != nil {
			return nil, e
		}
	}
	// Ask Go to parse the copied configuration, retaining every directive in the
	// original bytes. Relative replaces still belong to root, not temporary.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.Environment.GoTool, "mod", "edit", "-json", m.ModFile)
	cmd.WaitDelay = time.Second
	cmd.Dir, cmd.Env = root, NativeEnvironment(cfg.Environment.ExecEnv())
	data, err := cmd.Output()
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("input module configuration: %w: %s", err, bytes.TrimSpace(failure.Stderr))
		}
		return nil, fmt.Errorf("input module configuration: %w", err)
	}
	var declarations struct{ Module *struct{ Path string } }
	if err = json.Unmarshal(data, &declarations); err != nil {
		return nil, err
	}
	if declarations.Module == nil || declarations.Module.Path == "" {
		return nil, fmt.Errorf("input go.mod has no module directive")
	}
	rel, err := filepath.Rel(root, sourceDirectory)
	if err != nil {
		return nil, err
	}
	m.PackagePath = declarations.Module.Path
	if rel != "." {
		m.PackagePath += "/" + filepath.ToSlash(rel)
	}
	return m, nil
}

func (m *NativeModule) Close() error { return os.RemoveAll(m.Temporary) }

// NativeEnvironment matches the offline export importer and never mutates a workspace.
func NativeEnvironment(base []string) []string {
	env := []string{}
	for _, value := range base {
		name, _, _ := strings.Cut(value, "=")
		switch name {
		case "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOPROXY", "GOSUMDB", "GONOPROXY", "GOVCS":
			continue
		}
		env = append(env, value)
	}
	return append(env, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off", "GONOPROXY=none", "GOVCS=*:off")
}

func nativeModuleCommand(ctx context.Context, root string, cfg config.Config, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, cfg.Environment.GoTool, args...)
	command.WaitDelay = time.Second
	command.Dir, command.Env = root, NativeEnvironment(cfg.Environment.ExecEnv())
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	if err != nil {
		return nil, fmt.Errorf("native source module: %w: %s", err, stderr.String())
	}
	return output, nil
}

func SourceRoots(moduleRoot, sourceDirectory, explicit string) (paths.Roots, error) {
	if explicit != "" {
		return rootconfig.Read(explicit)
	}
	if moduleRoot != "" {
		return rootconfig.Load(moduleRoot)
	}
	return rootconfig.Discover(sourceDirectory)
}

// UsePreparedModules updates only this owner's private modfile with prepared dependencies.
func (owner *NativeModule) UsePreparedModules(ctx context.Context, root string, macros []string) error {
	data, err := nativeModuleCommand(ctx, root, owner.Config, "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var mod project.ModFile
	if err := json.Unmarshal(data, &mod); err != nil {
		return err
	}
	args := []string{"mod", "edit", "-modfile=" + owner.ModFile}
	for _, module := range macros {
		args = append(args, "-droprequire="+module)
	}
	for _, requirement := range mod.Require {
		args = append(args, "-require="+requirement.Path+"@"+requirement.Version)
	}
	for _, replacement := range mod.Replace {
		old, value := replacement.Old.Path, replacement.New.Path
		if replacement.Old.Version != "" {
			old += "@" + replacement.Old.Version
		}
		if replacement.New.Version != "" {
			value += "@" + replacement.New.Version
		} else if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		args = append(args, "-replace="+old+"="+value)
	}
	if len(args) == 3 {
		return nil
	}
	_, err = nativeModuleCommand(ctx, owner.Root, owner.Config, args...)
	return err
}
