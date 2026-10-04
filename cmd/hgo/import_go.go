package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hxsf/goxide/internal/project"
)

type importBins []string

func (b *importBins) String() string     { return strings.Join(*b, ",") }
func (b *importBins) Set(v string) error { *b = append(*b, v); return nil }

func runImportGo(args []string, out, stderr io.Writer) error {
	f := flag.NewFlagSet("import-go", flag.ContinueOnError)
	f.SetOutput(stderr)
	plan := f.Bool("plan", false, "read-only migration plan")
	dest := f.String("out", "", "new migrated project directory (never overwritten)")
	tool := f.String("go", "go", "real Go compiler")
	offline := f.Bool("offline", false, "validate with cached modules only")
	lib := f.String("lib", "", "explicit library directory/file")
	var bins importBins
	f.Var(&bins, "bin", "explicit name=path binary; repeatable")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 1 || (*plan == (*dest != "")) {
		return fmt.Errorf("usage: hgo import-go (--plan | --out NEWDIR) (--lib PATH | --bin NAME=PATH) ROOT")
	}
	if (*lib == "") == (len(bins) == 0) {
		return fmt.Errorf("choose --lib or one or more --bin targets")
	}
	root, e := filepath.Abs(f.Arg(0))
	if e != nil {
		return e
	}
	if e = project.NoSymlinks(root); e != nil {
		return e
	}
	if e = project.CheckWorkspace(root); e != nil {
		return e
	}
	for _, name := range []string{project.ManifestName, project.LegacyManifestName, "goxide.lock", "hgo.lock", "vendor"} {
		if _, e := os.Lstat(filepath.Join(root, name)); e == nil {
			return fmt.Errorf("import-go refuses existing %s; preserve and explicitly migrate that configuration first", name)
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if e = checkDirectoryCompiler(*tool); e != nil {
		return e
	}
	m, e := project.ImportMod(*tool, root)
	if e != nil {
		return e
	}
	if *lib != "" {
		m.Lib = &project.Target{Name: "lib", Path: *lib}
	}
	for _, raw := range bins {
		name, path, ok := strings.Cut(raw, "=")
		if !ok {
			return fmt.Errorf("--bin requires name=path")
		}
		m.Bins = append(m.Bins, project.Target{Name: name, Path: path})
	}
	for i := range m.Native.Replace {
		r := &m.Native.Replace[i]
		if r.Path != "" && !filepath.IsAbs(r.Path) {
			r.Path = filepath.Join(root, r.Path)
		}
	}
	data, e := project.Marshal(m)
	if e != nil {
		return e
	}
	if _, e = project.Parse("candidate/goxide.toml", data); e != nil {
		return e
	}
	if *plan {
		_, e = out.Write(data)
		return e
	}
	final, e := filepath.Abs(*dest)
	if e != nil {
		return e
	}
	if e = project.NoSymlinks(final); e != nil {
		return e
	}
	if within(root, final) {
		return fmt.Errorf("migration destination must be outside source root")
	}
	if e = project.RejectManagedOutput(final); e != nil {
		return e
	}
	if _, e = os.Lstat(final); !os.IsNotExist(e) {
		return fmt.Errorf("migration destination must not exist")
	}
	stage, e := os.MkdirTemp(filepath.Dir(final), ".goxide-import-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	// Explicit import copies this project's regular files, not arbitrary parent trees.
	e = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		if d.Name() == ".git" || d.Name() == ".hgo" || d.Name() == ".goxide-migration" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration source symlink unsupported: %s", rel)
		}
		if d.IsDir() {
			owned, err := project.ManagedSourceDirectory(path, root, m.Module)
			if err != nil {
				return err
			}
			if owned {
				return filepath.SkipDir
			}
			return os.Mkdir(filepath.Join(stage, rel), 0755)
		}
		if rel == "go.mod" || rel == "go.sum" {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("migration special file unsupported: %s", rel)
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(stage, rel), b, info.Mode().Perm())
	})
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, project.ManifestName), data, 0644); e != nil {
		return e
	}
	sums, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	original, e := os.ReadFile(filepath.Join(root, "go.mod"))
	if e != nil {
		return e
	}
	lock := project.Lock{Version: 2, PlanningSums: string(sums), Provenance: project.Digest([]any{original, sums})}
	b, e := json.MarshalIndent(lock, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "goxide.lock"), b, 0644); e != nil {
		return e
	}
	validate := func(flags projectFlags) error {
		var buildOut bytes.Buffer
		flags.offline = *offline
		flags.targetDir = "target"
		if e := runProject("build", stage, "", *tool, nil, flags, &buildOut, stderr); e != nil {
			return fmt.Errorf("migration validation failed; original files unchanged: %w", e)
		}
		return nil
	}
	if m.Lib != nil {
		if e = validate(projectFlags{lib: true}); e != nil {
			return e
		}
	} else {
		for _, t := range m.Bins {
			if e = validate(projectFlags{bin: t.Name}); e != nil {
				return e
			}
		}
	}
	// Ownership and generated absolute paths are staging-only, never published.
	for _, name := range []string{"target", ".hgo"} {
		if e = os.RemoveAll(filepath.Join(stage, name)); e != nil {
			return e
		}
	}
	// Same-filesystem rename publishes the validated candidate; no source deletion.
	if _, e = os.Lstat(final); !os.IsNotExist(e) {
		return fmt.Errorf("migration destination appeared during validation; not overwritten")
	}
	if e = os.Rename(stage, final); e != nil {
		return e
	}
	_, e = fmt.Fprintf(out, "migrated project: %s\noriginal go.mod/go.sum retained at: %s\n", final, root)
	return e
}
