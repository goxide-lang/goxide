package frontend

import (
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// Recover before syntax lowering: match needs a closed variant set, not merely
// the generated Go struct. A marker requires its matching producer artifact.
func recoverEnumImports(sources map[string][]byte, known map[string]PackageResult, imp types.Importer, maps map[string]*sourcemap.Map) (map[string]PackageResult, error) {
	out := map[string]PackageResult{}
	for p, r := range known {
		out[p] = r
	}
	if imp == nil {
		return out, nil
	}
	for _, name := range sortedSources(sources) {
		f, e := parseSourceImports(name, sources[name])
		if e != nil {
			continue
		}
		for _, spec := range f.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if path == "C" {
				continue
			}
			if p, ok := out[path]; ok && p.enumMetadata.present() {
				continue
			}
			pkg, e := imp.Import(path)
			if e != nil {
				return nil, e
			}
			if e := naming.Package(pkg); e != nil {
				return nil, e
			}
			if path == enums.SumPackage {
				meta, err := sharedSumMetadata(pkg, imp)
				if err != nil {
					return nil, err
				}
				p := out[path]
				p.enumMetadata = meta
				p.enumTypes = pkg
				out[path] = p
				continue
			}
			if pkg.Scope().Lookup(enumMetadataMarker) == nil {
				continue
			}
			provider, ok := imp.(refs.ArtifactImporter)
			if !ok {
				return nil, mappedEnumImportError(name, sources[name], maps[name], int(spec.Pos())-1, "enum metadata source importer unavailable for "+path)
			}
			meta, e := loadEnumArtifact(path, provider)
			if e != nil {
				return nil, mappedEnumImportError(name, sources[name], maps[name], int(spec.Pos())-1, e.Error())
			}
			p := out[path]
			p.enumMetadata = meta
			p.enumTypes = pkg
			out[path] = p
		}
	}
	return out, nil
}
func mappedEnumImportError(name string, source []byte, mapping *sourcemap.Map, offset int, message string) error {
	if mapping != nil {
		if segment := mapping.Lookup(offset); segment != nil && segment.Origin != nil {
			return &sourcemap.Error{Message: message, Origin: segment.Origin, Stack: segment.Stack}
		}
	}
	return enumImportError(name, source, offset, message)
}

func enumImportError(name string, source []byte, offset int, message string) error {
	if offset < 0 || offset > len(source) {
		offset = 0
	}
	prefix := string(source[:offset])
	line := strings.Count(prefix, "\n") + 1
	column := offset - strings.LastIndex(prefix, "\n")
	return fmt.Errorf("%s:%d:%d: %s", name, line, column, message)
}

