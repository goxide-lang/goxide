package refs

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

const MetadataFilename = ".goxide.refs.json"
const ArtifactVersion = 1

// MetadataArtifact binds permission analysis to one selected generated Go source
// set and the enhanced signatures it consumed. It is not an authenticated ABI.
type MetadataArtifact struct {
	Metadata
	ArtifactVersion int
	Content         string
	Files           map[string]string
	Dependencies    map[string]DependencyFingerprint
}
type DependencyFingerprint struct {
	Marker   string
	Content  string
	Artifact string
}
type ArtifactImporter interface {
	types.Importer
	PackageFiles(string) (string, []string, error)
	PackageFingerprint(string) (string, error)
}

func artifactDependencies(files map[string][]byte, imp types.Importer) (map[string]DependencyFingerprint, error) {
	deps := map[string]DependencyFingerprint{}
	for _, name := range sourceNames(files) {
		f, e := parser.ParseFile(token.NewFileSet(), name, files[name], parser.ImportsOnly)
		if e != nil {
			return nil, e
		}
		for _, s := range f.Imports {
			path, _ := strconv.Unquote(s.Path.Value)
			if path == "C" {
				continue
			}
			provider, ok := imp.(ArtifactImporter)
			if !ok {
				return nil, fmt.Errorf("source fingerprint importer required for %s", path)
			}
			content, e := provider.PackageFingerprint(path)
			if e != nil {
				return nil, e
			}
			fingerprint := DependencyFingerprint{Content: content}
			pkg, e := imp.Import(path)
			if e != nil {
				return nil, e
			}
			if obj := pkg.Scope().Lookup(MetadataMarker); obj != nil {
				marker, ok := obj.(*types.Const)
				if !ok || marker.Val().Kind() != constant.String {
					return nil, fmt.Errorf("invalid reference metadata marker for %s", path)
				}
				fingerprint.Marker = constant.StringVal(marker.Val())
				dir, _, e := provider.PackageFiles(path)
				if e != nil {
					return nil, e
				}
				data, e := os.ReadFile(filepath.Join(dir, MetadataFilename))
				if e != nil {
					return nil, e
				}
				fingerprint.Artifact = fmt.Sprintf("%x", sha256.Sum256(data))
			}
			deps[path] = fingerprint
		}
	}
	return deps, nil
}
func MarshalMetadataArtifact(meta Metadata, files map[string][]byte, imp types.Importer) ([]byte, error) {
	deps, e := artifactDependencies(files, imp)
	if e != nil {
		return nil, e
	}
	provider, ok := imp.(ArtifactImporter)
	if !ok {
		return nil, fmt.Errorf("source fingerprint importer required for %s", meta.Path)
	}
	content, e := provider.PackageFingerprint(meta.Path)
	if e != nil {
		return nil, e
	}
	a := MetadataArtifact{Content: content, Metadata: meta, ArtifactVersion: ArtifactVersion, Files: map[string]string{}, Dependencies: deps}
	for name, data := range files {
		a.Files[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	data, e := json.MarshalIndent(a, "", "  ")
	return append(data, '\n'), e
}
func LoadMetadataArtifact(path string, provider ArtifactImporter) (Metadata, error) {
	checked := map[string]Metadata{}
	active := map[string]bool{}
	var load func(string) (Metadata, error)
	load = func(path string) (Metadata, error) {
		bad := func(reason string) (Metadata, error) {
			return Metadata{}, fmt.Errorf("reference permission metadata missing, stale or wrong package for %s: %s", path, reason)
		}
		if m, ok := checked[path]; ok {
			return m, nil
		}
		if active[path] {
			return bad("cyclic metadata dependencies")
		}
		active[path] = true
		dir, names, e := provider.PackageFiles(path)
		if e != nil {
			return bad(e.Error())
		}
		data, e := os.ReadFile(filepath.Join(dir, MetadataFilename))
		if e != nil {
			return bad("cannot read " + MetadataFilename + ": " + e.Error())
		}
		var a MetadataArtifact
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&a); e != nil {
			return bad("invalid metadata JSON: " + e.Error())
		}
		if e = decoder.Decode(new(any)); e != io.EOF {
			return bad("trailing metadata JSON")
		}
		if a.ArtifactVersion != ArtifactVersion || a.Version != MetadataVersion {
			return bad("unsupported metadata version")
		}
		if a.Path != path {
			return bad("metadata package path mismatch")
		}
		if len(a.Files) == 0 || len(a.Files) != len(names) {
			return bad("selected Go source set mismatch; regenerate for this build configuration")
		}
		files := map[string][]byte{}
		for _, name := range names {
			if filepath.Base(name) != name {
				return bad("invalid selected Go filename")
			}
			digest, ok := a.Files[name]
			if !ok {
				return bad("selected Go source set mismatch: " + name)
			}
			source, e := os.ReadFile(filepath.Join(dir, name))
			if e != nil {
				return bad(e.Error())
			}
			if fmt.Sprintf("%x", sha256.Sum256(source)) != digest {
				return bad("Go source content mismatch: " + name)
			}
			files[name] = source
		}
		pkg, e := provider.Import(path)
		if e != nil {
			return bad(e.Error())
		}
		marker, ok := pkg.Scope().Lookup(MetadataMarker).(*types.Const)
		if !ok || marker.Val().Kind() != constant.String || constant.StringVal(marker.Val()) != a.Metadata.Marker() {
			return bad("metadata marker digest mismatch")
		}
		deps, e := artifactDependencies(files, provider)
		if e != nil {
			return bad(e.Error())
		}
		if a.Dependencies == nil || len(a.Dependencies) != len(deps) {
			return bad("enhanced dependency set mismatch")
		}
		depNames := make([]string, 0, len(deps))
		for dep := range deps {
			depNames = append(depNames, dep)
		}
		sort.Strings(depNames)
		for _, dep := range depNames {
			if a.Dependencies[dep].Marker != deps[dep].Marker {
				return bad("enhanced dependency marker changed: " + dep)
			}
			if deps[dep].Marker != "" {
				if _, e = load(dep); e != nil {
					return bad("dependency " + dep + ": " + e.Error())
				}
			}
			if a.Dependencies[dep].Content != deps[dep].Content {
				return bad("dependency source closure changed: " + dep)
			}
			if a.Dependencies[dep].Artifact != deps[dep].Artifact {
				return bad("enhanced dependency artifact changed: " + dep)
			}
		}
		content, e := provider.PackageFingerprint(path)
		if e != nil {
			return bad(e.Error())
		}
		if a.Content != content {
			return bad("package source closure or build configuration changed")
		}
		active[path] = false
		checked[path] = a.Metadata
		return a.Metadata, nil
	}
	return load(path)
}
