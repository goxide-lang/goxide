package frontend

import (
	"fmt"
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/refs"
)

// Recover before syntax lowering: match needs a closed variant set, not merely
// the generated Go interface. A marker requires its matching producer artifact.
func recoverEnumImports(sources map[string][]byte, known map[string]PackageResult, imp types.Importer) (map[string]PackageResult, error) {
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
			if p, ok := out[path]; ok && len(p.enumMetadata.Enums) > 0 {
				continue
			}
			pkg, e := imp.Import(path)
			if e != nil {
				return nil, e
			}
			if pkg.Scope().Lookup(enumMetadataMarker) == nil {
				continue
			}
			provider, ok := imp.(refs.ArtifactImporter)
			if !ok {
				return nil, enumImportError(name, sources[name], int(spec.Pos())-1, "enum metadata source importer unavailable for "+path)
			}
			meta, e := loadEnumArtifact(path, provider)
			if e != nil {
				return nil, enumImportError(name, sources[name], int(spec.Pos())-1, e.Error())
			}
			p := out[path]
			p.enumMetadata = meta
			out[path] = p
		}
	}
	return out, nil
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
	for _, file := range sortedResultNames(result) {
		for _, d := range result.Files[file].Enums {
			if !d.Public || d.Family != "" {
				continue
			}
			x := enumExport{Name: d.Name, DefinitionPath: path, DefinitionName: d.Name, Marker: d.Marker, Validator: "Validate" + d.Name}
			for _, v := range d.Variants {
				x.Variants = append(x.Variants, enumVariantExport{Name: v.Name, HasPayload: v.Payload != "", Case: fmt.Sprintf("HgoCase%d_%s_%s", len(d.Name), d.Name, v.Name), Constructor: "New" + d.Name + v.Name})
			}
			canonical[path+"."+d.Name] = x
		}
	}
	for _, m := range result.enumImports {
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
		x, ok := canonical[origin.Pkg().Path()+"."+origin.Name()]
		if !ok {
			continue
		}
		// A new defined interface type does not inherit closed enum identity.
		if !types.Identical(obj.Type(), named) {
			continue
		}
		x.Name = name
		meta.Enums = append(meta.Enums, x)
	}
	sort.Slice(meta.Enums, func(i, j int) bool { return meta.Enums[i].Name < meta.Enums[j].Name })
	return meta, nil
}

// Import declarations can be added after the original import block without
// rewriting package text or producer-private names. This runs after enum syntax
// lowering, alongside the existing compiler-owned import binding prelude.
func insertEnumImports(name string, source []byte, imports string) ([]byte, error) {
	if imports == "" {
		return source, nil
	}
	f, e := parseSourceImports(name, source)
	if e != nil {
		return nil, e
	}
	if len(f.Decls) == 0 {
		return nil, fmt.Errorf("%s: enum origin import has no source import anchor", name)
	}
	off := int(f.Decls[len(f.Decls)-1].End()) - 1
	out := append([]byte(nil), source[:off]...)
	out = append(out, imports...)
	out = append(out, source[off:]...)
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
