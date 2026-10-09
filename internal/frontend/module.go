package frontend

import (
	"bytes"
	"context"
	"fmt"
	"github.com/hxsf/goxide/internal/naming"
	"github.com/hxsf/goxide/internal/paths"
	"go/build"
	"go/types"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/macros"
	"github.com/hxsf/goxide/internal/project"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/rootconfig"
	"github.com/hxsf/goxide/internal/typecheck"
)

// ModuleResult names the real Go module and the generated per-file artifacts.
type ModuleResult struct {
	Root, Target, ModulePath, SourceRoot string
	Files                                map[string]Result
}

// CompileModule copies an existing module to a fresh destination, lowers each
// package independently, then checks packages in import dependency order.
// Relative filesystem replacements are rebased to preserve their meaning.
func CompileModule(input, destination string) (ModuleResult, error) {
	return CompileModuleWithTags(input, destination, nil)
}

// CompileModuleWithTags selects goxide and native files using the same host Go build
// context. The generated module is a view of this one explicit configuration.
func CompileModuleWithTags(input, destination string, tags []string) (ModuleResult, error) {
	return CompileModuleWithOptions(input, destination, ModuleOptions{Tags: tags})
}

type ModuleOptions struct {
	Roots         paths.Roots // Explicit external namespaces, shared with single-file and WASM compilation.
	WorkspaceFile string      // Explicit planner-owned Go workspace; ambient GOWORK is ignored.
	ModuleMode    string      // -mod=readonly for the owned workspace.
	// SourceModuleRoot/File are a trusted project-planner virtual Go module, never source directives.
	SourceModuleRoot string
	SourceModuleFile []byte
	// Context cancels module traversal and importer subprocesses; CPU passes check between packages.
	Context    context.Context
	Tags       []string
	Race       bool
	Features   map[string]bool
	OnlyTarget bool
	Prepare    func(string) error
	// EntryFiles maps explicitly registered source files to generated module paths.
	// An empty destination excludes an unselected entry without parsing it.
	EntryFiles    map[string]string
	ExcludeDirs   map[string]bool
	TargetPackage string
	Configuration string
	// GeneratedFiles are validated module-relative build-script outputs. They join
	// ordinary package selection/lowering without modifying the source tree.
	GeneratedFiles map[string][]byte
	// Environment scopes source selection/importer subprocesses for host tools.
	// Nil preserves ambient environment. Never populated by generated script output.
	Environment []string
	// ManagedOutputRoot is an ownership-checked exception to source-tree outputs.
	ManagedOutputRoot string
}

