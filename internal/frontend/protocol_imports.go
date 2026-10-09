package frontend

import (
	"encoding/json"
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func recoverProtocolImports(sources map[string][]byte, known map[string]PackageResult, imp types.Importer, maps map[string]*sourcemap.Map) (map[string]protocols.Metadata, error) {
	loader := newProtocolArtifactLoader(imp)
	for path, result := range known {
		if result.protocolMetadata.Present() {
			loader.checked[path] = result.protocolMetadata
		}
	}
	if imp == nil {
		return loader.checked, nil
	}
	for _, name := range sortedSources(sources) {
		f, err := parseSourceImports(name, sources[name])
		if err != nil {
			continue // The source parser owns malformed source diagnostics.
		}
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if path == "C" {
				continue
			}
			pkg, err := imp.Import(path)
			if err == nil && pkg != nil {
				err = naming.Package(pkg)
			}
			if err == nil {
				_, err = loader.load(path)
			}
			if err != nil {
				return nil, mappedEnumImportError(name, sources[name], maps[name], int(spec.Pos())-1, err.Error())
			}
		}
	}
	return loader.checked, nil
}

func lookupProtocolDefinition(meta protocols.Metadata, name string) (protocols.Definition, bool) {
	for _, definition := range meta.Definitions {
		if definition.Name == name && definition.Public {
			return definition, true
		}
	}
	return protocols.Definition{}, false
}

// Evidence transport does not make a private source protocol importable.
func lookupProtocolEvidence(meta protocols.Metadata, id protocols.ID) (protocols.Definition, bool) {
	for _, definition := range meta.Definitions {
		if definition.ID == id && (definition.Public || definition.ExportedEvidence) {
			return definition, true
		}
	}
	return protocols.Definition{}, false
}

func protocolDefinitionsEqual(a, b protocols.Definition) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func validateProtocolCanonicalOwners(meta protocols.Metadata, known map[string]protocols.Metadata) error {
	find := func(id protocols.ID) (protocols.Definition, bool) {
		owner := meta
		if id.Owner != meta.Path {
			owner = known[id.Owner]
		}
		for _, d := range owner.Definitions {
			if d.ID == id && (d.Public || d.ExportedEvidence) {
				return d, true
			}
		}
		return protocols.Definition{}, false
	}
	for _, d := range meta.Definitions {
		if d.Owner == meta.Path {
			continue
		}
		original, ok := find(d.ID)
		if !ok || !protocolDefinitionsEqual(original, d) {
			return fmt.Errorf("protocol re-export does not match canonical owner %s", d.ID.String())
		}
	}
	for _, impl := range meta.Implementations {
		if _, ok := find(impl.Protocol); !ok {
			return fmt.Errorf("implementation has no canonical protocol definition: %s", impl.Protocol.String())
		}
	}
	for _, f := range meta.Functions {
		for _, b := range f.Bounds {
			original, ok := find(b.Protocol.ID)
			if !ok || !protocolDefinitionsEqual(original, b.Protocol) {
				return fmt.Errorf("generic bound has no matching canonical protocol definition: %s", b.Protocol.ID.String())
			}
		}
	}
	return nil
}

func attachProtocolMetadata(result *PackageResult, pkg *types.Package, imp types.Importer) error {
	meta := protocols.Metadata{Version: protocols.MetadataVersion, Path: pkg.Path(), Package: pkg.Name()}
	if result.staticProtocols != nil {
		semantic := result.staticProtocols.SemanticMetadata()
		if semantic.Present() {
			if err := validateProtocolDeclarations(semantic, pkg, imp); err != nil {
				return err
			}
		}
		meta = result.staticProtocols.ExportMetadata()
	}
	known := map[string]protocols.Metadata{}
	for path, imported := range result.protocolImports {
		known[path] = imported
	}
	if result.useContext != nil {
		for _, binding := range result.useContext.metadata.Bindings {
			if binding.Kind != "protocol" || !binding.Public {
				continue
			}
			owner, ok := known[binding.OriginPath]
			if !ok {
				var err error
				owner, err = loadProtocolArtifact(binding.OriginPath, imp)
				if err != nil {
					return err
				}
				known[binding.OriginPath] = owner
			}
			d, ok := lookupProtocolDefinition(owner, binding.OriginName)
			if !ok {
				return fmt.Errorf("public protocol use has no canonical owner: %s::%s", binding.OriginPath, binding.OriginName)
			}
			present := false
			for _, prior := range meta.Definitions {
				present = present || prior.ID == d.ID
			}
			if !present {
				meta.Definitions = append(meta.Definitions, d)
			}
		}
	}
	existing, err := enumPackageMarker(pkg, protocolMetadataMarker)
	if err != nil {
		return err
	}
	if existing != "" {
		recovered, err := loadProtocolArtifact(pkg.Path(), imp)
		if err != nil {
			return err
		}
		if meta.Present() && recovered.Marker() != meta.Marker() {
			return fmt.Errorf("existing protocol metadata marker does not match recovered declarations")
		}
		if err := validateProtocolMetadata(recovered, pkg, imp); err != nil {
			return err
		}
		result.protocolMetadata = recovered
		return nil
	}
	if !meta.Present() {
		return nil
	}
	if err := validateProtocolMetadata(meta, pkg, imp); err != nil {
		return err
	}
	if err := validateProtocolCanonicalOwners(meta, known); err != nil {
		return err
	}
	result.protocolMetadata = meta
	for _, name := range sortedResultNames(*result) {
		if !strings.HasSuffix(name, ".gox") {
			continue
		}
		r := result.Files[name]
		r.Go = append(r.Go, []byte(fmt.Sprintf("\nconst %s = %q\n", protocolMetadataMarker, meta.Marker()))...)
		r.Expanded = r.Go
		refreshPackageMap(&r)
		result.Files[name] = r
		return nil
	}
	return fmt.Errorf("protocol metadata publication requires a generated goxide source file")
}
