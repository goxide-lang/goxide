package compile

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/project"
	"github.com/goxide-lang/goxide/internal/rootconfig"
)

func moduleRoots(node *project.Node) (paths.Roots, error) {
	if !node.HasManifest {
		return rootconfig.Load(node.Root)
	}
	if _, err := os.Stat(filepath.Join(node.Root, rootconfig.Filename)); err == nil {
		return nil, fmt.Errorf("%s: declare dependencies in goxide.toml instead of %s", node.Root, rootconfig.Filename)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	mappings := map[string]rootconfig.Mapping{}
	for alias := range node.Requests {
		dependency := node.Manifest.Dependencies[alias]
		if resolved, ok := node.Sources[alias]; ok {
			dependency = resolved
		}
		source := "module"
		packagePath := dependency.Module
		if dependency.IsToolchain() {
			source = "toolchain"
			packagePath = dependency.Go
		} else if child := node.Children[alias]; child != nil {
			var err error
			packagePath, err = libraryPackage(child)
			if err != nil {
				return nil, err
			}
		}
		mappings[alias] = rootconfig.Mapping{Module: dependency.Module, Package: packagePath, Source: source}
	}
	roots, err := rootconfig.Build(mappings)
	if err != nil {
		return nil, err
	}
	for alias, child := range node.Children {
		if child.HasManifest {
			root := roots[alias]
			root.GoPrefix = ""
			roots[alias] = root
		}
	}
	for alias := range node.Manifest.Dependencies {
		if _, active := node.Requests[alias]; !active {
			roots[alias] = paths.Namespace{Unavailable: fmt.Sprintf("dependency root %q is not activated", alias)}
		}
	}
	return roots, nil
}

func targetUnavailable(target project.Target, features map[string]bool) string {
	for _, feature := range target.RequiredFeatures {
		if !features[feature] {
			return fmt.Sprintf("target %s requires feature %s", target.Name, feature)
		}
	}
	return ""
}

func targetCrates(node *project.Node, selected project.Target, kind string) ([]lower.Crate, []lower.TargetBoundary, error) {
	var crates []lower.Crate
	var boundaries []lower.TargetBoundary
	packageRoots := map[string]string{}
	groups := []struct {
		kind    string
		targets []project.Target
	}{{"bin", node.Manifest.Bins}, {"example", node.Manifest.Examples}}
	if node.Manifest.Lib != nil {
		groups = append(groups, struct {
			kind    string
			targets []project.Target
		}{"lib", []project.Target{*node.Manifest.Lib}})
	}
	for _, group := range groups {
		for _, target := range group.targets {
			info, err := targetInfo(node.Root, target.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("target %s: %w", target.Name, err)
			}
			source := path.Join(node.Module, filepath.ToSlash(target.Path))
			boundaries = append(boundaries, lower.TargetBoundary{Path: source, File: !info.IsDir()})
			crate := lower.Crate{Name: target.Name, Root: source, SourceDir: source, Library: group.kind == "lib", Unavailable: targetUnavailable(target, node.Features)}
			if !info.IsDir() {
				crate.SourceDir = path.Dir(source)
				crate.Root = path.Join(node.Module, "goxide_targets", group.kind, target.Name)
				if crate.Library {
					crate.Root = node.Module
				}
			}
			label := fmt.Sprintf("%s target %s", group.kind, target.Name)
			if previous, exists := packageRoots[crate.Root]; exists {
				return nil, nil, fmt.Errorf("%s and %s select the same package %s", previous, label, crate.Root)
			}
			packageRoots[crate.Root] = label
			if group.kind != "lib" && (group.kind != kind || target.Name != selected.Name) {
				continue
			}
			crates = append(crates, crate)
		}
	}
	return crates, boundaries, nil
}

func libraryPackage(node *project.Node) (string, error) {
	if library := node.Manifest.Lib; library != nil {
		info, err := targetInfo(node.Root, library.Path)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return path.Join(node.Module, filepath.ToSlash(library.Path)), nil
		}
	}
	return node.Module, nil
}

