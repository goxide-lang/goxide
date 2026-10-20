package project

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// Logical identities exclude acquisition directories and Go's selected version.
type sourceIdentity struct {
	Kind, Path, Repo, Commit, Subdir, Module string
	Version, Content                         string
}

func pathSource(root string) sourceIdentity {
	return sourceIdentity{Kind: "path", Path: filepath.Clean(root)}
}

func selectedSource(module string, selected Selection) sourceIdentity {
	if source := effective(selected); source.Version == "" {
		return pathSource(source.Dir)
	}
	return sourceIdentity{Kind: "go", Module: module, Version: selected.Version, Content: Digest(selected)}
}

func (s sourceIdentity) description() string {
	switch s.Kind {
	case "path":
		return s.Path
	case "git":
		text := s.Repo + "@" + s.Commit
		if s.Subdir != "" {
			text += " (subdir " + s.Subdir + ")"
		}
		return text
	default:
		return s.Module + "@" + s.Version
	}
}

func (s sourceIdentity) key() string {
	switch s.Kind {
	case "path":
		return "path:" + filepath.ToSlash(s.Path)
	case "git":
		return "git:" + Digest([]string{s.Repo, s.Commit, s.Subdir})
	default:
		if s.Subdir != "" {
			return "go:" + s.Module + ":" + s.Subdir
		}
		return "go:" + s.Module
	}
}

func (n *Node) setSource(source sourceIdentity) {
	n.source = source
	n.SourceKey = source.key()
}

func (n *Node) lockSourceKey(ownerRoot string) string {
	source := n.source
	if source.Kind == "path" {
		if relative, err := filepath.Rel(ownerRoot, source.Path); err == nil {
			source.Path = "./" + filepath.ToSlash(relative)
		}
	}
	return source.key()
}

func gitIdentity(repository, commit, subdir string) (sourceIdentity, error) {
	if filepath.IsAbs(repository) {
		canonical, err := filepath.EvalSymlinks(repository)
		if err != nil {
			return sourceIdentity{}, err
		}
		repository = canonical
	} else {
		u, err := url.Parse(repository)
		if err != nil {
			return sourceIdentity{}, err
		}
		if u.Scheme == "file" {
			canonical, err := filepath.EvalSymlinks(u.Path)
			if err != nil {
				return sourceIdentity{}, err
			}
			repository = canonical
		} else {
			u.Host = strings.ToLower(u.Host)
			u.Path = strings.TrimSuffix(u.Path, "/")
			repository = u.String()
		}
	}
	subdir = path.Clean(filepath.ToSlash(subdir))
	if subdir == "." {
		subdir = ""
	}
	return sourceIdentity{Kind: "git", Repo: repository, Commit: commit, Subdir: subdir}, nil
}
