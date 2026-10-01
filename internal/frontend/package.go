package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
)

// PackageResult keeps a separate output and origin map for every source file.
type PackageResult struct {
	macros   *macros.Package
	Files    map[string]Result
	imported map[string][]enums.Decl
}

// preparePackage performs syntax lowering with shared declaration metadata. It
// never concatenates sources: imports retain their original file scope.
func preparePackage(sources map[string][]byte) (PackageResult, error) {
	return preparePackageImports(sources, nil)
}

func preparePackageImports(sources map[string][]byte, metadata map[string]PackageResult) (PackageResult, error) {
	return preparePackageContext("", sources, metadata, nil)
}
func preparePackageContext(path string, sources map[string][]byte, metadata map[string]PackageResult, imp types.Importer, budgets ...*macros.Budget) (PackageResult, error) {
	names := sortedSources(sources)
	cfg := &compileConfig{declarations: []enums.Decl{}, named: map[string]ast.Expr{}, models: map[string]model{}}
	cfg.imported, cfg.preludes = importedDeclarations(sources, metadata)
	dependencies := map[string]*macros.Package{}
	for path, p := range metadata {
		if p.macros != nil {
			dependencies[path] = p.macros
		}
	}
	registry, err := macros.CollectPackage(path, sources, dependencies)
	if err != nil {
		return PackageResult{}, err
	}
	cfg.macros, cfg.macroImporter = registry, imp
	cfg.macroBudget = &macros.Budget{}
	buildBudget := &macros.Budget{}
	if len(budgets) > 0 && budgets[0] != nil {
		buildBudget = budgets[0]
	}
	if err := buildBudget.AddPackage(registry); err != nil {
		return PackageResult{}, err
	}
	seen := map[string]string{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".hgo") {
			continue
		}
		meta, err := enums.CollectPackage(name, sources[name])
		if err != nil {
			return PackageResult{}, err
		}
		for _, d := range meta.Enums {
			if prior := seen[d.Name]; prior != "" {
				return PackageResult{}, fmt.Errorf("%s: duplicate enum %s (also in %s)", name, d.Name, prior)
			}
			if ast.IsExported(d.Name) && !d.Public {
				return PackageResult{}, fmt.Errorf("%s: exported enum %s requires pub in directory mode", d.Position, d.Name)
			}
			seen[d.Name] = name
			cfg.declarations = append(cfg.declarations, d)
		}
		for n, t := range meta.Types {
			if _, ok := cfg.named[n]; ok {
				return PackageResult{}, fmt.Errorf("%s: duplicate type %s", name, n)
			}
			cfg.named[n] = t
		}
	}
	// Native declarations participate in enum payload analysis too.
	for _, name := range names {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, sources[name], 0)
		if err != nil {
			return PackageResult{}, err
		}
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, sp := range gd.Specs {
					t := sp.(*ast.TypeSpec)
					cfg.named[t.Name.Name] = t.Type
				}
			}
		}
	}
	cfg.collect = true
	for _, name := range names {
		if !strings.HasSuffix(name, ".hgo") {
			continue
		}
		r, err := compile(name, sources[name], cfg)
		if err != nil {
			return PackageResult{}, err
		}
		for n, m := range r.models {
			cfg.models[n] = m
		}
	}
	cfg.collect = false
	cfg.macroBudget = buildBudget
	result := PackageResult{macros: registry, Files: map[string]Result{}, imported: cfg.imported}
	generated := map[string]string{}
	for index, name := range names {
		if !strings.HasSuffix(name, ".hgo") {
			result.Files[name] = Result{Go: sources[name]}
			continue
		}
		r, err := compile(name, sources[name], cfg)
		if err != nil {
			return PackageResult{}, err
		}
		if bytes.Contains(r.Go, []byte("__hgo_ref_")) {
			return PackageResult{}, fmt.Errorf("%s: reference syntax in directory mode is not yet supported", name)
		}
		if err = packageGeneratedNames(name, &r, index, generated); err != nil {
			return PackageResult{}, err
		}
		result.Files[name] = r
	}
	return result, nil
}

// CompilePackage checks the complete package under its actual module import path.
// sources may contain .hgo and native .go files already selected for this build.
func CompilePackage(path string, sources map[string][]byte, imp types.Importer) (PackageResult, error) {
	result, err := preparePackageContext(path, sources, nil, imp)
	if err != nil {
		return result, err
	}
	return finishPackage(path, result, imp)
}
func finishPackage(path string, result PackageResult, imp types.Importer) (PackageResult, error) {
	files := map[string][]byte{}
	var families []enums.Decl
	var conversions []propagate.Conversion
	for name, r := range result.Files {
		files[name] = r.Go
		families = append(families, r.Enums...)
		conversions = append(conversions, r.conversions...)
	}
	for index, name := range sortedSources(files) {
		r := result.Files[name]
		if !strings.HasSuffix(name, ".hgo") {
			continue
		}
		lowered, err := propagate.Lower(name, r.Go, propagate.Config{Families: append(append([]enums.Decl{}, families...), result.imported[name]...), From: conversions, Importer: imp, Provenance: r.Diagnostics, Files: files, PackagePath: path, Namespace: fmt.Sprintf("f%d", index)})
		if err != nil {
			return result, stageError("package propagate.Lower", err)
		}
		r.Go = lowered
		r.Expanded = lowered
		refreshPackageMap(&r)
		files[name] = lowered
		result.Files[name] = r
	}
	if err := typecheck.CheckPackage(path, files, imp); err != nil {
		if te, ok := err.(types.Error); ok {
			p := te.Fset.Position(te.Pos)
			mapped := &sourcemap.Error{Message: te.Msg, Generated: p}
			if r, ok := result.Files[p.Filename]; ok && r.Diagnostics != nil {
				if seg := r.Diagnostics.Lookup(p.Offset); seg != nil {
					mapped.Origin = seg.Origin
					mapped.Stack = seg.Stack
				}
			}
			return result, mapped
		}
		return result, err
	}
	return result, nil
}
func sortedSources(s map[string][]byte) []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type packageEdit struct {
	start, end int
	text       string
}

