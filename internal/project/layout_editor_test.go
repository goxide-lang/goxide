package project

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEditorMacroPublicationDoesNotCompeteWithCLILeases(t *testing.T) {
	root, layout, unit, staged := editorMacroFixture(t)
	release, err := LockProject(root)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	leases := editorMacroLeases(t, filepath.Join(root, "goxide.lock.guard"), filepath.Join(layout.Root, ".layout-lock"))
	if err := unit.PublishMacroWork(t.Context(), staged, false); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(unit.Intermediate, "macro", "cli", "macro.wasm")
	cliRecord := filepath.Join(unit.Directory, ".goxide-macro-cli-files.json")
	cliBytes, recordBytes := editorMacroRead(t, cli), editorMacroRead(t, cliRecord)
	wsWrite(t, staged, "macro.wasm", "editor program")
	if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, unit.Spec, staged); err != nil {
		t.Fatalf("editor publication competed with CLI leases: %v", err)
	}
	if got := editorMacroRead(t, filepath.Join(unit.Intermediate, "macro", "editor", "macro.wasm")); got != "editor program" {
		t.Fatalf("editor program = %q", got)
	}
	if editorMacroRead(t, cli) != cliBytes || editorMacroRead(t, cliRecord) != recordBytes {
		t.Fatal("editor publication changed the CLI lane or inventory")
	}
	editorMacroAssertLeases(t, leases)
	if _, err := os.Stat(filepath.Join(unit.Directory, ".macro-editor-lock")); !os.IsNotExist(err) {
		t.Fatalf("completed publication retained its editor lease: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "goxide.lock")); !os.IsNotExist(err) {
		t.Fatalf("editor publication wrote the dependency lock: %v", err)
	}
}

func TestBusyEditorMacroLaneSkipsWithoutBlockingCLI(t *testing.T) {
	root, layout, unit, staged := editorMacroFixture(t)
	if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, unit.Spec, staged); err != nil {
		t.Fatal(err)
	}
	lease := filepath.Join(unit.Directory, ".macro-editor-lock")
	if err := os.Mkdir(lease, 0700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lease)
	leases := editorMacroLeases(t, lease, filepath.Join(layout.Root, ".layout-lock"))
	before := editorMacroTree(t, layout.Root)
	wsWrite(t, staged, "macro.wasm", "new program")
	if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, unit.Spec, staged); err != nil {
		t.Fatalf("busy editor lane did not preserve private work: %v", err)
	}
	if after := editorMacroTree(t, layout.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("busy editor publication changed files or the other publisher's lease")
	}
	if err := unit.PublishMacroWork(t.Context(), staged, false); err != nil {
		t.Fatalf("editor-only lease blocked a CLI macro publisher: %v", err)
	}
	if editorMacroRead(t, filepath.Join(unit.Intermediate, "macro", "cli", "macro.wasm")) != "new program" {
		t.Fatal("CLI macro publisher did not update its lane")
	}
	if editorMacroRead(t, filepath.Join(unit.Intermediate, "macro", "editor", "macro.wasm")) != "initial program" {
		t.Fatal("CLI macro publisher changed the occupied editor lane")
	}
	editorMacroAssertLeases(t, leases)
}

func TestEditorMacroPublicationSkipsUnpublishedOwners(t *testing.T) {
	for _, state := range []string{"missing layout", "partial layout", "missing unit", "missing unit owner", "partial unit owner"} {
		t.Run(state, func(t *testing.T) {
			root := editorMacroCanonical(t, t.TempDir())
			staged := t.TempDir()
			wsWrite(t, staged, "macro.wasm", "private program")
			spec := layoutSpec()
			spec.Kind = "proc-macro"
			if state == "partial layout" {
				wsWrite(t, root, filepath.Join("target", LayoutMarker), `{"Version":`)
			} else if state != "missing layout" {
				layout, err := OpenLayout(root, "example.org/app", "target")
				if err != nil {
					t.Fatal(err)
				}
				defer layout.Close()
				if state != "missing unit" {
					directory := filepath.Join(layout.Root, "units", spec.Target, spec.Kind, spec.Name, spec.Config)
					if state == "partial unit owner" {
						wsWrite(t, directory, ".goxide-unit.json", `{"Owner":`)
					} else {
						wsWrite(t, directory, "user-note", "preserve unknown directory")
					}
				}
			}
			before := editorMacroTree(t, root)
			if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", "target", spec, staged); err != nil {
				t.Fatalf("unpublished ownership must leave the private program usable: %v", err)
			}
			if after := editorMacroTree(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("editor claimed or changed an unpublished layout/unit")
			}
		})
	}
}