// Metadata describes source enum identity and public helpers, never serialized
// ASTs or private payload type spellings. Go typing supplies actual payload types.
func collectEnumExports(path string, result PackageResult, pkg *types.Package) (enumMetadata, error) {
	meta := enumMetadata{Version: enumMetadataVersion, Path: path, Package: pkg.Name()}
	canonical := map[string]enumExport{}
	families := map[string]enumFamilyExport{}
	equalities := map[string]equalityExport{}
	defaults := map[string]defaultExport{}
	protocols := map[string]fieldProtocolExport{}
	for _, file := range sortedResultNames(result) {
		for name, kinds := range result.Files[file].fieldProtocols {
			for _, kind := range kinds {
				if !ast.IsExported(name) {
					continue
				}
				key := kind + "." + path + "." + name
				protocols[key] = fieldProtocolExport{Name: name, DefinitionPath: path, DefinitionName: name, Helper: kind + name, Kind: kind, Source: "derive"}
			}
		}
		for name, source := range result.Files[file].defaultRegistrations {
			if !ast.IsExported(name) {
				continue
			}
			x := defaultExport{Name: name, DefinitionPath: path, DefinitionName: name, Helper: "Default" + name, Source: source}
			if prior, ok := defaults[path+"."+name]; ok && prior != x {
				return meta, fmt.Errorf("conflicting Default registrations for %s", name)
			}
			defaults[path+"."+name] = x
		}
		for name, derive := range result.Files[file].equalityDerives {
			if !ast.IsExported(name) || !derive.PartialEq {
				continue
			}
			x := equalityExport{Name: name, DefinitionPath: path, DefinitionName: name, Helper: "Equals" + name, Eq: derive.Eq}
			x.PartialEqSource = "derive"
			if derive.ManualPartialEq {
				x.PartialEqSource = "manual"
			}
			if derive.Eq {
				x.EqSource = "derive"
				if derive.ManualEq {
					x.EqSource = "manual"
				}
			}

			if prior, ok := equalities[path+"."+name]; ok && prior != x {
				return meta, fmt.Errorf("conflicting equality derivations for %s", name)
			}
			equalities[path+"."+name] = x
		}
		for _, d := range result.Files[file].Enums {
			if d.Family != "" {
				// Imported families can also appear here after typed lowering
				// binds their helpers in a consumer file. Their registrations
				// come from verified enumImports below, not a local validator.
				if d.PackagePath != "" {
					continue
				}
				key := enums.SumPackage + "." + d.Family
				f := families[key]
				if f.Name == "" {
					f = sharedSumFamily(d.Family)
				}
				validator := d.ExportValidator
				found := false
				for _, x := range f.Instances {
					found = found || x.Validator == validator
				}
				if !found {
					f.Instances = append(f.Instances, enumFamilyInstanceExport{ValidatorPath: path, Validator: validator})
				}
				families[key] = f
				continue
			}
			if !d.Public {
				continue
			}
			x := enumExport{Name: d.Name, DefinitionPath: path, DefinitionName: d.Name, Tag: fmt.Sprintf("GoxideTag%d_%s", len(d.Name), d.Name), TagField: d.TagField, Validator: "Validate" + d.Name}
			for _, v := range d.Variants {
				variant := enumVariantExport{Name: v.Name, HasPayload: v.Payload != "", Tag: v.Tag, Constructor: "New" + d.Name + v.Name}
				if variant.HasPayload {
					variant.Field, variant.PayloadAlias = v.Field, v.PayloadAlias
					variant.Value = fmt.Sprintf("GoxideValue%d_%s_%s", len(d.Name), d.Name, v.Name)
					variant.Project = fmt.Sprintf("GoxideProject%d_%s_%s", len(d.Name), d.Name, v.Name)
				}
				x.Variants = append(x.Variants, variant)
			}
			canonical[path+"."+d.Name] = x
		}
	}
	for _, m := range result.enumImports {
		for _, x := range m.FieldProtocols {
			protocols[x.Kind+"."+x.DefinitionPath+"."+x.DefinitionName] = x
		}
		for _, x := range m.Defaults {
			defaults[x.DefinitionPath+"."+x.DefinitionName] = x
		}
		for _, x := range m.Equality {
			equalities[x.DefinitionPath+"."+x.DefinitionName] = x
		}
		for _, x := range m.Families {
			key := x.DefinitionPath + "." + x.DefinitionName
			if prior, ok := families[key]; ok {
				for _, v := range x.Instances {
					found := false
					for _, p := range prior.Instances {
						found = found || p == v
					}
					if !found {
						prior.Instances = append(prior.Instances, v)
					}
				}
				families[key] = prior
			} else {
				families[key] = x
			}
		}
		for _, x := range m.Enums {
			canonical[x.DefinitionPath+"."+x.DefinitionName] = x
		}
	}
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || !ast.IsExported(name) {
			continue
		}
		named, ok := types.Unalias(obj.Type()).(*types.Named)
		if !ok || named.Obj().Pkg() == nil {
			continue
		}
		origin := named.Obj()
		for _, kind := range []string{"Clone", "Debug", "Serialize", "Deserialize"} {
			if x, ok := protocols[kind+"."+origin.Pkg().Path()+"."+origin.Name()]; ok {
				x.Name = name
				meta.FieldProtocols = append(meta.FieldProtocols, x)
			}
		}
		if x, ok := defaults[origin.Pkg().Path()+"."+origin.Name()]; ok {
			x.Name = name
			meta.Defaults = append(meta.Defaults, x)
		}
		if x, ok := equalities[origin.Pkg().Path()+"."+origin.Name()]; ok {
			x.Name = name
			meta.Equality = append(meta.Equality, x)
		}
		if f, ok := families[origin.Pkg().Path()+"."+origin.Name()]; ok {
			f.Name = name
			sort.Slice(f.Instances, func(i, j int) bool {
				return f.Instances[i].ValidatorPath+"."+f.Instances[i].Validator < f.Instances[j].ValidatorPath+"."+f.Instances[j].Validator
			})
			meta.Families = append(meta.Families, f)
			continue
		}
		x, ok := canonical[origin.Pkg().Path()+"."+origin.Name()]
		if !ok {
			continue
		}
		// A new defined struct type does not inherit closed enum identity.
		if !types.Identical(obj.Type(), named) {
			continue
		}
		x.Name = name
		meta.Enums = append(meta.Enums, x)
	}
	sort.Slice(meta.Enums, func(i, j int) bool { return meta.Enums[i].Name < meta.Enums[j].Name })
	sort.Slice(meta.Families, func(i, j int) bool { return meta.Families[i].Name < meta.Families[j].Name })
	sort.Slice(meta.FieldProtocols, func(i, j int) bool {
		a, b := meta.FieldProtocols[i], meta.FieldProtocols[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Kind < b.Kind
	})
	sort.Slice(meta.Defaults, func(i, j int) bool { return meta.Defaults[i].Name < meta.Defaults[j].Name })
	sort.Slice(meta.Equality, func(i, j int) bool { return meta.Equality[i].Name < meta.Equality[j].Name })
	return meta, nil
}

