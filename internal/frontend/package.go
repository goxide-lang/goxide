package frontend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/equality"
	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/printing"
	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
)

// PackageResult keeps a separate output and origin map for every source file.
type PackageResult struct {
	payloadEnums     func(types.Type) (propagate.PayloadEnum, bool, error)
	uses             useMetadata
	useContext       *usePackage
	equalityImports  []equality.Protocol
	enumMarkerFile   string
	enumTypes        *types.Package
	enumMetadata     enumMetadata
	enumImports      map[string]enumMetadata
	references       refs.Metadata
	referenceImports map[string]refs.Metadata
	macros           *macros.Package
	Files            map[string]Result
	imported         map[string][]enums.Decl
}

// preparePackage performs syntax lowering with shared declaration metadata. It
// never concatenates sources: imports retain their original file scope.
func preparePackage(sources map[string][]byte) (PackageResult, error) {
	return preparePackageImports(sources, nil)
}

func preparePackageImports(sources map[string][]byte, metadata map[string]PackageResult) (PackageResult, error) {
	return preparePackageContext("", sources, metadata, nil, nil)
}
func preparePackageContext(path string, sources map[string][]byte, metadata map[string]PackageResult, imp types.Importer, roots paths.Roots, budgets ...*macros.Budget) (PackageResult, error) {
	return prepareCompilationContext(path, sources, metadata, imp, false, roots, budgets...)
}