func TestEditorMacroPublicationRejectsInvalidOwnership(t *testing.T) {
	for _, state := range []string{"invalid layout", "foreign layout", "invalid unit", "foreign unit", "symlink output", "modified output", "unrecorded output", "invalid spec"} {
		t.Run(state, func(t *testing.T) {
			root, layout, unit, staged := editorMacroFixture(t)
			spec := unit.Spec
			external := ""
			switch state {
			case "invalid layout":
				wsWrite(t, layout.Root, LayoutMarker, `{"unrecognized":true}`)
			case "foreign layout":
				owner := layout.owner
				owner.Module = "example.org/other"
				editorMacroJSON(t, filepath.Join(layout.Root, LayoutMarker), owner)
			case "invalid unit":
				wsWrite(t, unit.Directory, ".goxide-unit.json", `{"unknown":true}`)
			case "foreign unit":
				other := spec
				other.Config = "foreign"
				editorMacroJSON(t, filepath.Join(unit.Directory, ".goxide-unit.json"), unitOwner{layout.owner, other})
			case "symlink output":
				external = t.TempDir()
				wsWrite(t, external, "keep", "external file")
				if err := os.MkdirAll(filepath.Join(unit.Intermediate, "macro"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(unit.Intermediate, "macro", "editor")); err != nil {
					t.Fatal(err)
				}
			case "modified output", "unrecorded output":
				if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, spec, staged); err != nil {
					t.Fatal(err)
				}
				name := "macro.wasm"
				if state == "unrecorded output" {
					name = "user-note"
				}
				wsWrite(t, unit.Intermediate, filepath.Join("macro", "editor", name), "user contents")
			case "invalid spec":
				spec.Config = "../escape"
			}
			before := editorMacroTree(t, layout.Root)
			if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, spec, staged); err == nil {
				t.Fatal("editor publication accepted invalid ownership or modified output")
			}
			if after := editorMacroTree(t, layout.Root); !reflect.DeepEqual(before, after) {
				t.Fatal("ownership rejection changed existing output")
			}
			if external != "" && editorMacroRead(t, filepath.Join(external, "keep")) != "external file" {
				t.Fatal("publication followed an output symlink")
			}
		})
	}
}

func TestCanceledEditorMacroPublicationRetainsOldSlot(t *testing.T) {
	root, layout, unit, staged := editorMacroFixture(t)
	if err := PublishEditorMacroWork(t.Context(), root, "example.org/app", layout.Root, unit.Spec, staged); err != nil {
		t.Fatal(err)
	}
	before := editorMacroTree(t, layout.Root)
	wsWrite(t, staged, "macro.wasm", "canceled program")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := PublishEditorMacroWork(ctx, root, "example.org/app", layout.Root, unit.Spec, staged); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publication = %v", err)
	}
	if after := editorMacroTree(t, layout.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("canceled publication changed the previous slot or acquired a lease")
	}
}

func editorMacroFixture(t *testing.T) (string, *Layout, *Unit, string) {
	t.Helper()
	root := editorMacroCanonical(t, t.TempDir())
	layout, err := OpenLayout(root, "example.org/app", "target")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { layout.Close() })
	spec := layoutSpec()
	spec.Kind = "proc-macro"
	unit, err := layout.Begin(spec)
	if err != nil {
		t.Fatal(err)
	}
	staged := t.TempDir()
	wsWrite(t, staged, "macro.wasm", "initial program")
	return root, layout, unit, staged
}

func editorMacroCanonical(t *testing.T, name string) string {
	t.Helper()
	name, err := filepath.EvalSymlinks(name)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func editorMacroRead(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func editorMacroJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func editorMacroTree(t *testing.T, root string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			state[relative] = "directory"
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			state[relative] = "symlink:" + target
		default:
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			state[relative] = "file:" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func editorMacroLeases(t *testing.T, paths ...string) map[string]os.FileInfo {
	t.Helper()
	leases := map[string]os.FileInfo{}
	for _, name := range paths {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		leases[name] = info
	}
	return leases
}

func editorMacroAssertLeases(t *testing.T, leases map[string]os.FileInfo) {
	t.Helper()
	for name, original := range leases {
		current, err := os.Stat(name)
		if err != nil || !os.SameFile(original, current) {
			t.Fatalf("publication stole or replaced lease %s: %v", name, err)
		}
	}
}
