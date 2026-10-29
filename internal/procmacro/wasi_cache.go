//go:build !js

package procmacro

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"

	"github.com/tetratelabs/wazero"
)

func newWASICompilationCache() (wazero.CompilationCache, string, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, "", nil
	}
	identity := ""
	for _, dependency := range info.Deps {
		if dependency.Path == "github.com/tetratelabs/wazero" {
			// An unstamped or locally replaced engine has no reusable binary identity.
			if dependency.Replace != nil || dependency.Version == "" || dependency.Sum == "" {
				return nil, "", nil
			}
			identity = dependency.Version + "\n" + dependency.Sum
			break
		}
	}
	if identity == "" {
		return nil, "", nil
	}
	root := os.Getenv("GOXIDE_WASM_CACHE")
	if root == "" {
		directory, err := os.UserCacheDir()
		if err != nil {
			return nil, "", err
		}
		root = filepath.Join(directory, "goxide", "wazero")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, "", err
	}
	stat, err := os.Lstat(root)
	if err != nil {
		return nil, "", err
	}
	// Cached native code must remain under the caller's control. Windows uses
	// the inherited user-cache ACL rather than POSIX permission bits.
	if !stat.IsDir() || runtime.GOOS != "windows" && stat.Mode().Perm()&0o022 != 0 {
		return nil, "", fmt.Errorf("procedural macro cache directory is writable by other users")
	}
	id := sha256.Sum256([]byte(identity))
	directory := filepath.Join(root, hex.EncodeToString(id[:]))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, "", err
	}
	stat, err = os.Lstat(directory)
	if err != nil {
		return nil, "", err
	}
	if !stat.IsDir() || runtime.GOOS != "windows" && stat.Mode().Perm()&0o022 != 0 {
		return nil, "", fmt.Errorf("procedural macro cache identity directory is not private")
	}
	cache, err := wazero.NewCompilationCacheWithDir(directory)
	if err != nil {
		_ = cache.Close(context.Background())
		return nil, "", err
	}
	return cache, directory, nil
}