// A single file shares registered import/declaration binding, not the directory
// publication policy or package finishing pipeline.
func prepareCompilationContext(path string, sources map[string][]byte, metadata map[string]PackageResult, imp types.Importer, single bool, roots paths.Roots, budgets ...*macros.Budget) (PackageResult, error) {
	for name, source := range sources {
		if err := naming.Source(name, source); err != nil {
			return PackageResult{}, err
		}
	}
	useContext, err := prepareUses(path, sources, metadata, imp, roots, single)
	if err != nil {
		return PackageResult{}, err
	}
	sources = useContext.prepared
	names := sortedSources(sources)
	cfg := &compileConfig{single: single, protocolFunctions: map[string][]*ast.FuncDecl{}, packagePath: path, builtinTypes: map[string]macros.TypeContext{}, declarations: []enums.Decl{}, named: map[string]ast.Expr{}, models: map[string]model{}, manualDefaults: map[string]manualDefault{}, manualEquality: map[string]manualEquality{}, typeSpecs: map[string]*ast.TypeSpec{}}
	cfg.uses = useContext
	for _, name := range names {
		if !isGoxideInput(name, single) {
			continue
		}
		_, manual, err := lowerManualDefaults(name, sources[name])
		if err != nil {
			return PackageResult{}, err
		}
		for n, m := range manual {
			if prior, ok := cfg.manualDefaults[n]; ok {
				return PackageResult{}, fmt.Errorf("%s: duplicate Default implementation for %s (also %s)", m.position, n, prior.position)
			}
			cfg.manualDefaults[n] = m
		}
	}
	if single && path == "" && sourcesImport(sources, enums.SumPackage) {
		// Standalone/WASM has no consumer owner for arbitrary user artifacts.
		// std's compiler-known canonical contract does not need such an owner.
		pkg, importErr := imp.Import(enums.SumPackage)
		if importErr != nil {
			return PackageResult{}, importErr
		}
		meta, metadataErr := sharedSumMetadata(pkg, imp)
		if metadataErr != nil {
			return PackageResult{}, metadataErr
		}
		metadata = map[string]PackageResult{enums.SumPackage: {enumTypes: pkg, enumMetadata: meta}}
	} else {
		metadata, err = recoverEnumImports(sources, metadata, imp, useContext.preparedMaps)
	}
	if err != nil {
		return PackageResult{}, err
	}
	cfg.enumRegistry = metadata
	cfg.imported, cfg.preludes, cfg.enumGoImports, cfg.enumUnavailable = importedDeclarations(path, sources, metadata, single)
	// Share only unique package bindings, never file-local import spellings.
	for _, name := range names {
		for _, d := range cfg.imported[name] {
			if d.Family == "" {
				d.LookupName = ""
				cfg.declarations = append(cfg.declarations, d)
			}
		}
	}
	eqPrelude, eqImports, eqProtocols := importedEquality(path, sources, metadata, single)
	for n, p := range eqPrelude {
		cfg.preludes[n] += p
	}
	for n, p := range eqImports {
		cfg.enumGoImports[n] += p
	}

	defaultPrelude, defaultImports, defaultProtocols := importedDefaults(path, sources, metadata, single)
	cfg.defaultImports = defaultProtocols
	for n, p := range defaultPrelude {
		cfg.preludes[n] += p
	}
	for n, p := range defaultImports {
		cfg.enumGoImports[n] += p
	}

	fieldPrelude, fieldImports, fieldProtocols := importedFieldProtocols(path, sources, metadata, single)
	cfg.fieldImports = fieldProtocols
	for n, p := range fieldPrelude {
		cfg.preludes[n] += p
	}
	for n, p := range fieldImports {
		cfg.enumGoImports[n] += p
	}

	dependencies := map[string]*macros.Package{}
	for path, p := range metadata {
		if p.macros != nil {
			dependencies[path] = p.macros
		}
	}
	macroMaps := map[string]*sourcemap.Map{}
	for name := range sources {
		macroMaps[name] = useContext.sourceMap(name)
	}
	registry, err := macros.CollectPackage(path, sources, dependencies, useContext.resolveDerive, macroMaps)
	if err != nil {
		return PackageResult{}, err
	}
	if err := useContext.bindMacros(registry, dependencies); err != nil {
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
		if !isGoxideInput(name, single) {
			continue
		}
		meta, err := enums.CollectPackage(name, sources[name])
		if err != nil {
			return PackageResult{}, err
		}
		for _, d := range meta.Enums {
			if single && d.Public {
				return PackageResult{}, fmt.Errorf("%s: pub enum requires package compilation", d.Position)
			}
			if prior := seen[d.Name]; prior != "" {
				return PackageResult{}, fmt.Errorf("%s: duplicate enum %s (also in %s)", name, d.Name, prior)
			}
			if !single && ast.IsExported(d.Name) && !d.Public {
				return PackageResult{}, fmt.Errorf("%s: exported enum %s requires pub in directory mode", d.Position, d.Name)
			}
			seen[d.Name] = name
			cfg.declarations = append(cfg.declarations, d)
		}
		if parsed, e := parseSourceImports(name, sources[name]); e == nil {
			context := macros.TypeContext{Types: map[string]*ast.TypeSpec{}, Imports: parsed.Imports}
			for n, t := range meta.Types {
				context.Types[n] = &ast.TypeSpec{Name: ast.NewIdent(n), Type: t}
			}
			cfg.builtinTypes[name] = context
		}
		for n, t := range meta.Types {
			if _, ok := cfg.named[n]; ok {
				return PackageResult{}, fmt.Errorf("%s: duplicate type %s", name, n)
			}
			cfg.named[n] = enums.BindPayloadType(t, cfg.imported[name])
		}
	}
	// Native declarations participate in enum payload analysis too.
	for _, name := range names {
		if single || !strings.HasSuffix(name, ".go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, sources[name], 0)
		if err != nil {
			return PackageResult{}, err
		}
		context := macros.TypeContext{Types: map[string]*ast.TypeSpec{}, Imports: f.Imports}
		for n, sp := range typeSpecs(f) {
			context.Types[n] = sp
			cfg.typeSpecs[n] = sp
		}
		cfg.builtinTypes[name] = context
		for _, decl := range f.Decls {
			if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, sp := range gd.Specs {
					t := sp.(*ast.TypeSpec)
					cfg.named[t.Name.Name] = enums.BindPayloadType(t.Type, cfg.imported[name])
				}
			}
		}
	}
	cfg.collect = true
	for _, name := range names {
		if !isGoxideInput(name, single) {
			continue
		}
		r, err := compile(name, sources[name], cfg)
		if err != nil {
			return PackageResult{}, err
		}
		if err = mergeManualEquality(cfg.manualEquality, r.manualEquality); err != nil {
			return PackageResult{}, err
		}
		cfg.protocolFunctions[name] = r.typeFunctions
		cfg.sumDeclarations = append(cfg.sumDeclarations, r.Enums...)
		context := cfg.builtinTypes[name]
		context.Types = r.typeSpecs
		context.Imports = r.typeImports
		cfg.builtinTypes[name] = context
		for n, sp := range r.typeSpecs {
			cfg.typeSpecs[n] = sp
		}
		for n, m := range r.models {
			cfg.models[n] = m
		}
	}
	aliasProtocols, err := bindLocalProtocolAliases(path, sources, cfg, metadata, imp)
	if err != nil {
		return PackageResult{}, err
	}
	eqProtocols = append(eqProtocols, aliasProtocols...)
	cfg.equalityImports = eqProtocols
	cfg.collect = false
	cfg.macroBudget = buildBudget
	result := PackageResult{payloadEnums: payloadEnumResolver(path, metadata, imp), useContext: useContext, equalityImports: eqProtocols, enumImports: map[string]enumMetadata{}, macros: registry, Files: map[string]Result{}, imported: cfg.imported, referenceImports: map[string]refs.Metadata{}}
	for path, p := range metadata {
		result.referenceImports[path] = p.references
		result.enumImports[path] = p.enumMetadata
	}
	generated := map[string]string{}
	for index, name := range names {
		if !isGoxideInput(name, single) {
			result.Files[name] = Result{Go: sources[name]}
			if name == useContext.generatedFile {
				r := result.Files[name]
				r.Diagnostics = useContext.generatedMap
				result.Files[name] = r
			}
			continue
		}
		r, err := compile(name, sources[name], cfg)
		if err != nil {
			return PackageResult{}, err
		}
		if !single {
			if err = packageGeneratedNames(name, &r, index, generated); err != nil {
				return PackageResult{}, err
			}
		}
		result.Files[name] = r
	}
	return result, nil
}