func packageGeneratedNames(name string, r *Result, index int, seen map[string]string) error {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, name, r.Go, parser.ParseComments)
	if err != nil {
		return err
	}
	var edits []packageEdit
	exportedBuiltins := map[string]bool{}
	for _, d := range r.Enums {
		if d.Family == "" {
			continue
		}
		exportedBuiltins["HgoInst"+strings.TrimPrefix(d.Validator, "__hgo_enum_inst_")] = true
		for _, v := range d.Variants {
			exportedBuiltins["Hgo"+d.Family+v.Name] = true
			exportedBuiltins["HgoNew"+d.Family+v.Name] = true
		}
	}
	// Generic builtin definitions are shared by the package. Repeated concrete
	// validators must agree exactly; a conflict is rejected instead of guessed.
	for _, d := range f.Decls {
		key := ""
		switch d := d.(type) {
		case *ast.FuncDecl:
			key = d.Name.Name
			if d.Recv != nil {
				var b bytes.Buffer
				format.Node(&b, fs, d.Recv.List[0].Type)
				key = b.String() + "." + key
			}
		case *ast.GenDecl:
			if len(d.Specs) == 1 {
				if t, ok := d.Specs[0].(*ast.TypeSpec); ok {
					key = t.Name.Name
				}
			}
		}
		builtin := exportedBuiltins[key] || strings.HasPrefix(key, "__hgo_enum_inst_") || strings.Contains(key, "__hgo_enum_case_Result_") || strings.Contains(key, "__hgo_enum_case_Option_") || strings.HasPrefix(key, "__hgo_enum_ctor_Result_") || strings.HasPrefix(key, "__hgo_enum_ctor_Option_") || key == "Result" || key == "Option" || key == "__hgo_enum_validate_Result" || key == "__hgo_enum_validate_Option"
		if !builtin {
			continue
		}
		var b bytes.Buffer
		format.Node(&b, fs, d)
		if prior, ok := seen[key]; ok {
			if prior != b.String() {
				return fmt.Errorf("%s: conflicting package builtin metadata %s", name, key)
			}
			edits = append(edits, packageEdit{fs.Position(d.Pos()).Offset, fs.Position(d.End()).Offset, ""})
		} else {
			seen[key] = b.String()
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && (strings.HasPrefix(id.Name, "__hgo_write") || strings.HasPrefix(id.Name, "__hgo_read")) {
			edits = append(edits, packageEdit{fs.Position(id.Pos()).Offset, fs.Position(id.End()).Offset, fmt.Sprintf("%s_f%d", id.Name, index)})
		}
		return true
	})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	cursor := 0
	mapping := &sourcemap.Map{Stages: append(r.Diagnostics.Stages, "package.generated_names")}
	for _, e := range edits {
		if e.start < cursor {
			continue
		}
		mapping.Segments = append(mapping.Segments, r.Diagnostics.Copy(cursor, e.start, out.Len())...)
		out.Write(r.Go[cursor:e.start])
		out.WriteString(e.text)
		cursor = e.end
	}
	mapping.Segments = append(mapping.Segments, r.Diagnostics.Copy(cursor, len(r.Go), out.Len())...)
	out.Write(r.Go[cursor:])
	mapping.Source = out.Bytes()
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return err
	}
	r.Diagnostics = mapping.Format(out.Bytes(), formatted, "package.format")
	r.Go = formatted
	r.Expanded = formatted
	refreshPackageMap(r)
	return nil
}
func refreshPackageMap(r *Result) {
	var obj map[string]any
	if json.Unmarshal(r.SourceMap, &obj) != nil {
		obj = map[string]any{}
	}
	obj["diagnostics"] = r.Diagnostics
	if ds, ok := obj["derives"].([]any); ok {
		for _, item := range ds {
			if d, ok := item.(map[string]any); ok {
				if helper, ok := d["helper"].(string); ok {
					needle := "func " + helper + "("
					if recv, method, ok := strings.Cut(helper, "."); ok {
						needle = "func (v " + recv + ") " + method + "("
					}
					d["generatedByteOffset"] = bytes.Index(r.Go, []byte(needle))
				}
			}
		}
	}
	r.SourceMap, _ = json.MarshalIndent(obj, "", "  ")
	r.SourceMap = append(r.SourceMap, '\n')
}
