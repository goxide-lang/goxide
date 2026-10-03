package frontend

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
	"reflect"
	"sort"
	"strconv"

	"github.com/hxsf/goxide/internal/refs"
)

const enumMetadataMarker = "HgoEnumMetadata"
const enumMetadataFilename = ".hgo.enums.json"
const enumMetadataVersion = 1

type enumVariantExport struct {
	Name, Case, Constructor string
	HasPayload              bool
}
type enumExport struct {
	Name, DefinitionPath, DefinitionName, Marker, Validator string
	Variants                                                []enumVariantExport
}
type enumMetadata struct {
	Version       int
	Path, Package string
	Enums         []enumExport
}

func (m enumMetadata) Marker() string {
	data, _ := json.Marshal(m)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

type enumDependencyFingerprint struct {
	Content, Marker, Artifact, ReferenceMarker, ReferenceArtifact string
}
type enumArtifact struct {
	Metadata        enumMetadata
	ArtifactVersion int
	Files           map[string]string
	Content         string
	Dependencies    map[string]enumDependencyFingerprint
}

func enumDigest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func enumPackageMarker(pkg *types.Package, name string) (string, error) {
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return "", nil
	}
	c, ok := obj.(*types.Const)
	if !ok || c.Val().Kind() != constant.String {
		return "", fmt.Errorf("invalid metadata marker %s in %s", name, pkg.Path())
	}
	value := constant.StringVal(c.Val())
	if value == "" {
		return "", fmt.Errorf("empty metadata marker %s in %s", name, pkg.Path())
	}
	return value, nil
}
func enumArtifactDependencies(files map[string][]byte, imp refs.ArtifactImporter) (map[string]enumDependencyFingerprint, error) {
	deps := map[string]enumDependencyFingerprint{}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		f, e := parser.ParseFile(token.NewFileSet(), name, files[name], parser.ImportsOnly)
		if e != nil {
			return nil, e
		}
		for _, s := range f.Imports {
			path, _ := strconv.Unquote(s.Path.Value)
			if path == "C" {
				continue
			}
			if _, ok := deps[path]; ok {
				continue
			}
			content, e := imp.PackageFingerprint(path)
			if e != nil {
				return nil, e
			}
			pkg, e := imp.Import(path)
			if e != nil {
				return nil, e
			}
			d := enumDependencyFingerprint{Content: content}
			d.Marker, e = enumPackageMarker(pkg, enumMetadataMarker)
			if e != nil {
				return nil, e
			}
			d.ReferenceMarker, e = enumPackageMarker(pkg, refs.MetadataMarker)
			if e != nil {
				return nil, e
			}
			for _, a := range []struct {
				marker, file string
				digest       *string
			}{{d.Marker, enumMetadataFilename, &d.Artifact}, {d.ReferenceMarker, refs.MetadataFilename, &d.ReferenceArtifact}} {
				if a.marker == "" {
					continue
				}
				dir, _, e := imp.PackageFiles(path)
				if e != nil {
					return nil, e
				}
				data, e := os.ReadFile(filepath.Join(dir, a.file))
				if e != nil {
					return nil, e
				}
				*a.digest = enumDigest(data)
			}
			deps[path] = d
		}
	}
	return deps, nil
}
func marshalEnumArtifact(meta enumMetadata, files map[string][]byte, imp refs.ArtifactImporter) ([]byte, error) {
	deps, e := enumArtifactDependencies(files, imp)
	if e != nil {
		return nil, e
	}
	content, e := imp.PackageFingerprint(meta.Path)
	if e != nil {
		return nil, e
	}
	a := enumArtifact{Metadata: meta, ArtifactVersion: 1, Files: map[string]string{}, Content: content, Dependencies: deps}
	for name, data := range files {
		a.Files[name] = enumDigest(data)
	}
	data, e := json.MarshalIndent(a, "", "  ")
	if e != nil {
		return nil, e
	}
	return append(data, '\n'), nil
}
func loadEnumArtifact(path string, imp refs.ArtifactImporter) (enumMetadata, error) {
	checked := map[string]enumMetadata{}
	active := map[string]bool{}
	var load func(string) (enumMetadata, error)
	load = func(path string) (enumMetadata, error) {
		bad := func(reason string) (enumMetadata, error) {
			return enumMetadata{}, fmt.Errorf("enum metadata missing, stale or wrong package for %s: %s", path, reason)
		}
		if m, ok := checked[path]; ok {
			return m, nil
		}
		if active[path] {
			return bad("cyclic metadata dependencies")
		}
		active[path] = true
		dir, names, e := imp.PackageFiles(path)
		if e != nil {
			return bad(e.Error())
		}
		data, e := os.ReadFile(filepath.Join(dir, enumMetadataFilename))
		if e != nil {
			return bad("cannot read " + enumMetadataFilename + ": " + e.Error())
		}
		if e = enumUniqueJSON(data); e != nil {
			return bad("invalid metadata JSON: " + e.Error())
		}
		var a enumArtifact
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&a); e != nil {
			return bad("invalid metadata JSON: " + e.Error())
		}
		if e = dec.Decode(new(any)); e != io.EOF {
			return bad("trailing metadata JSON")
		}
		if a.ArtifactVersion != 1 || a.Metadata.Version != enumMetadataVersion {
			return bad("unsupported metadata version")
		}
		if a.Metadata.Path != path {
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
			hash, ok := a.Files[name]
			if !ok {
				return bad("selected Go source set mismatch: " + name)
			}
			source, e := os.ReadFile(filepath.Join(dir, name))
			if e != nil {
				return bad(e.Error())
			}
			if enumDigest(source) != hash {
				return bad("Go source content mismatch: " + name)
			}
			files[name] = source
		}
		pkg, e := imp.Import(path)
		if e != nil {
			return bad(e.Error())
		}
		marker, e := enumPackageMarker(pkg, enumMetadataMarker)
		if e != nil {
			return bad(e.Error())
		}
		if marker != a.Metadata.Marker() {
			return bad("metadata marker digest mismatch")
		}
		if e = validateEnumMetadata(a.Metadata, pkg, imp); e != nil {
			return bad(e.Error())
		}
		deps, e := enumArtifactDependencies(files, imp)
		if e != nil {
			return bad(e.Error())
		}
		if a.Dependencies == nil || len(a.Dependencies) != len(deps) {
			return bad("dependency set mismatch")
		}
		paths := make([]string, 0, len(deps))
		for dep := range deps {
			paths = append(paths, dep)
		}
		sort.Strings(paths)
		for _, dep := range paths {
			actual := deps[dep]
			recorded, ok := a.Dependencies[dep]
			if !ok {
				return bad("dependency set mismatch: " + dep)
			}
			if recorded.Marker != actual.Marker || recorded.ReferenceMarker != actual.ReferenceMarker {
				return bad("enhanced dependency marker changed: " + dep)
			}
			if actual.Marker != "" {
				if _, e = load(dep); e != nil {
					return bad("dependency " + dep + ": " + e.Error())
				}
			}
			if actual.ReferenceMarker != "" {
				if _, e = refs.LoadMetadataArtifact(dep, imp); e != nil {
					return bad("reference dependency " + dep + ": " + e.Error())
				}
			}
			if recorded.Content != actual.Content {
				return bad("dependency source closure changed: " + dep)
			}
			if recorded.Artifact != actual.Artifact || recorded.ReferenceArtifact != actual.ReferenceArtifact {
				return bad("enhanced dependency artifact changed: " + dep)
			}
		}
		// An exported alias may only relay a declaration authorized by its defining
		// package; a matching Go interface shape alone never grants enum semantics.
		for _, export := range a.Metadata.Enums {
			origin := a.Metadata
			if export.DefinitionPath != path {
				origin, e = load(export.DefinitionPath)
				if e != nil {
					return bad("enum definition: " + e.Error())
				}
			}
			found := false
			for _, definition := range origin.Enums {
				if definition.Name == export.DefinitionName && definition.DefinitionPath == export.DefinitionPath && definition.DefinitionName == export.DefinitionName {
					want := definition
					want.Name = export.Name
					if reflect.DeepEqual(want, export) {
						found = true
					}
					break
				}
			}
			if !found {
				return bad("enum alias does not match a public defining declaration: " + export.Name)
			}
		}
		content, e := imp.PackageFingerprint(path)
		if e != nil {
			return bad(e.Error())
		}
		if content != a.Content {
			return bad("package source closure or build configuration changed")
		}
		active[path] = false
		checked[path] = a.Metadata
		return a.Metadata, nil
	}
	return load(path)
}

