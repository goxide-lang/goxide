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

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/refs"
)

const enumMetadataMarker = "GoxideEnumMetadata"
const enumMetadataFilename = ".goxide.enums.json"
const enumMetadataVersion = 9

type enumVariantExport struct {
	Name, Field, PayloadAlias, Constructor, Value, Project string
	Tag                                                    int
	HasPayload                                             bool
}
type enumExport struct {
	Name, DefinitionPath, DefinitionName, TagField, Tag, Validator string
	Variants                                                       []enumVariantExport
}
type enumFamilyVariantExport struct {
	Name, Field, PayloadAlias, Constructor, Value, Project string
	Tag, PayloadIndex                                      int
}
type enumFamilyInstanceExport struct{ ValidatorPath, Validator string }
type enumFamilyExport struct {
	ABI                                                                    int
	Name, Family, DefinitionPath, DefinitionName, TagField, Tag, Validator string
	Variants                                                               []enumFamilyVariantExport
	Instances                                                              []enumFamilyInstanceExport
}

// Equality records are explicit PartialEq registrations; Eq adds qualification.
type equalityExport struct {
	Name, DefinitionPath, DefinitionName, Helper string
	PartialEqSource, EqSource                    string
	Eq                                           bool
}

type defaultExport struct{ Name, DefinitionPath, DefinitionName, Helper, Source string }

type fieldProtocolExport struct{ Name, DefinitionPath, DefinitionName, Helper, Kind, Source string }

type enumMetadata struct {
	FieldProtocols []fieldProtocolExport
	Defaults       []defaultExport
	Version        int
	Path, Package  string
	Enums          []enumExport
	Families       []enumFamilyExport
	Equality       []equalityExport
}