func stageSources(node *project.Node, selected project.Target, kind, output string) (string, error) {
	files := map[string]string{}
	excluded := map[string]bool{}
	targets := append(append([]project.Target(nil), node.Manifest.Bins...), node.Manifest.Examples...)
	for _, target := range targets {
		info, err := targetInfo(node.Root, target.Path)
		if err != nil {
			return "", fmt.Errorf("target %s: %w", target.Name, err)
		}
		name := filepath.Clean(target.Path)
		if info.IsDir() {
			if name != "." {
				excluded[name] = true
			}
		} else {
			files[name] = ""
		}
	}
	if library := node.Manifest.Lib; library != nil {
		info, err := targetInfo(node.Root, library.Path)
		if err != nil {
			return "", err
		}
		if targetUnavailable(*library, node.Features) == "" {
			if !info.IsDir() {
				files[filepath.Clean(library.Path)] = filepath.Base(library.Path)
			}
		} else if info.IsDir() {
			if name := filepath.Clean(library.Path); name != "." {
				excluded[name] = true
			}
		} else {
			files[filepath.Clean(library.Path)] = ""
		}
	}
	info, err := targetInfo(node.Root, selected.Path)
	if err != nil {
		return "", fmt.Errorf("target %s: %w", selected.Name, err)
	}
	entry := filepath.Clean(selected.Path)
	if info.IsDir() {
		delete(excluded, entry)
	} else {
		entry = filepath.Join("goxide_targets", kind, selected.Name)
		if kind == "lib" {
			entry = "."
		}
		files[filepath.Clean(selected.Path)] = filepath.Join(entry, filepath.Base(selected.Path))
	}
	// Keep ancestor directories traversable when they contain the selected
	// target or a library entry; crate ownership selects packages afterwards.
	included := []string{selected.Path}
	if library := node.Manifest.Lib; library != nil && targetUnavailable(*library, node.Features) == "" {
		included = append(included, library.Path)
	}
	for directory := range excluded {
		for _, target := range included {
			if within(filepath.Join(node.Root, directory), filepath.Join(node.Root, target)) {
				delete(excluded, directory)
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(node.Root, "goxide_targets")); err == nil {
		return "", fmt.Errorf("%s: goxide_targets is reserved for generated entries", node.Module)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return "", err
	}
	err = filepath.WalkDir(node.Root, func(name string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == node.Root {
			return nil
		}
		if item.Name() == ".git" {
			if item.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(node.Root, name)
		if err != nil {
			return err
		}
		if item.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("source module contains symlink: %s", name)
		}
		if item.IsDir() {
			if item.Name() == ".goxide" || item.Name() == "vendor" || item.Name() == "goxide.lock.guard" || excluded[relative] {
				return filepath.SkipDir
			}
			managed, err := project.ManagedSourceDirectory(name, node.Root, node.Module)
			if err != nil {
				return err
			}
			if managed {
				return filepath.SkipDir
			}
			for _, boundary := range []string{"go.mod", project.ManifestName} {
				if _, err := os.Stat(filepath.Join(name, boundary)); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return nil
		}
		if relative == "go.mod" || relative == "go.work" || relative == "go.work.sum" || relative == project.ManifestName || relative == "goxide.lock" {
			return nil
		}
		if destination, exists := files[relative]; exists {
			if destination == "" {
				return nil
			}
			relative = destination
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source must be a regular file: %s", name)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		destination := filepath.Join(output, relative)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(output, "go.mod"), node.ModBytes, 0o644); err != nil {
		return "", err
	}
	return entry, nil
}

func targetInfo(root, relative string) (os.FileInfo, error) {
	name := filepath.Join(root, relative)
	if !within(root, name) {
		return nil, fmt.Errorf("target escapes module")
	}
	if err := project.NoSymlinks(name); err != nil {
		return nil, err
	}
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	directory := name
	if !info.IsDir() {
		if filepath.Ext(name) != ".gox" && filepath.Ext(name) != ".go" {
			return nil, fmt.Errorf("target must be a package directory or .gox/.go entry file")
		}
		directory = filepath.Dir(name)
	}
	for dir := directory; dir != root; dir = filepath.Dir(dir) {
		for _, boundary := range []string{"go.mod", project.ManifestName} {
			if _, err := os.Stat(filepath.Join(dir, boundary)); err == nil {
				return nil, fmt.Errorf("target crosses nested module boundary: %s", dir)
			} else if !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	return info, nil
}
