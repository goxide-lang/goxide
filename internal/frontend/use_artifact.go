package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	"go/constant"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/hxsf/goxide/internal/refs"
)

const useMetadataVersion = 3

const useMetadataMarker = "GoxideUseMetadata"
const useMetadataFilename = ".goxide.uses.json"

type useArtifact struct {
	Version  int
	Metadata useMetadata
	Content  string
	Files    map[string]string
}

func (m useMetadata) present() bool  { return len(m.Bindings) > 0 || len(m.Unsupported) > 0 }
func (m useMetadata) marker() string { b, _ := json.Marshal(m); return enumDigest(b) }
func (u *usePackage) load(path string) (useMetadata, error) {
	if p, ok := u.known[path]; ok && p.uses.present() {
		return p.uses, nil
	}
	if u.imp == nil {
		return useMetadata{}, nil
	}
	pkg, e := u.imp.Import(path)
	if e != nil {
		return useMetadata{}, e
	}
	if e := naming.Package(pkg); e != nil {
		return useMetadata{}, e
	}
	obj := pkg.Scope().Lookup(useMetadataMarker)
	if obj == nil {
		return useMetadata{}, nil
	}
	c, ok := obj.(*types.Const)
	if !ok || c.Val().Kind() != constant.String {
		return useMetadata{}, fmt.Errorf("invalid use metadata marker in %s", path)
	}
	provider, ok := u.imp.(refs.ArtifactImporter)
	if !ok {
		return useMetadata{}, fmt.Errorf("use metadata source importer unavailable for %s", path)
	}
	dir, names, e := provider.PackageFiles(path)
	if e != nil {
		return useMetadata{}, e
	}
	handle, e := os.Open(filepath.Join(dir, useMetadataFilename))
	if e != nil {
		return useMetadata{}, fmt.Errorf("use metadata for %s: %w", path, e)
	}
	b, e := io.ReadAll(io.LimitReader(handle, (4<<20)+1))
	closeErr := handle.Close()
	if e != nil {
		return useMetadata{}, e
	}
	if closeErr != nil {
		return useMetadata{}, closeErr
	}
	if len(b) > 4<<20 {
		return useMetadata{}, fmt.Errorf("use metadata too large for %s", path)
	}
	var a useArtifact
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&a); e != nil {
		return useMetadata{}, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return useMetadata{}, fmt.Errorf("use metadata must contain exactly one JSON object for %s", path)
	}
	if a.Version != useMetadataVersion || a.Metadata.Version != useMetadataVersion || a.Metadata.Path != path || a.Metadata.Package != pkg.Name() || a.Metadata.marker() != constant.StringVal(c.Val()) {
		return useMetadata{}, fmt.Errorf("use metadata marker/schema mismatch for %s", path)
	}
	if len(a.Files) != len(names) {
		return useMetadata{}, fmt.Errorf("use metadata selected source set mismatch for %s", path)
	}
	for _, name := range names {
		data, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return useMetadata{}, e
		}
		if a.Files[name] != enumDigest(data) {
			return useMetadata{}, fmt.Errorf("stale use metadata source %s/%s", path, name)
		}
	}
	content, e := provider.PackageFingerprint(path)
	if e != nil {
		return useMetadata{}, e
	}
	if content != a.Content {
		return useMetadata{}, fmt.Errorf("stale use metadata build/dependency fingerprint for %s", path)
	}
	seen := map[string]bool{}
	for _, b := range a.Metadata.Bindings {
		if seen[b.Name] || !paths.ValidIdentifier(b.Name) || !b.Public {
			return useMetadata{}, fmt.Errorf("invalid use binding metadata in %s", path)
		}
		seen[b.Name] = true
		if b.Kind == "namespace" {
			if b.Namespace == nil || b.RoutePath != b.Namespace.PackagePath || b.OriginPath != b.Namespace.PackagePath || b.RouteName != b.Name || b.OriginName != b.Name {
				return useMetadata{}, fmt.Errorf("missing namespace metadata in %s", path)
			}
			if err := u.validateNamespaceMetadata(*b.Namespace, 0); err != nil {
				return useMetadata{}, err
			}
			continue
		}
		if b.Kind == "derive" {
			def, ok := derives.Lookup(b.OriginPath, b.OriginName)
			if !ok || b.RoutePath != b.OriginPath || b.RouteName != b.OriginName {
				return useMetadata{}, fmt.Errorf("invalid derive use owner in %s", path)
			}
			if err := derives.Validate(def, u.imp); err != nil {
				return useMetadata{}, err
			}
			continue
		}
		if b.RoutePath == "" || b.RouteName == "" || b.OriginPath == "" || b.OriginName == "" {
			return useMetadata{}, fmt.Errorf("invalid use binding metadata in %s", path)
		}
		switch b.Kind {
		case "type", "constant", "function", "variable":
		default:
			return useMetadata{}, fmt.Errorf("unknown use binding kind in %s", path)
		}
		route, e := u.imp.Import(b.RoutePath)
		if e != nil {
			return useMetadata{}, e
		}
		if route == nil {
			return useMetadata{}, fmt.Errorf("use route package unavailable: %s", b.RoutePath)
		}
		obj := route.Scope().Lookup(b.RouteName)
		if obj == nil || (b.RoutePath != path && !obj.Exported()) {
			return useMetadata{}, fmt.Errorf("use route is not accessible: %s.%s", b.RoutePath, b.RouteName)
		}
		actual, e := nativeUseBinding(obj, b.RoutePath, b.RouteName)
		if e != nil || actual.Kind != b.Kind {
			return useMetadata{}, fmt.Errorf("use route kind mismatch for %s.%s", path, b.Name)
		}
		if b.Kind == "type" && (actual.OriginPath != b.OriginPath || actual.OriginName != b.OriginName) {
			return useMetadata{}, fmt.Errorf("use type identity mismatch for %s.%s", path, b.Name)
		}
		if b.Kind == "type" {
			if b.IdentityPath == "" || b.IdentityName == "" {
				return useMetadata{}, fmt.Errorf("missing use type identity route")
			}
			originalPkg, e := u.imp.Import(b.IdentityPath)
			if e != nil {
				return useMetadata{}, e
			}
			if originalPkg == nil {
				return useMetadata{}, fmt.Errorf("use identity package unavailable")
			}
			original, ok := originalPkg.Scope().Lookup(b.IdentityName).(*types.TypeName)
			alias, aliasOK := obj.(*types.TypeName)
			if !ok || !aliasOK || !sameUseType(original.Type(), alias.Type()) {
				return useMetadata{}, fmt.Errorf("use type argument/identity mismatch for %s.%s", path, b.Name)
			}
		}
		if b.Kind == "constant" {
			origin, e := u.imp.Import(b.OriginPath)
			if e != nil {
				return useMetadata{}, e
			}
			if origin == nil {
				return useMetadata{}, fmt.Errorf("use constant origin unavailable")
			}
			original, ok := origin.Scope().Lookup(b.OriginName).(*types.Const)
			alias, aliasOK := obj.(*types.Const)
			if !ok || !aliasOK || !types.Identical(original.Type(), alias.Type()) || original.Val().ExactString() != alias.Val().ExactString() {
				return useMetadata{}, fmt.Errorf("use constant identity mismatch for %s.%s", path, b.Name)
			}
		}
		// Do not authorize a fabricated variable/function alias to another storage or
		// wrapper, even if signatures happen to be identical.
		if (b.Kind == "function" || b.Kind == "variable") && (b.RoutePath != b.OriginPath || b.RouteName != b.OriginName) {
			return useMetadata{}, fmt.Errorf("non-canonical function/variable use route in %s", path)
		}
	}
	return a.Metadata, nil
}

