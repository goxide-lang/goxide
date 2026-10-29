package project

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/goxide-lang/goxide/internal/naming"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const LayoutMarker = ".goxide-layout.json"

type layoutOwner struct {
	Version                    int
	Project, Module, Directory string
}

// Layout owns output paths only, never dependency caches, lockfiles or trust.
// OpenLayout holds an exclusive lease until Close; stale leases are not stolen.
type Layout struct {
	Root   string
	owner  layoutOwner
	active bool
}
type UnitSpec struct{ Host, Target, Kind, Name, Config string }
type Unit struct {
	Spec                                             UnitSpec
	Directory, Bundle, Intermediate, BinaryDirectory string
	layout                                           *Layout
}

type unitOwner struct {
	Owner layoutOwner
	Unit  UnitSpec
}

func contained(root, path string) bool {
	r, e := filepath.Rel(root, path)
	return e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}

// NoSymlinks validates every existing path component, including parents of a
// not-yet-created output. It is not a sandbox against a hostile concurrent OS user.
func NoSymlinks(path string) error {
	path, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for p := path; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed output path contains symlink: %s", p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func writeExclusive(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func readJSON(path string, v any) error {
	info, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("ownership record must be a regular file: %s", path)
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return fmt.Errorf("invalid ownership record: %s", path)
	}
	return nil
}

// ResolveTargetDir uses manifest-root-relative CLI/env values. No manifest fields
// are added; runner ownership/trust remains an independent contract.
func ResolveTargetDir(root, requested string) (string, error) {
	if err := naming.Environment(); err != nil {
		return "", err
	}
	if requested == "" {
		requested = os.Getenv("GOXIDE_TARGET_DIR")
	}
	if requested == "" {
		requested = "target"
	}
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	p, e := filepath.Abs(requested)
	if e != nil {
		return "", e
	}
	if e = NoSymlinks(p); e != nil {
		return "", e
	}
	if contained(p, root) {
		return "", fmt.Errorf("target directory must not contain the source root")
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == "vendor" || part == ".git" || part == ".goxide" {
			return "", fmt.Errorf("target directory cannot use reserved source/cache component %s", part)
		}
	}
	return p, nil
}
func OpenLayout(root, module, requested string) (*Layout, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return nil, e
	}
	path, e := ResolveTargetDir(root, requested)
	if e != nil {
		return nil, e
	}
	if e = RejectManagedOutput(path); e != nil {
		return nil, fmt.Errorf("nested target layout: %w", e)
	}
	want := layoutOwner{1, root, module, path}
	if _, e = os.Lstat(path); os.IsNotExist(e) {
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return nil, e
		}
		if e = os.Mkdir(path, 0755); e != nil {
			return nil, e
		}
		if e = writeExclusive(filepath.Join(path, LayoutMarker), want); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else {
		var got layoutOwner
		if e = readJSON(filepath.Join(path, LayoutMarker), &got); e != nil {
			return nil, fmt.Errorf("refusing unmanaged target directory %s: %w", path, e)
		}
		if got != want {
			return nil, fmt.Errorf("target directory belongs to a different project or location: %s", path)
		}
	}
	if e = os.Mkdir(filepath.Join(path, ".layout-lock"), 0700); e != nil {
		return nil, fmt.Errorf("target layout lease unavailable (active build or stale lease): %w", e)
	}
	return &Layout{Root: path, owner: want, active: true}, nil
}
func (l *Layout) Close() error {
	if !l.active {
		return nil
	}
	l.active = false
	return os.Remove(filepath.Join(l.Root, ".layout-lock"))
}

// ManagedSourceDirectory is a read-only scanner guard. Never infer ownership
// merely from a directory's name; copied/damaged/foreign markers are errors.
func ManagedSourceDirectory(path, sourceRoot, module string) (bool, error) {
	if _, e := os.Lstat(filepath.Join(path, LayoutMarker)); os.IsNotExist(e) {
		return false, nil
	} else if e != nil {
		return false, e
	}
	var owner layoutOwner
	if e := readJSON(filepath.Join(path, LayoutMarker), &owner); e != nil {
		return false, e
	}
	expected := layoutOwner{1, sourceRoot, module, path}
	if owner.Module == WorkspaceOwner {
		w, e := DiscoverWorkspace(sourceRoot)
		if e != nil {
			return false, e
		}
		if w != nil && w.Members[pathSource(sourceRoot).key()] == sourceRoot {
			expected = layoutOwner{1, w.Root, WorkspaceOwner, path}
		}
	}
	if owner != expected {
		return false, fmt.Errorf("invalid or foreign managed target directory in source tree: %s", path)
	}
	if e := NoSymlinks(path); e != nil {
		return false, e
	}
	return true, nil
}
func (l *Layout) ownedDirectory(path string, spec UnitSpec) error {
	if path == l.Root || !contained(l.Root, path) {
		return fmt.Errorf("unit path escapes managed layout")
	}
	if !l.active {
		return fmt.Errorf("target layout lease is closed")
	}
	if e := NoSymlinks(path); e != nil {
		return e
	}
	want := unitOwner{l.owner, spec}
	if _, e := os.Lstat(path); os.IsNotExist(e) {
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return e
		}
		if e = os.Mkdir(path, 0755); e != nil {
			return e
		}
		return writeExclusive(filepath.Join(path, ".goxide-unit.json"), want)
	} else if e != nil {
		return e
	}
	var got unitOwner
	if e := readJSON(filepath.Join(path, ".goxide-unit.json"), &got); e != nil {
		return fmt.Errorf("refusing unmanaged unit directory: %w", e)
	}
	if got != want {
		return fmt.Errorf("unit ownership mismatch: %s", path)
	}
	return nil
}
func (l *Layout) Begin(spec UnitSpec) (*Unit, error) {
	if err := validateUnitSpec(spec); err != nil {
		return nil, err
	}
	rel := filepath.Join(spec.Target, spec.Kind, spec.Name, spec.Config)
	dir := filepath.Join(l.Root, "units", rel)
	if e := l.ownedDirectory(dir, spec); e != nil {
		return nil, e
	}
	u := &Unit{Spec: spec, Directory: dir, Bundle: filepath.Join(dir, "bundle"), Intermediate: filepath.Join(dir, "intermediate"), BinaryDirectory: filepath.Join(l.Root, "bin", rel), layout: l}
	if e := NoSymlinks(u.Intermediate); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(u.Intermediate, 0755); e != nil {
		return nil, e
	}
	return u, nil
}

