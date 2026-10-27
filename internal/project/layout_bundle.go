package project

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

type bundleFile struct {
	Hash string
	Mode uint32
}

// PublishBundle keeps stable Go build paths and unchanged file mtimes. The file
// inventory is needed to remove obsolete generated files without deleting or
// overwriting user changes. Generation happens in a temporary sibling tree so
// failed lowering cannot leave old helpers mixed into a new source view.
func (u *Unit) PublishBundle(staged string) error {
	return u.publishBundle(context.Background(), staged, u.Bundle, filepath.Join(u.Directory, ".goxide-files.json"))
}

// PublishMacroWork records successfully compiled macro work in a fixed slot.
// Editor overlays and CLI disk inputs have separate destinations. Readers must
// retain their private source view and WASM bytes rather than use this mutable
// shared slot. Existing files retain the ordinary bundle ownership protection.
func (u *Unit) PublishMacroWork(ctx context.Context, staged string, editor bool) error {
	if u.Spec.Kind != "proc-macro" {
		return fmt.Errorf("macro work requires a proc-macro unit")
	}
	lane := "cli"
	if editor {
		lane = "editor"
	}
	return u.publishBundle(ctx, staged, filepath.Join(u.Intermediate, "macro", lane), filepath.Join(u.Directory, ".goxide-macro-"+lane+"-files.json"))
}

func (u *Unit) publishBundle(ctx context.Context, staged, bundle, record string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !u.layout.active {
		return fmt.Errorf("target layout lease is closed")
	}
	if e := NoSymlinks(bundle); e != nil {
		return e
	}
	old := map[string]bundleFile{}
	if e := readJSON(record, &old); e != nil && !os.IsNotExist(e) {
		return e
	}
	for name := range old {
		if !safeRelative(name) || filepath.Clean(name) != name || name == "." {
			return fmt.Errorf("invalid generated file record: %s", name)
		}
	}
	// Validate all old files before any mutation, including stale files.
	e := filepath.WalkDir(bundle, func(path string, entry fs.DirEntry, err error) error {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if os.IsNotExist(err) && path == bundle {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated output contains symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(bundle, path)
		want, ok := old[rel]
		hash, mode, err := fileDigest(path)
		if err != nil {
			return err
		}
		if !ok || hash != want.Hash || uint32(mode) != want.Mode {
			return fmt.Errorf("refusing modified or unrecorded generated output: %s", path)
		}
		return nil
	})
	if e != nil {
		return e
	}
	files := map[string][]byte{}
	next := map[string]bundleFile{}
	e = filepath.WalkDir(staged, func(path string, entry fs.DirEntry, err error) error {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generated output contains symlink: %s", path)
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(staged, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if entry.Name() == "go.mod" || entry.Name() == "go.work" {
			// Go quotes filesystem paths containing spaces or special characters.
			quote := func(s string) string { q := strconv.Quote(s); return q[1 : len(q)-1] }
			data = bytes.ReplaceAll(data, []byte(quote(staged)), []byte(quote(bundle)))
			data = bytes.ReplaceAll(data, []byte(staged), []byte(bundle))
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("generated output must be a regular file: %s", path)
		}
		files[rel] = data
		next[rel] = bundleFile{Hash: fmt.Sprintf("%x", sha256.Sum256(data)), Mode: uint32(info.Mode().Perm())}
		return nil
	})
	if e != nil {
		return e
	}
	// Cancellation is observed before the commit point. Publication itself is
	// short and serialized by the layout lease, as for ordinary bundles.
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, name := range keys(old) {
		if _, exists := next[name]; exists {
			continue
		}
		path := filepath.Join(bundle, name)
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			return e
		}
		for dir := filepath.Dir(path); dir != bundle; dir = filepath.Dir(dir) {
			if e = os.Remove(dir); e != nil {
				break // Preserve a directory that still has files.
			}
		}
	}
	for _, name := range keys(next) {
		path := filepath.Join(bundle, name)
		if next[name] == old[name] {
			if _, e := os.Stat(path); e == nil {
				continue
			}
		}
		if e = writeBundleFile(path, files[name], os.FileMode(next[name].Mode)); e != nil {
			return e
		}
	}
	data, e := json.MarshalIndent(next, "", "  ")
	if e != nil {
		return e
	}
	data = append(data, '\n')
	if prior, e := os.ReadFile(record); e == nil && bytes.Equal(prior, data) {
		return nil
	}
	return writeBundleFile(record, data, 0644)
}

func writeBundleFile(path string, data []byte, mode os.FileMode) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".write-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(data)
	if e == nil {
		e = f.Chmod(mode)
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
