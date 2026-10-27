package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PublishEditorMacroWork may refresh an existing configuration's editor lane.
// It never creates a layout/unit or acquires either CLI writer lease. An absent
// owner or busy editor lane leaves the caller's private program usable.
func PublishEditorMacroWork(ctx context.Context, root, module, targetDir string, spec UnitSpec, staged string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateUnitSpec(spec); err != nil {
		return err
	}
	if spec.Kind != "proc-macro" {
		return fmt.Errorf("macro work requires a proc-macro unit")
	}
	directory, cache, err := editorLayout(root, module, targetDir)
	if err != nil || cache == "" {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	unit := filepath.Join(directory, "units", spec.Target, spec.Kind, spec.Name, spec.Config)
	if err := NoSymlinks(unit); err != nil {
		return err
	}
	var owner unitOwner
	if err := readJSON(filepath.Join(unit, ".goxide-unit.json"), &owner); unpublishedOwnership(err) {
		return ctx.Err()
	} else if err != nil {
		return err
	}
	if owner != (unitOwner{layoutOwner{1, root, module, directory}, spec}) {
		return fmt.Errorf("unit ownership mismatch: %s", unit)
	}
	return publishEditorMacroWork(ctx, unit, staged)
}

func publishEditorMacroWork(ctx context.Context, unit, staged string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease := filepath.Join(unit, ".macro-editor-lock")
	if err := os.Mkdir(lease, 0700); os.IsExist(err) {
		return ctx.Err()
	} else if err != nil {
		return err
	}
	defer os.Remove(lease)
	return publishBundle(ctx, staged, filepath.Join(unit, "intermediate", "macro", "editor"), filepath.Join(unit, ".goxide-macro-editor-files.json"))
}
