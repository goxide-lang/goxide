package project

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// editorLayout never creates output or acquires a writer lease. An absent or
// still-incomplete owner record offers no shared cache; editor work remains
// private. Fully published foreign or malformed ownership is rejected.
func editorLayout(root, module, requested string) (string, string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	directory, err := ResolveTargetDir(root, requested)
	if err != nil {
		return "", "", err
	}
	if err := RejectManagedOutput(directory); err != nil {
		return "", "", fmt.Errorf("nested target layout: %w", err)
	}
	var owner layoutOwner
	err = readJSON(filepath.Join(directory, LayoutMarker), &owner)
	if unpublishedOwnership(err) {
		return directory, "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("invalid target ownership: %w", err)
	}
	if owner != (layoutOwner{1, root, module, directory}) {
		return "", "", fmt.Errorf("target directory belongs to a different project or location: %s", directory)
	}
	return directory, filepath.Join(directory, "cache", "git"), nil
}

func unpublishedOwnership(err error) bool {
	return os.IsNotExist(err) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// LockTemporaryFile recognizes the root-local temporary names used by Save.
// Those files may disappear during a read-only editor source traversal.
func LockTemporaryFile(name string) bool {
	suffix, ok := strings.CutPrefix(name, ".goxide-lock-")
	if !ok || suffix == "" {
		return false
	}
	for _, digit := range suffix {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
