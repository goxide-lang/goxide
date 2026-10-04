package main

import (
	"fmt"
	"github.com/hxsf/goxide/internal/project"
	"os"
	"path/filepath"
)

func copyImportVendor(src, dst string) error {
	if e := project.NoSymlinks(src); e != nil {
		return e
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, path)
		to := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("vendor symlink forbidden: %s", rel)
		}
		if d.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("vendor special file: %s", rel)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if e = os.WriteFile(to, b, info.Mode().Perm()); e != nil {
			return e
		}
		return os.Chmod(to, info.Mode().Perm())
	})
}
