package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

func scriptPackageID(moduleRoot, module string) string {
	b, _ := json.Marshal([]string{filepath.Clean(moduleRoot), module})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ScriptCachePath returns the stable package cache location shared by all target
// entries. moduleRoot must be the same canonical path supplied to ScriptCache.
// This read-only calculation does not claim ownership or create directories.
func ScriptCachePath(layoutRoot, host, target, moduleRoot, module string) string {
	return filepath.Join(layoutRoot, "scripts", host, target, scriptPackageID(moduleRoot, module), "script-cache")
}

// ScriptCache claims a package cache container while holding the layout lease.
// The runner separately owns the script-cache directory and configuration keys.
func (l *Layout) ScriptCache(host, target, moduleRoot, module string) (string, error) {
	for _, s := range []string{host, target} {
		if !ident.MatchString(s) {
			return "", fmt.Errorf("invalid script cache platform component %q", s)
		}
	}
	path := ScriptCachePath(l.Root, host, target, moduleRoot, module)
	spec := UnitSpec{Host: host, Target: target, Kind: "lib", Name: scriptPackageID(moduleRoot, module), Config: "scripts-v1"}
	if e := l.ownedDirectory(filepath.Dir(path), spec); e != nil {
		return "", e
	}
	if e := NoSymlinks(path); e != nil {
		return "", e
	}
	return path, nil
}

// CleanScriptCaches validates the complete layout-owned script tree before any
// callback runs. The callback must validate and clean only its own cache; errors
// inside a callback cannot roll back callbacks that already succeeded. Neither
// package ownership containers nor unrelated units/binaries are removed.
func (l *Layout) CleanScriptCaches(clean func(string) error) error {
	if !l.active {
		return fmt.Errorf("target layout lease is closed")
	}
	if clean == nil {
		return fmt.Errorf("script cache cleaner required")
	}
	root := filepath.Join(l.Root, "scripts")
	if e := NoSymlinks(root); e != nil {
		return e
	}
	if _, e := os.Lstat(root); os.IsNotExist(e) {
		return nil
	} else if e != nil {
		return e
	}
	paths := []string{}
	var walk func(string, []string) error
	walk = func(path string, parts []string) error {
		entries, e := os.ReadDir(path)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			if e = NoSymlinks(child); e != nil {
				return e
			}
			if !entry.IsDir() {
				return fmt.Errorf("unexpected script layout entry: %s", child)
			}
			if !ident.MatchString(entry.Name()) {
				return fmt.Errorf("invalid script layout component: %s", child)
			}
			next := append(append([]string(nil), parts...), entry.Name())
			if len(next) < 3 {
				if e = walk(child, next); e != nil {
					return e
				}
				continue
			}
			if len(entry.Name()) != 64 {
				return fmt.Errorf("invalid script package identity: %s", child)
			}
			if _, e = hex.DecodeString(entry.Name()); e != nil {
				return fmt.Errorf("invalid script package identity: %s", child)
			}
			spec := UnitSpec{Host: next[0], Target: next[1], Kind: "lib", Name: next[2], Config: "scripts-v1"}
			if e = l.ownedDirectory(child, spec); e != nil {
				return e
			}
			contents, e := os.ReadDir(child)
			if e != nil {
				return e
			}
			for _, item := range contents {
				itemPath := filepath.Join(child, item.Name())
				if e = NoSymlinks(itemPath); e != nil {
					return e
				}
				switch item.Name() {
				case ".hgo-unit.json":
					if !item.Type().IsRegular() {
						return fmt.Errorf("invalid script unit marker: %s", itemPath)
					}
				case "script-cache":
					if !item.IsDir() {
						return fmt.Errorf("script cache is not a directory: %s", itemPath)
					}
					// Reject symlinks/special files throughout the opaque runner cache before
					// any cleaner is allowed to mutate any package's cache.
					if e = filepath.WalkDir(itemPath, func(p string, d fs.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if err = NoSymlinks(p); err != nil {
							return err
						}
						if !d.IsDir() && !d.Type().IsRegular() {
							return fmt.Errorf("invalid script cache entry: %s", p)
						}
						return nil
					}); e != nil {
						return e
					}
					paths = append(paths, itemPath)
				default:
					return fmt.Errorf("unexpected script package entry: %s", itemPath)
				}
			}
		}
		return nil
	}
	if e := walk(root, nil); e != nil {
		return e
	}
	for _, path := range paths {
		if e := clean(path); e != nil {
			return e
		}
	}
	return nil
}