func validateEnumMetadata(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	if meta.Version != enumMetadataVersion || meta.Path != pkg.Path() || meta.Package != pkg.Name() || len(meta.Enums) == 0 {
		return fmt.Errorf("enum metadata package identity or version mismatch")
	}
	exported := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	function := func(p *types.Package, name string) (*types.Signature, error) {
		if !exported(name) {
			return nil, fmt.Errorf("invalid exported helper %q", name)
		}
		fn, ok := p.Scope().Lookup(name).(*types.Func)
		if !ok {
			return nil, fmt.Errorf("missing enum helper %s.%s", p.Path(), name)
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 {
			return nil, fmt.Errorf("invalid enum helper signature %s", name)
		}
		return sig, nil
	}
	seen := map[string]bool{}
	for _, d := range meta.Enums {
		if !exported(d.Name) || !exported(d.DefinitionName) || d.DefinitionPath == "" || seen[d.Name] || len(d.Variants) == 0 {
			return fmt.Errorf("invalid or duplicate enum export %q", d.Name)
		}
		seen[d.Name] = true
		if !token.IsIdentifier(d.Marker) || token.IsExported(d.Marker) || d.Marker == "_" {
			return fmt.Errorf("invalid private enum marker %q", d.Marker)
		}
		origin := pkg
		var e error
		if d.DefinitionPath != pkg.Path() {
			origin, e = imp.Import(d.DefinitionPath)
			if e != nil {
				return e
			}
		}
		obj, ok := origin.Scope().Lookup(d.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing enum definition %s.%s", d.DefinitionPath, d.DefinitionName)
		}
		enum, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || enum.Obj().Pkg() == nil || enum.Obj().Pkg().Path() != d.DefinitionPath || enum.Obj().Name() != d.DefinitionName || enum.TypeParams().Len() != 0 {
			return fmt.Errorf("invalid canonical enum definition %s", d.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(d.Name).(*types.TypeName)
		if !ok || !types.Identical(types.Unalias(exposed.Type()), enum) {
			return fmt.Errorf("enum alias identity mismatch: %s", d.Name)
		}
		iface, ok := enum.Underlying().(*types.Interface)
		if !ok || !iface.IsMethodSet() {
			return fmt.Errorf("enum definition is not a concrete interface: %s", d.Name)
		}
		iface.Complete()
		hasMarker := false
		for i := 0; i < iface.NumMethods(); i++ {
			m := iface.Method(i)
			if m.Name() == d.Marker && m.Pkg() != nil && m.Pkg().Path() == d.DefinitionPath {
				s := m.Type().(*types.Signature)
				hasMarker = s.Params().Len() == 0 && s.Results().Len() == 0 && !s.Variadic()
			}
		}
		if !hasMarker {
			return fmt.Errorf("enum private marker signature mismatch: %s", d.Name)
		}
		validator, e := function(origin, d.Validator)
		if e != nil {
			return e
		}
		if validator.Params().Len() != 1 || validator.Results().Len() != 0 || !types.Identical(validator.Params().At(0).Type(), enum) {
			return fmt.Errorf("enum validator signature mismatch: %s", d.Name)
		}
		variants := map[string]bool{}
		caseTypes := []types.Type{}
		for _, v := range d.Variants {
			if !token.IsIdentifier(v.Name) || v.Name == "_" || variants[v.Name] || !exported(v.Case) {
				return fmt.Errorf("invalid enum variant %q", v.Name)
			}
			variants[v.Name] = true
			caseObj, ok := origin.Scope().Lookup(v.Case).(*types.TypeName)
			if !ok {
				return fmt.Errorf("missing enum case %s", v.Case)
			}
			ct := types.Unalias(caseObj.Type())
			named, ok := ct.(*types.Named)
			if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != d.DefinitionPath || named.TypeParams().Len() != 0 {
				return fmt.Errorf("invalid enum case identity %s", v.Case)
			}
			st, ok := ct.Underlying().(*types.Struct)
			if !ok || !types.Implements(ct, iface) {
				return fmt.Errorf("invalid enum value case %s", v.Case)
			}
			fields := 0
			if v.HasPayload {
				fields = 1
			}
			if st.NumFields() != fields {
				return fmt.Errorf("enum case payload shape mismatch: %s", v.Case)
			}
			if v.HasPayload && (st.Field(0).Name() != "Value" || st.Field(0).Embedded()) {
				return fmt.Errorf("enum case payload field mismatch: %s", v.Case)
			}
			for _, prior := range caseTypes {
				if types.Identical(prior, ct) {
					return fmt.Errorf("duplicate enum case type %s", v.Case)
				}
			}
			caseTypes = append(caseTypes, ct)
			constructor, e := function(origin, v.Constructor)
			if e != nil {
				return e
			}
			if constructor.Params().Len() != fields || constructor.Results().Len() != 1 || !types.Identical(constructor.Results().At(0).Type(), enum) {
				return fmt.Errorf("enum constructor signature mismatch: %s", v.Constructor)
			}
			if v.HasPayload && !types.Identical(constructor.Params().At(0).Type(), st.Field(0).Type()) {
				return fmt.Errorf("enum constructor payload mismatch: %s", v.Constructor)
			}
		}
	}
	return nil
}

// Reject duplicate object keys rather than accepting last-write-wins metadata.
func enumUniqueJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				name, ok := key.(string)
				if !ok {
					return fmt.Errorf("invalid object key")
				}
				if seen[name] {
					return fmt.Errorf("duplicate object key %q", name)
				}
				seen[name] = true
				if e = value(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e = value(); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if e := value(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("trailing metadata JSON")
	}
	return nil
}