func CompileModuleWithOptions(input, destination string, options ModuleOptions) (ModuleResult, error) {
	if options.Context == nil {
		options.Context = context.Background()
	}
	if err := options.Context.Err(); err != nil {
		return ModuleResult{}, err
	}
	ctx := build.Default
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	}
	envValues := map[string]string{}
	for _, value := range environment {
		key, val, ok := strings.Cut(value, "=")
		if ok {
			envValues[key] = val
		}
	}
	ctx.BuildTags = append([]string(nil), options.Tags...)
	if options.Race {
		ctx.BuildTags = append(ctx.BuildTags, "race")
	}
	for key, dst := range map[string]*string{"GOOS": &ctx.GOOS, "GOARCH": &ctx.GOARCH} {
		if v := envValues[key]; v != "" {
			*dst = v
		}
	}
	if ctx.GOOS != runtime.GOOS || ctx.GOARCH != runtime.GOARCH {
		return ModuleResult{}, fmt.Errorf("directory mode currently requires host GOOS/GOARCH (%s/%s); cross-target type sizes are unsupported", runtime.GOOS, runtime.GOARCH)
	}
	if v := envValues["CGO_ENABLED"]; v != "" {
		ctx.CgoEnabled = v == "1"
	}

	input, err := filepath.Abs(input)
	if err != nil {
		return ModuleResult{}, err
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return ModuleResult{}, err
	}
	root := input
	if options.SourceModuleRoot != "" {
		root = options.SourceModuleRoot
	}
	for options.SourceModuleRoot == "" {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return ModuleResult{}, fmt.Errorf("directory mode requires an existing go.mod")
		}
		root = parent
	}
	if options.Roots == nil {
		options.Roots, err = rootconfig.Load(root)
		if err != nil {
			return ModuleResult{}, err
		}
	}
	destination, err = canonicalOutput(destination)
	if err != nil {
		return ModuleResult{}, err
	}
	insideSource := false
	if rel, e := filepath.Rel(root, destination); e == nil && (rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..") {
		insideSource = true
		if options.ManagedOutputRoot == "" {
			return ModuleResult{}, fmt.Errorf("output directory must be outside the input module")
		}
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return ModuleResult{}, fmt.Errorf("output directory must not exist: %s", destination)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if options.SourceModuleRoot != "" {
		mod = options.SourceModuleFile
		err = nil
	}
	if err != nil {
		return ModuleResult{}, err
	}
	modulePath := ""
	lines := strings.Split(string(mod), "\n")
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			modulePath = strings.Trim(fields[1], "\"")
		}
		if at := strings.Index(line, "=>"); at >= 0 {
			tail := strings.TrimSpace(line[at+2:])
			tokenText := strings.Fields(tail)
			if len(tokenText) > 0 {
				spelling := tokenText[0]
				if strings.HasPrefix(tail, "\"") {
					var err error
					spelling, err = strconv.QuotedPrefix(tail)
					if err != nil {
						return ModuleResult{}, fmt.Errorf("go.mod replacement: %w", err)
					}
				}
				path := spelling
				if strings.HasPrefix(spelling, "\"") {
					path, _ = strconv.Unquote(spelling)
				}
				if strings.HasPrefix(path, ".") {
					lines[i] = line[:at+2] + " " + strconv.Quote(filepath.ToSlash(filepath.Join(root, path))) + tail[len(spelling):]
				}
			}
		}
	}
	if modulePath == "" {
		return ModuleResult{}, fmt.Errorf("go.mod has no module directive")
	}
	if insideSource {
		managed, e := project.ManagedSourceDirectory(options.ManagedOutputRoot, root, modulePath)
		rel, re := filepath.Rel(options.ManagedOutputRoot, destination)
		if e != nil {
			return ModuleResult{}, e
		}
		if !managed || re != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ModuleResult{}, fmt.Errorf("source-tree output must be beneath the verified managed target directory")
		}
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return ModuleResult{}, err
	}
	success := false
	defer func() {
		if !success {
			os.RemoveAll(destination)
		}
	}()
	if options.SourceModuleRoot != "" {
		if err = os.WriteFile(filepath.Join(destination, "go.mod"), []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return ModuleResult{}, err
		}
	}
	packages := map[string]map[string][]byte{}
	sourceOutputs := map[string]string{}
	testSources := map[string]map[string][]byte{}
	outputOwners := map[string]string{}
	selectedTests := map[string]map[string][]byte{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := options.Context.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			if err := naming.Path(path); err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		// Worktrees can use a .git file rather than a directory. Neither belongs
		// in the independently buildable generated module.
		if entry.Name() == ".git" || entry.Name() == ".goxide" || entry.Name() == ".goxide-migration" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("module input symlinks are unsupported: %s", path)
		}
		if entry.IsDir() {
			managed, e := project.ManagedSourceDirectory(path, root, modulePath)
			if e != nil {
				return e
			}
			if managed {
				return filepath.SkipDir
			}
		}
		if entry.IsDir() && options.ExcludeDirs[rel] {
			return filepath.SkipDir
		}
		if mapped, ok := options.EntryFiles[rel]; ok {
			if mapped == "" {
				return nil
			}
			rel = mapped
		}
		sourceOutputs[path] = rel
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".goxide" || entry.Name() == ".goxide-migration" {
				return filepath.SkipDir
			}
			if _, e := os.Stat(filepath.Join(path, "goxide.toml")); e == nil {
				return filepath.SkipDir
			}
			if _, e := os.Stat(filepath.Join(path, "go.mod")); e == nil {
				return filepath.SkipDir
			}
			return os.MkdirAll(target, 0755)
		}
		if options.Features != nil && (entry.Name() == "goxide.toml" || entry.Name() == "hgo.json" || entry.Name() == "hgo.lock" || entry.Name() == "goxide.lock") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		if options.SourceModuleRoot != "" && (rel == "go.mod" || rel == "go.sum") {
			return nil
		}
		if rel == "go.mod" {
			data = []byte(strings.Join(lines, "\n"))
		}
		ext := filepath.Ext(path)
		if !options.OnlyTarget && options.Features != nil && (ext == ".gox" || ext == ".go") {
			var selected bool
			data, selected, e = ConfigureSource(path, data, options.Features)
			if e != nil {
				return e
			}
			if !selected {
				return nil
			}
		}
		sourceDir := true
		for _, part := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
			if part != "." && (part == "testdata" || part == "vendor" || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_")) {
				sourceDir = false
			}
		}
		goxideSource := ext == ".gox" && sourceDir
		if !options.OnlyTarget && goxideSource && strings.HasSuffix(strings.TrimSuffix(entry.Name(), ext), "_test") {
			return fmt.Errorf("%s: .gox test files are not yet supported; use native Go tests", path)
		}
		if goxideSource || sourceDir && ext == ".go" && !strings.HasSuffix(path, "_test.go") {
			selected := options.OnlyTarget
			var err error
			if !selected {
				selected, err = matchSourceFile(ctx, path, data)
			}
			if err != nil {
				return err
			}
			if selected {
				if goxideSource && !options.OnlyTarget {
					if _, err := os.Stat(strings.TrimSuffix(path, ".gox") + ".go"); err == nil {
						return fmt.Errorf("%s: generated Go filename conflicts with native source", path)
					}
				}
				dir := filepath.Dir(rel)
				if packages[dir] == nil {
					packages[dir] = map[string][]byte{}
				}
				packages[dir][path] = data
			}
		}
		if options.OnlyTarget && sourceDir && strings.HasSuffix(path, "_test.go") {
			dir := filepath.Dir(rel)
			if testSources[dir] == nil {
				testSources[dir] = map[string][]byte{}
			}
			testSources[dir][path] = data
			return nil
		}
		if goxideSource {
			return nil
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		mode := info.Mode().Perm()
		// Only the owned generated source copy is rewritten by lowering.
		if sourceDir && ext == ".go" {
			mode |= 0200
		}
		return os.WriteFile(target, data, mode)
	})
	if err != nil {
		return ModuleResult{}, err
	}

	// Script outputs enter the same package maps as source files, before import
	// discovery. Validate against final mapped source names, including .gox -> .go.
	generatedNames := make([]string, 0, len(options.GeneratedFiles))
	for name := range options.GeneratedFiles {
		generatedNames = append(generatedNames, name)
	}
	sort.Strings(generatedNames)
	occupied := map[string]string{}
	finalName := func(name string) string {
		if strings.HasSuffix(name, ".gox") {
			name = strings.TrimSuffix(name, ".gox") + ".go"
		}
		return strings.ToLower(filepath.ToSlash(name))
	}
	for source, rel := range sourceOutputs {
		occupied[finalName(rel)] = source
	}
	for _, name := range generatedNames {
		if err := naming.Source(name, options.GeneratedFiles[name]); err != nil {
			return ModuleResult{}, err
		}
		rel := filepath.FromSlash(name)
		if strings.HasSuffix(strings.ToLower(name), ".go.map.json") {
			return ModuleResult{}, fmt.Errorf("reserved generated source-map path %q", name)
		}
		if rel == "." || filepath.IsAbs(rel) || filepath.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.Contains(name, "\\") || strings.Contains(name, ":") {
			return ModuleResult{}, fmt.Errorf("invalid generated input path %q", name)
		}
		for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
			if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") || part == "vendor" || part == "testdata" || part == "goxide_targets" || part == "goxide_project_config.go" || part == "go.mod" || part == "go.sum" || part == "goxide.toml" || part == "hgo.json" || part == "hgo.lock" || part == "goxide.lock" {
				return ModuleResult{}, fmt.Errorf("reserved generated input path %q", name)
			}
		}
		if previous, ok := occupied[finalName(rel)]; ok {
			return ModuleResult{}, fmt.Errorf("generated output collides with source/output: %s and %s", name, previous)
		}
		occupied[finalName(rel)] = name
		path := filepath.Join(root, rel)
		data := options.GeneratedFiles[name]
		ext := filepath.Ext(rel)
		if ext == ".go" || ext == ".gox" {
			if strings.HasSuffix(strings.TrimSuffix(rel, ext), "_test") {
				return ModuleResult{}, fmt.Errorf("generated test sources are unsupported: %s", name)
			}
			dir := filepath.Dir(rel)
			if packages[dir] == nil {
				packages[dir] = map[string][]byte{}
			}
			packages[dir][path] = data
		}
		sourceOutputs[path] = rel
		if ext != ".gox" {
			output := filepath.Join(destination, rel)
			if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
				return ModuleResult{}, err
			}
			if err := os.WriteFile(output, data, 0644); err != nil {
				return ModuleResult{}, err
			}
		}
	}

	if options.Prepare != nil {
		if err = options.Prepare(destination); err != nil {
			return ModuleResult{}, err
		}
	}
	result := ModuleResult{Root: destination, ModulePath: modulePath, SourceRoot: root, Files: map[string]Result{}}
	relTarget, _ := filepath.Rel(root, input)
	if options.TargetPackage != "" {
		relTarget = options.TargetPackage
	}
	result.Target = "./" + filepath.ToSlash(relTarget)
	if relTarget == "." {
		result.Target = "."
	}
	localPackages := map[string]bool{}
	for dir := range packages {
		p := modulePath
		if dir != "." {
			p += "/" + filepath.ToSlash(dir)
		}
		localPackages[p] = true
	}
	metadata := map[string]PackageResult{}
	buildBudget := &macros.Budget{}
	state := map[string]int{}
	var compileDir func(string) error
	compileDir = func(dir string) error {
		if err := options.Context.Err(); err != nil {
			return err
		}
		if state[dir] == 2 {
			return nil
		}
		if state[dir] == 1 {
			return fmt.Errorf("module import cycle at %s", dir)
		}
		state[dir] = 1
		if options.OnlyTarget {
			for _, name := range sortedSources(packages[dir]) {
				data := packages[dir][name]
				selected, e := matchSourceFile(ctx, name, data)
				if e != nil {
					return e
				}
				if selected && options.Features != nil {
					data, selected, e = ConfigureSource(name, data, options.Features)
					if e != nil {
						return e
					}
				}
				if !selected {
					delete(packages[dir], name)
					if strings.HasSuffix(name, ".go") {
						os.Remove(filepath.Join(destination, sourceOutputs[name]))
					}
					continue
				}
				if strings.HasSuffix(name, "_test.gox") {
					return fmt.Errorf("%s: .gox test files are not yet supported; use native Go tests", name)
				}
				packages[dir][name] = data
				if strings.HasSuffix(name, ".go") {
					if e = os.WriteFile(filepath.Join(destination, sourceOutputs[name]), data, 0644); e != nil {
						return e
					}
				}
			}
			// Go test sources are outside normal package type checking. Configure
			// them only for test mode; ordinary builds must not resolve their cfg.
			for name, data := range testSources[dir] {
				output := filepath.Join(destination, sourceOutputs[name])
				if !options.Race {
					os.Remove(output)
					continue
				}
				selected, e := matchSourceFile(ctx, name, data)
				if e != nil {
					return e
				}
				if selected && options.Features != nil {
					data, selected, e = ConfigureSource(name, data, options.Features)
				}
				if e != nil {
					return e
				}
				if !selected {
					os.Remove(output)
					continue
				}
				if e = os.WriteFile(output, data, 0644); e != nil {
					return e
				}
				if selectedTests[dir] == nil {
					selectedTests[dir] = map[string][]byte{}
				}
				selectedTests[dir][name] = data
			}
		}
		if len(packages[dir]) == 0 {
			return fmt.Errorf("package %s has no selected source files", dir)
		}
		// Reserve final paths after selection, including moved goxide entries.
		for _, name := range sortedSources(packages[dir]) {
			output := sourceOutputs[name]
			if strings.HasSuffix(output, ".gox") {
				output = strings.TrimSuffix(output, ".gox") + ".go"
			}
			if prior, ok := outputOwners[output]; ok && prior != name {
				return fmt.Errorf("generated Go filename conflicts: %s and %s -> %s", prior, name, output)
			}
			outputOwners[output] = name
		}
		configuredSources, configFile, e := packageConfigurationSources(packages[dir], options.Roots, options.Configuration, filepath.Join(root, dir, packageConfigurationFile))
		if e != nil {
			return e
		}
		if configFile != "" {
			output := filepath.Join(destination, dir, packageConfigurationFile)
			if _, exists := outputOwners[filepath.Join(dir, packageConfigurationFile)]; exists {
				return fmt.Errorf("reserved project config filename conflicts: %s", output)
			}
			if _, e = os.Stat(output); !os.IsNotExist(e) {
				return fmt.Errorf("reserved project config filename conflicts: %s", output)
			}
			packages[dir] = configuredSources
			sourceOutputs[configFile] = filepath.Join(dir, packageConfigurationFile)
			if e = os.MkdirAll(filepath.Dir(output), 0755); e != nil {
				return e
			}
			if e = os.WriteFile(output, configuredSources[configFile], 0644); e != nil {
				return e
			}
		}
		pkgPath := modulePath
		if dir != "." {
			pkgPath += "/" + filepath.ToSlash(dir)
		}
		packageRoots, e := LocalDeclarations(pkgPath, packages[dir], options.Roots, localPackages, modulePath)
		if e != nil {
			return e
		}
		plans, e := PreparePackageSourcePaths(pkgPath, packages[dir], packageRoots, localPackages)
		if e != nil {
			return e
		}
		for _, name := range sortedSources(packages[dir]) {
			source := packages[dir][name]
			var dependencies []string
			if isGoxideInput(name) {
				dependencies = plans[name].Imports
			} else {
				f, e := parseSourceImports(name, source)
				if e != nil {
					return e
				}
				for _, spec := range f.Imports {
					dependency, _ := strconv.Unquote(spec.Path.Value)
					dependencies = append(dependencies, dependency)
				}
			}
			for _, dependency := range dependencies {
				dep := ""
				if dependency == modulePath {
					dep = "."
				} else if strings.HasPrefix(dependency, modulePath+"/") {
					dep = filepath.FromSlash(strings.TrimPrefix(dependency, modulePath+"/"))
				} else {
					continue
				}
				if _, ok := packages[dep]; ok {
					if e := compileDir(dep); e != nil {
						return e
					}
				}
			}
		}
		baseImporter := typecheck.NewImporterWithWorkspace(options.Context, filepath.Join(destination, dir, "package.go"), options.Tags, options.Race, options.Environment, options.WorkspaceFile, options.ModuleMode)
		var packageImporter types.Importer = selectedModuleImporter{base: baseImporter, prepare: func(dependency string) error {
			if dependency == pkgPath || !localPackages[dependency] {
				return nil
			}
			dep := "."
			if dependency != modulePath {
				dep = filepath.FromSlash(strings.TrimPrefix(dependency, modulePath+"/"))
			}
			return compileDir(dep)
		}}
		p, e := preparePackageContext(pkgPath, packages[dir], metadata, packageImporter, packageRoots, buildBudget)
		if e != nil {
			return e
		}
		if e := options.Context.Err(); e != nil {
			return e
		}
		checked, e := finishPackage(pkgPath, p, packageImporter)
		if e != nil {
			return e
		}
		emitted, e := PackageGoFiles(pkgPath, checked, packageImporter)
		if e != nil {
			return e
		}
		for name, r := range emitted {
			// Native Go files only change when carrying a transparent-alias marker.
			_, sourceFile := checked.Files[name]
			if sourceFile && !strings.HasSuffix(name, ".gox") && checked.enumMarkerFile != name && (checked.useContext == nil || checked.useContext.generatedFile != name) {
				continue
			}
			rel, ok := sourceOutputs[name]
			if !ok {
				rel = filepath.Join(dir, filepath.Base(name))
			}
			goName := rel
			if strings.HasSuffix(rel, ".gox") {
				goName = strings.TrimSuffix(rel, ".gox") + ".go"
			}
			if !sourceFile {
				// Unselected native files and resources were copied earlier, but
				// do not occur in checked.Files or outputOwners. A new helper may
				// never replace one of those original package files.
				if _, err := os.Lstat(filepath.Join(destination, goName)); err == nil {
					return fmt.Errorf("generated helper filename conflicts with existing output: %s", goName)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			if prior, ok := outputOwners[goName]; ok && prior != name {
				return fmt.Errorf("generated Go filename conflicts: %s and %s -> %s", prior, name, goName)
			}
			outputOwners[goName] = name
			if e = os.MkdirAll(filepath.Dir(filepath.Join(destination, goName)), 0755); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(destination, goName), r.Go, 0644); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(destination, goName+".map.json"), r.SourceMap, 0644); e != nil {
				return e
			}
			result.Files[goName] = r
		}

		if len(checked.references.Functions) > 0 || checked.enumMetadata.present() || checked.uses.present() {
			sourceFiles := map[string][]byte{}
			for name := range emitted {
				base := filepath.Base(name)
				if strings.HasSuffix(base, ".gox") {
					base = strings.TrimSuffix(base, ".gox") + ".go"
				}
				data, e := os.ReadFile(filepath.Join(destination, dir, base))
				if e != nil {
					return e
				}
				sourceFiles[base] = data
			}
			if e = writeUseArtifact(filepath.Join(destination, dir), pkgPath, checked.uses, sourceFiles, packageImporter); e != nil {
				return e
			}
			if len(checked.references.Functions) > 0 {
				data, e := refs.MarshalMetadataArtifact(checked.references, sourceFiles, packageImporter)
				if e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(destination, dir, refs.MetadataFilename), data, 0644); e != nil {
					return e
				}
			}
			if checked.enumMetadata.present() {
				provider, ok := packageImporter.(refs.ArtifactImporter)
				if !ok {
					return fmt.Errorf("enum artifact source importer unavailable")
				}
				data, e := marshalEnumArtifact(checked.enumMetadata, sourceFiles, provider)
				if e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(destination, dir, enumMetadataFilename), data, 0644); e != nil {
					return e
				}
			}
		}

		metadata[pkgPath] = checked
		state[dir] = 2
		// Test-only dependencies may import the package under test. Finish the
		// ordinary package first so this legitimate edge is not an import cycle.
		for _, name := range sortedSources(selectedTests[dir]) {
			f, e := parseSourceImports(name, selectedTests[dir][name])
			if e != nil {
				return e
			}
			for _, spec := range f.Imports {
				imp, _ := strconv.Unquote(spec.Path.Value)
				dep := ""
				if imp == modulePath {
					dep = "."
				} else if strings.HasPrefix(imp, modulePath+"/") {
					dep = filepath.FromSlash(strings.TrimPrefix(imp, modulePath+"/"))
				} else {
					continue
				}
				if _, ok := packages[dep]; ok {
					if e := compileDir(dep); e != nil {
						return e
					}
				}
			}
		}
		return nil
	}
	dirs := sortedPackageDirs(packages)
	if options.OnlyTarget {
		dirs = []string{relTarget}
		if len(packages[relTarget]) == 0 {
			return result, fmt.Errorf("target %s has no selected source files", relTarget)
		}
	}
	for _, dir := range dirs {
		if err = compileDir(dir); err != nil {
			return result, err
		}
	}
	success = true
	return result, nil
}
func sortedPackageDirs(packages map[string]map[string][]byte) []string {
	m := map[string][]byte{}
	for k := range packages {
		m[k] = nil
	}
	return sortedSources(m)
}

func canonicalOutput(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	parent := path
	var suffix []string
	for {
		resolved, e := filepath.EvalSymlinks(parent)
		if e == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(e) {
			return "", e
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", e
		}
		suffix = append(suffix, filepath.Base(parent))
		parent = next
	}
}

// MatchFile understands only Go extensions. Present goxide bytes through a virtual
// .go filename, preserving suffix selection and the standard constraint parser.
func matchSourceFile(ctx build.Context, path string, source []byte) (bool, error) {
	name := filepath.Base(path)
	if strings.HasSuffix(name, ".gox") {
		name = strings.TrimSuffix(name, ".gox") + ".go"
	}
	virtual := filepath.Join(filepath.Dir(path), name)
	ctx.OpenFile = func(name string) (io.ReadCloser, error) {
		if name == virtual {
			return io.NopCloser(bytes.NewReader(source)), nil
		}
		return os.Open(name)
	}
	return ctx.MatchFile(filepath.Dir(path), name)
}
