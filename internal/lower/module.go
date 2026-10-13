package lower

import (
	"fmt"
	"go/importer"
	"go/types"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

// PrepareModule lowers all .gox files beneath sourceDirs into a temporary Go
// module. Their paths are relative to the directories' common ancestor. The
// caller owns the returned directory and must remove it after use.
func PrepareModule(sourceDirs []string, modulePath string) (string, error) {
	if len(sourceDirs) == 0 || modulePath == "" {
		return "", fmt.Errorf("source directories and module path are required")
	}
	var root string
	names := map[string]bool{}
	for _, source := range sourceDirs {
		dir, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(dir)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s: source directory required", dir)
		}
		if root == "" {
			root = dir
		} else {
			root = commonDirectory(root, dir)
		}
		if err := filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && filepath.Ext(name) == ".gox" {
				names[name] = true
			}
			return nil
		}); err != nil {
			return "", err
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no .gox source files found")
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	files := make(map[string]*syntax.File, len(ordered))
	keys := make(map[string]string, len(ordered))
	for _, name := range ordered {
		data, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
		key := path.Join(modulePath, filepath.ToSlash(rel))
		keys[name] = key
		file, diagnostics := parser.ParseFile(key, data)
		for _, d := range diagnostics {
			if d.Severity == parser.Error {
				return "", &Diagnostic{File: key, Span: d.Span, Message: d.Message, Origin: resolve.Origin{File: key, Span: d.Span}}
			}
		}
		files[key] = file
	}

	packages := resolve.Resolve(files, nil)
	imports := &moduleImporter{local: map[string]*resolve.Scope{}, external: importer.Default(), cache: map[string]*types.Package{}}
	for _, pkg := range packages.Packages {
		imports.local[filepath.ToSlash(pkg.Dir)] = pkg.Scope
	}
	resolved := resolve.Resolve(files, imports)
	// Expansion removes macro definitions, so validate the source subset first.
	if _, err := Lower(files, resolved); err != nil {
		return "", err
	}
	if len(resolved.Diagnostics) != 0 {
		return "", resolved.Diagnostics[0]
	}

	expanded := &expand.Result{
		Expansions:    map[syntax.ExpnID]expand.Expansion{},
		Contexts:      map[syntax.SyntaxCtx]expand.Context{},
		Origins:       map[syntax.Node]expand.Origin{},
		Scopes:        map[*syntax.Ident]expand.Scope{},
		Introductions: map[*syntax.Ident]expand.Key{},
	}
	for _, pkg := range packages.Packages {
		var source []*syntax.File
		for _, file := range pkg.Files {
			source = append(source, file.AST)
		}
		part, err := expand.Expand(source, nil)
		if err != nil {
			return "", err
		}
		expanded.Files = append(expanded.Files, part.Files...)
		for node, origin := range part.Origins {
			expanded.Origins[node] = origin
		}
		for id, scope := range part.Scopes {
			expanded.Scopes[id] = scope
		}
	}
	resolved = resolve.ResolveExpanded(files, expanded, imports)
	expandedFiles := make(map[string]*syntax.File, len(resolved.Files))
	for name, file := range resolved.Files {
		expandedFiles[name] = file.AST
	}
	output, err := Lower(expandedFiles, resolved)
	if err != nil {
		return "", err
	}
	if len(resolved.Diagnostics) != 0 {
		return "", resolved.Diagnostics[0]
	}

	dir, err := os.MkdirTemp("", "goxide-lower-")
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			os.RemoveAll(dir)
		}
	}()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+strconv.Quote(modulePath)+"\n\ngo 1.27\n"), 0o644); err != nil {
		return "", err
	}
	for _, name := range ordered {
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return "", err
		}
		target := filepath.Join(dir, strings.TrimSuffix(rel, ".gox")+".go")
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, output[keys[name]], 0o644); err != nil {
			return "", err
		}
	}
	complete = true
	return dir, nil
}

type moduleImporter struct {
	local    map[string]*resolve.Scope
	external types.Importer
	cache    map[string]*types.Package
}

func (i *moduleImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	packagePath := strings.Join(parts, "/")
	if scope, ok := i.local[packagePath]; ok {
		if symbol := scope.Bindings[name]; symbol != nil && symbol.Visibility == resolve.Public {
			return []resolve.External{{Name: name, Kind: symbol.Kind, GoName: symbol.GoName, Visibility: resolve.Public}}
		}
		return nil
	}
	pkg, loaded := i.cache[packagePath]
	if !loaded {
		pkg, _ = i.external.Import(packagePath)
		i.cache[packagePath] = pkg
	}
	if pkg == nil || name.Ctx != 0 {
		return nil
	}
	object := pkg.Scope().Lookup(name.Name)
	if object == nil || !object.Exported() {
		return nil
	}
	var kind resolve.Kind
	switch object.(type) {
	case *types.Const:
		kind = resolve.Constant
	case *types.Var:
		kind = resolve.Variable
	case *types.Func:
		kind = resolve.Function
	case *types.TypeName:
		kind = resolve.Type
	default:
		return nil
	}
	return []resolve.External{{Name: name, Kind: kind, GoName: object.Name(), Visibility: resolve.Public}}
}