func (u *usePackage) validateNamespaceMetadata(node paths.Namespace, depth int) error {
	if depth > 64 {
		return fmt.Errorf("namespace metadata nesting limit exceeded")
	}
	switch node.Source {
	case "", "toolchain":
	case "module", "local":
		if node.ModulePath == "" {
			return fmt.Errorf("namespace metadata missing module identity")
		}
	default:
		return fmt.Errorf("unknown namespace metadata source %q", node.Source)
	}
	if node.PackagePath == "" && len(node.Children) == 0 {
		return fmt.Errorf("invalid namespace group metadata")
	}
	for name, child := range node.Children {
		if !paths.ValidIdentifier(name) {
			return fmt.Errorf("invalid namespace child %q", name)
		}
		if err := u.validateNamespaceMetadata(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
func writeUseArtifact(root, path string, m useMetadata, files map[string][]byte, imp types.Importer) error {
	if !m.present() {
		return nil
	}
	provider, ok := imp.(refs.ArtifactImporter)
	if !ok {
		return fmt.Errorf("use metadata source importer unavailable")
	}
	content, e := provider.PackageFingerprint(path)
	if e != nil {
		return e
	}
	a := useArtifact{Version: useMetadataVersion, Metadata: m, Content: content, Files: map[string]string{}}
	for name, source := range files {
		a.Files[name] = enumDigest(source)
	}
	b, e := json.MarshalIndent(a, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(root, useMetadataFilename), append(b, '\n'), 0644)
}
func attachUseMetadata(result *PackageResult, u *usePackage) error {
	m := u.metadata
	m.UnsupportedKinds = map[string]string{}
	if result.macros != nil {
		for _, name := range result.macros.PublicNames() {
			m.Unsupported = append(m.Unsupported, name)
			m.UnsupportedKinds[name] = "macro"
		}
	}
	for _, f := range result.enumMetadata.Families {
		if result.enumTypes.Scope().Lookup(f.Name) == nil {
			m.Unsupported = append(m.Unsupported, f.Name)
			m.UnsupportedKinds[f.Name] = "family"
		}
	}
	sort.Strings(m.Unsupported)
	result.uses = m
	if !m.present() {
		return nil
	}
	for _, name := range sortedResultNames(*result) {
		if len(name) < 4 || name[len(name)-4:] != ".gox" {
			continue
		}
		r := result.Files[name]
		r.Go = append(r.Go, []byte(fmt.Sprintf("\nconst %s = %q\n", useMetadataMarker, m.marker()))...)
		r.Expanded = r.Go
		refreshPackageMap(&r)
		result.Files[name] = r
		return nil
	}
	return fmt.Errorf("use metadata requires a generated goxide source file")
}
