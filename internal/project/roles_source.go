package project

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A source must be classified before its metadata can participate in MVS. In
// particular, a macro's native require directives belong to its own program.
func (g *Graph) roleSelection(module, version string) (Selection, error) {
	replacements, err := normalizedReplaces(g.rolePlanningRoot(), g.RootMod)
	if err != nil {
		return Selection{}, err
	}
	var replacement *ModReplace
	for index := range replacements {
		candidate := &replacements[index]
		if candidate.Old.Path == module && (candidate.Old.Version == "" || candidate.Old.Version == version) {
			if replacement == nil || candidate.Old.Version != "" {
				replacement = candidate
			}
		}
	}
	key := Digest([]any{module, version, replacement})
	if previous, ok := g.roles.selections[key]; ok {
		return previous, nil
	}
	selected := Selection{Path: module, Version: version}
	actualModule, actualVersion := module, version
	if replacement != nil {
		actualModule, actualVersion = replacement.New.Path, replacement.New.Version
		if actualVersion == "" {
			selected.Replace = &Selection{Path: actualModule, Dir: actualModule}
			g.roles.selections[key] = selected
			return selected, nil
		}
	}
	work, err := os.MkdirTemp(g.WorkRoot, "role-source-")
	if err != nil {
		return Selection{}, err
	}
	defer os.RemoveAll(work)
	metadata := fmt.Sprintf("module %ssource\n\ngo %s\n", plannerPrefix, g.RootMod.Go)
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(metadata), 0644); err != nil {
		return Selection{}, err
	}
	if err := os.WriteFile(filepath.Join(work, "go.sum"), []byte(g.roles.owner.Lock.PlanningSums), 0644); err != nil {
		return Selection{}, err
	}
	data, err := goCommand(g.Tool, work, g.Offline, g.Environment, "mod", "download", "-json", actualModule+"@"+actualVersion)
	if err != nil {
		return Selection{}, err
	}
	var downloaded struct {
		Path, Version, Sum, GoModSum, Dir, GoMod, Zip string
	}
	if err := json.Unmarshal(data, &downloaded); err != nil {
		return Selection{}, err
	}
	if downloaded.Path == "" || downloaded.Version == "" || downloaded.Sum == "" || downloaded.GoModSum == "" || downloaded.Dir == "" || downloaded.Zip == "" {
		return Selection{}, fmt.Errorf("download lacks verified source identity: %s@%s", actualModule, actualVersion)
	}
	verified := Selection{Path: downloaded.Path, Version: downloaded.Version, Sum: downloaded.Sum, GoModSum: downloaded.GoModSum, Dir: downloaded.Dir, GoMod: downloaded.GoMod}
	if err := verifyModuleDownload(verified, downloaded.Zip); err != nil {
		return Selection{}, err
	}
	if present, err := Detect(verified.Dir); err != nil {
		return Selection{}, err
	} else if present {
		if err := NoSymlinks(verified.GoMod); err != nil {
			return Selection{}, err
		}
		metadata, err := os.ReadFile(verified.GoMod)
		if err != nil {
			return Selection{}, err
		}
		source, err := os.ReadFile(filepath.Join(verified.Dir, "go.mod"))
		if err != nil {
			return Selection{}, err
		}
		if !bytes.Equal(metadata, source) {
			return Selection{}, fmt.Errorf("goxide module go.mod differs between selected metadata and verified source: %s", module)
		}
	}
	if replacement != nil {
		selected.Replace = &verified
	} else {
		selected = verified
	}
	if data, err := os.ReadFile(filepath.Join(work, "go.sum")); err == nil {
		g.roles.mergeSums(string(data))
	} else if !os.IsNotExist(err) {
		return Selection{}, err
	}
	g.roles.selections[key] = selected
	return selected, nil
}

func (g *Graph) rolePlanningRoot() string {
	if g.roleRoot != nil {
		return g.roleRoot.Root
	}
	return g.ownerRoot
}

// Verify just the acquired remote module. Graph-wide `go mod verify` also
// inspects replaced logical paths, including local-only dotless module names,
// and would cross a macro boundary before classification.
func verifyModuleDownload(selection Selection, zipPath string) error {
	if err := NoSymlinks(selection.Dir); err != nil {
		return err
	}
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer archive.Close()
	files := map[string]*zip.File{}
	var names []string
	for _, file := range archive.File {
		files[file.Name] = file
		names = append(names, file.Name)
	}
	zipSum, err := roleFileHash(names, func(name string) (io.ReadCloser, error) { return files[name].Open() })
	if err != nil {
		return err
	}
	if zipSum != selection.Sum {
		return fmt.Errorf("module source verification: %s@%s zip has been modified", selection.Path, selection.Version)
	}
	names = nil
	prefix := selection.Path + "@" + selection.Version + "/"
	err = filepath.WalkDir(selection.Dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("module source verification: unexpected symlink %s", name)
		}
		if !entry.IsDir() {
			relative, err := filepath.Rel(selection.Dir, name)
			if err != nil {
				return err
			}
			names = append(names, prefix+filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return err
	}
	dirSum, err := roleFileHash(names, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(selection.Dir, filepath.FromSlash(strings.TrimPrefix(name, prefix))))
	})
	if err != nil {
		return err
	}
	if dirSum != selection.Sum {
		return fmt.Errorf("module source verification: %s@%s directory has been modified", selection.Path, selection.Version)
	}
	return nil
}

// Go's h1 checksum hashes sorted "sha256  filename\n" lines. It is independent
// of zip encoding, timestamps, and acquisition directories.
func roleFileHash(names []string, open func(string) (io.ReadCloser, error)) (string, error) {
	sort.Strings(names)
	summary := sha256.New()
	for _, name := range names {
		if strings.Contains(name, "\n") {
			return "", fmt.Errorf("source filename contains a newline")
		}
		file, err := open(name)
		if err != nil {
			return "", err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		closeErr := file.Close()
		if err != nil {
			return "", err
		}
		if closeErr != nil {
			return "", closeErr
		}
		fmt.Fprintf(summary, "%x  %s\n", hash.Sum(nil), name)
	}
	return "h1:" + base64.StdEncoding.EncodeToString(summary.Sum(nil)), nil
}

func (p *rolePlanner) sourceContent(node *Node) (string, error) {
	if node.source.Content != "" {
		return node.source.Content, nil
	}
	if previous, ok := p.contents[node.Root]; ok {
		return previous, nil
	}
	var names []string
	err := filepath.WalkDir(node.Root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == node.Root {
			return nil
		}
		if entry.Name() == ".git" || entry.Name() == "goxide.lock.guard" || name == p.layoutRoot {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source module contains symlink: %s", name)
		}
		if entry.IsDir() {
			if entry.Name() == ".goxide" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			managed, err := ManagedSourceDirectory(name, node.Root, node.Module)
			if err != nil {
				return err
			}
			if managed {
				return filepath.SkipDir
			}
			for _, boundary := range []string{ManifestName, "go.mod"} {
				if _, err := os.Stat(filepath.Join(name, boundary)); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		relative, err := filepath.Rel(node.Root, name)
		if err != nil {
			return err
		}
		if relative != "goxide.lock" {
			names = append(names, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	content, err := roleFileHash(names, func(name string) (io.ReadCloser, error) {
		return os.Open(filepath.Join(node.Root, filepath.FromSlash(name)))
	})
	if err == nil {
		p.contents[node.Root] = content
	}
	return content, err
}
