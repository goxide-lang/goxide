package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/refs"
)

const protocolMetadataMarker = "GoxideProtocolMetadata"
const protocolMetadataFilename = ".goxide.protocols.json"
const protocolArtifactVersion = 1
const protocolArtifactLimit = 4 << 20

type protocolDependencyFingerprint struct {
	Content   string
	Artifacts map[string]protocolArtifactFingerprint
}

type protocolArtifactFingerprint struct{ Marker, Digest string }

// The envelope pins the Go build view which supplied both the actual types and
// enhanced protocol declarations. Hashes establish consistency, not authorship.
type protocolArtifact struct {
	ArtifactVersion int
	Metadata        protocols.Metadata
	Files           map[string]string
	Content         string
	Dependencies    map[string]protocolDependencyFingerprint
}

var protocolDependencyArtifacts = []struct{ marker, filename string }{
	{protocolMetadataMarker, protocolMetadataFilename},
	{refs.MetadataMarker, refs.MetadataFilename},
	{useMetadataMarker, useMetadataFilename},
}

func readProtocolArtifactFile(dir, name string) ([]byte, error) {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, protocolArtifactLimit+1))
	if err == nil && len(b) > protocolArtifactLimit {
		return nil, fmt.Errorf("protocol metadata exceeds %d bytes", protocolArtifactLimit)
	}
	return b, err
}

func protocolArtifactDependencies(meta protocols.Metadata, files map[string][]byte, imp refs.ArtifactImporter) (map[string]protocolDependencyFingerprint, error) {
	owners := map[string]bool{}
	for _, name := range sortedSources(files) {
		f, err := parser.ParseFile(token.NewFileSet(), name, files[name], parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, err
			}
			owners[path] = true
		}
	}
	for _, d := range meta.Definitions {
		owners[d.Owner] = true
	}
	for _, impl := range meta.Implementations {
		owners[impl.Protocol.Owner], owners[impl.Target.Owner] = true, true
	}
	for _, function := range meta.Functions {
		for _, bound := range function.Bounds {
			owners[bound.Protocol.Owner] = true
		}
	}
	delete(owners, "")
	delete(owners, "C")
	delete(owners, meta.Path)
	paths := make([]string, 0, len(owners))
	for path := range owners {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	deps := map[string]protocolDependencyFingerprint{}
	for _, path := range paths {
		content, err := imp.PackageFingerprint(path)
		if err != nil {
			return nil, err
		}
		pkg, err := imp.Import(path)
		if err != nil {
			return nil, err
		}
		if pkg == nil || pkg.Path() != path {
			return nil, fmt.Errorf("protocol dependency identity mismatch for %s", path)
		}
		dep := protocolDependencyFingerprint{Content: content, Artifacts: map[string]protocolArtifactFingerprint{}}
		for _, kind := range protocolDependencyArtifacts {
			marker, err := enumPackageMarker(pkg, kind.marker)
			if err != nil {
				return nil, err
			}
			if marker == "" {
				continue
			}
			dir, _, err := imp.PackageFiles(path)
			if err != nil {
				return nil, err
			}
			data, err := readProtocolArtifactFile(dir, kind.filename)
			if err != nil {
				return nil, err
			}
			dep.Artifacts[kind.filename] = protocolArtifactFingerprint{marker, enumDigest(data)}
		}
		deps[path] = dep
	}
	return deps, nil
}

func marshalProtocolArtifact(meta protocols.Metadata, files map[string][]byte, imp types.Importer) ([]byte, error) {
	provider, ok := imp.(refs.ArtifactImporter)
	if !ok {
		return nil, fmt.Errorf("protocol metadata source importer unavailable for %s", meta.Path)
	}
	deps, err := protocolArtifactDependencies(meta, files, provider)
	if err != nil {
		return nil, err
	}
	content, err := provider.PackageFingerprint(meta.Path)
	if err != nil {
		return nil, err
	}
	a := protocolArtifact{ArtifactVersion: protocolArtifactVersion, Metadata: meta, Files: map[string]string{}, Content: content, Dependencies: deps}
	for name, source := range files {
		if filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid protocol artifact Go filename: %s", name)
		}
		a.Files[name] = enumDigest(source)
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b)+1 > protocolArtifactLimit {
		return nil, fmt.Errorf("protocol metadata exceeds %d bytes", protocolArtifactLimit)
	}
	return append(b, '\n'), nil
}

type protocolArtifactLoader struct {
	imp     types.Importer
	checked map[string]protocols.Metadata
	active  map[string]bool
}