func (m enumMetadata) present() bool {
	return len(m.Enums)+len(m.Families)+len(m.Equality)+len(m.Defaults)+len(m.FieldProtocols) > 0
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
		if path == enums.SumPackage {
			pkg, err := imp.Import(path)
			if err != nil {
				return enumMetadata{}, err
			}
			return sharedSumMetadata(pkg, imp)
		}
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
			return bad("unsupported metadata version; regenerate producer artifacts")
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
		for _, export := range a.Metadata.Families {
			origin := a.Metadata
			if export.DefinitionPath != path {
				origin, e = load(export.DefinitionPath)
				if e != nil {
					return bad("builtin definition: " + e.Error())
				}
			}
			found := false
			for _, definition := range origin.Families {
				if definition.Name == export.DefinitionName && definition.DefinitionPath == export.DefinitionPath && definition.DefinitionName == export.DefinitionName {
					want := definition
					want.Name = export.Name
					if reflect.DeepEqual(familyContract(want), familyContract(export)) {
						found = true
					}
					break
				}
			}
			if !found {
				return bad("builtin alias does not match a public defining declaration: " + export.Name)
			}
			for _, instance := range export.Instances {
				owner := instance.ValidatorPath
				if owner == "" {
					owner = export.DefinitionPath
				}
				if owner == path {
					continue
				}
				om, err := load(owner)
				if err != nil {
					return bad("validator owner: " + err.Error())
				}
				registered := false
				for _, of := range om.Families {
					if reflect.DeepEqual(familyContract(of), familyContract(export)) {
						for _, oi := range of.Instances {
							if oi == instance {
								registered = true
							}
						}
					}
				}
				if !registered {
					return bad("validator not registered by owner: " + owner + "." + instance.Validator)
				}
			}
		}
		for _, export := range a.Metadata.Equality {
			origin := a.Metadata
			if export.DefinitionPath != path {
				origin, e = load(export.DefinitionPath)
				if e != nil {
					return bad("equality definition: " + e.Error())
				}
			}
			found := false
			for _, definition := range origin.Equality {
				if definition.Name == export.DefinitionName && definition.DefinitionPath == export.DefinitionPath && definition.DefinitionName == export.DefinitionName {
					want := definition
					want.Name = export.Name
					found = want == export
					break
				}
			}
			if !found {
				return bad("equality alias does not match a public defining declaration: " + export.Name)
			}
		}
		for _, export := range a.Metadata.Defaults {
			origin := a.Metadata
			if export.DefinitionPath != path {
				origin, e = load(export.DefinitionPath)
				if e != nil {
					return bad("Default definition: " + e.Error())
				}
			}
			found := false
			for _, definition := range origin.Defaults {
				if definition.Name == export.DefinitionName && definition.DefinitionPath == export.DefinitionPath && definition.DefinitionName == export.DefinitionName {
					want := definition
					want.Name = export.Name
					found = want == export
					break
				}
			}
			if !found {
				return bad("Default alias does not match a public defining declaration: " + export.Name)
			}
		}
		for _, export := range a.Metadata.FieldProtocols {
			origin := a.Metadata
			if export.DefinitionPath != path {
				origin, e = load(export.DefinitionPath)
				if e != nil {
					return bad("field protocol definition: " + e.Error())
				}
			}
			found := false
			for _, definition := range origin.FieldProtocols {
				if definition.Kind == export.Kind && definition.Name == export.DefinitionName && definition.DefinitionPath == export.DefinitionPath && definition.DefinitionName == export.DefinitionName {
					want := definition
					want.Name = export.Name
					found = want == export
					break
				}
			}
			if !found {
				return bad("field protocol alias does not match a public defining declaration: " + export.Name)
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

// Helpers are real functions owned by the canonical definition package. A
// same-shaped variable, alias or foreign helper never grants enum authority.
func enumFunction(origin *types.Package, name string, args []types.Type) (*types.Signature, error) {
	if !token.IsIdentifier(name) || !token.IsExported(name) {
		return nil, fmt.Errorf("invalid exported enum helper %q", name)
	}
	fn, ok := origin.Scope().Lookup(name).(*types.Func)
	if !ok || fn.Pkg() != origin {
		return nil, fmt.Errorf("missing canonical enum helper %s.%s", origin.Path(), name)
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() != nil || sig.Variadic() || !enumUnconstrained(sig.TypeParams(), len(args)) {
		return nil, fmt.Errorf("invalid enum helper signature %s", name)
	}
	if len(args) > 0 {
		t, err := types.Instantiate(nil, sig, args, true)
		if err != nil {
			return nil, err
		}
		sig = t.(*types.Signature)
	}
	return sig, nil
}
func enumUnconstrained(list *types.TypeParamList, count int) bool {
	if list.Len() != count {
		return false
	}
	for i := 0; i < count; i++ {
		c, ok := list.At(i).Constraint().Underlying().(*types.Interface)
		if !ok || !c.Empty() {
			return false
		}
	}
	return true
}
func enumSignature(sig *types.Signature, params, results []types.Type) bool {
	if sig.Params().Len() != len(params) || sig.Results().Len() != len(results) {
		return false
	}
	for i, t := range params {
		if !types.Identical(sig.Params().At(i).Type(), t) {
			return false
		}
	}
	for i, t := range results {
		if !types.Identical(sig.Results().At(i).Type(), t) {
			return false
		}
	}
	return true
}
func enumPrivateField(st *types.Struct, index int, name string, origin *types.Package) bool {
	if index >= st.NumFields() || !token.IsIdentifier(name) || token.IsExported(name) || name == "_" {
		return false
	}
	f := st.Field(index)
	return f.Name() == name && f.Pkg() == origin && !f.Embedded() && st.Tag(index) == ""
}

// Payload aliases expose the exact type needed to spell a nominal reference
// without granting access to its private storage or inventing a wrapper type.
func enumPayloadAlias(origin *types.Package, name string, args []types.Type, payload types.Type) error {
	if !token.IsIdentifier(name) || !token.IsExported(name) {
		return fmt.Errorf("invalid enum payload alias %q", name)
	}
	obj, ok := origin.Scope().Lookup(name).(*types.TypeName)
	if !ok || obj.Pkg() != origin || !obj.IsAlias() || !enumUnconstrained(enumGenericParameters(obj.Type()), len(args)) {
		return fmt.Errorf("invalid canonical enum payload alias %s", name)
	}
	actual := obj.Type()
	if len(args) > 0 {
		var err error
		actual, err = types.Instantiate(nil, actual, args, true)
		if err != nil {
			return err
		}
	}
	if !types.Identical(actual, payload) {
		return fmt.Errorf("enum payload alias identity mismatch: %s", name)
	}
	return nil
}

func enumShape(t types.Type, origin *types.Package, tagField string, fields int, tagType types.Type) (*types.Struct, error) {
	st, ok := t.Underlying().(*types.Struct)
	if !ok || st.NumFields() != fields+1 {
		return nil, fmt.Errorf("enum inline payload shape mismatch: %s", t)
	}
	if !enumPrivateField(st, 0, tagField, origin) || !types.Identical(st.Field(0).Type(), tagType) {
		return nil, fmt.Errorf("enum private tag field mismatch: %s", t)
	}
	return st, nil
}
func validateEnumMetadata(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	if meta.Version != enumMetadataVersion || meta.Path != pkg.Path() || meta.Package != pkg.Name() || !meta.present() {
		return fmt.Errorf("enum metadata package identity or version mismatch")
	}
	exported := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	seen := map[string]bool{}
	for _, d := range meta.Enums {
		if !exported(d.Name) || !exported(d.DefinitionName) || d.DefinitionPath == "" || seen[d.Name] || len(d.Variants) == 0 {
			return fmt.Errorf("invalid or duplicate enum export %q", d.Name)
		}
		seen[d.Name] = true
		origin := pkg
		if d.DefinitionPath != pkg.Path() {
			if imp == nil {
				return fmt.Errorf("missing enum definition importer")
			}
			var err error
			origin, err = imp.Import(d.DefinitionPath)
			if err != nil {
				return err
			}
		}
		obj, ok := origin.Scope().Lookup(d.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing enum definition %s.%s", d.DefinitionPath, d.DefinitionName)
		}
		enum, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || enum.Obj().Pkg() != origin || enum.Obj().Name() != d.DefinitionName || enum.TypeParams().Len() != 0 {
			return fmt.Errorf("invalid canonical enum definition %s", d.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(d.Name).(*types.TypeName)
		if !ok || !types.Identical(types.Unalias(exposed.Type()), enum) {
			return fmt.Errorf("enum alias identity mismatch: %s", d.Name)
		}
		fields := 0
		for _, v := range d.Variants {
			if v.HasPayload {
				fields++
			}
		}
		st, err := enumShape(enum, origin, d.TagField, fields, types.Typ[types.Int])
		if err != nil {
			return err
		}
		validator, err := enumFunction(origin, d.Validator, nil)
		if err != nil {
			return err
		}
		if !enumSignature(validator, []types.Type{enum}, nil) {
			return fmt.Errorf("enum validator signature mismatch: %s", d.Name)
		}
		tag, err := enumFunction(origin, d.Tag, nil)
		if err != nil {
			return err
		}
		if !enumSignature(tag, []types.Type{enum}, []types.Type{types.Typ[types.Int]}) {
			return fmt.Errorf("enum tag signature mismatch: %s", d.Name)
		}
		variants := map[string]bool{}
		slot := 1
		for i, v := range d.Variants {
			if !token.IsIdentifier(v.Name) || v.Name == "_" || variants[v.Name] || v.Tag != i+1 {
				return fmt.Errorf("invalid enum variant %q", v.Name)
			}
			variants[v.Name] = true
			var payload types.Type
			if v.HasPayload {
				if !enumPrivateField(st, slot, v.Field, origin) {
					return fmt.Errorf("enum payload field mismatch: %s", v.Name)
				}
				payload = st.Field(slot).Type()
				if err := enumPayloadAlias(origin, v.PayloadAlias, nil, payload); err != nil {
					return err
				}
				slot++
				value, err := enumFunction(origin, v.Value, nil)
				if err != nil {
					return err
				}
				if !enumSignature(value, []types.Type{enum}, []types.Type{payload}) {
					return fmt.Errorf("enum getter payload signature mismatch: %s", v.Name)
				}
				project, err := enumFunction(origin, v.Project, nil)
				if err != nil {
					return err
				}
				if !enumSignature(project, []types.Type{types.NewPointer(enum)}, []types.Type{types.NewPointer(payload)}) {
					return fmt.Errorf("enum projector payload signature mismatch: %s", v.Name)
				}
			} else if v.Field != "" || v.PayloadAlias != "" || v.Value != "" || v.Project != "" {
				return fmt.Errorf("unit enum variant must not name payload helpers: %s", v.Name)
			}
			ctor, err := enumFunction(origin, v.Constructor, nil)
			if err != nil {
				return err
			}
			params := []types.Type{}
			if payload != nil {
				params = append(params, payload)
			}
			if !enumSignature(ctor, params, []types.Type{enum}) {
				return fmt.Errorf("enum constructor payload signature mismatch: %s", v.Constructor)
			}
		}
	}
	for _, family := range meta.Families {
		if family.DefinitionPath != enums.SumPackage {
			return fmt.Errorf("builtin sum origin is not shared std; regenerate producer artifacts")
		}
	}
	if err := validateEnumFamilies(meta, pkg, imp); err != nil {
		return err
	}
	if err := validateEqualityMetadata(meta, pkg, imp); err != nil {
		return err
	}
	if err := validateDefaultMetadata(meta, pkg, imp); err != nil {
		return err
	}
	return validateFieldProtocolMetadata(meta, pkg, imp)
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

// Instantiate every protocol helper with the canonical family's own type
// parameters. Payload spelling and consumer aliases never substitute identity.
func validateEnumFamilies(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	seen := map[string]bool{}
	for _, d := range meta.Enums {
		seen[d.Name] = true
	}
	public := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	for _, f := range meta.Families {
		if f.DefinitionPath == enums.SumPackage && !reflect.DeepEqual(familyContract(f), sharedSumFamily(f.Family)) {
			return fmt.Errorf("shared sum family ABI mismatch: %s", f.Name)
		}
		count := 1
		names := []string{"Some", "None"}
		payloads := []int{0, -1}
		tags := []int{1, 0}
		if f.Family == "Result" {
			count = 2
			names = []string{"Ok", "Err"}
			payloads = []int{0, 1}
			tags = []int{1, 2}
		} else if f.Family != "Option" {
			return fmt.Errorf("unknown builtin family %q", f.Family)
		}
		if f.ABI != enums.SumABI || !public(f.Name) || !public(f.DefinitionName) || f.DefinitionPath == "" || seen[f.Name] || len(f.Variants) != 2 || (len(f.Instances) == 0 && f.DefinitionPath != enums.SumPackage) {
			return fmt.Errorf("invalid or duplicate builtin family export %q", f.Name)
		}
		seen[f.Name] = true
		origin := pkg
		if f.DefinitionPath != pkg.Path() {
			if imp == nil {
				return fmt.Errorf("missing builtin origin importer")
			}
			var err error
			origin, err = imp.Import(f.DefinitionPath)
			if err != nil {
				return err
			}
		}
		obj, ok := origin.Scope().Lookup(f.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing builtin family definition %s", f.DefinitionName)
		}
		canonical, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || canonical.Obj().Pkg() != origin || canonical.Obj().Name() != f.DefinitionName || canonical.TypeArgs().Len() != 0 || !enumUnconstrained(canonical.TypeParams(), count) {
			return fmt.Errorf("invalid builtin family definition %s", f.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(f.Name).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing builtin export %s", f.Name)
		}
		target, ok := types.Unalias(exposed.Type()).(*types.Named)
		if !ok || !types.Identical(target.Origin(), canonical) {
			return fmt.Errorf("builtin alias identity mismatch: %s", f.Name)
		}
		args := make([]types.Type, count)
		for i := range args {
			args[i] = canonical.TypeParams().At(i)
		}
		concrete, err := types.Instantiate(nil, canonical, args, true)
		if err != nil {
			return err
		}
		st, err := enumShape(concrete, origin, f.TagField, count, types.Typ[types.Uint8])
		if err != nil {
			return err
		}
		tag, err := enumFunction(origin, f.Tag, args)
		if err != nil {
			return err
		}
		if !enumSignature(tag, []types.Type{concrete}, []types.Type{types.Typ[types.Int]}) {
			return fmt.Errorf("builtin tag signature mismatch: %s", f.Name)
		}
		validator, err := enumFunction(origin, f.Validator, args)
		if err != nil {
			return err
		}
		if !enumSignature(validator, []types.Type{concrete}, nil) {
			return fmt.Errorf("builtin validator signature mismatch: %s", f.Name)
		}
		slot := 1
		for i, v := range f.Variants {
			if v.Name != names[i] || v.PayloadIndex != payloads[i] || v.Tag != tags[i] {
				return fmt.Errorf("builtin variant protocol mismatch: %s", f.Name)
			}
			var payload types.Type
			if v.PayloadIndex < 0 {
				if v.Field != "" || v.PayloadAlias != "" || v.Value != "" || v.Project != "" {
					return fmt.Errorf("Option None must not name payload helpers")
				}
			} else {
				payload = args[v.PayloadIndex]
				if !enumPrivateField(st, slot, v.Field, origin) || !types.Identical(st.Field(slot).Type(), payload) {
					return fmt.Errorf("builtin payload slot type or owner mismatch: %s", v.Name)
				}
				slot++
				value, err := enumFunction(origin, v.Value, args)
				if err != nil {
					return err
				}
				if !enumSignature(value, []types.Type{concrete}, []types.Type{payload}) {
					return fmt.Errorf("builtin getter payload signature mismatch: %s", v.Name)
				}
				project, err := enumFunction(origin, v.Project, args)
				if err != nil {
					return err
				}
				if !enumSignature(project, []types.Type{types.NewPointer(concrete)}, []types.Type{types.NewPointer(payload)}) {
					return fmt.Errorf("builtin projector payload signature mismatch: %s", v.Name)
				}
			}
			ctor, err := enumFunction(origin, v.Constructor, args)
			if err != nil {
				return err
			}
			params := []types.Type{}
			if payload != nil {
				params = append(params, payload)
			}
			if !enumSignature(ctor, params, []types.Type{concrete}) {
				return fmt.Errorf("builtin constructor payload signature mismatch: %s", v.Constructor)
			}
		}
		validators := map[string]bool{}
		for _, instance := range f.Instances {
			if !public(instance.Validator) || instance.ValidatorPath == "" || validators[instance.ValidatorPath+"."+instance.Validator] {
				return fmt.Errorf("invalid or duplicate builtin validator %s", instance.Validator)
			}
			validators[instance.ValidatorPath+"."+instance.Validator] = true
			owner := origin
			if instance.ValidatorPath != origin.Path() {
				if instance.ValidatorPath == pkg.Path() {
					owner = pkg
				} else {
					if imp == nil {
						return fmt.Errorf("missing validator owner importer")
					}
					var err error
					owner, err = imp.Import(instance.ValidatorPath)
					if err != nil {
						return err
					}
				}
			}
			sig, err := enumFunction(owner, instance.Validator, nil)
			if err != nil {
				return err
			}
			if sig.Params().Len() != 1 || sig.Results().Len() != 0 {
				return fmt.Errorf("builtin instance validator signature mismatch: %s", instance.Validator)
			}
			n, ok := types.Unalias(sig.Params().At(0).Type()).(*types.Named)
			if !ok || !types.Identical(n.Origin(), canonical) || n.TypeArgs().Len() != count {
				return fmt.Errorf("builtin instance validator family mismatch: %s", instance.Validator)
			}
		}
	}
	return nil
}

func enumGenericParameters(t types.Type) *types.TypeParamList {
	switch t := t.(type) {
	case *types.Named:
		return t.TypeParams()
	case *types.Alias:
		return t.TypeParams()
	}
	return nil
}

// Validate registrations, never infer a protocol from an arbitrary helper name.
func validateEqualityMetadata(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	public := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	seen := map[string]bool{}
	for _, x := range meta.Equality {
		if !public(x.Name) || !public(x.DefinitionName) || !public(x.Helper) || x.DefinitionPath == "" || seen[x.Name] {
			return fmt.Errorf("invalid or duplicate equality export %q", x.Name)
		}
		if x.PartialEqSource != "derive" && x.PartialEqSource != "manual" {
			return fmt.Errorf("invalid PartialEq source for %s", x.Name)
		}
		if (!x.Eq && x.EqSource != "") || (x.Eq && x.EqSource != "derive" && x.EqSource != "manual") {
			return fmt.Errorf("invalid Eq source for %s", x.Name)
		}
		if x.PartialEqSource == "manual" && x.EqSource == "derive" {
			return fmt.Errorf("derived Eq cannot qualify manual PartialEq: %s", x.Name)
		}
		seen[x.Name] = true
		origin := pkg
		if x.DefinitionPath != pkg.Path() {
			var err error
			origin, err = imp.Import(x.DefinitionPath)
			if err != nil {
				return err
			}
			if origin == nil {
				return fmt.Errorf("missing equality definition package %s", x.DefinitionPath)
			}
		}
		obj, ok := origin.Scope().Lookup(x.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing equality definition %s", x.DefinitionName)
		}
		target, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || target.Obj().Pkg() == nil || target.Obj().Pkg().Path() != x.DefinitionPath || target.Obj().Name() != x.DefinitionName || target.TypeParams().Len() != 0 || target.TypeArgs().Len() != 0 {
			return fmt.Errorf("invalid equality canonical type %s", x.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(x.Name).(*types.TypeName)
		if !ok || !types.Identical(types.Unalias(exposed.Type()), target) {
			return fmt.Errorf("equality alias identity mismatch: %s", x.Name)
		}
		fn, ok := origin.Scope().Lookup(x.Helper).(*types.Func)
		if !ok {
			return fmt.Errorf("missing equality helper %s", x.Helper)
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 2 || sig.Results().Len() != 1 || !types.Identical(sig.Params().At(0).Type(), target) || !types.Identical(sig.Params().At(1).Type(), target) || !types.Identical(sig.Results().At(0).Type(), types.Typ[types.Bool]) {
			return fmt.Errorf("equality helper signature mismatch: %s", x.Helper)
		}
	}
	return nil
}

func validateDefaultMetadata(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	public := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	seen := map[string]bool{}
	for _, x := range meta.Defaults {
		if !public(x.Name) || !public(x.DefinitionName) || !public(x.Helper) || x.DefinitionPath == "" || seen[x.Name] {
			return fmt.Errorf("invalid or duplicate Default export %q", x.Name)
		}
		if x.Source != "derive" && x.Source != "manual" {
			return fmt.Errorf("invalid Default source for %s", x.Name)
		}
		seen[x.Name] = true
		origin := pkg
		if x.DefinitionPath != pkg.Path() {
			var err error
			if imp == nil {
				return fmt.Errorf("missing Default definition importer for %s", x.DefinitionPath)
			}
			origin, err = imp.Import(x.DefinitionPath)
			if err != nil {
				return err
			}
			if origin == nil {
				return fmt.Errorf("missing Default definition package %s", x.DefinitionPath)
			}
		}
		obj, ok := origin.Scope().Lookup(x.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing Default definition %s", x.DefinitionName)
		}
		target, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || target.Obj().Pkg() == nil || target.Obj().Pkg().Path() != x.DefinitionPath || target.Obj().Name() != x.DefinitionName || target.TypeParams().Len() != 0 || target.TypeArgs().Len() != 0 {
			return fmt.Errorf("invalid Default canonical type %s", x.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(x.Name).(*types.TypeName)
		if !ok || !types.Identical(types.Unalias(exposed.Type()), target) {
			return fmt.Errorf("Default alias identity mismatch: %s", x.Name)
		}
		fn, ok := origin.Scope().Lookup(x.Helper).(*types.Func)
		if !ok {
			return fmt.Errorf("missing Default helper %s", x.Helper)
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 || sig.Params().Len() != 0 || sig.Results().Len() != 1 || !types.Identical(sig.Results().At(0).Type(), target) {
			return fmt.Errorf("Default helper signature mismatch: %s", x.Helper)
		}
	}
	return nil
}

func validateFieldProtocolMetadata(meta enumMetadata, pkg *types.Package, imp types.Importer) error {
	public := func(s string) bool { return token.IsIdentifier(s) && token.IsExported(s) }
	seen := map[string]bool{}
	for _, x := range meta.FieldProtocols {
		if !public(x.Name) || !public(x.DefinitionName) || !public(x.Helper) || x.DefinitionPath == "" || seen[x.Kind+"."+x.Name] {
			return fmt.Errorf("invalid or duplicate field protocol export %q", x.Name)
		}
		if (x.Kind != "Clone" && x.Kind != "Debug" && x.Kind != "Serialize" && x.Kind != "Deserialize") || x.Source != "derive" {
			return fmt.Errorf("invalid field protocol source for %s", x.Name)
		}
		seen[x.Kind+"."+x.Name] = true
		origin := pkg
		if x.DefinitionPath != pkg.Path() {
			var err error
			if imp == nil {
				return fmt.Errorf("missing field protocol definition importer for %s", x.DefinitionPath)
			}
			origin, err = imp.Import(x.DefinitionPath)
			if err != nil {
				return err
			}
			if origin == nil {
				return fmt.Errorf("missing field protocol definition package %s", x.DefinitionPath)
			}
		}
		obj, ok := origin.Scope().Lookup(x.DefinitionName).(*types.TypeName)
		if !ok {
			return fmt.Errorf("missing field protocol definition %s", x.DefinitionName)
		}
		target, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || target.Obj().Pkg() == nil || target.Obj().Pkg().Path() != x.DefinitionPath || target.Obj().Name() != x.DefinitionName || target.TypeParams().Len() != 0 || target.TypeArgs().Len() != 0 {
			return fmt.Errorf("invalid field protocol canonical type %s", x.DefinitionName)
		}
		exposed, ok := pkg.Scope().Lookup(x.Name).(*types.TypeName)
		if !ok || !types.Identical(types.Unalias(exposed.Type()), target) {
			return fmt.Errorf("field protocol alias identity mismatch: %s", x.Name)
		}
		fn, ok := origin.Scope().Lookup(x.Helper).(*types.Func)
		if !ok {
			return fmt.Errorf("missing field protocol helper %s", x.Helper)
		}
		var result types.Type = target
		if x.Kind == "Debug" {
			result = types.Typ[types.String]
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil || sig.Variadic() || sig.TypeParams().Len() != 0 {
			return fmt.Errorf("field protocol helper signature mismatch: %s", x.Helper)
		}
		params, results := []types.Type{types.NewPointer(target)}, []types.Type{result}
		if x.Kind == "Serialize" || x.Kind == "Deserialize" {
			if imp == nil {
				return fmt.Errorf("missing serde protocol importer for %s", x.Helper)
			}
			core, err := imp.Import("github.com/goxide-lang/serde")
			if err != nil {
				return err
			}
			if core == nil {
				return fmt.Errorf("missing serde core package")
			}
			protocol := "Serializer"
			if x.Kind == "Deserialize" {
				protocol = "Deserializer"
			}
			obj, ok := core.Scope().Lookup(protocol).(*types.TypeName)
			if !ok {
				return fmt.Errorf("missing serde core protocol %s", protocol)
			}
			errorType := types.Universe.Lookup("error").Type()
			if x.Kind == "Serialize" {
				params, results = []types.Type{obj.Type(), types.NewPointer(target)}, []types.Type{errorType}
			} else {
				params, results = []types.Type{obj.Type()}, []types.Type{target, errorType}
			}
		}
		matches := func(actual *types.Tuple, expected []types.Type) bool {
			if actual.Len() != len(expected) {
				return false
			}
			for i, typ := range expected {
				if !types.Identical(actual.At(i).Type(), typ) {
					return false
				}
			}
			return true
		}
		if !matches(sig.Params(), params) || !matches(sig.Results(), results) {
			return fmt.Errorf("field protocol helper signature mismatch: %s", x.Helper)
		}
	}
	return nil
}