// Import declarations can be added after the original import block without
// rewriting package text or producer-private names. This runs after enum syntax
// lowering, alongside the existing compiler-owned import binding prelude.
func insertEnumImports(name string, source []byte, imports string, provenance *sourcemap.Map) ([]byte, error) {
	if imports == "" {
		return source, nil
	}
	f, e := parseSourceImports(name, source)
	if e != nil {
		return nil, e
	}
	off := int(f.Name.End()) - 1
	if len(f.Decls) > 0 {
		off = int(f.Decls[len(f.Decls)-1].End()) - 1
	}
	out := append([]byte(nil), source[:off]...)
	out = append(out, imports...)
	out = append(out, source[off:]...)
	if provenance != nil {
		next := &sourcemap.Map{Source: out, Stages: append(append([]string(nil), provenance.Stages...), "enumImports")}
		next.Segments = append(next.Segments, provenance.Copy(0, off, 0)...)
		next.Segments = append(next.Segments, provenance.Copy(off, len(source), off+len(imports))...)
		*provenance = *next
	}
	return out, nil
}

// Go permits internal imports only below the enclosing import-path prefix.
// This gate concerns generated imports, not whether a public Go facade is usable.
func enumOriginImportAllowed(consumer, origin string) bool {
	parts := strings.Split(origin, "/")
	for i, p := range parts {
		if p == "internal" {
			if i == 0 {
				return false
			}
			parent := strings.Join(parts[:i], "/")
			if consumer != parent && !strings.HasPrefix(consumer, parent+"/") {
				return false
			}
		}
	}
	return true
}
func checkUnavailableEnumSyntax(name string, source []byte, blocked map[string]string) error {
	if len(blocked) == 0 {
		return nil
	}
	fs := token.NewFileSet()
	file := fs.AddFile(name, -1, len(source))
	var scan scanner.Scanner
	scan.Init(file, source, func(token.Position, string) {}, 0)
	type item struct {
		pos  token.Pos
		text string
	}
	var window []item
	for {
		p, k, v := scan.Scan()
		if k == token.EOF {
			break
		}
		if v == "" {
			v = k.String()
		}
		window = append(window, item{p, v})
		if len(window) > 5 {
			window = window[1:]
		}
		if len(window) == 5 && window[1].text == "." && window[3].text == ":" && window[4].text == ":" {
			if reason := blocked[window[0].text+"."+window[2].text]; reason != "" {
				return enumImportError(name, source, file.Offset(window[0].pos), reason)
			}
		}
	}
	return nil
}