func newProtocolArtifactLoader(imp types.Importer) *protocolArtifactLoader {
	return &protocolArtifactLoader{imp: imp, checked: map[string]protocols.Metadata{}, active: map[string]bool{}}
}

// Ordinary Go packages without the marker carry no source protocol evidence.
// A present marker always requires the matching selected-source artifact.
func loadProtocolArtifact(path string, imp types.Importer) (protocols.Metadata, error) {
	return newProtocolArtifactLoader(imp).load(path)
}

func (l *protocolArtifactLoader) load(path string) (protocols.Metadata, error) {
	bad := func(reason string) (protocols.Metadata, error) {
		return protocols.Metadata{}, fmt.Errorf("protocol metadata missing, stale or wrong package for %s: %s", path, reason)
	}
	if m, ok := l.checked[path]; ok {
		return m, nil
	}
	if l.active[path] {
		return bad("cyclic metadata dependencies")
	}
	if l.imp == nil {
		return bad("package importer unavailable")
	}
	pkg, err := l.imp.Import(path)
	if err != nil {
		return bad(err.Error())
	}
	if pkg == nil || pkg.Path() != path {
		return bad("imported package identity mismatch")
	}
	marker, err := enumPackageMarker(pkg, protocolMetadataMarker)
	if err != nil {
		return bad(err.Error())
	}
	if marker == "" {
		return protocols.Metadata{}, nil
	}
	provider, ok := l.imp.(refs.ArtifactImporter)
	if !ok {
		return bad("protocol metadata source importer unavailable")
	}
	l.active[path] = true
	defer delete(l.active, path)
	dir, names, err := provider.PackageFiles(path)
	if err != nil {
		return bad(err.Error())
	}
	data, err := readProtocolArtifactFile(dir, protocolMetadataFilename)
	if err != nil {
		return bad("cannot read " + protocolMetadataFilename + ": " + err.Error())
	}
	if err := enumUniqueJSON(data); err != nil {
		return bad("invalid metadata JSON: " + err.Error())
	}
	var a protocolArtifact
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&a); err != nil {
		return bad("invalid metadata JSON: " + err.Error())
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return bad("trailing metadata JSON")
	}
	if a.ArtifactVersion != protocolArtifactVersion || a.Metadata.Version != protocols.MetadataVersion {
		return bad("unsupported metadata version; regenerate producer artifacts")
	}
	if a.Metadata.Path != path || a.Metadata.Package != pkg.Name() {
		return bad("metadata package identity mismatch")
	}
	if !a.Metadata.Present() || a.Metadata.Marker() != marker {
		return bad("metadata marker digest mismatch")
	}
	if len(a.Files) == 0 || len(a.Files) != len(names) {
		return bad("selected Go source set mismatch")
	}
	files := map[string][]byte{}
	for _, name := range names {
		if filepath.Base(name) != name {
			return bad("invalid selected Go filename")
		}
		if _, duplicate := files[name]; duplicate {
			return bad("duplicate selected Go filename")
		}
		source, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return bad(err.Error())
		}
		if a.Files[name] != enumDigest(source) {
			return bad("Go source content mismatch: " + name)
		}
		files[name] = source
	}
	deps, err := protocolArtifactDependencies(a.Metadata, files, provider)
	if err != nil {
		return bad(err.Error())
	}
	want, _ := json.Marshal(deps)
	got, _ := json.Marshal(a.Dependencies)
	if !bytes.Equal(want, got) {
		return bad("dependency source closure or enhanced artifact changed")
	}
	for _, dep := range sortedProtocolDependencies(deps) {
		if _, required := deps[dep].Artifacts[protocolMetadataFilename]; required {
			if _, err := l.load(dep); err != nil {
				return bad("dependency " + dep + ": " + err.Error())
			}
		}
		if _, required := deps[dep].Artifacts[refs.MetadataFilename]; required {
			if _, err := refs.LoadMetadataArtifact(dep, provider); err != nil {
				return bad("reference dependency " + dep + ": " + err.Error())
			}
		}
	}
	content, err := provider.PackageFingerprint(path)
	if err != nil {
		return bad(err.Error())
	}
	if a.Content != content {
		return bad("package source closure or build configuration changed")
	}
	if err := validateProtocolMetadata(a.Metadata, pkg, l.imp); err != nil {
		return bad(err.Error())
	}
	if err := validateProtocolCanonicalOwners(a.Metadata, l.checked); err != nil {
		return bad(err.Error())
	}
	l.checked[path] = a.Metadata
	return a.Metadata, nil
}

func sortedProtocolDependencies(deps map[string]protocolDependencyFingerprint) []string {
	paths := make([]string, 0, len(deps))
	for path := range deps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
