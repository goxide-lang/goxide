package lower

import (
	"fmt"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

// crateGraph selects authored packages before parsing or checking unrelated
// targets. Go imports add compilation membership, never source namespace exports.
type crateGraph struct {
	modules        []Module
	files          map[string][]string
	owners         map[string]*Crate
	bases          map[string]string
	edges          []nativeCrateImport
	goImports      *moduleGoImporter
	packageModules map[string]string
}

type nativeCrateImport struct {
	from, to, file string
}

type crateModule struct {
	name   string
	public bool
}

func selectCrates(modules []Module, extra map[string][]crateModule) (*crateGraph, error) {
	g := &crateGraph{modules: modules, files: map[string][]string{}, owners: map[string]*Crate{}, bases: map[string]string{}, packageModules: map[string]string{}}
	if len(modules) != 0 {
		g.goImports = sourceModuleImporter(modules[0])
	}
	for _, module := range modules {
		if len(module.Crates) == 0 {
			continue
		}
		for _, ext := range []string{".gox", ".go"} {
			names, err := moduleFiles(module.Dir, ext)
			if err != nil {
				return nil, err
			}
			for _, name := range names {
				rel, err := filepath.Rel(module.Dir, name)
				if err != nil {
					return nil, err
				}
				pkg := path.Join(module.Path, filepath.ToSlash(filepath.Dir(rel)))
				g.files[pkg] = append(g.files[pkg], name)
			}
		}
	}
	// Establish the library's actual graph before binaries consume it. File
	// targets can share a source directory without sharing every child package.
	for _, library := range []bool{true, false} {
		for index := range modules {
			module := &modules[index]
			for n := range module.Crates {
				crate := &module.Crates[n]
				if crate.Unavailable != "" || crate.Library != library {
					continue
				}
				g.bases[crate.Root] = crate.SourceDir
				if err := g.load(module, crate, crate.Root, extra); err != nil {
					return nil, err
				}
			}
		}
	}
	return g, nil
}

func (g *crateGraph) load(module *Module, crate *Crate, pkg string, extra map[string][]crateModule) error {
	if owner := g.moduleOwner(pkg); owner != "" && owner != module.Path {
		return fmt.Errorf("source package %s belongs to selected module %s, not %s", pkg, owner, module.Path)
	}
	if old := g.owners[pkg]; old != nil {
		if old.Root != crate.Root {
			return fmt.Errorf("package %s belongs to two crates: %s and %s", pkg, old.Root, crate.Root)
		}
		return nil
	}
	if pkg != crate.Root {
		base := crate.SourceDir
		if base == "" {
			base = crate.Root
		}
		if !strings.HasPrefix(pkg, base+"/") {
			return fmt.Errorf("package %s is outside crate %s", pkg, crate.Root)
		}
		for _, boundary := range module.Targets {
			if boundary.File || boundary.Path == base {
				continue
			}
			if pkg == boundary.Path || strings.HasPrefix(pkg, boundary.Path+"/") {
				return fmt.Errorf("package %s crosses independent target boundary %s", pkg, boundary.Path)
			}
		}
	}
	if len(g.files[pkg]) == 0 {
		return fmt.Errorf("mod must name a selected local source package: %s", pkg)
	}
	g.owners[pkg] = crate
	var children []string
	active := false
	context := module.Config.Environment.Context(module.Tags, module.Race)
	for _, filename := range g.files[pkg] {
		matches, err := matchesModuleSource(filename, context)
		if err != nil {
			return err
		}
		if !matches {
			continue
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			return err
		}
		key := path.Join(pkg, filepath.Base(filename))
		if strings.HasSuffix(filename, ".gox") {
			active = true
			file, diagnostics := parser.ParseFile(key, data)
			for _, d := range diagnostics {
				if d.Severity == parser.Error {
					err := &Diagnostic{File: key, Span: d.Span, Message: d.Message, Origin: resolve.Origin{File: key, Span: d.Span}}
					return FormatSourceDiagnostic(err, map[string]*syntax.File{key: file}, nil, nil)
				}
			}
			if _, err := expand.Configure(file, module.Config); err != nil {
				return err
			}
			children = append(children, crateModules(file)...)
			continue
		}
		file, err := goparser.ParseFile(gotoken.NewFileSet(), key, data, goparser.ImportsOnly|goparser.SkipObjectResolution)
		if err != nil {
			return err
		}
		var targets []string
		cgo := false
		for _, imported := range file.Imports {
			target, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			targets = append(targets, target)
			cgo = cgo || target == "C"
		}
		if cgo && !context.CgoEnabled {
			continue
		}
		active = true
		for _, target := range targets {
			g.edges = append(g.edges, nativeCrateImport{pkg, target, key})
			// go.require/replace can select a nested module without declaring
			// a source alias. Ask Go for its owner before treating a path
			// prefix as membership in this authored crate.
			if g.moduleOwner(target) != "" {
				if owner, standard, err := g.goImports.PackageOrigin(target); err == nil && !standard && owner != "" {
					g.packageModules[target] = owner
				}
			}
			if g.moduleOwner(target) == module.Path {
				// Cross-crate library imports are checked after public source
				// reexports resolve; they never assign a second crate owner.
				library := false
				for _, candidate := range module.Crates {
					if candidate.Library && candidate.Root != crate.Root &&
						(target == candidate.Root || g.owners[target] != nil && g.owners[target].Root == candidate.Root) {
						library = true
					}
				}
				if !library {
					if err := g.load(module, crate, target, extra); err != nil {
						return fmt.Errorf("%s: native Go import: %w", key, err)
					}
				}
			}
		}
	}
	if !active {
		return fmt.Errorf("package %s has no source for the selected build configuration", pkg)
	}
	for _, child := range extra[pkg] {
		children = append(children, child.name)
	}
	sort.Strings(children)
	for _, child := range children {
		base := pkg
		if mapped := g.bases[pkg]; mapped != "" {
			base = mapped
		}
		if err := g.load(module, crate, path.Join(base, child), extra); err != nil {
			return err
		}
	}
	return nil
}

func (g *crateGraph) moduleOwner(pkg string) string {
	if owner, ok := g.packageModules[pkg]; ok {
		return owner
	}
	owner := ""
	for _, module := range g.modules {
		candidate := module.Path
		if (pkg == candidate || strings.HasPrefix(pkg, candidate+"/")) && len(candidate) > len(owner) {
			owner = candidate
		}
		for _, root := range module.Roots {
			candidate := root.ModulePath
			if candidate != "" && (pkg == candidate || strings.HasPrefix(pkg, candidate+"/")) && len(candidate) > len(owner) {
				owner = candidate
			}
		}
	}
	return owner
}

func crateModules(file *syntax.File) []string {
	var names []string
	syntax.Walk(file, func(node syntax.Node) bool {
		if mod, ok := node.(*syntax.ModDecl); ok && !mod.Inline {
			names = append(names, mod.Name.Name)
		}
		return true
	})
	return names
}

func (g *crateGraph) selected(module Module, pkg string) bool {
	return len(module.Crates) == 0 || g.owners[pkg] != nil
}

func (g *crateGraph) roots(module Module, pkg string, roots paths.Roots) (paths.Roots, string) {
	crate := g.owners[pkg]
	if crate == nil {
		return roots, module.Path
	}
	out := make(paths.Roots, len(roots)+2)
	for name, root := range roots {
		out[name] = root
	}
	out["crate"] = paths.Namespace{PackagePath: crate.Root, ModulePath: module.Path}
	if !crate.Library {
		for _, library := range module.Crates {
			if library.Library {
				out[library.Name] = paths.Namespace{PackagePath: library.Root, ModulePath: module.Path, Unavailable: library.Unavailable}
			}
		}
	}
	return out, pkg
}

// Expanded item macros can add module declarations. Repeat selection before
// resolving those declarations; the finite package graph bounds this loop.
func (g *crateGraph) expandedModules(expanded *expand.Result, configs map[*syntax.File]moduleRootConfig, extra map[string][]crateModule) bool {
	known := map[string]map[string]bool{}
	for file, cfg := range configs {
		pkg := path.Dir(cfg.name)
		if known[pkg] == nil {
			known[pkg] = map[string]bool{}
		}
		for _, name := range crateModules(file) {
			known[pkg][name] = true
		}
		for _, mod := range extra[pkg] {
			known[pkg][mod.name] = true
		}
	}
	changed := false
	files := expanded.Files
	if len(expanded.PartialFiles) > 0 {
		files = expanded.PartialFiles
	}
	for _, file := range files {
		origin := expanded.Origins[file].File
		cfg, ok := configs[origin]
		if !ok {
			continue
		}
		pkg := path.Dir(cfg.name)
		if g.owners[pkg] == nil {
			continue
		}
		syntax.Walk(file, func(node syntax.Node) bool {
			mod, ok := node.(*syntax.ModDecl)
			if !ok || mod.Inline || known[pkg][mod.Name.Name] {
				return true
			}
			// A Go import may have loaded the package without introducing
			// this source binding. Selection also checks its crate ownership.
			known[pkg][mod.Name.Name] = true
			extra[pkg] = append(extra[pkg], crateModule{mod.Name.Name, mod.Vis.Kind == syntax.VisPub})
			changed = true
			return true
		})
	}
	return changed
}

// The next macro-discovery pass needs module bindings already produced by the
// previous expansion. Keep them on temporary declaration trees: expanding the
// original source again must not emit duplicate mod declarations.
func (g *crateGraph) preliminaryFiles(files map[string]*syntax.File, extra map[string][]crateModule) map[string]*syntax.File {
	out := make(map[string]*syntax.File, len(files))
	var names []string
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	added := map[string]bool{}
	for _, name := range names {
		file := files[name]
		out[name] = file
		pkg := path.Dir(name)
		if len(extra[pkg]) == 0 || added[pkg] || file.NativeGo {
			continue
		}
		added[pkg] = true
		copy := *file
		copy.Items = append([]syntax.Item(nil), file.Items...)
		var id syntax.NodeID
		syntax.Walk(file, func(node syntax.Node) bool {
			if node.ID() > id {
				id = node.ID()
			}
			return true
		})
		for _, mod := range extra[pkg] {
			decl := &syntax.ModDecl{Name: &syntax.Ident{Name: mod.name}}
			if mod.public {
				decl.Vis.Kind = syntax.VisPub
			}
			syntax.Walk(decl, func(node syntax.Node) bool {
				id++
				node.(interface{ SetID(syntax.NodeID) }).SetID(id)
				return true
			})
			copy.Items = append(copy.Items, decl)
		}
		out[name] = &copy
	}
	return out
}

func (g *crateGraph) Lookup([]string, resolve.Key) []resolve.External { return nil }

func (g *crateGraph) ModulePath(pkg, child string) []string {
	if base := g.bases[pkg]; base != "" {
		pkg = base
	}
	return strings.Split(path.Join(pkg, child), "/")
}

// Remove only unreachable staged source, so downstream go test ./... cannot
// compile a different target accidentally. The user's checkout is untouched.
func (g *crateGraph) prune() error {
	for pkg, names := range g.files {
		if g.owners[pkg] != nil {
			continue
		}
		for _, name := range names {
			if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