// CompilePackage checks the complete package under its actual module import path.
// sources may contain .gox and native .go files already selected for this build.
func CompilePackage(path string, sources map[string][]byte, imp types.Importer) (PackageResult, error) {
	return CompilePackageWithOptions(path, sources, PackageOptions{Importer: imp})
}

// PackageOptions supplies the explicit namespace roots and type graph shared by
// every source file. Roots are available to use declarations, not implicit names.
type PackageOptions struct {
	Roots         paths.Roots
	Importer      types.Importer
	Configuration string // Planner-owned build context, before the roots fingerprint.
}

func CompilePackageWithOptions(path string, sources map[string][]byte, options PackageOptions) (PackageResult, error) {
	sources, _, err := packageConfigurationSources(sources, options.Roots, options.Configuration, "")
	if err != nil {
		return PackageResult{}, err
	}
	result, err := preparePackageContext(path, sources, nil, options.Importer, options.Roots)
	if err != nil {
		return result, err
	}
	return finishPackage(path, result, options.Importer)
}
func finishPackage(path string, result PackageResult, imp types.Importer) (PackageResult, error) {
	if imp == nil {
		names := sortedResultNames(result)
		if len(names) > 0 {
			imp = typecheck.NewImporter(names[0])
		}
	}
	files := map[string][]byte{}
	var families []enums.Decl
	var conversions []propagate.Conversion
	for _, name := range sortedResultNames(result) {
		r := result.Files[name]
		files[name] = r.Go
		families = append(families, r.Enums...)
		conversions = append(conversions, r.conversions...)
	}
	// Bind imported permissions before expression inference sees native pointer
	// signatures. Keep source signatures for export; restore helpers only after
	// the complete permission check. This does not authorize a native bridge.
	referenceIR := map[string][]byte{}
	for n, s := range files {
		referenceIR[n] = s
	}
	referenceDecls := map[string][]enums.Decl{}
	for _, name := range sortedSources(files) {
		referenceDecls[name] = append(append([]enums.Decl(nil), families...), result.imported[name]...)
	}
	_, enumTypes, _, err := enumReferenceEvidence(path, files, imp, referenceDecls, result.enumImports)
	if err != nil {
		return result, err
	}
	bound, bindings, err := refs.BindImportsWithPath(path, files, imp, result.referenceImports, enumTypes)
	if err != nil {
		if e, ok := err.(*refs.ImportError); ok {
			mapped := &sourcemap.Error{Message: e.Message, Generated: e.Position}
			if r, ok := result.Files[e.Position.Filename]; ok && r.Diagnostics != nil {
				if seg := r.Diagnostics.Lookup(e.Position.Offset); seg != nil {
					mapped.Origin = seg.Origin
					mapped.Stack = seg.Stack
				}
			}
			return result, mapped
		}
		return result, err
	}
	if result.useContext != nil {
		changed := false
		for name, source := range bound {
			changed = changed || !bytes.Equal(files[name], source)
		}
		if changed {
			beforeMaps := map[string]*sourcemap.Map{}
			for name, r := range result.Files {
				beforeMaps[name] = r.Diagnostics
			}
			if err := result.useContext.validateSelectors(path, files, beforeMaps, imp); err != nil {
				return result, err
			}
		}
	}
	for name, source := range bound {
		r := result.Files[name]
		if !bytes.Equal(r.Go, source) {
			if r.Diagnostics != nil {
				r.Diagnostics = bindings.MapSource(name, source, r.Diagnostics)
			}
			r.Go = source
			r.Expanded = source
			refreshPackageMap(&r)
			result.Files[name] = r
		}
		files[name] = source
	}

	// Resolve cross-file initializer types before file-local question lowering.
	adapterNodes := 0
	adapterMaps := map[string]*sourcemap.Map{}
	for n, r := range result.Files {
		adapterMaps[n] = r.Diagnostics
	}
	payloadEnums := result.payloadEnums
	adapterFiles, adapterFamilies, err := propagate.PrepareGoResults(files, propagate.Config{ResolvePayloadEnum: payloadEnums, Families: families, FileFamilies: result.imported, Importer: imp, PackagePath: path, GoResultNodes: &adapterNodes}, adapterMaps)
	if err != nil {
		return result, stageError("package Go result inference", err)
	}
	for _, n := range sortedSources(adapterFiles) {
		source := adapterFiles[n]
		r := result.Files[n]
		r.Go = source
		r.Expanded = source
		r.Diagnostics = adapterMaps[n]
		r.Enums = append(r.Enums, adapterFamilies[n]...)
		families = append(families, adapterFamilies[n]...)
		result.Files[n] = r
		files[n] = source
	}
	eqCfg := equality.Config{Native: map[string]bool{}, Importer: imp, PackagePath: path, Derives: map[string]equality.Derive{}, Enums: append([]enums.Decl(nil), families...), Imported: result.equalityImports, Maps: map[string]*sourcemap.Map{}}
	for name, r := range result.Files {
		eqCfg.Native[name] = strings.HasSuffix(name, ".go")
		for n, d := range r.equalityDerives {
			d.File = name
			eqCfg.Derives[n] = d
		}
		eqCfg.Maps[name] = r.Diagnostics
		eqCfg.Enums = append(eqCfg.Enums, result.imported[name]...)
	}
	eqPrepared, err := equality.Prepare(files, eqCfg)
	if err != nil {
		return result, stageError("equality.Prepare", err)
	}
	for name, source := range eqPrepared {
		r := result.Files[name]
		r.Go = source
		r.Expanded = source
		r.Diagnostics = eqCfg.Maps[name]
		result.Files[name] = r
		files[name] = source
	}
	for index, name := range sortedSources(files) {
		r := result.Files[name]
		if !strings.HasSuffix(name, ".gox") {
			continue
		}
		if r.matchReferenceTemps == nil {
			r.matchReferenceTemps = map[string]bool{}
		}
		var inferred []enums.Decl
		lowered, err := propagate.Lower(name, r.Go, propagate.Config{RewriteExpanded: result.useContext.rewriteExpanded, ResolvePayloadEnum: payloadEnums, FileFamilies: result.imported, InferredFamilies: &inferred, GoResultNodes: &adapterNodes, Families: append(append([]enums.Decl{}, families...), result.imported[name]...), From: conversions, Importer: imp, Provenance: r.Diagnostics, Files: files, PackagePath: path, Namespace: fmt.Sprintf("f%d", index), MatchReferenceTemps: r.matchReferenceTemps})
		if err != nil {
			return result, stageError("package propagate.Lower", err)
		}
		r.Enums = append(r.Enums, inferred...)
		families = append(families, inferred...)
		eqCfg.Enums = append(eqCfg.Enums, inferred...)
		r.Go = lowered
		r.Expanded = lowered
		refreshPackageMap(&r)
		files[name] = lowered
		result.Files[name] = r
	}
	eqLowered, err := equality.Lower(files, eqCfg)
	if err != nil {
		return result, stageError("equality.Lower", err)
	}
	for name, source := range eqLowered {
		r := result.Files[name]
		r.Go = source
		r.Expanded = source
		r.Diagnostics = eqCfg.Maps[name]
		result.Files[name] = r
		files[name] = source
	}
	if err := checkEnumEquality(path, files, imp, eqCfg.Enums); err != nil {
		return result, err
	}
	debugTypes := map[string]bool{}
	maps := map[string]*sourcemap.Map{}
	for name, r := range result.Files {
		for name := range r.debugTypes {
			debugTypes[name] = true
		}
		maps[name] = r.Diagnostics
	}
	printed, err := printing.Lower(files, printing.Config{Importer: imp, PackagePath: path, DebugTypes: debugTypes, Maps: maps})
	if err != nil {
		return result, err
	}
	for name, source := range printed {
		r := result.Files[name]
		r.Go = source
		r.Expanded = source
		refreshPackageMap(&r)
		result.Files[name] = r
		files[name] = source
	}
	matchTemps := map[string]map[string]bool{}
	for name, r := range result.Files {
		matchTemps[name] = r.matchReferenceTemps
	}
	for name, source := range files {
		r := result.Files[name]
		cleaned := unusedValidatorOwnerImports(name, source, r.Diagnostics)
		if !bytes.Equal(source, cleaned) {
			r.Go, r.Expanded = cleaned, cleaned
			result.Files[name] = r
			files[name] = cleaned
		}
	}
	// Finish erases permission IR; enforce source operators before that
	// transformation can discard provenance for replaced syntax.
	if result.useContext != nil {
		for name, r := range result.Files {
			maps[name] = r.Diagnostics
		}
		if err := result.useContext.validateSelectors(path, files, maps, imp); err != nil {
			return result, err
		}
	}
	for _, name := range sortedSources(files) {
		referenceDecls[name] = append(append([]enums.Decl(nil), families...), result.imported[name]...)
	}
	projections, updatedEnumTypes, observers, err := enumReferenceEvidence(path, files, imp, referenceDecls, result.enumImports)
	if err != nil {
		return result, err
	}
	for id := range updatedEnumTypes {
		enumTypes[id] = true
	}
	projected, projectionBindings, err := refs.BindProjectionsWithPath(path, files, imp, projections)
	if err != nil {
		return result, stageError("package refs.BindProjections", err)
	}
	for id := range projectionBindings.ReferenceElements {
		enumTypes[id] = true
	}
	projectionMaps := map[string]*sourcemap.Map{}
	for name, source := range projected {
		projectionMaps[name] = projectionBindings.MapSource(name, source, result.Files[name].Diagnostics)
	}
	finished, err := refs.FinishPackageWithEvidence(path, projected, imp, refs.PermissionEvidence{EnumTypes: enumTypes, Observers: observers}, matchTemps)
	if err != nil {
		return result, stageError("package refs.Finish", mapReferenceError(err, projectionMaps))
	}
	for name, ref := range finished {
		ref.Source, err = projectionBindings.Restore(name, ref.Source)
		if err != nil {
			return result, err
		}
		ref.Source, err = bindings.Restore(name, ref.Source)
		if err != nil {
			return result, err
		}
		r := result.Files[name]
		if !bytes.Equal(r.Go, ref.Source) {
			if r.Diagnostics != nil {
				r.Diagnostics = r.Diagnostics.Compose(r.Go, ref.Source, "package refs.Finish")
			}
			r.Go = ref.Source
			r.Expanded = ref.Source
			r.References = ref.References
			refreshPackageMap(&r)
			result.Files[name] = r
			files[name] = ref.Source
		}
	}
	if result.useContext != nil {
		for name, r := range result.Files {
			maps[name] = r.Diagnostics
		}
		if err := result.useContext.validateSelectors(path, files, maps, imp); err != nil {
			return result, err
		}
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

	helpers := map[string]bool{}
	for name, r := range result.Files {
		maps[name] = r.Diagnostics
		for n := range r.defaultHelpers {
			helpers[n] = true
		}
	}
	if err := checkDefaultCycles(path, files, imp, helpers, maps); err != nil {
		return result, err
	}
	result.references, err = refs.ExportMetadata(path, referenceIR, files, imp, enumTypes)
	if err != nil {
		return result, err
	}
	checkedTypes, err := typecheck.CheckPackageTypes(path, files, imp)
	if err != nil {
		return result, err
	}
	result.enumMetadata, err = collectEnumExports(path, result, checkedTypes)
	if err != nil {
		return result, err
	}
	for name, source := range files {
		r := result.Files[name]
		r.Go = source
		r.Expanded = source
		refreshPackageMap(&r)
		result.Files[name] = r
	}
	if len(result.references.Functions) > 0 {
		for _, name := range sortedSources(files) {
			if !strings.HasSuffix(name, ".gox") {
				continue
			}
			r := result.Files[name]
			r.Go = append(r.Go, []byte(fmt.Sprintf("\nconst %s = %q\n", refs.MetadataMarker, result.references.Marker()))...)
			r.Expanded = r.Go
			refreshPackageMap(&r)
			result.Files[name] = r
			break
		}
	}
	result.enumTypes = checkedTypes
	if result.enumMetadata.present() {
		if err := validateEnumMetadata(result.enumMetadata, checkedTypes, imp); err != nil {
			return result, err
		}
		if checkedTypes.Scope().Lookup(enumMetadataMarker) != nil {
			marker, err := enumPackageMarker(checkedTypes, enumMetadataMarker)
			if err != nil {
				return result, err
			}
			if marker != result.enumMetadata.Marker() {
				return result, fmt.Errorf("existing enum metadata marker does not match recovered declarations")
			}
		} else {
			markerFiles := sortedSources(files)
			for _, name := range markerFiles {
				if strings.HasSuffix(name, ".gox") {
					markerFiles = []string{name}
					break
				}
			}
			for _, name := range markerFiles {
				result.enumMarkerFile = name
				r := result.Files[name]
				r.Go = append(r.Go, []byte(fmt.Sprintf("\nconst %s = %q\n", enumMetadataMarker, result.enumMetadata.Marker()))...)
				r.Expanded = r.Go
				refreshPackageMap(&r)
				result.Files[name] = r
				break
			}
		}
	}
	if result.useContext != nil {
		if err := attachUseMetadata(&result, result.useContext); err != nil {
			return result, err
		}
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
	privateValidators := map[string]bool{}
	for _, d := range r.Enums {
		if d.Family == "" {
			continue
		}
		exportedBuiltins[d.ExportValidator] = true
		privateValidators[d.Validator] = true
		if d.ExportValidator != "" && d.PackagePath == "" {
			identityKey := "validator identity:" + d.Validator
			if prior, ok := seen[identityKey]; ok && prior != d.ExportValidator {
				return fmt.Errorf("%s: generated builtin validator name collision %s", name, d.Validator)
			}
			seen[identityKey] = d.ExportValidator
		}
		for _, v := range d.Variants {
			exportedBuiltins["GoxideNew"+d.Family+v.Name] = true
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
		builtin := strings.HasPrefix(key, "__goxide_ref_") || exportedBuiltins[key] || privateValidators[key] || strings.HasPrefix(key, "__goxide_enum_tag_Result") || strings.HasPrefix(key, "__goxide_enum_tag_Option") || strings.HasPrefix(key, "__goxide_enum_value_Result_") || strings.HasPrefix(key, "__goxide_enum_value_Option_") || strings.HasPrefix(key, "__goxide_enum_project_Result_") || strings.HasPrefix(key, "__goxide_enum_project_Option_") || strings.HasPrefix(key, "__goxide_enum_ctor_Result_") || strings.HasPrefix(key, "__goxide_enum_ctor_Option_") || key == "Result" || key == "Option" || key == "__goxide_enum_validate_Result" || key == "__goxide_enum_validate_Option"
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
		if id, ok := n.(*ast.Ident); ok && (strings.HasPrefix(id.Name, "__goxide_write") || strings.HasPrefix(id.Name, "__goxide_read")) {
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
	// A later file can lose every shared-family declaration during deduplication.
	// Keep no unused generated import in that case.
	raw := out.Bytes()
	if parsed, err := parser.ParseFile(token.NewFileSet(), name, raw, 0); err == nil {
		used := false
		ast.Inspect(parsed, func(n ast.Node) bool {
			if x, ok := n.(*ast.SelectorExpr); ok {
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "__goxide_enum_std" {
					used = true
				}
			}
			return true
		})
		if !used {
			raw = bytes.Replace(raw, []byte("import __goxide_enum_std "), []byte("import _ "), 1)
		}
	}
	formatted, err := format.Source(raw)
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
	obj["references"] = r.References
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

// Owner imports must stay available during typed candidate selection. Once all
// sum operations are lowered, only selected owners need a named Go import.
func unusedValidatorOwnerImports(name string, source []byte, provenance *sourcemap.Map) []byte {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, name, source, 0)
	if err != nil {
		return source
	}
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})
	result := append([]byte(nil), source...)
	for i := len(f.Imports) - 1; i >= 0; i-- {
		im := f.Imports[i]
		if im.Name == nil || !strings.HasPrefix(im.Name.Name, "__goxide_enum_validator_owner_") || used[im.Name.Name] {
			continue
		}
		a, b := fs.Position(im.Name.Pos()).Offset, fs.Position(im.Name.End()).Offset
		var next *sourcemap.Map
		if provenance != nil {
			next = &sourcemap.Map{Stages: append(append([]string(nil), provenance.Stages...), "package validator imports")}
			next.Segments = append(next.Segments, provenance.Copy(0, a, 0)...)
			next.Segments = append(next.Segments, provenance.Copy(b, len(result), a+1)...)
		}
		result = append(result[:a], append([]byte("_"), result[b:]...)...)
		if next != nil {
			next.Source = result
			*provenance = *next
		}
	}
	return result
}

// sourcesImport examines imports inserted by the common static-path preparation.
func sourcesImport(sources map[string][]byte, path string) bool {
	for name, source := range sources {
		file, err := parseSourceImports(name, source)
		if err != nil {
			continue
		}
		for _, spec := range file.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			if p == path {
				return true
			}
		}
	}
	return false
}
