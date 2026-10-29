package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const generationToolsMarker = "// Goxide generation dependencies."

func generationToolImports(owner *singleFileModule, files, generated map[string][]byte, validateAnchors bool, finalImports ...types.Importer) ([]string, string, error) {
	if owner == nil || len(owner.usedSourceRoots) == 0 {
		return nil, "", nil
	}
	imports := map[string]bool{}
	packageName := ""
	for index, sources := range []map[string][]byte{files, generated} {
		for name, data := range sources {
			if index == 0 && filepath.Ext(name) != ".go" || bytes.Contains(data, []byte(generationToolsMarker)) {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), name, data, parser.ImportsOnly|parser.SkipObjectResolution)
			if err != nil {
				return nil, "", err
			}
			packageName = file.Name.Name
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					return nil, "", err
				}
				imports[path] = true
			}
		}
	}
	retainedModules := map[string]bool{}
	if len(finalImports) != 0 {
		origin, ok := finalImports[0].(interface {
			PackageOrigin(string) (string, bool, error)
		})
		for imported := range imports {
			if !ok {
				break
			}
			for module := range owner.usedSourceRoots {
				if imported != module && !strings.HasPrefix(imported, module+"/") {
					continue
				}
				selected, standard, err := origin.PackageOrigin(imported)
				if err != nil {
					return nil, "", err
				}
				if !standard {
					retainedModules[selected] = true
				}
				break
			}
		}
	}
	var needed []string
	for module, namespace := range owner.usedSourceRoots {
		if !retainedModules[module] {
			needed = append(needed, namespace.PackagePath)
		}
	}
	slices.Sort(needed)
	needed = slices.Compact(needed)
	if validateAnchors && len(finalImports) != 0 && finalImports[0] != nil {
		for _, path := range needed {
			if _, err := finalImports[0].Import(path); err != nil {
				return nil, "", fmt.Errorf("generation dependency %s has no loadable Go package in the authored Go module graph; the library must publish stable Go artifacts: %w", path, err)
			}
		}
	}
	return needed, packageName, nil
}

// In-place generation publishes this with the package's other outputs. Go's
// tidy examines tagged files, while ordinary builds exclude macro-only imports.
func generationToolsSource(owner *singleFileModule, files, generated map[string][]byte, finalImports ...types.Importer) (string, []byte, error) {
	if owner == nil {
		return "", nil, nil
	}
	imports, packageName, err := generationToolImports(owner, files, generated, true, finalImports...)
	if err != nil {
		return "", nil, err
	}
	name := filepath.Join(filepath.Dir(owner.input), "goxide_tools_gox.go")
	previous, err := os.ReadFile(name)
	if err != nil && !os.IsNotExist(err) {
		return "", nil, err
	}
	if len(imports) == 0 && os.IsNotExist(err) {
		return "", nil, nil
	}
	if err == nil && (!bytes.HasPrefix(previous, []byte(generatedHeader)) || !bytes.Contains(previous, []byte(generationToolsMarker))) {
		return "", nil, fmt.Errorf("refusing to overwrite %s: not a generated dependency file", name)
	}
	if packageName == "" {
		for source, data := range generated {
			file, err := parser.ParseFile(token.NewFileSet(), source, data, parser.PackageClauseOnly)
			if err != nil {
				return "", nil, err
			}
			packageName = file.Name.Name
			break
		}
	}
	var source strings.Builder
	source.WriteString(generatedHeader)
	fmt.Fprintf(&source, "//go:build goxide_tools\n\n%s\n\npackage %s\n", generationToolsMarker, packageName)
	if len(imports) > 0 {
		source.WriteString("\nimport (\n")
		for _, path := range imports {
			fmt.Fprintf(&source, "\t_ %q\n", path)
		}
		source.WriteString(")\n")
	}
	return name, []byte(source.String()), nil
}

func reportGenerationTools(stderr io.Writer, owner *singleFileModule, files, generated map[string][]byte, validateAnchors bool, finalImports ...types.Importer) error {
	// Exported single-file output uses the prepared runtime graph, which may
	// exclude procedural macros. Its advisory anchors need not be loadable there.
	imports, _, err := generationToolImports(owner, files, generated, validateAnchors, finalImports...)
	if err != nil || len(imports) == 0 {
		return err
	}
	_, err = fmt.Fprintf(stderr, "generation-only dependencies need a Go reference to survive go mod tidy; use whole-package gen, or add a Go file with //go:build goxide_tools and blank imports of: %s\n", strings.Join(imports, ", "))
	return err
}
