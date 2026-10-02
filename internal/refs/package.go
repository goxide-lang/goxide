package refs

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
)

func sourceNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// deduplicateIR removes only compiler-owned reference declarations. Individual
// source file imports and user declarations always retain their own AST scope.
func deduplicateIR(files []*ast.File) {
	seen := map[string]bool{}
	for _, file := range files {
		decls := file.Decls[:0]
		for _, decl := range file.Decls {
			name := ""
			switch d := decl.(type) {
			case *ast.FuncDecl:
				name = d.Name.Name
			case *ast.GenDecl:
				if len(d.Specs) == 1 {
					if t, ok := d.Specs[0].(*ast.TypeSpec); ok {
						name = t.Name.Name
					}
				}
			}
			if strings.HasPrefix(name, prefix) {
				if seen[name] {
					continue
				}
				seen[name] = true
			}
			decls = append(decls, decl)
		}
		file.Decls = decls
	}
}

// FinishPackage validates every selected file against the same nominal
// permission-bearing signatures, then erases the package as a single operation.
// Frontend callers bind imported metadata before this check and publish exports
// only after final Go validation; ordinary Go pointer APIs are not reclassified.
func FinishPackage(files map[string][]byte, imp types.Importer) (map[string]Result, error) {
	active := false
	for _, src := range files {
		active = active || strings.Contains(string(src), "type __hgo_ref_write[")
	}
	result := map[string]Result{}
	if !active {
		for n, s := range files {
			result[n] = Result{Source: s}
		}
		return result, nil
	}

	for _, name := range sourceNames(files) {
		restored, err := restore(name, files[name], imp, files)
		if err != nil {
			return nil, err
		}
		lowered, err := lowerWithFiles(name, restored, imp, false, files)
		if err != nil {
			return nil, err
		}
		result[name] = lowered
	}
	return result, nil
}