func validateUnitSpec(spec UnitSpec) error {
	for _, s := range []string{spec.Host, spec.Target, spec.Kind, spec.Config} {
		if !ident.MatchString(s) {
			return fmt.Errorf("invalid target layout component %q", s)
		}
	}
	if spec.Kind != "bin" && spec.Kind != "example" && spec.Kind != "lib" && spec.Kind != "proc-macro" {
		return fmt.Errorf("invalid target kind")
	}
	validName := ident.MatchString(spec.Name)
	if spec.Kind == "lib" || spec.Kind == "proc-macro" {
		validName = validName || dependencyIdentifier(spec.Name)
	}
	if !validName {
		return fmt.Errorf("invalid target layout component %q", spec.Name)
	}
	return nil
}

type binaryRecord struct {
	Owner      layoutOwner
	Unit       UnitSpec
	File, Hash string
	Mode       uint32
}

func fileDigest(path string) (string, os.FileMode, error) {
	info, e := os.Lstat(path)
	if e != nil {
		return "", 0, e
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("binary must be a regular file: %s", path)
	}
	f, e := os.Open(path)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", 0, e
	}
	return hex.EncodeToString(h.Sum(nil)), info.Mode().Perm(), nil
}

// PublishBinary replaces only a previously recorded, unmodified artifact.
func (u *Unit) PublishBinary(source string) (string, error) {
	l := u.layout
	if e := l.ownedDirectory(u.BinaryDirectory, u.Spec); e != nil {
		return "", e
	}
	name := u.Spec.Name
	if strings.HasPrefix(u.Spec.Target, "windows-") {
		name += ".exe"
	}
	dest := filepath.Join(u.BinaryDirectory, name)
	recordPath := filepath.Join(u.BinaryDirectory, ".goxide-binary.json")
	var old binaryRecord
	recordErr := readJSON(recordPath, &old)
	if recordErr != nil && !os.IsNotExist(recordErr) {
		return "", recordErr
	}
	if recordErr == nil && (old.Owner != l.owner || old.Unit != u.Spec || old.File != name) {
		return "", fmt.Errorf("binary ownership mismatch")
	}
	if _, e := os.Lstat(dest); e == nil {
		if recordErr != nil {
			return "", fmt.Errorf("refusing unrecorded binary output: %s", dest)
		}
		hash, mode, e := fileDigest(dest)
		if e != nil {
			return "", e
		}
		if hash != old.Hash || uint32(mode) != old.Mode {
			return "", fmt.Errorf("refusing modified binary output: %s", dest)
		}
	} else if !os.IsNotExist(e) {
		return "", e
	}
	hash, mode, e := fileDigest(source)
	if e != nil {
		return "", e
	}
	if recordErr == nil && hash == old.Hash && uint32(mode) == old.Mode {
		if _, e := os.Stat(dest); e == nil {
			return dest, nil
		}
	}
	f, e := os.Open(source)
	if e != nil {
		return "", e
	}
	defer f.Close()
	tmp, e := os.CreateTemp(u.BinaryDirectory, ".binary-")
	if e != nil {
		return "", e
	}
	defer os.Remove(tmp.Name())
	if _, e = io.Copy(tmp, f); e != nil {
		tmp.Close()
		return "", e
	}
	if e = tmp.Chmod(mode); e != nil {
		tmp.Close()
		return "", e
	}
	if e = tmp.Close(); e != nil {
		return "", e
	}
	// A crash between the two renames causes a safe mismatch on the next build.
	rec := binaryRecord{l.owner, u.Spec, name, hash, uint32(mode)}
	b, _ := json.MarshalIndent(rec, "", "  ")
	rt, e := os.CreateTemp(u.BinaryDirectory, ".record-")
	if e != nil {
		return "", e
	}
	defer os.Remove(rt.Name())
	if _, e = rt.Write(append(b, '\n')); e != nil {
		rt.Close()
		return "", e
	}
	if e = rt.Close(); e != nil {
		return "", e
	}
	if e = os.Rename(tmp.Name(), dest); e != nil {
		return "", e
	}
	if e = os.Rename(rt.Name(), recordPath); e != nil {
		return "", e
	}
	return dest, nil
}

// RejectManagedOutput prevents explicit -o from bypassing any layout's lease,
// including a previous --target-dir outside the current source tree.
func RejectManagedOutput(path string) error {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if _, e := os.Lstat(filepath.Join(p, LayoutMarker)); e == nil {
			return fmt.Errorf("explicit output cannot claim a managed layout path: %s", path)
		} else if !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
