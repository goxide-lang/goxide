package procmacro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

const SDKModule = "github.com/goxide-lang/syntax"

// CompilerSDKIdentity is set by scripts/build.go from the SDK sources linked
// into the compiler. An unstamped binary cannot build procedural macros.
var CompilerSDKIdentity string

// ValidateSDKIdentity compares the selected macro SDK with the compiler's
// build-time identity, never with a checkout found beside the running binary.
func ValidateSDKIdentity(actual string) error {
	if CompilerSDKIdentity == "" {
		return fmt.Errorf("compiler has no procedural macro SDK identity; rebuild it with go run ./scripts/build.go")
	}
	if actual != CompilerSDKIdentity {
		return fmt.Errorf("procedural macro SDK content mismatch: selected %s, compiler %s; rebuild the compiler with the selected SDK sources", actual, CompilerSDKIdentity)
	}
	return nil
}

type sdkPackage struct {
	Dir, ImportPath string
	Standard        bool
	Module          *struct{ Path, Dir string }

	GoFiles, CgoFiles                   []string
	CFiles, CXXFiles, MFiles, HFiles    []string
	FFiles, SFiles, SysoFiles           []string
	SwigFiles, SwigCXXFiles, EmbedFiles []string
}

// ReadSDKIdentity follows the caller's completed module selection and Go build
// environment. Version labels and unrelated module files are not protocol data.
func ReadSDKIdentity(ctx context.Context, tool, dir string, env []string) (string, error) {
	command := exec.CommandContext(ctx, tool, "list", "-mod=readonly", "-deps", "-json", SDKPackage, TokenPackage)
	command.Dir = dir
	command.Env = env
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read procedural macro SDK selection: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var packages []sdkPackage
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg sdkPackage
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("read procedural macro SDK selection: %w", err)
		}
		if !pkg.Standard {
			packages = append(packages, pkg)
		}
	}
	return hashSDKPackages(packages)
}

func hashSDKPackages(packages []sdkPackage) (string, error) {
	root := ""
	found := map[string]bool{}
	files := map[string]string{}
	for _, pkg := range packages {
		if pkg.Module == nil || pkg.Module.Path != SDKModule || pkg.Module.Dir == "" {
			return "", fmt.Errorf("procedural macro SDK package %s must be supplied by module %s", pkg.ImportPath, SDKModule)
		}
		moduleRoot := filepath.Clean(pkg.Module.Dir)
		if root == "" {
			root = moduleRoot
		} else if root != moduleRoot {
			return "", fmt.Errorf("procedural macro SDK packages have different source roots")
		}
		found[pkg.ImportPath] = true
		if _, err := sdkSourcePath(root, pkg.Dir); err != nil {
			return "", err
		}
		// Go list ignores symlinked .go files, so inspect the package directory
		// before trusting its selected source list.
		entries, err := os.ReadDir(pkg.Dir)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
				return "", fmt.Errorf("procedural macro SDK source is a symlink: %s", filepath.Join(pkg.Dir, entry.Name()))
			}
		}
		for _, names := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.MFiles, pkg.HFiles, pkg.FFiles, pkg.SFiles, pkg.SwigFiles, pkg.SwigCXXFiles, pkg.SysoFiles, pkg.EmbedFiles} {
			for _, name := range names {
				path := filepath.Join(pkg.Dir, name)
				relative, err := sdkSourcePath(root, path)
				if err != nil {
					return "", err
				}
				files[filepath.ToSlash(relative)] = path
			}
		}
	}
	if !found[SDKPackage] || !found[TokenPackage] {
		return "", fmt.Errorf("procedural macro SDK selection must contain %s and %s", SDKPackage, TokenPackage)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	hash := sha256.New()
	_, _ = io.WriteString(hash, "goxide procedural macro SDK\n")
	for _, path := range paths {
		data, err := os.ReadFile(files[path])
		if err != nil {
			return "", fmt.Errorf("read procedural macro SDK source %s: %w", path, err)
		}
		_, _ = fmt.Fprintf(hash, "%d:%s%d:", len(path), path, len(data))
		_, _ = hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func sdkSourcePath(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("procedural macro SDK source is outside its module: %s", path)
	}
	current := root
	parts := []string{""}
	if relative != "." {
		parts = append(parts, strings.Split(relative, string(filepath.Separator))...)
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("read procedural macro SDK source %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("procedural macro SDK source is a symlink: %s", current)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return "", fmt.Errorf("procedural macro SDK source is not a regular file: %s", current)
		}
	}
	return relative, nil
}
